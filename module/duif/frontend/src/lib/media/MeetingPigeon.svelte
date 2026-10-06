<script lang="ts">
  import { onMount, untrack } from 'svelte'
  import catalogue from 'virtual:duif-media'
  import PigeonPortrait from '../shell/PigeonPortrait.svelte'
  import { mediaCopy } from './copy'
  import type { PigeonIdentity, PublicMedia } from './types'
  import type { PigeonStage } from './model/pigeonStage'

  let { pigeon, reactionId = null, stopRevision = 0, enabled = true, paused = false, lang = 'en', media = catalogue }: {
    pigeon: string
    reactionId?: string | null
    stopRevision?: number
    enabled?: boolean
    paused?: boolean
    lang?: 'en' | 'nl'
    media?: PublicMedia
  } = $props()

  let host: HTMLDivElement
  let mounted = $state(false)
  let stage: PigeonStage | null = null
  let ready = $state(false), failed = $state(false), active = $state(false), neutral = $state(false)
  let posterFailed = $state(false)
  let reduced = $state(true)
  // Elke duif heeft haar eigen model, rustposter en kijkhoek: de
  // Kennispartner recht van voren, de Stakeholder driekwart van voren, de
  // signalerende duif in zijaanzicht. Ontbreekt haar model, dan blijft haar
  // portret staan — nooit het model van een andere duif.
  const model = $derived(media.models?.[pigeon as PigeonIdentity] ?? null)
  // De bronvermelding noemt elke gebruikte opname één keer. Nergens staat bij
  // welke band een opname hoort: de deelnemer krijgt geen bandnaam te zien en
  // ook geen ordening die als betekenis te lezen is.
  const sounds = $derived(pigeon === 'nonverbal'
    ? [...new Map(Object.values(media.signalSounds ?? {}).map(asset => [asset.attribution.source, asset.attribution])).values()]
    : [])
  const assetUrl = $derived(model?.url)
  const view = $derived(model?.view)
  const canLoad = $derived(enabled && mounted)
  const copy = $derived(mediaCopy[lang])
  const portrait = $derived(pigeon === 'nonverbal' && media.rest ? media.rest : media.portraits[pigeon as keyof typeof media.portraits] ?? null)
  const temporary = $derived(!model && !portrait
    ? pigeon === 'nonverbal' ? media.temporarySound ? copy.signalTemporary : copy.signalImageTemporary : copy.imageUnavailable
    : pigeon === 'nonverbal' && media.temporarySound ? copy.soundTemporary : '')
  // An already visible contribution is history when this view mounts again.
  // Only new event identities can animate; loading and status updates cannot.
  const initialId = untrack(() => reactionId)
  const seen = new Set<string>(initialId ? [initialId] : [])
  let observedStop = untrack(() => stopRevision)
  let observedReduced = true
  let stopEvent = () => {}

  function react() {
    const engine = stage
    const motion = !reduced && ready && engine ? engine.beginReaction('auto') : null
    const duration = motion?.duration ?? 1.6
    active = true; neutral = !motion
    const start = performance.now()
    let ended = false, frame = 0
    let timer: ReturnType<typeof setTimeout>
    const cancel = () => {
      if (ended) return
      ended = true; active = false; neutral = false
      clearTimeout(timer); cancelAnimationFrame(frame)
      engine?.restore(); engine?.render()
    }
    const update = () => {
      if (ended) return
      const elapsed = (performance.now() - start) / 1000
      if (elapsed >= duration) { cancel(); return }
      engine?.pose(elapsed); engine?.render()
      frame = requestAnimationFrame(update)
    }
    stopEvent = cancel
    timer = setTimeout(cancel, duration * 1000)
    if (motion) frame = requestAnimationFrame(update)
  }

  $effect(() => {
    const id = reactionId, revision = stopRevision, allowed = enabled && !paused, reduce = reduced
    untrack(() => {
      if (revision !== observedStop || !allowed || reduce !== observedReduced) stopEvent()
      observedStop = revision; observedReduced = reduce
      if (!id) { if (neutral) stopEvent(); return }
      if (seen.has(id)) return
      // A session cannot accumulate an unbounded event ledger in the component.
      if (seen.size >= 4096) return
      seen.add(id)
      stopEvent()
      if (!allowed || document.hidden) return
      react()
    })
  })

  $effect(() => { model?.posterUrl; posterFailed = false })

  $effect(() => {
    const url = assetUrl, allowed = canLoad, angle = view
    if (!url || !angle || !allowed) return
    return untrack(() => {
      let cancelled = false
      let engine: PigeonStage | null = null
      let cancelLoad = () => {}
      ready = false; failed = false
      const failure = () => {
        if (cancelled) return
        stopEvent(); ready = false; failed = true
      }
      // Three and GLTFLoader are outside the initial workspace bundle. The
      // import and asset load both guard against a hidden or ended meeting.
      void import('./model/pigeonStage').then(({ createPigeonStage }) => {
        if (cancelled) return
        try {
          engine = createPigeonStage(host, failure)
          stage = engine
          cancelLoad = engine.load(url, reactive => {
            if (cancelled) return
            ready = reactive; failed = !reactive
          }, failure, angle)
        } catch { failure() }
      }).catch(failure)
      return () => {
        cancelled = true; stopEvent(); cancelLoad(); engine?.dispose()
        if (stage === engine) stage = null
        ready = false
      }
    })
  })

  onMount(() => {
    mounted = true
    const query = window.matchMedia('(prefers-reduced-motion: reduce)')
    const update = () => { reduced = query.matches }
    const hide = () => { if (document.hidden) stopEvent() }
    const leave = () => stopEvent()
    update(); query.addEventListener('change', update)
    document.addEventListener('visibilitychange', hide)
    window.addEventListener('pagehide', leave)
    return () => {
      stopEvent(); query.removeEventListener('change', update)
      document.removeEventListener('visibilitychange', hide)
      window.removeEventListener('pagehide', leave)
    }
  })
