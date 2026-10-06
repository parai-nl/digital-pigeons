<script lang="ts">
  import { tick } from 'svelte'
  import ClockCounterClockwiseIcon from 'phosphor-svelte/lib/ClockCounterClockwiseIcon'
  import PlusIcon from 'phosphor-svelte/lib/PlusIcon'
  import SessionList from '../chat/SessionList.svelte'
  import type { SessionRef } from '../chat/chatStore.svelte'
  import { useServices } from '../services.svelte'
  import '../chat/chat.css'

  // Historie en "nieuw gesprek" zijn navigatie, geen gesprek: ze staan daarom
  // in de navigatieregel van de schil, op één lijn met de tabs, in plaats van
  // op een tweede regel boven de chat. De lijst zelf blijft SessionList; dit
  // onderdeel bepaalt wanneer hij openstaat en wat een klik betekent.
  //
  // Ze staan links, direct achter Chat en Vergadering, en niet rechts: deze
  // twee knoppen gaan over het gesprek waar de tabs over gaan, terwijl rechts staat wat naast het gesprek ligt — de kennisgraaf
  // en de uitleg. De lade hangt dus ook links uit.
  //
  // "Nieuw gesprek" vraagt niets: de tabs kiezen de modus. Even stond hier een
  // menu met Chat en Vergadering, maar dat was dezelfde keuze nog een
  // keer — bovenin staat zij al, en op het vergaderscherm is "Laat de duif
  // plaatsnemen" de start van een nieuwe vergadering. Deze knop gaat
  // dus alleen over de chat.
  const { chat, locale, roster } = useServices()

  let historyOpen = $state(false)
  let confirmingReset = $state(false)
  let deleting = $state<SessionRef | null>(null)
  let deleteBusy = $state(false)
  let deleteFailed = $state(false)
  let deleteDialog: HTMLDialogElement | undefined = $state()
  let resetDialog: HTMLDialogElement | undefined = $state()
  let deleteReturnFocus: HTMLElement | null = null
  let toggle: HTMLButtonElement | undefined = $state()
  let wrapper: HTMLElement | undefined = $state()

  const ownSessions = $derived(chat.sessions.filter((s) => s.pigeonId === chat.selectedPigeonId))

  $effect(() => {
    if (deleting) { deleteDialog?.showModal?.(); deleteDialog?.querySelector<HTMLButtonElement>('button')?.focus() }
    else deleteDialog?.close?.()
  })
  $effect(() => {
    if (confirmingReset) { resetDialog?.showModal?.(); resetDialog?.querySelector<HTMLButtonElement>('button')?.focus() }
    else resetDialog?.close?.()
  })
  // De lade hangt over het gesprek heen; een klik ernaast legt hem weer weg.
  // Een openstaande bevestiging is de baas: die vangt zijn eigen klikken.
  $effect(() => {
    if (!historyOpen) return
    const close = (event: Event): void => {
      if (deleting) return
      const target = event.target
      if (target instanceof Node && wrapper?.contains(target)) return
      historyOpen = false
    }
    document.addEventListener('pointerdown', close, true)
    return () => document.removeEventListener('pointerdown', close, true)
  })

  function sessionTitle(session: SessionRef): string {
    return session.title || session.comparison?.originalQuestion || roster.name(session.pigeonId)
  }
  // De navigatieregel staat buiten het chatpaneel; het invoerveld dus ook. Na
  // een nieuw gesprek hoort de cursor daar te staan, en dat is de enige plek
  // waar dit onderdeel het chatscherm aanraakt.
  async function focusComposer(): Promise<void> {
    await tick()
    document.getElementById('duif-panel-chat')?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
  }
  function newConversation(): void {
    if (chat.streaming || chat.draft.trim()) { confirmingReset = true; return }
    startConversation()
  }
  function startConversation(): void {
    chat.newConversation()
    confirmingReset = false
    historyOpen = false
    void focusComposer()
  }
  function closeDelete(): void {
    deleting = null
    deleteFailed = false
    void tick().then(() => {
      if (deleteReturnFocus?.isConnected) deleteReturnFocus.focus()
      else toggle?.focus()
    })
  }
  async function confirmDelete(): Promise<void> {
    if (!deleting || deleteBusy) return
    const id = deleting.id
    deleteBusy = true; deleteFailed = false
    const ok = await chat.deleteSession(id)
    deleteBusy = false
    if (ok) closeDelete(); else deleteFailed = true
  }
