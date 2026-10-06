# Duiven-werkruimte (module)

Go-module en Svelte 5-interface van de Digitale Duif van Rotterdam (AI-MAPS ×
Parai). De module draait als module binnen het Parai-platform: zij levert een
eigen productcontract, routes, bronprojectie, vergaderregie en vormgeving, en
gebruikt de Parai-shell, -authenticatie en -diensten als dependencies. Zie
[`../ARCHITECTURE.md`](../ARCHITECTURE.md) voor het overzicht.

De kennispartner en de stakeholder hebben elk eigen chats en kunnen ieder
afzonderlijk aan een vergadering deelnemen. Beide gebruiken bronnen voor
feitelijke claims. De positie van de stakeholder is ontworpen; brongebruik
maakt die positie niet wetenschappelijk bewezen. De signalerende duif doet
alleen mee aan vergaderingen en krijgt geen menselijke betekenislabels. Engels
is de beginstand; de volledige interface bestaat ook in het Nederlands.

## Dependencies en grenzen

**Deze module bouwt niet zonder de privé Parai-componenten.** Ze importeert:

| Dependency | Soort | Hoe |
|---|---|---|
| `github.com/parai-nl/parai-core` | Go-framework (server, auth, data, taalmodel, audio) | gepind in `go.mod` |
| `github.com/parai-nl/parai-interaction` | Go-hulpfuncties voor de kennisbankimport | gepind in `go.mod` |
| `@parai/ui` | Svelte-componenten en designtokens | Vite-alias via `PARAI_UI_DIR` |
| `@parai/graph` | kennisgraafmodule | Vite-alias via `PARAI_GRAPH_DIR` |
| Parai-shell | de gebouwde frontend van parai-core | alleen voor een volledige lokale omgeving (`make shell-static`) |

Zonder toegang tot die repositories kun je de code lezen, maar niet bouwen of
testen. De publieke etalage in [`../site/`](../site/) heeft ze niet nodig.

Eigen projectbroncode valt onder MIT (zie [`../LICENSE`](../LICENSE) en
[`../NOTICE.md`](../NOTICE.md)). Importeer de privé-dependencies; kopieer
geen broncode, prompts of substantiële tekst ervan naar deze repository. Een
gebouwde binary of bundel bevat gecompileerde privé-onderdelen en is geen
uitsluitend MIT-artefact.

De frontend leest `@parai/ui` uit `PARAI_UI_DIR` en `@parai/graph` (de
bronmap van de graafcomponent) uit `PARAI_GRAPH_DIR`; beide
staan standaard op een sibling-checkout naast deze repository. Een lokale,
genegeerde `go.work` (zie [`go.work.example`](go.work.example)) mag de
Go-dependencies voor ontwikkeling vervangen; `make build-pin` controleert
afzonderlijk de echte pin. `go.work`, `.env`, kennisbundels, `bin/`,
`static/` en frontend-`dist/` blijven buiten Git.

## Bouwen en controleren

Vanuit deze map, met Go, Bun, Docker en de privé-dependencies beschikbaar:

```sh
make build-pin
PARAI_UI_DIR=/pad/naar/parai-ui PARAI_GRAPH_DIR=/pad/naar/parai-graph make verify
PARAI_UI_DIR=/pad/naar/parai-ui PARAI_GRAPH_DIR=/pad/naar/parai-graph make e2e
PARAI_UI_DIR=/pad/naar/parai-ui PARAI_GRAPH_DIR=/pad/naar/parai-graph make release
```

`make help` toont alle doelen. Voer ze na elkaar uit: verschillende doelen
raken dezelfde `node_modules` en `dist/`.

- `verify` omvat de Go-tests (met gofmt- en pincontrole), de frontendtests en
  de Svelte/TypeScript-controle.
- `e2e` bouwt eerst een verse frontend, start een wegwerp-Postgres (Docker,
  alleen op 127.0.0.1), boot de echte binary tegen een stub-taalmodel en ruimt
  de container op. Een overgeslagen test is geen bewijs.
