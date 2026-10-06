import { describe, expect, it } from 'vitest'
import { FakeAudioContext } from '../testing/meeting'
import { MAX_TTS_BYTES, PcmTurn, SoundBoard, TtsCollector, TtsPlayer } from './player'

const b64 = (text: string) => globalThis.btoa(text)

// flush laat de wachtende microtaken van een losgelaten decodering aflopen; de
// speler doet zijn werk in de voortzetting daarvan.
const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('TtsCollector', () => {
  it('joins the frames of one turn on byte level', () => {
    const collector = new TtsCollector()
    collector.add(b64('ABC'), 1)
    collector.add(b64('DEF'), 2)
    const result = collector.finish(2)

    expect(result.ok).toBe(true)
    expect(result.ok && String.fromCharCode(...result.bytes)).toBe('ABCDEF')
  })

  it('refuses a turn whose frame numbering has a gap', () => {
    const collector = new TtsCollector()
    collector.add(b64('ABC'), 1)
    collector.add(b64('GHI'), 3)

    expect(collector.finish(3)).toEqual({ ok: false, reason: 'gap' })
  })

  it('refuses a turn that delivered fewer frames than announced', () => {
    const collector = new TtsCollector()
    collector.add(b64('ABC'), 1)

    expect(collector.finish(2)).toEqual({ ok: false, reason: 'gap' })
  })

  it('reports an empty turn as empty, not as a gap', () => {
    expect(new TtsCollector().finish(0)).toEqual({ ok: false, reason: 'empty' })
  })

  it('throws away a turn that runs past the memory cap', () => {
    const collector = new TtsCollector()
    const chunk = b64('x'.repeat(3 * 1024 * 1024))
    collector.add(chunk, 1)
    collector.add(chunk, 2)
    collector.add(chunk, 3)

    expect(MAX_TTS_BYTES).toBe(8 * 1024 * 1024)
    expect(collector.finish(3)).toEqual({ ok: false, reason: 'overflow' })
  })

  it('treats an unreadable frame as a gap instead of breaking', () => {
    const collector = new TtsCollector()
    collector.add('not base64 at all!!', 1)

    expect(collector.finish(1)).toEqual({ ok: false, reason: 'gap' })
  })
})

describe('TtsPlayer', () => {
  it('plays two turns one after the other', async () => {
    const ctx = new FakeAudioContext()
    const player = new TtsPlayer(() => ctx, () => {})

    await player.enqueue(new Uint8Array([1, 2, 3]))
    await player.enqueue(new Uint8Array([4, 5]))
    expect(ctx.played).toHaveLength(1)

    ctx.finish(0)
    expect(ctx.played).toHaveLength(2)
  })

  it('keeps the turns in order when the second one decodes first', async () => {
    const ctx = new FakeAudioContext()
    ctx.holdDecode = true
    const player = new TtsPlayer(() => ctx, () => {})

    // Twee beurten tegelijk onderweg; de plek in de rij wordt gereserveerd bij
    // het aanbieden, niet bij het klaarkomen van de decodering.
    void player.enqueue(new Uint8Array([1, 2, 3]))
    void player.enqueue(new Uint8Array([4, 5]))
    expect(ctx.decodesPending).toBe(2)

    // De tweede is als eerste klaar: er klinkt nog niets, want de eerste zin
    // was eerder aan de beurt.
    ctx.releaseDecode(1)
    await flush()
    expect(ctx.played).toHaveLength(0)

    ctx.releaseDecode(0)
    await flush()
    expect(ctx.played).toHaveLength(1)
    expect(ctx.played[0].bytes).toBe(3)

    ctx.finish(0)
    expect(ctx.played).toHaveLength(2)
    expect(ctx.played[1].bytes).toBe(2)
  })

  it('does not let a failed decode block the turn behind it', async () => {
    const ctx = new FakeAudioContext()
    ctx.holdDecode = true
    let reported = 0
    const player = new TtsPlayer(() => ctx, () => reported++)

    void player.enqueue(new Uint8Array([1, 2, 3]))
    void player.enqueue(new Uint8Array([4, 5]))

    ctx.releaseDecode(1)
    await flush()
    expect(ctx.played).toHaveLength(0)

    // De eerste beurt is onspeelbaar; de tweede schuift door in plaats van
    // achter een lege plek te blijven staan.
    ctx.failDecodePending(0)
    await flush()
    expect(reported).toBe(1)
    expect(ctx.played).toHaveLength(1)
    expect(ctx.played[0].bytes).toBe(2)
  })

  it('ignores the onended of a source that was already stopped', async () => {
    const ctx = new FakeAudioContext()
    const player = new TtsPlayer(() => ctx, () => {})

    await player.enqueue(new Uint8Array([1, 2, 3]))
    await player.enqueue(new Uint8Array([4, 5]))
    player.silence()

    ctx.finish(0)
    expect(ctx.played).toHaveLength(1)
    expect(ctx.played[0].stopped).toBe(true)
  })

  it('reports a decode that fails instead of failing silently', async () => {
    const ctx = new FakeAudioContext()
    ctx.failDecode = true
    let reported = 0
    const player = new TtsPlayer(() => ctx, () => reported++)

    await player.enqueue(new Uint8Array([1]))
    expect(reported).toBe(1)
    expect(ctx.played).toHaveLength(0)
  })
})

