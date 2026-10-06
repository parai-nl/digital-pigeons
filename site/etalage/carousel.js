// De waaier: drie kaarten, de actieve in het midden, de andere kleiner en
// half doorzichtig opzij. Wisselen kan met de pijlen, door op een zijkaart te
// klikken, met de pijltjestoetsen en door te vegen. Op desktop kantelt de
// actieve kaart licht met de muis, en bewegen plaat en duif apart; bij touch en
// bij minder beweging staat alles stil. Komt een duif door een handeling van de
// bezoeker naar voren, dan speelt zij één keer een gebaar, zodat je haar uit de
// kaart ziet stappen (niet bij minder beweging).
//
// Tot de bezoeker iets doet, draait de waaier vanzelf: de duif vooraan maakt
// haar gebaar, rust even, en de volgende komt naar voren. Een klik of toets zet
// dat uit, twee minuten rust zet het weer aan, en de knop onder de pijl doet
// beide; buiten beeld, in een verborgen tabblad en zolang het gesprek of de
// vergadering eronder bezig is wacht de waaier: een wissel zou die afbreken.
// Bij minder beweging draait ze niet.
import { h, prefersReducedMotion, onReducedMotionChange, wait } from './dom.js';
import { icon } from './icons.js';
import { ORDER, START, MODES } from './pigeons.js';
import { createStage, breakout, cardAspect } from './stage.js';
import { fill } from './i18n.js';

const finePointer = window.matchMedia('(hover: hover) and (pointer: fine)');

// Vanzelf draaien: de adem voor het eerste gebaar, de rust na een gebaar, en de
// draai zelf (±0,55 s) waarna het gebaar begint.
const AUTO_START = 900;
const AUTO_REST = 2400;
const AUTO_TURN = 380;
const AUTO_IDLE = 120_000;
const PASSING_KEYS = ['Tab', 'Shift', 'Control', 'Alt', 'Meta'];

