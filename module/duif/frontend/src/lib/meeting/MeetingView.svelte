<script lang="ts">
  import { onMount } from 'svelte'
  import Banner from '@parai/ui/components/Banner.svelte'
  import Select from '@parai/ui/components/Select.svelte'
  import { useServices } from '../services.svelte'
  import PigeonPortrait from '../shell/PigeonPortrait.svelte'
  import AutonomyField from './AutonomyField.svelte'
  import ConfirmDialog from './ConfirmDialog.svelte'
  import { deviceCheckKey } from './labels'
  import LiveView from './LiveView.svelte'
  import SettingsFields from './SettingsFields.svelte'
  let { showPigeonSelector = true }: { showPigeonSelector?: boolean } = $props()
  const { locale, meeting, roster } = useServices()
  onMount(() => { void roster.load(); void meeting.loadPigeons() })
  const pigeonOptions = $derived(meeting.meetingPigeons.map((p) => ({ value: p.id, label: roster.name(p.id) })))
  // Ook de opzetkaart houdt het bij één melding tegelijk, in dezelfde volgorde
  // als het vergaderscherm: een fout gaat voor de rest.
  const setupNotice = $derived(
    meeting.errorText
      ? { id: 'error', tone: 'danger' as const, text: meeting.errorText, retry: false }
      : meeting.pigeonsFailed
        ? { id: 'pigeons', tone: 'danger' as const, text: locale.t('common.pigeons.unavailable'), retry: true }
        : meeting.closedText
          ? { id: 'closed', tone: 'secondary' as const, text: meeting.closedText, retry: false }
          : null,
  )
  const checkKey = $derived(deviceCheckKey(meeting.deviceCheck, meeting.deviceCheckHeard, meeting.deviceCheckAudio))
  let setupEl: HTMLElement | undefined = $state()
  let wasLive = false
  $effect(() => {
    const live = meeting.live
    if (!live && wasLive) setupEl?.querySelector<HTMLElement>('[data-testid="seat-button"]')?.focus()
    wasLive = live
  })
</script>

