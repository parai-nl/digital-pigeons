// De delen onder het gesprek: de videoband, onderzoek met posters en kaartjes,
// de methode en de voet. Alles wat later komt of wisselt (video, thesis,
// posters, kaartjes) is één regel in config.js; een poster zonder beeld toont
// een net kader met "binnenkort".
import { h } from './dom.js';
import { icon } from './icons.js';
import { CONFIG } from './config.js';
import { ORDER, FALLBACK_CREDITS } from './pigeons.js';
import { avatar } from './stage.js';
import { safeMedia } from './data.js';
import { darkMedia } from './theme.js';
import { fill } from './i18n.js';

// ---- videoband ----------------------------------------------------------------

export function renderVideo(root, copy, lang) {
  const video = readVideo(CONFIG.video);
  if (!video) {
    // Een stil, neutraal beeld zolang de uitlegvideo niet is goedgekeurd: de
    // drie koppen van het echte 3D-model, en "binnenkort". Geen afspeelknop:
    // er valt nog niets af te spelen.
    root.replaceChildren(h('div', { class: 'band__frame is-soon', role: 'img', 'aria-label': copy.video.soonLabel, 'data-video-soon': '' },
      h('span', { class: 'band__heads', 'aria-hidden': 'true' },
        ...ORDER.map((id) => avatar(id, { class: 'band__head', 'data-pigeon': id, width: 256, height: 256, loading: 'lazy' })),
      ),
      h('span', { class: 'band__soon', 'aria-hidden': 'true' }, copy.video.soon),
    ));
    return;
  }
  // Met een eigen maat in de config (width, height) volgt de band de
  // verhouding van de video, zoals de speler in de help; zonder blijft hij
  // 16:9. De verhouding gaat als eigenschap via de CSSOM, want een
  // style-attribuut verbiedt de CSP.
  const shape = video.ratio ? { 'data-ratio': video.ratio.label, style: { '--band-ratio': String(video.ratio.value) } } : {};
  const start = h('button', { type: 'button', class: 'band__frame', 'aria-label': copy.video.play, 'data-video-play': '', ...shape },
    video.poster ? h('img', { class: 'band__poster', src: video.poster, alt: '', loading: 'lazy', decoding: 'async' }) : null,
    h('span', { class: 'band__play', 'aria-hidden': 'true' }, icon('play', 24)),
  );
  start.addEventListener('click', () => {
    const player = h('video', { class: 'band__video', controls: true, playsinline: true, preload: 'auto', src: video.src, poster: video.poster, ...shape });
    // De ondertitels in de taal van de pagina, anders in de andere taal; taal en
    // label horen bij het bestand dat er werkelijk staat.
    const trackLang = video.vtt[lang] ? lang : ['nl', 'en'].find((l) => video.vtt[l]);
    if (trackLang) player.append(h('track', { kind: 'captions', src: video.vtt[trackLang], srclang: trackLang, label: trackLang === 'nl' ? 'Nederlands' : 'English', default: true }));
    root.replaceChildren(player);
    player.play().catch(() => { /* de bezoeker kan zelf op afspelen drukken */ });
    player.focus();
  });
  root.replaceChildren(start);
}

function readVideo(v) {
  if (!v || typeof v !== 'object') return null;
  const src = safeMedia(v.src);
  if (!src) return null;
  const vtt = typeof v.vtt === 'string' ? { nl: safeMedia(v.vtt), en: safeMedia(v.vtt) } : { nl: safeMedia(v.vtt?.nl), en: safeMedia(v.vtt?.en) };
  return { src, poster: safeMedia(v.poster), vtt, ratio: readRatio(v.width, v.height) };
}

// De verhouding uit width en height: hele, positieve pixels en een gewone
// beeldvorm (van staand 1:2 tot breed 21:9); anders geen, en blijft de band 16:9.
function readRatio(width, height) {
  if (!Number.isInteger(width) || !Number.isInteger(height) || width <= 0 || height <= 0) return null;
  const value = width / height;
  return value >= 0.5 && value <= 2.4 ? { value: +value.toFixed(4), label: `${width}:${height}` } : null;
}

// ---- onderzoek ----------------------------------------------------------------

