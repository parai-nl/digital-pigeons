import '@testing-library/jest-dom/vitest'
import { afterAll, vi } from 'vitest'

// jsdom heeft geen canvas, ResizeObserver of Web Animations; componenten uit
// @parai/ui (MessageList, VoiceOrb, Dialog) roepen ze aan. Browser-API's die de
// vergadermodus zelf nodig heeft (AudioContext, EventSource, getUserMedia,
// Audio) worden nooit globaal gestubd: die gaan via de ports-seam met fakes.
HTMLCanvasElement.prototype.getContext = (() => null) as unknown as typeof HTMLCanvasElement.prototype.getContext

// jsdom kent matchMedia niet; VoiceOrb vraagt er in onMount naar om
// prefers-reduced-motion te respecteren.
if (typeof window.matchMedia !== 'function') {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia
}

globalThis.ResizeObserver = class {
  observe() {}
  unobserve() {}
  disconnect() {}
}

Element.prototype.animate = (() => ({
  cancel() {},
  finish() {},
  finished: Promise.resolve(),
})) as unknown as typeof Element.prototype.animate

if (!Element.prototype.scrollTo) {
  Element.prototype.scrollTo = () => {}
}

// Een gesloten bits-ui-Dialog zet <body> pas 24 ms later terug (de scroll-lock
// wacht even op een eventuele heropening). Eindigt een testbestand binnen dat
// venster, dan vuurt die timer nadat jsdom al is opgeruimd en gooit hij
// "document is not defined": alle tests groen, de run toch rood. Op een
// drukke machine gebeurt dat steeds (meetingDialogs.test.ts). Eén korte
// wachttijd per bestand, met echte timers, laat de reset nog in jsdom landen.
afterAll(async () => {
  vi.useRealTimers()
  await new Promise((resolve) => setTimeout(resolve, 40))
})
