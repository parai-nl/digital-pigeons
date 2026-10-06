# Beeldstroom van de etalage

Maakt de duiven voor de carrouselkaarten en de mini-vergadering van de publieke
etalage: per duif een
achtergrondplaat, een duivenlaag met alfakanaal die boven de kaart uitsteekt
(rustbeeld en twee reactieclips) en een kop voor de chatbubbels. Alles komt in
`site/etalage/media/duiven/`, met `manifest.json` als contract voor de site.

```
python3 tools/etalage-beeld/build.py                # model uit config.json ("model": "v4")
python3 tools/etalage-beeld/build.py --model v3     # het oude v3-model (tot 24 september 2026)
python3 tools/etalage-beeld/build.py --draft        # proef: weinig samples, elk 6e beeld, alleen in out/
python3 tools/etalage-beeld/build.py --draft --rest-only --only stakeholder   # alleen plaat, rustbeeld en kop, seconden
python3 tools/etalage-beeld/sheet.py out/v4/draft   # proefvel met kaarten op licht en donker
node tools/etalage-beeld/shots.mjs --out <map>      # browsercontrole, screenshots en opname
node tools/etalage-beeld/serve.mjs 8780             # preview.html op http://127.0.0.1:8780/tools/etalage-beeld/preview.html
```

Een volledige build duurt op een snelle Apple-Silicon-Mac ongeveer drie kwartier (Cycles op de GPU,
twee lagen per beeld, 81 beelden per clip, ±5 s per beeld als de GPU vrij is);
`--draft --rest-only` duurt seconden, `--parts head` rendert alleen de koppen
opnieuw.

## Wat er gebeurt

1. **Platen** — `build.py` haalt de foto op bij Wikimedia Commons (cache in
   `cache/`), controleert sha1 en licentie tegen `config.json`, snijdt uit in de
   kaartverhouding, vervaagt als scherptediepte en geeft een kleurbehandeling.
   `plaat.webp` 1200×1464, ≤ 200 KB. Bron en bewerking: `HERKOMST.md`.
2. **Render** — `blender_render.py` draait in Blender (headless, Cycles/Metal).
   Het laadt de GLB van de module, speelt de gebakken acties `Peck` en
   `WingWave` op snelheid 1,0 af en rendert per beeld twee lagen: de duif en haar
   schaduw op een onzichtbare grond. De omgevingskleur komt uit de plaat, zodat
   de duif in dezelfde lucht staat. Het licht is bewolkt daglicht: een groot,
   hoog hoofdlicht en een randlicht dat geen schaduw werpt; de vleugelveren
   werpen geen schaduw op het lijf (anders een harde lijn langs de vleugelrand).
3. **Samenstellen** — van de schaduw blijft alleen een compacte contactschaduw
   onder en rond de poten over (een ellips om het grondpunt, dat per beeld
   meeschuift), die ook nog 5 % van de kaartranden zacht afloopt; zo schuift er
   bij het kantelen van de kaart nooit een schaduwrand buiten de plaat. De duif
   gaat erover. Het eerste en laatste beeld van elke clip zijn het rustbeeld
   zelf. Alle beelden krijgen één gedeeld canvas: de omhullende van rust en
   beide clips, en daaruit volgt de `layer`-box in kaartfracties. Die box hoeft
   de kaart niet te bedekken: hij omvat alleen duif en schaduw.
4. **Coderen** — `rust.webp` en `kop.webp` (cwebp, met alfa); per clip WebM
   (VP9, `yuva420p`, `-auto-alt-ref 0`, BT.601 gelabeld; crf stapt op tot de clip
   ≤ 400 KB is) en HEVC met alfa (`.mov`, `hvc1`, rechte alfa): de hoogste
   VideoToolbox-kwaliteit die onder 400 KB blijft, met alfakwaliteit 0,5 (0,3
   verdubbelde de fout langs de alfarand). De log noemt per clip de PSNR op de
   dekkende duif; dat is de scherpte die je ziet bij de wissel met `rust.webp`.
   De clips zijn 25 fps (81 beelden, 3240 ms): bij 30 fps paste de HEVC van de
   vleugelclips alleen op een lagere kwaliteit onder 400 KB, ±1,3 dB minder
   detail op de duif. Met bewegingsonscherpte is 25 fps rustig genoeg.
5. **Manifest en controle** — `manifest.json` volgens het contract, daarna
   `check.py` (zie onder).

## Keuzes

**Blender in plaats van de three.js-runtime.** De gebaren zitten gebakken in de
GLB (acties `Peck` en `WingWave` op een armature, 3,2 s, eerste en laatste beeld
gelijk aan de bindpose — zie `extras.animation_contract` in de GLB). De module
speelt precies die acties af; Blender importeert ze onveranderd, dus de beweging
is dezelfde. De module voegt alleen een willekeurige snelheid van 0,9–1,1 toe;
wij nemen 1,0. Cycles geeft wat een realtime-render niet geeft: zachte
padgevolgde belichting, bewegingsonscherpte, en nette alfaranden en een echte
contactschaduw in één pass. Het is ook het gereedschap waarin het model gemaakt
is. Er is geen code van de module of van Parai overgenomen.

