# Uitlegvideo — opnemen, afspelen, monteren

Deze map maakt de Engelstalige uitlegvideo voor de help van de werkruimte en de videoband van de etalage: de kale app, snel gemonteerd. Dit bestand beschrijft de machine; `storyboard.json` en `video.config.json` beschrijven de video. `stand/` bevat een losse pagina die de video zonder geluid in een loop afspeelt, voor een scherm op een stand.

```
record.mjs          één take live opnemen (de enige stap met modelaanroepen)
capture.mjs         een gekozen take afspelen in de echte frontend en het scherm opnemen
render.mjs          afspelen (tenzij --reuse-capture) + monteren → out/uitlegvideo[-formaat].mp4
video.config.json   gekozen takes (met reden), aanvullende takes, beeldformaten, tijdkaart, tempo, naamsvermelding
storyboard.json     de shots (ankers in de opnamen), ondertitelspelling, poster
takes/              alle opgenomen takes (in git)
out/                captures, tussenbestanden en de video (niet in git)
```

In deze publieke versie is het adres van de modelproxy vervangen door een neutrale waarde. Daarnaast is in het veld `selection` van de takes (`takes/`) een verwijzing naar een intern werkdocument weggelaten.

## Eisen

- Node 24 met Playwright (`npm ci` in de root van de repository) en de Chromium van Playwright.
- `ffmpeg` met libx264 en AAC (Homebrew-ffmpeg 8 is getest).
- Een draaiende duivenserver die de app serveert. Opnemen vraagt de echte providers (een eigen lokale skipauth-opnameserver op een vrije poort, `--base`; het verplichte model `vertex/gemini-3.6-flash-eu` via de proxy uit `PARAI_LLM_BASE_URL`; Soniox). Afspelen vraagt alleen een server met dezelfde frontend en kennisbank; er gaat dan geen enkel verzoek naar `/api/duif/v1`. Dat wordt gecontroleerd: capture en het lezen van de huisstijl houden een netwerklog bij en stoppen bij één zo'n verzoek (`network` in capture.json staat op 0).
- Een Mac met GPU: de opname draait headless Chromium op ANGLE/Metal. Zonder die vlaggen rendert WebGL in software en haalt de 3D-duif maar enkele beelden per seconde.

## Renderen (ook na een nieuw model of nieuwe portretten)

```
node tools/uitlegvideo/render.mjs                  # standaardformaat 4x3-1152 → out/uitlegvideo.mp4
node tools/uitlegvideo/render.mjs --format 16x9    # → out/uitlegvideo-16x9.mp4
```

Dat speelt de drie gekozen takes af tegen de server uit `video.config.json` (of `--base <url>`), neemt ze op en monteert de video. Geen modelaanroep. Het 3D-model, de portretten, de veldgeluiden en de lettertypen komen op dat moment uit de app, dus een nieuw duivenmodel staat vanzelf in beeld. Afspelen en opnemen duurt ongeveer drie minuten (de scènes lopen op hun eigen tempo, de vergaderingen op ware snelheid), monteren ongeveer een halve minuut. `--reuse-capture` slaat het afspelen over als de capture nog bij de gekozen take en het formaat hoort. `--out <map>` zet de eindbestanden elders.

Uitvoer in `out/`: de video (H.264 High yuv420p, volledige BT.709-kenmerken, 30 fps, AAC 48 kHz stereo, ongeveer −16 LUFS, faststart), een poster, een WebVTT en een tijdlijn (`.timeline.json`: shots, ondertitels, gesproken zinnen, takes, herkomst van de afspeelserver, naamsvermelding). Installeren in de help gebeurt met `stand/install-video.mjs <mp4> --poster <png> --vtt <vtt>` (zonder `--approve` blijft het pakket `temporary`); dat script vraagt H.264 yuv420p met vierkante pixels en AAC, tot 48 MB. De etalage krijgt dezelfde mp4 en vtt in `site/etalage/media/video/` (`uitleg.mp4`, `uitleg.en.vtt`, poster `uitleg.webp` in 4:3); de standspeler krijgt de mp4 als `stand/uitlegvideo.mp4` (niet in git).

### Beeldformaten

