<script lang="ts">
  // Eén vraag, twee antwoorden. Gedeeld door de duifwissel en de uitweg
  // uit "er is al een vergadering open": allebei doen ze iets wat de
  // gebruiker niet ongevraagd hoort te overkomen — een lopende vergadering
  // beëindigen — dus allebei stellen ze eerst dezelfde vraag, op dezelfde
  // manier. Escape en de kruisknop zijn "nee": niets verandert.
  //
  // Eén vraag kan een derde uitweg hebben: beëindigen vraagt na
  // zolang de duif spreekt, en dan is "alleen haar stil zetten" vaak wat je
  // bedoelde. Die knop staat links, apart van het ja/nee-paar, want hij is
  // geen antwoord op de vraag maar een andere weg. Laat je hem weg, dan is de
  // dialoog precies wat hij was.
  //
  // De kleuren komen uit de duifmodule (.duif-dialog* in app.css), net als bij
  // "Ben je er nog?": het venster portalt naar <body>, buiten .duif-root, en
  // daar zijn de knoppen van de bibliotheek merkrood en linkblauw — in donker
  // 3,4 en 3,5:1 op het venster.
  import Button from '@parai/ui/components/Button.svelte'
  import * as Dialog from '@parai/ui/components/dialog'

  let {
    open,
    title,
    body,
    confirm,
    cancel,
    busy = false,
    testid,
    alternative,
    onconfirm,
    oncancel,
    onalternative,
  }: {
    open: boolean
    title: string
    body: string
    confirm: string
    cancel: string
    busy?: boolean
    testid: string
    alternative?: string
    onconfirm: () => void
    oncancel: () => void
    onalternative?: () => void
  } = $props()
</script>

<Dialog.Root
  bind:open={
    () => open,
    (next: boolean) => {
      if (!next) oncancel()
    }
  }
>
  <Dialog.Content class="duif-dialog" data-testid={testid}>
    <Dialog.Title>{title}</Dialog.Title>
    <Dialog.Description class="duif-dialog-text">{body}</Dialog.Description>
    <div class="mt-6 flex flex-wrap items-center justify-end gap-2">
      {#if alternative && onalternative}
        <Button variant="ghost" class="duif-dialog-ghost" onclick={onalternative} data-testid={testid + '-alternative'}
          >{alternative}</Button
        >
        <span class="grow"></span>
      {/if}
      <Button variant="outline" class="duif-dialog-outline" onclick={oncancel} data-testid={testid + '-cancel'}>{cancel}</Button>
      <Button class="duif-dialog-primary" onclick={onconfirm} disabled={busy} aria-busy={busy} data-testid={testid + '-confirm'}
        >{confirm}</Button
      >
    </div>
  </Dialog.Content>
</Dialog.Root>