export function createCarousel(root, { onChange, onPickActive, busy }) {
  let index = ORDER.indexOf(START);
  let copy = null;

  const status = h('p', { class: 'sr-only', 'aria-live': 'polite', 'data-fan-status': '' });
  const help = h('p', { class: 'sr-only', id: 'fan-help' });
  const prev = h('button', { type: 'button', class: 'arrowbtn fan__prev', 'data-fan-prev': '' }, icon('arrowLeft', 22));
  const next = h('button', { type: 'button', class: 'arrowbtn fan__next', 'data-fan-next': '' }, icon('arrowRight', 22));

  const auto = h('button', { type: 'button', class: 'arrowbtn fan__auto', 'data-fan-auto': '' });

  const stages = new Map();
  let greeting = 0;

  const slots = ORDER.map((id) => {
    const stage = createStage(id);
    stages.set(id, stage);
    const label = h('span', { class: 'card__label' }, icon(MODES[id].includes('chat') ? 'chat' : 'listen', 15), h('span', { 'data-card-label': '' }));
    const name = h('span', { class: 'card__name', 'data-card-name': '' });
    const badge = id === 'stakeholder' ? h('span', { class: 'card__badge', 'data-card-badge': '' }) : null;
    const card = h('button', { type: 'button', class: 'card', 'data-pigeon': id },
      ...stage.nodes,
      h('span', { class: 'card__shade', 'aria-hidden': 'true' }),
      h('span', { class: 'card__caption', 'aria-hidden': 'true' }, label, name),
      badge,
    );
    card.addEventListener('click', () => {
      const i = ORDER.indexOf(id);
      if (i === index) onPickActive?.(id);
      else setIndex(i);
    });
    attachTilt(card);
    return h('div', { class: 'fan__slot', 'data-slot': id }, card);
  });

  const track = h('div', { class: 'fan__track' }, ...slots);
  const fan = h('div', { class: 'fan', role: 'region', 'aria-roledescription': '', 'aria-describedby': 'fan-help' }, help, track, prev, next, auto, status);
  root.replaceChildren(fan);

  // De vrije ruimte boven de kaarten volgt het manifest: steekt de duivenlaag
  // 22% boven de kaart uit, dan houdt de waaier die 22% vrij.
  fan.style.setProperty('--card-aspect', String(cardAspect()));
  fan.style.setProperty('--breakout', String(breakout()));

  prev.addEventListener('click', () => step(-1));
  next.addEventListener('click', () => step(1));

  fan.addEventListener('keydown', (event) => {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return;
    event.preventDefault();
    const onCard = event.target.closest?.('.card');
    step(event.key === 'ArrowLeft' ? -1 : 1);
    if (onCard) activeCard().focus({ preventScroll: true });
  });

  // Vegen op een touchscherm: een duidelijke horizontale beweging wisselt, een
  // verticale laat de pagina gewoon scrollen (touch-action: pan-y in de css).
  let swipe = null;
  let suppressClick = false;
  track.addEventListener('pointerdown', (event) => {
    suppressClick = false;
    if (event.pointerType === 'mouse') return;
    swipe = { x: event.clientX, y: event.clientY };
  });
  track.addEventListener('pointerup', (event) => {
    if (!swipe) return;
    const dx = event.clientX - swipe.x;
    const dy = event.clientY - swipe.y;
    swipe = null;
    if (Math.abs(dx) > 40 && Math.abs(dx) > Math.abs(dy) * 1.2) {
      // Een veeg eindigt soms met een klik op de kaart eronder; die telt niet.
      // Komt er geen klik, dan mag de volgende tik gewoon werken.
      suppressClick = true;
      setTimeout(() => { suppressClick = false; }, 350);
      step(dx < 0 ? 1 : -1);
    }
  });
  track.addEventListener('pointercancel', () => { swipe = null; });
  track.addEventListener('click', (event) => {
    if (suppressClick) {
      suppressClick = false;
      event.stopPropagation();
      event.preventDefault();
    }
  }, true);

  function step(delta) {
    setIndex((index + delta + ORDER.length) % ORDER.length);
  }

  function activeCard() {
    return slots[index].querySelector('.card');
  }

  function place() {
    slots.forEach((slot, i) => {
      const rel = (i - index + ORDER.length) % ORDER.length;
      slot.dataset.rel = rel === 0 ? 'center' : rel === 1 ? 'right' : 'left';
      const card = slot.querySelector('.card');
      if (rel === 0) card.setAttribute('aria-current', 'true');
      else card.removeAttribute('aria-current');
    });
  }

  // Draait de waaier vanzelf, dan zwijgt de live-regio (geen melding om de paar
  // seconden) en verzorgt de lus zelf het gebaar.
  function setIndex(i, { auto = false } = {}) {
    if (i === index) return;
    if (!auto) stopAuto();
    resetTilt(activeCard());
    stages.get(ORDER[index]).stop();
    index = i;
    place();
    if (!auto) {
      if (copy) status.textContent = fill(copy.carousel.current, { name: copy.pigeons[ORDER[index]].name });
      greet(ORDER[index]);
    }
    onChange?.(ORDER[index]);
  }

  // Eén stand, zichtbaar op de knop: aan of uit. Een klik of toets elders op de
  // pagina zet hem uit, de knop zet hem om, en na twee minuten zonder teken van
  // de bezoeker gaat hij vanzelf weer aan.
  let autoOn = false;
  let autoRun = 0;
  let lastActive = 0;
  let idleTimer = 0;

  function renderAuto() {
    auto.dataset.auto = autoOn ? 'on' : 'off';
    auto.replaceChildren(icon(autoOn ? 'pause' : 'play', 20));
    if (copy) auto.setAttribute('aria-label', autoOn ? copy.carousel.autoStop : copy.carousel.autoStart);
  }

  function startAuto() {
    if (autoOn || prefersReducedMotion()) return;
    autoOn = true;
    clearTimeout(idleTimer);
    renderAuto();
    autoplay(++autoRun);
  }

  function stopAuto() {
    if (!autoOn) return;
    autoOn = false;
    autoRun++;
    renderAuto();
    lastActive = Date.now();
    watchIdle();
  }

  // Kijken of luisteren naar een lopende reactie is geen rust: de twee minuten
  // tellen vanaf het einde ervan.
  function watchIdle() {
    clearTimeout(idleTimer);
    if (busy?.()) lastActive = Date.now();
    const left = AUTO_IDLE - (Date.now() - lastActive);
    if (left <= 0) startAuto();
    else idleTimer = setTimeout(watchIdle, left);
  }

  async function autoplay(run) {
    const running = () => autoOn && run === autoRun && !prefersReducedMotion();
    const free = async () => {
      while (running() && (!inView || document.hidden || busy?.())) await wait(400);
      return running();
    };
    await wait(AUTO_START);
    while (await free()) {
      await stages.get(ORDER[index]).play();
      await wait(AUTO_REST);
      if (!(await free())) return;
      setIndex((index + 1) % ORDER.length, { auto: true });
      await wait(AUTO_TURN);
    }
  }

  let inView = true;
  if ('IntersectionObserver' in window) {
    new IntersectionObserver(([entry]) => { inView = entry.isIntersecting; }, { threshold: 0.4 }).observe(fan);
  }
  auto.addEventListener('click', () => (autoOn ? stopAuto() : startAuto()));
  for (const type of ['click', 'keydown']) {
    document.addEventListener(type, (event) => {
      lastActive = Date.now();
      // Naar de knop tabben is nog geen keuze; anders staat hij al om voor je er bent.
      if (PASSING_KEYS.includes(event.key)) return;
      if (!auto.contains(event.target)) stopAuto();
    }, true);
  }
  for (const type of ['pointermove', 'pointerdown', 'wheel', 'scroll', 'touchstart']) {
    document.addEventListener(type, () => { lastActive = Date.now(); }, { capture: true, passive: true });
  }
  onReducedMotionChange(() => (prefersReducedMotion() ? stopAuto() : startAuto()));

  // Het gebaar begint als de kaart bijna vooraan staat (de draai duurt ±0,55 s).
  function greet(id) {
    clearTimeout(greeting);
    if (prefersReducedMotion()) return;
    greeting = setTimeout(() => {
      if (ORDER[index] === id) stages.get(id).play();
    }, AUTO_TURN);
  }

  function render(nextCopy) {
    copy = nextCopy;
    fan.setAttribute('aria-label', copy.carousel.label);
    fan.setAttribute('aria-roledescription', copy.carousel.roledesc);
    help.textContent = copy.carousel.help;
    prev.setAttribute('aria-label', copy.carousel.prev);
    next.setAttribute('aria-label', copy.carousel.next);
    renderAuto();
    slots.forEach((slot, i) => {
      const id = ORDER[i];
      const p = copy.pigeons[id];
      slot.querySelector('[data-card-label]').textContent = p.label;
      slot.querySelector('[data-card-name]').textContent = p.name;
      const badge = slot.querySelector('[data-card-badge]');
      if (badge) badge.textContent = copy.badge;
      slot.querySelector('.card').setAttribute('aria-label', [p.name, p.label, badge ? copy.badge : null].filter(Boolean).join(', '));
    });
  }

  place();
  renderAuto();
  startAuto();

  return {
    render,
    active: () => ORDER[index],
    setActive(id) {
      const i = ORDER.indexOf(id);
      if (i >= 0) setIndex(i);
    },
    card: (id) => slots[ORDER.indexOf(id)].querySelector('.card'),
  };
}

// De kanteling: alleen met een echte muis, alleen op de kaart vooraan, en niet
// bij minder beweging. De css verschuift plaat en duif elk met hun eigen maat.
function attachTilt(card) {
  let frame = 0;
  card.addEventListener('pointermove', (event) => {
    if (event.pointerType !== 'mouse' || !finePointer.matches || prefersReducedMotion()) return;
    if (!card.hasAttribute('aria-current')) return;
    const box = card.getBoundingClientRect();
    const x = ((event.clientX - box.left) / box.width) * 2 - 1;
    const y = ((event.clientY - box.top) / box.height) * 2 - 1;
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(() => {
      card.style.setProperty('--tilt-x', clamp(x).toFixed(3));
      card.style.setProperty('--tilt-y', clamp(y).toFixed(3));
      card.classList.add('is-tilting');
    });
  });
  card.addEventListener('pointerleave', () => resetTilt(card));
}

function resetTilt(card) {
  if (!card) return;
  card.classList.remove('is-tilting');
  card.style.setProperty('--tilt-x', '0');
  card.style.setProperty('--tilt-y', '0');
}

const clamp = (v) => Math.max(-1, Math.min(1, v));
