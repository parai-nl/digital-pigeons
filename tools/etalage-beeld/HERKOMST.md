# Herkomst van de beelden in `site/etalage/media/duiven/`

Alles wat hier staat is afgeleid werk. De bronnen, licenties en bewerkingen staan
hieronder; dezelfde gegevens staan kort in `credits` van `manifest.json`, zodat de
site ze in de footer kan tonen. `build.py` haalt elke foto op bij Wikimedia
Commons, controleert de sha1 van het origineel tegen `config.json` en weigert als
de licentie op Commons intussen anders is.

## De duif

| | |
|---|---|
| Model | Stadsduif v4, basis iteratie 17, per duif het eigen GLB van de module: `module/duif/frontend/public/media/stadsduif-v4-biologisch.glb` (Kennispartner, sha256 `b3f40b3b…1f11`), `stadsduif-v4-individualistisch.glb` (Stakeholder, met stropdas, `3985334f…6c17`) en `stadsduif-v4-signalerend.glb` (Signalerende duif, `835b4ab8…0919`). Gemaakt met `tools/design-model/build_pigeon_v4.py` en `variants_v4.py`. v4 is op 24 september 2026 visueel goedgekeurd; `models.v4.final` staat daarom op `true`. |
| Maker | Digitale Duiven (eigen Blender-geometrie, stropdas en de gebaren Peck en WingWave); fototextuur afgeleid van *Columba livia* door Luc Viatour / Lucnix.be |
| Bron textuur | https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg |
| Licentie | CC BY-SA 3.0 — de rustbeelden, clips en koppen hieronder zijn afgeleid werk en vallen onder dezelfde licentie |
| Bewerking | Gerenderd in Blender 5.0.1 (Cycles) met transparante achtergrond, bewolkt daglicht (groot hoog hoofdlicht, randlicht zonder schaduw; de vleugelveren werpen geen schaduw op het lijf). Per duif een eigen kijkhoek (zie README, "Keuzes"). De gebaren zijn de gebakken acties uit de GLB, onveranderd op snelheid 1,0. Alleen de camera beweegt bij een reactie: in 0,75 s een stap van 7,5 % naar de duif toe op dezelfde hoogte en 5° om haar heen, in 0,95 s terug naar exact de ruststand. Alleen een compacte contactschaduw onder en rond de poten, zacht aflopend naar de kaartranden. |

Geen levende duif gefilmd en geen scan; het is een 3D-model. Tot
24 september 2026 stond hier het v3-model (v3 iteratie 05, één GLB voor alle
drie, `final: false`); git bewaart die beelden.

## De platen

Elke plaat is een uitsnede in de kaartverhouding 0,82, geschaald naar 1200×1464,
met een sterke vervaging (Gauss, straal 12–14 px) als scherptediepte: alles op
de foto staat ver achter de duif. Daarna een kleurbehandeling: iets meer licht, minder
contrast en verzadiging, een lichte tint, wat nevel naar boven en een zachte
vignettering. Er staat geen herkenbaar persoon op: waar mensen in de foto staan,
zijn ze klein en na de vervaging vlekken. Plaat en duif worden in de browser
samengevoegd; de plaat zelf bevat geen duif.

### Kennispartner — de kade aan de Maas bij de Erasmusbrug

- **Waarom hier:** de kennispartner hoort bij de universiteit die de naam Erasmus
  draagt (AI-MAPS, Erasmus Universiteit Rotterdam); de brug met diezelfde naam
  verbindt de oevers van de Maas. De duif staat op de kade, de brug links achter haar.
- **Bron:** [File:Rotterdam (43546674525).jpg](https://commons.wikimedia.org/wiki/File:Rotterdam_(43546674525).jpg), 4656×3492, 15 augustus 2018
- **Maker:** Guillaume Vachey (via Flickr)
- **Licentie:** CC0 1.0 (publiek domein) — naamsvermelding niet verplicht, wel gedaan
- **Uitsnede:** x 1259–4123, y 0–3492 (volle hoogte)

### Stakeholder — de Coolsingel voor het stadhuis

- **Waarom hier:** de stakeholder komt op voor een belang; op het stadhuis worden
  belangen gewogen. Zij staat op de straat (de tramsporen van de Coolsingel), het
  stadhuis staat rechts achter haar, verder weg.
- **Bron:** [File:Coolsingel Rotterdam I73523 - copy.jpg](https://commons.wikimedia.org/wiki/File:Coolsingel_Rotterdam_I73523_-_copy.jpg), 3264×2448, 30 november 2008
- **Maker:** G.Lanting
- **Licentie:** CC BY 4.0
- **Uitsnede:** x 653–2660, y 0–2448 (volle hoogte), zo gekozen dat de auto op
  straat niet vlak achter haar poten staat

### Signalerende duif — het marktplein op de Binnenrotte, naast de Markthal

- **Waarom hier:** de vergadering op de etalage gaat over het marktplein; de
  signalerende duif staat op dat plein, met de Markthal rechts. De echte duiven
  op het plein staan vlak onder haar poten en lazen daar op de verkeerde schaal;
  door de sterke vervaging zijn het nu vage vlekjes in de bestrating.
- **Bron:** [File:Rotterdam - Binnenrotte plein.jpg](https://commons.wikimedia.org/wiki/File:Rotterdam_-_Binnenrotte_plein.jpg), 5971×3359, 18 december 2025
- **Maker:** Jorge Franganillo (via Flickr)
- **Licentie:** CC BY 4.0
- **Uitsnede:** x 3217–5971, y 0–3359 (volle hoogte)

## Overwogen en niet gekozen

- Stadhuis: *City hall Rotterdam 2018 1.jpg* (Steven Lek, CC BY-SA 4.0) was de
  eerste keus, met toren en gevel groot in beeld. Bij de beoordeling
  stond de duif daar tegen de gevel te zweven, met haar schaduw op de gevel; er
  is op die foto geen grond op de hoogte van de poten.
- Stadhuis: *Rotterdam stadhuis.jpg* (F. Eveleens, CC BY 3.0) — lossere licentie,
  maar harde zon en blauwe lucht; naast de twee bewolkte platen viel hij uit de toon.
- Markthal: *Markthal @ Rotterdam Blaak* (Guilhem Vellut, CC BY 2.0) — de boog vult
  het beeld, maar er lopen mensen vlak voor; de Binnenrotte-foto vertelt het verhaal
  van het marktplein beter.
- Erasmusbrug: de CC0-foto's uit Unsplash zijn strak blauw of avondlicht; deze kade
  heeft hetzelfde grijze daglicht als de andere twee.