- `release` levert `bin/duif` zonder `dev`- of `skipauth`-tag, met de gebouwde
  frontend, personaconfiguratie en migraties embedded.

De make-doelen zetten `GOWORK=off` en bouwen dus tegen de pins in `go.mod`.
Draai de tests daarom via `make test` of `make verify` en niet met een kaal
`go test ./duif/`: dat gebruikt de checkouts uit je `go.work`, en loopt een
daarvan niet gelijk met de pin, dan compileren tests die nieuwere core-API
gebruiken niet. Dat zegt dan iets over je lokale checkout, niet over de module.

De pin van parai-core is de pseudo-versie van één commit. Hoog je hem op,
draai dan `make verify` en `make e2e` opnieuw. Het bundelbudget in
`duif/frontend_bundle_test.go` ligt bewust zo'n 5 % boven de gemeten bundel:
het bewaakt onverwachte sprongen, niet de groei van `@parai/ui`. Zie het commentaar bij `totalBudget`.

De module laadt graaf, hulp, vergadering en vergelijking in aparte chunks. De
buildcontrole volgt de volledige eerste importketen en controleert ook alle
overige chunks op grootte, bereikbaarheid en privacyregels.

## Lokaal draaien

```sh
cp .env.example .env
# Vul de eigen database- en dienstconfiguratie in; toon geen geheimen in logs.
make dev
```

`make dev` bouwt de frontend en start de module met de tag `skipauth`: elk
verzoek is een lokale ontwikkelgebruiker. Gebruik dat nooit als
productiebinary. `DUIF_BIND_HOST=127.0.0.1` begrenst de lokale binding. Vite
kan los draaien vanuit `duif/frontend`; `DUIF_API_PROXY` kiest dan de backend.

Voor de volledige gebruikersomgeving is ook een gebouwde Parai-shell nodig:

```sh
PARAI_CORE_DIR=/pad/naar/parai-core make shell-static
```

Dit kopieert alleen gebouwde bestanden naar het genegeerde `static/`. Zonder
die shell serveert de module een placeholder.

### Taalmodel

Alle taalmodelaanroepen (chat, facilitator, vergaderdetectie en de
evaluaties) gebruiken verplicht het model `vertex/gemini-3.6-flash-eu`; een
afwijkende `DUIF_CHAT_MODEL` of `DUIF_DETECT_MODEL` laat de boot falen. Het
adres van de OpenAI-compatibele proxy komt uit `PARAI_LLM_BASE_URL` en moet
HTTPS zijn, zonder gebruikersnaam, query of fragment; er is geen
standaardadres in de code. Bestaande opnames in `tools/` vermelden dat ze
via de taalmodelproxy van het project zijn gemaakt. Zonder expliciete `PARAI_LLM_API_KEY` start de
module niet. Alleen de offline tests gebruiken een loopback-HTTP-adres met een
vaste, niet-geheime sentinelsleutel. Dezelfde regel geldt voor de
opnametools in `tools/`: zij volgen het HTTPS-adres uit `PARAI_LLM_BASE_URL` en weigeren een server die
een ander endpoint of model meldt.

## API en gedrag

Alle moduleroutes beginnen met `/api/duif/v1`. De Parai-authenticatie,
module-intrekking en gebruikersafbakening gelden voor echte toegang; een
geslaagde lokale `skipauth`-proef bewijst die keten niet. Fouten gebruiken
stabiele codes en EN/NL-meldingen; providerinhoud hoort niet op de wire of in
logs.

