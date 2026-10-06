<script module lang="ts">
  import { COMPACT_WIDTH } from './renderer'

  // De breedte van de boomkolom in pixels. Hij staat op moduleniveau zodat hij
  // een tabwissel overleeft: wie de scheiding verzet, naar het gesprek gaat en
  // terugkomt, vindt de kolom terug zoals hij hem zette. Verder dan de sessie
  // gaat hij niet — de bundelwachter laat deze module alleen de taalkeuze op
  // het apparaat bewaren, en een kolombreedte is geen taalkeuze.
  const TREE_MIN = 190
  const TREE_MAX = 560
  const TREE_DEFAULT = 320
  let rememberedTree = TREE_DEFAULT

  // De drie andere kolommen van de werkruimte, in dezelfde maten als het
  // stijlblok onderaan: de scheiding zelf (.6rem) en het leespaneel, dat tot
  // 26rem uitzet zodra er ruimte is. Wat overblijft is de kaart, en die mag
  // nooit onder COMPACT_WIDTH zakken — daaronder laat de tekenaar élk label
  // weg. De marge erbovenop houdt ook op een klein scherm nog wat lucht rond de
  // tekening.
  const SPLIT_WIDTH = 10
  const READING_WIDTH = 416
  const MAP_MIN = COMPACT_WIDTH + 40

  // treeCeiling is de breedste boom die op dít scherm nog een leesbare kaart
  // overlaat. Zonder deze rem levert "End" op een laptop van 1366 een kolom van
  // 560 op, een kaart van 380, en dus een tekening zonder één naam erin.
  function treeCeiling(available: number): number {
    if (available <= 0) return TREE_MAX
    return Math.max(TREE_MIN, Math.min(TREE_MAX, available - SPLIT_WIDTH - READING_WIDTH - MAP_MIN))
  }
</script>

