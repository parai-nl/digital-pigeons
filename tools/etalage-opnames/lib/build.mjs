// opnames.json samenstellen uit vragen.json, keuze.json, de gekozen takes en
// facts.json, volgens het contract in README.md. Zuivere logica: build.mjs doet het
// lezen, het omzetten van de stem naar mp3 en het schrijven.
import { CHAT_ROUTES, LANGS, MEETING_PIGEONS, endpointOrigin, modelAllowed } from './config.mjs'
import { choiceProblems, expectedItems } from './choose.mjs'
import { chatTurn, meetingReaction } from './map.mjs'

// compile geeft { opnames, voices, sounds, warnings }: het bestand zelf, de
// stemmen die als mp3 klaargezet moeten worden ({take, dir, file, target}),
// de veldgeluiden waarnaar verwezen wordt, en keuzes voor items die niet meer
// in vragen.json staan (die blijven buiten opnames.json).
export function compile({ vragen, keuze, takesById, facts, bands = {} }) {
  const problems = choiceProblems(keuze, takesById, vragen, { bands })
  if (problems.length) throw new Error(`opnames.json kan niet gebouwd worden:\n  ${problems.join('\n  ')}`)
  const expected = new Set(expectedItems(vragen))
  const warnings = Object.keys(keuze.items).filter((k) => !expected.has(k)).map((k) => `${k}: gekozen, maar niet in vragen.json; blijft buiten opnames.json`)
  const used = []
  const voices = []
  const sounds = new Set()
  const pick = (key) => {
    const take = takesById.get(keuze.items[key].take)
    used.push(take)
    return take
  }

  const questions = vragen.chat.map((q) => {
    const turns = {}
    for (const lang of LANGS) {
      turns[lang] = {}
      for (const route of CHAT_ROUTES) turns[lang][route] = chatTurn(pick(`chat/${q.id}/${lang}/${route}`))
    }
    return { id: q.id, text: { nl: q.text.nl, en: q.text.en }, turns }
  })

  const statements = vragen.meeting.map((s) => {
    const reactions = {}
    for (const lang of LANGS) {
      reactions[lang] = {}
      for (const pigeon of MEETING_PIGEONS) {
        const take = pick(`meeting/${s.id}/${lang}/${pigeon}`)
        const reaction = meetingReaction(take, { bands })
        if (reaction.audio) voices.push({ take: take.id, dir: take.dir, file: take.voice[0].file, target: reaction.audio })
        if (reaction.sound) sounds.add(reaction.sound)
        reactions[lang][pigeon] = reaction
      }
    }
    return { id: s.id, speaker: { nl: s.speaker.nl, en: s.speaker.en }, text: { nl: s.text.nl, en: s.text.en }, reactions }
  })

  const opnames = {
    schema: 1,
    recording: recording(used, vragen),
    facts: { ...facts },
    chat: { questions },
    meeting: { statements },
  }
  return { opnames, voices, sounds: [...sounds].sort(), warnings }
}

// recording: waar en wanneer de gekozen takes zijn opgenomen. Het model komt
// uit de takes (het modelbeleid dat de server meldde), niet uit deze code; de
// bouw weigert als dat niet overal hetzelfde model via één HTTPS-endpoint
// is, of als een take dat model niet mocht gebruiken (modelAllowed: het huidige
// verplichte model, of een eerder model vóór zijn einddatum).
// Het endpoint zelf komt niet in opnames.json; de takes leggen het vast.
// Ook de bouwgegevens van de server (module-tree, core-commit)
// blijven in de takes: de publieke pagina heeft er niets aan.
export function recording(takes, vragen) {
  const models = new Set(takes.map((t) => t.server.modelPolicy.model))
  const endpoints = new Set(takes.map((t) => t.server.modelPolicy.endpoint))
  if (models.size !== 1 || !takes.every((t) => modelAllowed(t.server.modelPolicy.model, t.recordedAt))) throw new Error(`de gekozen takes melden modellen ${[...models].join(', ')}`)
  if (endpoints.size !== 1 || !endpointOrigin([...endpoints][0])) throw new Error(`de gekozen takes melden endpoints ${[...endpoints].join(', ')}`)
  const trees = new Set(takes.map((t) => t.server.module.moduleTree))
  if (trees.size !== 1) throw new Error('de gekozen takes komen uit verschillende versies van module/')
  const kbIds = new Set(takes.map((t) => t.server.knowledge?.kbId))
  if (kbIds.size !== 1 || typeof [...kbIds][0] !== 'string') throw new Error(`de gekozen takes melden kennisbanken ${[...kbIds].join(', ')}`)
  const byTime = takes.slice().sort((a, b) => a.recordedAt.localeCompare(b.recordedAt))
  const commits = [...new Set(byTime.map((t) => t.server.module.commit))]
  const out = {
    model: [...models][0],
    moduleCommit: commits.at(-1),
    knowledgeBase: [...kbIds][0],
    recordedFrom: byTime[0].recordedAt,
    recordedTo: byTime.map((t) => t.finishedAt).sort().at(-1),
    placeholder: vragen.placeholder === true,
  }
  // Buiten het contract: alle module-commits als het er meer zijn, en het
  // aantal takes.
  if (commits.length > 1) out.moduleCommits = commits
  out.takes = takes.length
  return out
}
