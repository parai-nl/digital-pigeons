// Groot bekijken: een poster, of een kaartje met voor- en achterkant naast
// elkaar, in een dialoog. Vorige en volgende blijven binnen de reeks waaruit de
// bezoeker koos (de posters of de kaartjes), ook met de pijltjestoetsen.
//
// Het venster wordt één keer gebouwd en bij het bladeren alleen gevuld: de
// knoppen blijven staan, dus de focus blijft waar de bezoeker hem had.
import { h } from './dom.js';
import { icon } from './icons.js';
import { createDialog } from './dialog.js';
import { fill } from './i18n.js';

export function createViewer() {
  const dialog = createDialog({ className: 'dialog--viewer', labelledBy: 'viewer-title' });
  const title = h('h2', { id: 'viewer-title' });
  const count = h('p', { class: 'viewer__count' });
  const close = h('button', { type: 'button', class: 'dialog__close', onclick: () => dialog.close() }, icon('close', 18));
  const stage = h('div', { class: 'viewer__stage' });
  const text = h('div', { class: 'viewer__text' });
  const full = h('a', { class: 'rlink viewer__full', target: '_blank', rel: 'noopener' });
  const prev = h('button', { type: 'button', class: 'btn viewer__step', onclick: () => step(-1) }, icon('arrowLeft', 18));
  const next = h('button', { type: 'button', class: 'btn viewer__step', onclick: () => step(1) }, icon('arrowRight', 18));
  const nav = h('footer', { class: 'viewer__nav' }, prev, next);
  dialog.panel.append(
    h('header', { class: 'viewer__head' }, h('div', { class: 'viewer__heading' }, title, count), close),
    stage, text, h('p', { class: 'viewer__links' }, full), nav,
  );

  let items = [];
  let index = 0;
  let copy = null;

  dialog.el.addEventListener('keydown', (event) => {
    // Alt+pijl is in veel browsers "terug"; alleen de kale pijltjes bladeren.
    if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey || event.defaultPrevented) return;
    if (event.key === 'ArrowLeft') step(-1);
    else if (event.key === 'ArrowRight') step(1);
  });

  function step(delta) {
    if (items.length < 2) return;
    index = (index + delta + items.length) % items.length;
    show();
  }

  function show() {
    const item = items[index];
    title.textContent = item.title;
    count.textContent = fill(copy.count, { n: index + 1, total: items.length });
    stage.classList.toggle('is-pair', item.sides.length > 1);
    stage.classList.toggle('is-landscape', Boolean(item.landscape));
    stage.replaceChildren(...item.sides.map((side) => h('figure', { class: 'viewer__side' },
      h('img', { src: side.src, alt: side.alt, decoding: 'async' }),
      side.label ? h('figcaption', {}, side.label) : null,
    )));
    text.replaceChildren(...item.lines.map((line) => h('p', {}, line)));
    // Een poster heeft kleine letters: het beeld op ware grootte in een eigen tab.
    full.hidden = !item.full;
    if (item.full) full.href = item.full;
    // De dialoog zelf scrollt; elk nieuw beeld begint bovenaan.
    dialog.el.scrollTop = 0;
  }

  return {
    setCopy(c) {
      copy = c.viewer;
      close.setAttribute('aria-label', copy.close);
      full.replaceChildren(copy.full, icon('arrowUpRight', 16));
      prev.setAttribute('aria-label', copy.prev);
      next.setAttribute('aria-label', copy.next);
      if (dialog.isOpen()) dialog.close();
    },
    // items: [{ title, lines: [tekst], sides: [{ src, alt, label? }], full?, landscape? }]
    open(list, at) {
      items = list;
      index = at;
      nav.hidden = items.length < 2;
      show();
      dialog.open(close);
    },
  };
}
