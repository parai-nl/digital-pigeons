<script lang="ts">
  // De uitleg is een venster over de ervaring heen, geen tweede pagina.
  // Een uitleg die het gesprek volledig vervangt, met één tekstlink
  // rechtsboven, leest als een scherm waaruit geen weg terug is.
  // Nu schemert de ervaring eronder door — een lopende vergadering blijft
  // zichtbaar doorlopen — en zijn er vier uitgangen: het kruis rechtsboven,
  // een klik naast het venster, Escape (die vangt de schil) en de tekstlink
  // onderaan de uitleg.
  //
  // Bewust geen <dialog showModal>: die leeft in de top layer van de browser
  // en legt zijn dimlaag dus ook over de schil van de gastheer, terwijl deze
  // module niet verder hoort te reiken dan haar eigen wortel. Dit venster is
  // een laag binnen .duif-root, met aria-modal en een eigen toetsenbalval.
  //
  // De vensterregels hangen aan het venster (window) en het document, niet aan
  // de opmaak: zo sluit ook een klik die buiten de module valt de uitleg, en
  // vangt de toetsenbalval de Tab-toets ook wanneer de focus ergens anders
  // vandaan komt. Dezelfde constructie als de historielade in
  // ConversationActions.
  //
  // De toetsen worden in de vangfase gelezen, vóór de rest van de module: de
  // sneltoets van STOP op het vergaderscherm hoort niet af te gaan wanneer
  // Escape het uitlegvenster sluit. Daarom ook preventDefault().
  import { type Snippet } from 'svelte'
  import XIcon from 'phosphor-svelte/lib/XIcon'
  import type { UiLocale } from '../i18n.svelte'

  let { open, locale, onclose, children }: {
    open: boolean
    locale: UiLocale
    onclose: () => void
    children: Snippet
  } = $props()

  let frame: HTMLElement | undefined = $state()
  let closeButton: HTMLButtonElement | undefined = $state()

  // Wat binnen het venster de Tab-toets mag ontvangen. `summary` staat erbij
  // omdat de vragenlijst uit uitklappers bestaat; een tak die verborgen is
  // (de laadstand van de uitleg) telt niet mee.
  const FOCUSABLE = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])'

  // Een gesloten venster blijft gemonteerd maar verborgen, en een verborgen
  // video speelt gewoon door, met geluid. Dicht is dus ook stil.
  $effect(() => {
    if (open) return
    for (const media of frame?.querySelectorAll<HTMLMediaElement>('video, audio') ?? []) if (!media.paused) media.pause()
  })

  // De focus gaat het venster in zodra het opengaat, op het sluitkruis: de
  // eerste bediening van het venster en meteen de uitgang. De weg terug naar
  // de knop "Uitleg & hulp" regelt de schil die het venster sloot. Bewust
  // zonder tick(): wie ná deze beurt nog een eigen focus zet (de terugweg uit
  // de kennisgraaf) hoort te winnen.
  //
  // De uitleg begint ook weer bovenaan. Zij blijft gemonteerd, zodat een
  // uitgeklapte vraag een bezoek overleeft, maar de leespositie is van dit
  // bezoek: bij een tweede opening hoort de lezer weer de kop en de drie
  // duiven te zien, en niet de alinea waar hij de vorige keer stopte.
  $effect(() => {
    if (!open) return
    for (const child of frame?.children ?? []) if (child instanceof HTMLElement) child.scrollTop = 0
    closeButton?.focus()
  })

  $effect(() => {
    if (!open) return
    const inside = (target: EventTarget | null): boolean => target instanceof Node && frame?.contains(target) === true
    // Ligt er een dringende vraag over de uitleg heen — "Ben je er nog?" is een
    // alertdialog en komt ook boven de uitleg — dan zijn muis en
    // toetsen van die vraag: haar Escape, haar Tab-volgorde, haar klik ernaast.
    // De uitleg doet dan een stap opzij en blijft eronder openstaan.
    const covered = (): boolean => document.querySelector('[role="alertdialog"][aria-modal="true"]') !== null
    // Een klik naast het venster sluit de uitleg, maar pas bij de klik zelf en
    // niet al bij het indrukken. De browser verzet tijdens een muisklik zelf de
    // focus; wie eerder sluit, zet de focus terug op "Uitleg & hulp" en ziet
    // hem daarna alsnog door de standaardactie naar <body> wegvallen. Het
    // indrukken telt wel mee: begon het binnen het venster (tekst selecteren
    // die buiten de rand eindigt), dan is dit geen klik ernaast.
    let pressedAway = false
    const press = (event: Event): void => { pressedAway = !covered() && !inside(event.target) }
    const away = (event: Event): void => {
      const outside = pressedAway && !covered() && !inside(event.target)
      pressedAway = false
      if (outside) onclose()
    }
    const keys = (event: KeyboardEvent): void => {
      if (event.defaultPrevented || !frame || covered()) return
      if (event.key === 'Escape') { event.preventDefault(); onclose(); return }
      if (event.key !== 'Tab') return
      const items = [...frame.querySelectorAll<HTMLElement>(FOCUSABLE)].filter((el) => !el.closest('[hidden]'))
      if (!items.length) return
      const first = items[0]
      const last = items[items.length - 1]
      const active = document.activeElement
      if (event.shiftKey && (active === first || !inside(active))) { event.preventDefault(); last.focus() }
      else if (!event.shiftKey && (active === last || !inside(active))) { event.preventDefault(); first.focus() }
    }
    document.addEventListener('pointerdown', press, true)
    document.addEventListener('click', away, true)
    window.addEventListener('keydown', keys, true)
    return () => {
      document.removeEventListener('pointerdown', press, true)
      document.removeEventListener('click', away, true)
      window.removeEventListener('keydown', keys, true)
    }
  })
