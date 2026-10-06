-- LET OP: deze rollback is onomkeerbaar en niet neutraal. Hij gooit kind,
-- persona en question weg, en dat heeft drie gevolgen die verder reiken dan de
-- kolommen zelf:
--
--   1. Alle duif-antwoorden komen terug als kind='text', persona=''. De
--      facilitator krijgt ze daarna voorgeschoteld alsof hij ze zelf schreef —
--      precies de rolscheiding tussen duif en facilitator die de chat moet
--      bewaken.
--   2. De duif vindt geen enkel eigen antwoord meer terug en verliest haar
--      geheugen binnen bestaande sessies.
--   3. Een volgende up-migratie hernummert seq op (created_at, id), waardoor
--      duif- en facilitator-rijen uit dezelfde microseconde alsnog van volgorde
--      kunnen wisselen.
--
-- Herstel van de weggegooide informatie vereist een backup van vóór 0002.

-- De index uit 0001 die 0002 heeft opgeruimd, hoort bij een rollback terug.
CREATE INDEX IF NOT EXISTS chat_messages_session_created_idx
    ON duif.chat_messages (session_id, created_at ASC, id ASC);

DROP INDEX IF EXISTS duif.chat_messages_session_seq_idx;
ALTER TABLE duif.chat_messages
    DROP CONSTRAINT IF EXISTS chat_messages_question_only_on_answers,
    DROP CONSTRAINT IF EXISTS chat_messages_pigeon_answer_has_persona;
ALTER TABLE duif.chat_messages
    DROP COLUMN IF EXISTS seq,
    DROP COLUMN IF EXISTS question,
    DROP COLUMN IF EXISTS persona,
    DROP COLUMN IF EXISTS kind;
-- Met OWNED BY verdwijnt de sequence al met de kolom; deze regel is het
-- vangnet voor een database die 0002 draaide vóór die koppeling bestond.
DROP SEQUENCE IF EXISTS duif.chat_messages_seq;
