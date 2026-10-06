<script lang="ts">
  // De schakelaar "zelfstandig meedoen" plus de hint eronder. Hij staat apart
  // van de andere instellingen omdat hij niet overal op dezelfde plek hoort:
  // bij de signalerende duif staat hij op de opzetkaart zelf — zij
  // heeft geen woorden, dus dit is het enige wat zichtbaar maakt dat zij
  // meeluistert — en bij de pratende duiven blijft hij onder "Geavanceerd".
  // De hint zegt per duif wat de standaardstand is en waarom.
  import Switch from '@parai/ui/components/Switch.svelte'
  import type { UiLocale } from '../i18n.svelte'
  import type { MeetingStore } from './meetingStore.svelte'

  let { store, locale }: { store: MeetingStore; locale: UiLocale } = $props()

  const hint = $derived(store.speaks ? 'meeting.setup.autonomousHint.speaking' : 'meeting.setup.autonomousHint.signal')
</script>

<div class="flex flex-col gap-1" data-testid="autonomy-field">
  <label class="flex items-center justify-between gap-4 text-sm">
    <span>{locale.t('meeting.setup.autonomous')}</span>
    <Switch
      bind:checked={() => store.settings.autonomous, (next: boolean) => store.updateSettings({ autonomous: next })}
      onLabel={locale.t('meeting.setup.autonomous')}
      offLabel={locale.t('meeting.setup.autonomous')}
    />
  </label>
  <p class="text-xs text-muted-foreground">{locale.t(hint)}</p>
</div>