**Een reactie is een stap naar de kijker.** In rust breekt de kop ±13 % van de
kaarthoogte door de bovenrand. Tijdens een reactie schuift de camera op dezelfde
hoogte 7,5 % dichter naar de duif, zonder van kijkrichting te veranderen, en
draait 5° om haar heen; de lensverschuiving laat het grondpunt 2 % kaarthoogte
zakken. De poten komen zo iets naar voren en omlaag (±0,75 → ±0,78) en de kop
verder boven de kaart uit (±0,13 → ±0,18), terwijl het gebaar zelf dat van de
module blijft. Vroeger (13 %, grondpunt vast) las dat als opzwellen. Aan het
eind gaat de camera zacht terug naar exact de ruststand.

**Kijkhoeken (v4).** Elke duif heeft haar eigen kijkhoek, zoals in de
module; op de kaarten kijken de buitenste duiven bij binnenkomst naar het
midden van de waaier. Azimut 0 is recht van voren, positief naar haar
rechterflank (de kop wijst in beeld naar rechts).

| Duif | Azimut | Kop (avatar) | Waarom |
|---|---|---|---|
| Kennispartner | 0° (v3: −20°) | 0° (v3: −50°) | Recht van voren, in chat en vergadering; zo toont de module haar ook (azimut −π/2 in de conventie van de module). |
| Stakeholder | −30° (v3: −38°) | −30° | Driekwart van voren. De stropdas hangt midden op de borst en blijft heel in beeld; de kop wijst naar links, naar het midden. De module toont haar 15,5° van voren, gedraaid naar het antwoord; de etalage kiest de kant van het midden van de waaier en iets meer diepte. |
| Signalerende duif | 57° (v3: 72°) | 57° | Dezelfde hoek als de module (azimut −0,57 rad: 57° van voren), maar van haar rechterflank, zodat de snavel naar het midden wijst. Zo staat zij heel in beeld, en is zij minder breed dan het zuivere zijaanzicht van v3, wat de drie in gewicht dichter bij elkaar brengt. `centerX` 0,47. |

De pikbeweging van v4 brengt de kop laag en ver naar voren. Bij de Signalerende
duif kwam die buiten het renderbeeld; `pad.right` is daarom 0,6 (was 0,34), en
haar duivenlaag steekt tot 0,42 kaartbreedte rechts buiten de kaart (v3: 0,26).
In de waaier op een telefoon (≤ 390 px) viel haar kop daardoor bij het pikken
buiten het scherm, en op 320 px dook de pikkende kop van de Stakeholder onder de
vorige-pijl. Als een kaart naar voren komt, doet de duif daarom het
vleugelgebaar (`defaultClip: "ruffle"`, voor alle drie hetzelfde): de
vleugels gaan omhoog en boven de kaart uit, dus weg van de pijlen (die midden
op de kaart staan) en binnen het scherm vanaf 320 px, en de duif breekt zo
zichtbaar uit de kaart in plaats van erin weg te duiken. Een duif kan per duif
een eigen `defaultClip` krijgen, maar dat gebeurt nu niet. Aan tafel speelt
altijd het gebaar uit de opname; daar blijft de pikkende kop links van de
tekst. De koppen worden op 512 px uitgevoerd (was
256), zodat ze in de videoband en op papier (48 mm) scherp blijven.

**HEVC via AVAssetWriter, niet via ffmpeg.** `hevc_videotoolbox` in ffmpeg
schrijft bij BGRA-invoer geen kleurlabels (VUI 2/2/2, "onbekend"), en een remux
om ze toe te voegen breekt de alfalaag. In WebKit stond zo'n clip 5 niveaus
lichter dan `rust.webp` — een zichtbare sprong bij het wisselen. `hevc_alpha.swift`
gebruikt dezelfde VideoToolbox-encoder via AVAssetWriter met BT.709-primaries,
sRGB-overdracht en BT.709-matrix, en met rechte (niet voorvermenigvuldigde)
alfa: voorvermenigvuldigd gaf in WebKit en Chromium een donker randje langs de
silhouet. Gemeten in Playwright ligt het eerste en laatste videobeeld nu
gemiddeld 1–2 niveaus van het rustbeeld (WebKit met HEVC, Chromium met WebM),
wat de codering zelf is. De `.mov` heeft zijn index (moov) vooraan
(`shouldOptimizeForNetworkUse`), zodat Safari kan beginnen voor alles binnen is.
`build.py` compileert het één keer naar `out/bin/`.

## Voor de site

- **Bronvolgorde:** eerst `<source … type='video/quicktime; codecs="hvc1"'>`,
  dan `<source … type="video/webm">`. WebKit speelt VP9-WebM af maar zonder alfa
  (zwart vlak); Chromium slaat de `.mov` over en neemt de WebM.