</script>

<div class="help-overlay" hidden={!open} data-testid="help-overlay">
  <div class="help-frame" bind:this={frame} id="duif-help" role="dialog" aria-modal="true" aria-label={locale.t('shell.help')}>
    <button type="button" class="help-close" bind:this={closeButton} aria-label={locale.t('shell.helpClose')} data-testid="help-close" onclick={onclose}>
      <XIcon size="1.1rem" aria-hidden="true" />
    </button>
    {@render children()}
  </div>
</div>

<style>
  /* De laag ligt over de hele module, niet over de pagina: de schil van de
     gastheer blijft van de gastheer. Zij vangt ook de muis, zodat wat eronder
     schemert niet per ongeluk bediend wordt — één klik legt de uitleg weg en
     daarna is alles weer van jou. */
  .help-overlay { position: absolute; inset: 0; z-index: 50; display: flex; align-items: center; justify-content: center; padding: clamp(.5rem, 3vh, 2.5rem) clamp(.5rem, 3vw, 2.5rem); background: var(--duif-scrim); overflow: auto; overscroll-behavior: contain; }
  /* De rand draagt de scheiding met de gedimde achtergrond en heeft dus een
     eigen kleur nodig: in donker thema haalt de gewone lijnkleur de 3:1 van
     WCAG 1.4.11 niet tegen wat er onder de dimlaag ligt (app.css). */
  .help-frame { position: relative; display: flex; flex-direction: column; width: min(64rem, 100%); max-height: 100%; min-height: 0; background: var(--duif-paper); border: 1px solid var(--duif-dialog-edge); border-radius: var(--duif-radius-stage); box-shadow: 0 30px 70px -28px #00000080; overflow: hidden; }
  /* Het kruis hangt aan het venster en niet aan de tekst: het blijft dus staan
     terwijl de uitleg eronder doorscrollt. */
  .help-close { position: absolute; z-index: 2; top: .85rem; right: .85rem; display: grid; place-items: center; width: 2.4rem; height: 2.4rem; border: 1px solid var(--duif-line); border-radius: 50%; background: var(--duif-paper); color: var(--duif-dim); }
  .help-close:hover { background: var(--duif-feather); color: var(--duif-ink); }
  @media (max-width: 650px) {
    .help-overlay { padding: .5rem; }
    .help-frame { border-radius: var(--duif-radius-card); }
  }
</style>
