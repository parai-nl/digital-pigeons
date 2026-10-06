import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import SignalMedia from './SignalMedia.svelte'
import PigeonPortrait from '../shell/PigeonPortrait.svelte'
import type { PublicMedia } from './types'
const media: PublicMedia = {
  portraits: {}, headshots: {}, rest: { id: 'rest', url: '/modules/duif/media/rest.webp', width: 1000, height: 1000, focus: [.5, .5] },
  motions: [{ id: 'motion', url: '/modules/duif/media/movement.mp4', width: 1000, height: 1000, focus: [.5, .5], durationMs: 1100 }], temporarySound: false,
}
const signal = (id = 'event-1') => ({ id, durationMs: 1500 })
const settle = async () => { await Promise.resolve(); flushSync(); await Promise.resolve(); flushSync() }
beforeEach(() => {
  vi.useFakeTimers()
  vi.spyOn(HTMLMediaElement.prototype, 'play').mockResolvedValue()
  vi.spyOn(HTMLMediaElement.prototype, 'pause').mockImplementation(() => {})
})
afterEach(() => { cleanup(); vi.useRealTimers(); vi.restoreAllMocks() })
describe('one admitted visual event', () => {
  it('plays a muted clip once and restores exactly the configured rest image', async () => {
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media })
    await settle()
    const video = result.container.querySelector('video')!
    expect(video.play).toHaveBeenCalledOnce(); expect(video.muted).toBe(true); expect(video.loop).toBe(false)
    expect(video).toHaveAttribute('poster', media.rest!.url)
    await fireEvent.ended(video); await settle()
    expect(result.container.querySelector('video')).toBeNull()
    expect(result.container.querySelector('img')).toHaveAttribute('src', media.rest!.url)
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
    await result.rerender({ pigeon: 'nonverbal', signal: null, media }); await result.rerender({ pigeon: 'nonverbal', signal: signal(), media }); await settle()
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce()
  })
  it('waits for audio playback status before starting the video', async () => {
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), playback: 'pending', media })
    await settle(); expect(HTMLMediaElement.prototype.play).not.toHaveBeenCalled()
    await result.rerender({ pigeon: 'nonverbal', signal: signal(), playback: 'playing', media })
    await settle()
    // A repeated event object keeps the already admitted run, with one play.
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce()
  })
  it('begins the existing admitted event when pending changes to playing', async () => {
    const event = signal(); const result = render(SignalMedia, { pigeon: 'nonverbal', signal: event, playback: 'pending', media })
    await settle(); await result.rerender({ pigeon: 'nonverbal', signal: event, playback: 'playing', media }); await settle()
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledOnce()
  })
  it('keeps a visual fallback when audio failed and a video decode rejects', async () => {
    vi.mocked(HTMLMediaElement.prototype.play).mockRejectedValue(new Error('decode'))
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), playback: 'failed', media })
    await settle(); expect(screen.getByTestId('media-failure')).toHaveTextContent('Movement could not be shown')
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
    expect(result.container.querySelector('video')).toBeNull()
    vi.advanceTimersByTime(1500); await settle(); expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  })
  it('Stop prevents a late play promise from revealing or continuing the clip', async () => {
    let resolve!: () => void
    vi.mocked(HTMLMediaElement.prototype.play).mockImplementation(() => new Promise<void>(done => resolve = done))
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media }); await settle()
    const old = result.container.querySelector('video')!
    await result.rerender({ pigeon: 'nonverbal', signal: null, media }); resolve(); await settle()
    expect(old.pause).toHaveBeenCalled(); expect(result.container.querySelector('video')).toBeNull()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  })
  it('a newer event cannot be stopped by a late callback from the previous clip', async () => {
    let resolveOld!: () => void
    vi.mocked(HTMLMediaElement.prototype.play).mockImplementationOnce(() => new Promise<void>(done => resolveOld = done)).mockResolvedValue()
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal('old'), media }); await settle()
    await result.rerender({ pigeon: 'nonverbal', signal: signal('new'), media }); await settle(); resolveOld(); await settle()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
    expect(result.container.querySelector('video')).toHaveClass('showing')
    expect(HTMLMediaElement.prototype.play).toHaveBeenCalledTimes(2)
  })
  it('unmount cancels the media and its watchdog', async () => {
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media }); await settle()
    result.unmount(); expect(HTMLMediaElement.prototype.pause).toHaveBeenCalled(); expect(vi.getTimerCount()).toBe(0)
  })
  it('pagehide pauses the element immediately before a framework render', async () => {
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media }); await settle()
    const element = result.container.querySelector('video')!
    vi.mocked(HTMLMediaElement.prototype.pause).mockClear()
    window.dispatchEvent(new Event('pagehide'))
    expect(element.pause).toHaveBeenCalledOnce()
    await settle(); expect(result.container.querySelector('video')).toBeNull()
  })
  it('a stalled clip falls back visibly and a missing end event is bounded', async () => {
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media }); await settle()
    await fireEvent.stalled(result.container.querySelector('video')!); await settle(); expect(screen.getByTestId('media-failure')).toBeVisible()
    vi.advanceTimersByTime(1500); await settle(); expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
    await result.rerender({ pigeon: 'nonverbal', signal: signal('second'), media }); await settle()
    vi.advanceTimersByTime(1350); await settle(); expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest'); expect(screen.getByTestId('media-failure')).toBeVisible()
  })
  it('pending audio cannot leave the visual stage active indefinitely', async () => {
    render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), playback: 'pending', media }); await settle()
    vi.advanceTimersByTime(15000); await settle(); expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest'); expect(screen.getByTestId('media-failure')).toBeVisible()
  })
  it('reduced motion supplies a neutral bounded cue with no video playback', async () => {
    const original = window.matchMedia
    vi.spyOn(window, 'matchMedia').mockImplementation(query => ({ ...original(query), matches: true }))
    const result = render(SignalMedia, { pigeon: 'nonverbal', signal: signal(), media }); await settle()
    expect(result.container.querySelector('video')).toBeNull(); expect(HTMLMediaElement.prototype.play).not.toHaveBeenCalled()
    expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'active')
    vi.advanceTimersByTime(1500); await settle(); expect(screen.getByTestId('stage-portrait')).toHaveAttribute('data-signal', 'rest')
  })
  it('missing portraits keep the original drawing and identity layout available', async () => {
    const failure = vi.fn(); const result = render(PigeonPortrait, { pigeon: 'nonverbal', asset: media.rest, onfailure: failure })
    const image = result.container.querySelector('img')!
    await fireEvent.error(image); await settle()
    expect(result.container.querySelector('img')).toBeNull(); expect(result.container.querySelector('svg')).not.toHaveClass('concealed'); expect(failure).toHaveBeenCalledOnce()
  })
})