<script lang="ts">
  // De graafweergave is kaart-eerst: wat er opengaat is een plattegrond met de
  // domeinen en hun thema's, niet een lijst met tekst. De lijst blijft bestaan
  // als toegankelijk alternatief — dezelfde acties, met het toetsenbord — maar
  // hij staat achter een schakelaar in de knoppenbalk in plaats van ervoor.
  //
  // Er is één knoppenbalk: zoeken, relaties, legenda, weergave, terug. Alles wat
  // uitleg is (waar deze kaart wel en niet over gaat, wat de vormen en lijnen
  // betekenen) zit in de legenda, en die staat dicht.
  import { tick, untrack } from 'svelte'
  import Banner from '@parai/ui/components/Banner.svelte'
  import Button from '@parai/ui/components/Button.svelte'
  import Input from '@parai/ui/components/Input.svelte'
  import Spinner from '@parai/ui/components/Spinner.svelte'
  import KnowledgeBanner from '../KnowledgeBanner.svelte'
  import type { ImportState } from '../knowledge.svelte'
  import { useServices } from '../services.svelte'
  import { makePalette, type Palette } from './colors'
  import DetailPanel from './DetailPanel.svelte'
  import GraphMap from './GraphMap.svelte'
  import GraphTree from './GraphTree.svelte'
  import type { GraphControls } from './layout'
  import { graphTranslator, readingEn, readingNl } from './readingCopy'
  import { NODE_TYPES } from './types'
  import { relationStyle } from './visuals'

  const { locale, graph, knowledge } = useServices()
  const t = $derived(graphTranslator(locale))
  const copy = $derived(locale.lang === 'nl' ? readingNl : readingEn)
  let controls = $state<GraphControls | null>(null)
  let tree = $state<ReturnType<typeof GraphTree> | null>(null)
  let detailPanel = $state<ReturnType<typeof DetailPanel> | null>(null)
  let host: HTMLDivElement | undefined = $state()
  let palette = $state<Palette | null>(null)
  // De kaart is de standaard. De lijst is het alternatief, niet de voorkant.
  let showList = $state(false)
  let detailClosed = $state(false)
  const detail = $derived(detailClosed ? null : graph.selectedNode)

  $effect(() => {
    const wanted = graph.visible && graph.phase === 'idle'
    untrack(() => {
      if (wanted) {
        void knowledge.load()
        void graph.load()
      }
    })
  })

  // De store kent de services niet, dus de UI-taal komt van hier. Een wissel
  // her-kiest de tekst van wat al geladen is: geen tweede verzoek, en de tak
  // waar de lezer in zat blijft open staan.
  $effect(() => {
    const lang = locale.lang
    untrack(() => graph.setLang(lang))
  })

  // De tokens van de gastheer worden één keer gelezen; het leespaneel kleurt
  // zijn vlaggetje daarmee in dezelfde tint als de knoop op de kaart.
  $effect(() => {
    const el = host
    untrack(() => {
      if (el && !palette) palette = makePalette(graph.ports.readTheme(el))
    })
  })

  const kbState = $derived(knowledge.status?.state ?? graph.kb)
  let previous: ImportState | null = null
  $effect(() => {
    const state = kbState
    untrack(() => {
      if (previous === 'importing' && state === 'ready') {
        if (graph.visible && graph.phase !== 'idle') void graph.load()
        else graph.invalidate()
      }
      previous = state
    })
  })

  const noticeText = $derived(graph.notice ? locale.fromServer(graph.notice.message, graph.notice.reason, 'error.generic') : '')
  const emptyText = $derived(
    kbState === 'importing'
      ? 'graph.empty.importing'
      : kbState === 'incomplete'
        ? 'graph.empty.incomplete'
        : kbState === 'failed'
          ? 'graph.empty.failed'
          : 'graph.empty.body',
  )

  function closeDetail(): void {
    detailClosed = true
    // De omtrek staat er altijd; zonder deze regel valt de aandacht op <body>
    // en begint het toetsenbord weer bovenaan de pagina.
    void tree?.focusActive()
  }
  async function readDetail(): Promise<void> {
    detailClosed = false
    await tick()
    detailPanel?.focusTitle()
  }

  // Een <details> gaat niet vanzelf dicht als je ergens anders klikt, en een
  // opengeklapt menu dat over de kaart blijft hangen is precies de onrust die
  // deze knoppenbalk moet wegnemen. Één handler sluit wat niet meer aangewezen
  // wordt; klikken ín het menu laat het staan.
  let toolbar: HTMLDivElement | undefined = $state()
  // De zoomknoppen liggen over het doek; de kaart houdt hun vak vrij van namen.
  let mapControls: HTMLDivElement | undefined = $state()
  function closeMenus(event: PointerEvent): void {
    if (!toolbar) return
    const target = event.target
    for (const menu of toolbar.querySelectorAll<HTMLDetailsElement>('details[open]')) {
      if (target instanceof Node && menu.contains(target)) continue
      menu.open = false
    }
  }

  // --- de versleepbare scheiding ---------------------------------------------
  // De boom was elf tot vijftien rem breed en niet te verzetten; namen van
  // domeinen en thema's braken daar middenin af. Hij begint nu ruimer, en de
  // scheiding ernaast is te verslepen én met het toetsenbord te verzetten
  // (APG's window splitter: pijltjes, Home, End, en Enter terug naar de
  // standaardmaat).
  let workspace: HTMLDivElement | undefined = $state()
  let treeWidth = $state(rememberedTree)
  let treeMax = $state(TREE_MAX)
  let resizing = $state(false)

  function setTreeWidth(px: number): void {
    const ceiling = treeCeiling(workspace?.clientWidth ?? 0)
    treeMax = ceiling
    treeWidth = Math.round(Math.min(ceiling, Math.max(TREE_MIN, px)))
    rememberedTree = treeWidth
  }

  // Het venster verandert van maat: de kolom die gisteren paste kan nu de kaart
  // verdringen, dus hij wordt opnieuw geklemd. De camera kadert daarna vanzelf
  // opnieuw — de kaart merkt zijn eigen maatwissel op.
  function onWindowResize(): void {
    setTreeWidth(treeWidth)
  }

  // Bij het opengaan is de werkruimte nog niet gemeten; één klem zodra hij er
  // staat zet de onthouden breedte binnen de grenzen van dít scherm.
  $effect(() => {
    const el = workspace
    untrack(() => {
      if (el) setTreeWidth(treeWidth)
    })
  })

  function onSplitDown(event: PointerEvent): void {
    const handle = event.currentTarget
    if (handle instanceof HTMLElement) handle.setPointerCapture?.(event.pointerId)
    resizing = true
    // Zonder dit begint de browser tekst te selecteren zodra je sleept.
    event.preventDefault()
  }

  function onSplitMove(event: PointerEvent): void {
    if (!resizing || !workspace) return
    setTreeWidth(event.clientX - workspace.getBoundingClientRect().left)
  }

  function onSplitUp(event: PointerEvent): void {
    const handle = event.currentTarget
    if (handle instanceof HTMLElement && handle.hasPointerCapture?.(event.pointerId)) {
      handle.releasePointerCapture?.(event.pointerId)
    }
    resizing = false
  }

  function onSplitKey(event: KeyboardEvent): void {
    const step = event.shiftKey ? 48 : 16
    switch (event.key) {
      case 'ArrowLeft':
        setTreeWidth(treeWidth - step)
        break
      case 'ArrowRight':
        setTreeWidth(treeWidth + step)
        break
      case 'Home':
        setTreeWidth(TREE_MIN)
        break
      case 'End':
        setTreeWidth(treeCeiling(workspace?.clientWidth ?? 0))
        break
      case 'Enter':
      case ' ':
        setTreeWidth(TREE_DEFAULT)
        break
      default:
        return
    }
    event.preventDefault()
  }

  // --- terug naar de beginstand ----------------------------------------------
  // "Passend maken" verzet alleen de camera; wie drie niveaus diep zat bleef
  // drie niveaus diep zitten, alleen dan uitgezoomd. Beginstand zet de graaf
  // zelf terug op wat je bij het openen zag — de domeinen met hun thema's —
  // laat de keuze los, sluit het leespaneel en kadert opnieuw.
  async function goHome(): Promise<void> {
    graph.home()
    detailClosed = false
    tree?.toTop()
    // Eerst laten de effecten de layout bijwerken (de dichtgeklapte takken
    // verdwijnen); pas daarna mag de camera kaderen.
    await tick()
    controls?.home()
  }

  function toggleRelation(type: string): void {
    const next = new Set(graph.hiddenRelations)
    if (next.has(type)) next.delete(type)
    else next.add(type)
    graph.hiddenRelations = next
  }

  let last: string | null = null
  let lastSelectionRequest = 0
  $effect(() => {
    const selected = graph.selected
    const request = graph.selectionRequest
    untrack(() => {
      if (selected !== last || request !== lastSelectionRequest) detailClosed = false
      last = selected
      lastSelectionRequest = request
    })
  })