| Route | Contract |
|---|---|
| `GET /health`, `GET /duiven` | Module-/personaconfiguratie zonder prompts; interne IDs blijven stabiel |
| `GET/POST /access/notice` | Actuele productuitleg en accountgebonden versie/tijd van kennisneming; geen juridische toestemmingsregistratie |
| `POST /chat/turn` | Bewuste mensvraag, sessie/duif/taal; SSE met behoud van auteurschap en terminale status |
| `GET /chat/sessions` | Alleen eigen historie, per gesprek oorspronkelijke taal en beschikbare herkomst |
| `GET /chat/sessions/{id}/messages` | Oorspronkelijke opgeslagen berichten, bronsets en auteurs |
| `DELETE /chat/sessions/{id}` | Alleen het eigen gesprek en bijbehorende berichten verwijderen; expliciete kopieën in andere gesprekken blijven behouden |
| `POST /chat/comparisons` | Expliciete vraag, vaste context (het brongesprek tot en met de antwoordbeurt op die vraag; facilitator blijft achter) en idempotentie; nieuw doelgesprek, geen samenvoeging van auteurs |
| `POST /meeting/sessions` | Nieuwe tijdelijke vergadering met één duif; standaard autonomie uit |
| `GET /meeting/sessions/{id}/events` | Actuele sessiestand en SSE; herverbinden is geen audioreplay |
| `POST /meeting/sessions/{id}/audio` | PCM16 mono 16kHz via de streamingbrug; begrensde blokken, geen permanente opname |
| `POST /meeting/sessions/{id}/transcript` | Synthetische tekst-/testinvoer op dezelfde vensterseam; geen automatische UI-export |
| `POST /meeting/sessions/{id}/respond` | Handmatige uitnodiging aan een sprekende duif |
| `POST /meeting/sessions/{id}/intervene` | Handmatig één signaling-gebeurtenis uitnodigen |
| `POST /meeting/sessions/{id}/stop` | Bijdrage afbreken en automatische deelname pauzeren |
| `POST /meeting/sessions/{id}/resume` | Expliciet hervatten met de waargenomen `turnId`; oude hervatting kan geen nieuwere Stop opheffen |
| `POST /meeting/sessions/{id}/playback` | Echte afspeelstatus bij die beurt; generatie-einde is geen afspeeleinde |
| `POST /meeting/sessions/{id}/settings`, `…/presence` | Begrensde sessieregie; instellingen of aanwezigheid hervatten niet stil |
| `DELETE /meeting/sessions/{id}` | Sluiten en tijdelijke context opruimen |
| `GET /sounds/{id}` | Geconfigureerd bestand; tijdelijke samples worden niet als echte veldopname voorgesteld |
| `GET /knowledge/status` | Importstand/versie, het bundelformaat (`bundleSchema`, en bij een vertaalde bundel `languages` en `sourceLanguage`) en een veilige reden bij storing |
| `GET /graph/overview`, `GET /graph/nodes/{id}`, `GET …/children?offset=` | Geautoriseerde leesroutes, expliciete paginering en metadata-allowlist |

De volledige route- en DTO-definities staan in `duif/handlers.go`,
`duif/comparison.go`, `duif/meeting_handlers.go`, `duif/meeting.go` en
`duif/frontend/src/lib/meeting/protocol.ts`. Gebruik de code en de
bijbehorende tests voor actuele velden.

### Frames van één chatbeurt

`POST /chat/turn` levert SSE-frames met `type` als discriminator. De duif
schrijft haar antwoord in één stroom: de eerste delta vertrekt zodra het model
hem produceert, en er staat geen redactie- of keuringsstap tussen haar en de
lezer.

| Frame | Betekenis |
|---|---|
| `tool`, `tool_result` | De facilitator vroeg iets aan; de chip opent en sluit. Argumenten en samenvattingen blijven server-side |
| `artifact` (open/`done:true`) | Een blok van een andere stem dan de facilitator, met eigen `id`, persona en kennisbankstand; het sluitframe draagt een `stopReason` als het blok anders dan normaal eindigde |
| `sources` | De bronnen van dít blok, vóór de eerste tekst; op de pipeline vóór de eerste `text` van de beurt |
| `artifact_delta`, `text` | Tekst van de duif respectievelijk de facilitator, zoals het model hem produceert |
| `text` (vóór het eerste artifact) | Het kader: één zin waarmee de facilitator de vraag aan de mensen voorlegt vóór hij de duif het woord geeft |
| `error` | Stabiele reden-code met EN/NL-melding; nooit providerinhoud |
| `done` | Einde van de beurt: sessie, kennisbankstand, `stopReason` en herkomst. Er volgt niets meer; de stroom sluit erachter |

