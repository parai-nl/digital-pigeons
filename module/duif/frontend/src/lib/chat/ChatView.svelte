<script lang="ts">
  import { onMount, tick } from 'svelte'
  import ArrowBendUpRightIcon from 'phosphor-svelte/lib/ArrowBendUpRightIcon'
  import MessageBubble from '@parai/ui/patterns/MessageBubble.svelte'
  import MessageList from '@parai/ui/patterns/MessageList.svelte'
  import KnowledgeBanner from '../KnowledgeBanner.svelte'
  import { useServices } from '../services.svelte'
  import AssistantMessage from './AssistantMessage.svelte'
  import ChatComposer from './ChatComposer.svelte'
  import TurnSteps from './TurnSteps.svelte'
  import { FACILITATOR, type ChatMessage } from './chatStore.svelte'
  import { authorId, latestExchange, repeatedQuestionKey, unansweredTurn, visibleMessages } from './presentation'
  import { CONVERSATION_CASES, GAP_CASE } from './cases'
  import { elapsedSuffix } from '../waiting'
  import PigeonPortrait from '../shell/PigeonPortrait.svelte'
  import './chat.css'

  // De duif kiest men bovenaan de schil (PigeonSelector op shell/pigeons.ts).
  // De chat heeft daarom geen eigen zitstok meer: twee keuzelijsten voor
  // dezelfde keuze lieten de lezer raden welke telt. Historie en "nieuw
  // gesprek" staan in diezelfde navigatieregel
  // (shell/ConversationActions.svelte) en niet meer op een eigen regel hier.
  const { chat, knowledge, locale, sources, roster } = useServices()
  let pendingDraft = $state('')
  let confirmation: HTMLDialogElement | undefined = $state()
  let view: HTMLElement | undefined = $state()
  let anchor: HTMLElement | undefined = $state()

  const shown = $derived(visibleMessages(chat.messages, chat.selectedPigeonId))
  const exchange = $derived(latestExchange(chat.messages))
  // Naar de andere duif gaat een vráág, en elke vraag in dit gesprek mag mee:
  // de knop hangt daarom op de bubbel van de vraag zelf en niet meer los van
  // het gesprek. `beginComparison` zoekt de vraag op haar sleutel op,
  // dus een oudere vraag werkt net zo goed als de laatste. Tijdens het streamen
  // staat er geen knop: een half antwoord vergelijken is geen vergelijking.
  const canCompare = $derived(chat.sessionId !== '' && !chat.streaming)
  // De vraag die een vergelijking opnieuw stelde: opgeslagen als volle
  // mensrij, op het scherm één gedempte regel. Zie presentation.ts.
  const repeatedKey = $derived(repeatedQuestionKey(chat.messages, chat.activeComparison))
  // "Bewerk de laatste vraag" hoort bij de foutmelding die haar nodig maakt en
  // staat daarom onder de banner in het invoerblok.
  const retryable = $derived(chat.error !== null && exchange.question !== undefined)
  // Een laatste vraag zonder antwoord, terwijl er niets meer loopt en er geen
  // foutbanner staat: de beurt is ergens onderweg gestopt — meestal doordat
  // het tabblad sloot of herlaadde terwijl zij liep. Dan zegt het gesprek dat
  // zelf, met dezelfde weg terug als onder een foutmelding.
  // Tijdens het overschakelen naar een vergelijking staat het nieuwe gesprek
  // even zonder antwoord open; dat is geen afgebroken beurt.
  const unanswered = $derived(!chat.streaming && !chat.loadingHistory && !chat.loadingComparison && !chat.error ? unansweredTurn(chat.messages) : undefined)
  const announcement = $derived(chat.announcements.at(-1))
  const announced = $derived(announcement ? locale.t(announcement.voice === 'pigeon' ? 'chat.announce.pigeon' : 'chat.announce.facilitator') : '')
  // Het wachten op de duif is zichtbaar en, als het lang duurt, ook
  // hoorbaar in cijfers. Eén klok voor het hele gesprek: hij tikt alleen
  // zolang er werkelijk gewacht wordt, en de bubbels lezen er hun staartje uit.
  let waitNow = $state(Date.now())
  $effect(() => {
    if (chat.pigeonWaitSince <= 0) return
    waitNow = Date.now()
    const timer = setInterval(() => (waitNow = Date.now()), 1000)
    return () => clearInterval(timer)
  })
  const waitSuffix = $derived(elapsedSuffix(chat.pigeonWaitSince, waitNow))
  // De losse wachtbubbel overbrugt het gat tussen "de duif is gevraagd" en het
  // openen van haar blok: daartussen zit het opzoeken in de kennisbank, dat via
  // dezelfde onvoorspelbare proxy loopt.
  const awaitingPigeon = $derived(chat.pigeonWaitOpen && chat.streaming)
  // De stapregel is de voortgang van een beurt op de agent-motor:
  // welke stap loopt en welke al klaar zijn. Zolang zij er staat is de oude
  // algemene zin overbodig; de pipeline-duif meldt geen stappen en houdt hem.
  const steps = $derived(chat.streaming ? chat.steps : [])
  const progress = $derived(chat.streaming && steps.length === 0 && !awaitingPigeon && !shown.some((m) => m.role === 'assistant' && m.streaming))
  // De stap staat op één plek: de chiprij. De denkbubbels hielden even dezelfde
  // zin, maar dan stond er twee keer onder elkaar "Bronnen lezen…" — daarom
  // is de rij de drager van het woord, en blijft de bubbel wat hij al was:
  // een spinner met de secondenteller eronder.
  // Alleen het nog lege, streamende duif-blok draagt de teller: onder tekst die
  // al binnenkomt wordt er niet meer gewacht.
  function suffixFor(message: ChatMessage): string {
    if (message.kind !== 'pigeon_answer') return ''
    return message.streaming && message.content === '' ? waitSuffix : ''
  }
  // Zolang de duif zelf aan het woord komt, houdt de facilitator haar denkstap
  // in: twee identieke spinners onder elkaar zeggen niets meer dan één, en de
  // lezer zou moeten raden op welke van de twee hij wacht. Haar chips en haar
  // geschreven tekst blijven staan — alleen de lege denk-bubbel gaat weg.
  const pigeonPending = $derived(awaitingPigeon || shown.some((m) => m.kind === 'pigeon_answer' && m.streaming))
  function quietFor(message: ChatMessage): boolean {
    return pigeonPending && message.kind !== 'pigeon_answer'
  }
  const knowledgePigeon = $derived(chat.selectedPigeon?.knowledgeBase === true ? chat.selectedPigeonId : '')
  let polledFor = ''
  $effect(() => {
    const id = knowledgePigeon
    if (id && id !== polledFor) { polledFor = id; void knowledge.load() }
  })
  $effect(() => { if (chat.messages.at(-1)?.role === 'user') anchor?.scrollIntoView?.({ block: 'end' }) })
  $effect(() => {
    if (pendingDraft) { if (confirmation && !confirmation.open) confirmation.showModal?.() }
    else confirmation?.close?.()
  })

  function speakerFor(message: ChatMessage): string {
    return message.persona === FACILITATOR ? locale.t('chat.guidance') : roster.name(authorId(message))
  }
  function headerAt(index: number): boolean {
    const previous = shown[index - 1]
    return !previous || previous.role === 'user' || authorId(previous) !== authorId(shown[index])
  }
  async function focusComposer(): Promise<void> {
    await tick()
    view?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
  }
  function useExample(text: string): void {
    if (chat.draft.trim() && chat.draft !== text) { pendingDraft = text; return }
    chat.draft = text
    void focusComposer()
  }
  function replaceDraft(): void {
    chat.draft = pendingDraft
    pendingDraft = ''
    void focusComposer()
  }
  // De knop op de hoek draagt geen woord, dus zijn naam moet de vraag noemen:
  // anders hoort een schermlezer bij elke vraag dezelfde zin en weet hij niet
  // welke er meegaat. Een lange vraag wordt ingekort — een naam van duizend
  // tekens is geen naam meer.
  const NAME_LIMIT = 70
  function compareLabel(question: string): string {
    const runes = [...question.trim()]
    const short = runes.length > NAME_LIMIT ? `${runes.slice(0, NAME_LIMIT).join('').trimEnd()}…` : question.trim()
    return locale.t('chat.compare.action.question', { question: short })
  }
  onMount(() => { void chat.loadPigeons(); void chat.loadSessions() })
