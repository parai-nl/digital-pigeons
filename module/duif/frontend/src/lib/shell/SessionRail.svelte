<script lang="ts">
  // De rail is geen tweede bedieningspaneel. Zij verschijnt alleen wanneer het
  // vergaderscherm niet zichtbaar is — op de chat en in de graaf dus — en zegt
  // dan twee dingen: de vergadering loopt (met wie, met welke microfoonstand,
  // in welke stand) en "Stop inbreng" is hier. Beëindigen staat er bewust
  // niet: dat is één klik die de tafel opbreekt, en die hoort op het
  // vergaderscherm zelf thuis. Alle overige bediening staat één keer, daar.
  // Staat de uitleg open, dan schemert de rail onder haar dimlaag door:
  // zichtbaar dat de vergadering doorloopt, en één klik naast het venster
  // geeft hem terug.
  import type { UiLocale, MsgKey } from '../i18n.svelte'
  import { STATUS_KEYS } from '../meeting/labels'
  import type { MeetingStore } from '../meeting/meetingStore.svelte'
  import type { RosterStore } from './roster.svelte'
  let { store, locale, roster }: { store: MeetingStore; locale: UiLocale; roster: RosterStore } = $props()
  // De microfoonstand blijft apart en letterlijk: een lopende vergadering mag
  // nooit suggereren dat er wordt meegeluisterd terwijl dat niet zo is.
  const micKey: MsgKey = $derived(store.localMic === 'open' ? 'shell.session.micOn' : store.localMic === 'opening' ? 'shell.session.micOpening' : store.localMic === 'refused' ? 'shell.session.micRefused' : 'shell.session.micOff')
</script>

<aside class="session-rail" aria-label={locale.t('shell.session')} data-testid="global-session">
  <div class="session-identity"><span class="duif-eyebrow">{locale.t('shell.session')}</span><strong>{roster.name(store.pigeonId)}</strong></div>
  <div class="session-status" role="status" aria-live="polite">
    <span class="mic-status" class:mic-on={store.localMic === 'open'}><span class="mic-dot" aria-hidden="true"></span>{locale.t(micKey)}</span>
    <span data-testid="session-status">{locale.t(STATUS_KEYS[store.status])}</span>
  </div>
  <button class="session-stop" type="button" onclick={() => void store.stop()}>{locale.t('shell.session.stop')}</button>
</aside>

<style>
  .session-rail { display: flex; align-items: center; flex-wrap: wrap; gap: .7rem 1.5rem; padding: .85rem clamp(1rem, 3vw, 2rem); background: var(--duif-paper); border-block: 1px solid var(--duif-line); box-shadow: 0 3px 12px #00000005; flex: none; }
  .session-identity { display: grid; gap: .2rem; }
  strong { font-size: .85rem; }
  .session-status { display: flex; flex-direction: column; gap: .2rem; color: var(--duif-dim); font-size: .72rem; }
  .mic-status { display: flex; align-items: center; gap: .4rem; color: var(--duif-ink); font-weight: 600; font-size: .8rem; }
  .mic-dot { width: .6rem; height: .6rem; border-radius: 50%; border: 1px solid currentColor; }
  .mic-on .mic-dot { background: var(--duif-listen); border-color: var(--duif-listen); }
  /* Dezelfde stilteknop als op het vergaderpodium, alleen kleiner: dit is de
     enige bediening die overblijft zodra het vergaderscherm niet in beeld is,
     dus hij mag niet als een tekstknopje lezen. */
  .session-stop { margin-left: auto; background: var(--duif-stop); color: #fff; border: 0; padding: .7rem 1.4rem; border-radius: .75rem; font-weight: 700; font-size: .95rem; min-height: 3rem; min-width: 7rem; box-shadow: 0 8px 22px -14px var(--duif-stop); }
  .session-stop:hover { transform: translateY(-1px); }
  .session-stop:focus-visible { outline: 3px solid var(--duif-ink); outline-offset: 3px; }
  @media (max-width: 650px) { .session-rail { gap: .5rem 1rem; padding: .65rem .8rem; } .session-stop { margin-left: 0; } }
</style>
