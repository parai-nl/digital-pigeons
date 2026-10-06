<script lang="ts">
  import { graphTranslator } from './readingCopy'
  // De boom naast de plattegrond. Hij draait op dezelfde acties als het canvas
  // (uitklappen, kiezen, meer laden), maar is met het toetsenbord te bedienen
  // en in jsdom te toetsen — het canvas is dat allebei niet. Hij volgt het
  // ARIA-boompatroon: één tabstop (roving tabindex), pijltjes om te lopen,
  // aria-expanded alleen op ouders, en aria-setsize dat het servertotaal noemt
  // ook als er nog maar een pagina kinderen binnen is.
  import { tick, untrack } from 'svelte'
  import Spinner from '@parai/ui/components/Spinner.svelte'
  import type { UiLocale } from '../i18n.svelte'
  import type { Palette } from './colors'
  import type { GraphStore } from './graphStore.svelte'
  import NodeGlyph from './NodeGlyph.svelte'
  import { nodeShape } from './visuals'
  import { nestRows, type TreeBranch, type TreeRow } from './visibility'

  const {
    store,
    locale,
    palette = null,
    onEscape,
  }: { store: GraphStore; locale: UiLocale; palette?: Palette | null; onEscape?: () => void } = $props()

  const t = $derived(graphTranslator(locale))
  const branches = $derived(nestRows(store.rows))
  let treeEl: HTMLElement | undefined = $state()
  let activeKey = $state('')

  // De "meer laden"-knop is geen knoop, maar wél een plek waar de aandacht kan
  // staan. Hij krijgt daarom een eigen sleutel onder zijn ouder: één tabstop in
  // de hele boom (APG), en toch met de pijltjes bereikbaar. Het achtervoegsel
  // kan nooit een id zijn — die zijn altijd `tabel:uuid`.
  const MORE = '…meer'

  // NavItem is de leesvolgorde van de boom: de rijen zoals ze op het scherm
  // onder elkaar staan, met de "meer laden"-regel op zijn plek ertussen.
  type NavItem = { key: string; kind: 'row'; row: TreeRow } | { key: string; kind: 'more'; parent: TreeRow }

  // Leeg = deze tak heeft niets meer te laden. Een lege tekst in plaats van
  // null houdt het sjabloon getypeerd zonder narrowing-kunstjes.
  function moreKey(row: TreeRow): string {
    return row.expanded && store.nextOffsets.has(row.id) ? row.key + '/' + MORE : ''
  }

  function flatten(list: readonly TreeBranch[], out: NavItem[]): void {
    for (const branch of list) {
      out.push({ key: branch.row.key, kind: 'row', row: branch.row })
      flatten(branch.children, out)
      const more = moreKey(branch.row)
      if (more !== '') out.push({ key: more, kind: 'more', parent: branch.row })
    }
  }

  const nav = $derived.by((): NavItem[] => {
    const out: NavItem[] = []
    flatten(branches, out)
    return out
  })

  // De actieve plek is de enige tabstop. Verdwijnt hij (een tak die dichtklapt),
  // dan valt de tabstop terug op de eerste rij in plaats van weg te zijn.
  const active = $derived(nav.find((item) => item.key === activeKey) ?? nav[0])

  // Een selectie die van buiten komt (een bronkaart die "open in de graaf"
  // doet) verplaatst de tabstop mee, zodat het toetsenbord daar verdergaat.
  // De aandacht zelf verhuist alleen als de boom hem al had of als er echt van
  // buiten om gevraagd is: een canvasklik midden in een sleep hoort de focus
  // niet uit de handen van de gebruiker te trekken.
  let lastRequest = 0
  $effect(() => {
    const selected = store.selected
    const request = store.focusRequest
    untrack(() => {
      const external = request !== lastRequest
      lastRequest = request
      if (!selected) return
      const current = nav.find((item) => item.key === activeKey)
      // Staat de tabstop al op déze knoop, dan blijft hij staan: een knoop die
      // onder twee ouders hangt zou de tabstop anders naar de eerste padkopie
      // laten springen terwijl de gebruiker in de tweede staat.
      if (!(current?.kind === 'row' && current.row.id === selected)) {
        const row = store.rows.find((r) => r.id === selected)
        if (!row) return
        activeKey = row.key
      }
      if (external || treeEl?.contains(document.activeElement)) void focusActive()
    })
  })

  // De hardop-melding: hoeveel kinderen er van een tak geladen zijn. Alleen
  // voor de rij waar de aandacht is; anders praat de boom door elkaar.
  const status = $derived.by(() => {
    const item = active
    if (!item) return ''
    const row = item.kind === 'row' ? item.row : item.parent
    if (row.loading) return t('graph.loading')
    if (!row.expanded) return ''
    const loaded = store.childIds.get(row.id)?.length ?? 0
    const total = store.childTotals.get(row.id) ?? loaded
    return total > loaded ? t('graph.loaded', { loaded, total }) : ''
  })

  // focusActive loopt de rijen langs in plaats van er een attribuutselector op
  // los te laten: een sleutel is samengesteld uit server-id's, en die horen
  // nooit ongeëscaped in een selector terecht te komen.
  export async function focusActive(): Promise<void> {
    await tick()
    const key = active?.key
    for (const el of treeEl?.querySelectorAll<HTMLElement>('[data-key]') ?? []) {
      if (el.dataset.key === key) {
        el.focus()
        return
      }
    }
  }

  // toTop zet de boom terug in zijn beginstand: bovenaan, met de tabstop weer op
  // de eerste rij. De aandacht wordt niet verplaatst — wie de knop in de
  // knoppenbalk indrukt hoort zijn cursor niet kwijt te raken.
  export function toTop(): void {
    activeKey = ''
    if (treeEl) treeEl.scrollTop = 0
  }

  function setActive(key: string | undefined): void {
    if (!key) return
    activeKey = key
    void focusActive()
  }

  function step(delta: number): void {
    // Vanaf de plek die de tabstop draagt, en dat is `active`: activeKey kan nog
    // leeg zijn (er is alleen gefocust, nog niet gekozen) en valt dan terug op
    // de eerste rij — anders zou de eerste pijl-omlaag niets doen.
    const at = nav.findIndex((item) => item.key === active?.key)
    const next = at < 0 ? 0 : Math.min(nav.length - 1, Math.max(0, at + delta))
    setActive(nav[next]?.key)
  }

  function firstChildKey(row: TreeRow): string | undefined {
    return store.rows.find((r) => r.depth === row.depth + 1 && r.key.startsWith(row.key + '/'))?.key
  }

  function parentKey(key: string): string | undefined {
    const cut = key.lastIndexOf('/')
    return cut < 0 ? undefined : key.slice(0, cut)
  }

  function onKeyDown(event: KeyboardEvent, item: NavItem): void {
    const row = item.kind === 'row' ? item.row : null
    switch (event.key) {
      case 'ArrowDown':
        step(1)
        break
      case 'ArrowUp':
        step(-1)
        break
      case 'ArrowRight':
        if (!row?.expandable) return
        if (row.expanded) setActive(firstChildKey(row))
        // Na het laden staat het eerste kind klaar; de aandacht blijft op de
        // ouder (APG: pas de tweede pijl-rechts stapt naar binnen).
        else void store.expand(row.id)
        break
      case 'ArrowLeft':
        if (row?.expanded) store.collapse(row.id)
        else setActive(parentKey(item.key))
        break
      case 'Home':
        setActive(nav[0]?.key)
        break
      case 'End':
        setActive(nav.at(-1)?.key)
        break
      case ' ':
      case 'Enter':
        // Op de "meer laden"-regel staat een echte knop; die doet Enter zelf.
        if (!row) return
        store.select(row.id)
        break
      case 'Escape':
        // Escape sluit het detailpaneel, maar laat de selectie staan: de halo
        // op het canvas en de plek in de boom blijven waar ze waren.
        onEscape?.()
        break
      default:
        return
    }
    event.preventDefault()
    event.stopPropagation()
  }

  // Klikken doet allebei wat je van een boom verwacht: het kiest de knoop, en
  // bij een ouder klapt hij tegelijk open of dicht.
  function onActivate(row: TreeRow): void {
    activeKey = row.key
    store.select(row.id)
    if (row.expandable) void store.toggle(row.id)
  }

  // Het teken naast een rij is precies het teken op de kaart: dezelfde vorm
  // (wát het is), dezelfde familietint (bij welk thema het hoort) en hetzelfde
  // icoontje (waar het over gaat). Zonder palet — de gastheer heeft zijn tokens
  // nog niet gegeven — blijft het een neutraal vlakje in plaats van een gat.
  function markFill(row: TreeRow): string {
    return palette ? palette.fill(row.family, row.depth) : 'var(--duif-ground)'
  }

  // Dezelfde inkt als op de kaart: de donkere variant van de familietint.
  // Elke gebruikte combinatie kost één getekend doekje, en die worden gedeeld —
  // een boom laat er hooguit een handvol families tegelijk zien.
  function markInk(row: TreeRow): string {
    return palette ? palette.glyph(row.family) : '#3a3a3a'
  }

  function loadedText(row: TreeRow): string {
    const loaded = store.childIds.get(row.id)?.length ?? 0
    const total = store.childTotals.get(row.id) ?? loaded
    return t('graph.truncated', { loaded, total })
  }
