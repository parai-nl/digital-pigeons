import { cleanup, render } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import type { ModelAsset, PigeonIdentity, PigeonView, PublicMedia } from './types'

const engine = vi.hoisted(() => ({
  load: vi.fn((_url: string, _ready: (reactive: boolean) => void, _failed: () => void, _view: PigeonView) => vi.fn()),
  beginReaction: vi.fn(() => ({ duration: 3.2 })),
  pose: vi.fn(), restore: vi.fn(), render: vi.fn(), dispose: vi.fn(),
}))
vi.mock('./model/pigeonStage', () => ({ createPigeonStage: () => engine }))
import MeetingPigeon from './MeetingPigeon.svelte'

const VIEWS: Record<PigeonIdentity, PigeonView> = {
  biological: { azimuth: -Math.PI / 2, elevation: .1, fov: 32 },
  individualist: { azimuth: -1.8416, elevation: .13, fov: 32 },
  nonverbal: { azimuth: -.57, elevation: .105, fov: 32 },
}
const modelOf = (pigeon: PigeonIdentity): ModelAsset => ({
  id: `v4-${pigeon}`, url: `/modules/duif/media/stadsduif-v4-${pigeon}.glb`, posterUrl: `/modules/duif/media/stadsduif-v4-${pigeon}-rust.png`, view: VIEWS[pigeon],
  attribution: { title: 'Pigeon', creator: 'Luc Viatour', source: 'https://lucnix.be', license: 'CC BY-SA 3.0', licenseUrl: 'https://creativecommons.org/licenses/by-sa/3.0/', changes: 'Derived model.' },
})
const media: PublicMedia = {
  portraits: {}, headshots: {}, rest: null, motions: [], temporarySound: false,
  models: { biological: modelOf('biological'), individualist: modelOf('individualist'), nonverbal: modelOf('nonverbal') },
}
const settle = async () => { await vi.dynamicImportSettled(); flushSync(); await Promise.resolve(); flushSync() }
const setReady = async () => { engine.load.mock.calls.at(-1)![1](true); await settle() }
let preferenceChanged: () => void
let query: MediaQueryList
beforeEach(() => {
  vi.useFakeTimers(); vi.clearAllMocks()
  query = { matches: false, addEventListener: (_event: string, listener: () => void) => { preferenceChanged = listener }, removeEventListener: vi.fn() } as unknown as MediaQueryList
  vi.spyOn(window, 'matchMedia').mockReturnValue(query)
})
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })

it('shows the pigeon\'s own still while loading and loads her own model in her own view', async () => {
  const view = render(MeetingPigeon, { pigeon: 'biological', media }); await settle()
  const poster = view.container.querySelector('img.model-poster')
  expect(poster).toHaveAttribute('src', media.models!.biological!.posterUrl)
  expect(engine.load).toHaveBeenCalledExactlyOnceWith(media.models!.biological!.url, expect.any(Function), expect.any(Function), VIEWS.biological)
  await setReady()
  expect(view.container.querySelector('.pigeon-canvas')).toHaveAttribute('data-model-state', 'rest')
  // De poster vloeit weg onder het live model in plaats van te verdwijnen.
  expect(view.container.querySelector('img.model-poster')).toHaveClass('faded')
  expect(engine.beginReaction).not.toHaveBeenCalled()
})

it('switching pigeons swaps to the other pigeon\'s model, still and view', async () => {
  const view = render(MeetingPigeon, { pigeon: 'biological', media }); await settle(); await setReady()
  await view.rerender({ pigeon: 'individualist' }); await settle()
  expect(engine.dispose).toHaveBeenCalledTimes(1)
  expect(engine.load).toHaveBeenLastCalledWith(media.models!.individualist!.url, expect.any(Function), expect.any(Function), VIEWS.individualist)
  const poster = view.container.querySelector('img.model-poster')
  expect(poster).toHaveAttribute('src', media.models!.individualist!.posterUrl)
  // Naar rechts kijken is een andere kijkhoek, geen spiegeling.
  expect(poster).not.toHaveClass('mirror')
  expect(poster).not.toHaveClass('faded')
})

it('a pigeon without a model keeps her portrait and never borrows another pigeon\'s model', async () => {
  const portrait = { id: 'portrait-individualist', url: '/modules/duif/media/portret.webp', width: 512, height: 512, focus: [.5, .5] as [number, number] }
  const partial: PublicMedia = { ...media, portraits: { individualist: portrait }, models: { biological: media.models!.biological } }
  const view = render(MeetingPigeon, { pigeon: 'individualist', media: partial }); await settle()
  expect(engine.load).not.toHaveBeenCalled()
  expect(view.container.querySelector('.pigeon-canvas')).toHaveAttribute('data-model-state', 'unconfigured')
  expect(view.container.querySelector('img')).toHaveAttribute('src', portrait.url)
  expect(view.queryByTestId('media-credit')).not.toBeInTheDocument()
})

it('plays one bounded gesture per event even when the shorter audio signal clears first', async () => {
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media }); await settle(); await setReady()
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  expect(engine.beginReaction).toHaveBeenCalledExactlyOnceWith('auto')
  await view.rerender({ reactionId: null }); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
  vi.advanceTimersByTime(3200); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  expect(engine.beginReaction).toHaveBeenCalledTimes(1)
  await view.rerender({ reactionId: 'signal:2' }); await settle()
  expect(engine.beginReaction).toHaveBeenCalledTimes(2)
})

