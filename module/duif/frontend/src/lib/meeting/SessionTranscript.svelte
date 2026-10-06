<script lang="ts">
  // Het sessiegeheugen op één plek, onder het portret: standaard de laatst
  // gesproken zinnen, en het pijltje klapt dezelfde plek uit naar het hele
  // verloop — gesproken regels en eerdere bijdragen van de duif door elkaar, in
  // de volgorde waarin ze vielen. Het blijft weergave: het gaat nergens heen en
  // het verdwijnt met de sessie. De regel die dat zegt staat klein onder het
  // uitgeklapte verloop, niet als losse systeemzin op het podium.
  import type { UiLocale } from '../i18n.svelte'
  import MeetingAnswer from './MeetingAnswer.svelte'
  import type { AnswerLine, MeetingStore, TranscriptLine } from './meetingStore.svelte'

  // scroller is het schuifgebied van het podium. Het verloop meet zich daaraan
  // op: uitgeklapt mag het nooit hoger worden dan wat er nog zichtbaar is,
  // anders staan juist de nieuwste regels onder de vouw.
  let { store, locale, scroller = undefined }: { store: MeetingStore; locale: UiLocale; scroller?: HTMLElement } = $props()

  // Dicht blijft het bij de laatste twee zinnen: genoeg om te volgen waar het
  // gesprek staat, weinig genoeg om het portret niet weg te drukken.
  const RECENT = 2
  // De knop bedient dit gebied, of het nu de recente regels of het hele
  // verloop toont; het gebied bestaat dus in beide standen.
  const BODY_ID = 'meeting-transcript-body'
  const MIN_LOG = 150
  const MAX_LOG = 560

  let open = $state(false)
  let sectionEl: HTMLElement | undefined = $state()
  let logEl: HTMLOListElement | undefined = $state()
  let tailEl: HTMLDivElement | undefined = $state()
  let logMax = $state(0)

  // De STT-brug nummert sprekers; een kaal cijfer leest als "Speaker 2".
  const speakerLabel = (speaker: string, own: boolean): string => {
    if (own) return locale.t('meeting.transcript.own')
    if (/^\d+$/.test(speaker)) return locale.t('meeting.speaker', { n: speaker })
    return speaker
  }

  const recent = $derived(store.transcript.slice(-RECENT))
  // Het laatste antwoord staat groot op het podium; in het verloop staan de
  // eerdere bijdragen, op de plek in het gesprek waar ze vielen.
  const earlier = $derived(store.speaks ? store.answers.slice(0, -1) : [])

  type Entry = { key: string; at: number; rank: number; line?: TranscriptLine; answer?: AnswerLine }
  // rank houdt een bijdrage ná de regel waar ze op volgde, ook al dragen ze
  // hetzelfde volgnummer.
  const entries: Entry[] = $derived(
    [
      ...store.transcript.map((line) => ({ key: `line-${line.seq}`, at: line.seq, rank: 0, line })),
      ...earlier.map((answer) => ({ key: `answer-${answer.id}`, at: answer.afterSeq ?? 0, rank: 1, answer })),
    ].sort((a, b) => a.at - b.at || a.rank - b.rank),
  )

  // fit geeft het verloop precies de hoogte die het schuifgebied overhoudt:
  // alles buiten het log (kopregel, partial, geheugenregel) gaat er eerst af.
  function fit(): void {
    if (!open || !scroller || !sectionEl || !logEl) return
    const chrome = sectionEl.offsetHeight - logEl.offsetHeight
    const room = scroller.clientHeight - chrome - 24
    const next = Math.max(MIN_LOG, Math.min(MAX_LOG, room))
    if (next !== logMax) logMax = next
  }

  // Uitgeklapt leest men het laatste; nieuwe regels horen dan in beeld te
  // komen zonder dat iemand moet slepen.
  $effect(() => {
    entries.length
    store.partial
    if (!open || !logEl) return
    fit()
    logEl.scrollTop = logEl.scrollHeight
  })

  // Openen op de plek van de recente regels betekent dat de staart van het
  // verloop onder de vouw kan vallen; die halen we erbij. Het uitlijnen wacht
  // één beeldopbouw, anders rekent het met de hoogte van vóór de meting en
  // blijft de geheugenregel net onder de bedieningsbalk hangen.
  $effect(() => {
    logMax
    if (!open || !tailEl) return
    const tail = tailEl
    const log = logEl
    const align = () => {
      if (log) log.scrollTop = log.scrollHeight
      if (typeof tail.scrollIntoView === 'function') tail.scrollIntoView({ block: 'nearest' })
    }
    if (typeof requestAnimationFrame !== 'function') { align(); return }
    const frame = requestAnimationFrame(align)
    return () => cancelAnimationFrame(frame)
  })

  $effect(() => {
    const onResize = () => fit()
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  })
</script>

