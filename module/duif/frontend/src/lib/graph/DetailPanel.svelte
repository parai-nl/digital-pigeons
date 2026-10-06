<script lang="ts">
  // Het leespaneel: wat de gekozen knoop is, wat er staat, en waar hij aan
  // vastzit. De verbindingen staan per relatiesoort onder elkaar, met de kleur
  // van de lijn op de kaart ervoor — zo is het paneel de leesbare kant van
  // dezelfde tekening in plaats van een tweede verhaal.
  //
  // Elk veld draagt een mensennaam uit één woordenboek (readingCopy.ts). Een
  // ruwe kolomnaam als `kernstandpunt` of `interview_id` komt hier nooit op het
  // scherm, en een interne id al helemaal niet.
  import { bibliographyNotice } from '../bibliography'
  import type { UiLocale } from '../i18n.svelte'
  import type { Palette } from './colors'
  import { groupedConnections } from './connections'
  import type { GraphStore } from './graphStore.svelte'
  import { isReadableValue } from './readable'
  import NodeGlyph from './NodeGlyph.svelte'
  import { graphTranslator, readingEn, readingNl } from './readingCopy'
  import { nodeShape, relationStyle } from './visuals'
  import type { GraphNode } from './types'

  let {
    node,
    store,
    locale,
    palette = null,
    onClose,
  }: {
    node: GraphNode
    store: GraphStore
    locale: UiLocale
    palette?: Palette | null
    onClose: () => void
  } = $props()

  const copy = $derived(locale.lang === 'nl' ? readingNl : readingEn)
  const t = $derived(graphTranslator(locale))

  // De velden die een lezer iets zeggen, in leesvolgorde. `interview_id` en de
  // andere interne sleutels staan er bewust niet bij: een opaque id in de
  // kennisbank is geen inhoud, en het scherm hoeft niet te laten zien hoe de
  // kolommen heten. De doi krijgt hieronder zijn eigen regel, als tekst.
  //
  // `domain` staat er ook niet bij, en dat is een aparte keuze: de waarde is een
  // sleutel uit de importpijplijn (`wonen_en_leefomgeving`), en het echte
  // domeinlabel staat al in het kruimelpad boven de kaart. Twee keer hetzelfde
  // zeggen, waarvan één keer in databasetaal, is geen extra informatie.
  const FIELDS = [
    'beschrijving',
    'speaker',
    'thema_titel',
    'scope',
    'citation',
    'citationMissing',
  ] as const

  // De volledige tekst is de inhoud zelf en staat bovenaan als passage, niet
  // als veld in de lijst hieronder — zie `passage`. Wat overblijft moet
  // leesbaar zijn: een opsommingswaarde als `algemeen` of `respondent` zegt een
  // lezer niets, en een nette kop erboven maakt daar geen zin van.
  const fields = $derived(
    FIELDS.filter((key) => {
      const value = node.metadata[key]
      return value !== undefined && isReadableValue(value)
    }).map((key) => ({
      key,
      label: t(`graph.meta.${key}`),
      value: node.metadata[key],
    })),
  )

  // Het label van een bevinding en een citaat is een afkapping: de graaf-API
  // kort de tekst in tot honderdveertig tekens met een beletselteken, zodat er
  // op de kaart iets naast de knoop past. In het leespaneel is dat geen titel
  // maar een halve zin — dus staat hier de volledige passage, precies zoals hij
  // is opgeslagen, en verschijnt het afgekapte label nergens meer.
  //
  // De ruwe waarde gaat ongewijzigd het scherm op: geen trim, geen inkorting,
  // geen opmaak — een citaat is een overname. Alleen de regelval is van de
  // opmaak, niet van de tekst (zie de stijl van .detail-passage).
  const raw = $derived(node.metadata.text ?? '')
  const passage = $derived((node.type === 'destillaat' || node.type === 'quote') && raw.trim() !== '' ? raw : '')
  // De kop is de naam van het onderdeel. Draagt het geen naam maar een passage,
  // dan is de passage de kop — een afgekapte titel erboven zou hetzelfde twee
  // keer zeggen, één keer half.
  const heading = $derived(passage === '' ? node.label : passage)
  const glyph = $derived(store.iconOf(node.id))

  // Een doi is een adres, en de module opent geen adressen buiten haar eigen
  // API (frontend-wachter): hij staat er als tekst, nooit als link. Zo kan ook
  // een `javascript:`-waarde uit de kennisbank nooit klikbaar worden.
  const doi = $derived(node.metadata.doi ?? '')

  const bibliographyNote = $derived(node.type === 'interview' ? bibliographyNotice(node.metadata, locale.lang) : '')
  // Een bevinding en een citaat dragen hun tekst als kop. Ontbreekt die tekst,
  // dan staat er het label van de graaf-API — en dat is een afkapping op
  // honderdveertig tekens. Dan hoort er te staan dat het niet het hele verhaal
  // is; stilzwijgend een halve zin als kop tonen is de fout die deze batch juist
  // wegneemt.
  const incomplete = $derived.by((): 'graph.detail.quoteIncomplete' | 'graph.detail.textIncomplete' | '' => {
    if (passage !== '') return ''
    if (node.type === 'quote') return 'graph.detail.quoteIncomplete'
    if (node.type === 'destillaat') return 'graph.detail.textIncomplete'
    return ''
  })
  // De kanttekeningen die onder de inhoud kunnen staan. Ze bepalen mee of het
  // inhoudsblok er überhaupt hoort te zijn: een kop boven niets is een belofte
  // die niet wordt ingelost.
  // Staat hier een vertaling in plaats van het origineel, dan hoort de lezer
  // dat te weten: de samenvatting is vertaald, het citaat eronder niet.
  const translated = $derived(node.translated === true)
  const notes = $derived(bibliographyNote !== '' || incomplete !== '' || translated || node.metadata.grounding === 'rejected')
  const groups = $derived(groupedConnections(node, store.nodes, store.edges, store.hiddenRelations))
  const total = $derived(groups.reduce((sum, group) => sum + group.nodes.length, 0))
  const loading = $derived(store.loadingIds.has(node.id))
  const open = $derived(store.revealed.has(node.id))
  const canOpen = $derived(store.hasChildren(node.id))
  const family = $derived(store.view.family.get(node.id) ?? -1)
  const swatch = $derived(palette ? palette.fill(family, store.view.depth.get(node.id) ?? 0) : 'transparent')
  const ink = $derived(palette ? palette.glyph(family) : '#3a3a3a')

  let title: HTMLHeadingElement | undefined = $state()
  export function focusTitle(): void {
    title?.focus()
  }
