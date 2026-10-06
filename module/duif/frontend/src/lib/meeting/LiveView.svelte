<script lang="ts">
  import Banner from '@parai/ui/components/Banner.svelte'
  import Button from '@parai/ui/components/Button.svelte'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import type { UiLocale } from '../i18n.svelte'
  import MeetingPigeon from '../media/MeetingPigeon.svelte'
  import type { RosterStore } from '../shell/roster.svelte'
  import { listenKey } from './labels'
  import ListenIndicator from './ListenIndicator.svelte'
  import MeetingActions from './MeetingActions.svelte'
  import MeetingAnswer from './MeetingAnswer.svelte'
  import MeetingCandidate from './MeetingCandidate.svelte'
  import type { MeetingStore } from './meetingStore.svelte'
  import PresenceDialog from './PresenceDialog.svelte'
  import SessionTranscript from './SessionTranscript.svelte'
  let { store, locale, roster }: { store: MeetingStore; locale: UiLocale; roster: RosterStore } = $props()
  // Het schuifgebied gaat mee naar het sessieverloop: dat meet zijn uitgeklapte
  // hoogte aan wat hier nog zichtbaar is.
  let scrollEl: HTMLDivElement | undefined = $state()
  const latest = $derived(store.answers.at(-1))
  // De kaart van de duif die net van tafel ging. Zij staat er alleen
  // zolang de nieuwe duif nog niets gezegd heeft, en zij telt niet mee voor de
  // reactie van het portret: de nieuwe duif reageert niet op andermans woorden.
  const carried = $derived(latest ? null : store.carried)
  const reactionId = $derived(!store.speaks
    ? store.signal ? `signal:${store.sessionId}:${store.signal.id}` : null
    : latest?.text ? `answer:${store.sessionId}:${latest.id}` : null)
  const visualEnabled = $derived(store.live && !store.disposed && store.orbVisible)
  // De openingsvraag doet haar werk zolang de zaal nog stil is; zodra er
  // gepraat wordt neemt het gesprek het over en blijft er één rustige regel
  // staan die zegt waar het antwoord landt.
  const started = $derived(store.transcript.length > 0 || store.partial !== null)
  // Eén melding tegelijk; de store kent de volgorde.
  const notice = $derived(store.notice)
  // De duifwissel: de dialoog ís de vraag die in de store staat, en de
  // namen in de tekst zijn die van de twee duiven waar het over gaat.
  const switchNames = $derived({
    pigeon: roster.name(store.pendingSwitch || store.pigeonId),
    previous: roster.name(store.pigeonId),
  })
</script>

