<script lang="ts">
  // De permanente luisterindicator. Hij is een afgeleide van de
  // werkelijkheid, staat er altijd, en zegt het ook met vorm — gevuld, half,
  // hol — zodat kleur niet de enige drager is.
  import { LISTEN, type ListenState } from './protocol'

  // `listen` en niet `state`: de prop draagt de luisterstand, en `state` leest
  // in Svelte 5 als de runes-term voor "een stuk lokale toestand".
  let { listen, label }: { listen: ListenState; label: string } = $props()
</script>

<div class="listen" role="status" aria-live="polite" data-testid="listen-indicator" data-state={listen}>
  <span class="dot" class:listening={listen === LISTEN.listening} class:reconnecting={listen === LISTEN.reconnecting} aria-hidden="true"
  ></span>
  <span class="text">{label}</span>
</div>

<style>
  .listen {
    display: flex;
    align-items: center;
    gap: 0.75rem;
  }
  .text {
    font-size: clamp(1.5rem, 2.2vw, 2rem);
    font-weight: 600;
    letter-spacing: -0.01em;
  }
  .dot {
    width: 20px;
    height: 20px;
    flex: none;
    border-radius: 999px;
    border: 3px solid var(--duif-off);
    background: transparent;
  }
  .dot.listening {
    border-color: var(--duif-listen);
    background: var(--duif-listen);
    animation: duif-breathe 4s ease-in-out infinite;
  }
  /* Halfgevuld: de verbinding hapert, de duif hoort ons misschien niet. */
  .dot.reconnecting {
    border-color: var(--duif-reconnect);
    background: linear-gradient(90deg, var(--duif-reconnect) 50%, transparent 50%);
    animation: duif-blink 1s step-end infinite;
  }
  @keyframes duif-breathe {
    0%,
    100% {
      transform: scale(1);
    }
    50% {
      transform: scale(1.18);
    }
  }
  @keyframes duif-blink {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .dot {
      animation: none;
    }
  }
</style>
