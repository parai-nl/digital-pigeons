// Printmateriaal voor groepssessies: een A4-poster met de drie duiven en zes
// reflectiekaarten. ?kind=poster|cards en ?lang=nl|en; printen doet de bezoeker
// zelf. Dezelfde tekst en dezelfde portretten als de etalage.
import { COPY } from '../i18n.js';
import { h, $, $$ } from '../dom.js';
import { initialLang, rememberLang } from '../prefs.js';
import { ORDER, MODES, FALLBACK_CREDITS } from '../pigeons.js';
import { readManifest, safeMedia } from '../data.js';
import { renderCredits } from '../sections.js';

const params = new URLSearchParams(window.location.search);
let lang = initialLang();
let kind = params.get('kind') === 'cards' ? 'cards' : 'poster';

// Op papier staat de Kennispartner vooraan, net als op de etalage.
const PRINT_ORDER = ['kennispartner', ...ORDER.filter((id) => id !== 'kennispartner')];
const PORTRAIT = (id) => `../media/portretten/${id}.webp`;

// De koppen komen uit het manifest van de beeldstroom, net als in de videoband;
// laadt dat niet, dan staat het kaartportret er.
const heads = {};
// De naamsvermelding van de 3D-duif, met dezelfde tekst als in de voet van de
// etalage: koppen en portretten zijn beelden van het 3D-model. De platen staan
// niet op papier, dus hun vermelding ook niet; de reflectiekaarten hebben geen beeld.
let credits = FALLBACK_CREDITS;
async function loadHeads() {
  try {
    const res = await fetch('../media/duiven/manifest.json', { cache: 'no-cache' });
    const manifest = await res.json();
    for (const id of ORDER) {
      const src = safeMedia(manifest?.pigeons?.[id]?.avatar?.src);
      if (src) heads[id] = `../../${src}`;
    }
    const model = readManifest(manifest).credits.filter((c) => c.url === FALLBACK_CREDITS[0].url);
    if (model.length) credits = model;
  } catch { /* de kaartportretten en hun vermelding blijven staan */ }
}

function render() {
  const c = COPY[lang];
  const p = c.print;
  document.documentElement.lang = lang;
  document.title = `${p.title} · ${p.kinds[kind]}`;
  $('[data-back]').textContent = `← ${p.back}`;
  $('[data-print]').textContent = p.print;
  $('[data-controls]').setAttribute('aria-label', p.print);
  $('[data-kind-group]').setAttribute('aria-label', p.kindLabel);
  for (const b of $$('[data-kind]')) {
    b.textContent = p.kinds[b.dataset.kind];
    b.setAttribute('aria-pressed', String(b.dataset.kind === kind));
  }
  for (const b of $$('[data-lang]')) b.setAttribute('aria-pressed', String(b.dataset.lang === lang));

  const header = h('header', { class: 'print-header' },
    h('p', { class: 'print-eyebrow' }, 'AI-MAPS × Parai'),
    h('h1', {}, p.title),
    h('p', { class: 'print-intro' }, p.intro),
  );
  const body = kind === 'poster'
    ? h('div', { class: 'print-pigeons' }, ...PRINT_ORDER.map((id) => {
      const pigeon = c.pigeons[id];
      return h('section', { class: `print-pigeon print-pigeon--${id}` },
        heads[id]
          ? h('img', { class: 'is-head', src: heads[id], alt: '', width: 256, height: 256 })
          : h('img', { src: PORTRAIT(id), alt: '', width: 512, height: 512 }),
        h('div', {},
          h('p', { class: 'print-label' }, pigeon.label),
          h('h2', {}, pigeon.name),
          h('p', {}, pigeon.blurb),
          h('p', { class: 'print-mode' }, MODES[id].includes('chat') ? p.modes : p.modesSignal),
        ),
      );
    }), h('p', { class: 'print-disclosure' }, p.demoNote))
    : h('div', { class: 'print-cards' }, ...p.cards.map((card, i) => h('section', { class: 'print-card' },
      h('p', { class: 'print-card__number' }, `0${i + 1} · ${p.title}`),
      h('h2', {}, card.title),
      h('p', { class: 'print-card__action' }, card.action),
      h('p', { class: 'print-card__question' }, card.question),
      h('div', { class: 'print-lines', 'aria-hidden': 'true' }, h('span'), h('span'), h('span')),
    )));
  let credit = null;
  if (kind === 'poster') {
    const list = h('ul', { class: 'print-credits__list' });
    renderCredits(list, { credits }, lang);
    credit = h('div', { class: 'print-credits', 'data-print-credits': '' }, h('p', { class: 'print-credits__title' }, c.footer.images), list);
  }
  const footer = h('footer', { class: 'print-footer' },
    h('p', {}, h('strong', {}, p.working)),
    h('p', {}, p.note),
    credit,
  );
  $('[data-sheet]').className = `print-sheet print-sheet--${kind}`;
  $('[data-sheet]').replaceChildren(header, body, footer);
}

function syncUrl() {
  const url = new URL(window.location.href);
  url.searchParams.set('lang', lang);
  url.searchParams.set('kind', kind);
  window.history.replaceState(null, '', url);
}

$('[data-print]').addEventListener('click', () => window.print());
for (const b of $$('[data-lang]')) {
  b.addEventListener('click', () => {
    lang = b.dataset.lang;
    rememberLang(lang);
    syncUrl();
    render();
  });
}
for (const b of $$('[data-kind]')) {
  b.addEventListener('click', () => {
    kind = b.dataset.kind;
    syncUrl();
    render();
  });
}
render();
loadHeads().then(() => { if (Object.keys(heads).length) render(); });