<div class="meeting-live" data-testid="live-view" data-playback={store.playbackStatus}>
  <!-- Een benoemde schuifregio mag focus krijgen: zonder tabindex is het gebied
       niet met het toetsenbord te scrollen. De waarschuwing gaat over
       niet-interactieve elementen zonder rol en is hier een vals positief. -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="stage-scroll" tabindex="0" role="region" aria-label={locale.t('meeting.stage.content')} bind:this={scrollEl}>
    <div class="meeting-notices" data-testid="meeting-banners">
      {#if notice}
        <Banner
          tone={notice.tone}
          role={notice.tone === 'danger' ? 'alert' : undefined}
          aria-live={notice.tone === 'danger' ? 'assertive' : 'polite'}
          message={notice.text}
          data-testid="meeting-notice"
          data-notice={notice.id}
        >
          {#snippet trailing()}
            {#if notice.action === 'sound'}<Button size="sm" onclick={() => void store.enableAudio()} data-testid="audio-enable">{locale.t('meeting.audio.enable')}</Button>
            {:else if notice.action === 'microphone'}<Button size="sm" onclick={() => void store.retryMicrophone()} data-testid="mic-retry">{locale.t('meeting.retry')}</Button>{/if}
          {/snippet}
        </Banner>
      {/if}
    </div>
    <MeetingCandidate {store} {locale} />

    <div class="stage-scene" class:signaling={!store.speaks}>
      <section class="pigeon-stage" aria-label={roster.name(store.pigeonId)}>
        <div class="stage-identity">
          <h2>{roster.name(store.pigeonId)}</h2>
          <!-- De staat van de duif staat bij de duif zelf, in één woord:
               zij doet even niet mee. De luisterindicator ernaast
               gaat over de microfoon en blijft een ander verhaal — stil zijn en
               niet meeluisteren is niet hetzelfde. -->
          {#if store.autonomyPaused}<span class="quiet-state" data-testid="quiet-state">{locale.t('meeting.state.quiet')}</span>{/if}
          <ListenIndicator listen={store.listen} label={locale.t(listenKey(store.listen, store.localMic))} />
        </div>
        <MeetingPigeon pigeon={store.pigeonId} {reactionId} stopRevision={store.mediaStopRevision} enabled={visualEnabled} paused={store.stopped || store.stopPending} lang={locale.lang} />
      </section>
      {#if store.speaks}<div class="stage-answer">
        {#if latest}{#key latest.id}<MeetingAnswer answer={latest} {locale} prominent />{/key}
        {:else if carried}<div class="stage-carried" data-testid="carried-answer">
          <p class="carried-note">{locale.t('meeting.switch.carried', { previous: roster.name(carried.pigeonId), pigeon: roster.name(store.pigeonId) })}</p>
          {#key carried.id}<MeetingAnswer answer={carried} {locale} prominent />{/key}
        </div>
        {:else}<div class="stage-empty">
          <span class="duif-eyebrow">{locale.t('meeting.stage.ready')}</span>
          {#if started}<p class="stage-underway" data-testid="stage-underway">{locale.t('meeting.stage.listening')}</p>
          {:else}<h3>{locale.t('meeting.stage.invitation')}</h3><p>{locale.t('meeting.stage.hint')}</p>{/if}
        </div>{/if}
      </div>{/if}
      <!-- Het verloop hoort in de linkerkolom, onder de duif en haar
           mediaverantwoording: dan houdt het de breedte van die kolom en blijft
           de rechterkolom helemaal van het antwoord. In de DOM staat het ná het
           antwoord, want op een smal scherm valt de kolom uit elkaar en dan
           hoort het verloop onder het antwoord, niet ertussen; het raster zet
           het op brede schermen terug op zijn plek links. -->
      <div class="stage-transcript"><SessionTranscript {store} {locale} scroller={scrollEl} /></div>
    </div>
  </div>
  <!-- Geen uitlegregel meer boven de bediening: de stand van de duif staat bij
       de duif ("Stil") en de knop zegt zelf wat hij doet ("Inbreng hervatten").
       De regel die hier stond legde uit wat de knop toen niet zei. -->
  <footer class="meeting-control-footer">
    <MeetingActions {store} {locale} />
  </footer>
</div>
<PresenceDialog {store} {locale} />
<!-- Beëindigen is nu de grote rode knop. Spreekt de duif nog, dan vraagt hij
     één keer door: die klik is dan waarschijnlijk bedoeld om haar stil te
     krijgen, en de vergadertekst is na beëindigen weg. Die uitweg staat als
     derde knop in de vraag zelf: de focus zit in de dialoog, dus naar een knop
     erbuiten wijzen zou een doodlopende aanwijzing zijn. -->
<ConfirmDialog
  open={store.endConfirm}
  testid="end-dialog"
  title={locale.t('meeting.end.title')}
  body={locale.t('meeting.end.body')}
  confirm={locale.t('meeting.end.confirm')}
  cancel={locale.t('meeting.end.cancel')}
  alternative={locale.t('meeting.stop')}
  onconfirm={() => store.end()}
  oncancel={() => store.cancelEnd()}
  onalternative={() => { store.cancelEnd(); void store.stop() }}
/>
<ConfirmDialog
  open={store.pendingSwitch !== ''}
  testid="switch-dialog"
  title={locale.t('meeting.switch.title')}
  body={locale.t('meeting.switch.body', switchNames)}
  confirm={locale.t('meeting.switch.confirm', switchNames)}
  cancel={locale.t('meeting.switch.cancel', switchNames)}
  busy={store.switchInFlight}
  onconfirm={() => void store.confirmPigeonSwitch()}
  oncancel={() => store.cancelPigeonSwitch()}
/>

<style>
  /* Dezelfde kleurcodering als het startscherm en het hoofdmenu, maar hier heel
     dun: het podium mag de duif herkenbaar houden zonder onrustig te worden.
     De tint kost wel contrast: de gedempte tekst (kopjes van het verloop, het
     uitklapje, de gezegde regels, de modelnotitie, de wachtzin) zakte er in de
     lichte stand naar 4,30–4,34 en dat is onder de 4,5 die leesbaarheid vraagt.
     Een tiende inkt erbij tilt alle gedempte tekst op het podium terug naar
     4,77–4,81, en in donker blijft het ruim. Mengen gebeurt met
     --duif-dim-base, de themakleur uit app.css: --duif-dim mag hier niet naar
     zichzelf verwijzen, want zo'n verwijzing maakt de eigenschap ongeldig. */
  .meeting-live { --duif-dim: color-mix(in oklab, var(--duif-ink) 10%, var(--duif-dim-base)); min-height: 0; flex: 1; display: flex; flex-direction: column; background: color-mix(in oklab, var(--duif-identity, var(--duif-persona-biological)) 5%, var(--duif-stage)); }
  /* Het schuifgebied is de maat van het podium: de duif mag zo hoog worden als
     er onder de duivenkaarten en boven de bediening ruimte is. */
  .stage-scroll { overflow-y: auto; min-height: 0; flex: 1; padding: clamp(1rem,2.5vw,2rem); container-type: size; }
  /* Alles boven de scène deelt haar leesbreedte (78rem): de meldingsbalk en
     het kandidaatblok staan anders 64 pixels ingesprongen ten opzichte van de
     twee kolommen eronder, en dan lijkt het scherm scheef. */
  .meeting-notices { max-width: 78rem; margin: 0 auto; display: flex; flex-direction: column; gap: .5rem; }
  .meeting-notices:not(:empty) { margin-bottom: 1rem; }
  /* Twee kolommen van boven tot onder: links de duif met haar mediaverantwoording
     en daaronder het verloop, rechts het antwoord over de volle hoogte. De
     rasternamen doen het werk, zodat de DOM-volgorde (duif, antwoord, verloop)
     ook op een smal scherm de goede leesvolgorde is. */
  .stage-scene { display: grid; grid-template-columns: minmax(14rem,1fr) minmax(20rem,1.15fr); grid-template-rows: auto 1fr; grid-template-areas: 'pigeon answer' 'transcript answer'; align-items: start; column-gap: clamp(1rem,3vw,3rem); row-gap: .35rem; max-width: 78rem; margin: 0 auto 1rem; }
  .stage-scene.signaling { display: block; }
  /* De kaart van de vorige duif staat er als een naschrift: dezelfde kaart,
     met één regel erboven die zegt van wie hij is en waarom hij er nog staat. */
  .stage-carried { min-width: 0; display: grid; gap: .5rem; }
  .carried-note { margin: 0; color: var(--duif-dim); font-size: .78rem; line-height: 1.5; }
  .pigeon-stage { grid-area: pigeon; min-width: 0; align-self: start; }
  .signaling .pigeon-stage { max-width: 58rem; margin: 0 auto; }
  /* Het podium is nooit smaller dan de kadrering verdraagt. Bepaalt de
     breedte van de beweging de kadrering in plaats van de hoogte, dan staat de
     duif kleiner dan haar rustposter (die op de hoogte schaalt) en verspringt
     het beeld bij het laden. Gemeten per duif ligt die grens
     (heightLimitedFrom) voor de pratende duiven (voor en driekwart) rond 0,7 en
     voor de signalerende duif in zijaanzicht op 1,22. Met marge dus minstens 0,8 en 1,3
     keer zo breed als hoog; de maat is die van de eigen kolom. */
  .pigeon-stage { container-type: inline-size; }
  .stage-scene :global(.pigeon-canvas) { max-height: calc(100cqw / .8); }
  .signaling :global(.pigeon-canvas) { max-height: calc(100cqw / 1.3); }
  .stage-identity { display: flex; align-items: center; justify-content: center; flex-wrap: wrap; gap: .4rem 1rem; min-height: 2rem; margin: 0 0 .25rem; }
  h2 { font-size: .8rem; font-weight: 600; margin: 0; }
  .stage-identity :global(.text) { font-size: .75rem; }
  .stage-identity :global(.dot) { width: .55rem; height: .55rem; border-width: 2px; animation: none; }
  /* Op een telefoon houdt ook de signalerende duif de podiumhoogte van
     MeetingPigeon: de drie staan dan even groot, en het podium blijft breder
     dan hoog, zodat de rustposter precies over het model valt.
     Het podium is voor alle drie de duiven even hoog: de hoogte van het
     schuifgebied min de naamregel, de verantwoording en de marges, tussen
     17 en 34 rem. De kadrering (STAGE_COMPOSITION) laat de duif in rust dan
     vier vijfde van die hoogte vullen, zolang haar bewegingen erin passen. De
     eerste regel is de terugval voor browsers zonder containereenheden. */
  @media (min-width: 701px) {
    .stage-scene :global(.pigeon-canvas) { height: clamp(17rem, 44vh, 34rem); height: clamp(17rem, calc(100cqh - 7.5rem), 34rem); }
  }
  .stage-answer { grid-area: answer; min-width: 0; align-self: stretch; padding-block: 2rem; }
  .stage-transcript { grid-area: transcript; min-width: 0; padding-top: .25rem; }
  .stage-transcript :global(.session-transcript) { max-width: none; margin-top: 0; }
  /* Zonder antwoordkolom (de signalerende duif) staat het verloop gewoon onder
     de duif, op dezelfde leesbreedte als het portret. */
  .signaling .stage-transcript { max-width: 58rem; margin: 0 auto; }
  .stage-empty { padding: clamp(1rem,3vw,3rem); text-align: left; }
  /* De openingsvraag boven de duif deelt haar maat met de kop van de startkaart
     (--duif-type-stage-title, app.css): op een laptop beslaat zij zo hoogstens
     twee regels en blijft het podium een gesprek in plaats van een affiche. */
  h3 { font-family: var(--font-serif,Georgia,serif); font-size: var(--duif-type-stage-title); font-weight: 500; line-height: 1.35; max-width: 34ch; margin: .8rem 0; }
  .stage-empty p { font-size: .85rem; line-height: 1.65; color: var(--duif-dim); max-width: 42ch; }
  .stage-underway { margin-top: .6rem; }
  /* De bediening staat buiten het schuifgebied: uitklappen van het verloop mag
     de twee hoofdknoppen nooit uit beeld duwen. */
  .meeting-control-footer { flex: none; position: relative; z-index: 2; padding: .8rem clamp(1rem,3vw,2rem); border-top: 1px solid var(--duif-line); background: var(--duif-paper); }
  /* Eén woord naast de naam, in de rust van het podium: geen alarmkleur, want
     stil zijn is een stand en geen storing. */
  .quiet-state { font-size: .75rem; font-weight: 600; color: var(--duif-dim); padding: .1rem .5rem; border: 1px solid var(--duif-line); border-radius: 999px; }
  @media(max-width: 700px) {
    /* Eén kolom: duif, antwoord, en pas dan het verloop — het verloop komt nooit
       tussen de duif en haar antwoord in te staan. */
    .stage-scene { grid-template-columns: 1fr; grid-template-rows: auto; grid-template-areas: 'pigeon' 'answer' 'transcript'; gap: .25rem; margin-bottom: .8rem; }
    .stage-answer { padding-block: .25rem .8rem; }
    .stage-empty { padding: 1rem; text-align: center; }
    h3,.stage-empty p { margin-inline: auto; }
    .meeting-control-footer { padding: .6rem .8rem; }
  }
</style>
