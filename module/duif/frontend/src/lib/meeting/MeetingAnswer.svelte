<script lang="ts">
  import BookOpen from 'phosphor-svelte/lib/BookOpen'
  import Compass from 'phosphor-svelte/lib/Compass'
  import { type UiLocale } from '../i18n.svelte'
  import { useServices } from '../services.svelte'
  import { holdPartialCitation, isDropReason } from '../sources'
  import { elapsedSuffix } from '../waiting'
  import CitedText from '../sources/CitedText.svelte'
  import '../sources/answerChips.css'
  import type { AnswerLine } from './meetingStore.svelte'
  let { answer, locale, prominent = false }: { answer: AnswerLine; locale: UiLocale; prominent?: boolean } = $props()
  const { meeting, shell, sources, roster } = useServices()
  // Wat er wegviel vóór de bronnenlijst van deze bijdrage. Eén
  // neutrale regel in dezelfde toon als de gat- en stopmelding erboven; de
  // redencodes zijn begeleiderstaal en staan alleen in devMode. De chat leent
  // dezelfde codes, dus ook dezelfde vertalingen.
  const drops = $derived(answer.dropped)
  const dropsText = $derived(
    drops ? locale.t(drops.count === 1 ? 'meeting.dropped.one' : 'meeting.dropped.many', { count: drops.count }) : '',
  )
  const dropsDetail = $derived.by(() => {
    if (!drops) return [] as string[]
    return Object.entries(drops.reasons).flatMap(([reason, count]) =>
      isDropReason(reason) ? [locale.t(`chat.dropped.${reason}`, { count })] : [],
    )
  })
  const streaming = $derived(answer.status === 'streaming')
  // Zonder bron mag de Stakeholder een begrensd standpunt innemen.
  // Aan tafel geldt hetzelfde als in de chat: bedoeld gedrag, eigen markering,
  // en niet de algemene gatregel die alleen zegt dat er niets gevonden is.
  const stanceWithoutResearch = $derived(answer.pigeonId === 'individualist' && answer.kbStatus === 'gap')
  const showStance = $derived(stanceWithoutResearch && !streaming)

  // Zolang de bijdrage nog geen letter heeft, wacht de tafel op het
  // model. Duurt dat lang, dan zegt de stand hoe lang: eerlijker dan een kopje
  // dat na veertig seconden nog precies hetzelfde meldt. Zodra er tekst is, is
  // het wachten voorbij en verdwijnt de teller.
  const waiting = $derived(streaming && answer.text === '')
  let now = $state(Date.now())
  $effect(() => {
    if (!waiting) return
    now = Date.now()
    const timer = setInterval(() => (now = Date.now()), 1000)
    return () => clearInterval(timer)
  })
  const waitSuffix = $derived(waiting ? elapsedSuffix(answer.startedAt, now) : '')

  // De bijdrage draagt dezelfde bronweergave als een chatantwoord: verwijzingen
  // als klikbare markeringen in de zin (CitedText), eronder "n bronnen" en het
  // label over de brondekking. Eén implementatie voor allebei.
  const owner = $derived(`meeting-${answer.id}`)
  const shown = $derived(streaming ? holdPartialCitation(answer.text) : answer.text)

  // Het kopje zegt in welke fase deze bijdrage is. Zolang de tekst binnenkomt:
  // "bijdrage voorbereiden". Is de tekst compleet en klinkt de stem nog (het
  // antwoord is dan al af, de duif praat het alleen nog uit), dan "spreekt".
  // Daarna niets meer: een afgeronde bijdrage draagt geen stand.
  const statusKey = $derived(
    streaming
      ? 'meeting.answer.writing'
      : prominent && answer.status === 'complete' && meeting.speaking
        ? 'meeting.answer.speaking'
        : null,
  )

  // Dezelfde lade als in de chat. Tijdens een lopende vergadering blijft de
  // verduistering weg: STOP is dan de belangrijkste knop op het scherm en moet
  // één klik weg blijven, niet twee (eerst de lade sluiten, dán stoppen).
  function openSource(number: number): void {
    sources.show(answer.sources, number, { owner, backdrop: !meeting.live })
  }
</script>