<div class="meeting-view" data-testid="meeting-view" data-pigeon={meeting.pigeonId}>
  {#if meeting.live}<LiveView store={meeting} {locale} {roster} />
  {:else}
    <div class="setup-scroll" bind:this={setupEl}>
      <section class="setup-card" aria-labelledby="meeting-setup-title">
        <div class="setup-identity"><!-- Op de opzetkaart staat de duif die je kiest, dus hoort daar haar eigen
             kaartportret. Haar rustposter blijft waar hij
             thuishoort: onder het 3D-podium, tot de renderer er staat. -->
        <div class="setup-portrait"><PigeonPortrait pigeon={meeting.pigeonId} /></div><span class="duif-eyebrow">{locale.t('meeting.setup.eyebrow')}</span><h2 id="meeting-setup-title">{roster.name(meeting.pigeonId)}</h2><p>{locale.t(meeting.pigeonId === 'nonverbal' ? 'meeting.setup.signal' : 'meeting.setup.speaking')}</p></div>
        <div class="setup-body">
          <h3>{locale.t('meeting.setup.title')}</h3><p class="setup-intro">{locale.t('meeting.setup.intro')}</p>
          {#if setupNotice}
            <Banner tone={setupNotice.tone} role={setupNotice.tone === 'danger' ? 'alert' : undefined} aria-live={setupNotice.tone === 'danger' ? 'assertive' : 'polite'} message={setupNotice.text} data-testid="setup-notice" data-notice={setupNotice.id} />
            {#if setupNotice.retry}<button type="button" class="duif-text-action" onclick={() => void meeting.loadPigeons()}>{locale.t('meeting.retry')}</button>{/if}
            <!-- Zit de plaats vast door je éígen vergadering, dan hoort de
                 melding een uitweg te hebben in plaats van alleen een
                 constatering. Bij een tafel die door anderen bezet is
                 staat hier niets: daar valt niets te bevrijden. -->
            {#if meeting.stuck}<button type="button" class="duif-text-action" data-testid="stuck-action" disabled={meeting.replaceInFlight} onclick={() => meeting.askReplace()}>{locale.t(meeting.replaceInFlight ? 'meeting.stuck.busy' : 'meeting.stuck.action')}</button>{/if}
          {/if}
          {#if showPigeonSelector}<label class="setup-field"><span>{locale.t('meeting.setup.pigeon')}</span><Select bind:value={meeting.pigeonId} options={pigeonOptions} placeholder={locale.t('common.select.placeholder')} searchPlaceholder={locale.t('common.select.search')} /></label>{/if}
          <p class="setup-language"><strong>{locale.t('meeting.setup.language')}</strong><span>{locale.lang === 'nl' ? 'Nederlands' : 'English'}</span></p>
          <div class="device-check" role="status" aria-live="polite">
            <button type="button" class="duif-text-action" disabled={meeting.phase !== 'setup' || meeting.deviceCheck === 'checking'} onclick={() => void meeting.checkDevices()}>{locale.t('meeting.check.action')}</button>
            {#if checkKey}<p data-testid="device-check-status">{locale.t(checkKey)}</p>{/if}
          </div>
          <!-- De signalerende duif heeft geen woorden: staat zelfstandig meedoen
               uit, dan gebeurt er aan tafel niets en lijkt zij stuk. Haar
               schakelaar hoort daarom op de kaart zelf en niet onder
               "Geavanceerd". De pratende duiven onderbreken het
               gesprek met een hele bijdrage; bij hen blijft hij waar hij was. -->
          {#if !meeting.speaks}<div class="setup-autonomy"><AutonomyField store={meeting} {locale} /></div>{/if}
          <details class="advanced"><summary>{locale.t('meeting.settings.advanced')}</summary><SettingsFields store={meeting} {locale} showAutonomy={meeting.speaks} /></details>
          <button type="button" class="seat-button" aria-busy={meeting.phase === 'opening'} disabled={!pigeonOptions.some((p) => p.value === meeting.pigeonId) || meeting.phase === 'opening'} data-testid="seat-button" onclick={() => void meeting.start()}>{locale.t(meeting.phase === 'opening' ? 'meeting.seating' : 'meeting.seat')} <span aria-hidden="true">→</span></button>
          {#if meeting.phase === 'opening'}<button type="button" class="duif-text-action" onclick={() => meeting.end()}>{locale.t('meeting.cancelStart')}</button>{/if}
          <p class="setup-note">{locale.t('meeting.setup.data')}</p>
        </div>
      </section>
    </div>
  {/if}
</div>

<ConfirmDialog
  open={meeting.stuckConfirm}
  testid="stuck-dialog"
  title={locale.t('meeting.stuck.title')}
  body={locale.t('meeting.stuck.body')}
  confirm={locale.t('meeting.stuck.confirm')}
  cancel={locale.t('meeting.stuck.cancel')}
  busy={meeting.replaceInFlight}
  onconfirm={() => void meeting.confirmReplace()}
  oncancel={() => meeting.cancelReplace()}
/>

<style>
  /* De drie duiven delen in de vergadering hetzelfde rustbeeld, dus de kleur moet
     de overgang dragen. --duif-identity is precies de kleurcodering die het
     hoofdmenu (PigeonSelector) en het portret al per duif gebruiken; hier erven
     ook het lopende vergaderscherm en zijn onderdelen hem. Geen nieuw palet.
     Het hele startscherm ademt de kleur mee, dun genoeg om papier te blijven. */
  .meeting-view { --duif-identity: var(--duif-persona-biological); display: flex; min-height: 0; flex: 1; flex-direction: column; background: color-mix(in oklab,var(--duif-identity) 12%,var(--duif-ground)); }
  .meeting-view[data-pigeon='individualist'] { --duif-identity: var(--duif-persona-individualist); }
  .meeting-view[data-pigeon='nonverbal'] { --duif-identity: var(--duif-persona-nonverbal); }
  .setup-scroll { overflow-y: auto; padding: clamp(1rem, 3vw, 2rem); }
  .setup-card { display: grid; grid-template-columns: minmax(13rem,.8fr) minmax(20rem,1.2fr); max-width: 58rem; margin: 0 auto; background: var(--duif-paper); border: 1px solid color-mix(in oklab,var(--duif-identity) 40%,var(--duif-line)); border-radius: var(--duif-radius-stage); overflow: hidden; }
  /* De kaartzijde met de duif draagt de kleur het duidelijkst; 22% houdt de tekst
     op de kaart ruim boven de contrasteis in beide thema's. */
  .setup-identity { background: color-mix(in oklab,var(--duif-identity) 22%,var(--duif-paper)); display: flex; flex-direction: column; justify-content: center; align-items: center; padding: 2rem; text-align: center; }
  .setup-portrait { width: min(100%,18rem); }
  /* Dezelfde maat als de openingsvraag op het podium (--duif-type-stage-title,
     app.css): wie van de startkaart naar de vergadering gaat, ziet dezelfde
     schaal en geen sprong. */
  h2 { font-family: var(--font-serif,Georgia,serif); font-size: var(--duif-type-stage-title); font-weight: 500; margin: .7rem 0; }
  /* Op de gekleurde kaart wordt de gedempte tekstkleur te licht: een dertig
     procent inkt erbij houdt de rolregel en het bovenschrift in beide thema's
     ruim boven de 4.5 die de leesbaarheidseis vraagt. Mengen gebeurt in oklab en
     niet in oklch: de duivenkleuren gaan bijna-neutraal papier in, en dan is de
     tint in oklch niet betrouwbaar — de blauwgrijze duif werd in donker paars. */
  .setup-identity { --duif-on-identity: color-mix(in oklab,var(--duif-ink) 30%,var(--duif-dim)); }
  .setup-identity p { color: var(--duif-on-identity); font-size: .85rem; max-width: 30ch; line-height: 1.65; }
  .setup-identity :global(.duif-eyebrow) { color: var(--duif-on-identity); }
  .setup-body { padding: clamp(1.25rem,3vw,2rem); display: flex; flex-direction: column; gap: 1rem; }
  h3 { font-size: 1.25rem; margin: 0; }
  .setup-intro { font-size: .85rem; line-height: 1.65; color: var(--duif-dim); margin: -.4rem 0 0; }
  /* De actie hoort bij de melding erboven, niet bij de hele kolom: links
     uitgelijnd en dicht tegen de banner aan leest hij als het antwoord op wat
     daar staat, in plaats van als een tweede knop over de volle breedte. */
  .setup-body > .duif-text-action { align-self: flex-start; margin-top: -.6rem; }
  .setup-field { display: flex; flex-direction: column; gap: .4rem; font-size: .8rem; }
  .setup-language { display: flex; justify-content: space-between; gap: 1rem; padding-block: .6rem; border-block: 1px solid var(--duif-line); font-size: .78rem; margin: 0; }
  .setup-language strong { font-weight: 600; }
  .setup-language span { text-align: right; color: var(--duif-dim); }
  .device-check p { color: var(--duif-dim); font-size: .75rem; line-height: 1.5; margin: .25rem 0; }
  /* De schakelaar staat op de kaart zelf, dus hij krijgt de rust van een eigen
     blok: dezelfde scheidingslijnen als de taalregel erboven, zodat hij als een
     gewone keuze leest en niet als een uitgelichte knop. */
  .setup-autonomy { padding-block: .6rem; border-block-end: 1px solid var(--duif-line); font-size: .8rem; }
  .advanced { font-size: .75rem; color: var(--duif-dim); }
  .advanced summary { cursor: pointer; padding: .5rem 0; margin-bottom: .5rem; }
  .seat-button { display: flex; justify-content: space-between; gap: 1rem; min-height: 3.25rem; align-items: center; font-weight: 600; font-size: .9rem; border: 0; padding: .85rem 1rem; border-radius: .6rem; background: var(--duif-ink); color: var(--duif-paper); }
  .setup-note { font-size: .68rem; line-height: 1.6; color: var(--duif-dim); margin: 0; }
  @media(max-width: 700px) { .setup-card { grid-template-columns: 1fr; } .setup-identity { padding: 1rem; } .setup-portrait { width: 9rem; } .setup-identity p { max-width: 48ch; margin: 0; } h2 { font-size: 1.15rem; margin: .4rem; } }
</style>
