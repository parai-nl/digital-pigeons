<script lang="ts">
  import Badge from '@parai/ui/components/Badge.svelte'
  import MessageBubble from '@parai/ui/patterns/MessageBubble.svelte'
  import BookOpen from 'phosphor-svelte/lib/BookOpen'
  import Compass from 'phosphor-svelte/lib/Compass'
  import CitedText from '../sources/CitedText.svelte'
  import '../sources/answerChips.css'
  import { useServices } from '../services.svelte'
  import { FACILITATOR, notifiedStopReason, type ChatMessage } from './chatStore.svelte'
  import { holdPartialCitation, isDropReason } from '../sources'
  import PigeonPortrait from '../shell/PigeonPortrait.svelte'
  import { authorId } from './presentation'

  // The author belongs to the message. Guidance has a neutral, subordinate
  // treatment; only the pigeon statement can own research citations.
  let {
    message,
    personaName,
    showHeader = true,
    quiet = false,
    index = 0,
    waitSuffix = '',
  }: {
    message: ChatMessage
    personaName: string
    // waitSuffix is de verstreken wachttijd (" 23 s") zodra het wachten lang
    // duurt. Het gesprek rekent hem uit — daar loopt de ene klok — zodat elke
    // bubbel zijn eigen teller niet hoeft te zetten.
    //
    // Hij staat bewust náást de denkregel en niet erin: de bubbel wisselt zijn
    // stap met een overvloeier, en een tekst die elke seconde verandert zou dus
    // elke seconde knipperen. Een stille regel eronder telt gewoon door.
    waitSuffix?: string
    // Welke stap er loopt staat niet in de bubbel maar op de chiprij eronder
    // (TurnSteps). De bubbel houdt zijn eigen, algemene denkregel: twee keer
    // dezelfde zin onder elkaar las als een echo.
    // showHeader: alleen bij een sprekerwissel staat de naam boven de bubbel.
    showHeader?: boolean
    // quiet onderdrukt de denk-bubbel van de facilitator zolang de duif zelf
    // aan het woord is: twee spinners naast elkaar zegt niets meer dan één
    // blok dat streamt.
    quiet?: boolean
    index?: number
  } = $props()

  const { locale, shell, sources } = useServices()
  const identity = $derived(authorId(message))

  // De facilitator is geen duif: zij praat in de interfacetaal en dus in de
  // sans. Alles zonder persona (ook oudere rijen) is de duif.
  const isFacilitator = $derived(message.persona === FACILITATOR)

  // Alleen een stopreden die de lezer iets vertelt over dít antwoord krijgt
  // een regel. De interne redenen (te veel stappen, een mislukte stap) zijn
  // machinetaal over onze motor; de afgebroken-markering en de foutbanner
  // melden de gevolgen al.
  const stopText = $derived.by(() => {
    const reason = notifiedStopReason(message.stopReason)
    return reason ? locale.t(`chat.stopped.${reason}`) : ''
  })

  // Zolang de duif streamt en de facilitator nog niets zei, is er geen bubbel
  // te tonen — alleen haar chips.
  const showBubble = $derived(!(quiet && message.content === '' && message.streaming))

  // Het voetblok hoort bij een antwoord dat er staat. Zolang de bubbel nog de
  // denkstap toont (streamen zonder tekst) blijft het weg, precies zoals de
  // markeringen in de bubbel zelf: anders zou het aantal bronnen al onder een
  // leeg vlak hangen.
  const showChips = $derived(showBubble && !(message.streaming && message.content === ''))
  // Dezelfde grens als bij de bronchip: alles wat iets over de dekking van
  // bronnen zegt, hoort bij de duif. De facilitator bezit geen
  // bronnen, dus ook "geen bron gevonden voor deze vraag" is een uitspraak die
  // hij niet mag doen.
  // Zonder bron mag de Stakeholder een begrensd standpunt innemen.
  // Dat is bedoeld gedrag, geen storing — maar het mag niet als onderzoek
  // gelezen worden. Zij krijgt daarom haar eigen markering in plaats van de
  // algemene gat-badge: die zegt alleen dat er niets gevonden is, en niet wat
  // er dan wél staat.
  const stanceWithoutResearch = $derived(
    !isFacilitator && identity === 'individualist' && message.kbStatus === 'gap',
  )
  const showGap = $derived(!isFacilitator && message.kbStatus === 'gap' && !stanceWithoutResearch)
  const showStance = $derived(showChips && stanceWithoutResearch && !message.streaming)
  const showUnavailable = $derived(!isFacilitator && message.kbStatus === 'unavailable')
  const showSources = $derived(showChips && !isFacilitator && message.sources.length > 0)
  // Deze bronnen kwamen niet uit de gewone zoekopdracht maar uit een
  // herformulering of uit de brede zoekactie die ná een kennisgat op een
  // lagere lat draait. "Verwant" zegt dus waar ze vandaan komen, niet wat ze
  // waard zijn: soms dragen ze de vraag alsnog (de duif antwoordt dan met
  // citaties), soms raken ze hem alleen. De markering benoemt die herkomst en
  // laat het oordeel aan de tekst van de duif — een zin als "geen bron voor
  // deze vraag" sprak het antwoord eronder tegen zodra er wél geciteerd werd.
  // Het staat op twee plekken: in de chiptekst zelf ("3
  // verwante bronnen") en als zin eronder, zodat de lezer het ook ziet zonder
  // de chip te lezen. Zo leest "3 bronnen" nooit als volle dekking.
  const isRelated = $derived(!isFacilitator && message.related === true && message.sources.length > 0)
  const showRelated = $derived(showChips && isRelated && !message.streaming)
  const sourceLabel = $derived.by(() => {
    const many = message.sources.length !== 1
    const key = isRelated
      ? many ? 'chat.source.related.many' : 'chat.source.related.one'
      : many ? 'chat.source.count.many' : 'chat.source.count.one'
    return locale.t(key, { count: message.sources.length })
  })

  // Wat er wegviel vóór de bronnenlijst. Eén neutrale zin, en alleen
  // wanneer het antwoord er staat: tijdens het streamen weet de lezer nog niet
  // eens hoeveel bronnen er wél zijn. De facilitator bezit geen bronnen, dus
  // hij doet ook geen uitspraak over wat er van die bronnen afviel.
  const dropped = $derived(message.dropped)
  const showDropped = $derived(showChips && !isFacilitator && !message.streaming && dropped !== undefined)
  const droppedText = $derived(
    dropped
      ? locale.t(dropped.count === 1 ? 'chat.dropped.one' : 'chat.dropped.many', { count: dropped.count })
      : '',
  )
  // De redenen zijn begeleiderstaal en staan alleen in devMode. Alleen de
  // codes die de server kent (DROP_REASONS) krijgen een zin; een onbekende
  // code laat niets achter, want machinetaal op het scherm is erger dan een
  // regel minder.
  const droppedDetail = $derived.by(() => {
    if (!dropped) return [] as string[]
    return Object.entries(dropped.reasons).flatMap(([reason, count]) =>
      isDropReason(reason) ? [locale.t(`chat.dropped.${reason}`, { count })] : [],
    )
  })

  // De bronnen zitten niet meer onder het antwoord: ze staan in de gedeelde
  // lade (lib/sources). Dit bericht is hun eigenaar, zodat alleen de verwijzing
  // in dít antwoord oplicht als de lade openstaat.
  function openSource(number: number): void {
    sources.show(message.sources, number, { owner: message.key })
  }

  // Tijdens het streamen houden we een half ontvangen "[1" achter: anders
  // flitst de ruwe markup door de zin voordat hij een verwijzing kan worden.
  const shown = $derived(message.streaming ? holdPartialCitation(message.content) : message.content)

