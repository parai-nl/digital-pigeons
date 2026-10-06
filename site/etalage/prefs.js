// De pagina onthoudt drie keuzes en verder niets: taal, geluid en licht of
// donker. Elke lees- en schrijfactie mag mislukken (privévenster, geblokkeerde
// opslag); de pagina werkt dan gewoon met de standaard.
export const LANG_KEY = 'duif_etalage_lang';
export const SOUND_KEY = 'duif_etalage_geluid';
// Licht of donker deelt de pagina met de werkomgeving op /duif, op dezelfde
// oorsprong: de parai-core-schil bewaart die keuze via mode-watcher onder deze
// sleutel ('light', 'dark' of 'system'). Wie daar donker koos, krijgt hier
// donker, en andersom.
export const THEME_KEY = 'mode-watcher-mode';
// De keuze zoals ze hier gemaakt is. Nodig omdat de werkomgeving haar standaard
// ('light') al bij het eerste bezoek onder THEME_KEY schrijft, ook als niemand
// iets koos; alleen met deze sleutel ernaast weet de pagina of 'light' een keuze is.
export const OWN_THEME_KEY = 'duif_etalage_thema';

function read(key) {
  try { return window.localStorage.getItem(key); } catch { return null; }
}
function write(key, value) {
  try { window.localStorage.setItem(key, value); } catch { /* opslag geblokkeerd */ }
}

const isLang = (value) => value === 'nl' || value === 'en';

// ?lang= forceert; daarna een eerder gemaakte keuze; anders de browser:
// Nederlands wordt NL, al het andere EN.
export function initialLang() {
  const forced = new URLSearchParams(window.location.search).get('lang');
  if (isLang(forced)) return forced;
  const stored = read(LANG_KEY);
  if (isLang(stored)) return stored;
  const first = (navigator.languages && navigator.languages[0]) || navigator.language || '';
  return /^nl\b/i.test(first) ? 'nl' : 'en';
}

export function rememberLang(lang) {
  write(LANG_KEY, lang);
  // Een ?lang= in de adresbalk zou de keuze bij herladen overrulen.
  const url = new URL(window.location.href);
  if (url.searchParams.has('lang')) {
    url.searchParams.set('lang', lang);
    window.history.replaceState(window.history.state, '', url);
  }
}

// Geluid staat standaard aan, maar speelt alleen na een klik in de vergadering.
export function soundOn() {
  return read(SOUND_KEY) !== 'uit';
}

export function rememberSound(on) {
  write(SOUND_KEY, on ? 'aan' : 'uit');
}

// Een uitgesproken keuze, of null: dan volgt de pagina het systeem.
// - 'dark' onder THEME_KEY is altijd een keuze: uit zichzelf schrijft de
//   werkomgeving 'light'.
// - 'light' telt pas als er hier ooit gekozen is. Staat hier 'dark' en daar
//   'light', dan is daarna in de werkomgeving licht gekozen.
// - Al het andere ('system', leeg, onzin) is geen keuze.
// theme-early.js past vóór de eerste verf dezelfde regel toe.
export function storedTheme() {
  const shared = read(THEME_KEY);
  if (shared === 'dark') return 'dark';
  if (shared === 'light' && read(OWN_THEME_KEY)) return 'light';
  return null;
}

export function rememberTheme(theme) {
  write(OWN_THEME_KEY, theme);
  write(THEME_KEY, theme);
}
