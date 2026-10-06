<script lang="ts">
  import { onMount } from 'svelte'
  import { useServices } from '../services.svelte'
  import { noticeEn, noticeNl, noticeVersion } from './noticeCopy'
  const { locale } = useServices()
  const copy = $derived(locale.lang === 'nl' ? noticeNl : noticeEn)
  let phase = $state<'loading' | 'read' | 'saving' | 'failed' | 'ready'>('loading')
  let controller: AbortController | undefined
  let disposed = false
  const url = '/api/duif/v1/access/notice'

  async function request(confirm = false): Promise<void> {
    if (phase === 'saving' || disposed) return
    controller?.abort()
    const current = new AbortController(); controller = current
    phase = confirm ? 'saving' : 'loading'
    try {
      const response = await fetch(url, { method: confirm ? 'POST' : 'GET', signal: current.signal, cache: 'no-store', ...(confirm ? { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ version: noticeVersion }) } : {}) })
      const body = await response.json()
      if (disposed || controller !== current) return
      if (!response.ok || body.version !== noticeVersion || typeof body.acknowledged !== 'boolean') throw new Error('notice unavailable')
      phase = body.acknowledged ? 'ready' : 'read'
    } catch {
      if (!disposed && controller === current) phase = 'failed'
    }
  }
  onMount(() => { void request(); return () => { disposed = true; controller?.abort() } })
</script>

{#if phase !== 'ready'}
  <section class="duif-first-use" lang={locale.lang} aria-labelledby="duif-notice-title">
    <!-- Geen eigen taalkeuze: de melding staat in de taal van de gastheer,
         net als de rest van de module. Twee plekken om te kiezen lieten de
         lezer met een module in de ene en een schil in de andere taal achter. -->
    <h2 id="duif-notice-title">{copy.title}</h2>
    <p>{copy.intro}</p><p>{copy.chat}</p><p>{copy.meeting}</p>
    <details><summary>{copy.detail}</summary><p>{copy.providers}</p><p>{copy.school}</p><p>{copy.record}</p></details>
    {#if phase === 'failed'}<p role="alert">{copy.failed}</p><button type="button" onclick={() => request()}>{copy.retry}</button>
    {:else if phase === 'loading'}<p role="status">{copy.loading}</p>
    {:else}<button type="button" disabled={phase === 'saving'} onclick={() => request(true)}>{phase === 'saving' ? copy.saving : copy.action}</button>{/if}
  </section>
{/if}

<style>
  .duif-first-use { max-height: 65vh; overflow: auto; flex-shrink: 0; padding: 1rem clamp(1rem,4vw,3rem); border-bottom: 1px solid #b7b1a0; background: #f4efe3; color: #252820; font-family: var(--font-sans, sans-serif); font-size: .875rem; line-height: 1.55; }
  h2 { margin: 0; font-size: 1.25rem; font-family: var(--font-serif,serif); font-weight: 500; }
  p { max-width: 110ch; margin: .65rem 0; }
  button { font: inherit; border: 1px solid #5d654c; border-radius: .4rem; padding: .55rem .8rem; background: #fffdf7; color: inherit; margin-top: .75rem; cursor: pointer; }
  button:disabled { opacity: .65; cursor: wait; }
  summary { cursor: pointer; text-decoration: underline; text-underline-offset: .2em; }
  :is(button,summary):focus-visible { outline: 3px solid #606d3c; outline-offset: 3px; }
</style>