Een formaat is het venster van de app, de schaal waarmee Chromium het tekent en de videomaat (`formats` in `video.config.json`; venster × schaal = videomaat, `lib/format.mjs`). Er is geen uitsnede of zoom: het formaat bepaalt alleen hoe groot de app oogt en hoeveel er onder elkaar past. `16x9` is 1280×720 bij 1,5 (1920×1080). De 4:3-varianten zijn 1440×1080: `4x3-1024` (1024×768 bij 1,40625; grotere letters, maar onder 1100 px zet de kennisgraaf zijn delen onder elkaar en valt het leespaneel van het artikel onder de vouw) en `4x3-1152` (1152×864 bij 1,25; de kennisgraaf in drie kolommen). `4x3-1152` is het standaardformaat (`defaultFormat`), dus `render.mjs` zonder `--format` maakt het, als `out/uitlegvideo.mp4`. De indeling van de app hangt af van de breedte: onder 1100 CSS-pixels zet de kennisgraaf boom, kaart en leespaneel onder elkaar, en de bronlade schuift het gesprek pas opzij vanaf 83 rem (1328 px); daaronder ligt zij eroverheen.

## Een take opnemen en kiezen

```
node tools/uitlegvideo/record.mjs --scene chat
node tools/uitlegvideo/record.mjs --scene meeting-nonverbal
node tools/uitlegvideo/record.mjs --scene meeting-individualist
```

Opnemen alleen tegen de eigen opnameserver, nooit tegen een server die anderen gebruiken, en één take tegelijk: de server begrenst dure verzoeken per account. Het script weigert als de server voor chat of detectie niet het verplichte model gebruikt, via precies het HTTPS-adres uit `PARAI_LLM_BASE_URL` in de omgeving van `record.mjs` (`lib/model.mjs`); een eerdere alias telt als een ander model. De takes van 23 september zijn opgenomen met de alias die toen gold, `vertex_ai/gemini-3.5-flash`. Dat blijft hun herkomst (`app.modelPolicy` in de take, bij de chat ook de provenance per antwoord). Afspelen en monteren doen geen modelaanroep en kijken er niet naar, dus die takes blijven bruikbaar. Een nieuwe chatopname loopt meteen door de bronnenlade en de kennisgraaf, dus die leesverzoeken zitten dan in de take zelf.

- **chat** (`lib/scenes.mjs`): de vraag typen en versturen bij de Knowledge Partner, bronnummer 6 openen, *Open in the knowledge graph*, het volgende niveau openen, het artikel openen, terug naar het gesprek, *ask the other pigeon*.
- **meeting-…-respond** (de huidige video): de duif aan tafel zetten met zelfstandig meedoen uit, *Full transcript* openen, het fragment laten uitspelen en 1,8 s na het laatste woord *Let the pigeon respond* (Stakeholder) of *Let the pigeon step in* (signalerende duif) drukken. Bij het afspelen blijft *Full transcript* dicht zolang `capture.fullTranscript` in `video.config.json` op `false` staat (zie *De vergadering in beeld*).
- **meeting-…** (eerdere takes): de duif aan tafel zetten (zelfstandig meedoen aan; bij de Stakeholder zet de driver de schakelaar om, zoals een begeleider dat kan). Het vergaderfragment gaat met vier seconden stilte vooraf door de synthetische microfoon (zonder aanloop viel de eerste zin weg bij het verbinden van de spraakherkenning). `meeting-audio/meeting-dialogue.wav` is afgeleid en niet in git; ontbreekt het, dan mengen de scripts het opnieuw uit de vier mp3's en `meeting-dialogue.json`. De duif mag het hele fragment plus stilte reageren; komt een pratende duif niet uit zichzelf, dan drukt de driver *Let the pigeon respond*. Daarna *End meeting*.

Elke run maakt `takes/<scène>-<tijd>/take.json` met alle verkeer van `/api/duif/v1` (gestreamde antwoorden per brok met hun moment), alle berichten op het vergaderkanaal, de klikken, de momenten waarop geluid begon, de broncommit en de bundle van de server, het modelbeleid, de kennisbank en (chat) de provenance uit het `done`-frame. De duifstem staat ernaast als wav per beurt. Cookies en autorisatiekoppen komen er niet in; de gesprekslijst van het opnameaccount wordt tijdens de opname beperkt tot gesprekken uit die opname.

