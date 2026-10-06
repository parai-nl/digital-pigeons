import { fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { serviceContext, testServices, SOURCE } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import SourceDrawer from '../sources/SourceDrawer.svelte'
import MeetingAnswer from './MeetingAnswer.svelte'
import type { AnswerLine } from './meetingStore.svelte'

// De vergadering gebruikt dezelfde bronweergave als de chat: verwijzingen als
// kleine klikbare markeringen in de zin, daaronder de chip "n bronnen" (die de
// gedeelde lade opent) en het label over de brondekking.

const CITED = {
  ...SOURCE,
  grounding: 'corpus_grounded' as const,
  ref: { paper: 'Synthetic title', citation: 'Synthetic Author (1999). Synthetic title.', doi: '10.1000/synthetic', citationConfidence: 'medium' as const },
}

function view(lang: 'en' | 'nl' = 'en', sources = [CITED], patch: Partial<AnswerLine> = {}, prominent = false) {
  const services = testServices(scriptedFetch([]).fetch)
  services.locale.choose(lang)
  const context = serviceContext(services)
  render(MeetingAnswer, {
    props: {
      locale: services.locale,
      prominent,
      answer: { id: 1, pigeonId: 'biological', text: 'Synthetic answer [1].', note: '', status: 'complete', kbStatus: 'sources', sources, startedAt: 0, ...patch },
    },
    context,
  })
  render(SourceDrawer, { context })
  return { services }
}

describe('MeetingAnswer sources', () => {
  for (const lang of ['en', 'nl'] as const) {
    it(`opens the shared drawer with the original reference in ${lang}`, async () => {
      const { services } = view(lang)
      expect(screen.getByTestId('source-count')).toHaveTextContent(lang === 'nl' ? '1 bron' : '1 source')
      await fireEvent.click(screen.getByTestId('source-count'))
      await tick()
      expect(services.sources.open).toBe(true)
      expect(screen.getByTestId('source-citation')).toHaveTextContent('Synthetic Author (1999). Synthetic title.')
      expect(screen.getByTestId('source-doi')).toHaveTextContent('10.1000/synthetic')
      expect(screen.getByTestId('source-drawer').querySelector('a[href]')).toBeNull()
      expect(screen.getByTestId('source-bibliography-note')).toHaveTextContent(lang === 'nl' ? 'bibliografische gegevens' : 'bibliographic details')
      expect(screen.queryByTestId('source-unverified')).toBeNull()
      // De ruwe "[1]" staat niet meer in de zin: daar staat de markering.
      expect(screen.getByTestId('answer-text')).toHaveTextContent('Synthetic answer1.')
      expect(screen.getByTestId('answer-text').textContent).not.toContain('[1]')
    })
  }

  it('renders the citation as a clickable mark that opens the drawer at that source', async () => {
    const second = { ...CITED, number: 2, text: 'Second supporting source' }
    const { services } = view('en', [CITED, second], { text: 'One [1]. Two [2].' })
    const marks = screen.getAllByRole('button', { name: /^Source \d$/ })
    expect(marks.map((mark) => mark.textContent)).toEqual(['1', '2'])
    expect(marks[1]).toHaveAttribute('aria-expanded', 'false')
    await fireEvent.click(marks[1])
    await tick()
    expect(services.sources.open).toBe(true)
    expect(screen.getByTestId('source-drawer')).toHaveTextContent('Second supporting source')
    expect(marks[1]).toHaveAttribute('aria-expanded', 'true')
    // De lade weet welke bron zij toont, dus de andere markering licht niet op.
    expect(marks[0]).toHaveAttribute('aria-expanded', 'false')
  })

  it('holds a half-received citation back while streaming', () => {
    view('en', [CITED], { text: 'Synthetic answer [1', status: 'streaming' })
    // De ruwe markup flitst niet door de zin; de cursor loopt mee.
    expect(screen.getByTestId('answer-text').textContent).toBe('Synthetic answer▍')
  })

  it('darkens the screen behind the drawer when no meeting is running', async () => {
    view()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-backdrop')).toBeInTheDocument()
    expect(screen.getByTestId('source-drawer')).toHaveAttribute('aria-modal', 'true')
  })

  it('leaves the running meeting reachable behind the drawer, so STOP stays one click away', async () => {
    const { services } = view()
    services.meeting.live = true
    await tick()
    await fireEvent.click(screen.getByTestId('source-count'))
    await tick()
    expect(screen.getByTestId('source-drawer')).toBeInTheDocument()
    expect(screen.queryByTestId('source-backdrop')).toBeNull()
    expect(screen.getByTestId('source-drawer')).toHaveAttribute('aria-modal', 'false')
    // Escape blijft de uitweg, ook zonder verduistering.
    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('source-drawer')).toBeNull()
  })
})