`stopReason` zegt waaróm een uiting ophield: `natural`, `max_iter`,
`tool_error`, `refusal`, `truncated`, `filtered` of `error`. Op de `done` gaat
hij over de beurt (de facilitator); op het sluitende `artifact` over dat ene
duif-blok. `truncated` betekent dat de provider de stream op het tokenplafond
sloot (`finish_reason: length`); `filtered` dat zijn inhoudsfilter hem sloot
(`finish_reason: content_filter`). Beide zijn geen storing: de tekst blijft
precies wat het model schreef en gaat naar de opslag, maar de client zegt het
erbij. De pipeline laat `stopReason` weg bij een normaal einde (`omitempty`).
De opgeslagen boodschap draagt dezelfde reden, zodat de melding een
herlaadbeurt overleeft. Het plafond (`duif/answer_stream.go`) telt de
redeneertokens van het model mee en staat daarom veel hoger dan de zichtbare
lengte van een antwoord.

De volgorde van één bemiddelde beurt is:

```
tool → [text: kader] → artifact (open) → sources? → artifact_delta… →
artifact (done) → tool_result → [text: bemiddeling] → done
```

De twee `text`-momenten zijn de facilitator. Het kader is één zin en reist mee
als het `framing`-argument van `ask_pigeon`: het kost geen extra modelronde.
De bemiddeling is één afsluitende iteratie zonder tools (`tool_choice: none`),
waarin de facilitator het standpunt van de duif aan de vraag en aan het
perspectief van de mensen koppelt: twee tot drie zinnen, en daarna hoogstens
één korte vraag aan de mens. Hij spreekt nooit namens de duif en herhaalt of
verzacht haar antwoord niet. Valt de bemiddeling om op budget of annulering,
dan eindigt de beurt gewoon: het antwoord van de duif staat er al.

Wie de facilitator hardop hoort, staat per duif in `duif/config/personas.yaml`:

```yaml
facilitator: { framing: true, mediation: true }
```

Standaard staat dat blok aan bij de stakeholder en uit bij de kennispartner;
daar blijft de facilitator intern, en alleen een noodzakelijke neutrale
`clarify` of `redirect_to_pigeon` bereikt de lezer. Een blok op een duif zonder
`engine: agent` is een bootfout.

De markeringen `eis:F1`, `eis:P2` enzovoort in de prompts en het commentaar
zijn testsentinels (F = facilitator, P = identiteit, G = gronding, R = grenzen,
M = vergadering); de regel zelf is de zin onder de markering in
`duif/config/prompts/`.

Het artifact sluit meteen na de laatste delta, en `done` volgt na de
bemiddeling. Een lezer die intussen wegloopt kost het antwoord zijn rij niet:
de opslag draait op een van het verzoek losgekoppelde context. De vergadering
eindigt op dezelfde manier bij `response_done`; daar wordt niets bewaard, want
vergaderinhoud blijft tijdelijk.

### Gesprekken en vergelijken

Chats bewaren mensberichten, eigen assistentartefacten en de kopie die een
vergelijking meebracht. Een bemiddelde beurt levert drie rijen in
leesvolgorde (facilitator, duif, facilitator), geordend op `seq`, zodat
herladen exact reproduceert wat op het scherm stond. Een gewone duifwissel
opent het eigen gesprek van die duif.

De vergelijkactie is één klik: de vraag gaat direct naar het gesprek van de
andere sprekende duif. Wat meegaat ligt vast: het brongesprek tot en met de
antwoordbeurt op die vraag. De facilitatorregels blijven achter, en
vergaderingstekst reist nooit mee. Een gekopieerd duifantwoord behoudt zijn
persona, bronnen en versie; de tweede duif krijgt het in haar prompt als
uitspraak van die andere duif, nooit als eigen eerdere beurt. Historische
citatienummers worden niet aan nieuwe bronnen gekoppeld. Onzekere retries
gebruiken dezelfde gebruikersgebonden idempotentiesleutel.

