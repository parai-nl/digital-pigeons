// De etalage van Digitale Duif van Rotterdam. Een statische pagina zonder
// bouwstap: ze speelt echte, vooraf opgenomen modeloutput af en doet zelf geen
// modelaanroepen, gebruikt geen microfoon en haalt niets van een andere
// oorsprong. Ze onthoudt alleen de taalkeuze, de geluidskeuze en licht of
// donker (dat laatste gedeeld met de werkomgeving op /duif).
import { COPY, formatFacts, formatMonth } from './i18n.js';
import { initialLang, rememberLang, soundOn, rememberSound } from './prefs.js';
import { loadManifest, loadOpnames } from './data.js';
import { setManifest } from './stage.js';
import { createCarousel } from './carousel.js';
import { createChat } from './chat.js';
import { createMeeting } from './meeting.js';
import { setSourcesCopy } from './sources.js';
import { createFaq } from './faq.js';
import { createViewer } from './viewer.js';
import { renderVideo, renderResearch, renderMethod, renderCredits } from './sections.js';
import { initTheme, setThemeCopy } from './theme.js';
import { icon } from './icons.js';
import { $, $$, prefersReducedMotion } from './dom.js';
import { MODES, START } from './pigeons.js';
import { CONFIG } from './config.js';

const state = {
  lang: initialLang(),
  pigeon: START,
  mode: 'chat',
  talkMode: 'chat',
  data: null,
  dataStatus: 'laden',
  manifest: null,
};

let carousel;
let chat;
let meeting;
let faq;
let viewer;

const copy = () => COPY[state.lang];

// Kop, hero en waaier staan er zodra het (kleine) manifest binnen is; het
// gesprek, de Data-regel en de vergadering vullen zich als de opnames er zijn en
// tonen tot dan een nette laadtoestand.
async function boot() {
  initTheme();
  const opnamesOnderweg = loadOpnames();
  const manifest = await loadManifest();
  state.manifest = manifest;
  state.dataStatus = 'laden';
  setManifest(manifest);
  document.body.dataset.beeld = manifest ? 'manifest' : 'portretten';
  document.body.dataset.opnames = 'laden';

  carousel = createCarousel($('[data-carousel]'), {
    onChange: (id) => setPigeon(id),
    onPickActive: () => focusPanel(),
    // Zolang een antwoord loopt of een stem klinkt, wisselt de waaier niet vanzelf.
    busy: () => chat.busy() || meeting.busy(),
  });
  chat = createChat($('[data-chat-root]'), { requestPigeon: (id) => carousel.setActive(id) });
  meeting = createMeeting($('[data-meeting-root]'), {
    sound: { on: soundOn, set: rememberSound },
  });
  chat.setData(null, 'laden');
  meeting.setData(null, 'laden');
  faq = createFaq({ context: faqContext });
  viewer = createViewer();

  wireControls();
  wireDigiPigie();
  trackTopbar();
  applyLang(state.lang);
  setPigeon(START);
  openMethodFromHash();
  window.addEventListener('hashchange', openMethodFromHash);
  document.documentElement.classList.remove('is-loading');
  document.body.dataset.shell = 'true';

  const opnames = await opnamesOnderweg;
  state.data = opnames;
  state.dataStatus = opnames ? 'klaar' : 'ontbreekt';
  document.body.dataset.opnames = opnames ? (opnames.recording.placeholder ? 'plaatshouder' : 'geladen') : 'ontbreekt';
  chat.setData(opnames, state.dataStatus);
  meeting.setData(opnames, state.dataStatus);
  // Getallen, eerlijke regel, methode en FAQ hangen af van de opnames.
  renderTalk();
  renderHonest();
  renderMethod($('[data-method-body]'), copy(), facts(), state.lang);
  document.body.dataset.ready = 'true';
}

// ---- taal -----------------------------------------------------------------------

