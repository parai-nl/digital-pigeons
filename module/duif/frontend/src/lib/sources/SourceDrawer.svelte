<script lang="ts">
  import { tick } from 'svelte'
  import { bibliographyNotice } from '../bibliography'
  import { paperTitle, sourceKey } from './title'
  import { trailSteps } from './trail'
  import { sourcePerspective, sourceText, sourceTranslated } from '../sources'
  import { useServices } from '../services.svelte'

  // De bronlade. Eén exemplaar in de hele module (App.svelte hangt hem op);
  // elk antwoord opent hem via de gedeelde store. Onder een antwoord staat
  // daardoor geen lijst kaarten meer: alleen de verwijzingen in de zin en één
  // rustige regel "3 bronnen".
  //
  // De lade legt zich over de inhoud heen — `position: fixed`, dus de chat
  // schuift niet op. Escape, de sluitknop en (als hij er is) de verduistering
  // sluiten hem; een klik op een verwijzing onder de verduistering springt naar
  // die bron (backdropClick). De focus gaat naar de lade en keert terug naar de
  // knop die hem opende. Scrollen boven de verduistering, met wiel of vinger,
  // schuift het gesprek eronder.
  //
  // Waarom de verduistering soms wegblijft: tijdens een lopende vergadering is
  // STOP de belangrijkste knop op het scherm. Achter een verduistering zou de
  // eerste klik daarop de lade sluiten en pas de tweede echt stoppen. De lade
  // is daar dus een gewoon zijpaneel — hij vangt geen muisklikken af en houdt
  // de focus niet vast, zodat STOP één klik en één tab weg blijft. Escape en de
  // sluitknop blijven werken.
  //
  // De opbouw, van boven naar beneden, met bij elk blok een label dat zegt wat
  // het is:
  //   1. één stille regel "PERSPECTIEF · Strikte plaagbestrijding": waar dit
  //      standpunt vandaan komt;
  //   2. het standpunt uit deze bron — het visuele hoofdelement, in een
  //      grotere letter dan al het andere, met één regel die zegt dat dit een
  //      samenvatting is en waar je die kunt controleren;
  //   3. de originele woorden, cursief;
  //   4. "Over deze bron", dichtgeklapt: bronvermelding, bronsleutel, doi als
  //      tekst, de bibliografische waarschuwing en het kruimelpad.
  //
  // Wat weg is: de auteurslijst als titel (die zei de lezer niets en stond ook
  // al in de bronvermelding), de buurtgraaf, en de kop met
  // het perspectief erin. Perspectieven zijn breed: in 51 van de 59 antwoorden
  // met meerdere bronnen deelden minstens twee bronnen er één, zodat zo'n kop
  // de bron niet identificeerde en bij "Volgende bron" gewoon bleef staan. Als
  // regel met een label doet hij wél wat hij moet doen.
  //

  const { locale, shell, sources } = useServices()

  const source = $derived(sources.current)
  // Het perspectief uit de kennisbank ("Wetenschappelijke interesse") is het
  // laatste niveau van het kruimelpad; het staat al op de bron zelf, dus het
  // is er zonder op de structuurvraag te wachten.
  const perspective = $derived(sourcePerspective(source, locale.pickContent).trim())
  // De samenvatting in de taal van de lezer, met terugval op de taal die er wél
  // is; een oud opgeslagen antwoord houdt zijn eigen momentopname.
  const summary = $derived(sourceText(source, locale.pickContent))
  // Staat er een vertaling in plaats van het origineel, dan zegt de kaart dat.
  // Het citaat eronder is nooit vertaald.
  const translated = $derived(sourceTranslated(source, locale.contentLang))
  const passages = $derived(source?.ref?.passages?.length ? source.ref.passages : source?.ref?.quote ? [{ text: source.ref.quote }] : [])
  // De regel "dit is de samenvatting, en hier kun je dat controleren" blijft
  // weg bij een afgekeurde bron zonder passages: de afkeuringsregel eronder
  // zegt hetzelfde en zegt er bovendien bij waaróm de passages ontbreken. Twee
  // grijze regels die allebei melden dat de passage er niet bij staat, laten de
  // lezer zoeken naar het verschil dat er niet is.
  const showReading = $derived(passages.length > 0 || source?.grounding !== 'rejected')
  // De bibliografische waarschuwing één keer. Ontbreken er gegevens, dan zegt
  // die ene zin welke — plus dat het niets zegt over het onderzoek. Alleen als
  // er niets ontbreekt komt de algemene opmerking (te controleren gegevens,
  // geen bronvermelding) van bibliographyNotice erdoorheen.
  const missing = $derived(source?.ref?.citationMissing?.trim() ?? '')
  const note = $derived(missing === '' && source ? bibliographyNotice(source.ref, locale.lang) : '')
  // Een doi is een adres; de module opent geen adressen buiten haar eigen API
  // (frontend-wachter). Hij staat er dus als tekst, nooit als link. En niet
  // twee keer: de meeste bronvermeldingen dragen hem al in hun staart.
  const doi = $derived(source?.ref?.doi ?? '')
  const showDoi = $derived(doi !== '' && !(source?.ref?.citation ?? '').includes(doi))
  const paper = $derived(paperTitle(source?.ref))
  const key = $derived(sourceKey(source?.ref))
  // Het kruimelpad in de taal van de lezer; een subthema met de titel van zijn
  // thema staat er één keer in (trailSteps).
  const trail = $derived(trailSteps(sources.trail, locale.pickContent))
  const hasAbout = $derived(paper !== '' || key !== '' || !!source?.ref?.citation || showDoi || missing !== '' || note !== '' || trail.length > 0)

  let closeButton: HTMLButtonElement | undefined = $state()
  let returnFocus: HTMLElement | null = null
  // Is het gesprek met de lade open via de verduistering verschoven? Dan staat
  // de lezer waar hij wil staan, en mag het teruggeven van de focus hem niet
  // terugtrekken naar de verwijzing waar hij begon.
  let scrolledBelow = false

  // De focus hoort bij het scherm, niet bij de store. Bij het openen staat de
  // aanroepende knop nog scherp: die onthouden we en geven we terug zodra de
  // lade dichtgaat — ook als hij via Escape of de verduistering sloot.
  $effect(() => {
    if (!sources.open) return
    returnFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
    scrolledBelow = false
    void tick().then(() => closeButton?.focus())
    return () => {
      stopGlide()
      const previous = returnFocus
      const stay = scrolledBelow
      returnFocus = null
      void tick().then(() => {
        if (previous?.isConnected) previous.focus({ preventScroll: stay })
      })
    }
  })

  function close(): void {
    sources.hide()
  }

  // Een klik op de verduistering sluit de lade, behalve als er onder die klik
  // een verwijzing staat. Wie een antwoord zin voor zin natrekt, loopt van [9]
  // naar [17]; zonder dit kostte elke sprong eerst een blinde klik om te
  // sluiten. De klik gaat dan door naar die verwijzing, die haar
  // eigen bron opent. De lade blijft open en de focus blijft erin.
  function backdropClick(event: MouseEvent): void {
    // Een tik die een lopende uitloop stilzet, is geen keuze voor wat eronder
    // ligt: de lade blijft dan gewoon open. Een muisklik telt altijd.
    const swallow = swallowClick && (event as PointerEvent).pointerType !== 'mouse'
    swallowClick = false
    if (swallow) return
    const cite = citationBelow(event)
    if (!cite) {
      close()
      return
    }
    cite.click()
    closeButton?.focus()
  }

  // Zolang de lade open is, kan elke verwijzing hem op een andere bron zetten:
  // met de muis door de verduistering heen of met Tab en Enter. Bij sluiten
  // keert de focus terug naar de verwijzing die het laatst gekozen werd, niet
  // naar de knop die de lade ooit opende.
  function rememberCitation(event: MouseEvent): void {
    if (!sources.open || !(event.target instanceof Element)) return
    const cite = event.target.closest('[data-source-cite]')
    if (cite instanceof HTMLElement) returnFocus = cite
  }

  function citationBelow(event: MouseEvent): HTMLElement | null {
    const cite = elementBelow(event.clientX, event.clientY, event.currentTarget)?.closest('[data-source-cite]')
    return cite instanceof HTMLElement ? cite : null
  }

  function elementBelow(x: number, y: number, backdrop: EventTarget | null): Element | undefined {
    if (typeof document.elementsFromPoint !== 'function') return undefined
    return document.elementsFromPoint(x, y).find((el) => el !== backdrop)
  }

  // Scrollen boven de verduistering schuift wat eronder ligt, meestal de
  // berichtenlijst. Wie een antwoord met twintig bronnen natrekt, wil met de
  // lade open bij de verwijzing verderop komen; zonder dit stond het gesprek
  // stil tot de lade dicht was. De verduistering zelf blijft klikken
  // opvangen, want een klik ernaast moet sluiten en niet de knop eronder
  // indrukken. De browser stuurt wiel en vinger naar de laag waar ze op landen,
  // en die zit niet in de lijst; dus geven we ze hier zelf door. Ligt er onder
  // de cursor niets dat scrolt (de kop, het invoerveld, de rand naast de
  // lijst), dan schuift het gesprek zelf: overal boven de verduistering doet
  // scrollen hetzelfde.
  function scrollerBelow(x: number, y: number, backdrop: EventTarget | null): HTMLElement | null {
    for (let el = elementBelow(x, y, backdrop); el; el = el.parentElement ?? undefined) {
      if (el instanceof HTMLElement && scrolls(el)) return el
    }
    const conversation = document.querySelector('[data-testid="message-list-scroll"]')
    return conversation instanceof HTMLElement && scrolls(conversation) ? conversation : null
  }

  function scrolls(el: HTMLElement): boolean {
    const { overflowY } = getComputedStyle(el)
    return (overflowY === 'auto' || overflowY === 'scroll') && el.scrollHeight > el.clientHeight
  }

  function scrollBelow(scroller: HTMLElement, left: number, top: number): void {
    scrolledBelow = true
    scroller.scrollBy({ left, top })
  }

  function backdropWheel(event: WheelEvent): void {
    // Ctrl met wiel is zoomen; dat blijft van de browser.
    if (event.ctrlKey) return
    const scroller = scrollerBelow(event.clientX, event.clientY, event.currentTarget)
    if (!scroller) return
    event.preventDefault()
    stopGlide()
    const unit = event.deltaMode === WheelEvent.DOM_DELTA_LINE ? 16 : event.deltaMode === WheelEvent.DOM_DELTA_PAGE ? scroller.clientHeight : 1
    // Shift met wiel is opzij scrollen. Niet elke browser zet de beweging dan
    // zelf om naar deltaX; staat die nog op deltaY, dan doen we dat hier.
    const sideways = event.shiftKey && event.deltaX === 0
    scrollBelow(scroller, (sideways ? event.deltaY : event.deltaX) * unit, sideways ? 0 : event.deltaY * unit)
  }

  // De vinger: de verduistering laat de browser niet zelf pannen
  // (`touch-action`), dus schuiven we de lijst mee met de vinger en laten hem
  // na het loslaten uitlopen, zoals een gewone veeg. Een tik blijft een klik,
  // behalve een tik die de uitloop stilzet: die houdt alleen het gesprek tegen,
  // zoals bij elke andere lijst, en sluit of springt niet (swallowClick).
  let pan: { id: number; x: number; y: number; at: number; vx: number; vy: number; scroller: HTMLElement } | null = null
  let glide = 0
  let swallowClick = false

  function stopGlide(): void {
    if (glide) cancelAnimationFrame(glide)
    glide = 0
  }

  function backdropTouchStart(event: TouchEvent): void {
    swallowClick = glide !== 0
    stopGlide()
    const touch = event.touches.length === 1 ? event.touches[0] : undefined
    const scroller = touch ? scrollerBelow(touch.clientX, touch.clientY, event.currentTarget) : null
    pan = touch && scroller ? { id: touch.identifier, x: touch.clientX, y: touch.clientY, at: event.timeStamp, vx: 0, vy: 0, scroller } : null
  }

  function backdropTouchMove(event: TouchEvent): void {
    const current = pan
    const touch = current ? Array.from(event.touches).find((t) => t.identifier === current.id) : undefined
    if (!current || !touch) return
    const dx = current.x - touch.clientX
    const dy = current.y - touch.clientY
    const dt = Math.max(1, event.timeStamp - current.at)
    scrollBelow(current.scroller, dx, dy)
    // Snelheid in px per ms, licht uitgemiddeld tegen haperende meetpunten.
    pan = { ...current, x: touch.clientX, y: touch.clientY, at: event.timeStamp, vx: 0.8 * (dx / dt) + 0.2 * current.vx, vy: 0.8 * (dy / dt) + 0.2 * current.vy }
  }

  function backdropTouchEnd(event: TouchEvent): void {
    const current = pan
    pan = null
    // Bleef de vinger even stil liggen voor het loslaten, of was het een tik,
    // dan geen uitloop.
    if (!current || event.timeStamp - current.at > 100 || !moving(current.vx, current.vy)) return
    let { vx, vy } = current
    let last = performance.now()
    const step = (now: number): void => {
      const dt = now - last
      last = now
      scrollBelow(current.scroller, vx * dt, vy * dt)
      const decay = Math.pow(0.95, dt / 16)
      vx *= decay
      vy *= decay
      glide = moving(vx, vy) ? requestAnimationFrame(step) : 0
    }
    glide = requestAnimationFrame(step)
  }

  // Trager dan 0,02 px per ms is geen uitloop meer.
  function moving(vx: number, vy: number): boolean {
    return Math.abs(vx) + Math.abs(vy) > 0.02
  }

  function openInGraph(id: string): void {
    sources.hide()
    shell.openInGraph(id)
  }
