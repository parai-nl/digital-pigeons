<script lang="ts">
  import { onMount, type Component } from 'svelte'
  import { useServices } from '../services.svelte'

  // Only the visual module is deferred. The services context and graph store
  // already exist, including an incoming source focus while the chunk loads.
  let { loadGraph = () => import('./GraphView.svelte') }: { loadGraph?: () => Promise<{ default: Component }> } = $props()
  const { locale } = useServices()
  let View = $state<Component | null>(null)
  let phase = $state<'loading' | 'failed' | 'ready'>('loading')
  let pending = false
  let disposed = false

  async function load(): Promise<void> {
    if (pending || disposed || View) return
    pending = true
    phase = 'loading'
    try {
      const module = await loadGraph()
      if (!disposed) { View = module.default; phase = 'ready' }
    } catch {
      if (!disposed) phase = 'failed'
    } finally { pending = false }
  }
  onMount(() => { void load(); return () => { disposed = true } })
</script>

{#if View}<View />
{:else}
  <div class="graph-module-status">
    {#if phase === 'failed'}<p role="alert">{locale.t('graph.module.failed')}</p><button type="button" class="duif-quiet-button" onclick={() => void load()}>{locale.t('graph.retry')}</button>
    {:else}<p role="status">{locale.t('graph.module.loading')}</p>{/if}
  </div>
{/if}

<style>
  .graph-module-status { padding: 2rem; max-width: 38rem; margin: auto; }
  p { font-size: .95rem; line-height: 1.65; margin: 0 0 1rem; color: var(--duif-dim); }
</style>
