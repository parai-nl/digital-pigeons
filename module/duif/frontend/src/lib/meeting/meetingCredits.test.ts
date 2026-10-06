// Geen menselijke duiding van het signaleren, ook niet in de bronvermelding
// onder het vergaderbeeld.
//
// De scan in MeetingView.test.ts leest alles wat de zaal kan zien, maar het
// blok "Bronvermelding" staat er alleen als er media gestaged is: zonder
// public/media is `virtual:duif-media` leeg, valt `{#if model || sounds.length}`
// weg en leest die scan dus niets van de credits. Juist daar staan de titels
// van derden, en juist daar zou een titel als "Pigeon alarm call" ongemerkt een
// bandnaam op het scherm zetten.
//
// Deze test zet het native pakket zelf klaar, met drie opnames zoals ze
// werkelijk in het manifest staan, en laat dezelfde scan er wél overheen lopen.
import { cleanup, render, screen } from '@testing-library/svelte'
import { flushSync } from 'svelte'
import { afterEach, expect, it, vi } from 'vitest'
import type { SignalSounds } from '../media/types'
import { SERVICES } from '../services.svelte'
import { scriptedFetch } from '../testing/fetch'
import { meetingHarness, meetingRoutes, readEverything, SIGNAL_EXPLANATION } from '../testing/meeting'

// Het gemokte pakket is één object dat de test per geval vult; de componenten
// en de store lezen het allemaal via dezelfde virtuele module.
const catalogue = vi.hoisted(() => ({
  portraits: {}, headshots: {}, rest: null, motions: [], temporarySound: false,
  model: null as unknown, signalSounds: null as SignalSounds | null,
}))
vi.mock('virtual:duif-media', () => ({ default: catalogue }))

import MeetingView from './MeetingView.svelte'

// De drie opnames van het manifest: echte titels en makers van derden, want een
// verzonnen titel zou nooit op een echte bandnaam kunnen lijken.
const RECORDINGS: SignalSounds = {
  calm: {
    id: 'pigeon-field-coo', url: '/modules/duif/media/pigeon-field-coo.mp3', durationMs: 4529,
    attribution: {
      title: 'Pigeons & Doves - Homing Pigeon Cooing', creator: 'TheKingOfGeeks360',
      source: 'https://freesound.org/people/TheKingOfGeeks360/sounds/808001/',
      license: 'CC0 1.0', licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/',
      changes: 'Fragment uit de openbare HQ-preview, genormaliseerd naar −24,3 LUFS.',
    },
  },
  insistent: {
    id: 'pigeon-field-coos-repeated', url: '/modules/duif/media/pigeon-field-coos-repeated.mp3', durationMs: 3950,
    attribution: {
      title: 'R01-28 Pigeon Coos', creator: 'craigsmith',
      source: 'https://freesound.org/people/craigsmith/sounds/479589/',
      license: 'CC0 1.0', licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/',
      changes: 'Fragment 3,90–7,85 s uit de openbare HQ-preview, genormaliseerd naar −19,8 LUFS.',
    },
  },
  alarm: {
    id: 'pigeon-field-loft-chorus', url: '/modules/duif/media/pigeon-field-loft-chorus.mp3', durationMs: 3600,
    attribution: {
      title: 'Tauben im Taubenschlag (pigeons)', creator: 'Beskinsky',
      source: 'https://freesound.org/people/Beskinsky/sounds/778143/',
      license: 'CC0 1.0', licenseUrl: 'https://creativecommons.org/publicdomain/zero/1.0/',
      changes: 'Fragment 95,20–98,80 s uit de openbare HQ-preview, genormaliseerd naar −20,1 LUFS.',
    },
  },
}

const tick = () => new Promise((resolve) => setTimeout(resolve, 0))

afterEach(() => { cleanup(); catalogue.signalSounds = null })

async function live(lang: 'en' | 'nl') {
  const scripted = scriptedFetch(meetingRoutes())
  const harness = meetingHarness(scripted.fetch)
  render(MeetingView, { context: new Map([[SERVICES, harness.services()]]) })
  await tick()
  flushSync()
  harness.store.pigeonId = 'nonverbal'
  harness.locale.choose(lang)
  await harness.store.start()
  await tick()
  flushSync()
  return harness
}

it('shows no credit block at all in a checkout without staged media', async () => {
  await live('en')

  // Dit is de reden dat de scan hiernaast niets over de credits bewijst: er
  // staat dan domweg geen bronvermelding op het scherm.
  expect(screen.queryByTestId('media-credit')).toBeNull()
})

it('names no band, emotion or theme in the source credits of the field recordings', async () => {
  for (const lang of ['en', 'nl'] as const) {
    catalogue.signalSounds = RECORDINGS
    const { ports } = await live(lang)

    const spoken: string[] = []
    for (const band of ['calm', 'insistent', 'alarm']) {
      ports.channel().emit({ type: 'sound', eventId: `event-${band}-${lang}`, sound: { id: 'clip', url: '/api/duif/v1/sounds/clip', band }, durationMs: 1200 })
      flushSync()
      expect(ports.soundPlayers.at(-1)!.url).toBe(RECORDINGS[band as keyof SignalSounds].url)
      readEverything(spoken)
      ports.soundPlayers.at(-1)!.end()
      flushSync()
      readEverything(spoken)
    }

    // Zonder deze twee regels zou de scan langs een leeg of
    // ontbrekend credit-blok lopen en alsnog groen zijn.
    expect(screen.getByTestId('media-credit')).toBeInTheDocument()
    for (const recording of Object.values(RECORDINGS)) {
      expect(spoken.some((text) => text.includes(recording.attribution.title))).toBe(true)
      expect(spoken.some((text) => text.includes(recording.attribution.creator))).toBe(true)
    }

    const offending = spoken.filter((text) => SIGNAL_EXPLANATION.test(text))
    expect(offending, `${lang}: de bronvermelding legt het signaal uit`).toEqual([])
    cleanup()
  }
})
