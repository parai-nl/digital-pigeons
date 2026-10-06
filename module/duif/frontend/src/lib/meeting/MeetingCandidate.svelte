<script lang="ts">
  // Het kandidaatblok. De duif heeft iets klaarliggen; dit blok maakt dat
  // zichtbaar en geeft één uitweg: laat het nu horen. Zij wacht niet op een
  // stilte — zij breekt zelf in — dus de onderregel belooft geen wachten.
  // Aangesproken worden is de uitzondering: dan komt haar antwoord ná de
  // lopende zin, en dat is het enige wat deze kaart weet.
  //
  // Wat er níét staat is de bijdrage zelf. De vergadering ziet dát de duif iets
  // heeft en waarover — de woorden hoor je pas als ze klinken. Bij de
  // signalerende duif staat er geen thema en geen score: aan haar geluid hangt
  // geen menselijke betekenis.
  import Button from '@parai/ui/components/Button.svelte'
  import type { UiLocale } from '../i18n.svelte'
  import type { MeetingStore } from './meetingStore.svelte'
  import { CANDIDATE_REASONS } from './protocol'

  let { store, locale }: { store: MeetingStore; locale: UiLocale } = $props()

  const candidate = $derived(store.candidate)
  // Zolang de duif praat is er niets te laten horen: dan is het blok van de
  // vorige gedachte, niet van deze.
  const visible = $derived(Boolean(candidate) && !store.speaking && !store.autonomyPaused)

  // Het themalabel is een titel ("Wat duiven toekomt") en begint met een
  // hoofdletter. Midden in een zin las dat als een tikfout ("iets over Wat
  // duiven toekomt"), dus de zin eindigt op een dubbele punt en het
  // label staat erachter als een eigen label, ongewijzigd.
  const topic = $derived(
    candidate && candidate.reason !== CANDIDATE_REASONS.signal && candidate.reason !== CANDIDATE_REASONS.addressed
      ? locale.pick(candidate.topic?.label)
      : '',
  )

  const headline = $derived.by(() => {
    if (!candidate) return ''
    if (candidate.reason === CANDIDATE_REASONS.signal) return locale.t('meeting.candidate.signal')
    if (candidate.reason === CANDIDATE_REASONS.addressed) return locale.t('meeting.candidate.addressed')
    return topic ? locale.t('meeting.candidate.topic') : locale.t('meeting.candidate.general')
  })

  const timing = $derived.by(() => {
    if (!candidate) return ''
    return candidate.reason === CANDIDATE_REASONS.addressed
      ? locale.t('meeting.candidate.after-the-sentence')
      : locale.t('meeting.candidate.breaking-in')
  })
</script>

{#if visible && candidate}
  <div class="candidate" data-testid="meeting-candidate" data-reason={candidate.reason} role="status" aria-live="polite">
    <div class="what">
      <p class="headline">
        {headline}{#if topic}{' '}<span class="topic" data-testid="candidate-topic">{topic}</span>{/if}
      </p>
      <p class="timing">
        {timing}
        {#if candidate.sources > 0}<span class="sources" data-testid="candidate-sources"
            >{locale.t(
              candidate.sources === 1 ? 'meeting.candidate.sources.one' : 'meeting.candidate.sources.many',
              { n: candidate.sources },
            )}</span
          >{/if}
      </p>
    </div>
    <Button size="sm" disabled={store.candidateInFlight} onclick={() => void store.speakCandidate()} data-testid="candidate-speak">
      {locale.t('meeting.candidate.speak')}
    </Button>
  </div>
{/if}

<style>
  .candidate {
    /* Dezelfde leesbreedte als de scène eronder (.stage-scene, 78rem): het
       blok kondigt iets aan over de duif die daar staat, dus het hoort met
       haar uit te lijnen. Op de 68rem van de meldingsbalken sprong het 64
       pixels naar binnen ten opzichte van de kolommen. */
    max-width: 78rem;
    margin: 0 auto 1rem;
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 0.5rem 1rem;
    padding: 0.7rem 0.9rem;
    border: 1px solid var(--duif-line);
    border-left-width: 3px;
    border-radius: 0.4rem;
    background: var(--duif-paper);
  }
  .what {
    min-width: 0;
  }
  .headline {
    margin: 0;
    font-size: 0.85rem;
    font-weight: 600;
    line-height: 1.4;
  }
  .topic {
    display: inline-block;
    padding: 0 0.45rem;
    border: 1px solid var(--duif-line);
    border-radius: 999px;
    font-weight: 600;
  }
  .timing {
    margin: 0.15rem 0 0;
    font-size: 0.75rem;
    line-height: 1.5;
    color: var(--duif-dim);
  }
  .sources::before {
    content: ' · ';
  }
</style>
