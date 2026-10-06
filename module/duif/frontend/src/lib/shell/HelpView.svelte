<script lang="ts">
  // De volgorde is die van de vraag waarmee mensen de uitleg openen:
  // eerst wie die drie duiven zijn, dan pas hoe je begint; onder de
  // introductie en de drie stappen vond je ze niet terug. De
  // uitlegvideo gaat daar nog boven: wie wil zien hoe het werkt, hoeft
  // niets te lezen. Zij speelt nooit vanzelf; geluid en bediening zijn van de
  // kijker, en het venster zet haar stil bij het sluiten (HelpDialog).
  import catalogue from 'virtual:duif-media'
  import type { UiLocale } from '../i18n.svelte'
  import { helpEn, helpNl, type HelpKey } from './helpCopy'
  import { PIGEONS } from './pigeons'
  import PigeonPortrait from './PigeonPortrait.svelte'
  import type { RosterStore } from './roster.svelte'
  import { noticeEn, noticeNl } from '../access/noticeCopy'
  let { locale, roster, onclose, ongraph, meetingLive = false }: { locale: UiLocale; roster: RosterStore; onclose: () => void; ongraph: () => void; meetingLive?: boolean } = $props()
  const copy = $derived(locale.lang === 'nl' ? helpNl : helpEn)
  const privacy = $derived(locale.lang === 'nl' ? noticeNl : noticeEn)
  const steps = ['one', 'two', 'three'] as const
  const questions = ['real', 'sources', 'method', 'voice', 'chat', 'controls', 'access', 'problem', 'memory', 'data'] as const
  const explainer = catalogue.explainer ?? null
  // De beeldverhouding komt uit het pakket, niet uit een aanname: de video voor
  // de stand is 4:3, de placeholder 16:9. check-media --probe controleert
  // dat de maat in de catalogus die van het bestand is en dat de pixels
  // vierkant zijn (SAR 1:1), zodat die maat ook de verhouding op het scherm is.
  const ratio = explainer ? `${explainer.width} / ${explainer.height}` : undefined
  let videoFailed = $state(false)
  let player: HTMLVideoElement | undefined = $state()
  // Tijdens een vergadering staat de microfoon open onder het venster; wat de
  // video laat horen (verzonnen deelnemers, duivenstemmen) zou dan als inbreng
  // de echte vergadering in gaan. Dus geen afspelen zolang zij loopt, en een
  // video die al speelde staat stil vóórdat de speler plaatsmaakt voor de
  // poster.
  $effect.pre(() => { if (meetingLive && player && !player.paused) player.pause() })
  const forms: Record<string, HelpKey> = { biological: 'help.forms.biological', individualist: 'help.forms.individualist', nonverbal: 'help.forms.nonverbal' }
</script>

