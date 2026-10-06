// Een echte dialoog op de native <dialog>: de rest van de pagina is inert, Tab
// blijft binnen het venster, Escape en een klik naast het venster sluiten, en
// de focus gaat terug naar de knop die het opende.
import { h } from './dom.js';

const FOCUSABLE = 'a[href], button:not([disabled]), textarea:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])';

export function createDialog({ className, labelledBy }) {
  const panel = h('div', { class: 'dialog__panel' });
  const el = h('dialog', { class: `dialog ${className}`, 'aria-labelledby': labelledBy }, panel);
  document.body.append(el);
  let opener = null;
  const closeHandlers = [];

  el.addEventListener('cancel', (event) => {
    event.preventDefault();
    close();
  });
  // Alleen een klik die op de achtergrond begint én eindigt, sluit: wie tekst
  // selecteert en de muis naast het venster loslaat, wil niet dat het dichtgaat.
  let downOnBackdrop = false;
  el.addEventListener('pointerdown', (event) => {
    downOnBackdrop = event.target === el;
  });
  el.addEventListener('click', (event) => {
    if (event.target === el && downOnBackdrop) close();
    downOnBackdrop = false;
  });
  el.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const items = [...el.querySelectorAll(FOCUSABLE)].filter((node) => node.offsetParent !== null || node === document.activeElement);
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    if (event.shiftKey && (document.activeElement === first || document.activeElement === el)) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  function open(focusTarget) {
    opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    if (!el.open) el.showModal();
    document.documentElement.classList.add('has-dialog');
    const target = focusTarget || el.querySelector(FOCUSABLE);
    target?.focus({ preventScroll: Boolean(focusTarget) });
  }

  function close() {
    if (!el.open) return;
    el.close();
    document.documentElement.classList.remove('has-dialog');
    closeHandlers.forEach((fn) => fn());
    if (opener?.isConnected) opener.focus({ preventScroll: true });
    opener = null;
  }

  return { el, panel, open, close, isOpen: () => el.open, onClose: (fn) => closeHandlers.push(fn) };
}
