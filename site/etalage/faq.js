// De FAQ als echte dialoog: één vraag tegelijk open. De eerlijke
// regel onder het gesprek opent hem meteen op "Zijn de antwoorden op deze pagina
// echt?".
import { h } from './dom.js';
import { icon } from './icons.js';
import { createDialog } from './dialog.js';
import { CONFIG } from './config.js';

export function createFaq({ context }) {
  const dialog = createDialog({ className: 'dialog--faq', labelledBy: 'faq-title' });
  let copy = null;
  let openId = null;

  function toggle(id) {
    openId = openId === id ? null : id;
    sync();
  }

  function sync() {
    dialog.panel.querySelectorAll('[data-faq-item]').forEach((item) => {
      const expanded = item.dataset.faqItem === openId;
      item.querySelector('button').setAttribute('aria-expanded', String(expanded));
      item.querySelector('.faq__answer').hidden = !expanded;
      item.classList.toggle('is-open', expanded);
    });
  }

  function render() {
    const c = copy.faq;
    const ctx = context();
    const contact = readContact(CONFIG.contact);
    const items = c.items.map((item) => {
      const answerId = `faq-a-${item.id}`;
      const buttonId = `faq-q-${item.id}`;
      return h('div', { class: 'faq__item', 'data-faq-item': item.id },
        h('h3', { class: 'faq__q' },
          h('button', { type: 'button', id: buttonId, 'aria-expanded': 'false', 'aria-controls': answerId, onclick: () => toggle(item.id) },
            h('span', {}, item.q),
            icon('plus', 20),
          ),
        ),
        h('div', { class: 'faq__answer', id: answerId, role: 'region', 'aria-labelledby': buttonId, hidden: true },
          h('p', {}, item.a(ctx)),
          contact && c.contact[item.id]
            ? h('p', { class: 'faq__contact' }, `${c.contact[item.id]} `, h('a', { href: contact.href }, contact.label), '.')
            : null,
        ),
      );
    });
    dialog.panel.replaceChildren(
      h('header', { class: 'faq__head' },
        h('div', { class: 'faq__heading' },
          h('span', { class: 'faq__mark', 'aria-hidden': 'true' }, '?'),
          h('h2', { id: 'faq-title' }, c.title),
        ),
        h('button', { type: 'button', class: 'dialog__close', 'aria-label': c.close, onclick: () => dialog.close() }, icon('close', 18)),
      ),
      h('div', { class: 'faq__items' }, ...items),
      h('footer', { class: 'faq__foot' },
        h('button', { type: 'button', class: 'btn btn--ink', onclick: () => dialog.close() }, c.close),
      ),
    );
    sync();
  }

  return {
    setCopy(nextCopy) {
      copy = nextCopy;
      if (dialog.isOpen()) render();
    },
    // Opent de FAQ; met een id staat die vraag open en in beeld.
    open(id) {
      openId = id || copy.faq.items[0].id;
      render();
      const button = dialog.panel.querySelector(`[data-faq-item="${openId}"] button`);
      dialog.open(button);
      if (id) button?.scrollIntoView({ block: 'center' });
    },
    close: () => dialog.close(),
  };
}

// Alleen een e-mailadres of een https-adres; al het andere telt als geen contact.
function readContact(value) {
  const href = typeof value === 'string' ? value : value?.href;
  if (typeof href !== 'string' || !/^(mailto:[^\s@]+@[^\s@]+|https:\/\/\S+)$/.test(href)) return null;
  const label = (typeof value === 'object' && value?.label) || href.replace(/^mailto:/, '');
  return { href, label };
}