</script>

<!-- "Bewerk de laatste vraag" hoort bij de fout die haar nodig maakt:
     ChatComposer zet dit fragment onder zijn foutbanner. -->
{#snippet retryAction()}
  <button type="button" class="duif-text-action error-retry" onclick={() => useExample(exchange.question!.content)}>{locale.t('chat.retry')}</button>
{/snippet}

<section class="duif-chat" bind:this={view} aria-label={roster.name(chat.selectedPigeonId)} data-drawer={sources.open ? 'open' : 'closed'}>
  {#if knowledgePigeon}<KnowledgeBanner />{/if}
  {#if chat.conversationLang !== locale.lang && chat.sessionId}<p class="language-notice">{locale.t('chat.language.notice', { language: locale.t(chat.conversationLang === 'nl' ? 'chat.language.nl' : 'chat.language.en') })}</p>{/if}
  {#if chat.activeComparison}
    {@const origin = chat.activeComparison}
    <!-- De herkomstregel van een vergelijkgesprek: één compacte regel boven
         het gesprek, met de ene weg terug. Hij hoorde even in een zijbaan
         thuis; zonder banen staat hij weer waar hij over gaat. -->
    <p class="comparison-origin"><span>{locale.t('chat.compare.origin', { pigeon: roster.name(origin.pigeonId) })}</span><button type="button" class="duif-text-action" onclick={() => void chat.openSession(origin.sessionId)}>{locale.t('chat.compare.back')}</button></p>
  {/if}
  <div class="chat-thread" aria-busy={chat.loadingHistory} data-testid="chat-thread">
    <MessageList scrollLabel={locale.t('chat.latest')} contentClass="chat-thread-content">
      {#if chat.loadingHistory}<p class="chat-loading" role="status">{locale.t('chat.history.loading')}</p>{/if}
      {#if !chat.messages.length && !chat.loadingHistory}
        <!-- De openers staan alleen op het lege scherm: zij brengen een gesprek
             op gang. Zodra er een antwoord staat bedenkt de lezer zelf wat hij
             vraagt, en biedt de chat geen vervolgvragen meer aan. -->
        <!-- Het lege gesprek toont de gekozen duif groot, in haar eigen render
             (de Kennispartner recht van voren). Een stilstaand beeld: live
             3D blijft voor de vergadering, want de renderer hoort niet in de
             eerste bundel. Het portret is decoratief; de naam staat al in de
             keuzeregel erboven. -->
        <section class="chat-starters" data-testid="chat-empty">
          <div class="starters-head">
            <div class="starters-copy">
              <span class="duif-eyebrow">{locale.t('chat.starters.title')}</span>
              <h2>{locale.t('chat.empty')}</h2>
              <p>{locale.t('chat.starters.intro')}</p>
            </div>
            <span class="starters-portrait" data-testid="chat-empty-portrait"><PigeonPortrait pigeon={chat.selectedPigeonId} /></span>
          </div>
          <div class="starter-grid">{#each CONVERSATION_CASES as item}<button type="button" data-case-id={item.id} onclick={() => useExample(item.question[locale.lang])}><small>{locale.t(item.kind === 'case' ? 'chat.starters.case' : 'chat.starters.question')}</small><span>{item.title[locale.lang]}</span><span class="starter-arrow" aria-hidden="true">↗</span></button>{/each}</div>
          <button type="button" class="duif-text-action gap-starter" data-case-id={GAP_CASE.id} onclick={() => useExample(GAP_CASE.question[locale.lang])}>{GAP_CASE.title[locale.lang]} <span aria-hidden="true">→</span></button>
        </section>
      {/if}
      {#each shown as message, i (message.key)}
        {#if repeatedKey !== '' && message.key === repeatedKey}
          <!-- Dezelfde vraag, doorgegeven aan de andere duif. Eén regel in
               plaats van een tweede bubbel met dezelfde tekst: de rij blijft
               bestaan, alleen het gesprek leest als een overdracht. -->
          <p class="repeated-question" data-testid="repeated-question">{locale.t('chat.compare.repeated', { pigeon: roster.name(chat.selectedPigeonId) })}</p>
        {:else if message.role === 'user'}
          <!-- Op de hoek van je eigen vraag de weg naar de andere duif: zo is
               te zien wélke vraag er meegaat. De knop hangt in de koptekst-slot
               van MessageBubble en ligt door `position: absolute` op de hoek
               van de bubbel, zonder dat de gedeelde component ervan weet. -->
          <div class="human-message">
            <MessageBubble role="user" content={message.content} index={i}>
              {#snippet header()}
                {#if canCompare}
                  <button
                    type="button"
                    class="ask-other"
                    data-testid="ask-other-pigeon"
                    disabled={chat.loadingComparison}
                    aria-label={compareLabel(message.content)}
                    title={compareLabel(message.content)}
                    onclick={() => void chat.beginComparison(message.key)}
                  ><ArrowBendUpRightIcon size="1rem" aria-hidden="true" /></button>
                {/if}
              {/snippet}
            </MessageBubble>
          </div>
        {:else}<AssistantMessage {message} personaName={speakerFor(message)} showHeader={headerAt(i)} index={i} waitSuffix={suffixFor(message)} quiet={quietFor(message)} />{/if}
      {/each}
      {#if awaitingPigeon}
        <!-- De duif is gevraagd, haar blok staat er nog niet. Zonder deze
             bubbel is het scherm stil terwijl de kennisbank bevraagd wordt —
             en dat leest als "er komt geen antwoord". -->
        <div class="answer-message" data-testid="pigeon-waiting" data-author={chat.selectedPigeonId} aria-busy="true">
          <div class="answer-author"><span class="answer-portrait"><PigeonPortrait pigeon={chat.selectedPigeonId} variant="head" /></span><span>{roster.name(chat.selectedPigeonId)}</span></div>
          <MessageBubble role="assistant" content="" streaming stepLabel={locale.t('chat.thinking')} index={shown.length} tail={false} />
          {#if waitSuffix !== ''}<p class="answer-waiting" data-testid="answer-waiting">{waitSuffix.trim()}</p>{/if}
        </div>
      {/if}
      <!-- De stapregel staat onder het gesprek en boven het invoerveld: op de
           plek waar de algemene voortgangszin stond, zodat het oog hem vindt
           zonder dat hij tussen de bubbels gaat zitten. De teller blijft waar
           hij was — onder de bubbel waar het antwoord komt — want daar kijkt de
           lezer, en één klok hoort maar op één plek te staan. -->
      <TurnSteps {steps} />
      {#if progress}<p class="chat-loading" role="status">{locale.t('chat.progress')}</p>{/if}
      {#if unanswered}<div class="no-answer" data-testid="chat-no-answer"><p class="chat-loading" role="status">{locale.t(unanswered.stopped ? 'chat.noAnswer' : 'chat.interruptedTurn')}</p>{@render retryAction()}</div>{/if}
      <div bind:this={anchor} aria-hidden="true"></div>
    </MessageList>
  </div>
  {#if chat.loadingComparison}<p class="chat-loading" role="status">{locale.t('chat.compare.busy')}</p>{/if}
  <p class="sr-only" aria-live="polite" data-testid="chat-status">{#key announcement?.seq}{announced}{/key}</p>
  <div class="composer-wrap"><ChatComposer errorAction={retryable ? retryAction : undefined} /></div>
</section>
<dialog bind:this={confirmation} class="duif-chat-dialog" aria-labelledby="chat-confirm-title" oncancel={() => pendingDraft = ''}>
  <h2 id="chat-confirm-title">{locale.t('chat.draft.title')}</h2>
  <p>{locale.t('chat.draft.body')}</p>
  <blockquote class="pending-draft">{pendingDraft}</blockquote>
  <footer><button type="button" class="duif-text-action" onclick={() => pendingDraft = ''}>{locale.t('chat.draft.keep')}</button><button type="button" class="chat-primary" onclick={replaceDraft}>{locale.t('chat.draft.replace')}</button></footer>
</dialog>

<style>
  /* De wachtbubbel die de duif aankondigt vóórdat haar blok er staat: dezelfde
     opmaak als een echt antwoord (AssistantMessage), zodat de overgang naar
     haar blok geen sprong is. De teller staat als eigen regel onder de bubbel
     — in de bubbel zou hij elke seconde meeflitsen met de overvloeier. */
  .answer-message { margin-bottom: 1.85rem; --answer-accent: var(--duif-persona-nonverbal); }
  [data-testid='pigeon-waiting'][data-author='biological'] { --answer-accent: var(--duif-persona-biological); }
  [data-testid='pigeon-waiting'][data-author='individualist'] { --answer-accent: var(--duif-persona-individualist); }
  [data-testid='pigeon-waiting'] .answer-author { display: flex; align-items: center; gap: .6rem; color: var(--answer-accent); font-size: .8rem; font-weight: 550; margin: .25rem .5rem .1rem; }
  [data-testid='pigeon-waiting'] .answer-portrait { width: 2.3rem; flex: none; }
  .answer-waiting { margin: .3rem .75rem 0; font-size: .75rem; font-variant-numeric: tabular-nums; color: var(--duif-dim); }

  /* Eén maatvoering voor het hele gesprek. `--duif-chat-frame` is de kolom
     waarin de balk, de startkaarten, de berichten, het voetblok en het
     invoerveld staan; `--duif-chat-read` is de leesmaat van één antwoord
     daarbinnen — een bubbel die de hele kolom vult leest niet meer prettig.
     `--duif-drawer-width` spiegelt de vaste breedte van lib/sources: de lade
     ligt over de pagina (position: fixed), dus alleen deze kolom kan opzij. */
  .duif-chat {
    /* Zonder de zijbanen is het raam weer de kolom zelf: de leesmaat
       met haar marges, en niets ernaast. */
    --duif-chat-frame: 66rem;
    --duif-chat-read: 46rem;
    --duif-drawer-width: 31rem;
    display: flex; flex-direction: column; min-height: 0; height: 100%;
    width: min(100%, var(--duif-chat-frame));
    margin: 0 auto;
    padding: 0 clamp(1rem, 3vw, 2.5rem);
    transition: width .2s ease, padding-right .2s ease;
  }
  /* Met de lade open schuift de kolom naar de vrije ruimte ernaast, zodat de
     zin waarin je een verwijzing aanklikte zichtbaar blijft. De drempel is de
     som die het antwoord heel houdt: 31 rem lade + 46 rem leesmaat + twee keer
     de zijmarge (max 2,5 rem) = 82 rem, plus een rem speling. Daaronder blijft
     de kolom staan waar hij staat en dekt de lade het scherm af met haar
     verduistering — anders zou het antwoord bij het openen opnieuw afbreken. */
  @media (min-width: 83rem) {
    .duif-chat[data-drawer='open'] {
      width: min(100%, calc(var(--duif-chat-frame) + var(--duif-drawer-width)));
      padding-right: calc(var(--duif-drawer-width) + clamp(1rem, 3vw, 2.5rem));
    }
  }
  /* A long toolbar, history or zoomed composer must not collapse the reading
     region to zero. The workspace can scroll when this minimum cannot fit. */
  .chat-thread { display: flex; flex: 1; min-height: 12rem; flex-direction: column; }
  /* Leave room for the shared message list's floating latest-message action
     when the browser scrolls a source or comparison control into focus. */
  .chat-thread :global(.overflow-y-auto) { scroll-padding-block: .5rem 4rem; }
  .chat-thread :global(.chat-thread-content) { display: flex; flex-direction: column; width: 100%; margin: 0 auto; padding: 1.4rem 0 1.6rem; }
  /* De bubbels van @parai/ui staan op 80% van hun kolom; met een bredere kolom
     is dat te breed om te lezen, dus geldt hier de leesmaat. */
  .chat-thread :global([data-testid='message-bubble'][data-role='assistant'] > div) { max-width: min(100%, var(--duif-chat-read)); }
  .chat-thread :global([data-testid='message-bubble'][data-role='user'] > div) { max-width: min(88%, 36rem); }
  .chat-starters { padding: clamp(1.2rem, 4vh, 2.5rem) .3rem; }
  /* De duif staat rechts naast de openingsvraag, in de ruimte die de korte
     kop daar openlaat, en valt met haar onderkant op de lijn van de intro. Op
     een smal scherm blijft zij klein naast de kop staan: daar kost elke regel
     erbij de startkaarten hun plek boven het invoerveld. */
  .starters-head { display: flex; align-items: flex-end; justify-content: space-between; gap: clamp(1rem, 4vw, 3rem); }
  .starters-copy { min-width: 0; }
  .starters-portrait { width: clamp(6.5rem, 13vw, 10rem); flex: none; margin-bottom: 1.4rem; }
  .chat-starters h2 { font-family: var(--font-serif); font-size: clamp(1.5rem, 3vw, 2rem); font-weight: 500; line-height: 1.35; max-width: 24ch; margin: .75rem 0; }
  .chat-starters p { color: var(--duif-dim); font-size: .9rem; line-height: 1.6; margin: 0 0 1.4rem; max-width: 58ch; }
  /* De startkaarten volgen dezelfde kolom: drie naast elkaar zolang ze breed
     genoeg blijven, daarna vanzelf minder. */
  .starter-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 15rem), 1fr)); gap: .9rem; }
  .starter-grid button { position: relative; padding: 1.15rem 1.15rem 2.2rem; text-align: left; border: 1px solid var(--duif-line); border-radius: .7rem; background: var(--duif-paper); cursor: pointer; transition: border-color .15s; }
  .starter-grid button:hover { border-color: var(--duif-ink); }
  .starter-grid small { color: var(--duif-dim); font-size: .7rem; display: block; margin-bottom: .65rem; }
  .starter-grid span { font-size: .95rem; line-height: 1.4; }
  .starter-grid .starter-arrow { position: absolute; right: .8rem; bottom: .6rem; color: var(--duif-dim); }
  .gap-starter { margin-top: 1rem; }
  .human-message { display: flex; flex-direction: column; align-items: flex-end; margin-bottom: 1.2rem; }
  /* De kolom van MessageBubble is het anker van de hoekknop: zij is precies zo
     groot als de bubbel, want de knop zelf staat buiten de stroom. */
  .chat-thread :global([data-testid='message-bubble'][data-role='user'] > div) { position: relative; }
  /* Hij ligt op de onderste hoek, half op de bubbel en half ernaast. Op de
     bubbel is het papiervlak wat hem aanwijst (gemeten 6,4:1 op het groen in
     licht, 7,3:1 in donker), naast de bubbel doet de gedempte rand dat (4,1:1
     op de grond, 10,6:1 in donker) — ruim boven de 3:1 die WCAG 1.4.11 voor
     de rand van een bediening vraagt. */
  .ask-other {
    position: absolute; left: 0; bottom: 0; transform: translate(-50%, 50%);
    display: inline-flex; align-items: center; justify-content: center;
    width: 1.9rem; height: 1.9rem; padding: 0;
    border: 1px solid var(--duif-dim); border-radius: 999px;
    background: var(--duif-paper); color: var(--duif-ink);
    box-shadow: 0 1px 4px rgb(0 0 0 / .14);
    transition: border-color .12s, background .12s;
  }
  .ask-other:hover:not(:disabled) { border-color: var(--duif-ink); background: var(--duif-feather); }
  .ask-other:disabled { cursor: wait; }
  /* De overdrachtsregel scheidt het meegereisde gesprek van het antwoord van
     de tweede duif: gedempt zoals de herkomstregel, met een haarlijn links en
     rechts zodat het een moment in de tijd is en geen systeemmelding. */
  .repeated-question { display: flex; align-items: center; gap: .8rem; margin: .1rem 0 1rem; color: var(--duif-dim); font-size: .75rem; line-height: 1.5; }
  .repeated-question::before, .repeated-question::after { content: ''; flex: 1; height: 1px; background: var(--duif-line); }
  /* Eén compacte regel boven het gesprek, met een haarlijn eronder die haar
     van de berichten scheidt. */
  .comparison-origin { display: flex; flex-wrap: wrap; align-items: baseline; gap: .2rem 1.2rem; margin: 0; padding: .55rem .3rem; font-size: .75rem; line-height: 1.5; color: var(--duif-dim); border-bottom: 1px solid var(--duif-line); }
  .chat-loading, .language-notice { color: var(--duif-dim); font-size: .8rem; line-height: 1.6; padding: .75rem; }
  .composer-wrap { flex: none; padding: .7rem 0 1.2rem; }
  /* De herstelknop staat in het invoerblok, onder de foutbanner. Zij draagt
     daar een onderstreping: naast een melding die om een handeling vraagt moet
     te zien zijn dát er iets te klikken valt, ook zonder de muis erheen. */
  .no-answer { display: flex; flex-direction: column; align-items: flex-start; gap: .1rem; margin-bottom: 1rem; }
  .no-answer .chat-loading { padding-bottom: 0; }
  .no-answer .error-retry { margin-left: .75rem; }
  .error-retry { align-self: flex-start; color: var(--duif-ink); text-decoration: underline; text-underline-offset: .3rem; }
  .pending-draft { padding: 1rem; white-space: pre-wrap; background: var(--duif-feather); border-radius: .5rem; font-size: .9rem; line-height: 1.6; }
  button:disabled { opacity: .5; cursor: default; }
  button:focus-visible { outline: 2px solid var(--duif-ink); outline-offset: 3px; }
  @media (max-width: 550px) { .starters-head { align-items: flex-start; gap: .8rem; } .starters-portrait { width: 3.75rem; margin: 0; } .starter-grid { grid-template-columns: 1fr; } .starter-grid button { padding: .8rem 2rem .8rem 1rem; } .starter-grid small { margin-bottom: .2rem; } .starter-grid .starter-arrow { bottom: .9rem; } .chat-starters { padding-top: 1rem; } }
</style>
