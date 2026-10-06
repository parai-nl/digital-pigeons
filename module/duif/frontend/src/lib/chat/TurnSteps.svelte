<script lang="ts">
  import { useServices } from '../services.svelte'
  import '../sources/answerChips.css'
  import type { TurnStep } from './chatStore.svelte'

  // De stapregel van één beurt. Een chatbeurt is vier tot vijf
  // modelcalls achter elkaar; zonder deze regel ziet de lezer daar één trage
  // gedachte van, en weet hij niet of er nog iets komt.
  //
  // Vormtaal: dezelfde chips als onder een antwoord (answerChips.css), zodat
  // dit als hetzelfde scherm leest en niet als een systeemmelding. De stap die
  // loopt draagt een beletselteken en een rustig kloppend bolletje; de stappen
  // die klaar zijn blijven staan, gedempt, zodat zichtbaar is wat er al gebeurd
  // is. Ze verdwijnen met de beurt: chips worden niet bewaard, dus na herladen
  // zouden ze er niet meer zijn (zie ChatStore.steps).
  //
  // De schermlezer hoort één regel — de stap die nu loopt — en niet elke chip
  // apart. De chips zelf staan daarom buiten het live-gebied.
  // De teller van het wachten hoort niet hier maar onder de bubbel waar het
  // antwoord komt (ChatView, answer-waiting): daar kijkt de lezer, en één klok
  // hoort op één plek te staan.
  let { steps }: { steps: TurnStep[] } = $props()

  const { locale } = useServices()
  const current = $derived(steps.at(-1)?.done === false ? steps.at(-1) : undefined)
  const label = (step: TurnStep): string => locale.t(`chat.step.${step.phase}`)
</script>

{#if steps.length}
  <div class="turn-steps" role="group" data-testid="turn-steps" aria-label={locale.t('chat.steps.label')}>
    {#each steps as step (step.phase)}
      <span
        class="duif-answer-chip is-step"
        class:is-running={!step.done}
        data-testid="turn-step"
        data-step={step.phase}
        data-state={step.done ? 'done' : 'running'}
      >
        <span>{label(step)}{step.done ? '' : '…'}</span>
        {#if !step.done}<span class="step-pulse" aria-hidden="true"></span>{/if}
      </span>
    {/each}
  </div>
{/if}
<!-- Eén melding per stapwissel. De sleutel dwingt een nieuwe knoop af, zodat
     een schermlezer ook een herhaalde formulering opnieuw uitspreekt.
     De regel staat buiten de {#if}: een live-gebied dat pas ontstaat mét zijn
     eerste inhoud wordt door schermlezers niet voorgelezen, dus is hij er
     altijd — leeg buiten een beurt — net als de Announcement-regel van de
     store. -->
<p class="sr-only" role="status" data-testid="turn-step-status">
  {#key current?.phase}{current ? locale.t('chat.steps.busy', { step: label(current) }) : ''}{/key}
</p>

<style>
  /* De regel staat waar de algemene voortgangszin stond: onder het gesprek, op
     dezelfde inspringing als de bubbels. Hij wikkelt op smalle schermen. */
  .turn-steps {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: .35rem .4rem;
    padding: .35rem .75rem .2rem;
  }
  /* De afgeronde stappen zijn er om terug te kijken, niet om te lezen: ze
     staan een tint zwakker dan de lopende stap en dragen geen beweging. */
  .turn-steps :global(.duif-answer-chip.is-step[data-state='done']) { opacity: .62; }
  .step-pulse {
    width: .35rem; height: .35rem; border-radius: 999px; flex: none;
    background: currentColor;
    animation: step-pulse 1.2s ease-in-out infinite alternate;
  }
  @keyframes step-pulse { from { opacity: .25; } to { opacity: 1; } }
  /* Wie beweging uit heeft gezet, ziet een stil bolletje in plaats van geen
     bolletje: de stap die loopt blijft herkenbaar. */
  @media (prefers-reduced-motion: reduce) {
    .step-pulse { animation: none; opacity: .8; }
  }
</style>
