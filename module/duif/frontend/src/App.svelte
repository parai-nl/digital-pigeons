<script lang="ts">
  import './app.css'
  import { onMount, tick } from 'svelte'
  import ArrowLeftIcon from 'phosphor-svelte/lib/ArrowLeftIcon'
  import ChatCircleIcon from 'phosphor-svelte/lib/ChatCircleIcon'
  import GraphIcon from 'phosphor-svelte/lib/GraphIcon'
  import HouseSimpleIcon from 'phosphor-svelte/lib/HouseSimpleIcon'
  import LockSimpleIcon from 'phosphor-svelte/lib/LockSimpleIcon'
  import QuestionIcon from 'phosphor-svelte/lib/QuestionIcon'
  import UsersThreeIcon from 'phosphor-svelte/lib/UsersThreeIcon'
  import ChatView from './lib/chat/ChatView.svelte'
  import LazyGraph from './lib/graph/LazyGraph.svelte'
  import type { Lang } from './lib/i18n.svelte'
  import LazyMeeting from './lib/meeting/LazyMeeting.svelte'
  import { useServices, type Tab } from './lib/services.svelte'
  import SourceDrawer from './lib/sources/SourceDrawer.svelte'
  import ConversationActions from './lib/shell/ConversationActions.svelte'
  import HelpDialog from './lib/shell/HelpDialog.svelte'
  import LazyHelp from './lib/shell/LazyHelp.svelte'
  import PigeonPortrait from './lib/shell/PigeonPortrait.svelte'
  import PigeonSelector from './lib/shell/PigeonSelector.svelte'
  import SessionRail from './lib/shell/SessionRail.svelte'
  import { hostLanguage, observeHostLanguage } from './lib/shell/hostLanguage'
  import { followHostLanguage } from './lib/shell/languagePreference'
  import { currentPlace, rememberWorkspacePlace } from './lib/shell/workspacePlace'
  import { type PigeonId } from './lib/shell/pigeons'

  const { locale, chat, meeting, shell, graph, roster } = useServices()
  const LANGUAGE_NAMES: Record<Lang, string> = { en: 'English', nl: 'Nederlands' }
  // Op het vergaderscherm wijst de kiezer de duif aan tafel aan — dat is daar
  // de duif waar het over gaat, en na een wissel staat de nieuwe duif
  // dus meteen geselecteerd. Elders wijst hij het gesprek aan.
  //
  // De uitleg telt hier niet meer mee: zij is een venster over het
  // scherm heen, geen ander scherm. Wat eronder ligt blijft dus precies wat het
  // was — de kop verspringt niet bij het openen, en de vergadering die
  // doorschemert is nog steeds de vergadering waar de kiezer over gaat.
  const meetingSeat = $derived(shell.tab === 'meeting' && meeting.live)
  const selected = $derived(meetingSeat ? meeting.pigeonId : shell.signaling ? 'nonverbal' : chat.selectedPigeonId || 'biological')
  const currentMode = $derived(shell.tab === 'graph' ? shell.previousTab : shell.tab)
  const locked = $derived(meeting.phase !== 'setup')
  const activeLang = $derived(meeting.live ? meeting.lang : locale.lang)
  const lockedNotice = $derived(locale.t('shell.languageLocked', { language: LANGUAGE_NAMES[activeLang] }))
  // Het graafscherm houdt van de kop alleen de compacte regel over; alle hoogte
  // die daarmee vrijkomt is voor de kaart.
  const mapOnly = $derived(shell.tab === 'graph')
  // De gespreksacties horen bij het gesprek dat eronder staat: ze verschijnen
  // alleen wanneer dat gesprek ook echt zichtbaar is.
  const conversing = $derived(shell.tab === 'chat' && !shell.signaling)
  let meetingVisited = $state(false)
  let graphVisited = $state(false)
  let helpVisited = $state(false)
  let helpReturnFocus: HTMLElement | null = null
  let graphReturnFocus: HTMLElement | null = null
  let workspace: HTMLElement | undefined = $state()
  let previousRoute: Tab = 'chat'
  // De taal komt van de gastheer (de parai-core-schil, of de DevShell die haar
  // nabootst): de module kiest niet zelf, zij volgt. Null betekent dat er geen
  // gastheertaal is; dan blijft de bewaarde voorkeur staan.
  let hostLang = $state<Lang | null>(hostLanguage())
  // Includes source-card navigation, which enters via services.openInGraph.
  $effect.pre(() => {
    const route = shell.tab
    if (route === 'graph' && previousRoute !== 'graph') {
      graphReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    }
    previousRoute = route
  })

  onMount(() => { void roster.load(); void chat.loadPigeons(); void chat.loadSessions(); void meeting.loadPigeons() })
  onMount(() => observeHostLanguage((lang) => { hostLang = lang }))

  $effect(() => {
    // De uitleg dooft de tekenlussen niet meer: zij ligt erover, en een
    // vergadering die stilvalt zodra je de uitleg opent, leest als een
    // vergadering die stopte. Wat achter de dimlaag doorschemert
    // beweegt dus door; de tabbladen die niet openstaan blijven stil.
    meeting.orbVisible = shell.tab === 'meeting'
    graph.visible = shell.tab === 'graph'
    if (shell.tab === 'meeting' || meeting.live) meetingVisited = true
    if (shell.tab === 'graph') graphVisited = true
    if (shell.help) helpVisited = true
  })
  // De plek van de lezer staat ook in het adres: een taalwissel in de schil
  // herlaadt de pagina, en wat de module in het geheugen heeft overleeft dat
  // niet (lib/shell/workspacePlace.ts).
  $effect(() => { rememberWorkspacePlace(currentPlace({ shell, graph, chat })) })
  // Een lopende vergadering houdt haar taal vast: wisselt de gastheer van taal
  // terwijl er gesproken wordt, dan blijft de module staan waar de vergadering
  // staat en zegt het slot waarom.
  $effect(() => { if (hostLang && !locked) followHostLanguage(locale, hostLang) })
  // A running (or opening) meeting owns its pigeon independently of browsing.
  // Setting a setup choice never starts microphone capture.
  $effect(() => { if (meeting.phase === 'setup') meeting.pigeonId = selected })

  // De duifkiezer bovenaan doet twee dingen, afhankelijk van waar je staat.
  // Op het gespreksscherm kiest hij het gesprek. Staat er een lopende
  // vergadering op het scherm, dan gaat hij over de duif aan tafel: dan stelt
  // hij de vraag of die moet wisselen en verandert er verder niets tot
  // de gebruiker antwoordt. De vergadering houdt haar eigen duif, dus de
  // chatkeuze blijft waar hij was.
  //
  // Kan die wissel niet — een duif zonder vergaderrol, dezelfde duif, of een
  // wissel die al loopt — dan gebeurt er niets. Doorvallen naar de chatkeuze
  // zou achter de vergadering om iets anders verzetten dan de klik beloofde.
  function choosePigeon(id: PigeonId): void {
    if (shell.tab === 'meeting' && meeting.live && meeting.phase === 'live') {
      meeting.requestPigeonSwitch(id)
      return
    }
    shell.signaling = id === 'nonverbal'
    if (id !== 'nonverbal') chat.selectPigeon(id)
  }
  function chooseTab(tab: Tab): void { shell.help = false; shell.navigate(tab) }
  function openHelp(): void {
    if (shell.help) { closeHelp(); return }
    helpReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    shell.help = true
  }
  function closeHelp(): void {
    shell.help = false
    void tick().then(() => helpReturnFocus?.isConnected && helpReturnFocus.focus())
  }
  function openGraph(): void {
    graphReturnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    shell.navigate('graph')
  }
  function backFromGraph(): void {
    shell.back()
    void tick().then(() => {
      if (graphReturnFocus?.isConnected && !graphReturnFocus.closest('[hidden]')) graphReturnFocus.focus()
      else workspace?.focus()
    })
  }
  function modeKey(event: KeyboardEvent): void {
    if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
    event.preventDefault()
    const tab = event.key === 'Home' ? 'chat' : event.key === 'End' ? 'meeting' : currentMode === 'chat' ? 'meeting' : 'chat'
    chooseTab(tab)
    document.getElementById(`duif-tab-${tab}`)?.focus()
  }