<section class="help-view" aria-labelledby="duif-help-title" data-testid="help-view">
  <div class="help-content">
    <header><span class="duif-eyebrow">{copy['help.eyebrow']}</span><h2 id="duif-help-title">{copy['help.title']}</h2><p class="help-intro">{copy['help.intro']}</p></header>
    {#if explainer}<section class="help-video" aria-labelledby="duif-help-video" data-testid="help-video">
      <h3 id="duif-help-video">{copy['help.video.title']}</h3><p>{copy[explainer.temporary ? 'help.video.placeholder' : 'help.video.intro']}</p>
      {#if videoFailed}<p class="help-video-failed" role="status">{copy['help.video.failed']}</p>
      {:else if meetingLive}<div class="help-video-held"><img src={explainer.posterUrl} width={explainer.width} height={explainer.height} style:aspect-ratio={ratio} alt="" /><p role="status">{copy['help.video.meeting']}</p></div>
      {:else}<!-- svelte-ignore a11y_media_has_caption: de ondertitels zijn optioneel; zonder VTT vertelt de help eronder hetzelfde in tekst. -->
      <video bind:this={player} src={explainer.url} poster={explainer.posterUrl} width={explainer.width} height={explainer.height} style:aspect-ratio={ratio} controls preload="metadata" playsinline lang={explainer.language} onerror={() => videoFailed = true}>
        {#if explainer.captionsUrl}<track kind="captions" src={explainer.captionsUrl} srclang={explainer.language} label={copy['help.video.captions']} />{/if}
      </video>{/if}
    </section>{/if}
    <section aria-labelledby="duif-help-forms"><h3 id="duif-help-forms">{copy['help.forms.title']}</h3><div class="help-forms">
      {#each PIGEONS as pigeon}<article><div class="help-portrait"><PigeonPortrait pigeon={pigeon.id} /></div><h4 class:pending={roster.pending}>{roster.name(pigeon.id)}</h4><p>{copy[forms[pigeon.id]]}</p></article>{/each}
    </div></section>
    <section aria-labelledby="duif-help-start"><h3 id="duif-help-start">{copy['help.start.title']}</h3><p>{copy['help.short']}</p><ol class="help-steps">
      {#each steps as step, index}<li><span class="help-step-number" aria-hidden="true">{index + 1}</span><div><h4>{copy[`help.start.${step}.title`]}</h4><p>{copy[`help.start.${step}.body`]}</p></div></li>{/each}
    </ol></section>
    <section aria-labelledby="duif-help-faq"><h3 id="duif-help-faq">{copy['help.faq.title']}</h3>
      {#each questions as question}<details><summary>{copy[`help.${question}.q`]}</summary><p>{copy[`help.${question}.a`]}</p></details>{/each}
    </section>
    <section class="help-reflect"><h3>{copy['help.reflect.title']}</h3><p>{copy['help.reflect.body']}</p><button type="button" class="duif-text-action" onclick={ongraph}>{copy['help.graph']} <span aria-hidden="true">↗</span></button></section>
    <section><h3>{privacy.detail}</h3><p>{privacy.chat}</p><p>{privacy.meeting}</p><p>{privacy.providers}</p><p>{privacy.school}</p><p>{privacy.record}</p></section>
    <!-- De tweede uitgang, onderaan: wie de hele uitleg uitleest hoeft niet
         terug te scrollen naar het kruis om weer verder te kunnen. -->
    <div class="help-exit"><button type="button" class="duif-text-action" onclick={onclose}><span aria-hidden="true">←</span> {locale.t('shell.closeHelp')}</button></div>
  </div>
</section>

<style>
  /* De uitleg scrollt binnen het venster, niet het venster binnen de laag: zo
     blijft het sluitkruis staan waar het staat. */
  .help-view { flex: 1; min-height: 0; overflow-y: auto; padding: 1.25rem clamp(1rem, 4vw, 3rem) 2rem; }
  .help-content { max-width: 62rem; margin: 0 auto; display: grid; gap: 2.5rem; }
  /* De kop houdt rechts ruimte vrij voor het sluitkruis van het venster. */
  header { padding-right: 2.75rem; }
  h2 { font-family: var(--font-serif); font-size: clamp(1.8rem, 4vw, 2.8rem); font-weight: 500; line-height: 1.2; margin: .4rem 0 1rem; }
  h3 { font-size: 1.1rem; margin: 0 0 1rem; }
  h4 { font-size: .95rem; margin: .6rem 0; }
  /* De naam komt van de server; zolang hij onderweg is staat er een eerlijke
     laadregel in plaats van een naam. */
  h4.pending { color: var(--duif-dim); font-weight: 450; font-style: italic; }
  p { line-height: 1.75; max-width: 75ch; color: var(--duif-dim); margin: .5rem 0; font-size: .9rem; }
  .help-intro { color: var(--duif-ink); font-size: 1.05rem; }
  /* De verhouding van de video (inline, uit het pakket) staat al vast voordat
     de poster binnen is, zodat de tekst eronder niet verspringt. Op een breed
     scherm blijft het beeld rond 640 px breed, zodat de drie duivenrollen dicht
     bij de kop blijven; een 4:3-video wordt daardoor hoger, niet
     smaller, omdat de letters van de app erin leesbaar moeten blijven. Op een
     smal scherm vult het de breedte. */
  .help-video video, .help-video-held img { display: block; width: 100%; max-width: 40rem; height: auto; margin-top: 1rem; background: var(--duif-feather); border: 1px solid var(--duif-line); border-radius: var(--duif-radius-card); }
  .help-video-held img { opacity: .6; object-fit: contain; }
  .help-video-failed { border-left: 2px solid var(--duif-line); padding-left: 1rem; }
  .help-steps { list-style: none; padding: 0; margin: 1rem 0 0; display: grid; gap: 1rem; }
  .help-steps li { display: flex; align-items: start; gap: 1rem; }
  .help-step-number { display: grid; place-items: center; width: 2rem; height: 2rem; flex-shrink: 0; border: 1px solid var(--duif-line); border-radius: 50%; font-size: .8rem; }
  .help-steps h4 { margin: .2rem 0; }
  .help-forms { display: grid; grid-template-columns: repeat(3, minmax(0,1fr)); gap: 1.5rem; }
  .help-portrait { width: 4rem; }
  details { border-top: 1px solid var(--duif-line); padding: 1rem 0; }
  summary:focus-visible { outline: 2px solid var(--duif-focus-ring); outline-offset: 4px; }
  summary { cursor: pointer; font-weight: 550; line-height: 1.5; }
  details p { margin: .8rem 0 0; padding-left: 1rem; }
  .help-reflect { border: 1px solid var(--duif-line); padding: 1.5rem; border-radius: var(--duif-radius-card); background: var(--duif-paper); }
  .help-exit { border-top: 1px solid var(--duif-line); padding-top: .5rem; }
  @media (max-width: 650px) { .help-forms { grid-template-columns: 1fr; gap: 1rem; } .help-forms article { border-left: 2px solid var(--duif-line); padding-left: 1rem; } .help-portrait { display: none; } }
</style>