### Aanvullende take (alleen lezen)

De gekozen chattake is opgenomen vóór de video door de bronnen en de kennisgraaf ging. Die leesverzoeken zijn één keer opgehaald zonder modelaanroep:

```
node tools/uitlegvideo/capture.mjs --scene chat --supplement
```

Dat speelt de chattake af en laat alleen GET-verzoeken naar `/api/duif/v1/graph/` echt naar de server gaan; ze worden bewaard als `takes/chat-supplement-<tijd>/` (met `supplementFor`) en staan onder `supplements` in `video.config.json`. Bij gewoon afspelen komen ook zij uit de take.

Kiezen is één regel onder `takes` in `video.config.json`; zet de reden onder `choices`. Alle kandidaat-takes blijven staan.

## Hoe afspelen werkt

`replay/page-runtime.js` draait als init-script in de echte frontend. Het vervangt `fetch` voor `/api/duif/v1/**` en `EventSource` voor het vergaderkanaal: elk verzoek krijgt het opgenomen antwoord op dezelfde methode en hetzelfde pad, in dezelfde volgorde (`lib/match.mjs`). Een verzoek of duifkanaal dat de opname niet kent krijgt nooit een verzonnen antwoord en gaat ook niet naar de server: de capture stopt. Een beurt, een vergelijking of een vergadering openen moet dezelfde inhoud sturen als bij de opname (alleen de willekeurige aanvraag-id mag verschillen); de driver typt de vraag uit de take zelf. Na elk chatantwoord vergelijkt de capture de tekst in beeld met de `artifact_delta`'s van de take en stopt bij verschil.

De microfoon is ook bij het afspelen de synthetische microfoon met hetzelfde fragment, dus de luisterindicator en de transcriptie in de app zijn echt; de audioblokken krijgen het opgenomen 202-antwoord. De stem en de veldgeluiden speelt de app zelf af; het init-script meet wanneer. Een veldgeluid moest bij de opname eerst laden (0,6 s); bij afspelen staat het meteen klaar, dus wacht het init-script even lang, zodat het geluid klinkt op het moment waarop het toen klonk. Aan het eind van elke vergadering vergelijkt de capture de bijdrage van de duif in beeld met de take.

Er staat een muis in beeld: headless Chromium tekent geen cursor, dus de afspeelpagina krijgt een pijltje dat de echte muisbewegingen van de driver volgt, met een korte ring bij elke klik. De driver laat de muis naar elk element glijden voordat hij klikt (`click` in `lib/scenes.mjs`) en zet hem na het aan tafel zetten opzij. Het pijltje doet niets met de app (`pointer-events: none`).

De knop in de vergadering gaat in op het moment waarop de app in de take haar verzoek deed (`respond` of `intervene`), 100 ms eerder (de take markeerde de klik 212 ms vóór het verzoek, dus dat valt binnen het venster waarin zij klikte). Zo doet de app haar verzoek 4–6 ms na de klik en komt de reactie van de duif ~105 ms na de klik, in die volgorde, zoals in de take. In beeld vallen klik en reactie in hetzelfde frame: de beelden van de capture lopen ongeveer 0,1 s achter op de klok van de gebeurtenissen. De muis vertrekt daarvoor eerder en glijdt 0,45 s naar de knop. Daarna wacht de capture op het verzoek van de knop; komt dat niet (de app negeerde de klik), dan stopt zij. Tot die klik speelt de vergadering op ware snelheid, zoals altijd.

In de kennisgraaf vliegt de kaart bij het openen naar de bevinding, maar de indeling schuift daarna nog even door; waar de bevinding dan ligt hangt af van hoe snel alles binnenkwam, en zonder perspectieven lag zij soms buiten beeld. Zodra de kaart stilligt (`pacing.chat.graphSettle`) kiest de driver de bevinding opnieuw met Enter op de rij in de boom die al gekozen is; de camera vliegt er dan heen. Dat doet geen verzoek.

Het scherm wordt opgenomen met de CDP-screencast: venster 1280×720 bij schaal 1,5, dus beelden van 1920×1080. De screencast levert een beeld als er iets verandert (tot 95–100 per seconde bij de gebaren van de 3D-duif en het typen; gemiddeld over een hele opname 8–47, afhankelijk van hoe lang het scherm stilstond). De montage zet dat om naar vaste 30 fps door per uitvoerbeeld het laatst getoonde opnamebeeld te nemen.