</script>

{#snippet body({ content, streaming }: { content: string; streaming: boolean })}
  <CitedText
    text={isFacilitator ? content.replace(/\[\s*\d+(?:\s*,\s*\d+)*\s*\]/g, '') : content}
    sources={isFacilitator ? [] : message.sources}
    {streaming}
    owner={message.key}
    open={openSource}
    class={isFacilitator ? 'break-words whitespace-pre-wrap' : 'break-words whitespace-pre-wrap duif-voice'}
    testid="assistant-content"
  />
{/snippet}

{#snippet extras()}
  <div class="flex flex-col gap-1">
    {#if message.partial || showGap || showRelated || showUnavailable || stopText !== ''}
      <div class="flex flex-wrap items-center gap-2">
        {#if showGap}
          <Badge variant="warning" data-testid="gap-badge">{locale.t('chat.gap')}</Badge>
        {/if}
        <!-- Dezelfde plek en dezelfde kleur als de gat-badge: ook dit is een
             voorbehoud bij wat eronder staat. De mededeling is een andere —
             niet "er is niets", maar "dit is wat eraan grenst" — en die
             terughoudendheid hoort de lezer te zien vóór hij de bronnen als
             antwoord op zijn vraag leest. -->
        {#if showRelated}
          <Badge variant="warning" data-testid="related-badge">{locale.t('chat.source.related')}</Badge>
        {/if}
        {#if showUnavailable}<Badge variant="warning">{locale.t('chat.source.unavailable')}</Badge>{/if}
        {#if stopText !== ''}
          <Badge variant="warning" data-testid="stop-badge">{stopText}</Badge>
        {/if}
        {#if message.partial}
          <span class="duif-partial text-xs text-muted-foreground" data-testid="partial-marker">
            {locale.t('chat.interrupted')}
          </span>
        {/if}
      </div>
    {/if}
  </div>
{/snippet}

<!-- Het voetblok van een antwoord: de bronchip (opent de lade) en de
     dekkingsmarkering van de Stakeholder. De vervolgacties staan hier niet:
     ze horen bij de beurt en niet bij dit bericht, en in de kolom lazen ze
     als rommel. -->
{#snippet footerBlock()}
  <div class="answer-footer" data-testid="answer-footer">
    <div class="footer-chips">
      {#if showStance}
        <span class="duif-answer-chip is-stance" data-testid="stance-marker"><Compass size={14} weight="regular" aria-hidden="true" /><span>{locale.t('chat.stance.withoutResearch')}</span></span>
      {/if}
      {#if showSources}
        <button
          type="button"
          class="duif-answer-chip is-sources"
          data-testid="source-count"
          aria-haspopup="dialog"
          onclick={() => openSource(message.sources[0].number)}
        ><BookOpen size={14} weight="regular" aria-hidden="true" /><span>{sourceLabel}</span></button>
      {/if}
    </div>
  </div>
{/snippet}

<div
  data-testid="assistant-message"
  data-persona={message.persona ?? ''}
  data-author={identity}
  class:guidance-message={isFacilitator}
  class="answer-message"
  aria-busy={message.streaming && showBubble}
>
  {#if showBubble}
    {#if showHeader}<div class="answer-author">{#if !isFacilitator && (identity === 'biological' || identity === 'individualist')}<span class="answer-portrait"><PigeonPortrait pigeon={identity} variant="head" /></span>{/if}<span>{personaName}</span></div>{/if}
    <MessageBubble
      role="assistant"
      content={shown}
      streaming={message.streaming}
      stepLabel={locale.t(isFacilitator ? 'chat.thinking.facilitator' : 'chat.thinking')}
      persona={undefined}
      grouped={!showHeader}
      tail={false}
      {index}
      contentRenderer={body}
      footer={extras}
    />
    {#if waitSuffix !== '' && message.streaming && message.content === ''}
      <p class="answer-waiting" data-testid="answer-waiting">{waitSuffix.trim()}</p>
    {/if}
  {/if}
  <!-- `showChips` is de ondergrens: onder een bubbel die nog alleen de
       denkstap toont hoort geen chiprij. -->
  {#if showChips && (showSources || showStance)}{@render footerBlock()}{/if}
  {#if showDropped}
    <p class="answer-dropped" data-testid="answer-dropped">
      {droppedText}{#if shell.dev && droppedDetail.length}<span class="dropped-detail"> · {locale.t('chat.dropped.detail')}: {droppedDetail.join(', ')}</span>{/if}
    </p>
  {/if}
  {#if shell.dev && !isFacilitator && !message.streaming && message.content.trim()}
    <details class="answer-version" data-testid="answer-version"><summary>{locale.t('chat.version')}</summary>
      {#if message.provenance}
        <dl><dt>{locale.t('chat.version.model')}</dt><dd>{message.provenance.model}</dd><dt>{locale.t('chat.version.contract')}</dt><dd>{message.provenance.contractVersion}</dd><dt>{locale.t('chat.version.persona')}</dt><dd>{message.provenance.personaVersion}</dd><dt>{locale.t('chat.version.prompt')}</dt><dd>{message.provenance.promptVersion}</dd><dt>{locale.t('chat.version.kb')}</dt><dd>{message.provenance.kbVersion || locale.t('chat.version.unverified')}</dd></dl>
      {:else}<p>{locale.t('chat.version.legacy')}</p>{/if}
    </details>
  {/if}
</div>

<style>
  .answer-message { margin-bottom: 1.85rem; --answer-accent: var(--duif-persona-nonverbal); }
  [data-author='biological'] { --answer-accent: var(--duif-persona-biological); }
  [data-author='individualist'] { --answer-accent: var(--duif-persona-individualist); }
  .answer-author { display: flex; align-items: center; gap: .6rem; color: var(--answer-accent); font-size: .8rem; font-weight: 550; margin: .25rem .5rem .1rem; }
  .answer-portrait { width: 2.3rem; flex: none; }
  /* De teller onder de denkbubbel: klein, gedempt en op dezelfde inspringing
     als de bubbel zelf. Hij is er alleen als het wachten lang duurt, en dan
     hoort hij te vertellen zonder aandacht op te eisen. */
  .answer-waiting { margin: .3rem .75rem 0; font-size: .75rem; font-variant-numeric: tabular-nums; color: var(--duif-dim); }
  /* De uitvalmelding staat onder de chiprij, in dezelfde gedempte toon als de
     wachtteller: zij is een mededeling over de zoekopdracht, geen fout en geen
     tweede label. De devMode-redenen erachter krijgen nog een tint minder. */
  .answer-dropped { margin: .3rem .75rem 0; font-size: .75rem; color: var(--duif-dim); }
  .dropped-detail { color: color-mix(in oklab, var(--duif-dim) 80%, transparent); }
  /* De facilitator staat tussen de mens en de duif en moet daar gewoon leesbaar
     zijn: dezelfde lettergrootte als de mensbubbel (0,9375 rem, de basis van
     MessageBubble) en de gewone inktkleur. Met --duif-dim op 13,6 px haalde
     deze tekst 4,34:1 op het bubbelvlak en bleef hij onder de AA-drempel; op
     --duif-ink is het 13,8:1 in licht en 12,7:1 in donker.
     Onderscheiden van de duif blijft hij door zijn eigen vlak: een lichte,
     "witte" kaart met een dunne rand en zijn naam in kapitalen erboven,
     ingesprongen ten opzichte van het antwoord. De duifbubbel blijft ongemoeid. */
  .guidance-message { margin: .15rem .7rem 1.4rem 1.15rem; }
  /* Het naamlabel staat buiten de kaart, op het chatvlak (--duif-ground), en
     haalde daar op kale --duif-dim maar 4,62:1 in licht. Eén tint richting de
     inkt maakt er 6,85:1 van; in donker loopt dezelfde menging naar lichter,
     van 8,03:1 naar 10,07:1. Mengen in oklab en niet in oklch: bij deze bijna
     neutrale grijzen is de tint-hoek onstabiel en slaat de kleur in oklch naar
     paars. */
  .guidance-message .answer-author { margin-left: .9rem; font-size: .68rem; font-weight: 650; letter-spacing: .08em; text-transform: uppercase; color: color-mix(in oklab, var(--duif-dim) 62%, var(--duif-ink)); }
  /* De kaart hangt aan het vak dát de tekst draagt, niet aan een plaats in de
     kolom: krijgt MessageBubble ooit een kop of een persona-blok, dan schuift
     de kaart niet stil mee naar het verkeerde vak. Het denkstapvak (spinner,
     nog geen tekst) draagt bewust geen kaart — daar staat nog geen bericht. */
  .guidance-message :global([data-testid='message-bubble'] > div > div:has(> [data-testid='assistant-content'])) {
    background: var(--duif-paper);
    border: 1px solid var(--duif-line);
    border-radius: .85rem;
  }
  .guidance-message :global([data-testid='assistant-content']) { font-size: .9375rem; line-height: 1.65; color: var(--duif-ink); }

  /* De duif leest als lopende tekst, niet als een chatregel: iets groter dan
     de basis van MessageBubble (0,9375 rem) en met ruimere regelafstand. */
  .answer-message:not(.guidance-message) :global([data-testid='assistant-content']) {
    font-size: 1.0625rem;
    line-height: 1.78;
    letter-spacing: -.003em;
  }

  /* Het voetblok volgt de leesmaat van de bubbel, zodat het onder het antwoord
     staat en niet onder de kolom. */
  .answer-footer {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-start;
    gap: .45rem .9rem;
    max-width: min(100%, calc(var(--duif-chat-read, 46rem) + 10rem));
    margin: .5rem .35rem 0;
  }
  .footer-chips { display: flex; flex-wrap: wrap; align-items: flex-start; gap: .4rem; min-width: 0; }

  .answer-version { margin: .4rem 1rem; color: var(--duif-dim); font-size: .7rem; line-height: 1.6; }
  .answer-version summary { cursor: pointer; width: fit-content; }
  .answer-version dl { display: grid; grid-template-columns: minmax(7rem,1fr) minmax(0,2fr); gap: .3rem .8rem; padding: .7rem 0; }
  .answer-version dd { overflow-wrap: anywhere; }
  .answer-version p { padding: .5rem 0; }

  .duif-partial {
    border-left: 2px dashed var(--duif-off);
    padding-left: 0.5rem;
    font-style: italic;
  }
</style>
