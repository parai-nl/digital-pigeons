import { fireEvent, render, screen, within } from '@testing-library/svelte'
import { tick } from 'svelte'
import { describe, expect, it, vi } from 'vitest'
import { UiLocale } from '../i18n.svelte'
import { loadedRoster, pigeonRoute } from '../testing/chat'
import { scriptedFetch } from '../testing/fetch'
import type { ExplainerVideo } from '../media/types'
import { RosterStore } from './roster.svelte'

// De uitlegvideo komt uit het mediapakket; de test zet per geval zelf klaar wat
// er geïnstalleerd is, los van wat er toevallig in public/media staat.
const catalogue = vi.hoisted(() => ({ portraits: {}, headshots: {}, rest: null, motions: [], temporarySound: false, explainer: null as ExplainerVideo | null }))
vi.mock('virtual:duif-media', () => ({ default: catalogue }))
const EXPLAINER: ExplainerVideo = {
  id: 'explainer-video', url: '/modules/duif/media/uitlegvideo.mp4', posterUrl: '/modules/duif/media/uitlegvideo-poster.png',
  captionsUrl: '/modules/duif/media/uitlegvideo-en.vtt', width: 1920, height: 1080, durationMs: 95000, language: 'en', temporary: false,
}

import HelpView from './HelpView.svelte'