function applyLang(lang) {
  state.lang = lang;
  const c = copy();
  document.documentElement.lang = lang;
  document.title = c.meta.title;
  $('meta[name="description"]')?.setAttribute('content', c.meta.description);

  for (const el of $$('[data-t]')) {
    const value = el.dataset.t.split('.').reduce((obj, key) => obj?.[key], c);
    if (typeof value === 'string') el.textContent = value;
  }
  for (const button of $$('[data-lang]')) button.setAttribute('aria-pressed', String(button.dataset.lang === lang));
  $('[data-lang-group]').setAttribute('aria-label', c.header.lang);
  const login = $('[data-login]');
  login.href = CONFIG.loginUrl;
  login.title = c.header.loginTitle;
  for (const brands of $$('[data-brands]')) brands.setAttribute('aria-label', c.header.brands);
  setThemeCopy(c);
  $('[data-faq-open]').setAttribute('aria-label', c.hero.faq);
  $('[data-tabs]').setAttribute('aria-label', c.modes.label);
  $('#tab-chat').textContent = c.modes.chat;
  $('#tab-meeting').textContent = c.modes.meeting;

  carousel.render(c);
  setSourcesCopy(c);
  chat.setCopy(c, lang);
  meeting.setCopy(c, lang);
  faq.setCopy(c);
  viewer.setCopy(c);
  renderTalk();
  renderHonest();
  renderVideo($('[data-video]'), c, lang);
  renderResearch({ links: $('[data-research-links]'), posters: $('[data-posters]'), cards: $('[data-cards]') }, c, lang, viewer);
  renderMethod($('[data-method-body]'), c, facts(), lang);
  renderCredits($('[data-credits]'), state.manifest, lang);
}

function facts() {
  return formatFacts(state.lang, state.data?.facts);
}

function faqContext() {
  const r = state.data?.recording || {};
  return {
    facts: facts(),
    recording: {
      month: r.recordedFrom ? formatMonth(state.lang, r.recordedFrom) : '',
    },
  };
}

// ---- duif en modus --------------------------------------------------------------

function setPigeon(id) {
  state.pigeon = id;
  const talking = MODES[id].includes('chat');
  state.mode = talking ? state.talkMode : 'meeting';
  renderTalk();
  if (talking) chat.show(id);
  meeting.show(id);
  renderMode();
}

function setMode(mode, { focus = false } = {}) {
  if (!MODES[state.pigeon].includes(mode)) return;
  state.mode = mode;
  if (MODES[state.pigeon].includes('chat')) state.talkMode = mode;
  renderMode();
  if (focus) $(`#tab-${mode}`).focus();
}

function renderMode() {
  const modes = MODES[state.pigeon];
  for (const tab of $$('[data-tab]')) {
    const mode = tab.dataset.tab;
    const selected = mode === state.mode;
    tab.hidden = !modes.includes(mode);
    tab.setAttribute('aria-selected', String(selected));
    tab.tabIndex = selected ? 0 : -1;
  }
  const chatPanel = $('#panel-chat');
  const meetingPanel = $('#panel-meeting');
  const showChat = state.mode === 'chat';
  if (showChat && chatPanel.hidden) meeting.hide();
  if (!showChat && !chatPanel.hidden) chat.hide();
  chatPanel.hidden = !showChat;
  meetingPanel.hidden = showChat;
}

function renderTalk() {
  const c = copy();
  const p = c.pigeons[state.pigeon];
  const talk = $('[data-talk]');
  talk.dataset.pigeon = state.pigeon;
  $('[data-talk-icon]').replaceChildren(icon(MODES[state.pigeon].includes('chat') ? 'chat' : 'listen', 15));
  $('[data-talk-chip]').textContent = p.chip;
  $('[data-talk-name]').textContent = p.name;
  $('[data-talk-blurb]').textContent = p.blurb;
}

// Eén kleine, eerlijke regel onder het paneel, zonder banner.
function renderHonest() {
  const c = copy();
  const line = $('[data-honest]');
  const r = state.data?.recording;
  line.hidden = !state.data;
  if (!r) return;
  const month = r.recordedFrom ? formatMonth(state.lang, r.recordedFrom) : '';
  $('[data-honest-text]').textContent = month ? `${c.honest.recorded} · ${month}` : c.honest.recorded;
  $('[data-honest-how]').textContent = c.honest.how;
}

function focusPanel() {
  const target = $(`#tab-${state.mode}`);
  $('#gesprek').scrollIntoView({ behavior: prefersReducedMotion() ? 'auto' : 'smooth', block: 'start' });
  target.focus({ preventScroll: true });
}

// ---- bediening ------------------------------------------------------------------

