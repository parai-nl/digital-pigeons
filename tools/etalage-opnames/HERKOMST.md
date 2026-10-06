# Herkomst van wat de opnamestroom op de etalage zet

Dit bestand hoort bij `site/etalage/data/opnames.json`,
`site/etalage/media/geluid/` en `site/etalage/media/beeld/kennisgraaf-*`. Onder
`site/` staan alleen json, mp3 en webp; de herkomst staat daarom hier. Wat
hieronder niet vastgesteld kon worden, staat er als zodanig bij.

In deze publieke versie is het adres van de modelproxy vervangen door een neutrale waarde.

## De tekst van de duiven en de facilitator

Elke zin van een duif of van de facilitator in `opnames.json` is letterlijk
modeluitvoer van `vertex_ai/gemini-3.5-flash` via de taalmodelproxy van het project,
opgenomen tegen de echte module op 23 september 2026 tussen 14:46 en
15:14 UTC. Dat was het model van de module op main van dat moment: de
wissel naar `vertex/gemini-3.6-flash-eu` (op 24 september) zat daar nog niet in. Nieuwe opnames
gaan alleen met 3.6-eu (`lib/config.mjs`); deze takes blijven de eerlijke
uitvoer van het model waarmee ze gemaakt zijn en worden daarom niet overgedaan
of omgelabeld. Het
model staat per take in het modelbeleid dat de server meldde, en
per chatantwoord in de provenance van het `done`-frame. Niets is ingekort,
aangevuld of gecorrigeerd, ook een tikfout van het model niet. De takes staan
in `takes/`, de keuze met reden in `keuze.json`.

De bronnen bij een antwoord komen uit de kennisbank *Duiven* (bundel
`duiven/3`, geïmporteerd 15 september 2026, inhoudshash `774f03e5…`): de
bronvermelding zoals de kennisbank haar draagt (APA), een link naar de DOI, en
de korte oorspronkelijke passage uit het paper zoals de bronlade van de module
haar toont, met bronvermelding. Titel, auteurs en jaar zijn uit die
bronvermelding gelezen (`lib/map.mjs`).

## De stem van de pratende duiven

- **Dienst en stem.** Soniox-spraaksynthese, stem **Logan**
  (`SONIOX_TTS_VOICE`), dezelfde stem voor de Kennispartner en de Stakeholder
  (een ontwerpkeuze; zie `module/README.md`). Vastgesteld uit het
  opstartlog van de opnameserver op 23 september (*audioproviders gekozen: stt
  soniox, tts soniox, stem Logan*); de audioberichten van de vergadering noemen
  de stem zelf niet.
- **Spreektempo per duif** (`module/duif/config/personas.yaml`):
  Kennispartner 0,97, Stakeholder 1,10. De Signalerende duif spreekt niet.
- **Wat er op de site staat.** De twaalf gesproken reacties in
  `site/etalage/media/geluid/stem/<take>.mp3`: de PCM die de module in de
  vergadering afspeelde (16 bit, mono, 24 kHz, `tts_audio`), omgezet naar mp3
  mono 64 kbit/s. Lengte en SHA-256 van de oorspronkelijke PCM staan per take in
  `take.voice`. Verder is er niets aan veranderd.
- **Voorwaarden.** Met toestemming opgenomen; geen open licentie, zie
  `NOTICE.md` §4.

## De vergaderdeelnemers (ElevenLabs)

- **Synthetische deelnemers.** De vier vergaderuitspraken zijn onze eigen,
  verzonnen tekst (`vragen.json`, definitief), uitgesproken door
  ElevenLabs-spraaksynthese. Er spreekt geen echt persoon: Voorzitter / Chair is
  de premade stem **Charlotte** (`XB0fDUnXU5powFXDhCwa`), Beheer / Operations
  **Daniel** (`onwK4e9ZLuTAKqWW03F9`), Adviseur / Advisor **Sarah**
  (`EXAVITQu4vr4xnSDxMaL`). Model `eleven_multilingual_v2`, uitvoer
  `mp3_44100_128`, gemaakt op 23 september 2026 om 14:47 UTC. Het zijn
  dezelfde stemmen als in het vergaderfragment van de uitlegvideo.
- **Waar ze staan.** Alleen in `tools/etalage-opnames/uitspraken/` (in git, dus
  in de publieke repository), met tekst, stem, model, duur en hash in
  `uitspraken.json`. De etalage gebruikt ze niet: ze zijn alleen de invoer van
  de synthetische microfoon. Wat de module ervan verstond staat per take in het
  transcript.
