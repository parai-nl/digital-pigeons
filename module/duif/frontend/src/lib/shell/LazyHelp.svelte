<script lang="ts">
  import { onMount } from 'svelte'
  import type { UiLocale } from '../i18n.svelte'
  import type { RosterStore } from './roster.svelte'
  type HelpComponent = typeof import('./HelpView.svelte').default
  let { locale, roster, onclose, ongraph, meetingLive = false, loadHelp = () => import('./HelpView.svelte') }: {
    locale: UiLocale; roster: RosterStore; onclose: () => void; ongraph: () => void; meetingLive?: boolean
    loadHelp?: () => Promise<{ default: HelpComponent }>
  } = $props()
  let View = $state<HelpComponent | null>(null)
  let failed = $state(false)
  let pending = false
  let disposed = false
  async function load(): Promise<void> {
    if (pending || disposed || View) return
    pending = true
    failed = false
    try { const module = await loadHelp(); if (!disposed) View = module.default }
    catch { if (!disposed) failed = true }
    finally { pending = false }
  }
  onMount(() => { void load(); return () => { disposed = true } })
</script>

{#if View}<View {locale} {roster} {onclose} {ongraph} {meetingLive} />
{:else}<div class="help-module-status">
  <button type="button" class="duif-text-action" onclick={onclose}>{locale.t('shell.closeHelp')}</button>
  {#if failed}<p role="alert">{locale.t('shell.helpFailed')}</p><button type="button" class="duif-quiet-button" onclick={() => void load()}>{locale.t('graph.retry')}</button>
  {:else}<p role="status">{locale.t('shell.helpLoading')}</p>{/if}
</div>{/if}

<style>
  .help-module-status { padding: 2rem; max-width: 38rem; margin: auto; }
  p { font-size: .95rem; line-height: 1.65; margin: 1rem 0; color: var(--duif-dim); }
</style>
