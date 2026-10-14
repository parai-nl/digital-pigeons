# Etalage-opnames — echte duivenantwoorden voor de publieke etalage

De etalage (`site/`) doet zelf geen modelaanroepen. Wat de duiven daar zeggen is
vooraf opgenomen tegen de echte module, via de taalmodelproxy van het
project, en bewaard met model, module-commit, kennisbank en tijdstip.

- **Huidige regel:** `vertex/gemini-3.6-flash-eu`, sinds 24 september 2026.
  Nieuwe opnames gaan alleen met dit model (`MODEL` in `lib/config.mjs`).
- **De opnames op de etalage:** opgenomen op 23 september 2026 met het model van
  de module op main van dat moment, `vertex_ai/gemini-3.5-flash`; de wissel zat
  daar nog niet in. Ze blijven de eerlijke uitvoer van dat model. Kiezen, bouwen
  en de tests aanvaarden het als herkomst voor een take van vóór 25 september
  2026 (`HISTORICAL_MODELS`, `modelAllowed`); een latere take met dat model
  wordt geweigerd, en opnemen gebeurt er nooit mee.

Deze map neemt op, kiest per item één take en bouwt daaruit
`site/etalage/data/opnames.json` volgens het contract onder
[Wat er in `opnames.json` staat](#wat-er-in-opnamesjson-staat), dat
`site/etalage/data.js` bij het laden controleert.

```
server.mjs          de opnameserver: database kopiëren, bouwen, starten, stoppen
speak.mjs           de vergaderuitspraken uitspreken (ElevenLabs) → uitspraken/
record-chat.mjs     chat opnemen: direct en de vergelijking in beide richtingen
record-meeting.mjs  vergadering opnemen: verse vergadering per uitspraak, duif en taal
keuze.mjs           per item één take aanwijzen → keuze.json
build.mjs           keuze.json + takes → site/etalage/data/opnames.json, stemmen, veldgeluiden
facts.json          de feiten voor de Data:-regels, geteld in de kennisbank
graph-shots.mjs     schermbeelden van de kennisgraaf → site/etalage/media/beeld/
vragen.json         de vragen en uitspraken (definitief)
takes/<id>/         alle opnames, rauw (take.json, en bij gesproken reacties stem.mp3)
lib/, test/         gedeelde logica en de tests
```

## Eisen

- Node 24 met Playwright (`npm ci` in de root van de repository), plus de
  Chromium van Playwright. Getest op macOS:
  de GPU-vlaggen voor headless Chromium (ANGLE op Metal) gelden alleen daar
  (`GPU_FLAGS` in `lib/config.mjs`); op een ander systeem vallen ze weg en is
  de vergaderopname niet getest (werkt WebGL daar niet, dan maakt de 3D-duif geen gebaar en
  laat de take `gesture` weg).
- `ffmpeg`/`ffprobe` (Homebrew 8), `cwebp`/`webpinfo`, Docker met de
  Postgres-container van de lokale werkomgeving (`ETALAGE_PG_CONTAINER`, zie
  hieronder).
- Go en Bun, en de privé Parai-checkouts naast de repository: `parai-ui`,
  `parai-graph` en `parai-core` (gebouwde schil in `frontend/build`). De
  Go-broncode van de core komt uit de pin in `module/go.mod` (zie hieronder).
  Andere paden via `PARAI_UI_DIR`, `PARAI_GRAPH_DIR`, `PARAI_CORE_DIR`, en
  `PARAI_CORE_SRC` voor een andere core dan de pin.
- `module/.env` met de sleutels van de taalmodelproxy,
  Soniox en ElevenLabs. De scripts lezen die alleen in; ze komen nooit in een
  take, log of commit, en elke take wordt vóór het opslaan gecontroleerd op
  elke geheime waarde uit dat bestand.

## De opnameserver

```
node tools/etalage-opnames/server.mjs db       # eenmalig: de eigen databasekopie maken
node tools/etalage-opnames/server.mjs start    # bouwen en starten op 127.0.0.1:8094
node tools/etalage-opnames/server.mjs status
node tools/etalage-opnames/server.mjs stop     # alleen het eigen proces; de kopie blijft
```

- **Eigen poort (8094).** De opname weigert elk proces op 8094 dat niet door
  `server.mjs start` gestart is, en neemt nooit op tegen de poorten van andere
  lokale duivenservers (`FORBIDDEN_PORTS` in `lib/config.mjs`).
- **Eigen database.** Opnemen maakt gesprekken aan in het (skipauth-)account.
  Die komen in een eigen kopie van de database met de kennisbank, gemaakt via
  `pg_dump`/`pg_restore` binnen de container; de bron wordt alleen gelezen.
  `db --replace` maakt een verse kopie (de server moet dan gestopt zijn). De
  namen van kopie, bron en container staan in `lib/config.mjs` en zijn te
  zetten met `ETALAGE_DATABASE`, `ETALAGE_SOURCE_DATABASE` en
  `ETALAGE_PG_CONTAINER`.
- **Welke parai-core.** Standaard de pin uit `module/go.mod`, gebouwd met
  `GOWORK=off` (zie `module/README.md`, "Bouwen en controleren"). Wie bewust een andere core
  wil opnemen, zet `PARAI_CORE_SRC` op een core-checkout; `server.mjs` schrijft
  dan een eigen `module/bin/etalage.go.work` (genegeerd) met een replace
  daarheen. In beide gevallen staat de core-commit in elke take
  (`server.module.paraiCore`).
- **Verplicht model.** `start` en elke opname lezen het modelbeleid uit de
  server zelf (`GET /api/duif/v1/duiven` → `modelPolicy`) en weigeren alles
  behalve `vertex/gemini-3.6-flash-eu` via het HTTPS-adres uit `PARAI_LLM_BASE_URL` in `module/.env`, voor chat
  én detectie, ook het historische `vertex_ai/gemini-3.5-flash`. Een chattake legt daarnaast het model uit het `done`-frame van
  elk antwoord vast (`provenance.model`).
- De bouwstand (commit, `module/`-tree, core-commit, bouwmoment) staat in
  `module/bin/etalage-server.json`; het serverlog in
  `module/bin/etalage-runtime.log`. Beide genegeerd. Een take neemt uit de
  bouwstand alleen commits, hashes en het bouwmoment over, nooit een lokaal
  pad (`server.module`), en legt daarnaast de commit van dit gereedschap zelf
  vast, met of de code ervan op dat moment afweek (`server.tool`). Oudere
  takes hebben dat laatste veld nog niet.
- Lokale databasenamen blijven in de genegeerde serverstand. Ze worden niet
  in takes of beeldherkomst opgenomen; in de gepubliceerde takes zijn die
  metadata verwijderd. Antwoorden, onderzoeksdata en herkomsthashes blijven
  ongewijzigd.
- De server begrenst dure verzoeken per account; de opname blijft daaronder
  en wacht bij een 429 (dat gebeurt vóór elke modelaanroep) de gevraagde tijd.

## Opnemen

```
node tools/etalage-opnames/speak.mjs                  # uitspraken uitspreken (alleen nieuwe/gewijzigde)
node tools/etalage-opnames/record-chat.mjs            # 4 vragen × 2 talen × 4 routes
node tools/etalage-opnames/record-meeting.mjs         # 4 uitspraken × 2 talen × 3 duiven
node tools/etalage-opnames/record-chat.mjs --takes 3  # drie takes per item
node tools/etalage-opnames/record-meeting.mjs --takes 3
```

Filteren kan met `--question`/`--statement`, `--lang` en (vergadering)
`--pigeon`, elk herhaalbaar. Eén take tegelijk; een run van alles duurt voor de
chat ongeveer een kwartier en voor de vergadering ongeveer twintig minuten.

**Chat** gaat via de API, met precies de verzoeken van de frontend
(`chatStore.svelte.ts`): `POST /chat/turn` voor een direct gesprek; voor de
hoekknop *Vraag het ook aan de andere duif* eerst het brongesprek teruglezen,
dan `POST /chat/comparisons` (het brongesprek tot en met die beurt gaat mee,
het nieuwe gesprek openen en dezelfde vraag als gewone beurt. Eén ronde
per vraag en taal levert vier takes; de twee vergelijkingen bouwen voort op
precies de twee directe gesprekken van die ronde (`item.basedOn`). Elke take
bewaart de gestreamde frames per netwerkbrok met hun moment, en leest het
gesprek daarna terug om te controleren dat de opslag gelijk is aan de stroom
(`checks.storedEqualsStreamed`). `--redo <take-id>` neemt één item opnieuw op:
een vergelijking vanuit hetzelfde brongesprek, zodat een afgebroken antwoord
vervangen kan worden zonder de rest te veranderen.

**Vergadering** gaat het echte pad in: de echte frontend in headless Chromium,
de schil in de taal van de take (het account krijgt die taal via
`PUT /api/auth/profile`, zoals bij een taalwissel in de schil), de duif aan
tafel met *zelfstandig meedoen* aan, en de uitspraak door een synthetische
microfoon (`--use-file-for-fake-audio-capture`: vier seconden stilte, de zin,
daarna stilte). De spraakherkenning van de server maakt er het transcript van
(staat in de take). Na het einde van de zin krijgt de duif **25 seconden**;
begint zij daarin, dan mag zij uitpraten. Er wordt nooit op *Laat de duif
reageren* gedrukt. Per uitspraak, duif en taal is het een verse vergadering,
dus een reactie hangt niet af van eerdere uitspraken. De stem die de module
afspeelde (`tts_audio`, PCM16 24 kHz) staat als `stem.mp3` naast de take, al in
de vorm van de pagina (mono, 64 kbit/s, bit-exact), met lengte en hash van de
oorspronkelijke PCM in `take.voice`; het gebaar van de 3D-duif (de frontend kiest per reactie zelf tussen
de clips *Peck* en *WingWave*) wordt gemeten via de animatie die de pagina
werkelijk start (`lib/page-runtime.js`), het veldgeluid via het bestand dat de
pagina afspeelde.

De uitspraken (`uitspraken/`, ElevenLabs `eleven_multilingual_v2`, stemmen per
rol zoals in het vergaderfragment van de uitlegvideo: Voorzitter Charlotte,
Beheer Daniel, Adviseur Sarah) zijn onze eigen invoer. `speak.mjs` maakt alleen
opnieuw wat in tekst of stem veranderde; `record-meeting.mjs` weigert een
uitspraak waarvan de tekst niet overeenkomt met `vragen.json`.

**Elke run wordt bewaard.** `record-chat.mjs` en `record-meeting.mjs` schrijven
elke take weg, ook een mislukte (met `failure`), en een take wordt niet
weggegooid: `keuze.mjs` kiest een onbruikbare take eenvoudig niet.

### Proefruns die niet bewaard zijn

Vóór de definitieve opnameronde van 23 september 2026 zijn een paar proefruns
met een eerdere versie van het gereedschap niet bewaard. Dat gebeurde vóór de
volledige run en niet na vergelijking met de uitkomst; er is niet op inhoud
gekozen. Sindsdien blijft elke run staan.

## Kiezen en bouwen

```
node tools/etalage-opnames/keuze.mjs     # keuze.json aanvullen en de stand tonen
node tools/etalage-opnames/build.mjs     # opnames.json, stemmen en veldgeluiden
```

`keuze.json` wijst per item (`chat/<vraag>/<taal>/<route>` of
`meeting/<uitspraak>/<taal>/<duif>`) één take aan, met een reden. `keuze.mjs`
vult ontbrekende items aan met de nieuwste bruikbare take (`"auto": true`) en
laat handmatige keuzes staan: zet `take`, schrijf een eigen `reason` en haal
`auto` weg.

Of een take bruikbaar is, wordt elke keer opnieuw afgeleid uit de ruwe opname
(`lib/evidence.mjs`): de frames van de beurt, de teruglezing, de verzoeken en
het vergaderkanaal. De samenvatting die de opname in `take.checks` schreef is
alleen leesbaar commentaar; kiezen en bouwen vertrouwen haar niet. Niet
bruikbaar is:

- een mislukte take, of een take met een andere tekst dan in `vragen.json`;
- een ander modelbeleid van de server dan de vaste regel. Als model telt het
  huidige, of een eerder model voor een take van vóór diens einddatum
  (`HISTORICAL_MODELS`: 3.5-flash tot 25 september 2026); chat en detectie
  moeten hetzelfde model zijn, en het endpoint moet een HTTPS-adres zijn (welk,
  staat in de take; de bouw eist dat alle gekozen takes hetzelfde noemen);
- chat: een ander model in het `done`-frame van de beurt dan de server meldde, een afgebroken
  antwoord (`stopReason` `truncated` of `filtered`), geen teruglezing of een
  opgeslagen tekst die afwijkt van de gestreamde, een beurt naar een andere
  duif, met een andere vraag of in een andere taal dan het item, of een
  antwoord van een andere duif;
- vergelijking: zonder `POST /chat/comparisons`, of een vergelijking die niet
  bij de beurt hoort;
- vergadering: geopend met een andere duif, in een andere taal of zonder
  zelfstandig meedoen (volgens het antwoord van de server), geen definitief
  transcript van de spraakherkenning, een `error`- of `stopped`-bericht op het
  kanaal, of een gesproken reactie zonder stem (zoals bij `tts_start_failed`).

Een vergelijking moet bovendien voortbouwen op de take die voor haar
brongesprek gekozen is, volgens `item.basedOn` én volgens de opname zelf (de
vergelijking kopieerde uit precies het gesprek dat die brontake aanmaakte);
anders zou de pagina onder de eerste duif een ander antwoord tonen dan de
tweede duif meekreeg.

`build.mjs` weigert zolang een item niet klopt. Het schrijft:

- `site/etalage/data/opnames.json` met `recording.placeholder` uit
  `vragen.json`. `recording` bevat het model van de gekozen takes (één model
  voor alle takes, anders weigert de bouw), niet het endpoint: dat staat in
  de takes en hoort niet op de publieke pagina, net als de bouwgegevens van
  de server (module-tree, core-commit). `knowledgeBase` is de id
  van de kennisbank uit de takes. Naast het contract staat er het aantal takes
  in.
- `site/etalage/media/geluid/stem/<take>.mp3`: de stem van elke gesproken
  reactie, byte voor byte de `stem.mp3` uit de take (git bewaart hem één
  keer). Stemmen die niet meer gekozen zijn worden verwijderd.
- `site/etalage/media/geluid/koer/`: de drie veldgeluiden van de module (CC0,
  gecontroleerd tegen de hash in de mediacatalogus).

Onder `site/` schrijft de opnamestroom alleen json, mp3 en webp: geen
dotfiles, geen symlinks, geen .md. Een test bewaakt dat. De herkomst van alles
wat daar staat — de tekst, de stem van de duiven (Soniox, Logan, spreektempo
per duif), de synthetische vergaderdeelnemers (ElevenLabs), de veldgeluiden
(CC0) en de kennisgraafbeelden — staat in [`HERKOMST.md`](HERKOMST.md), met wat
niet vastgesteld kon worden. `build.mjs` weigert als een veldgeluid daar niet
met zijn hash in staat.

### Wat er in `opnames.json` staat

- **Turn**: de wachtfasen die voorbijkwamen, en de berichten zoals de module
  ze toonde (`presentation.visibleMessages`): bij de Stakeholder kader en
  bemiddeling van de facilitator rond het antwoord; bij de Kennispartner
  alleen het antwoord, omdat de facilitator daar intern blijft. `chunks` zijn
  de netwerkbrokken met hun moment vanaf het eerste woord; samen precies de
  tekst. `kind`: `grounded` (bronnen), `gap` (Kennispartner zonder bron),
  `standpoint` (Stakeholder zonder bron: *Standpunt zonder onderzoek*).
- **`related: true`** (bericht van een duif; onderdeel van het
  contract) betekent *verwante bronnen*: de bronnen kwamen niet uit
  de gewone zoekopdracht op de vraag, maar uit een herformulering door de
  facilitator, uit een eerdere treffer in dezelfde beurt, of uit de brede
  zoekactie op een lagere drempel (0,65) die ná een kennisgat draait (fase
  `pigeon_related_search`). Het zegt iets over de herkomst, niet over de
  waarde: soms dragen die bronnen de vraag alsnog en citeert de duif ze
  gewoon. De module zet het alleen als er bronnen zijn, en dan is de
  kennisbankstand altijd `sources`: `related` komt dus alleen voor bij
  `kind: grounded`, nooit bij `gap` of `standpoint` (een gat is per definitie
  zonder bronnen, `agent_tools.go`, `groundQuestion`). Op het scherm toont de
  module dan in plaats van *3 bronnen* de chip *3 verwante bronnen* (*3 related
  sources*), en na het streamen een neutrale markering *Deze bronnen grenzen
  aan je vraag.* (*These sources border on your question.*), op dezelfde plek
  en in dezelfde stijl als de gat-markering. Zonder `related` staat het veld er
  niet.
- **Source**: titel, auteurs en jaar uit de APA-bronvermelding van de
  kennisbank (de titel in de kennisbank is vaak de bronsleutel, zoals
  `jiguet2019`). Valt er geen titel uit de bronvermelding te lezen en is er
  ook geen papertitel, dan weigert de bouw met de take-id en de bronsleutel:
  een machinesleutel komt nooit op de pagina. Verder de doi als
  `https://doi.org/…` (alleen https), de eerste
  oorspronkelijke passage zoals de bronlade die toont, en de taal van die
  passage (geteld; de kennisbank draagt geen taal per citaat). Heeft een bron
  meer passages (de lade toont ze allemaal), dan staan ze buiten het contract
  ook in `passages` (`[{text, lang}]`). Een bron met afgekeurde gronding
  heeft geen passage; de lade meldt dat met een eigen regel, het
  contract heeft daar geen veld voor. Alle bronnen die
  de module bij het antwoord gaf staan erin, ook als het antwoord er maar een
  paar citeert: de bronlade laat ze ook allemaal zien.
- **Reaction**: `speech` (tekst, bronnen, stem, gebaar), `signal` (veldgeluid,
  gebaar) of `silence`. `delayMs` is het begin van de reactie (`persona` of
  `sound` op het vergaderkanaal) min het einde van de uitspraak in de
  microfoon; de stem begon enkele honderden milliseconden later.
- `gesture` is `peck` of `ruffle` (de namen van de frontend en van de
  beeldstroom; clip *WingWave* heet in de frontend `ruffle`).

## Feiten

`facts.json` is de vastgelegde uitkomst van een alleen-lezende telling in de
kennisbank: de totalen uit de importmarker (die de werkomgeving toont) naast
een eigen telling van knopen en relaties; bij verschil stopt de telling. Stand
23 september 2026: **120 bronnen, 4.067 uitspraken, 4.225 passages** — gelijk
aan de oude publieke getallen. Elke uitspraak heeft minstens één passage.

De 120 bronnen zijn bronknopen, geen publicaties: twee papers staan er elk
twee keer in (twee bestanden van dezelfde publicatie, dezelfde DOI). `works`
telt de verschillende werken — op DOI, en zonder DOI op de genormaliseerde
titel uit de bronvermelding — en komt op **118**; `facts.json` noemt de twee
gedeelde paren (`sharedWorks`). De publieke tekst gebruikt *118 publicaties*;
`sources` blijft in de feiten staan voor de volledigheid.
Daarnaast: 3.815 passages zijn bij de import in het paper teruggevonden
(`grounded`), en 3.668 uitspraken hebben minstens één zo'n passage; de module
gebruikt alleen die passages als bewijs. `facts.json` draagt alle zes getallen
en de vergelijking met de oude publieke getallen.

## Kennisgraaf

```
node tools/etalage-opnames/graph-shots.mjs            # kennisgraaf-licht.webp en -donker.webp (Nederlands)
node tools/etalage-opnames/graph-shots.mjs --lang en  # kennisgraaf-licht-en.webp en -donker-en.webp
```

Alleen de kaart zelf: het canvas met de knopen, hun namen en de verbindingen,
met het domein *Milieu en dierenwelzijn* geselecteerd (zijn veertien thema's in
kleur, de rest gedimd). Boom, leespaneel, knoppenbalk en zoomknoppen blijven
buiten beeld; het leespaneel zou op ±900 pixels breed in de methode-sectie
onleesbaar klein worden. `--select ""` geeft de beginstand. Geen
modelaanroepen; start wel de opnameserver en stop hem na afloop.

- Venster 1600×1000 op dubbele pixeldichtheid, dan *Passend maken*. Het beeld
  wordt bijgesneden tot wat er op het canvas staat, met een rand
  (`lib/image.mjs`): ongeveer 760×590 CSS-pixels, dus op 900 pixels breed
  staan de namen iets groter dan in de module zelf. WebP onder 250 KB (nu
  ongeveer 50 KB).
- Een domein en geen thema: bij een geselecteerd thema tekent de module de
  naam van de band naar het domein midden op die band, met de lijn erdoor. Bij
  een domein met meer dan acht thema's tekent zij geen bandnamen.
- Een klik op de rij van een domein klapt het ook in; het script klikt dan nog
  eens, zodat de thema's op de kaart blijven.
- De module plaatst de namen zelf: een naam die naast zijn knoop geen plek
  heeft, zakt omlaag of blijft weg. Dat is haar gedrag, niet dat van het
  script.
- Maat, hash, tijdstip, module-commit en de commit van het
  gereedschap staan per beeld in `kennisgraaf.json`.

## Opnieuw opnemen met andere vragen

De vragen en uitspraken in `vragen.json` zijn definitief, met de takes zoals
ze zijn (`placeholder: false`). Wil iemand later andere vragen, dan:

1. Vervang de teksten in `vragen.json` (`placeholder` blijft `false`).
2. `node tools/etalage-opnames/server.mjs start` (en eventueel eerst
   `server.mjs db --replace` voor een verse kopie van de kennisbank).
3. `node tools/etalage-opnames/speak.mjs`
4. `node tools/etalage-opnames/record-chat.mjs --takes 3` en
   `node tools/etalage-opnames/record-meeting.mjs --takes 3`
5. `node tools/etalage-opnames/keuze.mjs`, dan de keuzes nalezen en waar nodig
   vastzetten met een eigen reden.
6. `node tools/etalage-opnames/build.mjs` en `node --test 'tools/etalage-opnames/test/*.test.mjs'`
7. `node tools/etalage-opnames/server.mjs stop`

Takes van de oude teksten blijven staan maar zijn niet meer bruikbaar (andere
tekst); `keuze.mjs` kiest ze niet meer. Een nieuwe ronde gaat met het huidige
model; omdat de bouw één model voor alle gekozen takes eist, neem je
dan alle items opnieuw op, niet een deel.

**Een nieuwe sprekersrol.** Krijgt een vergaderuitspraak een spreker die er nu
niet is (niet Chair, Operations of Advisor), voeg dan in `lib/config.mjs` bij
`SPEAKER_VOICES` een ElevenLabs-stem toe onder de Engelse sprekersnaam
(`speaker.en` in `vragen.json`), met naam en `voiceId` van een premade stem.
`speak.mjs` stopt anders met *geen stem voor spreker …*.

**De repository groeit.** Elke run blijft staan, ook als hij later niet meer
bruikbaar is. Eén volledige run met één take per item is ongeveer 5 MB aan
takes (de stemmen als mp3, de rest JSON); `--takes 3` ongeveer drie keer zoveel.
De takes van september blijven daarnaast in git staan.

**Als de nieuwste ronde een mislukte vergelijking heeft.** `keuze.mjs` kiest
voor een direct gesprek de nieuwste bruikbare take, en voor een vergelijking
alleen een take die op precies dát gesprek voortbouwt. Brak in de nieuwste
ronde een vergelijking af (bijvoorbeeld `stopReason truncated`), dan heeft dat
item geen keuze en weigert `build.mjs`. Twee uitwegen:

- Neem alleen die vergelijking opnieuw op, vanuit hetzelfde brongesprek:
  `node tools/etalage-opnames/record-chat.mjs --redo <take-id van de afgebroken vergelijking>`.
- Of zet het brongesprek vast op een oudere ronde waarvan de vergelijking wél
  lukte. Bijvoorbeeld in `keuze.json`:

  ```json
  "chat/afschot/nl/kennispartner": {
    "take": "chat-afschot-nl-kennispartner-20261001T091500Z",
    "reason": "vastgezet: de ronde van 09:15, omdat de vergelijking naar de Stakeholder in de nieuwste ronde afbrak"
  }
  ```

  (zonder `"auto": true`). `keuze.mjs` kiest daarna voor
  `chat/afschot/nl/kennispartner>stakeholder` vanzelf de vergelijking die op
  die take voortbouwt.

## Tests

```
node --test 'tools/etalage-opnames/test/*.test.mjs'
```

Het lezen van de stroom, de mapping naar Turn, Source en Reaction, kiezen en
bouwen met synthetische takes; en over wat er werkelijk in git staat: geen
cookies, autorisatie of geheime waarde in een take, model en commit in elke
take, de stem aanwezig en even lang als de PCM van de module, `keuze.json` volledig en consistent,
en `opnames.json` precies wat `build.mjs` uit de keuze maakt, met alle media.

## Bekende beperkingen

- De vergadertake heeft geen provenance per beurt (het vergaderkanaal draagt
  die niet); het model komt daar uit het modelbeleid van de server.
- `delayMs` rekent vanaf het moment dat de microfoon openging plus de aanloop;
  op ±0,1 s nauwkeurig.
- Het gebaar is een keuze van de frontend (willekeurig tussen twee clips per
  reactie), geen keuze van de server; de take legt vast welke het werd.
- Opnemen hangt af van de testid's en knoppen van de module-frontend
  (`#duif-tab-meeting`, `seat-button`, `autonomy-field`, `call-control-end`);
  verandert die, dan faalt de vergaderopname met een duidelijke melding.