describe('MeetingAnswer provider close notices', () => {
  for (const lang of ['en', 'nl'] as const) {
    for (const stopReason of ['truncated', 'filtered'] as const) {
      it(`shows ${stopReason} beside the unchanged answer in ${lang}`, () => {
        const { services } = view(lang, [CITED], { stopReason })
        expect(screen.getByTestId('answer-stop-reason')).toHaveTextContent(services.locale.t(`chat.stopped.${stopReason}`))
        expect(screen.getByTestId('answer-text')).toHaveTextContent('Synthetic answer1.')
        expect(screen.getByTestId('source-count')).toBeInTheDocument()
      })
    }
    it(`does not say it is preparing after an empty filtered answer in ${lang}`, () => {
      const { services } = view(lang, [], { text: '', stopReason: 'filtered' })
      expect(screen.getByTestId('answer-stop-reason')).toHaveTextContent(services.locale.t('chat.stopped.filtered'))
      expect(screen.queryByText(services.locale.t('meeting.answer.preparing'))).toBeNull()
      expect(screen.queryByTestId('answer-text')).toBeNull()
    })
  }
  it('keeps a normal completed answer free of provider notices', () => {
    view()
    expect(screen.queryByTestId('answer-stop-reason')).toBeNull()
  })
})


// De kaart sluit af met de tekst, niet met de stem. Zolang er tekst
// binnenkomt staat er "bijdrage voorbereiden" met de cursor; is de tekst af en
// klinkt de stem nog, dan "spreekt"; daarna draagt de bijdrage geen stand meer.
describe('MeetingAnswer completion', () => {
  // De drie standen dragen elk hun eigen woord, en dat woord moet in allebei
  // de talen kloppen: het Engelse "Speaking" hoort nooit in een Nederlandse
  // vergadering te staan, en andersom.
  const PHASE = {
    en: { writing: 'Preparing a contribution', speaking: 'Speaking' },
    nl: { writing: 'Bijdrage voorbereiden', speaking: 'Spreekt' },
  } as const

  for (const lang of ['en', 'nl'] as const) {
    it(`shows the writing phase with a cursor in ${lang}`, async () => {
      const { services } = view(lang, [CITED], { status: 'streaming' }, true)
      const card = screen.getByTestId('latest-answer')
      expect(services.locale.t('meeting.answer.writing')).toBe(PHASE[lang].writing)
      expect(card).toHaveTextContent(PHASE[lang].writing)
      expect(screen.getByTestId('answer-text').textContent).toContain('▍')
    })

    it(`drops the cursor and says the pigeon is speaking once the text is complete in ${lang}`, async () => {
      const { services } = view(lang, [CITED], { status: 'complete' }, true)
      services.meeting.speaking = true
      await tick()
      expect(services.locale.t('meeting.answer.speaking')).toBe(PHASE[lang].speaking)
      expect(screen.getByTestId('answer-text').textContent).not.toContain('▍')
      expect(screen.getByTestId('latest-answer')).toHaveTextContent(PHASE[lang].speaking)
      expect(screen.getByTestId('latest-answer')).not.toHaveTextContent(PHASE[lang].writing)
    })

    it(`carries no phase label once the voice has finished in ${lang}`, async () => {
      const { services } = view(lang, [CITED], { status: 'complete' }, true)
      services.meeting.speaking = false
      await tick()
      const card = screen.getByTestId('latest-answer')
      expect(card).not.toHaveTextContent(PHASE[lang].speaking)
      expect(card).not.toHaveTextContent(PHASE[lang].writing)
    })
  }

  // De modelcall achter een bijdrage duurt onvoorspelbaar lang. Zolang
  // er nog geen letter is, telt de stand mee hoe lang het al duurt: een kopje
  // dat na veertig seconden nog precies hetzelfde zegt, leest als een
  // vastgelopen scherm.
  it('counts the elapsed time into the preparing label once the wait gets long', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const started = Date.now()
      view('nl', [CITED], { status: 'streaming', text: '', startedAt: started }, true)
      await tick()
      expect(screen.getByTestId('answer-status')).toHaveTextContent('Bijdrage voorbereiden')
      expect(screen.getByTestId('answer-status')).not.toHaveTextContent('s')

      await vi.advanceTimersByTimeAsync(5_000)
      await tick()
      expect(screen.getByTestId('answer-status').textContent).toBe('Bijdrage voorbereiden')

      await vi.advanceTimersByTimeAsync(18_000)
      await tick()
      expect(screen.getByTestId('answer-status').textContent).toBe('Bijdrage voorbereiden 23 s')
    } finally {
      vi.useRealTimers()
    }
  })

  it('drops the counter as soon as the first words arrive', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    try {
      const started = Date.now()
      view('en', [CITED], { status: 'streaming', text: 'The square [1]', startedAt: started }, true)
      await vi.advanceTimersByTimeAsync(23_000)
      await tick()
      expect(screen.getByTestId('answer-status').textContent).toBe('Preparing a contribution')
    } finally {
      vi.useRealTimers()
    }
  })

  // Een oudere bijdrage in het sessieverloop mag nooit "spreekt" zeggen: daar
  // hoort de stand van de lopende beurt niet thuis.
  it('leaves a filed answer without a phase label while the pigeon speaks again', async () => {
    const { services } = view('en', [CITED], { status: 'complete' })
    services.meeting.speaking = true
    await tick()
    expect(screen.getByTestId('past-answer')).not.toHaveTextContent('Speaking')
  })
})
