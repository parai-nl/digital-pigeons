<script lang="ts">
  // De enige bedieningsset van het vergaderscherm: vier acties — beëindigen,
  // laat de duif reageren, de inbreng van de duif stoppen of hervatten,
  // microfoon aan/uit. Twee regels, één set: boven de twee handelingen die het
  // gesprek zelf sturen, groot en in het zicht; eronder het rustige regiewerk,
  // inclusief de geavanceerde instellingen.
  //
  // De twee stopknoppen zijn van plaats gewisseld. "Stop" stond groot
  // en rood vooraan maar zette alleen de duif stil, terwijl de knop die de
  // vergadering écht sluit klein op de tweede regel stond; wie op rood drukte
  // verwachtte dat het ophield. Nu draagt de grote rode knop "Vergadering
  // beëindigen" en staat "Stop inbreng" op de tweede regel, waar hij na gebruik
  // "Inbreng hervatten" wordt. De invariant van de betrouwbare lokale stop
  // verhuist mee: "Stop inbreng" is nooit uitgeschakeld en zwijgt hier vóórdat
  // de server iets bevestigt — wat er ook wegvalt.
  import ChatTeardropTextIcon from 'phosphor-svelte/lib/ChatTeardropTextIcon'
  import MicrophoneIcon from 'phosphor-svelte/lib/MicrophoneIcon'
  import MicrophoneSlashIcon from 'phosphor-svelte/lib/MicrophoneSlashIcon'
  import PhoneXIcon from 'phosphor-svelte/lib/PhoneXIcon'
  import PlayIcon from 'phosphor-svelte/lib/PlayIcon'
  import StopIcon from 'phosphor-svelte/lib/StopIcon'
  import WaveformIcon from 'phosphor-svelte/lib/WaveformIcon'
  import type { UiLocale } from '../i18n.svelte'
  import MeetingSettings from './MeetingSettings.svelte'
  import type { MeetingStore } from './meetingStore.svelte'

  let { store, locale }: { store: MeetingStore; locale: UiLocale } = $props()

  let stopEl: HTMLButtonElement | undefined = $state()

  // De knop van de inbreng heeft twee standen. Hij gaat pas op "hervatten" als
  // de server de stop ook kent: zolang die onderweg is of hem niet bereikte,
  // blijft hij "Stop inbreng" — de storingsmelding vraagt dan om nog een keer
  // stoppen, en dat moet deze knop dan ook doen.
  const resumable = $derived(store.contributionResumable)

  // De stand van de microfoon is meer dan de dempknop: staat de toegang dicht
  // of is hij geweigerd, dan luistert er niets, hoe de wens ook staat. De knop
  // is dan uitgeschakeld en mag geen open microfoon tonen. Drie standen dus:
  // 'live' (open en niet gedempt), 'muted' (open maar gedempt) en 'off' (er is
  // geen microfoon om te dempen).
  const micState = $derived(
    store.localMic === 'refused' || store.localMic === 'closed' ? 'off' : store.muted ? 'muted' : 'live',
  )

  // Zodra de duif aan tafel zit hoort de hand op de stilteknop te kunnen landen
  // zonder te zoeken. De focus blijft dus op "Stop inbreng" en verhuist niet
  // mee naar de grote rode knop: die beëindigt de vergadering, en een
  // toetsenbord dat na het aanschuiven meteen een spatie krijgt, hoort de duif
  // stil te zetten en niet de tafel op te breken.
  $effect(() => {
    stopEl?.focus()
  })

  // Waar Escape van iets anders is: een invoerveld wist zijn tekst, een open
  // keuzelijst of dialoog sluit zichzelf. De sneltoets van STOP mag daar niet
  // overheen.
  const EDITABLE =
    'input, textarea, select, [contenteditable="true"], [contenteditable=""], [role="combobox"], [role="listbox"], [role="dialog"], [role="alertdialog"]'

  function editable(target: EventTarget | null): boolean {
    return target instanceof Element && target.closest(EDITABLE) !== null
  }

  // Escape is de sneltoets van STOP, maar alleen wanneer de vergadering ook
  // echt op het scherm staat (orbVisible: de vergadertab is actief) en de
  // aanslag niet ergens anders thuishoort. Staat de vraag "Ben je er nog?" open, dan hoort
  // Escape bij die dialoog.
  $effect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key !== 'Escape' || ev.defaultPrevented) return
      if (store.presence !== null || !store.orbVisible) return
      if (editable(ev.target)) return
      ev.preventDefault()
      void store.stop()
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  })
</script>

