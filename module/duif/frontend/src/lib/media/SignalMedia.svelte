<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import catalogue from 'virtual:duif-media'
  import type { MotionAsset, PublicMedia } from './types'
  import PigeonPortrait from '../shell/PigeonPortrait.svelte'
  import { mediaCopy } from './copy'

  type Signal = { id: string; durationMs: number }
  type Run = { id: string; cancelled: boolean; attempted: boolean; timer?: ReturnType<typeof setTimeout> }
  let { pigeon, signal, playback = 'playing', lang = 'en', media = catalogue }: {
    pigeon: string; signal: Signal | null; playback?: string; lang?: 'en' | 'nl'; media?: PublicMedia
  } = $props()
  let video: HTMLVideoElement | undefined = $state()
  let run: Run | null = $state.raw(null)
  let motion: MotionAsset | null = $state.raw(null)
  let active = $state(false)
  let showingVideo = $state(false)
  let failed = $state(false)
  let imageFailed = $state(false)
  let reduced = $state(typeof window === 'undefined' || typeof window.matchMedia !== 'function' ? true : window.matchMedia('(prefers-reduced-motion: reduce)').matches)
  const signalKey = $derived(`${pigeon}:${signal?.id ?? ''}`)
  const seen = new Set<string>()
  let lastMotion = -1
  const text = $derived(mediaCopy[lang])
  const image = $derived(pigeon === 'nonverbal' && media.rest ? media.rest : media.portraits[pigeon as keyof typeof media.portraits] ?? null)
  const temporary = $derived(!image ? (pigeon === 'nonverbal' && media.temporarySound ? text.signalTemporary : pigeon === 'nonverbal' ? text.signalImageTemporary : text.imageUnavailable) : pigeon === 'nonverbal' && media.temporarySound ? text.soundTemporary : '')

  function pause(element = video) {
    if (!element) return
    try { element.pause(); element.currentTime = 0 } catch { /* Failed decodes may not have a seekable position. */ }
  }
  function finish(current: Run) {
    if (run !== current || current.cancelled) return
    current.cancelled = true
    clearTimeout(current.timer)
    pause()
    showingVideo = false; active = false; motion = null
  }
  function deadline(current: Run, duration: number) {
    clearTimeout(current.timer)
    current.timer = setTimeout(() => { if (run === current && motion) failed = true; finish(current) }, duration)
  }
  function mediaFailure(current: Run) {
    if (run !== current || current.cancelled) return
    pause(); showingVideo = false; motion = null; failed = true
    deadline(current, 1500)
  }
  function begin(signal: Signal | null) {
    if (run) finish(run)
    run = null; failed = false
    if (!signal || pigeon !== 'nonverbal' || seen.has(signal.id)) return
    if (seen.size >= 4096) { failed = true; return }
    seen.add(signal.id)
    const current: Run = { id: signal.id, cancelled: false, attempted: false }
    run = current; active = true; showingVideo = false
    if (media.rest && media.motions.length && !reduced) {
      lastMotion = (lastMotion + 1) % media.motions.length
      motion = media.motions[lastMotion]
      // Pending/blocked playback and malformed media can never leave an active
      // stage indefinitely. Audio lifecycle remains owned by meetingStore.
      deadline(current, 15000)
    } else {
      motion = null
      deadline(current, Math.max(200, Math.min(8000, Number.isFinite(signal.durationMs) ? signal.durationMs : 1500)))
    }
    return () => { finish(current) }
  }
  $effect(() => { image?.url; imageFailed = false })
  $effect(() => {
    signalKey
    // Event identity owns the run. Later statuses and browser media callbacks
    // must not restart it, including after Stop has cleared the same event.
    return untrack(() => begin(signal))
  })
  $effect(() => {
    const current = run, element = video, selected = motion, status = playback, reduce = reduced
    untrack(() => {
      if (!current || current.cancelled || !selected || !element) return
      if (reduce) { pause(element); motion = null; showingVideo = false; deadline(current, 1500); return }
      if (status === 'pending' || current.attempted) return
      current.attempted = true
      const stillCurrent = () => run === current && !current.cancelled && video === element
      try {
        element.muted = true; element.loop = false
        Promise.resolve(element.play()).then(() => {
          if (!stillCurrent()) { pause(element); return }
          showingVideo = true
          deadline(current, Math.max(200, Math.min(8000, selected.durationMs)) + 250)
        }).catch(() => { if (stillCurrent()) mediaFailure(current); else pause(element) })
      } catch { mediaFailure(current) }
    })
  })
  onMount(() => {
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const update = () => { reduced = query.matches }
    const hide = () => { if (run) finish(run) }
    update(); query.addEventListener('change', update); window.addEventListener('pagehide', hide)
    return () => { query.removeEventListener('change', update); window.removeEventListener('pagehide', hide); hide() }
  })
</script>

<div class="media-group">
  <div class="media-frame" class:active class:gesture={active && !motion && !reduced} data-testid="stage-portrait" data-signal={active ? 'active' : 'rest'}>
    <PigeonPortrait {pigeon} asset={image} onfailure={() => imageFailed = true} />
    {#if motion && run && !run.cancelled}{#key run.id}{@const current = run}
      <!-- Decorative bird footage is always muted; meetingStore owns sound. -->
      <!-- svelte-ignore a11y_media_has_caption -->
      <video bind:this={video} src={motion.url} poster={image?.url} muted playsinline preload="metadata" aria-hidden="true" tabindex="-1" class:showing={showingVideo} style:object-position={`${motion.focus[0] * 100}% ${motion.focus[1] * 100}%`} onended={() => finish(current)} onstalled={() => mediaFailure(current)} onerror={() => mediaFailure(current)}></video>
    {/key}{/if}
  </div>
  {#if temporary}<p class="media-note" data-testid="media-temporary">{temporary}</p>{/if}
  {#if failed || imageFailed}<p class="media-failure" role="status" data-testid="media-failure">{imageFailed ? text.imageUnavailable : text.unavailable}</p>{/if}
</div>

<style>
  .media-group { width: 100%; }
  .media-frame { width: 100%; aspect-ratio: 1; border-radius: 50%; position: relative; }
  video { width: 100%; height: 100%; position: absolute; inset: 0; object-fit: cover; border-radius: inherit; opacity: 0; pointer-events: none; }
  video.showing { opacity: 1; }
  .active { box-shadow: 0 0 0 .5rem var(--duif-line); }
  .gesture { animation: media-gesture 1200ms ease-in-out 1; }
  .media-note, .media-failure { font-size: .62rem; line-height: 1.5; margin: .8rem 0 0; color: var(--duif-dim); }
  .media-failure { font-weight: 600; }
  @keyframes media-gesture { 0%,100% { transform: rotate(0) translateY(0); } 35% { transform: rotate(-3deg) translateY(-.25rem); } 70% { transform: rotate(2deg); } }
  @media(prefers-reduced-motion: reduce) { .gesture { animation: none; } }
</style>
