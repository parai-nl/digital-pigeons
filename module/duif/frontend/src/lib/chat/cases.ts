import type { Lang } from '../i18n.svelte'
import type { ChatMessage } from './chatStore.svelte'

type Translation = Record<Lang, string>
export interface ConversationCase {
  id: string
  kind: 'question' | 'case'
  title: Translation
  question: Translation
  learningGoal: string
  coverage: string[]
  gapExample: string
}

// Curated prompts are invitations to investigate, never promised answers.
// Stable IDs also identify these cases in the facilitator playbook/evaluation.
// Live corpus verification is recorded separately from this content registry.
export const CONVERSATION_CASES: readonly ConversationCase[] = [
  {
    id: 'square-event', kind: 'case',
    title: { en: 'An event on the square', nl: 'Een evenement op het plein' },
    question: {
      en: 'We are planning a busy event on a Rotterdam square where pigeons live. What should we consider from your perspective, and what can research actually tell us?',
      nl: 'We plannen een druk evenement op een Rotterdams plein waar duiven leven. Waar moeten we vanuit jouw perspectief rekening mee houden, en wat kan onderzoek ons werkelijk vertellen?',
    },
    learningGoal: 'Compare a research translation with a designed standpoint about shared urban space.',
    coverage: ['urban habitat', 'disturbance', 'human–pigeon interaction'],
    gapExample: 'The exact effect of this unnamed event on a specific local flock cannot be established.',
  },
  {
    id: 'nesting-space', kind: 'question',
    title: { en: 'Room for a nest', nl: 'Ruimte voor een nest' },
    question: {
      en: 'What does research tell us about the places pigeons choose to nest in a city?',
      nl: 'Wat vertelt onderzoek ons over de plekken waar duiven in een stad kiezen om te nestelen?',
    },
    learningGoal: 'Practice asking an answerable biological question and inspecting the supporting source.',
    coverage: ['nesting', 'urban habitat'],
    gapExample: 'A particular balcony or local nesting outcome is not guaranteed by general research.',
  },
  {
    id: 'shared-square', kind: 'case',
    title: { en: 'Who is the square for?', nl: 'Van wie is het plein?' },
    question: {
      en: 'A square is being redesigned with less space for pigeons. Which research findings are relevant, and what is your position on the plan? Keep those two things separate.',
      nl: 'Een plein wordt opnieuw ingericht met minder ruimte voor duiven. Welke onderzoeksbevindingen zijn relevant, en wat is jouw positie over het plan? Houd die twee dingen apart.',
    },
    learningGoal: 'Distinguish a documented finding from an attributed, constructed position.',
    coverage: ['urban habitat', 'coexistence', 'human–pigeon interaction'],
    gapExample: 'The sources do not establish a real pigeon’s political preference or the outcome of this plan.',
  },
]

export const GAP_CASE: ConversationCase = {
  id: 'individual-unknown', kind: 'question',
  title: { en: 'Explore a limit', nl: 'Verken een grens' },
  question: {
    en: 'Which exact place will the pigeon outside my window choose to sleep tonight, and can you establish that from the available research?',
    nl: 'Welke exacte plek kiest de duif voor mijn raam vannacht om te slapen, en kun je dat vaststellen uit het beschikbare onderzoek?',
  },
  learningGoal: 'Demonstrate a genuine limit of inference without simulating a technical failure.',
  coverage: [], gapExample: 'An unidentified individual’s exact future choice is unavailable.',
}

export interface Followup { id: string; label: Translation; question: Translation }

// No extra provider call or invented topical recommendation. These bounded
// actions explicitly refer to the actual completed answer and its KB state.
export function followups(answer: ChatMessage | undefined, question: string): Followup[] {
  if (!answer || [...question].length > 3500 || !question.trim() || answer.streaming || answer.partial || !answer.content.trim()
    || (answer.stopReason && answer.stopReason !== 'natural') || answer.kbStatus === 'unavailable') return []
  if (answer.kbStatus === 'gap') return [{
    id: 'limits',
    label: { en: 'Clarify the limit', nl: 'Verhelder de grens' },
    question: {
      en: `What cannot be established from the available sources about my question: “${question}”? Please do not fill the gap with an assumption.`,
      nl: `Wat is met de beschikbare bronnen niet vast te stellen over mijn vraag: “${question}”? Vul het gat niet in met een aanname.`,
    },
  }]
  if (answer.kbStatus !== 'sources' || answer.sources.length === 0) return []
  return [{
    id: 'evidence',
    label: { en: 'Look more closely at the evidence', nl: 'Bekijk de onderbouwing' },
    question: {
      en: `For my question “${question}”, which parts of your previous answer can the available sources support, and where are their limits?`,
      nl: `Welke onderdelen van je vorige antwoord op mijn vraag “${question}” kunnen de beschikbare bronnen onderbouwen, en waar liggen hun grenzen?`,
    },
  }]
}
