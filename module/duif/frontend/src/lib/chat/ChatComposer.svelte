<script lang="ts">
  import type { Snippet } from 'svelte'
  import Banner from '@parai/ui/components/Banner.svelte'
  import ChatInput from '@parai/ui/patterns/ChatInput.svelte'
  import { useServices } from '../services.svelte'
  import { COUNTER_WARN_RUNES, MAX_MESSAGE_RUNES } from './chatStore.svelte'

  // De invoerbalk: één ChatInput met de harde limiet van 4096, een teller die
  // van muted naar waarschuwing naar destructief kleurt, en boven de invoer de
  // Banner met de laatste fout (cap, netwerk, servermelding). Geen toasts.
  //
  // `errorAction` is wat de lezer na die fout kan doen — vandaag "Bewerk de
  // laatste vraag". Hij hoort onder de melding zelf te staan en niet
  // ergens anders op het scherm, dus de chat geeft het fragment mee en wij
  // zetten het op zijn plek.
  const { chat, locale } = useServices()
  const { errorAction }: { errorAction?: Snippet } = $props()

  const count = $derived([...chat.draft].length)
  const tone = $derived(
    count >= MAX_MESSAGE_RUNES ? 'destructive' : count >= COUNTER_WARN_RUNES ? 'warning' : 'muted',
  )
  const counterColour = $derived(
    tone === 'destructive'
      ? 'var(--destructive)'
      : tone === 'warning'
        ? 'var(--warning)'
        : 'var(--muted-foreground)',
  )

  // ChatInput roept onSubmit('') aan wanneer de stopknop wordt ingedrukt.
  function submit(message: string): void {
    if (message === '') {
      chat.abort()
      return
    }
    void chat.send(message)
  }

  // ChatInput laat het veld meegroeien in zijn eigen `oninput`. Een concept dat
  // niet van het toetsenbord komt — een startkaart, een vervolgactie, "bewerk
  // de laatste vraag", een teruggehaald concept bij het wisselen van duif —
  // gaat via de binding en vuurt dus geen input-event: het veld bleef één regel
  // hoog met een scrollbalk terwijl er drie regels in stonden. We geven dat
  // event zelf, zodat de maatvoering (en het maximum van 200 px) van ChatInput
  // zelf blijft en hier niets verdubbelt.
  //
  // `typed` onthoudt wat het veld zelf het laatst meldde. Zonder dat zou elke
  // toetsaanslag een tweede, overbodig input-event krijgen: de browser meldt
  // eerst, wij daarna nog eens hetzelfde.
  let composer: HTMLDivElement | undefined = $state()
  let typed = ''
  function noteInput(event: Event): void {
    const field = event.target
    if (field instanceof HTMLTextAreaElement) typed = field.value
  }
  $effect(() => {
    const draft = chat.draft
    const field = composer?.querySelector('textarea')
    if (!field || field.value !== draft || draft === typed) return
    typed = draft
    field.dispatchEvent(new Event('input', { bubbles: true }))
  })
</script>

<div
  bind:this={composer}
  oninput={noteInput}
  class="duif-composer flex flex-col gap-2"
  style:--duif-counter={counterColour}
  data-counter-tone={tone}
  data-testid="chat-composer"
>
  {#if chat.error}
    <div class="duif-composer-error flex flex-col gap-1" data-testid="chat-error">
      <Banner
        message={chat.errorText}
        tone={chat.capExceeded ? 'warning' : 'danger'}
        role={chat.capExceeded ? 'alert' : 'status'}
        aria-live={chat.capExceeded ? 'assertive' : 'polite'}
        dismissible
        dismissLabel={locale.t('chat.dismiss')}
        onDismiss={() => (chat.error = null)}
        data-testid="chat-banner"
      />
      {#if errorAction}{@render errorAction()}{/if}
    </div>
  {/if}
  <ChatInput
    bind:value={chat.draft}
    placeholder={locale.t('chat.placeholder')}
    maxlength={MAX_MESSAGE_RUNES}
    loading={chat.streaming}
    disabled={chat.selectedPigeonId === '' || chat.loadingHistory || chat.confirmingComparison}
    onSubmit={submit}
  />
</div>

<style>
  /* De teller zit in ChatInput; alleen zijn kleur is van ons, zodat er niet
     twee tellers naast elkaar staan. */
  .duif-composer :global([data-testid='chat-char-counter']) {
    color: var(--duif-counter);
  }
</style>
