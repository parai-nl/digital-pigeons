// Lijniconen, eigen tekening op een raster van 24. Altijd decoratief
// (aria-hidden): de knop of het label eromheen draagt de naam.
const PATHS = {
  chat: '<path d="M4 5.5h11a2 2 0 0 1 2 2v6a2 2 0 0 1-2 2H9l-4 3v-3H4a2 2 0 0 1-2-2v-6a2 2 0 0 1 2-2Z"/><path d="M17 9h3a2 2 0 0 1 2 2v5.5a2 2 0 0 1-2 2h-.5v2.5l-3.2-2.5H12"/>',
  listen: '<path d="M7 10a5 5 0 0 1 10 0c0 2.6-1.6 3.6-2.6 4.6-.9.9-1 2-1.4 3.2A2.8 2.8 0 0 1 8 17.5"/><path d="M10 10a2 2 0 0 1 4 0"/><path d="M19.5 6.5a8.5 8.5 0 0 1 0 7"/><path d="M4.5 6.5a8.5 8.5 0 0 0 0 7"/>',
  lock: '<rect x="5" y="10.5" width="14" height="10" rx="2"/><path d="M8 10.5V7.5a4 4 0 0 1 8 0v3"/>',
  arrowLeft: '<path d="M14.5 6 8.5 12l6 6"/>',
  arrowRight: '<path d="m9.5 6 6 6-6 6"/>',
  arrowUpRight: '<path d="M8 16 16 8"/><path d="M9.5 8H16v6.5"/>',
  download: '<path d="M12 4.5v10"/><path d="m7.5 10.5 4.5 4.5 4.5-4.5"/><path d="M5 19.5h14"/>',
  arrowBend: '<path d="M4 18v-3.5A5.5 5.5 0 0 1 9.5 9H20"/><path d="m15.5 4.5 4.5 4.5-4.5 4.5"/>',
  send: '<path d="M5 12h13"/><path d="m12.5 6 6 6-6 6"/>',
  book: '<path d="M12 7c-1.6-1.5-4.2-2-8-2v12.5c3.8 0 6.4.5 8 2 1.6-1.5 4.2-2 8-2V5c-3.8 0-6.4.5-8 2Z"/><path d="M12 7v12.5"/>',
  compass: '<circle cx="12" cy="12" r="8.5"/><path d="m15.2 8.8-2 4.4-4.4 2 2-4.4 4.4-2Z"/>',
  close: '<path d="M6.5 6.5l11 11M17.5 6.5l-11 11"/>',
  plus: '<path d="M12 5.5v13M5.5 12h13"/>',
  soundOn: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4Z"/><path d="M15.5 9a4 4 0 0 1 0 6"/><path d="M18 6.5a7.5 7.5 0 0 1 0 11"/>',
  soundOff: '<path d="M4 9.5h3.5L12 5.5v13l-4.5-4H4Z"/><path d="m16 9.5 5 5M21 9.5l-5 5"/>',
  play: '<path d="M8 5.5v13l10.5-6.5Z"/>',
  pause: '<path d="M9 6.5v11M15 6.5v11"/>',
  stop: '<rect x="7" y="7" width="10" height="10" rx="1.5"/>',
  signal: '<circle cx="12" cy="12" r="2.2"/><path d="M8.2 8.2a5.4 5.4 0 0 0 0 7.6M15.8 8.2a5.4 5.4 0 0 1 0 7.6"/><path d="M5.4 5.4a9.3 9.3 0 0 0 0 13.2M18.6 5.4a9.3 9.3 0 0 1 0 13.2"/>',
  image: '<rect x="3.5" y="5" width="17" height="14" rx="2"/><circle cx="9" cy="10" r="1.6"/><path d="m4 17 5-4.5 3.5 3 3-2.5 4.5 4"/>',
  restart: '<path d="M5 12a7 7 0 1 0 2.1-5"/><path d="M5 4.5V9h4.5"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>',
  moon: '<path d="M19.5 14.2A7.5 7.5 0 0 1 9.8 4.5a7.5 7.5 0 1 0 9.7 9.7Z"/>',
};

export function icon(name, size = 18) {
  const span = document.createElement('span');
  span.className = `icon icon--${name}`;
  span.setAttribute('aria-hidden', 'true');
  const fill = name === 'play' ? 'currentColor' : 'none';
  span.innerHTML = `<svg viewBox="0 0 24 24" width="${size}" height="${size}" fill="${fill}" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" focusable="false">${PATHS[name] || ''}</svg>`;
  return span;
}
