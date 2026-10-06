<script lang="ts">
  // Hetzelfde teken als op de kaart, maar dan buiten het canvas. De boom en het
  // leespaneel tekenen geen plattegrond, dus daar kan de tekenaar niet heen —
  // en de iconenfamilie van parai-graph bestaat alleen als canvaspaden, niet
  // als losse componenten. Elk teken krijgt daarom zijn eigen doekje van
  // zestien bij zestien: nul bytes extra in de bundel, en een thema draagt in
  // de boom, in het paneel en op de kaart gegarandeerd hetzelfde glyph — het is
  // letterlijk dezelfde tekening.
  //
  // Waarom een canvas en geen plaatje: een tekening omzetten naar een bestand,
  // byte of pixel is precies wat deze module nergens doet: zij bewaart niets. Er wordt
  // dus getekend en verder niets.
  //
  // Het is decoratie naast een naam die er al staat: het doekje is leeg voor
  // een schermlezer, en de soort van een knoop staat er in woorden naast.
  import type { GraphPorts } from './ports'

  const {
    ports,
    icon,
    ink,
    size = 15,
  }: { ports: GraphPorts; icon: string; ink: string; size?: number } = $props()

  let canvas: HTMLCanvasElement | undefined = $state()

  $effect(() => {
    const el = canvas
    if (el) ports.drawGlyph(el, icon, ink, size)
  })
</script>

<canvas
  bind:this={canvas}
  class="node-glyph"
  style="width:{size}px; height:{size}px"
  aria-hidden="true"
  data-glyph={icon}
></canvas>

<style>
  .node-glyph { display: block; }
</style>