- **Server:** `video/webm`, `video/quicktime` en Range-verzoeken (206); Safari
  speelt anders geen video.
- **Wisselen:** toon `rust.webp`; bij een reactie `currentTime = 0`, `play()`, en
  verberg het beeld pas bij het eerste gepresenteerde videobeeld
  (`requestVideoFrameCallback`, anders `playing`); bij `ended` terug naar het
  beeld. Beide staan in dezelfde `layer`-box. `preview.html` doet dit voor.
- **Lagen:** plaat → naamverloop → duivenlaag → naamtekst.
- **Manifest:** per duif staat `model` { file, version }. `model` bovenaan
  volgt daaruit: hetzelfde model voor alle drie (v3) geeft file, version en
  final; één versie met een eigen bestand per duif (v4) geeft version en final;
  gemengd (bv. na `--model v4 --only signalerend`) geeft `version: "per duif
  verschillend: …"`. `final` is alleen true als het model van elke duif final is. Elke credit heeft `nl`, `en`, `url` (de bron), `license` en
  `licenseUrl`.
- **Firefox** speelt VP9 met alfa volgens de documentatie (met de software-
  decoder); headless Firefox startte in deze omgeving niet, dus dat is niet
  getest.

## v4

Sinds 24 september 2026 staat `config.json` op `"model": "v4"` (v4 iteratie 17,
`final: true`): v4 is die dag visueel goedgekeurd zoals het is. Per duif wordt
haar eigen GLB uit de module geladen (`stadsduif-v4-biologisch.glb`,
`-individualistisch.glb`, `-signalerend.glb` in
`module/duif/frontend/public/media/`); platen en licht bleven, de kijkhoeken zijn
per duif gekozen (zie "Keuzes").

Bij een nieuwe modeliteratie:

```
python3 tools/etalage-beeld/build.py --draft --rest-only            # eerst de kadrering, seconden
python3 tools/etalage-beeld/build.py --draft                        # dan de gebaren, minuten
python3 tools/etalage-beeld/build.py                                # dan alles, ±40 min
python3 tools/etalage-beeld/build.py --glb-dir <map>                # GLB's die nog niet in de module staan
```

v4-proeven horen in `out/` (`--draft`), niet in `site/`. De acties moeten `Peck`
en `WingWave` heten (anders `gestures` in `config.json` aanpassen). Zet
`models.v4.version` bij, zet `final` op `false` tot de nieuwe stand is goedgekeurd,
controleer de kopuitsnede (`kop.webp`) en kijk het proefvel en de clips beeld
voor beeld na: een stropdas, een andere pose of een ander gebaar kan de
omhullende en dus de `layer`-box veranderen (de site leest die uit het
manifest), en een gebaar dat buiten het renderbeeld komt stopt de build
(`pad` vergroten). Draai daarna `tools/etalage-site/deelbeeld.py` opnieuw.

Draai je `--only` met de manifest-stap (of op een verse clone zonder `out/`),
dan houden de duiven die niet gebouwd zijn hun regel uit het bestaande manifest.

## Controle (`check.py`)

Manifest tegen het contract (velden, paden, `final` als boolean, `model` per
duif, credits met NL, EN, bron-URL, licentie en licentie-URL), groottes (plaat ≤ 200 KB, clip ≤ 400 KB per formaat), canvas van
rust en clips gelijk en de layer-box consistent met de kaartverhouding. Alfa:
WebM heeft `alpha_mode=1`, HEVC de tag `hvc1`; beide worden met ffmpeg naar RGBA
gedecodeerd (libvpx-vp9 voor de alfa van VP9; de HEVC-decoder van ffmpeg 8 leest
de alfalaag) en gemeten: transparante hoeken, dekkende duif. Naad: eerste en
laatste gedecodeerde beeld tegen `rust.webp`. Verder dat de clip beweegt en hoe
ver de duif boven de rustlijn uitkomt. En de eis van de deploy: onder
`site/etalage/media/duiven/` geen dotfiles, geen symlinks en alleen json, webp,
png, jpg, webm, mov en mp4 (proefbestanden van de HEVC-keuze staan in `out/`).
Verslag: `out/check.json`.

`shots.mjs` doet de browserkant: elke clip in Chromium (WebM) en WebKit (HEVC)
over magenta, met gemeten hoeken, gekozen bron en naad. Safari zelf is headless
niet te testen; WebKit uit Playwright is de benadering.

## Bestanden

| | |
|---|---|
| `config.json` | modellen (v3/v4), kaart, kijkhoek, framing, reactie, gebaar in de waaier, licht en plaat per duif |
| `build.py` | het ene commando |
| `blender_render.py` | render binnen Blender |
| `hevc_alpha.swift` | HEVC met alfa en kleurlabels |
| `check.py` | controle van de uitvoer |
| `sheet.py` | proefvel |
| `preview.html`, `serve.mjs`, `shots.mjs` | proef in de browser |
| `HERKOMST.md` | bronnen, licenties en bewerkingen |
| `cache/`, `out/` | niet in git |