Elke capture legt de herkomst van de afspeelserver vast (broncommit en bundlehash, `app` in capture.json en `replayApp` in de tijdlijn). Wijkt de bundle af van die bij de opname (`bundleSha256` in de take), dan meldt capture.mjs dat met een groot waarschuwingsblok. Het is geen fout (na een nieuw model of nieuwe portretten hoort dat zo); kijk dan de beelden na.

## Eerlijkheid

- **Geen tekst veranderd.** Elke zin van een duif of van de facilitator is modeloutput uit de take, byte voor byte doorgegeven aan de app; de app tekent hem zelf, en de capture controleert het antwoord in beeld. De stem is de opgenomen stem.
- **Wachten is ingekort, de volgorde niet.** De tijdkaart (`lib/timemap.mjs`) is monotoon: stukken wachttijd worden gelijkmatig korter, niets wisselt van volgorde. Chat: elke pauze vóór het eerste woord van de duif duurt hooguit 0,5 s (de stappen blijven even zichtbaar), een pauze tijdens een beurt hooguit 6 s. Vergadering: alles tot de knop op ware snelheid (wat er gezegd wordt, het verloop, de knop); alleen het denken van het model tussen de knop en het eerste woord van de duif wordt korter (elke pauze daarin hooguit 1,2 s; bij de Stakeholder werden 13,2 s zo 3,0 s), en de stille wacht tussen haar tekst en haar stem, de tijd die de spraaksynthese nodig had (`voiceWaitMs`, hooguit 1 s; bij de Stakeholder werden 4,2 s zo 1,0 s). De tekst verschijnt op hetzelfde moment en de stem speelt in haar eigen tempo, alleen eerder. Het veldgeluid klinkt met de laadtijd van toen. capture.mjs weigert een tijdkaart die een vergadering vóór de knop zou inkorten.
- **Wat de montage kiest:** welke take, welk stuk (harde knippen, en aan het eind een overvloeier van het laatste beeld naar het eerste, zodat de loop geen sprong maakt) en hoe hard het geluid is. De mensen zijn niet te horen; wat zij zeggen staat in het verloop van de app. De stem van de duif en haar veldgeluid zijn wel te horen.
- **Wat van de schil verborgen is (alleen bij afspelen, `visibility: hidden`):** de elementen van de omringende Parai-schil in de kop die niet bij de duivenmodule horen. Er wordt geen account getoond, en paginabezoek- en CSP-meldingen gaan niet naar de server. Er wordt geen `?dev=1` gebruikt.

## De vergadering in beeld

De verzonnen deelnemers zijn niet te horen en krijgen geen ondertitel: wat zij zeggen staat in het transcriptdeel van de app zelf, zoals de spraakherkenning het maakte. De opzetkaart van de vergadering valt weg; de shots beginnen in de lopende vergadering.

Sinds de v4-duiven vult elke duif haar beeld tot de bovenrand. Op 1152×864 passen de hele duif en het hele verloop (*Full transcript*) dan niet samen op het podium: de app houdt de nieuwste regel in beeld en schuift de kop van de duif eruit (bij de Stakeholder twaalf seconden lang). Daarom blijft *Full transcript* bij het afspelen dicht (`capture.fullTranscript: false`): de app toont dan haar standaardstand, de laatste twee zinnen onder de duif (*Recent speech*). Opnemen deed het wel open; dat is alleen weergave, geen verzoek. Het podium is dan nog iets hoger dan het scherm, dus de driver scrolt zoals een kijker: zo ver omlaag dat de laatste zinnen te lezen zijn, nooit zo ver dat de bovenkant van de duif wegvalt, en opnieuw zodra er een zin bijkomt (`holdPigeonInView` in `lib/scenes.mjs`). Na de knop scrolt het podium bij de Stakeholder naar boven zodra zij reageert, zodat haar bijdrage naast haar staat; bij de signalerende duif blijft het staan. `capture.fullTranscript: true` geeft de oude weergave terug.

## Naamsvermelding