<div class="meeting-actions" data-testid="meeting-actions">
  <div class="primary-row">
    <button type="button" class="primary end" data-testid="call-control-end" onclick={() => store.askEnd()}><span class="btn-icon" data-icon="end" aria-hidden="true"><PhoneXIcon size="1.15em" /></span>{locale.t('meeting.end')}</button>
    <button type="button" class="primary invite" aria-disabled={store.speaking || store.respondInFlight} aria-busy={store.respondInFlight} data-testid={store.speaks ? 'respond-button' : 'intervene-button'} onclick={() => !store.speaking && !store.respondInFlight && void (store.speaks ? store.respond() : store.intervene())}><span class="btn-icon" data-icon={store.speaks ? 'respond' : 'intervene'} aria-hidden="true">{#if store.speaks}<ChatTeardropTextIcon size="1.15em" />{:else}<WaveformIcon size="1.15em" />{/if}</span>{locale.t(store.speaks ? 'meeting.respond' : 'meeting.intervene')}</button>
  </div>
  <div class="secondary-row">
    <MeetingSettings {store} {locale} />
    <div class="secondary-right">
      <!-- Eén knop voor twee standen, zoals de uitnodigingsknop dat ook doet:
           zolang de duif meedoet staat er "Stop inbreng", daarna "Inbreng
           hervatten". Hij is nooit `disabled` — ook niet terwijl het hervatten
           nog loopt, want dan zou de stilteknop juist op het verkeerde moment
           wegvallen; `aria-busy` zegt dat er iets loopt en de store weigert de
           dubbele aanvraag zelf. -->
      <button type="button" class="duif-quiet-button stop-button" data-paused={resumable} aria-keyshortcuts={resumable ? undefined : 'Escape'} aria-busy={store.resumeInFlight} title={locale.t(resumable ? 'meeting.resume.hint' : 'meeting.stop.hint')} data-testid={resumable ? 'resume-button' : 'stop-button'} bind:this={stopEl} onclick={() => void (resumable ? store.resumeAutonomy() : store.stop())}><span class="btn-icon" data-icon={resumable ? 'resume' : 'stop'} aria-hidden="true">{#if resumable}<PlayIcon size="1.15em" weight="fill" />{:else}<StopIcon size="1.15em" weight="fill" />{/if}</span>{locale.t(resumable ? 'meeting.resume' : 'meeting.stop')}</button>
      <!-- Het opschrift noemt de handeling ("dempen"), het icoon toont de stand
           ("de microfoon staat open"). Zo is aan de knop zelf te zien of er nog
           geluisterd wordt, zonder dat de tekst iets anders belooft dan de klik
           doet. data-mic herhaalt die stand voor de tests. -->
      <button type="button" class="duif-quiet-button mute-button" data-testid="call-control-mute" data-mic={micState} aria-pressed={store.muted} onclick={() => store.setMuted(!store.muted)} disabled={micState === 'off'}><span class="btn-icon" data-icon="microphone-{micState}" aria-hidden="true">{#if micState === 'live'}<MicrophoneIcon size="1.15em" />{:else}<MicrophoneSlashIcon size="1.15em" />{/if}</span>{locale.t(store.muted ? 'meeting.unmute' : 'meeting.mute')}</button>
    </div>
  </div>
</div>

<style>
  .meeting-actions { position: relative; max-width: 72rem; margin: 0 auto; display: grid; gap: .55rem; }
  /* Eén iconenmaat voor de hele set: het icoon staat vóór het opschrift, schaalt
     mee met de tekstgrootte van zijn knop en is nooit zelf de betekenis — de
     tekst blijft het label, het icoon is aria-hidden. */
  .btn-icon { display: inline-flex; align-items: center; justify-content: center; flex: none; }
  .duif-quiet-button { display: inline-flex; align-items: center; justify-content: center; gap: .4rem; }
  /* Gedempt is een stand die je moet kunnen zien zonder te lezen: de doorgestreepte
     microfoon draagt de stopkleur, de open microfoon blijft rustige inkt. */
  .mute-button[data-mic='muted'] .btn-icon { color: var(--duif-stop); }
  .mute-button[data-mic='muted'] { border-color: color-mix(in oklab, var(--duif-stop) 45%, var(--duif-line)); }
  /* Zonder microfoon valt er niets te dempen: de knop is uit, en het teken
     zegt rustig dat er niet geluisterd wordt — geen alarmkleur voor iets waar
     de zaal hier niets aan kan doen; de melding erboven legt het uit. */
  .mute-button[data-mic='off'] { opacity: .6; }
  .mute-button[data-mic='off'] .btn-icon { color: var(--duif-dim); }
  /* Het opschrift van de rode knop is een hele zin in plaats van
     één woord; de linkerkolom krijgt daar de ruimte voor, zonder dat de
     uitnodiging haar overwicht verliest. */
  .primary-row { display: grid; grid-template-columns: minmax(9.5rem, 1fr) minmax(0, 1.9fr); gap: .75rem; }
  .primary { min-height: 4rem; border: 0; border-radius: 1rem; padding: .9rem 1.25rem; font-size: clamp(.95rem, 1.5vw, 1.3rem); font-weight: 650; letter-spacing: -.01em; line-height: 1.25; display: flex; align-items: center; justify-content: center; gap: .55rem; text-align: center; text-wrap: balance; transition: transform 120ms ease, box-shadow 120ms ease; }
  /* Beëindigen draagt de stopkleur: het is de handeling die de tafel opbreekt,
     en die hoort de opvallendste knop van het scherm te zijn. */
  .end { background: var(--duif-stop); color: #fff; box-shadow: 0 8px 22px -14px var(--duif-stop); }
  .invite { background: var(--duif-ink); color: var(--duif-paper); box-shadow: 0 8px 22px -14px var(--duif-ink); }
  /* In donker is --duif-ink bijna wit: een witte plaat zou de rode knop
     overstemmen. De uitnodiging zakt daar een tint terug naar het papier van de
     balk, zodat beëindigen in beide thema's de opvallendste knop blijft. */
  :global(.dark) .invite { background: color-mix(in oklch, var(--duif-feather) 82%, var(--duif-ink)); color: var(--duif-ink); border: 1px solid var(--duif-line); box-shadow: none; }
  /* "Stop inbreng" staat op de rustige regel maar is geen tekstknopje: hij
     draagt de stopkleur in zijn rand en icoon, zodat de hand hem terugvindt
     zonder te lezen. Hervatten laat die kleur los — er valt dan niets meer stil
     te zetten. */
  .stop-button .btn-icon { color: var(--duif-stop); }
  /* Van de rustige regel is dit de knop die onder druk gevonden moet worden:
     een tikje groter en zwaarder dan de rest van de regel, zonder een tweede
     hoofdknop te worden. */
  .stop-button { border-color: color-mix(in oklab, var(--duif-stop) 45%, var(--duif-line)); font-weight: 600; font-size: .8rem; min-height: 2.75rem; padding-inline: .85rem; }
  .stop-button[data-paused='true'] { border-color: var(--duif-line); font-weight: 550; }
  .stop-button[data-paused='true'] .btn-icon { color: var(--duif-listen); }
  .stop-button[aria-busy='true'] { opacity: .65; }
  .primary:hover { transform: translateY(-1px); box-shadow: 0 12px 26px -14px #0006; }
  /* De focusring moet op de grootste knop van het scherm van een afstand te
     zien zijn: een harde ring in de inktkleur, met de halo eromheen. */
  .end:focus-visible { outline: 3px solid var(--duif-ink); outline-offset: 3px; box-shadow: 0 0 0 7px var(--duif-stop-halo); }
  /* De focus landt bij het aanschuiven op "Stop inbreng"; die ring moet dus
     net zo hard te zien zijn als die van de grote knop. */
  .stop-button:focus-visible { outline: 3px solid var(--duif-ink); outline-offset: 2px; box-shadow: 0 0 0 5px var(--duif-stop-halo); }
  /* Een bezette duif blijft aanwijsbaar: de knop dooft, verdwijnt niet, en
     houdt haar focus (aria-disabled, geen disabled). */
  .invite[aria-disabled='true'] { opacity: .55; transform: none; box-shadow: none; cursor: default; }
  .secondary-row { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem 1rem; }
  .secondary-right { margin-left: auto; display: flex; flex-wrap: wrap; align-items: center; gap: .65rem; }
  @media(max-width: 650px) {
    .primary-row { grid-template-columns: minmax(7rem, 1fr) minmax(0, 1.4fr); gap: .5rem; }
    .primary { min-height: 3.5rem; padding: .7rem .8rem; border-radius: .8rem; }
    .secondary-right { margin-left: 0; }
  }
  @media(max-height: 650px) {
    .primary { min-height: 3.25rem; }
  }
</style>
