<script lang="ts">
  // De vergaderinstellingen op één plek, en ook maar op één plek per scherm: de
  // opzetkaart en de live-instellingen tonen precies dezelfde knoppen. De
  // autonomieschakelaar hoort daarbij, behalve waar hij al in het zicht staat
  // (`showAutonomy`). De velden schrijven
  // met function bindings rechtstreeks in de store — geen spiegelende
  // $effect-paren meer die met het settings-echo van de server vechten, en één
  // POST per bedoeling omdat updateSettings de verzending uitstelt.
  import Select from '@parai/ui/components/Select.svelte'
  import Slider from '@parai/ui/components/Slider.svelte'
  import type { UiLocale } from '../i18n.svelte'
  import AutonomyField from './AutonomyField.svelte'
  import { EAGERNESS_KEYS, MAX_COOLDOWN_SECONDS } from './labels'
  import type { MeetingStore } from './meetingStore.svelte'
  import { EAGERNESS, isEagerness } from './protocol'

  // showAutonomy staat uit wanneer de schakelaar al buiten dit blok staat: op
  // de opzetkaart van de signalerende duif hoort hij in het zicht, en twee keer
  // dezelfde schakelaar op één scherm is er één te veel.
  let {
    store,
    locale,
    showAutonomy = true,
  }: { store: MeetingStore; locale: UiLocale; showAutonomy?: boolean } = $props()

  const options = $derived(EAGERNESS.map((value) => ({ value, label: locale.t(EAGERNESS_KEYS[value]) })))

  // Select levert een kale string; alleen een stand die het protocol kent mag
  // door, zodat een onbekende waarde de instellingen niet stilletjes verzet.
  function chooseEagerness(value: string): void {
    if (isEagerness(value)) store.updateSettings({ eagerness: value })
  }
</script>

<div class="flex flex-col gap-4" data-testid="settings-fields">
  {#if showAutonomy}<AutonomyField {store} {locale} />{/if}

  <label class="flex flex-col gap-1 text-sm">
    <span>{locale.t('meeting.setup.eagerness')}</span>
    <Select
      bind:value={() => store.settings.eagerness, chooseEagerness}
      {options}
      placeholder={locale.t('common.select.placeholder')}
      searchPlaceholder={locale.t('common.select.search')}
    />
  </label>

  <!-- De huidige stand staat op één vaste plek: direct achter het label, in
       de kleur en het gewicht van de tekst. De uitersten staan klein en grijs
       onder de twee uiteinden van de schuif, en altijd allebei. Stond de stand
       rechtsboven, dan las hij als een tweede maximum naast "1 uur"; en de
       schuif zelf verbergt zijn minimumlabel zodra de knop erboven staat, dus
       bij de standaardstand 0 bleef er onder de schuif alleen "1 uur" over.
       Daarom krijgt de schuif lege labels en staan de uitersten
       hier. -->
  <label class="flex flex-col gap-1 text-sm">
    <span class="flex flex-wrap items-baseline gap-x-2">
      <span>{locale.t('meeting.setup.cooldown')}</span>
      <!-- 0 is geen "geen pauze": het betekent dat de duif haar eigen rust
           houdt, en die verschilt per duif (spreeklat). -->
      <span class="font-semibold" data-testid="cooldown-value"
        >{store.settings.cooldownSeconds === 0
          ? locale.t('meeting.setup.cooldown.own')
          : locale.t('meeting.setup.seconds', { n: store.settings.cooldownSeconds })}</span
      >
    </span>
    <Slider
      bind:value={() => store.settings.cooldownSeconds, (next: number) => store.updateSettings({ cooldownSeconds: next })}
      min={0}
      max={MAX_COOLDOWN_SECONDS}
      step={30}
      showValue={false}
      minLabel=""
      maxLabel=""
    />
    <span class="-mt-1 flex justify-between gap-4 text-[0.65rem] text-muted-foreground" data-testid="cooldown-range">
      <span>{locale.t('meeting.setup.cooldown.own')}</span>
      <span>{locale.t('meeting.setup.cooldown.max')}</span>
    </span>
  </label>
</div>