</script>

<aside class="graph-detail" aria-labelledby="duif-graph-detail-title" data-testid="graph-detail" data-type={node.type} data-id={node.id}>
  <header>
    <div>
      <span class="detail-type"
        ><span class="detail-swatch" data-shape={nodeShape(node.type)} data-glyph={glyph} style="background:{swatch}" aria-hidden="true"
          ><NodeGlyph ports={store.ports} icon={glyph} {ink} size={14} /></span
        >{t(`graph.type.${node.type}`)}</span
      >
      <h3
        id="duif-graph-detail-title"
        tabindex="-1"
        bind:this={title}
        class:detail-passage={passage !== ''}
        class:detail-quotation={passage !== '' && node.type === 'quote'}
        data-testid="graph-detail-title">{heading}</h3
      >
    </div>
    <button
      type="button"
      class="duif-text-action"
      data-testid="detail-close"
      onclick={onClose}
      onkeydown={(event) => {
        if (event.key === 'Escape') {
          event.stopPropagation()
          onClose()
        }
      }}>{t('graph.close')}</button
    >
  </header>
  <p class="detail-explanation" data-testid="graph-item-kind">{copy[`graph.kind.${node.type}`]}</p>

  {#if fields.length > 0 || doi !== '' || passage === '' || notes}
    <section aria-labelledby="duif-graph-content-title">
      <h4 id="duif-graph-content-title">{t('graph.detail.content')}</h4>
      {#if fields.length > 0 || doi !== ''}
        <dl>
          {#each fields as field (field.key)}<div><dt>{field.label}</dt><dd>{field.value}</dd></div>{/each}
          {#if doi !== ''}<div><dt>{t('graph.meta.doi')}</dt><dd data-testid="graph-doi">{doi}</dd></div>{/if}
        </dl>
      {:else if passage === ''}
        <p>{copy['graph.detail.missing']}</p>
      {/if}
      {#if doi !== ''}<p class="detail-doi-hint">{t('graph.source.doi')}</p>{/if}
      {#if node.type === 'quote' && node.metadata.grounding === 'rejected'}
        <p class="detail-note" data-testid="graph-quote-rejected">{copy['graph.detail.quoteRejected']}</p>
      {/if}
      {#if incomplete !== ''}<p class="detail-note" data-testid="graph-text-incomplete">{copy[incomplete]}</p>{/if}
      {#if translated}<p class="detail-note" data-testid="graph-translated">{copy['graph.detail.translated']}</p>{/if}
      {#if bibliographyNote}<p class="detail-note" data-testid="graph-bibliography-note">{bibliographyNote}</p>{/if}
    </section>
  {/if}

  <section aria-labelledby="duif-graph-links-title">
    <h4 id="duif-graph-links-title">{t('graph.connections')} <span class="detail-count" data-testid="graph-connection-count">{total}</span></h4>
    <p>{copy['graph.links.scope']}</p>
    {#if groups.length > 0}
      <div class="detail-groups">
        {#each groups as group (group.key)}
          <div class="detail-group">
            <p class="detail-group-title">
              <span class="detail-rule" style="background:{relationStyle(group.relation).color}" aria-hidden="true"></span>
              {group.nodes.length > 1 ? t(`graph.types.${group.otherType}`) : t(`graph.type.${group.otherType}`)}
              <span class="detail-count">{group.nodes.length}</span>
            </p>
            <ul>
              {#each group.nodes as other (other.id)}
                <li>
                  <button
                    type="button"
                    onclick={() => store.select(other.id)}
                    onpointerenter={() => (store.hovered = other.id)}
                    onpointerleave={() => (store.hovered = null)}>{other.label}</button
                  >
                </li>
              {/each}
            </ul>
          </div>
        {/each}
      </div>
    {:else}
      <p class="detail-note" data-testid="graph-connections-empty">{copy['graph.connections.empty']}</p>
    {/if}
    <div class="detail-actions">
      {#if canOpen}
        <button
          type="button"
          class="duif-quiet-button"
          data-testid="detail-expand"
          disabled={loading}
          onclick={() => (open ? store.collapse(node.id) : void store.expand(node.id))}
          >{t(loading ? 'graph.loading' : open ? 'graph.collapse' : 'graph.expand')}</button
        >
      {/if}
      {#if store.nextOffsets.has(node.id)}
        <button type="button" class="duif-text-action" disabled={loading} onclick={() => void store.more(node.id)}
          >{t(loading ? 'graph.loading' : 'graph.more')}</button
        >
      {/if}
      {#if store.childTotals.has(node.id)}
        <span class="detail-loaded">{t('graph.loaded', { loaded: store.childIds.get(node.id)?.length ?? 0, total: store.childTotals.get(node.id) ?? 0 })}</span>
      {/if}
    </div>
  </section>
</aside>

<style>
  .graph-detail { height: 100%; min-height: 0; overflow-y: auto; padding: clamp(1rem, 2vw, 1.4rem); overflow-wrap: anywhere; }
  header { display: flex; align-items: start; gap: 1rem; justify-content: space-between; }
  header > div { min-width: 0; }
  header button { font-size: .75rem; flex-shrink: 0; }
  .detail-type { display: flex; align-items: center; gap: .45rem; font-size: .7rem; font-weight: 650; text-transform: uppercase; letter-spacing: .06em; color: var(--duif-dim); }
  /* Hetzelfde vlaggetje als op de kaart en in de boom: de vorm van de soort,
     de tint van de familie, het teken van het onderwerp. */
  .detail-swatch { width: 1.5rem; height: 1.5rem; display: grid; place-items: center; border: 1px solid var(--duif-line); flex-shrink: 0; }
  .detail-swatch[data-shape='disc'] { border-radius: 50%; }
  .detail-swatch[data-shape='tile'] { border-radius: .35rem; }
  .detail-swatch[data-shape='card'] { border-radius: .2rem; width: 1.25rem; }
  h3 { font: 500 1.2rem/1.45 var(--font-serif); margin: .55rem 0 0; }
  h3:focus { outline: 2px solid var(--duif-focus-ring); outline-offset: 4px; }
  /* Een bevinding en een citaat dragen geen titel maar hun tekst. Die staat
     hier voluit, in een maat die leest als een passage in plaats van als een
     kop. Bewust geen `pre-wrap`: de regelval in een uit een pdf gehaalde
     passage is die van de kolom waar hij in stond, niet van de zin, en over de
     volle breedte hoort hij gewoon door te lopen. De tekst zelf blijft woord
     voor woord wat er staat. */
  h3.detail-passage { font-size: 1.02rem; line-height: 1.65; }
  h3.detail-quotation { font-style: italic; }
  p { color: var(--duif-dim); font-size: .8rem; line-height: 1.6; margin: .5rem 0; }
  .detail-explanation { border-left: 2px solid var(--duif-line); padding-left: .75rem; margin: 1rem 0 1.4rem; }
  section { margin-top: 1.3rem; }
  h4 { display: flex; align-items: baseline; gap: .5rem; font-size: .85rem; font-weight: 600; margin-bottom: .6rem; }
  .detail-count { font-size: .7rem; font-weight: 500; color: var(--duif-dim); }
  dl { display: grid; gap: .9rem; }
  dt { font-size: .72rem; font-weight: 600; color: var(--duif-dim); }
  dd { font-size: .95rem; white-space: pre-wrap; line-height: 1.7; margin: .25rem 0 0; }
  .detail-note { padding: .7rem; border: 1px dashed var(--duif-line); border-radius: .5rem; }
  .detail-doi-hint { font-size: .72rem; }
  .detail-groups { display: grid; gap: 1.1rem; margin-top: .8rem; }
  .detail-group-title { display: flex; align-items: center; gap: .45rem; margin: 0 0 .35rem; font-size: .72rem; font-weight: 600; text-transform: uppercase; letter-spacing: .05em; color: var(--duif-ink); }
  .detail-rule { display: inline-block; width: 1.1rem; height: 3px; border-radius: 2px; flex-shrink: 0; }
  .detail-group ul { list-style: none; margin: 0; padding: 0; display: grid; gap: .15rem; }
  .detail-group li button { display: block; width: 100%; text-align: left; font-size: .85rem; line-height: 1.5; padding: .35rem .45rem; border-radius: .4rem; }
  .detail-group li button:hover { background: var(--duif-hover, rgba(0, 0, 0, .04)); }
  .detail-actions { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; margin-top: 1rem; }
  .detail-loaded { font-size: .72rem; color: var(--duif-dim); }
  button:disabled { cursor: wait; }
  @media (max-width: 760px) { .graph-detail { height: auto; } }
</style>