</script>

<div class="duif-root" lang={locale.lang} data-testid="duif-root">
  <!-- Eén regel onder de schilbalk van parai-core, in dezelfde stijl: links
       het gesprek, rechts wat ernaast ligt. De module heeft geen eigen
       titelblok en geen tweede taalkiezer meer; de schil zegt al welke module
       openstaat en in welke taal. -->
  <header class="duif-header">
    <div class="duif-nav">
      <!-- Links staat het gesprek: waar je bent (Chat of Vergadering) en wat je
           met dat gesprek doet (Historie, Nieuw gesprek), met een dunne streep
           ertussen. Rechts staat wat naast het gesprek ligt: de kennisgraaf en
           de uitleg. De gespreksacties staan bewust buiten het navigatielandmark
           — zij horen bij het gesprek eronder en brengen hun eigen dialogen mee,
           terwijl het landmark de weergaven en de hulp van de module benoemt. -->
      <div class="duif-nav-left">
        {#if mapOnly}
          <button type="button" class="duif-nav-item duif-nav-strong" onclick={backFromGraph}><ArrowLeftIcon size="1.05rem" aria-hidden="true" /><span class="duif-nav-label">{locale.t('shell.back', { view: locale.t(shell.previousTab === 'meeting' ? 'common.tab.meeting' : 'common.tab.chat') })}</span></button>
        {:else}
          <div class="duif-nav-tabs" role="tablist" aria-label={locale.t('shell.mode')}>
            {#each ['chat', 'meeting'] as mode}
              {@const label = locale.t(mode === 'chat' ? 'common.tab.chat' : 'common.tab.meeting')}
              <button id={`duif-tab-${mode}`} type="button" class="duif-nav-item" role="tab" aria-label={label} onkeydown={modeKey} aria-selected={currentMode === mode} aria-controls={`duif-panel-${mode}`} tabindex={currentMode === mode ? 0 : -1} onclick={() => chooseTab(mode === 'chat' ? 'chat' : 'meeting')}>
                {#if mode === 'chat'}<ChatCircleIcon size="1.05rem" aria-hidden="true" />{:else}<UsersThreeIcon size="1.05rem" aria-hidden="true" />{/if}
                <span class="duif-nav-label">{label}</span>
              </button>
            {/each}
          </div>
        {/if}
        {#if conversing}<span class="duif-nav-rule" aria-hidden="true"></span><ConversationActions />{/if}
      </div>
      <nav class="duif-nav-views" aria-label={locale.t('shell.nav')}>
        {#if !mapOnly}
          <button type="button" class="duif-nav-item" aria-label={locale.t('graph.tab')} aria-pressed={shell.tab === 'graph'} onclick={openGraph}><GraphIcon size="1.05rem" aria-hidden="true" /><span class="duif-nav-label">{locale.t('graph.tab')}</span></button>
        {/if}
        <button type="button" class="duif-nav-item" aria-label={locale.t('shell.help')} aria-expanded={shell.help} aria-controls="duif-help" onclick={openHelp}><QuestionIcon size="1.05rem" aria-hidden="true" /><span class="duif-nav-label">{locale.t('shell.help')}</span></button>
        <!-- De weg terug naar de etalage, de voorpagina van pigeon.parai.nl.
             Een echte paginawissel: anders routeert de SvelteKit-schil "/" zelf,
             naar haar dashboard, dat met één module meteen weer hierheen stuurt.
             Tijdens een vergadering staat hij er niet: weggaan breekt haar af. -->
        {#if !locked}
          <a class="duif-nav-item duif-nav-site" href="/" data-sveltekit-reload aria-label={locale.t('shell.site')} title={locale.t('shell.site')}><HouseSimpleIcon size="1.05rem" aria-hidden="true" /><span class="duif-nav-label">{locale.t('shell.site')}</span></a>
        {/if}
        {#if locked}
          <span class="duif-language-locked" title={lockedNotice} data-testid="language-locked"><LockSimpleIcon size=".85rem" aria-hidden="true" /><span aria-hidden="true">{activeLang.toUpperCase()}</span><span class="sr-only">{lockedNotice}</span></span>
        {/if}
      </nav>
    </div>
    {#if !mapOnly}
      <div class="duif-pigeon-row"><PigeonSelector {selected} {locale} {roster} onselect={choosePigeon} /></div>
    {/if}
  </header>

  <!-- Eén bedieningsset: op het vergaderscherm bedient MeetingActions, en pas
       wanneer dat scherm niet zichtbaar is verschijnt de rail met de globale
       stand en STOP. De uitleg verandert daar niets meer aan: staat
       zij open over het vergaderscherm, dan schemert dat scherm er met zijn
       eigen bediening doorheen, en zou de rail een tweede bedieningsset zijn. -->
  {#if meeting.live && shell.tab !== 'meeting'}<SessionRail store={meeting} {locale} {roster} />{/if}

  <main class="duif-workspace" tabindex="-1" bind:this={workspace}>
    <!-- Keep the working views mounted: draft input, scroll and session content
         survive help/graph navigation. Hidden panels cannot receive focus. -->
    <div id="duif-panel-chat" role="tabpanel" aria-labelledby={mapOnly ? undefined : 'duif-tab-chat'} class="duif-panel" hidden={shell.tab !== 'chat'}>
      <div class="duif-panel" hidden={shell.signaling}><ChatView /></div>
      {#if shell.signaling}
        <section class="duif-signal-intro"><div class="duif-signal-portrait"><PigeonPortrait pigeon="nonverbal" /></div><span class="duif-eyebrow">{roster.name('nonverbal')}</span><h2>{locale.t('shell.signal.title')}</h2><p>{locale.t('shell.signal.body')}</p><button type="button" class="duif-primary-button" onclick={() => chooseTab('meeting')}>{meeting.live ? locale.t('shell.signal.existing', { pigeon: roster.name(meeting.pigeonId) }) : locale.t('shell.signal.action')} <span aria-hidden="true">→</span></button><small>{locale.t('shell.signal.note')}</small></section>
      {/if}
    </div>
    <div id="duif-panel-meeting" role="tabpanel" aria-labelledby={mapOnly ? undefined : 'duif-tab-meeting'} class="duif-panel" hidden={shell.tab !== 'meeting'}>{#if meetingVisited}<LazyMeeting />{/if}</div>
    <div class="duif-panel" hidden={shell.tab !== 'graph'}>{#if graphVisited}<LazyGraph />{/if}</div>
  </main>

  <!-- De uitleg ligt over de hele module, buiten de werkruimte: zij dimt ook de
       kop en de vergaderstrook, zodat zichtbaar blijft dat de ervaring eronder
       doorloopt. Zij blijft gemonteerd zodra ze één keer open was, want een
       uitgeklapte vraag hoort er bij terugkomst nog steeds uit te staan. -->
  <HelpDialog open={shell.help} {locale} onclose={closeHelp}>
    {#if helpVisited}<LazyHelp {locale} {roster} onclose={closeHelp} ongraph={openGraph} meetingLive={meeting.phase !== 'setup'} />{/if}
  </HelpDialog>

  <!-- De bronlade hangt over de hele schil: één exemplaar voor de chat en de
       vergadering, dat elk antwoord via de gedeelde store opent. De buurtgraaf
       gaat er als snippet in: de lade kent de kennisgraaf niet, en de graaf komt
       pas van de schijf zodra er werkelijk een bron in beeld staat. -->
  <SourceDrawer>
  </SourceDrawer>
</div>