describe('SoundBoard', () => {
  function recording() {
    let ended: (() => void) | null = null
    return {
      stopped: false,
      stop() {
        this.stopped = true
      },
      onEnded(cb: () => void) {
        ended = cb
      },
      end() {
        ended?.()
      },
    }
  }

  it('holds the recordings it plays so the stop button can reach them', () => {
    const board = new SoundBoard()
    const one = recording()
    const two = recording()
    board.play(one)
    board.play(two)
    expect(board.count).toBe(2)

    board.silence()
    expect(one.stopped && two.stopped).toBe(true)
    expect(board.count).toBe(0)
  })

  it('lets go of a recording once it has finished sounding', () => {
    const board = new SoundBoard()
    const one = recording()
    const two = recording()
    board.play(one)
    board.play(two)

    one.end()
    expect(board.count).toBe(1)

    board.silence()
    expect(one.stopped).toBe(false)
    expect(two.stopped).toBe(true)
  })
})

// pcmBase64 maakt van monsters het little-endian PCM16-frame zoals de server
// het stuurt, base64-gecodeerd.
function pcmBase64(samples: number[]): string {
  const bytes = new Uint8Array(samples.length * 2)
  samples.forEach((v, i) => {
    const u = v < 0 ? v + 0x10000 : v
    bytes[2 * i] = u & 0xff
    bytes[2 * i + 1] = u >> 8
  })
  return globalThis.btoa(String.fromCharCode(...bytes))
}

describe('PcmTurn', () => {
  it('decodes little-endian PCM16 including negative samples', () => {
    const turn = new PcmTurn()
    const samples = turn.add(pcmBase64([1, -1, 32767, -32768, 0]), 1)
    expect(samples && Array.from(samples)).toEqual([1, -1, 32767, -32768, 0])
    expect(turn.finish(1)).toEqual({ ok: true })
  })

  it('carries a half sample over to the next frame instead of dropping it', () => {
    const turn = new PcmTurn()
    const whole = new Uint8Array([0x34, 0x12, 0x78, 0x56]) // 0x1234, 0x5678
    const first = globalThis.btoa(String.fromCharCode(...whole.subarray(0, 3)))
    const second = globalThis.btoa(String.fromCharCode(...whole.subarray(3)))
    const a = turn.add(first, 1)
    const b = turn.add(second, 2)
    expect(a && Array.from(a)).toEqual([0x1234])
    expect(b && Array.from(b)).toEqual([0x5678])
  })

  it('stops playing after a gap in the numbering and reports it at the end', () => {
    const turn = new PcmTurn()
    expect(turn.add(pcmBase64([1]), 1)).not.toBeNull()
    expect(turn.add(pcmBase64([2]), 3)).toBeNull()
    expect(turn.add(pcmBase64([3]), 4)).toBeNull()
    expect(turn.finish(4)).toEqual({ ok: false, reason: 'gap' })
  })

  it('reports fewer frames than announced as a gap and none as empty', () => {
    const turn = new PcmTurn()
    turn.add(pcmBase64([1]), 1)
    expect(turn.finish(2)).toEqual({ ok: false, reason: 'gap' })
    expect(new PcmTurn().finish(0)).toEqual({ ok: false, reason: 'empty' })
  })

  it('throws away a turn that runs past the memory cap', () => {
    const turn = new PcmTurn()
    const chunk = globalThis.btoa('x'.repeat(3 * 1024 * 1024))
    expect(turn.add(chunk, 1)).not.toBeNull()
    expect(turn.add(chunk, 2)).not.toBeNull()
    expect(turn.add(chunk, 3)).toBeNull()
    expect(turn.finish(3)).toEqual({ ok: false, reason: 'overflow' })
  })
})

