<script lang="ts">
  // DEV-ONLY: imiteert de shell-chrome van parai-core rond de module, zodat
  // standalone dev op de in-shell-ervaring lijkt. Wordt nooit meegebundeld:
  // alleen dev.ts importeert dit bestand. De tokens komen uit dev-theme.css;
  // in de echte shell levert core ze. De dark-toggle is bewust alleen in
  // geheugen: de module raakt geen enkele opslag-API aan.
  import './dev-theme.css'
  import { mountModule } from './index'
  import type { Lang } from './lib/i18n.svelte'
  import { storedLanguage, storeLanguage } from './lib/shell/languagePreference'

  let dark = $state(false)
  // De taal staat in de schil, niet in de module: de echte shell houdt
  // <html lang> gelijk aan haar taal en de module leest dat af. Hier
  // is de bewaarde voorkeur de bron van diezelfde ene regel.
  let lang = $state<Lang>(storedLanguage() ?? 'en')
  let host: HTMLElement | undefined = $state()

  $effect(() => {
    document.documentElement.classList.toggle('dark', dark)
  })

  // Vóór het mount-effect, zodat de module bij haar eerste beurt al de taal van
  // deze schil ziet in plaats van die een tel later te moeten inhalen.
  $effect(() => {
    document.documentElement.lang = lang
  })

  $effect(() => {
    if (!host) return
    const mounted = mountModule(host, { mode: 'standalone', user: { role: 'admin' } })
    return () => mounted.unmount()
  })

  function chooseLang(value: string): void {
    lang = value === 'nl' ? 'nl' : 'en'
    storeLanguage(lang)
  }
</script>

<div class="duif-devshell">
  <header class="duif-devshell-bar">
    <span class="duif-devshell-brand">parai · dev</span>
    <span class="duif-devshell-title">Digital Pigeons</span>
    <div class="duif-devshell-controls">
      <label>
        <span class="duif-devshell-label">language</span>
        <select value={lang} onchange={(event) => chooseLang(event.currentTarget.value)}>
          <option value="en">EN</option>
          <option value="nl">NL</option>
        </select>
      </label>
      <button type="button" onclick={() => (dark = !dark)}>{dark ? 'light' : 'dark'}</button>
    </div>
  </header>
  <div class="duif-devshell-body" bind:this={host}></div>
</div>

<style>
  .duif-devshell {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: var(--background);
    color: var(--foreground);
    font-family: var(--font-sans, system-ui, sans-serif);
  }
  .duif-devshell-bar {
    display: grid;
    grid-template-columns: 1fr auto 1fr;
    align-items: center;
    height: 3.5rem;
    padding: 0 1rem;
    border-bottom: 1px solid var(--border);
    background: var(--card);
  }
  .duif-devshell-brand { font-weight: 600; }
  .duif-devshell-title { font-size: 0.875rem; font-weight: 600; }
  .duif-devshell-controls { justify-self: end; display: flex; align-items: center; gap: 0.5rem; }
  .duif-devshell-controls label { display: flex; align-items: center; gap: 0.35rem; }
  .duif-devshell-label { font-size: 0.75rem; color: var(--muted-foreground, inherit); }
  .duif-devshell-bar select,
  .duif-devshell-bar button {
    font: inherit;
    font-size: 0.8125rem;
    padding: 0.25rem 0.75rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--secondary);
    color: var(--secondary-foreground);
    cursor: pointer;
  }
  .duif-devshell-body { flex: 1; min-height: 0; }
</style>