**Nieuw gesprek** leegt alleen de duif die op dat moment gekozen is; per duif
houdt de client een eigen gesprek en concept bij. Een volgende groep op
hetzelfde apparaat begint daarom met uitloggen. De opgeslagen
accountgeschiedenis verdwijnt daar niet mee; daarvoor is de afzonderlijke
verwijderactie.

## Bronnen en kennisbank

De import leest een buiten de repo beheerde `.json`-bundel met schema
`duiven/2` of `duiven/3`, of een oudere ZIP-bundel. `DUIF_IMPORT_KB` wijst
bij boot naar dat bestand (`make import-kb KB=…`). Een geslaagde importmarker
bevat hash, mapper-/embeddingversie, dimensie en tellingen; dezelfde versie
wordt niet opnieuw geschreven. Een model- of dimensiemismatch faalt; er wordt
niet stil gemengd. `DUIF_IMPORT_KB_FORCE=1` vervangt de collectie en is een
bewuste beheeractie, eerst te proberen op een eigen database.

### Het bundelformaat `duiven/3`

`duiven/3` is dezelfde graaf als `duiven/2`, met de mensleesbare velden in
twee talen. `duiven/2` blijft geldig en importeert ongewijzigd. Alle regels
zijn fail-closed: een bundel die op één punt niet klopt, wordt geweigerd.

**Topniveau.**

```json
{
  "bundle_schema": "duiven/3",
  "languages": ["nl", "en"],
  "source_language": "nl",
  "nodes": { … }, "relationships": { … }, "statistics": { … }
}
```

Bij `duiven/2` moeten `languages` en `source_language` afwezig zijn; een
`duiven/2`-bundel die ze wél draagt is meestal een half vertaalde bundel met
het oude etiket en wordt geweigerd. `nodes`, `relationships` en `statistics`
zijn gelijk aan de `duiven/2`-bundel waar de vertaling uit voortkomt.

**Wat een taal krijgt.** Een vertaald tekstveld is een object met precies `nl`
en `en`, allebei niet leeg; een vertaalde lijst is `{"nl": [...], "en": [...]}`
met twee even lange lijsten. Een kale string op zo'n veld is in `duiven/3`
ongeldig, en een ontbrekende vertaling laat de import vallen. Optionele velden
blijven optioneel.

Welke velden per label vertaald zijn, staat in `duif/kb_bundle.go`
(`kbNodeProps`) en `duif/kb_v2.go`. Citaten, bronvelden, identiteiten,
opsommingssleutels, tellingen, byte-offsets, hashes en relaties krijgen géén
taal en blijven byte-identiek; de citaten dragen bytegrenzen tegen de
passagecontrole.

**Hoe het in de graaf landt.** Een vertaald veld wordt platgeslagen: de
brontaal houdt de bestaande kolom, het Engels krijgt een zijspoorprop ernaast
(`titel` plus `titel_en`, `samenvatting` plus `samenvatting_en`, enzovoort).
Bestaande lezers van die kolommen (getypeerde reads, inspectiequery's op de
opslag, graaflabel, themabepaling van de bronkaart) zien zo niets veranderen. Elke
geïmporteerde knoop draagt daarnaast `bundle_schema` en `source_language`.

**Embedden gaat op het Engels.** Eén vector per standpunt over de Engelse
samenvatting plus het eerste citaat. Vectorbehoud vraagt bij `duiven/3` om een
bytegelijke Engelse samenvatting én een vorige import die zelf al `duiven/3`
was; een corpus dat van `duiven/2` overstapt, embedt volledig opnieuw. De
marker legt naast hash en mapperversie ook schema, talen en brontaal vast.

**Op het scherm.** Graaf en bronkaart sturen een vertaald veld als taalobject
en een onvertaald veld als kale string, met de brontaal van de knoop erbij. De
client kiest zijn UI-taal en valt terug op de andere taal, nooit op een lege
regel. Staat er een vertaling in plaats van het origineel, dan zegt het
leespaneel dat met één regel; het citaat eronder is niet vertaald.

