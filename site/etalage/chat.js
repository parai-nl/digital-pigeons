// Het gesprek met een pratende duif. Elke duifzin is een opname uit de
// werkomgeving (opnames.json); deze module speelt die af zoals de module hem
// liet zien: de vraag typt zich in de balk, de stappen staan kort in beeld,
// het antwoord groeit in brokken en eronder staat de bronchip.
//
// Een gesprek is de reeks handelingen van de bezoeker (een vraagknop, een eigen
// vraag, een vergelijking), per duif in het paginageheugen. Wat er op het scherm
// staat, wordt uit die reeks en de opnames in de huidige taal opgebouwd; alleen
// de nieuwste handeling speelt af. Wisselen van duif kopieert niets,
// herladen wist alles, en getypte tekst gaat nergens heen.
import { h, wait, isAbort, prefersReducedMotion, follower } from './dom.js';
import { icon } from './icons.js';
import { fill } from './i18n.js';
import { renderCited, renderPlain, stripCitations } from './cited.js';
import { openSources } from './sources.js';
import { avatar } from './stage.js';
import { TALKING, otherTalking } from './pigeons.js';
import { CONFIG } from './config.js';

// Totale wachttijd tot het eerste woord van de duif, verdeeld over de stappen
// ervoor. De opname duurde langer; de pagina kort alleen het wachten in.
const PRE_WAIT = 1900;
const MAX_GAP = 250;