</script>

{#snippet item(branch: TreeBranch)}
  {@const row = branch.row}
  {@const more = moreKey(row)}
  <li
    role="treeitem"
    tabindex={active?.key === row.key ? 0 : -1}
    aria-level={row.depth + 1}
    aria-posinset={row.index + 1}
    aria-setsize={row.setsize}
    aria-selected={store.selected === row.id}
    aria-expanded={row.expandable ? row.expanded : undefined}
    aria-busy={row.loading || undefined}
    data-key={row.key}
    data-id={row.id}
    data-type={row.type}
    data-testid="tree-item"
    class="graph-tree-item"
    aria-label={t(`graph.type.${row.type}`) + ": " + row.label}
    onclick={(event) => {
      event.stopPropagation()
      onActivate(row)
    }}
    onkeydown={(event) => onKeyDown(event, { key: row.key, kind: 'row', row })}
  >
    <span
      class="graph-tree-row"
      class:selected={store.selected === row.id}
      class:matched={store.searching && store.matches.has(row.id)}
      style:padding-left="{Math.min(row.depth, 4) * 12 + 8}px"
    >
      <span class="tree-arrow" aria-hidden="true">{#if !row.expandable}·{:else if row.expanded}▾{:else}▸{/if}</span>
      <span
        class="tree-mark"
        data-shape={nodeShape(row.type)}
        data-glyph={store.iconOf(row.id)}
        style="background:{markFill(row)}"
        aria-hidden="true"
      >
        <NodeGlyph ports={store.ports} icon={store.iconOf(row.id)} ink={markInk(row)} size={13} />
      </span>
      <span class="tree-copy"><span class="tree-type">{t(`graph.type.${row.type}`)}</span><span class="tree-label">{row.label}</span></span>
      {#if store.searching && store.matches.has(row.id)}<span class="tree-match">{t('graph.search.match')}</span>{/if}
      {#if row.loading}<Spinner size="sm" color="muted" />{/if}
    </span>

    {#if branch.children.length > 0 || more !== ''}
      <ul role="group">
        {#each branch.children as child (child.row.key)}
          {@render item(child)}
        {/each}
        {#if more !== ''}
          <!-- De knop draagt de roving tabindex zelf: een tweede tabstop in de
               boom zou het patroon breken (APG: één tabstop), maar met de
               pijltjes moet hij wél te bereiken zijn. -->
          <li role="none" style:padding-left="{(row.depth + 1) * 12 + 8}px" class="px-2 py-1">
            <button
              type="button"
              class="text-xs text-secondary underline underline-offset-2"
              tabindex={active?.key === more ? 0 : -1}
              data-key={more}
              data-testid="load-more"
              data-id={row.id}
              onclick={(event) => {
                event.stopPropagation()
                activeKey = more
                void store.more(row.id)
              }}
              onkeydown={(event) => onKeyDown(event, { key: more, kind: 'more', parent: row })}
            >
              {t('graph.more')}
            </button>
            <span class="ml-2 text-xs text-muted-foreground">{loadedText(row)}</span>
          </li>
        {/if}
      </ul>
    {/if}
  </li>
{/snippet}

<div class="tree-wrap flex h-full min-h-0 flex-col">
  <!-- De hint staat bóven de boom en nergens anders: hij vertelt wat je met
       het toetsenbord kunt doen, en dat lees je voordat je begint, niet nadat je
       eronder uitgekomen bent. -->
  <p id="duif-graph-tree-hint" class="tree-hint">{t('graph.tree.hint')}</p>
  <ul
    bind:this={treeEl}
    role="tree"
    aria-label={t('graph.tree')}
    aria-describedby="duif-graph-tree-hint"
    aria-busy={store.phase === 'loading' || undefined}
    class="min-h-0 flex-1 overflow-y-auto py-2"
    data-testid="graph-tree"
  >
    {#each branches as branch (branch.row.key)}
      {@render item(branch)}
    {/each}
  </ul>
  <p class="sr-only" aria-live="polite" data-testid="tree-status">{status}</p>
</div>

<style>
  .tree-wrap { flex: 1; }
  .graph-tree-item { outline: none; }
  .graph-tree-item:focus-visible > .graph-tree-row { outline: 2px solid var(--duif-focus-ring); outline-offset: -2px; }
  .graph-tree-row { display: flex; align-items: center; gap: .55rem; padding: .5rem .5rem; margin: .1rem .4rem; border-radius: .5rem; cursor: pointer; border-left: 3px solid transparent; }
  .graph-tree-row:hover { background: var(--duif-ground); }
  .graph-tree-row.selected { background: var(--duif-ground); border-left-color: var(--duif-ink); }
  .tree-arrow { width: .75rem; flex-shrink: 0; }
  /* Hetzelfde vormenalfabet als op de kaart: een domein is een tegel, een thema
     en een subthema een schijf, een bron een kaartje.
     Wie de kaart even bekeken heeft, herkent de boom zonder legenda. */
  .tree-mark { flex-shrink: 0; width: 1.45rem; height: 1.45rem; display: grid; place-items: center; border: 1px solid var(--duif-line); }
  .tree-mark[data-shape='disc'] { border-radius: 50%; }
  .tree-mark[data-shape='tile'] { border-radius: .35rem; }
  .tree-mark[data-shape='card'] { border-radius: .2rem; width: 1.2rem; }
  .tree-copy { min-width: 0; display: grid; gap: .1rem; }
  .tree-type { font-size: .68rem; color: var(--duif-dim); font-weight: 650; line-height: 1.4; text-transform: uppercase; letter-spacing: .05em; }
  /* Drie regels is genoeg om een bevinding te herkennen; de hele passage staat
     in het leespaneel zodra je hem kiest. Zonder deze grens vulde één rij een
     halve kolom. */
  .tree-label { font-size: .92rem; line-height: 1.45; overflow-wrap: anywhere; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; line-clamp: 3; overflow: hidden; }
  .tree-match { font-size: .65rem; margin-left: auto; font-weight: 650; border: 1px solid var(--duif-line); border-radius: .3rem; padding: .15rem .3rem; }
  .matched .tree-label { font-weight: 650; text-decoration: underline; text-underline-offset: .15em; }
  .tree-hint { font-size: .72rem; line-height: 1.5; color: var(--duif-dim); border-bottom: 1px solid var(--duif-line); padding: .55rem 1rem .6rem; margin: 0; }
</style>