it('local stop cancels retained answers and late loading cannot replay a consumed event', async () => {
  const view = render(MeetingPigeon, { pigeon: 'individualist', media }); await settle()
  await view.rerender({ reactionId: 'answer:1' }); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
  await view.rerender({ stopRevision: 1 }); await settle(); await setReady()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  await view.rerender({ reactionId: 'answer:2' }); await settle()
  expect(engine.beginReaction).toHaveBeenCalledTimes(1)
  await view.rerender({ stopRevision: 2 }); await settle()
  vi.advanceTimersByTime(10000); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  expect(vi.getTimerCount()).toBe(0)
  expect(engine.dispose).not.toHaveBeenCalled()
  expect(view.container.querySelector('.pigeon-canvas')).toHaveAttribute('data-model-state', 'rest')
})

it('a hidden meeting frees the renderer and does not replay events on returning', async () => {
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media, enabled: false }); await settle()
  expect(engine.load).not.toHaveBeenCalled()
  await view.rerender({ enabled: true }); await settle(); await setReady()
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  await view.rerender({ enabled: false }); await settle()
  expect(engine.dispose).toHaveBeenCalledTimes(1)
  await view.rerender({ reactionId: 'signal:2' }); await settle()
  await view.rerender({ enabled: true }); await settle(); await setReady()
  expect(engine.beginReaction).toHaveBeenCalledTimes(1)
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
})

it('hiding the browser cancels immediately without replay on visibility restoration', async () => {
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media }); await settle(); await setReady()
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
  document.dispatchEvent(new Event('visibilitychange')); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  hidden.mockReturnValue(false); document.dispatchEvent(new Event('visibilitychange')); await settle()
  expect(engine.beginReaction).toHaveBeenCalledTimes(1)
})

it('reduced motion keeps a neutral finite cue and preference changes cannot restart it', async () => {
  Object.defineProperty(query, 'matches', { value: true, configurable: true })
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media, lang: 'nl' }); await settle(); await setReady()
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  expect(view.getByRole('status')).toHaveTextContent('Er wordt een signaal getoond.')
  vi.advanceTimersByTime(1600); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  Object.defineProperty(query, 'matches', { value: false }); preferenceChanged(); await settle()
  expect(engine.beginReaction).not.toHaveBeenCalled()
})

it('keeps the poster and a finite signal when WebGL or the model is unavailable', async () => {
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media }); await settle()
  engine.load.mock.calls[0][2](); await settle()
  expect(view.getByTestId('model-unavailable')).toHaveTextContent('The meeting continues.')
  expect(view.container.querySelector('img')).toHaveAttribute('src', media.models!.nonverbal!.posterUrl)
  expect(view.container.querySelector('img')).not.toHaveClass('faded')
  await view.rerender({ reactionId: 'signal:1' }); await settle()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  vi.advanceTimersByTime(1600); await settle()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
})

it('does not replay a retained answer on remount, and unmount cancels all work', async () => {
  const view = render(MeetingPigeon, { pigeon: 'biological', media, reactionId: 'answer:old' }); await settle(); await setReady()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  await view.rerender({ reactionId: 'answer:new' }); await settle()
  view.unmount()
  expect(engine.dispose).toHaveBeenCalledTimes(1)
  expect(vi.getTimerCount()).toBe(0)
})

it('labels a missing local package honestly and exposes the actual recording source', async () => {
  const fallback = render(MeetingPigeon, { pigeon: 'nonverbal', media: { portraits: {}, headshots: {}, rest: null, motions: [], temporarySound: true } }); await settle()
  expect(fallback.getByTestId('media-temporary')).toHaveTextContent('synthesized sample sound')
  expect(engine.load).not.toHaveBeenCalled()
  fallback.unmount()
  const recording = (band: string, id: string, source: string) => ({ id, url: `/modules/duif/media/${id}.mp3`, durationMs: 4529, attribution: { title: `Pigeon recording ${id}`, creator: 'Field recordist', source, license: 'CC0', licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/', changes: `Trimmed (${band}).` } })
  const native: PublicMedia = { ...media, signalSounds: { calm: recording('calm', 'coo', 'https://example.org/recording'), insistent: recording('insistent', 'coo-urgent', 'https://example.org/recording-urgent'), alarm: recording('alarm', 'loft', 'https://example.org/recording-loft') } }
  const view = render(MeetingPigeon, { pigeon: 'nonverbal', media: native }); await settle()
  // Elke gebruikte opname wordt één keer vermeld, met haar eigen bron.
  expect(view.getAllByText('Sound recording').map(node => node.closest('a')?.getAttribute('href')))
    .toEqual(['https://example.org/recording', 'https://example.org/recording-urgent', 'https://example.org/recording-loft'])
  expect(view.queryByTestId('media-temporary')).not.toBeInTheDocument()
})

it('Stop dominates a newly arriving contribution in the same render without unloading the scene', async () => {
  const view = render(MeetingPigeon, { pigeon: 'individualist', media }); await settle(); await setReady()
  await view.rerender({ reactionId: 'answer:1', stopRevision: 1, paused: true }); await settle()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  expect(engine.dispose).not.toHaveBeenCalled()
  expect(view.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  await view.rerender({ paused: false }); await settle()
  expect(engine.beginReaction).not.toHaveBeenCalled()
  // beginTurn also cancels the preceding media generation. It must still allow
  // a fresh, accepted contribution to start in that same render.
  await view.rerender({ reactionId: 'answer:2', stopRevision: 2 }); await settle()
  expect(engine.beginReaction).toHaveBeenCalledTimes(1)
})