<article class="meeting-answer" class:prominent data-author={answer.pigeonId} data-testid={prominent ? 'latest-answer' : 'past-answer'}>
  <header><span>{roster.name(answer.pigeonId)}</span>{#if statusKey}<span class="answer-status" data-testid="answer-status">{locale.t(statusKey) + waitSuffix}</span>{/if}</header>
  {#if answer.note}<p class="answer-note">{answer.note}</p>{/if}
  {#if answer.text || !answer.stopReason}
    <p class="answer-text duif-voice" data-testid="answer-text"><CitedText
        text={answer.text ? shown : locale.t('meeting.answer.preparing')}
        sources={answer.text ? answer.sources : []}
        {streaming}
        {owner}
        open={openSource}
      /></p>
  {/if}
  {#if answer.stopReason}<p class="answer-note" data-testid="answer-stop-reason">{locale.t(`chat.stopped.${answer.stopReason}`)}</p>{/if}
  {#if answer.status === 'stopped' || answer.status === 'interrupted'}<p class="answer-note">{locale.t(answer.status === 'stopped' ? 'meeting.answer.stopped' : 'meeting.answer.interrupted')}</p>{/if}
  {#if answer.kbStatus === 'unavailable'}<p class="answer-note">{locale.t('meeting.answer.unavailable')}</p>{/if}
  {#if answer.kbStatus === 'gap' && !stanceWithoutResearch}<p class="answer-note">{locale.t('meeting.answer.gap')}</p>{/if}
  {#if drops && !streaming}
    <p class="answer-note" data-testid="answer-dropped">
      {dropsText}{#if shell.dev && dropsDetail.length}<span class="dropped-detail"> · {locale.t('chat.dropped.detail')}: {dropsDetail.join(', ')}</span>{/if}
    </p>
  {/if}
  <!-- Eén voetregel, net als in de chat: de bronchip die de lade opent, en de
       dekkingsmarkering van de Stakeholder ernaast. -->
  {#if answer.sources.length > 0 || showStance}
    <div class="answer-footer" data-testid="answer-footer">
      {#if showStance}
        <span class="duif-answer-chip is-stance" data-testid="stance-marker"><Compass size={14} weight="regular" aria-hidden="true" /><span>{locale.t('chat.stance.withoutResearch')}</span></span>
      {/if}
      {#if answer.sources.length > 0}
        <button
          type="button"
          class="duif-answer-chip is-sources"
          data-testid="source-count"
          aria-haspopup="dialog"
          onclick={() => openSource(answer.sources[0].number)}
        ><BookOpen size={14} weight="regular" aria-hidden="true" /><span>{locale.t(answer.sources.length === 1 ? 'chat.source.count.one' : 'chat.source.count.many', { count: answer.sources.length })}</span></button>
      {/if}
    </div>
  {/if}
</article>

<style>
  /* De accentkleur per duif voedt de verwijzingen in de zin en de bronchip;
     CitedText en answerChips.css lezen hem als --answer-accent. */
  .meeting-answer { background: var(--duif-paper); border: 1px solid var(--duif-line); border-radius: 1rem; padding: 1.25rem; min-width: 0; --answer-accent: var(--duif-persona-nonverbal); }
  [data-author='biological'] { --answer-accent: var(--duif-persona-biological); }
  [data-author='individualist'] { --answer-accent: var(--duif-persona-individualist); }
  header { display: flex; flex-wrap: wrap; justify-content: space-between; gap: .5rem; color: var(--duif-dim); font-size: .72rem; font-weight: 600; }
  .answer-text { white-space: pre-wrap; overflow-wrap: anywhere; font-size: 1.1rem; line-height: 1.5; margin: .8rem 0; }
  .prominent { position: relative; padding: clamp(1.1rem,2.4vw,2rem); box-shadow: 0 10px 40px #00000005; }
  /* De bijdrage vooraan leest als lopende tekst, niet als een plakkaat: de maat
     komt uit --duif-type-answer (app.css), zodat het podium en de startkaart
     samen één schaal houden. Kleinere letters vragen meer lucht tussen de
     regels dan de 1,4 die bij een affichemaat hoorde. */
  .prominent .answer-text { font-size: var(--duif-type-answer); line-height: 1.6; }
  .answer-note { color: var(--duif-dim); font-size: .75rem; line-height: 1.6; margin: .75rem 0; }
  /* De devMode-redenen achter de uitvalmelding: één tint stiller dan de
     mededeling zelf, want zij zijn voor de begeleider en niet voor de zaal. */
  .dropped-detail { color: color-mix(in oklab, var(--duif-dim) 80%, transparent); }
  /* De voetregel scheidt zich van het antwoord met dezelfde dunne lijn die de
     bronregel hiervoor had; de chips staan erop, niet eronder gestapeld. */
  .answer-footer { display: flex; flex-wrap: wrap; align-items: flex-start; gap: .4rem .5rem; margin-top: .9rem; padding-top: .7rem; border-top: 1px solid var(--duif-line); }
</style>
