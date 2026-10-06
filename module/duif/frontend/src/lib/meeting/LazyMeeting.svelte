<script lang="ts">
  import { onMount } from 'svelte'
  type MeetingComponent = typeof import('./MeetingView.svelte').default
  import { useServices } from '../services.svelte'

  // Loading the meeting view never starts capture. The shared meeting store
  // and global stop/end controls remain mounted independently of this chunk.
  let { loadMeeting = () => import('./MeetingView.svelte') }: { loadMeeting?: () => Promise<{ default: MeetingComponent }> } = $props()
  const { locale } = useServices()
  let View = $state<MeetingComponent | null>(null)
  let phase = $state<'loading' | 'failed' | 'ready'>('loading')
  let pending = false
  let disposed = false

  async function load(): Promise<void> {
    if (pending || disposed || View) return
    pending = true
    phase = 'loading'
    try {
      const module = await loadMeeting()
      if (!disposed) { View = module.default; phase = 'ready' }
    } catch {
      if (!disposed) phase = 'failed'
    } finally { pending = false }
  }
  onMount(() => { void load(); return () => { disposed = true } })
</script>

{#if View}<View showPigeonSelector={false} />
{:else}
  <div class="meeting-module-status">
    {#if phase === 'failed'}<p role="alert">{locale.t('meeting.module.failed')}</p><button type="button" class="duif-quiet-button" onclick={() => void load()}>{locale.t('graph.retry')}</button>
    {:else}<p role="status">{locale.t('meeting.module.loading')}</p>{/if}
  </div>
{/if}

<style>
  .meeting-module-status { padding: 2rem; max-width: 38rem; margin: auto; }
  p { font-size: .95rem; line-height: 1.65; margin: 0 0 1rem; color: var(--duif-dim); }
</style>
