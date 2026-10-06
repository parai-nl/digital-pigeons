// Het bronpaneel: per bron titel, auteurs, jaar, de passage en een link naar het
// paper. Titels en passages blijven in hun oorspronkelijke taal. "Bekijk in de
// kennisgraaf" gaat naar Inloggen: de graaf woont in de werkomgeving.
import { h } from './dom.js';
import { icon } from './icons.js';
import { createDialog } from './dialog.js';
import { fill } from './i18n.js';
import { CONFIG } from './config.js';

let dialog = null;
let copy = null;

export function setSourcesCopy(nextCopy) {
  copy = nextCopy;
  if (dialog?.isOpen()) dialog.close();
}

export function openSources({ sources, author, focus }) {
  if (!dialog) dialog = createDialog({ className: 'dialog--sheet', labelledBy: 'sources-title' });
  const c = copy.sources;
  const close = h('button', { type: 'button', class: 'dialog__close', 'aria-label': c.close, onclick: () => dialog.close() }, icon('close', 18));
  const head = h('header', { class: 'sheet__head' },
    h('div', {},
      h('h2', { id: 'sources-title', class: 'sheet__title' }, c.title),
      h('p', { class: 'sheet__from' }, fill(c.from, { nameThe: copy.pigeons[author].nameThe })),
    ),
    close,
  );
  const items = sources.map((s) => {
    const meta = [s.authors, s.year].filter(Boolean).join(' · ');
    return h('li', { class: 'source', id: `bron-${s.n}`, 'data-source': s.n, tabindex: '-1' },
      h('span', { class: 'source__n', 'aria-hidden': 'true' }, String(s.n)),
      h('div', { class: 'source__body' },
        h('h3', { class: 'source__title', lang: s.lang || null }, s.title),
        meta ? h('p', { class: 'source__meta' }, meta) : null,
        s.passages.length ? h('figure', { class: 'source__passage' },
          h('figcaption', {}, c.passage),
          ...s.passages.map((p) => h('blockquote', { lang: p.lang || null }, p.text)),
        ) : null,
        s.url ? h('a', { class: 'source__link', href: s.url, target: '_blank', rel: 'noopener noreferrer' }, c.open, icon('arrowUpRight', 16)) : null,
      ),
    );
  });
  const foot = h('footer', { class: 'sheet__foot' },
    h('a', { class: 'graph-link', href: CONFIG.loginUrl }, icon('lock', 16), c.graph),
    h('span', { class: 'sheet__note' }, c.graphNote),
  );
  dialog.panel.replaceChildren(head, h('ol', { class: 'source-list', 'data-author': author }, ...items), foot);
  dialog.el.dataset.author = author;

  const target = dialog.panel.querySelector(`[data-source="${focus}"]`) || dialog.panel.querySelector('.source');
  dialog.panel.querySelectorAll('.source').forEach((li) => li.classList.toggle('is-focus', li === target));
  dialog.open(target);
  target?.scrollIntoView({ block: 'nearest' });
}