<section class="session-transcript" data-testid="session-transcript" bind:this={sectionEl}>
  <div class="transcript-head">
    <span class="head-label">{locale.t(open ? 'meeting.transcript.title' : store.partial ? 'meeting.preview.partial' : 'meeting.preview.final')}</span>
    <!-- Eén vast opschrift; aria-expanded zegt de stand. Een label dat meedraait
         laat een schermlezer "alleen recente spraak, uitgeklapt" voorlezen. -->
    <button
      type="button"
      class="transcript-toggle"
      data-testid="transcript-toggle"
      aria-expanded={open}
      aria-controls={BODY_ID}
      onclick={() => (open = !open)}
    >
      <span>{locale.t('meeting.transcript.expand')}</span>
      <svg class="chevron" class:up={open} viewBox="0 0 16 16" aria-hidden="true" focusable="false">
        <path d="M3.5 6 8 10.5 12.5 6" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" />
      </svg>
    </button>
  </div>

  <div class="transcript-body" id={BODY_ID}>
    {#if open}
      <!-- role="log" beschrijft wat dit is: het gesprek op volgorde. De
           voorleesregio staat er bewust uit. Het verloop bevat hele bijdragen
           van de duif, en die zouden bij elke nieuwe beurt integraal worden
           voorgelezen terwijl iemand aan het teruglezen is. Dezelfde regel als
           bij de partials: melden doen de banner en de luisterindicator. -->
      <ol
        class="transcript-log"
        bind:this={logEl}
        role="log"
        aria-live="off"
        aria-label={locale.t('meeting.transcript.title')}
        data-testid="transcript-log"
        style:max-height={logMax ? `${logMax}px` : null}
      >
        {#each entries as entry (entry.key)}
          <li class:own={entry.line?.own} class:contribution={Boolean(entry.answer)}>
            {#if entry.line}
              {#if entry.line.speaker || entry.line.own}<span class="speaker">{speakerLabel(entry.line.speaker, entry.line.own)}</span>{/if}{entry.line.text}
            {:else if entry.answer}
              <MeetingAnswer answer={entry.answer} {locale} />
            {/if}
          </li>
        {:else}
          <li class="quiet">{locale.t('meeting.transcript.empty')}</li>
        {/each}
      </ol>
      <div class="transcript-tail" bind:this={tailEl}>
        <!-- Partials horen niet in de live-regio: ze veranderen per woord en
             zouden een schermlezer onophoudelijk onderbreken. -->
        <p class="partial" aria-live="off" data-testid="transcript-partial">{store.partial ? store.partial.text : ''}</p>
        <p class="transcript-notice" data-testid="transcript-notice">{locale.t('meeting.transcript.notice')}</p>
      </div>
    {:else}
      <!-- transcript-preview bestaat alleen in de dichte stand; de browsercontroles
           (zoals tools/meeting-visueel-check.mjs) lezen deze testid. -->
      <div class="transcript-recent" data-testid="transcript-preview" aria-live="off">
        {#each recent as line (line.seq)}
          <p class="said">{#if line.speaker || line.own}<span class="speaker">{speakerLabel(line.speaker, line.own)}</span>{/if}{line.text}</p>
        {/each}
        {#if store.partial}<p class="said partial">{store.partial.text}</p>{/if}
        {#if recent.length === 0 && !store.partial}<p class="quiet">{locale.t('meeting.transcript.empty')}</p>{/if}
      </div>
    {/if}
  </div>
</section>

<style>
  .session-transcript { width: 100%; max-width: 34rem; margin-top: .9rem; text-align: left; }
  .transcript-head { display: flex; align-items: baseline; justify-content: space-between; gap: .75rem; padding-bottom: .2rem; border-bottom: 1px solid var(--duif-line); }
  .head-label { font-size: .6rem; text-transform: uppercase; letter-spacing: .06em; font-weight: 650; color: var(--duif-dim); }
  .transcript-toggle { display: inline-flex; align-items: center; gap: .35rem; min-height: 2rem; padding: .1rem 0 .1rem .4rem; border: 0; background: transparent; color: var(--duif-dim); font-size: .72rem; font-weight: 550; }
  .transcript-toggle:hover { color: var(--duif-ink); }
  .chevron { width: .85rem; height: .85rem; flex: none; transition: transform 150ms ease; }
  .chevron.up { transform: rotate(180deg); }
  /* Dicht houdt het gebied dezelfde hoogte, zodat het podium niet danst zodra
     er iets gezegd wordt. */
  .transcript-recent { min-height: 4rem; padding-top: .45rem; }
  .said { font-size: .8rem; line-height: 1.55; color: var(--duif-dim); margin: .15rem 0; overflow-wrap: anywhere; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 2; line-clamp: 2; overflow: hidden; }
  .said:last-of-type { color: var(--duif-ink); }
  .partial { font-style: italic; }
  .quiet { font-size: .8rem; line-height: 1.55; color: var(--duif-dim); margin: .15rem 0; }
  .speaker { font-weight: 650; color: var(--duif-ink); }
  .speaker::after { content: ': '; }
  /* De hoogte hieronder is de terugval zonder meting (geen scroller bekend);
     fit() zet er een inline hoogte overheen zodra het podium bekend is. */
  .transcript-log { list-style: none; margin: .5rem 0 0; padding: 0 .1rem 0 0; max-height: min(32vh, 19rem); overflow-y: auto; display: grid; gap: .5rem; font-size: .8rem; line-height: 1.55; }
  .transcript-log li { overflow-wrap: anywhere; }
  .transcript-log li.own { color: var(--duif-dim); }
  .transcript-log li.contribution { margin: .25rem 0; }
  /* Een bijdrage in het verloop is een teruglezing, geen tweede podium. */
  .transcript-log :global(.meeting-answer) { padding: .8rem .9rem; border-radius: .7rem; }
  .transcript-log :global(.answer-text) { font-size: .85rem; margin: .45rem 0; }
  .transcript-tail .partial { min-height: 1.1rem; font-size: .78rem; color: var(--duif-dim); margin: .4rem 0 0; }
  .transcript-notice { font-size: .65rem; line-height: 1.5; color: var(--duif-dim); margin: .5rem 0 0; }
</style>
