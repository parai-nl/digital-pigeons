// Kleine DOM-hulpen. Tekst gaat altijd als tekst de pagina in (textContent),
// nooit als HTML: de opnames komen uit een bestand dat de pagina niet zelf schrijft.

export function h(tag, props = {}, ...children) {
  const el = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (value == null || value === false) continue;
    if (key === 'class') el.className = value;
    else if (key === 'text') el.textContent = value;
    else if (key === 'style' && typeof value === 'object') {
      for (const [prop, v] of Object.entries(value)) el.style.setProperty(prop, v);
    } else if (key.startsWith('on') && typeof value === 'function') {
      el.addEventListener(key.slice(2).toLowerCase(), value);
    } else if (key === 'dataset') Object.assign(el.dataset, value);
    else if (value === true) el.setAttribute(key, '');
    else el.setAttribute(key, String(value));
  }
  append(el, children);
  return el;
}

function append(el, children) {
  for (const child of children.flat(Infinity)) {
    if (child == null || child === false || child === '') continue;
    el.append(child instanceof Node ? child : document.createTextNode(String(child)));
  }
}

export const $ = (selector, root = document) => root.querySelector(selector);
export const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

// Een onderbreekbare pauze. Een afgebroken signaal wijst de belofte af met
// AbortError; de aanroeper vangt dat op en tekent de eindstand.
export function wait(ms, signal) {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(abortError());
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort);
      resolve();
    }, ms);
    const onAbort = () => {
      clearTimeout(timer);
      reject(abortError());
    };
    signal?.addEventListener('abort', onAbort, { once: true });
  });
}

export function abortError() {
  return new DOMException('Afgebroken', 'AbortError');
}

export const isAbort = (err) => err?.name === 'AbortError';

const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
export const prefersReducedMotion = () => reducedMotion.matches;
export const onReducedMotionChange = (fn) => reducedMotion.addEventListener('change', fn);

// Houdt een scrollvak onderaan terwijl er tekst bijkomt, behalve als de lezer
// zelf omhoog scrolde. Of de lezer onderaan stond, wordt bij het scrollen
// bijgehouden en niet pas na het toevoegen: een nieuw blok is vaak hoger dan
// elke marge.
export function follower(scroller) {
  let follow = true;
  scroller.addEventListener('scroll', () => {
    follow = scroller.scrollHeight - scroller.scrollTop - scroller.clientHeight < 60;
  }, { passive: true });
  return (force = false) => {
    if (force || follow) {
      scroller.scrollTop = scroller.scrollHeight;
      follow = true;
    }
  };
}
