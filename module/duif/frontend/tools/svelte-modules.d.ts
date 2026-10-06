// Alleen voor tsconfig.unused.json: die pas kijkt naar de TypeScript-laag en
// laat de .svelte-bestanden aan svelte-check. Zonder deze verklaring zou een
// `import App from './App.svelte'` de pas al bij het oplossen laten stranden.
declare module '*.svelte' {
  // eslint-disable-next-line
  const component: never
  export default component
}