export function renderResearch({ links, posters, cards }, copy, lang, viewer) {
  const r = copy.research;
  const l = r.links;
  const external = (href, text) => h('a', { class: 'rlink', href, target: '_blank', rel: 'noopener' }, text, icon('arrowUpRight', 16));
  links.replaceChildren(
    h('li', {}, h('span', { class: 'rlink is-soon', 'data-link': 'paper' }, l.paper)),
    h('li', {}, CONFIG.thesisUrl ? external(CONFIG.thesisUrl, l.thesis) : h('span', { class: 'rlink is-plain', 'data-link': 'thesis' }, l.thesis)),
    h('li', {}, CONFIG.aboutUrl ? external(CONFIG.aboutUrl, l.about) : h('span', { class: 'rlink is-plain' }, l.about)),
    // Het materiaal voor groepssessies zijn de ontworpen posters en kaartjes, als pdf.
    ...['posters', 'cards'].map((kind) => {
      const file = readDownload(CONFIG.downloads?.[kind]);
      if (!file) return null;
      const size = new Intl.NumberFormat(lang, { maximumFractionDigits: 1 }).format(file.bytes / 1e6);
      return h('li', {}, h('a', { class: 'rlink', href: file.src, download: file.name, 'data-download': kind },
        fill(l.download[kind], { size }), icon('download', 16)));
    }),
  );

  // Alleen de posters met beeld gaan de viewer in; een leeg kader blijft staan.
  const posterItems = [];
  posters.setAttribute('aria-label', r.postersLabel);
  posters.replaceChildren(...r.posters.map((p, i) => {
    const art = readArt(CONFIG.posters?.[i]);
    const caption = h('figcaption', {}, h('span', { class: 'poster__title' }, p.title), h('span', { class: 'poster__sub' }, p.sub));
    if (!art) {
      return h('figure', { class: 'poster' },
        h('span', { class: 'poster__frame', 'data-poster-soon': '' }, icon('image', 26), h('span', { class: 'poster__soon' }, r.soon)),
        caption);
    }
    const at = posterItems.push({ title: p.title, lines: [p.sub], sides: [{ src: art.src, alt: p.alt }], full: art.src }) - 1;
    return h('figure', { class: 'poster' },
      h('button', {
        type: 'button', class: 'poster__frame has-art', 'aria-haspopup': 'dialog', 'aria-label': fill(r.view, { title: p.title }), 'data-poster': i + 1,
        onclick: () => viewer.open(posterItems, at),
      }, h('img', { src: art.thumb, alt: '', loading: 'lazy', decoding: 'async', width: 510, height: 720 })),
      caption);
  }));

  // De kaartjes: in de rij de voorkant, in de viewer voor- en achterkant samen,
  // met de twee vragen erbij als tekst.
  const cardItems = [];
  cards.replaceChildren(...r.cards.flatMap((card, i) => {
    const entry = CONFIG.cards?.[i];
    const front = readArt(entry && { src: entry.front, thumb: entry.thumb });
    if (!front) return [];
    const back = safeMedia(entry.back);
    const landscape = entry.landscape === true;
    const sides = [{ src: front.src, alt: `${r.cardFront}: ${card.alt || card.front}`, label: r.cardFront }];
    if (back) sides.push({ src: back, alt: `${r.cardBack}: ${card.back}`, label: r.cardBack });
    const at = cardItems.push({ title: card.front, lines: back ? [card.back] : [], sides, landscape }) - 1;
    // Het nummer is de plek in de viewer, ook als er een kaartje zonder beeld wegvalt.
    const [w, hgt] = landscape ? [600, 423] : [423, 600];
    return [h('li', { class: `pcard${landscape ? ' is-landscape' : ''}` },
      h('button', {
        type: 'button', class: 'pcard__button', 'aria-haspopup': 'dialog', 'aria-label': fill(r.cardView, { n: at + 1, question: card.front }), 'data-card': at + 1,
        onclick: () => viewer.open(cardItems, at),
      }, h('img', { src: front.thumb, alt: '', loading: 'lazy', decoding: 'async', width: w, height: hgt })),
    )];
  }));
  cards.closest('[data-cards-block]').hidden = cardItems.length === 0;
}

// Een download uit de config: { src, bytes } met een pdf onder etalage/media/.
function readDownload(entry) {
  const src = safeMedia(entry?.src);
  if (!src || !src.endsWith('.pdf') || !(entry.bytes > 0)) return null;
  return { src, bytes: entry.bytes, name: src.split('/').pop() };
}

// Een beeld uit de config: één pad, of { src, thumb } met een kleiner beeld voor de rij.
function readArt(entry) {
  const src = safeMedia(typeof entry === 'string' ? entry : entry?.src);
  if (!src) return null;
  return { src, thumb: safeMedia(entry?.thumb) || src };
}

// ---- methode ------------------------------------------------------------------

export function renderMethod(body, copy, facts, lang) {
  const m = copy.method;
  const note = m.note(facts);
  // De graaf draagt labels in één taal: de Engelse pagina krijgt de -en-beelden.
  const suffix = lang === 'en' ? '-en' : '';
  const img = h('img', {
    class: 'method__graph-img',
    src: `etalage/media/beeld/kennisgraaf-licht${suffix}.webp`,
    alt: m.graphAlt,
    loading: 'lazy',
    decoding: 'async',
  });
  const figure = h('figure', { class: 'method__graph', 'data-method-graph': '' },
    h('picture', {},
      h('source', { srcset: `etalage/media/beeld/kennisgraaf-donker${suffix}.webp`, media: darkMedia(), 'data-dark': true }),
      img,
    ),
    h('figcaption', {}, m.graphCaption),
  );
  // Het beeld komt van de opnamestroom. Is het er (nog) niet, dan verdwijnt het
  // kader in plaats van een kapot beeld te tonen.
  img.addEventListener('error', () => figure.remove(), { once: true });
  body.replaceChildren(
    h('div', { class: 'method__text' },
      h('p', { class: 'method__intro' }, m.intro),
      h('dl', { class: 'method__list' }, ...m.items.flatMap((item) => [
        h('dt', { id: `methode-${item.id}` }, item.title),
        h('dd', {}, item.body(facts)),
      ])),
      note ? h('p', { class: 'method__note' }, note) : null,
    ),
    figure,
  );
}

// ---- voet ---------------------------------------------------------------------

// Elke naamsvermelding linkt naar haar bron; heeft zij een eigen licentielink,
// dan wordt de licentienaam die link (of komt hij erachter).
export function renderCredits(root, manifest, lang) {
  const credits = manifest?.credits?.length ? manifest.credits : FALLBACK_CREDITS;
  const link = (href, text) => h('a', { href, target: '_blank', rel: 'noopener' }, text);
  root.replaceChildren(...credits.map((c) => {
    const text = c[lang];
    const source = (part) => (c.url ? link(c.url, part) : part);
    if (!c.license || !c.licenseUrl) return h('li', {}, source(text));
    const at = text.lastIndexOf(c.license);
    if (at < 0) return h('li', {}, source(text), ' · ', link(c.licenseUrl, c.license));
    const before = text.slice(0, at).replace(/[\s·]+$/, '');
    return h('li', {}, source(before), ' · ', link(c.licenseUrl, c.license));
  }));
}
