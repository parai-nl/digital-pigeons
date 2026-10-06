<script lang="ts">
  import type { UiLocale } from '../i18n.svelte'
  import PigeonPortrait from './PigeonPortrait.svelte'
  import { PIGEONS, type PigeonId } from './pigeons'
  import type { RosterStore } from './roster.svelte'
  // De naam komt uit de roster (personas.yaml via de API), niet uit de
  // interfaceteksten: één naambron voor de hele schil.
  let { selected, locale, roster, onselect }: { selected: string; locale: UiLocale; roster: RosterStore; onselect: (id: PigeonId) => void } = $props()
</script>

<div class="pigeon-selector" role="group" aria-label={locale.t('shell.choosePigeon')}>
  {#each PIGEONS as pigeon}
    <button type="button" class="pigeon-choice" data-pigeon={pigeon.id} aria-label={roster.name(pigeon.id)} aria-pressed={selected === pigeon.id}
      onclick={() => onselect(pigeon.id)}>
      <span class="portrait"><PigeonPortrait pigeon={pigeon.id} /></span>
      <span class="identity"><span class="choice-number">{pigeon.number}</span><strong class:pending={roster.pending}>{roster.name(pigeon.id)}</strong><span class="choice-role">{locale.t(pigeon.role)}</span></span>
      <span class="selected-mark" aria-hidden="true">{selected === pigeon.id ? '●' : '○'}</span>
    </button>
  {/each}
</div>

<style>
  /* Compacter dan de eerste opzet: de kaarten blijven de duidelijke keuze
     bovenaan, maar geven hoogte terug aan het gesprek eronder. */
  .pigeon-selector { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: .6rem; }
  .pigeon-choice { --identity: var(--duif-persona-biological); display: flex; align-items: center; gap: .7rem; text-align: left; padding: .55rem .8rem; border: 1px solid var(--duif-line); border-radius: var(--duif-radius-card); background: var(--duif-paper); color: var(--duif-ink); transition: border-color 140ms, background 140ms; min-width: 0; position: relative; }
  .pigeon-choice[data-pigeon='individualist'] { --identity: var(--duif-persona-individualist); }
  .pigeon-choice[data-pigeon='nonverbal'] { --identity: var(--duif-persona-nonverbal); }
  .pigeon-choice:hover { background: color-mix(in oklch, var(--identity) 7%, var(--duif-paper)); }
  .pigeon-choice[aria-pressed='true'] { border-color: var(--identity); box-shadow: inset 0 0 0 1px var(--identity); background: color-mix(in oklch, var(--identity) 7%, var(--duif-paper)); }
  .portrait { width: clamp(2.2rem, 4vw, 3.4rem); flex: none; }
  .identity { min-width: 0; display: flex; flex-direction: column; gap: .1rem; }
  .choice-number { font-size: .6rem; font-weight: 650; letter-spacing: .12em; color: var(--duif-dim); }
  strong { font-size: clamp(.78rem, 1.1vw, .95rem); line-height: 1.25; }
  /* Zolang de naam nog onderweg is staat er een eerlijke laadregel; die hoort
     stiller te zijn dan de naam die er zo meteen staat. */
  strong.pending { color: var(--duif-dim); font-weight: 450; font-style: italic; }
  .choice-role { font-size: .72rem; line-height: 1.35; color: var(--duif-dim); }
  .selected-mark { margin-left: auto; align-self: flex-start; color: var(--identity); font-size: .7rem; }
  @media (max-width: 650px) {
    .pigeon-selector { gap: .4rem; }
    .pigeon-choice { flex-direction: column; align-items: flex-start; gap: .2rem; padding: .6rem; }
    .portrait { width: 2.7rem; }
    .choice-number, .choice-role { display: none; }
    .selected-mark { position: absolute; right: .6rem; top: .5rem; }
    strong { font-size: .75rem; overflow-wrap: anywhere; }
  }
  @media (max-height: 650px) {
    .pigeon-choice { flex-direction: row; align-items: center; gap: .5rem; padding: .45rem .6rem; }
    .portrait { width: 1.9rem; }
    .choice-number, .choice-role { display: none; }
    .selected-mark { position: static; align-self: center; }
  }
</style>
