<script lang="ts">
  // "Ben je er nog?". De server stelt de vraag na tien minuten
  // zonder klik of toets en stopt de vergadering als niemand binnen de
  // aftelling antwoordt; deze dialoog toont alleen wat hij weet.
  //
  // Twee uitwegen. "Ja, ga door" heeft de focus, en Escape of een klik naast
  // het venster betekent hetzelfde: wie de vraag wegdrukt, is er duidelijk nog.
  // "Vergadering beëindigen" is de rode knop zonder de extra vraag of de duif
  // nog spreekt: wie hier beëindigen kiest, heeft die vraag al beantwoord.
  //
  // Een alertdialog, geen gewone: het is een onderbreking die om een antwoord
  // vraagt. bits-ui geeft die rol alleen via zijn eigen AlertDialog.Root; de
  // inhoud blijft het venster van @parai/ui. Het venster portalt naar <body>,
  // dus het staat er ook als de vergadertab verborgen is, en boven de uitleg.
  //
  // De kleuren komen uit de duifmodule (.duif-dialog* in app.css), niet uit de
  // schil: buiten .duif-root is --primary het merkrood van de gastheer, en dat
  // haalt in donker geen leesbaar contrast.
  import { AlertDialog } from 'bits-ui'
  import Button from '@parai/ui/components/Button.svelte'
  import * as Dialog from '@parai/ui/components/dialog'
  import type { UiLocale } from '../i18n.svelte'
  import { formatCountdown } from './labels'
  import { PRESENCE_SOON_MS, type MeetingStore } from './meetingStore.svelte'

  let { store, locale }: { store: MeetingStore; locale: UiLocale } = $props()

  const text = $derived(store.presence ? locale.pick(store.presence, locale.t('meeting.presence.body')) : '')
  const remaining = $derived(store.presenceRemainingMs)
  // De seconden zelf leest een schermlezer niet voor; wel één keer dat het
  // bijna zover is.
  const soon = $derived(remaining !== null && remaining <= PRESENCE_SOON_MS)

  // De focus gaat naar "Ja, ga door" en niet naar de eerste knop: links staat
  // beëindigen, en Enter op de vraag hoort de vergadering te laten lopen.
  function focusConfirm(event: Event): void {
    event.preventDefault()
    document.querySelector<HTMLElement>('[data-presence-confirm]')?.focus()
  }
</script>

<!-- De open-stand is de vraag zelf, niet een kopie ervan: een function binding
     leest hem uit de store en schrijft het sluiten er meteen in terug. -->
<AlertDialog.Root
  bind:open={
    () => store.presence !== null,
    (next: boolean) => {
      if (!next) void store.acknowledgePresence()
    }
  }
>
  <Dialog.Content class="duif-dialog" data-testid="presence-dialog" onOpenAutoFocus={focusConfirm}>
    <Dialog.Title>{locale.t('meeting.presence.title')}</Dialog.Title>
    <Dialog.Description class="duif-dialog-text">{text}</Dialog.Description>
    {#if remaining !== null}
      <p class="presence-countdown" data-testid="presence-countdown">
        {locale.t('meeting.presence.countdown', { time: formatCountdown(remaining) })}
      </p>
    {/if}
    <p class="sr-only" aria-live="polite">{soon ? locale.t('meeting.presence.soon') : ''}</p>
    <div class="mt-6 flex flex-wrap items-center justify-end gap-2">
      <Button variant="outline" class="duif-dialog-outline" onclick={() => store.end()} data-testid="presence-end">{locale.t('meeting.end')}</Button>
      <Button class="duif-dialog-primary" onclick={() => void store.acknowledgePresence()} data-presence-confirm data-testid="presence-confirm">
        {locale.t('meeting.presence.confirm')}
      </Button>
    </div>
  </Dialog.Content>
</AlertDialog.Root>

<style>
  /* De aftelling is wat je op afstand moet kunnen lezen, dus groot. Geen
     tabular-nums: de interfaceletter maakt dan ook spaties en de dubbele punt
     even breed als een cijfer, en de regel valt uit elkaar. De tijd staat
     achteraan, dus een cijfer dat smaller wordt verschuift niets. */
  .presence-countdown { margin: 1rem 0 0; font-size: 1.25rem; font-weight: 650; }
</style>