- **Voorwaarden.** Met toestemming opgenomen; geen open licentie, zie
  `NOTICE.md` §4.

## Veldgeluiden van de Signalerende duif (CC0)

`site/etalage/media/geluid/koer/` bevat de drie veldopnames van de module,
byte voor byte (`module/duif/frontend/public/media/`). `build.mjs` controleert
de hash tegen `module/duif/config/media-manifest.v1.json` (`native.sounds`),
waar ook de volledige beoordeling staat, en weigert als een bestand
hieronder niet met die hash genoemd wordt. CC0 1.0: vrij te gebruiken, geen
naamsvermelding nodig. De etalage speelt het bestand af dat de module in de
opgenomen vergadering koos; bij welke band een opname hoort, wordt nergens
getoond of benoemd.

| Bestand | Bron | Maker | Licentie | Duur | SHA-256 | Bewerking |
|---|---|---|---|---|---|---|
| `pigeon-field-coo.mp3` | [Pigeons & Doves - Homing Pigeon Cooing](https://freesound.org/people/TheKingOfGeeks360/sounds/808001/) | TheKingOfGeeks360 | CC0 1.0 | 4529 ms | `5e9c89c10b1061d9e164dfabafa94f1cdcd3878cd1e4c562c4a933a9f46a2a6e` | Fragment uit de openbare HQ-preview, 20 ms fade-in en 78 ms fade-out, +2,31 dB naar −24,3 LUFS (true peak −6,6 dBTP); MP3 192 kbit/s, stereo 48 kHz. |
| `pigeon-field-coos-repeated.mp3` | [R01-28 Pigeon Coos](https://freesound.org/people/craigsmith/sounds/479589/) | craigsmith | CC0 1.0 | 3950 ms | `97ff30b6767a4837209c57628f85c434c5e3be1a34dd41c43bb2e0c4c67a14d5` | Fragment 3,90–7,85 s uit de openbare HQ-preview, 30 ms fade-in en 120 ms fade-out, +7,67 dB naar −19,8 LUFS (true peak −11,4 dBTP); mono bron, MP3 192 kbit/s, stereo 48 kHz. |
| `pigeon-field-loft-chorus.mp3` | [Tauben im Taubenschlag (pigeons)](https://freesound.org/people/Beskinsky/sounds/778143/) | Beskinsky | CC0 1.0 | 3600 ms | `8ada2bdf415c24b527eff7b8f8400defbc3a98da5998c24d05334b1e8986316c` | Fragment 95,20–98,80 s uit de openbare HQ-preview, 30 ms fade-in en 150 ms fade-out, +7,83 dB met true-peak-limiter op −1 dBTP naar −15,4 LUFS (true peak −1,2 dBTP); MP3 192 kbit/s, stereo 48 kHz. |

Alle drie zijn andere duiven dan die van de fototextuur van het 3D-model; het
geluid is niet oorspronkelijk met het model gesynchroniseerd.

## Kennisgraafbeelden

`site/etalage/media/beeld/kennisgraaf-licht.webp`, `-donker.webp` en de
Engelse `-licht-en.webp`, `-donker-en.webp` zijn beelden van de kaart van de
module zelf (`graph-shots.mjs`), gemaakt op 24 september 2026 om 08:29 UTC op
de opnameserver (daarna gestopt), zonder modelaanroepen:

- dezelfde module als bij de opnames, frontend-bundle `d64a902b…`;
- de eigen databasekopie van de opnames, kennisbank *Duiven* (geïmporteerd
  15 september 2026);
- het domein *Milieu en dierenwelzijn* / *Environment and animal welfare*
  geselecteerd: de veertien thema's in kleur, de rest van de kaart gedimd;
- alleen het canvas van de kaart na *Passend maken*, bijgesneden tot de
  knopen, namen en verbindingen met een rand van 24 CSS-pixels; zonder boom,
  leespaneel, knoppenbalk en zoomknoppen (de zoomknoppen en de hint zijn
  alleen tijdens de opname verborgen). Op dubbele pixeldichtheid:
  1525×1186 (NL) en 1517×1185/1186 (EN) pixels, 50–52 KB.

Per beeld staan maat, hash, tijdstip, module, database en de commit van het
gereedschap in `kennisgraaf.json`. In beeld staan alleen de namen
uit de kennisbank en het domeinlabel van de module; geen schil, geen account.
