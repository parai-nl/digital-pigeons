// Tekst met verwijzingen, zoals de module hem toont: "[1]" en "[2, 3]" worden
// kleine knoppen die de bron openen. Tijdens het afspelen blijft een half
// binnengekomen "[1" achter, anders flitst de ruwe opmaak door de zin.
import { h } from './dom.js';

const CITATION = /\[\s*(\d+(?:\s*,\s*\d+)*)\s*\]/g;

export const holdPartial = (text) => text.replace(/\[[\d,\s]*$/, '');

export const stripCitations = (text) => text.replace(CITATION, '').replace(/\s+([.,;:!?])/g, '$1');

// Alinea's (gescheiden door een lege regel) worden eigen <p>'s, zoals in de module.
export function paragraphs(text) {
  return text.split(/\n\s*\n/).map((part) => part.trim()).filter(Boolean);
}

export function renderPlain(el, text) {
  el.replaceChildren(...paragraphs(text).map((part) => h('p', {}, part)));
}

export function renderCited(el, text, options) {
  const shown = options.streaming ? holdPartial(text) : text;
  el.replaceChildren(...paragraphs(shown).map((part) => {
    const p = document.createElement('p');
    p.append(citedFragment(part, options));
    return p;
  }));
}

function citedFragment(shown, { sources = [], streaming = false, onCite, label }) {
  const known = new Set(sources.map((s) => s.n));
  const frag = document.createDocumentFragment();
  let last = 0;
  for (const match of shown.matchAll(CITATION)) {
    frag.append(shown.slice(last, match.index));
    const numbers = match[1].split(',').map((n) => Number.parseInt(n, 10));
    if (!numbers.every((n) => known.has(n))) {
      frag.append(match[0]);
    } else {
      const group = h('span', { class: 'cites' });
      numbers.forEach((n) => {
        // Zolang de tekst nog groeit, wordt dit stuk bij elk brokje opnieuw
        // getekend; een knop zou dan onder de muis of de focus verdwijnen. De
        // verwijzing wordt pas een knop als het antwoord er staat.
        group.append(streaming
          ? h('span', { class: 'cite', 'data-cite': n }, String(n))
          : h('button', {
            type: 'button',
            class: 'cite',
            'data-cite': n,
            'aria-label': label(n),
            'aria-haspopup': 'dialog',
            onclick: () => onCite(n),
          }, String(n)));
      });
      frag.append(group);
    }
    last = match.index + match[0].length;
  }
  frag.append(shown.slice(last));
  return frag;
}
