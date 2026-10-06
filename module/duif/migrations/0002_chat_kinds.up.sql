-- Twee stemmen in één chat. De individualistische duif
-- praat vanaf nu via een facilitator: die schrijft prose, de duif levert een
-- eigen blok. Beide zijn assistantberichten in dezelfde sessie, dus de rij
-- moet zeggen wélke stem hij draagt en, bij een duif-antwoord, op welke vraag
-- van de facilitator hij het antwoord is.
--
-- De role-CHECK blijft ongemoeid: er komen geen tool-rollen in de database.
--
-- Deze migratie draait als één transactie (parai-core zet MultiStatementEnabled
-- op false, waardoor Postgres de hele simple query impliciet omsluit). Dat is
-- precies wat we willen — ALTER, backfill en SET NOT NULL zijn samen atomair —
-- maar het betekent ook dat de ACCESS EXCLUSIVE lock van de eerste ALTER blijft
-- staan tot de laatste CREATE INDEX. Daartussen zitten drie volle passages over
-- de tabel: de CHECK-validatie, de backfill-UPDATE (herschrijft elke rij) en de
-- NOT NULL-validatie. Bij pilotvolume is dat onzichtbaar; op een grote tabel
-- gijzelt het de chat, en erger: de lockaanvraag gaat achter een lopende query
-- in de wachtrij staan en blokkeert vanaf dat moment ook alle nieuwe lezers.
-- Vandaar de twee vangrails hieronder: liever falen en opnieuw proberen dan de
-- tabel vasthouden. Groeit chat_messages ooit naar miljoenen rijen, dan splitst
-- deze migratie in drieën (kolom nullable toevoegen, gebatchte backfill buiten
-- een transactie, daarna SET NOT NULL via een NOT VALID CHECK).
--
-- SET LOCAL werkt alleen binnen een transactie. Draait deze migratie ooit
-- statement voor statement (bijvoorbeeld met de hand in psql), dan zijn de
-- twee regels een no-op met een waarschuwing — geen fout. Dat is de goede
-- kant om op te falen: de vangrail verdwijnt, de migratie niet.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '5min';

ALTER TABLE duif.chat_messages
    ADD COLUMN kind text NOT NULL DEFAULT 'text'
        CHECK (kind IN ('text', 'pigeon_answer')),
    -- Leeg in plaats van NULL: elke lezer krijgt dan een string en nooit een
    -- nulwaarde die per driver anders scant. Oude rijen dragen '' en renderen
    -- daarmee als vanouds (de duif zelf).
    ADD COLUMN persona text NOT NULL DEFAULT '',
    -- question is de facilitator-vraag bij een pigeon_answer: nodig om de
    -- duif haar eigen historie terug te geven. Blijft server-side.
    ADD COLUMN question text NOT NULL DEFAULT '',
    ADD COLUMN seq bigint;

-- Op persona hoort bewust géén enum-CHECK: die waarden komen uit personas.yaml
-- en een CHECK zou elke nieuwe duif een migratie kosten — of erger, midden in
-- een stream een 500 opleveren voor een persona die de config wél kent.
-- Wat wél afdwingbaar is, is de samenhang tussen de kolommen:
ALTER TABLE duif.chat_messages
    -- Een duif-antwoord moet zeggen wiens stem het draagt, anders kan de UI het
    -- niet attribueren en valt het terug op "de duif van deze sessie".
    ADD CONSTRAINT chat_messages_pigeon_answer_has_persona
        CHECK (kind <> 'pigeon_answer' OR persona <> ''),
    -- question hoort alléén bij een duif-antwoord. Dit is net zo goed een
    -- privacygrens als een integriteitsregel: het maakt van "we schrijven
    -- question alleen in persistPigeonAnswer" een gegarandeerde eigenschap in
    -- plaats van een code-afspraak.
    ADD CONSTRAINT chat_messages_question_only_on_answers
        CHECK (kind = 'pigeon_answer' OR question = '');

-- seq is de volgorde binnen een sessie. `created_at, id` is niet genoeg: een
-- duif-artifact en de facilitator-tekst van dezelfde beurt kunnen op dezelfde
-- microseconde landen, en dan bepaalt de id-sortering de volgorde — dus niet
-- de werkelijke. Eén sequence over de hele tabel is monotoon per sessie en
-- kost niets.
CREATE SEQUENCE duif.chat_messages_seq;

-- Backfill in de volgorde die tot nu toe gold, zodat bestaande gesprekken na
-- de migratie exact hetzelfde teruglezen.
WITH volgorde AS (
    SELECT id, row_number() OVER (ORDER BY created_at ASC, id ASC) AS n
    FROM duif.chat_messages
)
UPDATE duif.chat_messages m SET seq = v.n FROM volgorde v WHERE m.id = v.id;

SELECT setval('duif.chat_messages_seq',
              COALESCE((SELECT max(seq) FROM duif.chat_messages), 0) + 1, false);

ALTER TABLE duif.chat_messages
    ALTER COLUMN seq SET DEFAULT nextval('duif.chat_messages_seq'),
    ALTER COLUMN seq SET NOT NULL;

-- De sequence hoort bij de kolom. Zonder OWNED BY blijft hij als weesobject
-- achter bij een DROP TABLE, negeert TRUNCATE … RESTART IDENTITY hem, en moet
-- de down-migratie hem met de hand opruimen.
ALTER SEQUENCE duif.chat_messages_seq OWNED BY duif.chat_messages.seq;

-- Uniek, niet zomaar een index: dat seq uit één sequence komt maakt botsingen
-- vandaag onmogelijk, maar dat is een eigenschap van de code (AppendMessage
-- laat seq bewust uit de kolomlijst), niet van het schema. Een latere import of
-- backfill die seq wél expliciet zet, zou de volgorde binnen een sessie weer
-- onbepaald maken — precies het probleem dat deze kolom oplost.
CREATE UNIQUE INDEX chat_messages_session_seq_idx
    ON duif.chat_messages (session_id, seq ASC);

-- De oude sorteerindex bediende `ORDER BY created_at, id`; sinds ListMessages
-- op seq sorteert gebruikt geen enkele query hem nog. De nieuwe index dekt
-- beide richtingen (vooruit voor ASC, achteruit voor DESC LIMIT), dus wat
-- overblijft is schrijfoverhead op elke INSERT.
DROP INDEX IF EXISTS duif.chat_messages_session_created_idx;