### Updates, gronding en retrieval

Een gewone update behoudt bestaande zoekvectoren als de marker dezelfde
embeddinginstellingen heeft en de samenvatting/citaatbasis gelijk blijft.
Gewijzigde onderzoeksinhoud krijgt nieuwe embeddings. Een `duiven`-bundel is
een volledige snapshot: vervallen eigen corpusknopen en relaties worden
opgeruimd. Maak vóór een update een databaseback-up; de import is geen
atomaire databasewissel, en na een gedeeltelijke schrijffout ontbreekt de
complete marker.

Statussen zijn `empty`, `importing`, `ready`, `incomplete` en `failed`. Een
ontbrekende embedder of onbereikbare database geeft een storing en geen
inhoudelijk kennisgat. Graafmetadata is per tabel toegelaten; embeddings,
private opslagkeys en hashes gaan niet als inhoud mee. Domeinen, thema's en
perspectieven zijn een ordening van materiaal, niet het brein of een bewezen
overtuiging van een echte duif.

Retrieval en nummering zijn gedeeld voor chat en vergadering. Afgewezen of
ongeldige citaatgronding wordt uitgesloten; ontbrekende legacygronding blijft
`unverified`. Waar oorspronkelijke passages beschikbaar zijn, krijgt het model
die als feitelijke basis. Iedere feitclaim gebruikt zijn eigen actuele
bronnummer; eerdere antwoorden zijn historie, geen nieuw bewijs.
`Source.grounding` en `AnswerProvenance` benoemen het bewijs en de versies
zonder prompts te publiceren.

De duif ziet haar eigen gespreksgeschiedenis en krijgt haar personaprompt bij
elke beurt. Een antwoord van een andere duif in hetzelfde gesprek (de kopie uit
een vergelijking) gaat mee als benoemde uitspraak van die duif. In de
vergadering is dat het transcriptvenster.

Een semantische bronaudit per beurt bestaat niet: de bronnen zijn bij de
import geverifieerd. De kolom `audit` in
`GET /chat/sessions/{id}/messages` blijft bestaan voor oudere rijen; nieuwe
rijen dragen `null`.

## Vergadering en audio

De vergadering bewaart inhoud alleen in tijdelijk sessiegeheugen. Stop legt
lokale uitvoer direct stil; microfoon dempen en sessie beëindigen zijn aparte
regiehandelingen. Een andere vergaderduif begint met nieuwe tijdelijke context.

De detector gebruikt gecommitteerde, actuele tekst en wacht op menselijke rust.
Partials helpen bepalen of iemand nog spreekt, maar worden geen classificatie-
of broninvoer. Venster, frequentie, retries en cooldowns zijn begrensd; er is
geen hitwachtrij. De signalerende duif stuurt geen arousal-, thema- of
aanleidingduiding naar het deelnemerspodium.

PCM kan tijdens generatie spelen; MP3 wordt als complete beurt gedecodeerd.
`tts_done` beschrijft audiolevering, niet het hoorbare einde: de echte
afspeelbevestiging en de lokale fout-/Stoplogica bepalen wanneer de duif nog
spreekt.

`DUIF_STT_PROVIDER` en `DUIF_TTS_PROVIDER` kiezen per richting tussen
ElevenLabs en Soniox; een onbekende waarde is een bootfout en geen stille
terugval. Beide pratende duiven spreken met één stem, uit `SONIOX_TTS_VOICE`;
er is geen stem per duif en geen stemmenmenu. Alleen het spreektempo verschilt
per duif (`personas.yaml`). Op de Soniox-route is `voice_id` in
`personas.yaml` een aan/uit-schakelaar. Het opstartlog noemt de gekozen
providers en stem. Alle variabelen staan met uitleg in
[`.env.example`](.env.example).

Een gekozen host of stemnaam bewijst geen verwerkingsafspraak: regio,
retentie, accountlimieten en de verwerkersovereenkomst worden per installatie
apart vastgesteld. De signalerende duif gebruikt geen TTS.