</script>

<div class="meeting-pigeon" data-testid="stage-portrait" data-signal={active ? 'active' : 'rest'}>
  <!-- Arrow keys rotate only this image; they never control the meeting. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="pigeon-canvas" class:ready class:neutral={active && neutral} bind:this={host}
    role="img" aria-label={ready ? copy.modelControls : copy.modelStill} tabindex={ready ? 0 : undefined}
    data-model-state={!model ? 'unconfigured' : failed ? 'failed' : !ready ? 'loading' : active ? 'active' : 'rest'}>
    <!-- De rustposter is in dezelfde kijkhoek en kadrering gerenderd als het
         live model. Hij ligt onder het canvas: het model vloeit eroverheen in
         en pas daarna verdwijnt de poster, zodat de wissel geen sprong en geen
         lege flits is. Valt het model later weg, dan ligt hij er meteen weer.
         Geen klasse `hidden`: die naam is een Tailwind-utility (display: none)
         en zou de overvloeier overslaan. -->
    {#if model?.posterUrl && !posterFailed}
      <img class="model-poster" class:faded={ready} src={model.posterUrl} alt="" onerror={() => posterFailed = true} />
    {:else if !ready}<div class="portrait-fallback"><PigeonPortrait {pigeon} asset={portrait} /></div>{/if}
  </div>
  <p class="visual-status" role="status" aria-live="polite">{active ? copy.modelSignal : ''}</p>
  {#if temporary}<p class="model-note" data-testid="media-temporary">{temporary}</p>{/if}
  {#if failed}<p class="model-note" data-testid="model-unavailable">{copy.modelUnavailable}</p>{/if}
  {#if model || sounds.length}<details class="model-credit" data-testid="media-credit">
    <summary>{copy.modelCredit}</summary>
    {#if model}<p>{model.attribution.title} — {model.attribution.creator}.
      <a href={model.attribution.source} target="_blank" rel="noopener noreferrer">{copy.modelSource}</a> ·
      <a href={model.attribution.licenseUrl} target="_blank" rel="noopener noreferrer">{model.attribution.license}</a>.
      {model.attribution.changes}
    </p>{/if}
    {#each sounds as credit (credit.source)}<p>{credit.title} — {credit.creator}.
      <a href={credit.source} target="_blank" rel="noopener noreferrer">{copy.modelSoundSource}</a> ·
      <a href={credit.licenseUrl} target="_blank" rel="noopener noreferrer">{credit.license}</a>.
      {credit.changes}
    </p>{/each}
  </details>{/if}
</div>

<style>
  .meeting-pigeon { width: 100%; position: relative; }
  .pigeon-canvas { position: relative; height: clamp(17rem, 36vh, 30rem); width: 100%; overflow: hidden; border-radius: 1.25rem; outline: none; }
  .pigeon-canvas :global(canvas) { display: block; position: relative; z-index: 1; width: 100%; height: 100%; touch-action: pan-y; transition: opacity .4s ease; }
  .pigeon-canvas.ready :global(canvas) { cursor: grab; }
  .pigeon-canvas.ready :global(canvas:active) { cursor: grabbing; }
  .pigeon-canvas:focus-visible { outline: 2px solid var(--duif-dim); outline-offset: -3px; }
  .pigeon-canvas.neutral { box-shadow: inset 0 0 0 2px var(--duif-dim); }
  /* De poster is op zijn hoogte gekadreerd, net als het live model zolang de
     hoogte bepaalt: hij schaalt dus op de podiumhoogte en wordt gecentreerd,
     ook als het podium smaller is dan hij (de zijkanten zijn leeg). */
  .model-poster { position: absolute; top: 0; left: 50%; z-index: 0; width: auto; max-width: none; height: 100%; transform: translateX(-50%); pointer-events: none; }
  /* Weg pas als het canvas er helemaal overheen ligt; terug zonder wachten. */
  .model-poster.faded { opacity: 0; transition: opacity .15s linear .4s; }
  .portrait-fallback { position: absolute; inset: 8%; display: flex; align-items: center; justify-content: center; }
  .portrait-fallback :global(.pigeon-portrait) { width: auto; height: 100%; max-width: 100%; }
  .pigeon-canvas:not(.ready) :global(canvas) { opacity: 0; }
  @media (prefers-reduced-motion: reduce) { .model-poster.faded, .pigeon-canvas :global(canvas) { transition: none; } }
  .visual-status { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  .model-note { color: var(--duif-dim); font-size: .7rem; line-height: 1.5; margin: .25rem 1rem; text-align: center; }
  .model-credit { width: fit-content; max-width: 100%; margin: .2rem auto 0; font-size: .62rem; line-height: 1.5; color: var(--duif-dim); }
  .model-credit summary { cursor: pointer; padding: .3rem; }
  .model-credit p { max-width: 48ch; margin: .3rem .5rem .7rem; }
  .model-credit a { color: inherit; text-underline-offset: .15rem; }
  @media(max-width: 700px) { .pigeon-canvas { height: clamp(13rem, 31vh, 20rem); } }
</style>
