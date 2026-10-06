# Digital Pigeon of Rotterdam — AI-MAPS × Parai

*Nederlandse samenvatting: zie [onderaan](#nederlands).*

What would it mean to let a pigeon take part in a decision about the city? This
repository contains a research prototype that explores that question with three
designed pigeons:

- the **Knowledge Partner** translates available research about city pigeons
  and answers only with sources;
- the **Stakeholder** combines that research with a recognisably designed
  standpoint of its own;
- the **Signalling pigeon** takes part through movement and sound only, without
  any human translation of what it "means".

The two speaking pigeons can be consulted in a chat and invited to a meeting;
the signalling pigeon only joins meetings. The prototype was made for
[AI-MAPS](https://www.eur.nl/en/research/research-groups-initiatives/artificial-intelligence-multi-agency-public-safety)
(Erasmus University Rotterdam) and built by [Parai](https://parai.nl). The
interface is available in English (default) and Dutch.

This README and `ARCHITECTURE.md` are in English; the code comments, the
workspace and tool READMEs and the Makefile are mostly in Dutch, the working
language of the project.

## Two environments

| | Public showcase | Workspace |
|---|---|---|
| Where | [`site/`](site/) | [`module/`](module/README.md) |
| What | A static page with the three pigeons, a chat, a mini meeting, the research and the method. It makes no model calls: it plays back real answers and meeting reactions recorded earlier, with their sources, voice and coos. | The full application: separate chats per pigeon, explicit comparison between pigeons, transient meetings with a live microphone, source references and a knowledge graph. |
| Runs | Anywhere, with Node.js only. Live at <https://pigeon.parai.nl>. | Only inside the Parai platform. |
| Builds outside Parai | **Yes** | **No** — it imports private Parai components (`parai-core`, `parai-interaction`, `@parai/ui`, `@parai/graph`) that are not public. The code is here to read; see [`module/README.md`](module/README.md). |

[`ARCHITECTURE.md`](ARCHITECTURE.md) describes how the parts fit together.

## Running the showcase locally

Requirements: Node.js 20.11 or newer (the tools use `import.meta.dirname`). There is no build step; the page uses ES
modules and must be served over HTTP.

```sh
node tools/static-server.mjs 8770 site
```

Open <http://127.0.0.1:8770>. With `npm run serve` the server also sends the
same Content-Security-Policy and Permissions-Policy headers as the hosting.
Settings that change without a rebuild (login address, video, posters, cards,
downloads) are one line each in [`site/etalage/config.js`](site/etalage/config.js).

### Tests

```sh
npm ci
npm run test:e2e:install   # once: the Chromium build of Playwright
npm test
```

This checks the rules for `site/` (nothing loaded from another origin, no
microphone, storage only for language, sound and theme, only allowed file
types), the Dutch/English parity of the texts, and the real browser flows of
the showcase under the hosting CSP. It
says nothing about the workspace, model quality or microphone behaviour.

## Repository layout

| Path | Contents |
|---|---|
| `site/` | The public showcase: `index.html` on `/`, everything else under `site/etalage/` |
| `tests/etalage/`, `scripts/check-etalage.mjs` | Browser tests and static checks of the showcase |
| `module/` | The workspace: Go module (`module/duif`) and Svelte 5 frontend (`module/duif/frontend`) |
| `tools/etalage-opnames/` | Records the real pigeon answers for the showcase against a running workspace and builds `site/etalage/data/opnames.json` |
| `tools/etalage-beeld/` | Renders the pigeon images and clips of the showcase from the 3D model (Blender) |
| `tools/uitlegvideo/` | Records and edits the explainer video from recorded sessions |
| `tools/design-model/`, `tools/etalage-site/` | Builds the 3D pigeon model and derived portraits |
| `LICENSE`, `NOTICE.md`, `LICENSES/` | Licensing (see below) |

The recording and rendering tools need a running workspace, Blender or
`ffmpeg`. `etalage-opnames`, `etalage-beeld` and `uitlegvideo` have their own
README; the scripts in `design-model`, `design-model-assets` and `etalage-site`
describe their usage in their header.

## Language model

Every language-model call of the workspace (chat, facilitator, meeting
detection) uses exactly one model alias, `vertex/gemini-3.6-flash-eu`, through
an OpenAI-compatible proxy whose HTTPS address is configured with
`PARAI_LLM_BASE_URL`. A different model makes the module refuse to start. The
recordings on the showcase list the model they were made with.

## License

The source code is released under the [MIT license](LICENSE), copyright
Parai B.V. Not everything in this repository is covered by it:

- the 3D pigeon and everything rendered from it, including the picture of the
  explainer video, is licensed under **CC BY-SA 3.0** (photo texture by Luc
  Viatour / Lucnix.be);
- fonts are under the SIL Open Font License ([`LICENSES/`](LICENSES/));
- background photos and field recordings have their own open licenses;
- synthetic voices (including the soundtrack of the explainer video),
  quotations from research, posters and cards, and third-party logos are
  included with permission; they are not covered by MIT or any other open
  license, and reusing them outside this repository requires permission of
  their rights holders.

The prompts and personas of the pigeons are project content and are covered by
MIT, like the source code.

[`NOTICE.md`](NOTICE.md) lists every exception.

## Nederlands

De Digitale Duif van Rotterdam is een onderzoeksprototype van AI-MAPS (Erasmus
Universiteit Rotterdam), gebouwd door Parai. Drie ontworpen duiven doen mee aan
een gedeeld besluit: de kennispartner vertaalt onderzoekskennis met bronnen, de
stakeholder combineert die kennis met een ontworpen standpunt, en de
signalerende duif doet mee met beweging en geluid, zonder menselijke vertaling.

- **De publieke etalage** (`site/`) draait overal met alleen Node.js en doet
  zelf geen modelaanroepen: `node tools/static-server.mjs 8770 site`, testen
  met `npm ci && npm test`.
- **De werkruimte** (`module/`) draait alleen binnen het Parai-platform, omdat
  zij privé Parai-componenten importeert. De code staat hier om te lezen; buiten
  Parai bouwt zij niet.

De broncode valt onder MIT (Parai B.V.), net als de prompts en persona's. Het
3D-model en alles wat ervan is afgeleid valt onder CC BY-SA 3.0. Stemmen,
posters en kaartjes, logo's van derden en citaten staan hier met toestemming,
maar vallen niet onder een open licentie; voor alle uitzonderingen zie
[`NOTICE.md`](NOTICE.md).
