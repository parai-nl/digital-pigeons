// Licht of donker, vóór de eerste verf. Dit is bewust een gewoon, blokkerend
// script in de <head>: een module draait pas na het parsen, en dan flitst eerst
// de verkeerde kleur. De CSP staat geen inline script toe, vandaar een eigen
// bestand.
//
// Dezelfde regel als storedTheme() in prefs.js, met dezelfde sleutels: 'dark'
// van de werkomgeving is altijd een keuze, 'light' alleen als er hier ooit
// gekozen is (de werkomgeving schrijft 'light' ook uit zichzelf). Zonder keuze
// volgt de pagina het systeem. theme.js doet de rest: de schakelaar, de beelden
// en de wissels van buiten.
(function () {
  var shared = null;
  var own = null;
  try {
    shared = window.localStorage.getItem('mode-watcher-mode');
    own = window.localStorage.getItem('duif_etalage_thema');
  } catch (e) { /* opslag geblokkeerd */ }
  var choice = shared === 'dark' ? 'dark' : shared === 'light' && own ? 'light' : null;
  var dark = choice ? choice === 'dark' : window.matchMedia('(prefers-color-scheme: dark)').matches;
  document.documentElement.dataset.theme = dark ? 'dark' : 'light';
})();