</script>

<svelte:window onclickcapture={rememberCitation} onkeydown={(event) => { if (sources.open && event.key === 'Escape' && !event.defaultPrevented) { event.preventDefault(); close() } }} />

{#if sources.open && source}
  {#if sources.backdrop}
    <div
      class="drawer-backdrop"
      role="presentation"
      data-testid="source-backdrop"
      onclick={backdropClick}
      onwheel={backdropWheel}
      ontouchstart={backdropTouchStart}
      ontouchmove={backdropTouchMove}
      ontouchend={backdropTouchEnd}
      ontouchcancel={() => { pan = null }}
    ></div>
  {/if}
  <div
    class="source-drawer"
    class:plain={!sources.backdrop}
    data-testid="source-drawer"
    role="dialog"
    aria-modal={sources.backdrop}
    aria-label={locale.t('chat.source.panel')}
  >
    <header class="drawer-head">
      <p class="drawer-kicker" data-testid="source-position">{locale.t('chat.source.position', { number: sources.index + 1, total: sources.total })}</p>
      <button type="button" class="drawer-close" bind:this={closeButton} onclick={close} aria-label={locale.t('chat.source.close')} data-testid="source-close"><span aria-hidden="true">✕</span></button>
    </header>

    <div class="drawer-body">
      {#if perspective !== ''}
        <p class="drawer-perspective" data-testid="source-perspective"><span class="label-inline">{locale.t('chat.source.perspective')}<span class="duif-visually-hidden">:</span></span><span class="perspective-sep" aria-hidden="true">·</span>{perspective}</p>
      {/if}

      {#if summary !== ''}
        <section class="drawer-block" data-testid="source-finding">
          <p class="block-label">{locale.t('chat.source.finding')}</p>
          <p class="drawer-summary" data-testid="source-summary">{summary}</p>
          {#if showReading}
            <p class="block-note" data-testid="source-reading">{locale.t(passages.length ? 'chat.source.summary' : 'chat.source.summaryAlone')}</p>
          {/if}
          {#if translated}
            <p class="block-note" data-testid="source-translated">{locale.t('chat.source.translated')}</p>
          {/if}
          {#if source.grounding === 'unverified'}
            <p class="block-note" data-testid="source-unverified">{locale.t('chat.source.unverified')}</p>
          {:else if source.grounding === 'rejected'}
            <!-- Sterker dan "niet vergeleken": hier ís vergeleken, en de
                 oorspronkelijke woorden waren niet terug te vinden. De bron
                 staat er nog, haar passages niet meer. -->
            <p class="block-note" data-testid="source-rejected">{locale.t('chat.source.rejected')}</p>
          {/if}
        </section>
      {/if}

      {#if passages.length}
        <section class="drawer-block" data-testid="source-original">
          <p class="block-label">{locale.t(paper !== '' ? 'chat.source.original' : 'chat.source.originalPlain')}</p>
          {#each passages as passage, i (passage.id ?? i)}
            <blockquote class="duif-voice duif-quote" data-testid="source-quote">{passage.text}</blockquote>
          {/each}
        </section>
      {/if}

      {#if hasAbout}
        <details class="drawer-about" data-testid="source-about">
          <summary class="about-summary">{locale.t('chat.source.about')}</summary>
          <div class="about-body">
            {#if source.ref?.citation}<p class="drawer-citation" data-testid="source-citation"><span>{locale.t('chat.source.citation')}</span> {source.ref.citation}</p>{/if}
            {#if paper !== ''}<p class="drawer-paper" data-testid="source-paper">{locale.t('chat.source.paper', { paper })}</p>{/if}
            {#if key !== '' && !source?.ref?.citation}<p class="drawer-paper" data-testid="source-key">{locale.t('chat.source.key', { key })}</p>{/if}
            {#if showDoi}<p class="drawer-doi" data-testid="source-doi">{locale.t('chat.source.doi', { doi })}</p>{/if}
            {#if missing !== ''}<p class="about-note" data-testid="source-bibliography-missing">{locale.t('chat.source.citationMissing', { missing })}</p>{/if}
            {#if note}<p class="about-note" data-testid="source-bibliography-note">{note}</p>{/if}
            {#if trail.length}
              <p class="block-label about-trail-label">{locale.t('chat.source.trail')}</p>
              <nav class="drawer-trail" data-testid="source-trail" aria-label={locale.t('chat.source.trail')}>
                {#each trail as step, i (step.id)}{#if i > 0}<span class="trail-sep" aria-hidden="true">›</span>{/if}<span class="trail-step">{step.text}</span>{/each}
              </nav>
            {/if}
          </div>
        </details>
      {/if}
    </div>

    <footer class="drawer-foot">
      <div class="drawer-steps">
        <button type="button" class="duif-quiet-button" data-testid="source-previous" disabled={sources.index === 0} onclick={() => sources.step(-1)}><span aria-hidden="true">‹</span> {locale.t('chat.source.previous')}</button>
        <button type="button" class="duif-quiet-button" data-testid="source-next" disabled={sources.index >= sources.total - 1} onclick={() => sources.step(1)}>{locale.t('chat.source.next')} <span aria-hidden="true">›</span></button>
      </div>
      {#if source.id}
        {@const nodeId = source.id}
        <button type="button" class="duif-text-action" data-testid="source-graph-button" onclick={() => openInGraph(nodeId)}>{locale.t('chat.source.graph')} <span aria-hidden="true">↗</span></button>
      {/if}
    </footer>
  </div>
{/if}

<style>
  .drawer-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: color-mix(in srgb, var(--duif-ink) 28%, transparent);
    /* Een vinger pant hier niet de pagina: backdropTouchMove schuift het
       gesprek eronder. Knijpen om te zoomen blijft van de browser. */
    touch-action: pinch-zoom;
  }

  .source-drawer {
    position: fixed;
    inset-block: 0;
    right: 0;
    z-index: 41;
    display: flex;
    flex-direction: column;
    width: min(31rem, 100vw);
    background: var(--duif-paper);
    border-left: 1px solid var(--duif-line);
    box-shadow: -18px 0 48px #0000000f;
    animation: duif-drawer-in 220ms cubic-bezier(.22, 1, .36, 1);
  }

  /* Zonder verduistering hoort de lade minder hard op de voorgrond te staan:
     het scherm eronder blijft volwaardig in gebruik. */
  .source-drawer.plain { box-shadow: -14px 0 36px #0000000a; }

  .drawer-head {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding: .9rem 1.1rem .7rem;
    border-bottom: 1px solid var(--duif-line);
  }

  .drawer-kicker {
    margin: 0;
    font-size: .65rem;
    font-weight: 650;
    letter-spacing: .12em;
    text-transform: uppercase;
    color: var(--duif-dim);
  }

  .drawer-close {
    flex: none;
    min-width: 2.25rem;
    min-height: 2.25rem;
    border: 0;
    border-radius: .5rem;
    background: transparent;
    color: var(--duif-dim);
    font-size: .9rem;
    line-height: 1;
  }

  .drawer-close:hover { background: var(--duif-feather); color: var(--duif-ink); }

  .drawer-body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 1.2rem 1.1rem 1.6rem;
  }

  /* Eén stille regel: waar dit standpunt vandaan komt. Bewust geen kop — hij
     is te breed om de bron te identificeren, en hij mag het standpunt eronder
     niet overstemmen. */
  .drawer-perspective {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: .4rem;
    margin: 0;
    font-size: .72rem;
    color: var(--duif-dim);
    overflow-wrap: anywhere;
  }

  .perspective-sep { color: var(--duif-line); }

  /* Elk blok draagt een label dat zegt wat je leest. Het label is klein en
     stil; de inhoud eronder draagt het gewicht. */
  .drawer-block { margin-top: 1.3rem; }
  .drawer-block:first-child { margin-top: .1rem; }

  .block-label, .label-inline {
    font-size: .62rem;
    font-weight: 650;
    letter-spacing: .12em;
    text-transform: uppercase;
    color: var(--duif-dim);
  }

  .block-label { display: block; margin: 0 0 .5rem; }

  /* Het standpunt is waar de lezer voor kwam: het staat bovenaan en het is het
     grootste op het scherm. Alles eronder — passage, brongegevens — leest
     kleiner. */
  .drawer-summary {
    font-size: 1.15rem;
    font-weight: 400;
    line-height: 1.55;
    letter-spacing: -.005em;
    margin: 0;
    color: var(--duif-ink);
  }

  .block-note {
    font-size: .72rem;
    line-height: 1.6;
    color: var(--duif-dim);
    margin: .5rem 0 0;
  }

  /* Brongegevens zijn er voor wie ze wil: dicht bij het openen, één klik open,
     en met <details> ook vanaf het toetsenbord te bedienen. */
  .drawer-about {
    margin-top: 1.6rem;
    padding-top: .9rem;
    border-top: 1px solid var(--duif-line);
  }

  .about-summary {
    list-style: none;
    display: flex;
    align-items: center;
    gap: .45rem;
    min-height: 2.25rem;
    font-size: .78rem;
    font-weight: 600;
    color: var(--duif-dim);
    cursor: pointer;
  }

  .about-summary::-webkit-details-marker { display: none; }

  .about-summary::before {
    content: '›';
    display: inline-block;
    font-size: .95rem;
    line-height: 1;
    transition: transform 140ms ease;
  }

  .drawer-about[open] .about-summary::before { transform: rotate(90deg); }
  .about-summary:hover { color: var(--duif-ink); }

  .about-body {
    font-size: .78rem;
    line-height: 1.6;
    padding: .2rem 0 .3rem 1.05rem;
  }

  .about-body p { margin: .35rem 0; overflow-wrap: anywhere; }
  .drawer-citation { white-space: pre-wrap; }
  .drawer-citation > span { font-weight: 600; }
  .drawer-paper, .about-note { color: var(--duif-dim); }
  .about-trail-label { margin-top: .9rem; }

  /* Het kruimelpad is oriëntatie, geen navigatie: leesbare labels, geen id's,
     en het breekt gewoon af als het niet op één regel past. */
  .drawer-trail {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: .3rem .45rem;
    font-size: .72rem;
    color: var(--duif-dim);
  }

  .trail-sep { color: var(--duif-line); }
  .trail-step { overflow-wrap: anywhere; }

  .drawer-foot {
    flex: none;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: .5rem 1rem;
    padding: .7rem 1.1rem;
    border-top: 1px solid var(--duif-line);
    background: var(--duif-paper);
  }

  .drawer-steps { display: flex; gap: .5rem; }

  .duif-quote {
    font-size: .9rem;
    font-style: italic;
    line-height: 1.7;
    margin: 0 0 .6rem;
    padding-left: .8rem;
    border-left: 2px solid var(--duif-feather);
    overflow-wrap: anywhere;
  }

  .duif-quote:last-child { margin-bottom: 0; }

  /* Typografische aanhalingstekens uit de CSS: de tekst zelf blijft een kale
     interpolatie, zonder één teken opmaak erin. */
  .duif-quote::before { content: '„'; }
  .duif-quote::after { content: '”'; }

  @keyframes duif-drawer-in {
    from { transform: translateX(1.5rem); opacity: 0; }
    to { transform: translateX(0); opacity: 1; }
  }

  /* Smal scherm: de lade wordt een onderpaneel over de volle breedte. */
  @media (max-width: 700px) {
    .source-drawer {
      inset-block: auto 0;
      inset-inline: 0;
      width: auto;
      max-height: 85vh;
      border-left: 0;
      border-top: 1px solid var(--duif-line);
      border-radius: var(--duif-radius-stage) var(--duif-radius-stage) 0 0;
      box-shadow: 0 -18px 48px #0000000f;
      animation-name: duif-sheet-in;
    }
  }

  @keyframes duif-sheet-in {
    from { transform: translateY(1.5rem); opacity: 0; }
    to { transform: translateY(0); opacity: 1; }
  }

  @media (prefers-reduced-motion: reduce) {
    .source-drawer { animation: none; }
    .about-summary::before { transition: none; }
  }
</style>