describe('Help reading routes', () => {
  // Wie de uitleg opent, zoekt eerst de drie duivenrollen; onder de
  // introductie en de drie stappen vond je ze pas laat. De volgorde is die van
  // die vraag; de weg terug staat onderaan, omdat het sluitkruis van het venster
  // bovenaan blijft staan (HelpDialog).
  it('zet de drie duiven direct onder de kop en houdt de uitgang onderaan', async () => {
    catalogue.explainer = null
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    const onclose = vi.fn()
    const ongraph = vi.fn()
    render(HelpView, { locale, roster, onclose, ongraph })
    await tick()
    const sections = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
    expect(sections[0]).toBe('Three forms, three different encounters')
    expect(sections[1]).toBe('Start together in three steps')
    expect(screen.getByRole('heading', { level: 2 })).toHaveTextContent('Who gets a place at the table?')

    await fireEvent.click(screen.getByRole('button', { name: /Explore the knowledge graph/ }))
    expect(ongraph).toHaveBeenCalledOnce()
    await fireEvent.click(screen.getByRole('button', { name: /Back to the experience/ }))
    expect(onclose).toHaveBeenCalledOnce()
  })

  it('offers the same operational and epistemic help in Dutch without translating source content', async () => {
    const locale = new UiLocale('nl')
    const roster = await loadedRoster(locale)
    render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    expect(screen.getByRole('heading', { name: 'Begin samen in drie stappen' })).toBeInTheDocument()
    // De bediening heet nu "Stop inbreng" en "Vergadering beëindigen";
    // de hulp beschrijft die twee knoppen met dezelfde woorden als het scherm.
    expect(screen.getByText(/Stop inbreng zet de duif en haar lokale geluid meteen stil/)).toBeInTheDocument()
    expect(screen.getByText(/de bewaartermijnen van alle betrokken diensten/)).toBeInTheDocument()
    expect(screen.getByText('Kan ik dit zonder geluid of muis gebruiken?')).toBeInTheDocument()
    // De helppagina verwijst naar de methodesectie en herhaalt hem niet.
    expect(screen.getByText('Waar lees ik hoe de kennisbank is opgebouwd?')).toBeInTheDocument()
    expect(screen.getByText(/op de publieke projectsite/)).toBeInTheDocument()
    // Het adres staat erbij, in plaats van "vraag je begeleider".
    expect(screen.getByText(/pigeon\.parai\.nl\/#methode, onder Onderzoek/)).toBeInTheDocument()
    expect(screen.getByText(/geen volledige notulen/)).toBeInTheDocument()
    locale.choose('en')
    await tick()
    expect(screen.getByRole('heading', { name: 'Start together in three steps' })).toBeInTheDocument()
    expect(screen.getByText(/not an authentic pigeon voice/)).toBeInTheDocument()
    expect(screen.getByText(/retention policies of every service involved/)).toBeInTheDocument()
    expect(screen.getByText('Where can I read how the knowledge base was built?')).toBeInTheDocument()
    expect(screen.getByText(/on the public project site/)).toBeInTheDocument()
    expect(screen.getByText(/pigeon\.parai\.nl\/#methode, under Research/)).toBeInTheDocument()
  })

  // De helppagina draagt de namen uit personas.yaml, niet uit de
  // interfaceteksten. Zolang de lijst onderweg is staat er een laadregel.
  it('noemt de drie duiven bij de naam uit de configuratie, in beide talen', async () => {
    const locale = new UiLocale()
    const pending = new RosterStore(scriptedFetch([pigeonRoute()]).fetch, locale)
    const mounted = render(HelpView, { locale, roster: pending, onclose: vi.fn(), ongraph: vi.fn() })
    expect(screen.getAllByText('Loading the pigeon’s name…')).toHaveLength(3)

    const roster = await loadedRoster(locale)
    await mounted.rerender({ locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    await tick()
    for (const name of ['Knowledge Partner', 'Stakeholder', 'Signaling Pigeon']) {
      expect(screen.getByRole('heading', { name })).toBeInTheDocument()
    }
    locale.choose('nl')
    await tick()
    for (const name of ['Kennispartner', 'Stakeholder', 'Signalerende duif']) {
      expect(screen.getByRole('heading', { name })).toBeInTheDocument()
    }
  })

  // Bovenaan staat de Engelstalige uitlegvideo, met bediening en geluid
  // voor de kijker. Zij speelt niet vanzelf en laadt alleen haar metadata.
  it('opent met de uitlegvideo: bediening, geluid, geen autoplay, Engelse ondertitels', async () => {
    catalogue.explainer = EXPLAINER
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    const sections = screen.getAllByRole('heading', { level: 3 }).map((heading) => heading.textContent)
    expect(sections.slice(0, 2)).toEqual(['See how it works', 'Three forms, three different encounters'])
    const video = screen.getByTestId('help-video').querySelector('video') as HTMLVideoElement
    expect(video.getAttribute('src')).toBe(EXPLAINER.url)
    expect(video.getAttribute('poster')).toBe(EXPLAINER.posterUrl)
    expect(video.controls).toBe(true)
    expect(video.autoplay).toBe(false)
    expect(video.muted).toBe(false)
    expect(video.getAttribute('preload')).toBe('metadata')
    expect(video.hasAttribute('playsinline')).toBe(true)
    const track = video.querySelector('track') as HTMLTrackElement
    expect(track).toMatchObject({ kind: 'captions', srclang: 'en', label: 'English' })
    expect(track.getAttribute('src')).toBe(EXPLAINER.captionsUrl)

    // In het Nederlands blijft de video Engels, en dat staat erbij.
    locale.choose('nl')
    await tick()
    expect(screen.getByRole('heading', { name: 'Kijk hoe het werkt' })).toBeInTheDocument()
    expect(screen.getByText(/De video is Engelstalig/)).toBeInTheDocument()
    expect(track.label).toBe('Engels')
    expect(track.srclang).toBe('en')
  })

  it('laat de track weg zonder ondertitels en de hele sectie zonder video', async () => {
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    catalogue.explainer = { ...EXPLAINER, captionsUrl: null }
    const first = render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    expect(screen.getByTestId('help-video').querySelector('video')).not.toBeNull()
    expect(screen.getByTestId('help-video').querySelector('track')).toBeNull()
    first.unmount()

    catalogue.explainer = null
    render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    expect(screen.queryByTestId('help-video')).not.toBeInTheDocument()
    expect(screen.getAllByRole('heading', { level: 3 })[0]).toHaveTextContent('Three forms, three different encounters')
  })

  // De video voor de stand is 4:3, de placeholder 16:9. Het vlak dat
  // de speler vóór het laden reserveert heeft de verhouding van het pakket, dus
  // geen balken in een vaste 16:9-lijst; ook de stilstaande poster tijdens een
  // vergadering. Afspelen blijft aan de kijker.
  it('reserveert de beeldverhouding van het geïnstalleerde pakket, 4:3 en 16:9', async () => {
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    const props = { locale, roster, onclose: vi.fn(), ongraph: vi.fn() }
    for (const [width, height] of [[1440, 1080], [1920, 1080]]) {
      catalogue.explainer = { ...EXPLAINER, width, height }
      const view = render(HelpView, { ...props, meetingLive: false })
      const video = screen.getByTestId('help-video').querySelector('video') as HTMLVideoElement
      expect(video.style.aspectRatio).toBe(`${width} / ${height}`)
      expect(video.autoplay).toBe(false)
      await view.rerender({ ...props, meetingLive: true })
      const poster = screen.getByTestId('help-video').querySelector('img') as HTMLImageElement
      expect(poster.style.aspectRatio).toBe(`${width} / ${height}`)
      expect(screen.getByTestId('help-video').querySelector('video')).toBeNull()
      view.unmount()
    }
  })

  // Een video die niet laadt, laat geen kapotte speler achter maar zegt eerlijk
  // wat er is gebeurd; de uitleg eronder blijft.
  it('vervangt een video die niet laadt door een rustige melding', async () => {
    catalogue.explainer = EXPLAINER
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    await fireEvent.error(screen.getByTestId('help-video').querySelector('video') as HTMLVideoElement)
    expect(screen.getByRole('status')).toHaveTextContent('The video could not be loaded.')
    expect(screen.getByTestId('help-video').querySelector('video')).toBeNull()
  })

  // Een placeholder krijgt geen inhoud of duur toegeschreven die er nog niet is.
  it('beschrijft een placeholder als placeholder', async () => {
    catalogue.explainer = { ...EXPLAINER, temporary: true }
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    render(HelpView, { locale, roster, onclose: vi.fn(), ongraph: vi.fn() })
    const section = screen.getByTestId('help-video')
    expect(section).toHaveTextContent('what you see now is a placeholder')
    expect(section).not.toHaveTextContent(/a chat, and the same meeting|minute/)
  })

  // Tijdens een vergadering staat de microfoon open onder het venster. De
  // stemmen uit de video mogen dan niet als inbreng de vergadering in gaan: de
  // help biedt geen afspelen aan, en een video die al speelde staat stil.
  it('biedt tijdens een vergadering geen afspelen aan en zet een spelende video stil', async () => {
    catalogue.explainer = EXPLAINER
    const pause = vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
    const locale = new UiLocale()
    const roster = await loadedRoster(locale)
    const props = { locale, roster, onclose: vi.fn(), ongraph: vi.fn() }
    const view = render(HelpView, { ...props, meetingLive: false })
    const video = screen.getByTestId('help-video').querySelector('video') as HTMLVideoElement
    Object.defineProperty(video, 'paused', { configurable: true, value: false })

    await view.rerender({ ...props, meetingLive: true })
    expect(pause.mock.contexts).toContain(video)
    const section = screen.getByTestId('help-video')
    expect(section.querySelector('video')).toBeNull()
    expect(section.querySelector('img')?.getAttribute('src')).toBe(EXPLAINER.posterUrl)
    expect(within(section).getByRole('status')).toHaveTextContent('The video plays once the meeting has ended — the microphone is on.')
    locale.choose('nl')
    await tick()
    expect(within(section).getByRole('status')).toHaveTextContent('De video speelt zodra de vergadering is afgelopen — de microfoon staat aan.')

    await view.rerender({ ...props, meetingLive: false })
    expect(screen.getByTestId('help-video').querySelector('video')).not.toBeNull()
    pause.mockRestore()
  })
})