export function createChat(root, { requestPigeon }) {
  const events = Object.fromEntries(TALKING.map((id) => [id, []]));
  let pigeon = 'kennispartner';
  let copy = null;
  let lang = 'nl';
  let data = null;
  let status = 'laden';
  let run = null;
  let typing = null;
  let animateNext = null;
  let focusOrigin = false;
  let compareButtons = [];

  const thread = h('div', { class: 'chat__thread', 'data-chat-thread': '' });
  const scroller = h('div', { class: 'chat__scroll', role: 'region', tabindex: '0', 'data-chat-scroll': '' }, thread);
  const toBottom = follower(scroller);
  const stepsRow = h('div', { class: 'chat__steps', role: 'group', 'data-chat-steps': '' });
  const stepStatus = h('p', { class: 'sr-only', role: 'status', 'data-chat-step-status': '' });
  const announcer = h('p', { class: 'sr-only', 'aria-live': 'polite', 'data-chat-announce': '' });
  const questions = h('div', { class: 'chat__questions', role: 'group', 'data-chat-questions': '' });
  const unavailable = h('p', { class: 'chat__notice', 'data-chat-unavailable': '', hidden: true });
  const input = h('textarea', { id: 'chat-input', class: 'composer__input', rows: '1', maxlength: '500', autocomplete: 'off', 'data-chat-input': '' });
  const inputLabel = h('label', { class: 'sr-only', for: 'chat-input' });
  const send = h('button', { type: 'submit', class: 'composer__send', 'data-chat-send': '' }, icon('send', 18));
  const form = h('form', { class: 'composer', 'data-composer': '' }, inputLabel, input, send);
  root.replaceChildren(h('div', { class: 'chat' },
    scroller,
    stepsRow,
    stepStatus,
    announcer,
    h('div', { class: 'chat__foot' }, questions, unavailable, form),
  ));

  input.addEventListener('input', refreshControls);
  input.addEventListener('keydown', (event) => {
    if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
      event.preventDefault();
      form.requestSubmit();
    }
  });
  form.addEventListener('submit', (event) => {
    event.preventDefault();
    const text = input.value.trim();
    if (!text || busy()) return;
    input.value = '';
    push({ kind: 'own', text });
  });

  // ---- toestand -------------------------------------------------------------

  const busy = () => Boolean(run || typing);
  const question = (id) => data?.questions.find((q) => q.id === id) || null;
  const name = (id) => copy.pigeons[id].name;
  const nameThe = (id) => copy.pigeons[id].nameThe;

  function refreshControls() {
    const isBusy = busy();
    // Een vraag die in dit gesprek al gesteld is, staat er nog, maar gedimd: de
    // opname zou zich alleen herhalen.
    const asked = new Set(events[pigeon].filter((e) => e.kind === 'ask').map((e) => e.q));
    questions.querySelectorAll('button').forEach((b) => {
      const done = asked.has(b.dataset.question);
      b.classList.toggle('is-asked', done);
      b.setAttribute('aria-disabled', String(isBusy || done));
      b.querySelector('[data-asked-note]').textContent = done ? ` (${copy.chat.asked})` : '';
    });
    send.setAttribute('aria-disabled', String(isBusy || !input.value.trim()));
    for (const [button, ev] of compareButtons) {
      button.hidden = ev.compared || run?.ev === ev;
      button.setAttribute('aria-disabled', String(isBusy));
    }
    root.dataset.busy = String(isBusy);
  }

  function cancelRun() {
    typing?.abort();
    typing = null;
    if (run) {
      run.controller.abort();
      run = null;
    }
    clearSteps();
  }

  function push(ev) {
    events[pigeon].push(ev);
    thread.querySelector('.chat__empty, .chat__loading')?.remove();
    startRun(ev);
  }

  // ---- opbouw ---------------------------------------------------------------

  function renderThread() {
    cancelRun();
    compareButtons = [];
    thread.replaceChildren();
    const list = events[pigeon];
    // Zolang de opnames onderweg zijn, staat er een rustige laadregel waar
    // straks het gesprek staat.
    if (!list.length && status === 'laden') {
      thread.append(h('p', { class: 'chat__loading', 'data-chat-loading': '' }, copy.chat.loading));
    }
    // De hint noemt de vragen hieronder; zolang die er niet zijn, zwijgt hij.
    if (!list.length && data?.questions.length) {
      thread.append(h('div', { class: 'chat__empty' },
        avatar(pigeon, { class: 'chat__empty-avatar', width: 56, height: 56 }),
        h('p', {}, copy.chat.empty),
      ));
    }
    for (const ev of list) {
      if (ev === animateNext) {
        animateNext = null;
        startRun(ev);
      } else {
        const block = h('div', { class: 'turn' });
        thread.append(block);
        fillEvent(block, ev, null);
      }
    }
    refreshControls();
    toBottom(true);
  }

  function startRun(ev) {
    const controller = new AbortController();
    run = { controller, ev };
    const block = h('div', { class: 'turn' });
    thread.append(block);
    refreshControls();
    fillEvent(block, ev, { signal: controller.signal })
      .then((answered) => {
        if (answered) announcer.textContent = fill(copy.chat.answered, { nameThe: nameThe(pigeon) });
      })
      .catch((err) => {
        if (!isAbort(err)) console.error(err);
      })
      .finally(() => {
        if (run?.controller !== controller) return;
        run = null;
        clearSteps();
        refreshControls();
      });
  }

  // Bouwt één handeling op. Zonder `anim` staat alles er meteen, met `anim`
  // speelt de beurt af. Geeft true terug als er een duif antwoordde.
  async function fillEvent(block, ev, anim) {
    if (ev.kind === 'own') {
      block.append(userBubble(ev.text, null));
      toBottom(true);
      if (anim) await wait(prefersReducedMotion() ? 0 : 450, anim.signal);
      block.append(ownNote());
      scroll(anim);
      return false;
    }
    const q = question(ev.q);
    const text = q ? q.text[lang] : '';
    if (ev.kind === 'ask') {
      block.append(userBubble(text, ev));
      toBottom(true);
      const turn = q?.turns[lang][pigeon];
      if (!turn) return block.append(note(copy.chat.missing)), false;
      await showTurn(block, turn, anim);
      return true;
    }
    // Vergelijken: de herkomstregel, de vraag met het antwoord van de
    // eerste duif onder haar eigen naam en bronnen, en dan dezelfde vraag met
    // het opgenomen vergelijkantwoord van deze duif.
    const origin = originLine(ev.from);
    block.append(origin, userBubble(text, null));
    // Zoals de module: alleen het antwoord van de eerste duif reist mee, niet de
    // facilitatorberichten van dat gesprek.
    const first = q?.turns[lang][ev.from];
    if (first) await showTurn(block, { ...first, messages: first.messages.filter((m) => m.author !== 'facilitator') }, null);
    block.append(h('p', { class: 'chat__repeated' }, fill(copy.chat.repeated, { nameThe: nameThe(pigeon) })));
    if (anim && focusOrigin) {
      focusOrigin = false;
      origin.focus({ preventScroll: true });
    }
    toBottom(true);
    const second = q?.turns[lang][`${ev.from}>${pigeon}`];
    if (!second) return block.append(note(copy.chat.missing)), false;
    await showTurn(block, second, anim);
    return true;
  }

  // Een opbouw zonder afspelen springt naar het einde; tijdens het afspelen
  // volgt het vak alleen als de lezer onderaan staat.
  function scroll(playing) {
    toBottom(!playing);
  }

  function userBubble(text, ev) {
    const bubble = h('div', { class: 'msg__bubble' }, h('span', { class: 'msg__text' }, text));
    const wrap = h('div', { class: 'msg msg--user' }, bubble);
    if (ev?.kind === 'ask') {
      const other = otherTalking(pigeon);
      const canCompare = Boolean(question(ev.q)?.turns[lang][`${pigeon}>${other}`]);
      if (canCompare) {
        const button = h('button', {
          type: 'button',
          class: 'ask-other',
          'data-ask-other': other,
          onclick: () => compare(ev),
        }, icon('arrowBend', 15), fill(copy.chat.compare, { nameThe: nameThe(other) }));
        // Pas als het antwoord er staat, komt de weg naar de andere duif.
        button.hidden = ev.compared || run?.ev === ev;
        bubble.append(button);
        wrap.classList.add('has-compare');
        compareButtons.push([button, ev]);
      }
    }
    return wrap;
  }

  function ownNote() {
    return h('p', { class: 'chat__note', 'data-chat-own': '' },
      copy.chat.own, ' ',
      h('a', { href: CONFIG.loginUrl, class: 'chat__note-login' }, icon('lock', 14), copy.chat.ownLogin),
    );
  }

  function note(text) {
    return h('p', { class: 'chat__note' }, text);
  }

  function originLine(from) {
    return h('p', { class: 'chat__origin', tabindex: '-1', 'data-chat-origin': from },
      h('span', {}, fill(copy.chat.origin, { nameThe: nameThe(from) })),
      h('button', {
        type: 'button',
        class: 'textbtn',
        onclick: () => {
          requestPigeon(from);
          // De knop zelf verdwijnt met het gesprek; de focus gaat naar het gesprek
          // waar je naar terugkeert.
          scroller.focus({ preventScroll: true });
        },
      }, copy.chat.back),
    );
  }

  function authorLine(author) {
    if (author === 'facilitator') return h('p', { class: 'msg__author' }, copy.chat.facilitator);
    return h('p', { class: 'msg__author' },
      avatar(author, { class: 'msg__avatar', width: 28, height: 28 }),
      name(author),
    );
  }

  function messageEl(msg) {
    const text = h('div', { class: 'msg__text' });
    if (msg.author === 'facilitator') {
      renderPlain(text, stripCitations(msg.text));
      return h('div', { class: 'msg msg--facilitator', 'data-author': 'facilitator' }, authorLine('facilitator'), h('div', { class: 'msg__bubble' }, text));
    }
    return h('div', { class: 'msg msg--pigeon', 'data-author': msg.author, 'data-kind': msg.kind },
      authorLine(msg.author),
      h('div', { class: 'msg__bubble' }, text),
      h('div', { class: 'msg__foot' }),
    );
  }

  function citeOptions(msg, streaming) {
    return {
      sources: msg.sources,
      streaming,
      label: (n) => fill(copy.chat.cite, { n }),
      onCite: (n) => openSources({ sources: msg.sources, author: msg.author, focus: n }),
    };
  }

  function finish(el, msg) {
    renderCited(el.querySelector('.msg__text'), msg.text, citeOptions(msg, false));
    const foot = el.querySelector('.msg__foot');
    const chips = [];
    if (msg.sources.length) {
      const one = msg.related ? copy.chat.relatedOne : copy.chat.sourcesOne;
      const many = msg.related ? copy.chat.relatedMany : copy.chat.sourcesMany;
      const label = msg.sources.length === 1 ? one : fill(many, { count: msg.sources.length });
      chips.push(h('button', {
        type: 'button',
        class: 'chip chip--sources',
        'data-sources': msg.sources.length,
        'aria-haspopup': 'dialog',
        onclick: () => openSources({ sources: msg.sources, author: msg.author, focus: msg.sources[0].n }),
      }, icon('book', 15), label));
    }
    // Zoals in de module: "verwant" zegt waar de bronnen vandaan komen, niet wat
    // ze waard zijn; een neutrale regel, geen waarschuwing.
    if (msg.related && msg.sources.length) chips.push(h('span', { class: 'chip chip--related', 'data-related': '' }, copy.chat.related));
    if (msg.kind === 'standpoint') chips.push(h('span', { class: 'chip chip--stance', 'data-stance': '' }, icon('compass', 15), copy.chat.stance));
    if (msg.kind === 'gap' && !msg.sources.length) chips.push(h('span', { class: 'chip chip--gap', 'data-gap': '' }, copy.chat.gap));
    foot.replaceChildren(...chips);
    foot.hidden = !chips.length;
  }

  async function showTurn(block, turn, anim) {
    if (!anim) {
      for (const msg of turn.messages) {
        const el = messageEl(msg);
        block.append(el);
        if (msg.author !== 'facilitator') finish(el, msg);
      }
      return;
    }
    await playTurn(block, turn, anim.signal);
  }

  // ---- afspelen -------------------------------------------------------------

  async function playTurn(block, turn, signal) {
    const queue = [...turn.messages];
    const author = queue.find((m) => m.author !== 'facilitator').author;
    const phases = turn.phases.length ? turn.phases : ['pigeon_grounding', 'pigeon'];
    const pigeonAt = phases.indexOf('pigeon');
    const before = pigeonAt >= 0 ? pigeonAt : phases.length;
    const stepMs = Math.min(1000, Math.max(450, Math.round(PRE_WAIT / Math.max(1, before))));
    const take = (test) => {
      const i = queue.findIndex(test);
      return i >= 0 ? queue.splice(i, 1)[0] : null;
    };
    let thinking = null;
    const think = (who) => {
      thinking?.remove();
      thinking = thinkingBubble(who);
      block.append(thinking);
      scroll(true);
    };
    const stopThinking = () => {
      thinking?.remove();
      thinking = null;
    };

    for (const phase of phases) {
      startStep(phase);
      if (phase === 'pigeon') {
        stopThinking();
        while (queue[0]?.author === 'facilitator') appendWhole(block, queue.shift());
        const msg = take((m) => m.author !== 'facilitator');
        if (msg) await stream(block, msg, signal);
      } else {
        const facilitatorNext = phase === 'mediation'
          ? queue.some((m) => m.author === 'facilitator')
          : phase === 'facilitator' && queue[0]?.author === 'facilitator';
        think(facilitatorNext ? 'facilitator' : author);
        await wait(stepMs, signal);
        stopThinking();
        if (facilitatorNext) appendWhole(block, take((m) => m.author === 'facilitator'));
      }
      endStep(phase);
    }
    for (const msg of queue) {
      if (msg.author === 'facilitator') appendWhole(block, msg);
      else await stream(block, msg, signal);
    }
    clearSteps();
  }

  function appendWhole(block, msg) {
    if (!msg) return;
    const el = messageEl(msg);
    block.append(el);
    if (msg.author !== 'facilitator') finish(el, msg);
    scroll(true);
  }

  function thinkingBubble(who) {
    const label = who === 'facilitator'
      ? copy.chat.thinkingFacilitator
      : fill(copy.chat.thinking, { nameThe: nameThe(who) });
    return h('div', { class: `msg msg--thinking ${who === 'facilitator' ? 'msg--facilitator' : 'msg--pigeon'}`, 'data-author': who, 'data-thinking': '' },
      authorLine(who),
      h('div', { class: 'msg__bubble' },
        h('span', { class: 'dots', 'aria-hidden': 'true' }, h('span'), h('span'), h('span')),
        h('span', { class: 'sr-only' }, label),
      ),
    );
  }

  // Het antwoord groeit in de brokken van de opname, met de pauzes ingekort;
  // zonder brokken gelijkmatig per paar woorden. Bij minder beweging staat het
  // er in één keer.
  async function stream(block, msg, signal) {
    const el = messageEl(msg);
    el.querySelector('.msg__foot').hidden = true;
    el.setAttribute('aria-busy', 'true');
    block.append(el);
    const target = el.querySelector('.msg__text');
    if (!prefersReducedMotion()) {
      const start = performance.now();
      let shown = '';
      for (const [at, piece] of plan(msg)) {
        const delay = at - (performance.now() - start);
        if (delay > 4) await wait(delay, signal);
        shown += piece;
        renderCited(target, shown, citeOptions(msg, true));
        scroll(true);
      }
    }
    el.removeAttribute('aria-busy');
    finish(el, msg);
    scroll(true);
  }

  function plan(msg) {
    const budget = Math.min(3200, Math.max(900, msg.text.length * 5));
    if (msg.chunks) {
      let t = 0;
      const times = msg.chunks.map((c, i) => {
        if (i > 0) t += Math.min(MAX_GAP, c[0] - msg.chunks[i - 1][0]);
        return t;
      });
      const scale = t > budget ? budget / t : 1;
      return msg.chunks.map((c, i) => [times[i] * scale, c[1]]);
    }
    const words = msg.text.match(/\S+\s*/g) || [msg.text];
    const groups = [];
    for (let i = 0; i < words.length; i += 3) groups.push(words.slice(i, i + 3).join(''));
    const lead = msg.text.match(/^\s*/)[0];
    if (lead) groups[0] = lead + groups[0];
    const every = budget / Math.max(1, groups.length);
    return groups.map((g, i) => [i * every, g]);
  }

  // ---- stappen ----------------------------------------------------------------

  function startStep(phase) {
    const label = copy.chat.steps[phase];
    if (!label) return;
    stepsRow.append(h('span', { class: 'chip chip--step is-running', 'data-step': phase, 'data-state': 'running' },
      h('span', {}, `${label}…`),
      h('span', { class: 'pulse', 'aria-hidden': 'true' }),
    ));
    stepStatus.textContent = fill(copy.chat.stepsBusy, { step: label });
  }

  function endStep(phase) {
    const chip = stepsRow.querySelector(`[data-step="${phase}"][data-state="running"]`);
    if (!chip) return;
    chip.dataset.state = 'done';
    chip.classList.remove('is-running');
    chip.querySelector('.pulse')?.remove();
    chip.firstChild.textContent = copy.chat.steps[phase];
  }

  function clearSteps() {
    stepsRow.replaceChildren();
    stepStatus.textContent = '';
  }

  // ---- handelingen ------------------------------------------------------------

  async function ask(id) {
    if (busy()) return;
    const q = question(id);
    if (!q) return;
    const controller = new AbortController();
    typing = controller;
    refreshControls();
    const draft = input.value;
    input.readOnly = true;
    try {
      await typeInto(q.text[lang], controller.signal);
    } catch {
      return;
    } finally {
      input.readOnly = false;
      input.value = draft;
      if (typing === controller) typing = null;
      refreshControls();
    }
    push({ kind: 'ask', q: id });
  }

  // De vraag typt zichtbaar in de balk en gaat dan weg, zoals bij versturen.
  async function typeInto(text, signal) {
    if (prefersReducedMotion()) {
      input.value = text;
      await wait(120, signal);
      return;
    }
    const per = Math.max(10, Math.min(26, 850 / text.length));
    for (let i = 1; i <= text.length; i++) {
      input.value = text.slice(0, i);
      input.scrollTop = input.scrollHeight;
      await wait(per, signal);
    }
    await wait(220, signal);
  }

  function compare(ev) {
    if (busy() || ev.compared) return;
    const from = pigeon;
    const to = otherTalking(from);
    if (!question(ev.q)?.turns[lang][`${from}>${to}`]) return;
    ev.compared = true;
    const next = { kind: 'compare', q: ev.q, from };
    events[to].push(next);
    animateNext = next;
    focusOrigin = true;
    requestPigeon(to);
  }

  function renderQuestions() {
    questions.setAttribute('aria-label', copy.chat.questions);
    const list = data?.questions || [];
    questions.replaceChildren(...list.map((q) => h('button', {
      type: 'button',
      class: 'qbtn',
      'data-question': q.id,
      onclick: (event) => {
        if (event.currentTarget.getAttribute('aria-disabled') === 'true') return;
        ask(q.id);
      },
    }, q.text[lang], h('span', { class: 'sr-only', 'data-asked-note': '' }))));
    questions.hidden = !list.length;
    unavailable.hidden = Boolean(list.length) || status === 'laden';
    unavailable.textContent = copy.chat.unavailable;
  }

  function renderStatic() {
    scroller.setAttribute('aria-label', fill(copy.chat.log, { nameThe: nameThe(pigeon) }));
    stepsRow.setAttribute('aria-label', copy.chat.stepsLabel);
    inputLabel.textContent = copy.chat.input;
    input.placeholder = copy.pigeons[pigeon].placeholder;
    send.setAttribute('aria-label', copy.chat.send);
  }

  return {
    // status: 'laden' zolang opnames.json onderweg is, 'klaar' of 'ontbreekt'.
    setData(nextData, nextStatus = nextData ? 'klaar' : 'ontbreekt') {
      data = nextData;
      status = nextStatus;
      if (copy) {
        renderQuestions();
        renderThread();
      }
    },
    setCopy(nextCopy, nextLang) {
      copy = nextCopy;
      lang = nextLang;
      renderQuestions();
      renderStatic();
      renderThread();
    },
    show(id) {
      if (!TALKING.includes(id)) return;
      pigeon = id;
      renderStatic();
      renderThread();
    },
    hide() {
      if (!busy()) return;
      cancelRun();
      renderThread();
    },
    pigeon: () => pigeon,
    busy,
  };
}
