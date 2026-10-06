// Licht of donker. theme-early.js zette de kleur al vóór de eerste verf; dit
// deel houdt haar bij: de schakelaar in de kop, de beelden met een donkere
// variant, en de wissels van buiten (het systeem, of de werkomgeving in een
// ander tabblad).
import { THEME_KEY, OWN_THEME_KEY, storedTheme, rememberTheme } from './prefs.js';
import { $, $$ } from './dom.js';
import { icon } from './icons.js';

const system = window.matchMedia('(prefers-color-scheme: dark)');
let choice = storedTheme();
let copy = null;

const isDark = () => document.documentElement.dataset.theme === 'dark';

// Een <source media> ziet data-theme niet, dus de pagina schakelt de donkere
// bronnen zelf. "screen" houdt de afdruk licht, net als de kleuren in tokens.css.
export const darkMedia = () => (isDark() ? 'screen' : 'not all');

function apply() {
  document.documentElement.dataset.theme = choice || (system.matches ? 'dark' : 'light');
  for (const source of $$('source[data-dark]')) source.media = darkMedia();
  render();
}

// Stand en icoon staan er meteen; de naam volgt met de taal (tot dan staat de
// Nederlandse uit index.html er).
function render() {
  const button = $('[data-theme-switch]');
  const dark = isDark();
  button.setAttribute('aria-checked', String(dark));
  button.replaceChildren(icon(dark ? 'moon' : 'sun', 16));
  if (!copy) return;
  button.setAttribute('aria-label', copy.header.theme);
  button.title = copy.header.theme;
}

export function initTheme() {
  const button = $('[data-theme-switch]');
  // Zonder script doet de schakelaar niets; daarom staat hij pas nu in beeld.
  button.hidden = false;
  // Een klik is een uitgesproken keuze; die geldt daarna ook in de werkomgeving.
  button.addEventListener('click', () => {
    choice = isDark() ? 'light' : 'dark';
    rememberTheme(choice);
    apply();
  });
  // Zonder eigen keuze loopt de pagina mee met het systeem.
  system.addEventListener('change', () => { if (!choice) apply(); });
  // Een keuze in een ander tabblad, hier of in de werkomgeving (key null: de
  // opslag is daar leeggemaakt).
  window.addEventListener('storage', (event) => {
    if (event.key !== null && event.key !== THEME_KEY && event.key !== OWN_THEME_KEY) return;
    choice = storedTheme();
    apply();
  });
  apply();
}

export function setThemeCopy(c) {
  copy = c;
  render();
}