function wireControls() {
  for (const button of $$('[data-lang]')) {
    button.addEventListener('click', () => {
      const lang = button.dataset.lang;
      if (lang === state.lang) return;
      rememberLang(lang);
      applyLang(lang);
      setPigeon(state.pigeon);
    });
  }
  $('[data-faq-open]').addEventListener('click', () => faq.open());
  $('[data-honest-how]').addEventListener('click', () => faq.open('echt'));

  for (const tab of $$('[data-tab]')) {
    tab.addEventListener('click', () => setMode(tab.dataset.tab));
  }
  $('[data-tabs]').addEventListener('keydown', (event) => {
    const visible = $$('[data-tab]').filter((t) => !t.hidden).map((t) => t.dataset.tab);
    const at = visible.indexOf(state.mode);
    let next = null;
    if (event.key === 'ArrowRight') next = visible[(at + 1) % visible.length];
    else if (event.key === 'ArrowLeft') next = visible[(at - 1 + visible.length) % visible.length];
    else if (event.key === 'Home') next = visible[0];
    else if (event.key === 'End') next = visible[visible.length - 1];
    if (!next) return;
    event.preventDefault();
    setMode(next, { focus: true });
  });

  $('[data-method]').addEventListener('toggle', (event) => {
    $('[data-method-plus]').replaceChildren(icon('plus', 20));
    if (!event.target.open && window.location.hash === '#methode') {
      history.replaceState(history.state, '', window.location.pathname + window.location.search);
    }
  });
  $('[data-method-plus]').replaceChildren(icon('plus', 20));

  // Op papier staat de methode open; daarna weer zoals de bezoeker haar had.
  let methodWasOpen = false;
  window.addEventListener('beforeprint', () => {
    const details = $('[data-method]');
    methodWasOpen = details.open;
    details.open = true;
  });
  window.addEventListener('afterprint', () => {
    $('[data-method]').open = methodWasOpen;
  });
}

// Vijf keer snel op het AI-MAPS-logo opent DigiPigie, een knipoog (zie
// digipigie.js). Dat kan in de kop en in de voet: op een telefoon toont de kop
// het logo niet. Het logo blijft een plaatje, geen knop; wie het niet weet,
// merkt er niets van. Pas bij de vijfde klik laadt de pagina iets.
const DIGIPIGIE_TAPS = 5;
const DIGIPIGIE_GAP_MS = 1000;

function wireDigiPigie() {
  let taps = 0;
  let lastTap = 0;
  for (const logo of $$('[data-brands] .brands__aimaps')) {
    // Snel tikken zoomt niet in en selecteert geen tekst naast het logo.
    logo.style.setProperty('touch-action', 'manipulation');
    logo.addEventListener('mousedown', (event) => {
      if (event.detail > 1) event.preventDefault();
    });
    logo.addEventListener('click', () => {
      const now = performance.now();
      taps = now - lastTap <= DIGIPIGIE_GAP_MS ? taps + 1 : 1;
      lastTap = now;
      if (taps < DIGIPIGIE_TAPS) return;
      taps = 0;
      import('./digipigie.js')
        .then(({ openDigiPigie }) => openDigiPigie(copy()))
        .catch((err) => console.error(err));
    });
  }
}

// De onderkant van de meelopende kop, als --kop op de pagina: de tafelstrook in
// de vergadering sluit daar direct op aan. Loopt de kop niet mee (laag venster),
// dan is --kop 0.
function trackTopbar() {
  const bar = $('.topbar');
  const measure = () => {
    const style = getComputedStyle(bar);
    const bottom = style.position === 'sticky' ? parseFloat(style.top) + bar.getBoundingClientRect().height : 0;
    document.documentElement.style.setProperty('--kop', `${Math.ceil(bottom)}px`);
  };
  measure();
  new ResizeObserver(measure).observe(bar);
  window.addEventListener('resize', measure);
}

// #methode opent het methodeblok en scrolt ernaartoe; de help van de module
// verwijst hierheen.
function openMethodFromHash() {
  if (window.location.hash !== '#methode') return;
  const details = $('[data-method]');
  details.open = true;
  requestAnimationFrame(() => details.scrollIntoView({ block: 'start' }));
}

boot().catch((err) => {
  console.error(err);
  document.documentElement.classList.remove('is-loading');
});