describe('TtsPlayer streaming', () => {
  it('writes blocks into one stream and keeps it across turns', () => {
    const ctx = new FakeAudioContext()
    const player = new TtsPlayer(
      () => ctx,
      () => {},
    )
    player.write(new Int16Array(100), 24000)
    player.write(new Int16Array(50), 24000)
    expect(ctx.streams).toHaveLength(1)
    expect(ctx.stream?.written).toEqual([100, 50])
    expect(ctx.stream?.sampleRate).toBe(24000)
  })

  it('opens a fresh stream when the sample rate changes', () => {
    const ctx = new FakeAudioContext()
    const player = new TtsPlayer(
      () => ctx,
      () => {},
    )
    player.write(new Int16Array(10), 24000)
    player.write(new Int16Array(10), 16000)
    expect(ctx.streams).toHaveLength(2)
    expect(ctx.streams[0].stopped).toBe(true)
    expect(ctx.streams[1].sampleRate).toBe(16000)
  })

  it('silence stops the stream and a later block starts a new one', () => {
    const ctx = new FakeAudioContext()
    const player = new TtsPlayer(
      () => ctx,
      () => {},
    )
    player.write(new Int16Array(10), 24000)
    player.silence()
    expect(ctx.streams[0].stopped).toBe(true)
    player.write(new Int16Array(10), 24000)
    expect(ctx.streams).toHaveLength(2)
    expect(ctx.streams[1].written).toEqual([10])
  })

  it('writes nothing without a context or with an empty block', () => {
    const ctx = new FakeAudioContext()
    const gone = new TtsPlayer(
      () => null,
      () => {},
    )
    gone.write(new Int16Array(10), 24000)
    const player = new TtsPlayer(
      () => ctx,
      () => {},
    )
    player.write(new Int16Array(0), 24000)
    expect(ctx.streams).toHaveLength(0)
  })
})


describe('TtsPlayer real playback lifecycle', () => {
  it('keeps MP3 pending through decoding and only ends after audible playback', async () => {
    const ctx = new FakeAudioContext()
    ctx.holdDecode = true
    const states: string[] = []
    const player = new TtsPlayer(() => ctx, () => {}, (state) => states.push(state))
    const decoding = player.enqueue(new Uint8Array([1, 2]))
    player.finish()
    expect(states).toEqual([])
    ctx.releaseDecode(0)
    await decoding
    expect(states).toEqual(['started'])
    ctx.finish(0)
    expect(states).toEqual(['started', 'ended'])
  })

  it('waits for the PCM port drain after tts_done', () => {
    const ctx = new FakeAudioContext()
    let ended: (() => void) | undefined
    ctx.openStream = () => ({ write() {}, stop() {}, finish(cb: () => void) { ended = cb } })
    const states: string[] = []
    const player = new TtsPlayer(() => ctx, () => {}, (state) => states.push(state))
    player.write(new Int16Array([1, 2]), 24000)
    player.finish()
    expect(states).toEqual(['started'])
    ended?.()
    expect(states).toEqual(['started', 'ended'])
  })

  it('cancels decoding without late playback after stop', async () => {
    const ctx = new FakeAudioContext()
    ctx.holdDecode = true
    const states: string[] = []
    const player = new TtsPlayer(() => ctx, () => {}, (state) => states.push(state))
    const decoding = player.enqueue(new Uint8Array([1]))
    player.finish()
    player.silence()
    ctx.releaseDecode(0)
    await decoding
    expect(states).toEqual(['aborted'])
    expect(ctx.played).toHaveLength(0)
  })

  it('does not report suspended audio as playing', async () => {
    const ctx = new FakeAudioContext()
    ctx.state = 'suspended'
    const states: string[] = []
    const player = new TtsPlayer(() => ctx, () => {}, (state) => states.push(state))
    await player.enqueue(new Uint8Array([1]))
    player.finish()
    expect(states).toEqual(['failed'])
    expect(ctx.played).toHaveLength(0)
  })
})
