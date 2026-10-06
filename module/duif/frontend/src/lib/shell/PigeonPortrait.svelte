<script lang="ts">
  import catalogue from 'virtual:duif-media'
  import type { ImageAsset, PigeonIdentity } from '../media/types'
  // `variant="head"` kiest de krappe kopuitsnede die naast een chatbeurt staat.
  // Die weegt een derde van de kaartuitsnede en blijft leesbaar op 37 px; valt
  // hij weg, dan komt het kaartportret in de plaats en daarna de getekende duif.
  // Een expliciete `asset` (vergaderpodium, signaalbeeld) gaat overal voor.
  let { pigeon = 'biological', variant = 'card', asset, onfailure }: { pigeon?: string; variant?: 'card' | 'head'; asset?: ImageAsset | null; onfailure?: () => void } = $props()
  const identity = $derived(pigeon as PigeonIdentity)
  const slot = $derived(variant === 'head' ? catalogue.headshots?.[identity] ?? catalogue.portraits[identity] : catalogue.portraits[identity])
  const selected = $derived(asset === undefined ? slot : asset)
  let failed = $state(false)
  let loaded = $state(false)
  $effect(() => { selected?.url; failed = false; loaded = false })
</script>

<div class="pigeon-portrait" class:disc={loaded && !failed} data-pigeon={pigeon} data-variant={variant} aria-hidden="true">
  {#if selected && !failed}
    <img src={selected.url} width={selected.width} height={selected.height} alt="" draggable="false" style:object-position={`${selected.focus[0] * 100}% ${selected.focus[1] * 100}%`} class:loaded onload={() => loaded = true} onerror={() => { failed = true; onfailure?.() }} />
  {/if}
  <svg class:concealed={loaded && !failed} viewBox="0 0 120 120" fill="none">
    <circle cx="60" cy="60" r="57" fill="currentColor" opacity=".1" />
    <path d="M22 92c9-5 13-18 17-31 4-14 13-24 28-24 11 0 20 7 21 18 1 9-5 13-10 18-4 4-5 12-3 19H22Z" fill="currentColor" opacity=".8" />
    <path d="M38 76c13-9 27-7 34 5-7 6-20 9-34 5" stroke="var(--duif-paper)" stroke-width="2" opacity=".55" />
    <path d="m87 53 12 6-12 4" fill="currentColor" />
    <circle cx="77" cy="51" r="3" fill="var(--duif-paper)" />
    <circle cx="78" cy="51" r="1.5" fill="var(--duif-ink)" />
    <path d="m47 92-2 10m14-10 2 10M39 103h12m4 0h12" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
  </svg>
  <!-- De rand draagt de personakleur die het silhouet vroeger als vulling gaf:
       drie renders van hetzelfde model blijven zo op kleur uit elkaar te houden.
       Een SVG-cirkel in plaats van een border, zodat de ring meeschaalt van het
       portret van 37 px naast een chatbeurt tot dat van 144 px op de entree. -->
  {#if loaded && !failed}<svg class="rim" viewBox="0 0 100 100" fill="none"><circle cx="50" cy="50" r="48.5" stroke="currentColor" stroke-width="3" /></svg>{/if}
</div>

<style>
  .pigeon-portrait { color: var(--duif-persona-biological); width: 100%; aspect-ratio: 1; flex: none; position: relative; overflow: hidden; border-radius: 50%; }
  /* Onder de render een schijf in een lichte tint van de personakleur, zodat
     ook een donkere duif op 37 px van de kaart loskomt (--duif-portrait-disc,
     app.css). Alleen onder een geladen beeld: de getekende duif heeft haar eigen
     cirkel. */
  .pigeon-portrait.disc { background: var(--duif-portrait-disc); }
  [data-pigeon='individualist'] { color: var(--duif-persona-individualist); }
  [data-pigeon='nonverbal'] { color: var(--duif-persona-nonverbal); }
  img { position: absolute; inset: 0; width: 100%; height: 100%; object-fit: cover; opacity: 0; }
  img.loaded { opacity: 1; }
  .concealed { visibility: hidden; }
  svg { display: block; width: 100%; height: 100%; }
  .rim { position: absolute; inset: 0; z-index: 1; pointer-events: none; }
</style>
