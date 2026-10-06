import { FACILITATOR, notifiedStopReason, type ChatMessage, type ComparisonOrigin } from './chatStore.svelte'

export function authorId(message: ChatMessage): string {
  if (message.persona === FACILITATOR) return FACILITATOR
  return message.persona || message.pigeonId || ''
}

// De vulzin die de facilitator een tijd lang onder elk antwoord zette. De
// backend stuurt hem niet meer, maar oude rijen staan in de database en die
// worden nog steeds ingeladen. Een regel die alleen maar zegt dat het antwoord
// hierboven staat, is geen begeleiding: hij verdwijnt.
const GUIDANCE_FILLER = [
  "The pigeon's answer is shown above.",
  'The pigeon’s answer is shown above.',
  'Het antwoord van de duif staat hierboven.',
]

export function isGuidanceFiller(content: string): boolean {
  return GUIDANCE_FILLER.includes(content.trim())
}

export function visibleMessages(messages: ChatMessage[], pigeonId: string): ChatMessage[] {
  return messages.filter((message, index) => {
    if (message.role === 'user') return true
    if (message.persona === FACILITATOR && isGuidanceFiller(message.content)) return false
    if (message.persona !== FACILITATOR || pigeonId !== 'biological') return true
    // Keep historical guidance attributed to its actual author. New bio turns
    // use one progress status and expose guidance only if no pigeon answered,
    // or if stopping/failure needs a visible explanation.
    if (message.streaming) return false
    if (message.partial || (message.stopReason && message.stopReason !== 'natural')) return true
    if (!message.provenance) return message.content.trim() !== ''
    let start = index
    while (start > 0 && messages[start - 1].role !== 'user') start--
    let end = index + 1
    while (end < messages.length && messages[end].role !== 'user') end++
    const hasAnswer = messages.slice(start, end).some((m) => m.kind === 'pigeon_answer' && m.content.trim() !== '')
    return !hasAnswer && message.content.trim() !== ''
  })
}

// In een vergelijkgesprek staat de vraag twee keer: één keer gekopieerd uit
// het brongesprek, met het antwoord van de eerste duif eronder, en daarna nog
// een keer als eigen mensrij, want de tweede duif wordt werkelijk opnieuw
// gevraagd. Beide rijen bestaan echt en blijven in de opslag staan; alleen de
// weergave verandert. Deze functie wijst de tweede rij aan,
// zodat het gesprek leest als "dit gesprek ging naar de andere duif" in plaats
// van als een vraag die de lezer zelf herhaalt.
//
// De grens komt uit `contextMessageIds`: dat noemt de gekopieerde rijen op
// volgorde, dus de rij daarna is de herhaling. Tekst alleen zou te grof zijn —
// in een ketting van vergelijkingen staat dezelfde zin al meermaals in de
// kopie, en wie een vraag oprecht herhaalt hoort gewoon zijn eigen bubbel te
// zien. De tekstvergelijking blijft er wél naast staan: wijkt de rij ook maar
// één teken af van de laatst gekopieerde vraag, dan is het een nieuwe vraag.
export function repeatedQuestionKey(messages: ChatMessage[], comparison: ComparisonOrigin | undefined): string {
  const copied = comparison?.contextMessageIds.length ?? 0
  if (copied === 0 || messages.length <= copied) return ''
  const repeated = messages[copied]
  if (repeated.role !== 'user') return ''
  const asked = messages.slice(0, copied).findLast((m) => m.role === 'user')
  if (!asked || asked.content !== repeated.content) return ''
  return repeated.key
}

export function latestExchange(messages: ChatMessage[]): { question?: ChatMessage; answer?: ChatMessage } {
  let index = messages.length - 1
  while (index >= 0 && messages[index].role !== 'user') index--
  if (index < 0) return {}
  return {
    question: messages[index],
    answer: messages.slice(index + 1).findLast((m) => m.role === 'assistant' && m.persona !== FACILITATOR),
  }
}

// unansweredTurn zegt of de laatste vraag zonder antwoord bleef, en zo ja
// hoe. Een antwoord is elke rij met tekst ná die vraag, behalve een
// afgebroken rij van de facilitator: dat is zijn kaderzin van een beurt die
// stopte voordat de duif iets zei. De server bewaart die kaderzin als
// afgebroken zolang er geen duif-antwoord achter staat (askPigeon in
// agent_tools.go). Zo blijft een gesprek achter waarvan het tabblad sloot
// terwijl de beurt liep, en na herladen las het alsof de duif de vraag
// negeerde. Een half antwoord van de duif telt wél: dat draagt zijn
// eigen markering.
//
// Bekende beperking: na herladen is een afgebroken facilitatorrij niet van
// een kaderzin te onderscheiden. Valt de lezer weg precies terwijl de
// facilitator zijn eigen antwoord schrijft (een profiel, een wedervraag, een
// terugverwijzing), dan bewaart de server ook dát als afgebroken rij, en zegt
// dit gesprek na heropenen "afgebroken voordat er een antwoord kwam". Het
// venster is klein; een eigen markering voor het kader vraagt een migratie
// (de kolom `kind` laat alleen 'text' en 'pigeon_answer' toe).
//
// `stopped` zegt dat er wel een rij staat, maar zonder tekst en met een
// gemelde stopreden (een weigering of het filter van het model): die beurt is
// niet afgebroken maar door het model beëindigd, en de badge zegt al waarom.
// Die stand bestaat alleen in het geheugen, direct na de beurt en alleen op
// de pipeline-motor: de server bewaart geen lege rij, dus na herladen is hij
// weg, en de agent-motor maakt van zo'n einde een `error` plus `done`, waarna
// de lege rij verdwijnt en de foutbanner het woord heeft.
export function unansweredTurn(messages: ChatMessage[]): { question: ChatMessage; stopped: boolean } | undefined {
  const { question } = latestExchange(messages)
  if (!question) return undefined
  const after = messages.slice(messages.indexOf(question) + 1)
  const answered = after.some((m) => m.content.trim() !== '' && !(m.persona === FACILITATOR && m.partial))
  if (answered) return undefined
  return { question, stopped: after.some((m) => notifiedStopReason(m.stopReason) !== undefined) }
}