</script>

<svelte:window onkeydown={(event) => {
  if (!historyOpen || deleting || confirmingReset || event.key !== 'Escape' || event.defaultPrevented) return
  event.preventDefault()
  historyOpen = false
  toggle?.focus()
}} />

<div class="conversation-actions" bind:this={wrapper} role="group" aria-label={locale.t('chat.sessions.title')}>
  <button
    type="button"
    class="duif-nav-item"
    bind:this={toggle}
    aria-label={locale.t('chat.history.toggle')}
    aria-expanded={historyOpen}
    aria-controls="chat-history"
    onclick={() => historyOpen = !historyOpen}
  >
    <ClockCounterClockwiseIcon size="1.05rem" aria-hidden="true" />
    <span class="duif-nav-label">{locale.t('chat.history.toggle')}</span>
    {#if ownSessions.length}<span class="action-count" aria-hidden="true">{ownSessions.length}</span>{/if}
  </button>
  <button type="button" class="duif-nav-item" aria-label={locale.t('chat.new')} onclick={newConversation}>
    <PlusIcon size="1.05rem" aria-hidden="true" />
    <span class="duif-nav-label">{locale.t('chat.new')}</span>
  </button>
  {#if historyOpen}
    <div class="history-panel">
      <SessionList
        sessions={ownSessions}
        title={sessionTitle}
        onopen={(session) => { historyOpen = false; void chat.openSession(session.id) }}
        ondelete={(session, trigger) => { deleteReturnFocus = trigger; deleting = session; deleteFailed = false }}
      />
    </div>
  {/if}
</div>

<dialog bind:this={resetDialog} class="duif-chat-dialog" aria-labelledby="chat-reset-title" oncancel={() => confirmingReset = false}>
  <h2 id="chat-reset-title">{locale.t('chat.reset.title')}</h2>
  <p>{locale.t('chat.reset.body')}</p>
  <footer>
    <button type="button" class="duif-text-action" onclick={() => confirmingReset = false}>{locale.t('chat.reset.cancel')}</button>
    <button type="button" class="chat-primary" onclick={startConversation}>{locale.t('chat.new')}</button>
  </footer>
</dialog>

<dialog bind:this={deleteDialog} class="duif-chat-dialog" aria-labelledby="chat-delete-title" oncancel={(event) => { if (deleteBusy) event.preventDefault(); else closeDelete() }}>
  <h2 id="chat-delete-title">{locale.t('chat.delete.title')}</h2><p>{locale.t('chat.delete.body')}</p>
  {#if deleting}<blockquote>{sessionTitle(deleting)}</blockquote>{/if}
  {#if deleteFailed}<p role="alert">{locale.t('chat.delete.failed')}</p>{/if}
  <footer>
    <button type="button" class="duif-text-action" disabled={deleteBusy} onclick={closeDelete}>{locale.t('chat.delete.cancel')}</button>
    <button type="button" class="duif-danger-action" disabled={deleteBusy} onclick={confirmDelete}>{deleteBusy ? locale.t('chat.delete.busy') : locale.t('chat.delete.action')}</button>
  </footer>
</dialog>

<style>
  .conversation-actions { position: relative; display: flex; align-items: center; gap: .1rem; }
  .action-count { color: var(--duif-dim); font-variant-numeric: tabular-nums; }
  /* De lade hangt onder de regel, boven het gesprek: de navigatieregel mag het
     scherm niet opnieuw een regel hoger duwen zodra je hem opent. Hij lijnt
     links uit, want daar staat de knop nu. */
  .history-panel { position: absolute; z-index: 20; top: calc(100% + .1rem); left: 0; width: min(26rem, 88vw); filter: drop-shadow(0 12px 30px rgba(0, 0, 0, .12)); }
  blockquote { margin: 0; padding: .8rem 1rem; background: var(--duif-feather); border-radius: .5rem; font-size: .9rem; line-height: 1.5; }
  @media (max-width: 650px) {
    .history-panel { width: min(22rem, 92vw); }
  }
</style>
