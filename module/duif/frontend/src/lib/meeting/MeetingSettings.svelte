<script lang="ts">
  // De instelknoppen, live tijdens de vergadering: dezelfde velden als op de
  // opzetkaart, zodat "voor" en "tijdens" niet uit elkaar lopen. Ze staan in de
  // onderste regel bij het andere regiewerk en openen als een klein paneel
  // boven die regel — rustig, en nooit tussen de twee hoofdknoppen in. Elke
  // wijziging gaat naar /settings; de server bevestigt met een settings-event
  // en is daarmee de baas.
  import type { UiLocale } from '../i18n.svelte'
  import type { MeetingStore } from './meetingStore.svelte'
  import SettingsFields from './SettingsFields.svelte'

  let { store, locale }: { store: MeetingStore; locale: UiLocale } = $props()
</script>

<details class="meeting-settings" data-testid="meeting-settings">
  <summary>
    <span>{locale.t('meeting.settings.advanced')}</span>
    <svg class="chevron" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
      <path d="M3.5 6 8 10.5 12.5 6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
    </svg>
  </summary>
  <div class="settings-content"><SettingsFields {store} {locale} /></div>
</details>

<style>
  summary { display: inline-flex; align-items: center; gap: .35rem; min-height: 2.5rem; padding: .25rem .1rem; color: var(--duif-dim); font-size: .75rem; font-weight: 550; cursor: pointer; list-style: none; }
  summary::-webkit-details-marker { display: none; }
  summary:hover { color: var(--duif-ink); }
  .meeting-settings[open] > summary { color: var(--duif-ink); }
  .chevron { width: .85rem; height: .85rem; flex: none; transition: transform 150ms ease; }
  .meeting-settings[open] .chevron { transform: rotate(180deg); }
  /* Het paneel hangt boven de hele bedieningsbalk (position: relative staat op
     .meeting-actions), nooit erover: STOP blijft zichtbaar en aanraakbaar. */
  .settings-content { position: absolute; z-index: 20; bottom: calc(100% + .5rem); left: 0; width: min(24rem, calc(100vw - 2rem)); max-height: min(50vh, 20rem); overflow-y: auto; background: var(--duif-paper); border: 1px solid var(--duif-line); border-radius: var(--duif-radius-card); padding: 1rem; box-shadow: 0 16px 40px -18px #00000059; }
</style>