Het 3D-model en de portretten vallen onder CC BY-SA 3.0 (fototextuur Luc Viatour / Lucnix.be, bron https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg). De render leest de catalogus uit `credits.manifest` in `video.config.json` (pad vanaf de root van de repo, geen terugval) en vergelijkt per vergadering de bron en de licentie van het model van die duif (sinds v4 heeft elke duif haar eigen model, `native.pigeons` in de catalogus) met wat de afgespeelde app zelf onder *Media credits* vermeldt; wijken ze af, dan stopt de render. Per herkomst komt er één Engelse regel uit de gestructureerde velden van `attribution` (rol vertaald, "fototextuur" → "photo texture by"); een onbekende vorm blijft letterlijk staan, met een waarschuwing. In beeld staat de regel met de licentie (`creativecommons.org/licenses/by-sa/3.0`), klein maar leesbaar (22 px) in het lege midden van de kop, de laatste seconden; de bron-URL staat in de tijdlijn. CC0-werk (de veldgeluiden) vraagt geen vermelding.

## WebVTT

De WebVTT is voor de speler in de help en in de etalage (met geluid; in de etalage staan de ondertitels standaard aan). Er staat alleen in wat de duif laat horen (`lib/captions.mjs`): de uitgesproken zinnen van de Stakeholder (`<v Stakeholder>`, per zin getimed op de pauzes in haar eigen stem) en `[pigeon sound]` voor het veldgeluid. De regels van de vergaderdeelnemers staan in het verloop in beeld. De naamsvermelding staat er niet in: zij is in beeld ingebrand en staat in de voet van de site; als ondertitel lag zij in de etalage bovenop die ingebrande regel.

## Herhaalbaarheid

Vergadershots hangen aan de microfoon en aan geluidsmomenten uit de app; chatshots aan de klikken van de driver in een vast tempo. Twee renders van dezelfde takes verschillen hooguit een paar beelden in lengte; wat er gebeurt en in welke volgorde is altijd gelijk.

## Standspeler

`stand/play-loop.html` speelt de video zonder geluid in een eindeloze loop,
schermvullend op zwart, zonder bediening en zonder netwerk; hij werkt als los
bestand (`file://`) in Chrome en Safari. Zet de pagina met de gekozen video als
`uitlegvideo.mp4` in één map en open de pagina; **F** of dubbelklikken schakelt
schermvullend. Een `uitlegvideo.mp4` in `stand/` wordt door git genegeerd.

`stand/install-video.mjs` installeert een nieuwe montage in de werkruimte (zie
`module/duif/frontend/public/media/README.md`) en werkt het mediamanifest bij.

## Tests

```
node --test tools/uitlegvideo/lib/*.test.mjs
```

Tijdkaart, koppelen van verzoeken, beeldformaten, de gesproken zinnen van de duif, naamsvermelding, dat elk anker in storyboard.json leesbaar is en naar een bestaand moment wijst; dat een vergadertake tot de knop op ware snelheid afspeelt (geen bericht vroeger of later) en daarna nooit later; dat elke take in git na de tijdkaart dezelfde tekst in dezelfde volgorde afspeelt (en nooit later dan opgenomen); de stem heen en terug byte voor byte; geen cookies of autorisatie in de takes; en in een echte Chromium dat een onbekend verzoek of duifkanaal een melding en een dicht antwoord krijgt, zonder netwerk.

## Bekende beperkingen

- Afspelen vraagt een frontend die dezelfde verzoeken doet als bij de opname. Verandert de app haar verkeer, dan stopt de capture en is een nieuwe take nodig. Een nieuw model of nieuwe portretten veranderen het verkeer niet.
- Vergadertakes hebben geen provenance per beurt: het vergaderkanaal draagt die niet (daarvoor moet de app veranderen). De take bewaart het modelbeleid van de server en de broncommit; de chattake heeft de provenance per antwoord.
- De aanvullende take (kennisgraaf) is later opgehaald dan de chattake, tegen dezelfde kennisbank. Die eerste aanvullende take heeft geen kennisbankversie vastgelegd (de chattake wel, in zijn provenance: `kbVersion`); nieuwe aanvullende takes leggen de kennisbankstatus van de server vast. Verandert de kennisbank, dan past een nieuwe aanvullende take of een nieuwe chatopname.
- Een wachtteller in de app telt de afspeeltijd, niet de opnametijd.
