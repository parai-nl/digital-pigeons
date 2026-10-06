# Architecture

The repository holds two applications that share pigeons, texts and media but
no runtime: the **public showcase** (`site/`) and the **workspace** (`module/`).
The showcase is fed by recordings made against the workspace.

```
                 tools/etalage-opnames ──records──▶ site/etalage/data/opnames.json
                 tools/etalage-beeld   ──renders──▶ site/etalage/media/duiven/
                         │
  browser ──▶ site/ (static, no model calls)

  browser ──▶ Parai shell ──▶ module (Go, /api/duif/v1) ──▶ Postgres, language-model proxy,
                 │                                          speech-to-text, text-to-speech
                 └──loads──▶ module frontend bundle (Svelte 5)
```

## Public showcase (`site/`)

A static, build-free page of ES modules: `site/index.html` on `/`, everything
else under `site/etalage/`.

- **Data.** `data/opnames.json` holds real pigeon answers and meeting reactions
  recorded against the workspace (text chunks with their timing, sources,
  voice files). `media/duiven/manifest.json` describes the rendered pigeon
  images and clips. `data.js` validates both and drops whatever does not match
  the contract instead of breaking the page.
- **Behaviour.** `carousel.js`, `chat.js`, `meeting.js` and `stage.js` replay
  the recordings; `i18n.js` holds every text in English and Dutch; `prefs.js`
  is the only place that stores anything (language, sound, theme).
- **Configuration.** `config.js`: one line per thing that may change without a
  rebuild (login address, video, posters, cards, downloads).
- **Constraints.** Nothing is loaded from another origin, there is no
  microphone, and the page must run under a strict Content-Security-Policy.
  `scripts/check-etalage.mjs` and the Playwright tests in `tests/etalage/`
  enforce this; `tools/static-server.mjs` serves the page with the hosting
  headers.

## Workspace (`module/`)

A Go module that runs inside the Parai platform. The platform provides the
shell, authentication, the database pool, the language-model client, the
knowledge-graph storage and the audio providers; this repository only uses
them through their public Go and frontend interfaces. Without the private Parai
components the workspace does not build.

### Backend (`module/duif`)

- **Registration.** `module.go` registers the module with the platform: its
  routes under `/api/duif/v1`, its database migrations (`duif/migrations`), its
  embedded frontend bundle and media, and its persona configuration.
- **Personas.** `duif/config/personas.yaml` defines the three pigeons (topics,
  speaking threshold per pigeon, voice speed, knowledge-base use) and the
  facilitator; their prompts are in `duif/config/prompts/`.
- **Language-model policy.** `model_policy.go` and `provider_limits.go`: one
  fixed model alias for every call, an HTTPS proxy configured by the
  deployment, bounded requests, and a short process-wide pause after the
  proxy rejects a request on budget.
- **Knowledge base.** `kb_*.go` import a `duiven/2` or bilingual `duiven/3`
  bundle into the platform knowledge graph (format in
  [`module/README.md`](module/README.md)); `retrieval.go` and
  `source_grounding.go` select sources and their verified passages;
  `graph*.go` serve the knowledge-graph read routes.
- **Chat.** `agent_turn.go` and `agent_tools.go` run a facilitator agent that
  asks the chosen pigeon (`ask_pigeon`); the pigeon answers in one streaming
  call (`answer_stream.go`) with her own history and persona prompt. The turn
  is streamed to the client as server-sent events and stored per pigeon
  (`repo.go`, `postgres.go`). `comparison.go` copies a fixed context into a new
  conversation with the other speaking pigeon.
- **Meeting.** `meeting*.go`: microphone audio goes through streaming
  speech-to-text into a transient transcript window; a detector decides per
  pigeon whether to speak (`meeting_detect.go`, `meeting_candidate.go`); a
  speaking pigeon answers with text and synthesized speech, the signalling
  pigeon with a field recording and a gesture. Meeting content is kept in
  memory only.
- **Media.** `media_manifest.go` and `duif/config/media-manifest.v1.json`
  describe every image, model, sound and video with hash, rights and
  attribution; the build refuses files that do not match.

### Frontend (`module/duif/frontend`)

Svelte 5 and TypeScript, built with Vite into one module bundle that the Parai
shell loads. `src/lib/chat`, `src/lib/meeting`, `src/lib/graph` and
`src/lib/media` hold the chat, the meeting stage (with the three.js pigeon),
the knowledge graph and the media catalogue. `media-build.ts` validates the
media manifest at build time; `scripts/check-media.ts` probes the files.

## Invariants

These hold across both applications and are covered by tests:

- each pigeon has its own conversations; comparison takes only an explicitly
  selected context and keeps who said what;
- factual claims are grounded in numbered sources;
- the signalling pigeon gets no human meaning labels;
- meeting content stays transient;
- microphone and playback state shown to the user is the real state, and a
  local stop always works.

## Tools

The folders in `tools/` produce what the showcase and the workspace ship:
recordings (`etalage-opnames`), pigeon renders (`etalage-beeld`), the 3D model
and portraits (`design-model`, `etalage-site`), the explainer video
(`uitlegvideo`) and the selected media package (`prepare-meeting-media.mjs`).
The recording, rendering and video tools have their own README; the model and
portrait scripts describe their usage in their header.