</script>

<svelte:window onpointerdown={closeMenus} onresize={onWindowResize} />

<!-- Een huisje, met de hand getekend in zestien bij zestien: klein genoeg om
     naast een woord te staan en groot genoeg om in een werkbalk op te vallen. -->
{#snippet house()}
  <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" focusable="false"
    ><path d="M8 1.4 1.3 7.1v7.5h4.4v-4.5h4.6v4.5h4.4V7.1z" fill="currentColor" /></svg
  >
{/snippet}

<div class="graph-view h-full overflow-hidden" data-testid="graph-view" bind:this={host}>
  <div class="graph-frame">
    {#if kbState && kbState !== 'ready'}<KnowledgeBanner class="mx-4 mt-2" />{/if}
    {#if knowledge.unreachable}<Banner class="mx-4 mt-2" tone="warning" message={t('graph.kb.unreachable')} data-testid="graph-kb-banner" />{/if}
    {#if graph.notice}
      <Banner
        class="mx-4 mt-2"
        tone="danger"
        role="alert"
        aria-live="assertive"
        message={noticeText}
        data-testid="graph-notice"
        dismissible
        dismissLabel={t('graph.close')}
        onDismiss={() => (graph.notice = null)}
      />
    {/if}
    {#if graph.phase === 'failed'}
      <div class="graph-retry"><Button variant="outline" size="sm" data-testid="graph-retry" onclick={() => void graph.load()}>{t('graph.retry')}</Button></div>
    {/if}

    {#if graph.phase === 'loading' && graph.roots.length === 0}
      <p class="graph-status" role="status"><Spinner size="sm" color="muted" /> {t('graph.loading')}</p>
    {:else if graph.empty}
      <div class="graph-empty" data-testid="graph-empty"><h3>{t('graph.empty.title')}</h3><p>{t(emptyText)}</p></div>
    {:else if graph.roots.length > 0}
      <div class="graph-toolbar" bind:this={toolbar}>
        <!-- Eén kopregel, in de rij zelf. De schil draagt de terugknop en zegt
             al waar je bent; een tweede grote kop erboven kostte alleen hoogte
             die de kaart beter kan gebruiken. -->
        <h2 class="graph-title">{copy['graph.title']}</h2>
        <div class="graph-search-group">
          <label class="sr-only" for="duif-graph-search">{t('graph.search')}</label>
          <Input
            id="duif-graph-search"
            type="search"
            placeholder={t('graph.search')}
            bind:value={graph.query}
            aria-describedby="duif-graph-search-scope duif-graph-search-count"
            data-testid="graph-search"
          />
          <span id="duif-graph-search-scope" class="sr-only">{copy['graph.search.scope']}</span>
          <p id="duif-graph-search-count" aria-live="polite" aria-atomic="true">
            {#if graph.searching}
              <span data-testid="graph-search-count"
                >{graph.matchesLoaded.size === 0
                  ? t('graph.search.none')
                  : t('graph.search.loaded', { count: graph.matchesLoaded.size, total: graph.nodes.size, onMap: graph.matches.size })}</span
              >
            {:else}
              <span data-testid="graph-shown">{t('graph.map.nodes', { count: graph.visibleIds.size })}</span>
            {/if}
          </p>
        </div>

        <details class="graph-menu" data-testid="graph-relations">
          <summary>{t('graph.relations')}</summary>
          <div class="graph-menu-body">
            <p>{copy['graph.relations.hint']}</p>
            <ul>
              {#each graph.relationLinks as link (link.type)}
                <li>
                  <label>
                    <input
                      type="checkbox"
                      data-relation={link.type}
                      checked={!graph.hiddenRelations.has(link.type)}
                      onchange={() => toggleRelation(link.type)}
                    />
                    <span class="graph-rule" style="background:{relationStyle(link.type).color}" aria-hidden="true"></span>
                    {t(`graph.type.${link.from}`)} <span aria-hidden="true">→</span> {t(`graph.type.${link.to}`)}
                  </label>
                </li>
              {/each}
            </ul>
            <button type="button" class="duif-text-action" onclick={() => (graph.hiddenRelations = new Set())}>{t('graph.relations.all')}</button>
          </div>
        </details>

        <details class="graph-menu" data-testid="graph-legend">
          <summary>{t('graph.legend')}</summary>
          <div class="graph-menu-body graph-legend-body">
            <p>{copy['graph.intro']}</p>
            <p>{copy['graph.scope']}</p>
            <p>{copy['graph.legend.lines']}</p>
            <h4>{t('graph.legend.nodes')}</h4>
            <dl>
              {#each NODE_TYPES as type (type)}<div><dt>{t(`graph.type.${type}`)}</dt><dd>{copy[`graph.kind.${type}`]}</dd></div>{/each}
            </dl>
          </div>
        </details>

        <button type="button" class="graph-home" data-testid="graph-home" title={t('graph.home.hint')} onclick={() => void goHome()}>
          {@render house()}{t('graph.home')}
        </button>

        <div class="graph-view-choice" role="group" aria-label={t('graph.view.label')}>
          <button type="button" aria-pressed={!showList} data-testid="graph-view-map" onclick={() => (showList = false)}>{t('graph.view.map.short')}</button>
          <button type="button" aria-pressed={showList} data-testid="graph-view-list" onclick={() => (showList = true)}>{t('graph.view.list.short')}</button>
        </div>
      </div>

      {#if graph.trail.length > 0}
        <nav class="graph-path" aria-label={t('graph.path')} data-testid="graph-path">
          <ol>
            {#each graph.trail as node, index (node.id)}
              <li>
                {#if index > 0}<span aria-hidden="true">/</span>{/if}
                <button type="button" aria-current={node.id === graph.selected ? 'location' : undefined} onclick={() => graph.select(node.id)}>{node.label}</button>
              </li>
            {/each}
          </ol>
        </nav>
      {/if}

      <div class="graph-workspace" class:list-first={showList} style:--graph-tree-width="{treeWidth}px" bind:this={workspace}>
        <section class="graph-outline" id="duif-graph-outline" aria-label={t('graph.tree')}>
          <!-- In de kaartweergave staat Beginstand in de knoppenbalk, een
               handbreedte hiervandaan; hem hier herhalen is twee keer dezelfde
               knop op één scherm. In de lijstweergave neemt de boom de plek van
               de kaart in en staat de knoppenbalk ver weg — daar hoort hij wel. -->
          {#if showList}
            <div class="graph-outline-head">
              <button
                type="button"
                class="graph-home wide"
                data-testid="graph-home-tree"
                title={t('graph.home.hint')}
                onclick={() => void goHome()}
              >
                {@render house()}{t('graph.home')}
              </button>
            </div>
          {/if}
          <GraphTree bind:this={tree} store={graph} {locale} {palette} onEscape={closeDetail} />
        </section>

        <!-- De scheiding is zelf het bedieningselement (APG, window splitter):
             te verslepen met de muis, te verzetten met de pijltjes, en met Enter
             terug op de standaardmaat. Een separator mét aria-valuenow is in
             ARIA een widget en hoort de aandacht te kunnen krijgen; de regel
             hieronder kent alleen de vaste lijst met rollen en ziet dat verschil
             niet. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
        <div
          class="graph-split"
          class:dragging={resizing}
          role="separator"
          tabindex="0"
          aria-orientation="vertical"
          aria-label={t('graph.tree.resize')}
          title={t('graph.tree.resize.hint')}
          aria-controls="duif-graph-outline"
          aria-valuenow={treeWidth}
          aria-valuemin={TREE_MIN}
          aria-valuemax={treeMax}
          aria-valuetext={t('graph.tree.resize.value', { width: treeWidth })}
          data-testid="graph-split"
          onpointerdown={onSplitDown}
          onpointermove={onSplitMove}
          onpointerup={onSplitUp}
          onpointercancel={onSplitUp}
          ondblclick={() => setTreeWidth(TREE_DEFAULT)}
          onkeydown={onSplitKey}
        ></div>

        <section class="graph-stage" aria-label={t('graph.map.title')} hidden={showList}>
          <div class="graph-map-controls" bind:this={mapControls}>
            <Button variant="ghost" size="sm" aria-label={t('graph.zoom.out')} onclick={() => controls?.zoomOut()}>−</Button>
            <Button variant="ghost" size="sm" aria-label={t('graph.zoom.in')} onclick={() => controls?.zoomIn()}>+</Button>
            <Button variant="ghost" size="sm" onclick={() => controls?.reset()}>{t('graph.zoom.reset')}</Button>
          </div>
          <GraphMap store={graph} {locale} bind:controls active={!showList} overlays={[mapControls]} />
        </section>

        <div class="graph-reading">
          {#if detail}
            <DetailPanel bind:this={detailPanel} node={detail} store={graph} {locale} {palette} onClose={closeDetail} />
          {:else}
            <div class="graph-reading-empty">
              <h3>{copy['graph.detail.empty.title']}</h3>
              <p>{copy['graph.detail.empty.map']}</p>
            </div>
          {/if}
          {#if graph.selectedNode && detailClosed}
            <div class="graph-reopen"><button type="button" class="duif-text-action" onclick={() => void readDetail()}>{t('graph.detail.reopen')} <span aria-hidden="true">→</span></button></div>
          {/if}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .graph-view { min-height: 0; color: var(--duif-ink); }
  .graph-frame { height: 100%; min-height: 0; display: flex; flex-direction: column; }
  .graph-title { font: 500 .95rem/1.3 var(--font-serif); margin: 0; max-width: 16rem; color: var(--duif-ink); }
  h3 { font-size: .95rem; font-weight: 600; margin: 0 0 .4rem; }
  p { line-height: 1.55; color: var(--duif-dim); font-size: .85rem; margin: .3rem 0; }

  .graph-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem 1rem; border-bottom: 1px solid var(--duif-line); padding: .6rem clamp(1rem, 3vw, 2rem); flex-shrink: 0; }
  @media (max-width: 820px) { .graph-title { display: none; } }
  .graph-search-group { flex: 1; min-width: min(100%, 15rem); max-width: 32rem; display: grid; gap: .2rem; }
  .graph-search-group p { font-size: .72rem; margin: 0; }

  .graph-menu { position: relative; }
  .graph-menu summary { cursor: pointer; font-size: .8rem; font-weight: 600; padding: .4rem .7rem; border: 1px solid var(--duif-line); border-radius: .5rem; width: fit-content; }
  .graph-menu-body { position: absolute; z-index: 5; top: calc(100% + .35rem); left: 0; width: min(24rem, 80vw); max-height: 22rem; overflow: auto; padding: .8rem 1rem; background: var(--duif-paper); border: 1px solid var(--duif-line); border-radius: var(--duif-radius-card); box-shadow: 0 12px 30px rgba(0, 0, 0, .12); }
  .graph-menu-body ul { list-style: none; margin: .5rem 0; padding: 0; display: grid; gap: .3rem; }
  .graph-menu-body label { display: flex; align-items: center; gap: .5rem; font-size: .82rem; }
  .graph-rule { display: inline-block; width: 1.1rem; height: 3px; border-radius: 2px; flex-shrink: 0; }
  .graph-legend-body dl { display: grid; gap: .8rem; margin: .6rem 0 0; }
  .graph-legend-body dt { font-weight: 600; font-size: .8rem; }
  .graph-legend-body dd { margin: .2rem 0 0; font-size: .78rem; line-height: 1.5; color: var(--duif-dim); }
  .graph-legend-body h4 { font-size: .8rem; font-weight: 600; margin: .9rem 0 0; }

  .graph-home { display: inline-flex; align-items: center; justify-content: center; gap: .4rem; font-size: .8rem; font-weight: 600; padding: .4rem .8rem; border: 1px solid var(--duif-line); border-radius: .5rem; }
  .graph-home:hover { background: var(--duif-ground); }
  /* In de lijstweergave neemt de omtrek de plek van de kaart in; een knop die
     dan over anderhalve meter scherm uitrekt is geen knop meer. */
  .graph-home.wide { width: 100%; max-width: 22rem; }

  .graph-view-choice { display: flex; gap: .25rem; padding: .2rem; border-radius: .5rem; border: 1px solid var(--duif-line); }
  .graph-view-choice button { padding: .4rem .75rem; font-size: .8rem; border-radius: .4rem; }
  .graph-view-choice button[aria-pressed='true'] { background: var(--duif-ink); color: var(--duif-paper); }

  .graph-path { padding: .4rem clamp(1rem, 3vw, 2rem); flex-shrink: 0; border-bottom: 1px solid var(--duif-line); }
  .graph-path ol { display: flex; flex-wrap: wrap; gap: .4rem; list-style: none; margin: 0; padding: 0; }
  .graph-path li { display: flex; gap: .4rem; align-items: baseline; font-size: .75rem; min-width: 0; }
  /* Eén regel per stap: het laatste kruimeltje is de tekst van een bevinding,
     en die vulde anders twee regels boven de kaart. Waar het pad naartoe leidt
     staat voluit in het leespaneel. */
  .graph-path button { text-decoration: underline; text-underline-offset: .2em; text-align: left; max-width: 22rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .graph-path button[aria-current] { font-weight: 600; text-decoration: none; }

  /* Vier kolommen: de omtrek links (niet meer smal en uit de weg, maar een
     volwaardige inhoudsopgave), de versleepbare scheiding, de kaart in het
     midden als hoofdzaak, en rechts wat je leest. De schakelaar in de
     knoppenbalk zet de omtrek op de plek van de kaart voor wie liever leest of
     alleen een toetsenbord heeft; de scheiding heeft dan niets te scheiden en
     verdwijnt. */
  .graph-workspace { flex: 1; min-height: 0; display: grid; grid-template-columns: var(--graph-tree-width, 20rem) .6rem minmax(0, 1fr) minmax(19rem, 26rem); }
  .graph-workspace.list-first { grid-template-columns: minmax(0, 1fr) 0 0 minmax(19rem, 26rem); }
  .graph-workspace.list-first .graph-split { display: none; }
  .graph-outline { min-width: 0; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
  .graph-outline-head { flex-shrink: 0; padding: .55rem .7rem; border-bottom: 1px solid var(--duif-line); }

  .graph-split { position: relative; z-index: 3; cursor: col-resize; touch-action: none; }
  .graph-split::before { content: ''; position: absolute; top: 0; bottom: 0; left: 50%; width: 1px; transform: translateX(-50%); background: var(--duif-line); }
  /* De kolom is .6rem breed; dat is een streep, geen greep. Dit onzichtbare
     vlak steekt aan beide kanten uit tot ruim boven de vierentwintig pixels die
     een muis of een vinger nodig heeft, zonder dat de streep zelf dikker wordt.
     Het hoort bij de scheiding zelf, dus een klik erop is een klik erop. */
  /* Symmetrische greep van ~26 px: even ver de boom als de kaart in, zodat de
     linkerrand van het doek klikbaar blijft. */
  .graph-split::after { content: ''; position: absolute; top: 0; bottom: 0; left: -8px; right: -8px; }
  .graph-split:hover::before, .graph-split:focus-visible::before, .graph-split.dragging::before { width: 3px; background: var(--duif-ink); }
  .graph-split:focus-visible { outline: 2px solid var(--duif-focus-ring); outline-offset: -2px; }

  .graph-reading { min-width: 0; min-height: 0; overflow-y: auto; background: var(--duif-paper); border-left: 1px solid var(--duif-line); display: flex; flex-direction: column; }
  .graph-reading-empty { padding: 1.6rem 1.2rem; }
  .graph-reopen { padding: 0 1.2rem 1.2rem; }
  .graph-stage { position: relative; min-width: 0; min-height: 0; display: flex; flex-direction: column; padding: .5rem; }
  .graph-stage[hidden] { display: none; }
  .graph-map-controls { position: absolute; z-index: 2; top: .8rem; right: .8rem; display: flex; align-items: center; gap: .2rem; background: var(--duif-paper); border: 1px solid var(--duif-line); border-radius: .5rem; padding: .1rem; }

  .graph-empty, .graph-status { margin: auto; padding: 2rem; max-width: 40rem; }
  .graph-status { display: flex; gap: .5rem; }
  .graph-retry { padding: .5rem 1rem; }

  @media (max-width: 1100px) {
    .graph-workspace, .graph-workspace.list-first { display: flex; flex-direction: column; }
    /* Onder elkaar valt er niets meer te verslepen: de kolommen zijn dan geen
       kolommen. */
    .graph-split { display: none; }
    .graph-outline { border-bottom: 1px solid var(--duif-line); max-height: 34%; }
    .graph-reading { border-left: 0; border-top: 1px solid var(--duif-line); max-height: 45%; }
    .graph-stage { min-height: 20rem; }
  }
</style>
