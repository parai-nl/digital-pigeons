# Notice

Copyright (c) 2026 Parai B.V.

The source code in this repository is released under the MIT license (see
[`LICENSE`](LICENSE)). **Not everything in the repository is source code, and
not everything is covered by MIT.** This file lists the parts that are not,
and under which terms they are included.

The prompts and personas of the pigeons (`module/duif/config/personas.yaml`,
`module/duif/config/prompts/` and the prompt texts in the Go code) are
project-specific content of this repository and are covered by MIT, like the
source code.

Some material is **included with permission** of its makers, of the people
shown and of the rights holders: the posters, cards and print files, the
third-party logos, the synthetic voices (including the soundtrack of the
explainer video) and a quoted student work. That permission covers its
inclusion in this repository. The material is **not** covered by MIT or by any
other open license; all rights remain with its makers and rights holders, and
reusing it outside this repository requires their permission.

## Overview

| Part | Where | Terms | Status |
|---|---|---|---|
| Source code, configuration, prompts and personas, own documentation | everywhere unless listed below | MIT | — |
| 3D pigeon and everything rendered from it, including the picture of the explainer video | see [§1](#1-the-3d-pigeon-and-its-derivatives) | CC BY-SA 3.0 | licensed |
| Soundtrack of the explainer video (synthetic voices) and its captions | see [§1](#the-explainer-video) | no open license | included with permission; **not under MIT or CC BY-SA** |
| Background photos of the showcase cards | `site/etalage/media/duiven/*/plaat.webp` | CC0 1.0 / CC BY 4.0 | licensed |
| Pigeon field recordings | see [§3](#3-field-recordings) | CC0 1.0 | licensed |
| Fonts | `site/etalage/vendor/fonts/` | SIL OFL 1.1, see [`LICENSES/FONTS.md`](LICENSES/FONTS.md) | licensed |
| Synthetic voices (Soniox, ElevenLabs) | see [§4](#4-synthetic-voices) | provider terms, no open license | included with permission; **not under MIT** |
| Quotations from research literature | `site/etalage/data/opnames.json`, recording takes | right of quotation; one student work with permission of its author | **not under MIT** |
| Posters, cards and print files | `site/etalage/media/{posters,kaartjes,materiaal}/` | all rights reserved by their makers | included with permission; **not under MIT** |
| Logos and trademarks | `site/etalage/media/logo/`, and inside the posters and cards | trademark law | third-party logos included with permission; **not under MIT** |
| Knowledge-graph images | `site/etalage/media/beeld/kennisgraaf-*.webp` | Parai imagery, all rights reserved | not under MIT, see [§9](#9-private-dependencies) |

## 1. The 3D pigeon and its derivatives

The 3D pigeon is an own Blender model whose photo texture is derived from
*Columba livia* by **Luc Viatour / Lucnix.be**
(<https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg>),
licensed under [CC BY-SA 3.0](https://creativecommons.org/licenses/by-sa/3.0/).
The model and every image, clip or video rendered from it are adaptations and
are distributed under **CC BY-SA 3.0**, not under MIT:

- the models, rest posters and portraits in `module/duif/frontend/public/media/`
  (`stadsduif-v4-*.glb`, `stadsduif-v4-*-rust.png`, `stadsduif-v4-*-kaart.webp`,
  `stadsduif-v4-*-kop.webp`);
- the picture of the explainer video and its poster in `site/etalage/media/video/`,
  and of the identical explainer package (`uitlegvideo*`) in
  `module/duif/frontend/public/media/` where present (the soundtrack is not
  included, see below);
- the pigeon layers, clips and heads of the showcase
  (`site/etalage/media/duiven/*/{rust,kop}.webp`, `*.webm`, `*.mov`), the
  fallback portraits (`site/etalage/media/portretten/`) and the share image
  (`site/etalage/media/deel/deel.jpg`);
- the test fixtures `tests/etalage/fixtures/media/rust.webp`, `kop.webp` and
  `clip.webm`: renders of an earlier version (v3) of the same model, with the
  same photo texture.

Attribution, as shown in the footer of the showcase:

> 3D pigeon: Digitale Duiven, photo texture Luc Viatour / Lucnix.be · CC BY-SA 3.0

The changes (own geometry and anatomy, the gestures *Peck* and *WingWave*, the
plumage variants and the tie of the stakeholder pigeon) are listed per file in
the public attribution of `module/duif/config/media-manifest.v1.json`. The
scripts that build and render the model (`tools/design-model/`,
`tools/etalage-beeld/`) are source code under MIT.

### The explainer video

The explainer video (`site/etalage/media/video/uitleg.mp4`, poster
`uitleg.webp`, English captions `uitleg.en.vtt`) is licensed in parts:

- **Picture.** The rendered pigeon and its animation, in the video and in the
  poster frame, are an adaptation of the model and are distributed under
  **CC BY-SA 3.0**. Around the pigeon, the picture is a screen recording of the
  workspace: the Parai interface and logo in it are Parai imagery and a
  trademark (§7, §9), not covered by MIT.
- **Soundtrack.** The spoken voices are synthetic voices from Soniox and
  ElevenLabs (§4). They are included with permission and are **not** under an
  open license: the soundtrack is covered by neither CC BY-SA 3.0 nor MIT. The
  captions transcribe that soundtrack and follow its status.

The media manifest (`module/duif/config/media-manifest.v1.json`) marks the
video, poster and captions as approved for public distribution. That approval
concerns showing them on the existing public showcase site; it is not a
license for the soundtrack, and reusing the soundtrack outside this repository
requires permission of its rights holders.

The other two test fixtures are not part of this: `tests/etalage/fixtures/media/plaat.webp`
shows only blurred abstract blocks (no recognisable photo; its origin is not
recorded), and `tests/etalage/fixtures/media/stem.mp3` is 1.6 seconds of
silence.

## 2. Background photos

The three background plates of the showcase cards are crops of photos from
Wikimedia Commons, blurred and colour-graded:

| Plate | Source | Author | License |
|---|---|---|---|
| `kennispartner/plaat.webp` | [Rotterdam (43546674525).jpg](https://commons.wikimedia.org/wiki/File:Rotterdam_(43546674525).jpg) | Guillaume Vachey | CC0 1.0 |
| `stakeholder/plaat.webp` | [Coolsingel Rotterdam I73523 - copy.jpg](https://commons.wikimedia.org/wiki/File:Coolsingel_Rotterdam_I73523_-_copy.jpg) | G.Lanting | [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) |
| `signalerend/plaat.webp` | [Rotterdam - Binnenrotte plein.jpg](https://commons.wikimedia.org/wiki/File:Rotterdam_-_Binnenrotte_plein.jpg) | Jorge Franganillo | [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/) |

Crops and processing are documented in
[`tools/etalage-beeld/HERKOMST.md`](tools/etalage-beeld/HERKOMST.md).

## 3. Field recordings

The three pigeon field recordings
(`module/duif/frontend/public/media/pigeon-field-*.mp3`, identical copies in
`site/etalage/media/geluid/koer/`) are excerpts of recordings on Freesound by
TheKingOfGeeks360, craigsmith and Beskinsky, all **CC0 1.0**. Sources, excerpts
and processing are in [`tools/etalage-opnames/HERKOMST.md`](tools/etalage-opnames/HERKOMST.md).

The placeholder sounds in `module/duif/config/sounds/` are synthetic tones and
noise generated by the project, not recordings of real pigeons.

## 4. Synthetic voices

Text-to-speech output is generated by third-party services and is subject to
their terms. The voices are included in this repository with permission; they
are **not** covered by MIT or by any other open license, and reusing them
outside this repository requires permission of the rights holders.

- **Soniox** (voice *Logan*): the spoken pigeon replies in
  `site/etalage/media/geluid/stem/` and the identical `stem.mp3` files in
  `tools/etalage-opnames/takes/`; the pigeon voice recorded with the explainer
  takes (`tools/uitlegvideo/takes/*/individualist-turn1.wav`); and the pigeon
  voice in the explainer video.
- **ElevenLabs** (premade voices): the synthetic meeting participants in
  `tools/etalage-opnames/uitspraken/` and `tools/uitlegvideo/meeting-audio/`, and
  their voices in the explainer video.

## 5. Quotations from research literature

The recorded answers in `site/etalage/data/opnames.json` and in the recording
takes (`tools/etalage-opnames/takes/`, `tools/uitlegvideo/takes/`) contain short
verbatim passages from published research, each with its bibliographic
reference. They are quoted under the right of quotation; copyright remains with
the authors and publishers, and they are not covered by MIT.

One of the cited sources is an unpublished student writing assignment
(Utrecht University, 2026). It is quoted with permission; all rights remain
with its author, and it is **not** covered by MIT.

## 6. Posters, cards and print files

The posters, reflection cards and their print files
(`site/etalage/media/posters/`, `site/etalage/media/kaartjes/`,
`site/etalage/media/materiaal/*.pdf`) were designed for the project. They show
recognisable people, first names of people (on the posters and in the poster
print file), AI-generated imagery and third-party logos (Erasmus University
Rotterdam, Utrecht University, opiniON). They are included with permission of
their makers, of the people shown and of the logo owners. They are **not**
covered by MIT or by any other open license: all rights remain with their
makers and rights holders, and reusing them outside this repository requires
their permission.

## 7. Logos and trademarks

Logos are trademarks of their owners and are not licensed under MIT; no
trademark rights are granted by this repository.

- `site/etalage/media/logo/parai.svg`: the Parai logo, trademark of Parai B.V.
- `site/etalage/media/logo/ai-maps.png`, `ai-maps-donker.png`: the AI-MAPS
  logo, cut out of a project banner on eur.nl. Included with permission;
  **not** under MIT.
- The logos of Erasmus University Rotterdam, Utrecht University and opiniON
  inside the posters and cards (§6), included with permission; **not** under
  MIT.

## 8. Fonts

Fustat and a subset of Nanum Myeongjo (renamed "Duif Serif") are licensed under
the SIL Open Font License 1.1. See [`LICENSES/FONTS.md`](LICENSES/FONTS.md) and
the license texts in [`LICENSES/`](LICENSES/).

## 9. Private dependencies

The workspace in `module/` imports private Parai components (the Go modules
`parai-core` and `parai-interaction`, and the frontend packages `@parai/ui` and
`@parai/graph`). They are not part of this repository and are not covered by
its license. A binary or bundle built with them is not an MIT-only artifact.

The knowledge-graph images of the showcase
(`site/etalage/media/beeld/kennisgraaf-*.webp`) are renders of the private graph
component. They are Parai imagery: all rights reserved by Parai B.V., not
covered by MIT. The same holds for the Parai interface in the screen recordings
of the explainer video (§1). Their origin is in
[`tools/etalage-opnames/HERKOMST.md`](tools/etalage-opnames/HERKOMST.md).
