import { afterEach, expect, it, vi } from 'vitest'
import { scriptedFetch } from '../testing/fetch'
import { meetingHarness, meetingRoutes } from '../testing/meeting'
type Recording = { url: string; durationMs: number }
const catalogue = vi.hoisted(() => ({ signalSounds: null as null | Record<string, { url: string; durationMs: number }> }))
// Het native pakket levert één opname per band. De band komt van de
// server mee; de test controleert dat de keuze die band volgt en dat de
// deelnemer er geen woord over te zien krijgt.
const packaged: Record<string, Recording> = {
  calm: { url: '/modules/duif/media/coo-field.mp3', durationMs: 4529 },
  insistent: { url: '/modules/duif/media/coo-field-repeated.mp3', durationMs: 3950 },
  alarm: { url: '/modules/duif/media/loft-chorus.mp3', durationMs: 3600 },
}
vi.mock('virtual:duif-media', () => ({ default: catalogue }))
const stores: { dispose(): void }[] = []
afterEach(() => { for (const store of stores.splice(0)) store.dispose(); catalogue.signalSounds = null })
async function live() {
  const script = scriptedFetch(meetingRoutes())
  const h = meetingHarness(script.fetch)
  stores.push(h.store)
  await h.store.loadPigeons(); h.store.pigeonId = 'nonverbal'; await h.store.start()
  await new Promise(resolve => setTimeout(resolve, 0))
  return h
}
function signal(h: Awaited<ReturnType<typeof live>>, eventId: string, band?: string) {
  h.ports.channel().emit({ type: 'sound', eventId, sound: { url: '/api/duif/v1/sounds/coo-soft', band }, durationMs: 1500 })
}
it('plays the selected field recording once per event and follows actual playback end', async () => {
  catalogue.signalSounds = packaged
  const h = await live(); signal(h, 'one', 'calm')
  expect(h.ports.soundPlayers).toHaveLength(1)
  expect(h.ports.soundPlayers[0].url).toBe(packaged.calm.url)
  expect(h.store.signal?.durationMs).toBe(4529)
  expect(h.store.playbackStatus).toBe('started')
  signal(h, 'one', 'calm'); expect(h.ports.soundPlayers).toHaveLength(1)
  const revision = h.store.mediaStopRevision
  h.ports.soundPlayers[0].end()
  expect(h.store.playbackStatus).toBe('ended')
  expect(h.store.mediaStopRevision).toBe(revision)
  signal(h, 'two', 'calm'); expect(h.ports.soundPlayers).toHaveLength(2)
  await h.store.stop()
  expect(h.ports.soundPlayers[1].stopped).toBe(true)
  expect(h.store.mediaStopRevision).toBeGreaterThan(revision)
  expect(h.store.signal).toBeNull()
})
it('preserves server audio when no field recording was packaged and cancels visuals on session disposal', async () => {
  const h = await live(); signal(h, 'one')
  expect(h.ports.soundPlayers[0].url).toBe('/api/duif/v1/sounds/coo-soft')
  const revision = h.store.mediaStopRevision
  h.store.dispose()
  expect(h.store.mediaStopRevision).toBeGreaterThan(revision)
  expect(h.ports.soundPlayers[0].stopped).toBe(true)
  h.ports.soundPlayers[0].end()
  expect(h.store.signal).toBeNull(); expect(h.store.live).toBe(false)
})

it('follows the band of the event instead of playing one recording for everything', async () => {
  catalogue.signalSounds = packaged
  const h = await live()
  const durations: (number | undefined)[] = []
  for (const [index, band] of ['calm', 'insistent', 'alarm'].entries()) {
    signal(h, `event-${band}`, band)
    durations.push(h.store.signal?.durationMs)
    h.ports.soundPlayers[index].end()
  }
  expect(h.ports.soundPlayers.map(player => player.url)).toEqual([packaged.calm.url, packaged.insistent.url, packaged.alarm.url])
  expect(durations).toEqual([4529, 3950, 3600])
  // De band stuurt alleen het bestand. Hij staat in geen enkele zichtbare of
  // voorgelezen tekst van de deelnemersweergave.
  expect(JSON.stringify({ signal: h.store.signal, status: h.store.playbackStatus })).not.toMatch(/calm|insistent|alarm/)
})
it('keeps the server sound when the event carries no band this build knows', async () => {
  catalogue.signalSounds = packaged
  const h = await live()
  signal(h, 'one', 'thundering')
  expect(h.ports.soundPlayers[0].url).toBe('/api/duif/v1/sounds/coo-soft')
  signal(h, 'two')
  expect(h.ports.soundPlayers[1].url).toBe('/api/duif/v1/sounds/coo-soft')
})
