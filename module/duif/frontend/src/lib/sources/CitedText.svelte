<script lang="ts">
  import { parseCitations } from '@parai/ui/utils/citations'
  import { inlineRuns } from '../inline'
  import { useServices } from '../services.svelte'
  import type { Source } from '../sources'

  // Eén weergave van een gefundeerd antwoord, voor de chat én de vergadering:
  // de lopende tekst met lichte opmaak, de verwijzingen als kleine klikbare
  // markeringen, en de typcursor zolang er nog tekst binnenkomt. De bronnenlade
  // zelf is al gedeeld (lib/sources); dit is het stuk dat ernaartoe wijst.
  //
  // De component houdt geen halve citatie achter: wie streamt haalt zijn tekst
  // eerst door holdPartialCitation (lib/sources.ts), want in de chat gaat die
  // tekst ook nog door de bubbel heen.
  let {
    text,
    sources,
    streaming = false,
    // owner is de sleutel waarmee de lade weet wélk antwoord haar opende; zo
    // licht alleen de verwijzing in dít antwoord op.
    owner,
    open,
    class: klass = '',
    testid,
  }: {
    text: string
    sources: Source[]
    streaming?: boolean
    owner: string
    open: (number: number) => void
    class?: string
    testid?: string
  } = $props()

  const { locale, sources: panel } = useServices()

  // Een leesteken direct ná een markering hoort bij de zin ervoor, niet bij de
  // volgende regel. De browser ziet tussen de markering en dat leesteken een
  // afbreekkans (twee losse inline-vakken), dus zakte een punt of komma
  // regelmatig alleen naar beneden. Daarom staan de markering(en) en het
  // leesteken dat erop volgt samen in één wikkel die niet afbreekt.
  const TRAILING = /^[.,;:!?)\]}»”’…"']+/
  type Piece = { kind: 'text'; value: string } | { kind: 'cites'; marks: number[]; tail: string }

  function pieces(text: string, sources: Source[]): Piece[] {
    const segments = parseCitations(text, sources)
    const out: Piece[] = []
    for (let i = 0; i < segments.length; i++) {
      const segment = segments[i]
      if (segment.type === 'text') {
        if (segment.value) out.push({ kind: 'text', value: segment.value })
        continue
      }
      // "[1,2]" levert twee markeringen achter elkaar: die horen in dezelfde
      // wikkel, anders breekt het nog steeds tussen de twee.
      const marks: number[] = [segment.source.number]
      let next = segments[i + 1]
      while (next?.type === 'citation') {
        marks.push(next.source.number)
        next = segments[++i + 1]
      }
      let tail = ''
      if (next?.type === 'text') {
        const match = TRAILING.exec(next.value)
        if (match) {
          tail = match[0]
          segments[i + 1] = { type: 'text', value: next.value.slice(tail.length) }
        }
      }
      out.push({ kind: 'cites', marks, tail })
    }
    return out
  }

  const parts = $derived(pieces(text, sources))
</script>

<span class={klass} data-testid={testid}
  >{#each parts as part, i (i)}{#if part.kind === 'text'}{#each inlineRuns(part.value) as run, j (j)}{#if run.kind === 'strong'}<strong>{run.value}</strong>{:else if run.kind === 'em'}<em>{run.value}</em>{:else}{run.value}{/if}{/each}{:else}<span class="duif-cite-hold" data-testid="cite-hold">{#each part.marks as number, k (k)}<sup class="duif-cite-mark"><button
            type="button"
            class="duif-cite"
            data-source-cite
            aria-haspopup="dialog"
            aria-expanded={panel.showsSource(owner, number)}
            aria-label={locale.t('chat.citation', { number })}
            onclick={() => open(number)}>{number}</button
          ></sup>{/each}{part.tail}</span>{/if}{/each}{#if streaming}<span class="duif-cursor" aria-hidden="true">▍</span>{/if}</span
>

<style>
  /* De wikkel houdt de markering en het leesteken erna op dezelfde regel. Zij
     gedraagt zich verder als gewone tekst: geen eigen vak, geen eigen ruimte. */
  .duif-cite-hold {
    white-space: nowrap;
  }

  /* De verwijzing hoort bij het woord ervoor en blijft dus een superscript,
     maar wel een zichtbare markering: de accentkleur van de duif op een licht
     vlak. Een <button> is en blijft een inline-block, dus zijn verticale
     padding telt wél mee; het merkje is met .82em plus die padding ruim 20 px
     hoog en past daarmee binnen de regel van 30 px, zodat de leesregel er
     aantoonbaar niet van oprekt (gemeten: gelijke alineahoogte met en zonder
     merktekens). */
  .duif-cite-mark {
    /* <sup> verkleint en verhoogt van zichzelf; allebei teruggedraaid, anders
       is de markering weer het minuscule cijfertje van hiervoor. De nulregel
       houdt de sup buiten de regelhoogte. */
    font-size: inherit;
    vertical-align: baseline;
    line-height: 0;
  }

  .duif-cite {
    display: inline-block;
    font-family: var(--font-sans);
    font-size: 0.82em;
    font-weight: 700;
    /* Eigen regelhoogte: de nulregel van de sup zou het vlak van de markering
       platdrukken. 1 blijft ruim onder de 1,78 van de zin. */
    line-height: 1;
    font-variant-numeric: tabular-nums;
    color: var(--answer-accent, var(--duif-ink));
    /* Het vlak is papier met een haarlijn in de accentkleur, en niet meer een
       20%-tint van datzelfde accent. Een accent op een tint van
       zichzelf houdt per definitie weinig contrast over: op het bubbelvlak
       haalde de markering 4,24:1 bij de Kennispartner en rond 3,4:1 bij de
       Stakeholder en de Signalerende duif — onder AA, en niet op te lossen
       door de tint te verzachten, want de oker haalt ook op puur wit maar
       5,08:1. Op papier haalt elk van de drie ruim boven de 4,5:1, en de
       markering blijft even zichtbaar: zij steekt nu af doordat zij lichter is
       dan de bubbel, met de accentkleur in de rand en het cijfer. */
    background: var(--duif-paper);
    border: 1px solid color-mix(in oklch, var(--answer-accent, var(--duif-ink)) 45%, transparent);
    border-radius: 0.4em;
    padding: 0.16em 0.26em;
    /* Lucht vóór het merkje; de negatieve marge erna trekt de komma of punt
       die volgt weer tegen de zin aan, zodat het leesteken niet los zweeft. */
    margin-inline: 0.18em -0.12em;
    cursor: pointer;
    transition:
      background-color 0.12s,
      color 0.12s;
  }

  .duif-cite:hover,
  .duif-cite:focus-visible,
  .duif-cite[aria-expanded='true'] {
    background: var(--answer-accent, var(--duif-ink));
    border-color: var(--answer-accent, var(--duif-ink));
    color: var(--duif-paper);
  }

  .duif-cursor {
    animation: duif-blink 1s step-end infinite;
  }

  @keyframes duif-blink {
    50% {
      opacity: 0;
    }
  }
</style>
