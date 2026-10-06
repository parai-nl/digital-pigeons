// De mini-vergadering: één gedeelde vergadering voor alle drie de duiven. De
// bezoeker kiest wat er in de zaal gezegd wordt; de duif aan tafel reageert
// zoals de opname het vastlegde: spraak (tekst, bronnen, stem, gebaar), signaal
// (beweging en echt gekoer, geen tekst en geen label) of stilte (niets).
//
// Het verslag hoort bij de vergadering, de reacties bij de duif: wie van duif
// wisselt, ziet dezelfde uitspraken met de reacties van de nieuwe duif, stil.
// Alleen een nieuwe uitspraak speelt af. Geluid klinkt alleen na een klik, en de
// geluidsknop zet het uit en onthoudt die keuze.
//
// De duif zit zelf aan tafel (zoals in de module): het paneel heeft een
// eigen podium met dezelfde lagen als de kaart, dat in beeld blijft terwijl je
// een uitspraak kiest. Daar speelt haar reactie.
import { h, wait, isAbort, prefersReducedMotion, follower } from './dom.js';
import { icon } from './icons.js';
import { fill } from './i18n.js';
import { renderCited, stripCitations } from './cited.js';
import { openSources } from './sources.js';
import { avatar, createStage, breakout, sideout, cardAspect } from './stage.js';
import * as audio from './audio.js';

// De opgenomen wachttijd voor een reactie mag korter, nooit langer.
const MAX_DELAY = 1800;

export function createMeeting(root, { sound }) {
  let pigeon = 'kennispartner';
  let copy = null;
  let lang = 'nl';
  let data = null;
  let run = null;
  let status = 'laden';
  let stage = null;
  const said = [];

  const tableName = h('p', { class: 'meeting__table-name', 'data-meeting-table': '' });
  const tableCard = h('div', { class: 'table-card', 'aria-hidden': 'true', 'data-table-card': '' });
  // Een schakelaar, geen opdracht: "Geluid" met een pil die de stand toont en
  // de stand klein ernaast. De naam is alleen "Geluid"; aria-checked draagt de
  // stand, dus pil en standwoord zijn voor schermlezers verborgen.
  const soundLabel = h('span', { class: 'soundbtn__label' });
  const soundState = h('span', { class: 'soundbtn__state', 'aria-hidden': 'true' });
  const soundButton = h('button', { type: 'button', role: 'switch', class: 'soundbtn', 'data-sound': '' },
    h('span', { class: 'soundbtn__icon' }),
    soundLabel,
    h('span', { class: 'soundbtn__track', 'aria-hidden': 'true' }),
    soundState,
  );
  // Staat het geluid uit, dan zegt een rustige regel bij de uitspraken dat, met
  // een knop die het meteen weer aanzet.
  const turnOn = h('button', { type: 'button', class: 'textbtn', 'data-sound-on': '' });
  const mutedNote = h('p', { class: 'meeting__muted', hidden: true, 'data-sound-note': '' },
    h('span', { class: 'meeting__muted-icon', 'aria-hidden': 'true' }),
    h('span', {}),
    turnOn,
  );
  const intro = h('p', { class: 'meeting__intro' });
  const log = h('div', { class: 'meeting__log', 'data-meeting-log': '' });
  const scroller = h('div', { class: 'meeting__scroll', role: 'region', tabindex: '0' }, intro, log);
  const toBottom = follower(scroller);
  const announcer = h('p', { class: 'sr-only', 'aria-live': 'polite', 'data-meeting-announce': '' });
  const askLabel = h('p', { class: 'meeting__ask', id: 'meeting-ask' });
  const statements = h('div', { class: 'meeting__statements', role: 'group', 'aria-labelledby': 'meeting-ask', 'data-meeting-statements': '' });
  const unavailable = h('p', { class: 'chat__notice', hidden: true, 'data-meeting-unavailable': '' });
  const restart = h('button', { type: 'button', class: 'textbtn meeting__restart', 'data-meeting-restart': '', hidden: true }, icon('restart', 15), h('span', {}));
  const foot = h('div', { class: 'meeting__foot' }, askLabel, mutedNote, statements, unavailable, restart);

  const table = h('div', { class: 'meeting__table', 'data-meeting-tafel': '' },
    tableCard,
    h('div', { class: 'meeting__table-meta' }, tableName, soundButton),
  );
  const meeting = h('div', { class: 'meeting' },
    table,
    h('div', { class: 'meeting__main' },
      scroller,
      announcer,
      foot,
    ),
  );
  root.replaceChildren(meeting);

  // De hoogte van de tafelstrook, voor de CSS: op een smal scherm bedekt zij
  // samen met de kop de bovenkant van het scherm, en het verslag houdt die
  // ruimte vrij voor de toetsenbordfocus (scroll-margin-top).
  new ResizeObserver(() => {
    meeting.style.setProperty('--tafel-h', `${Math.ceil(table.getBoundingClientRect().height)}px`);
  }).observe(table);

  function setSound(on) {
    sound.set(on);
    if (!on) audio.stop();
    // Gaat het geluid aan terwijl een reactie nog komt, dan ontgrendelt deze
    // klik haar fragment alvast (Safari).
    else if (run?.src) audio.prime(run.src);
    renderSound();
  }
  soundButton.addEventListener('click', () => setSound(!sound.on()));
  // De regel verdwijnt na de klik; de focus gaat naar de schakelaar, die nu
  // "aan" zegt.
  turnOn.addEventListener('click', () => {
    setSound(true);
    soundButton.focus();
  });
  restart.addEventListener('click', () => {
    stopRun();
    said.length = 0;
    renderLog();
    refreshStatements();
    statements.querySelector('button')?.focus();
  });

  const statement = (id) => data?.statements.find((s) => s.id === id) || null;
  const nameThe = (id) => copy.pigeons[id].nameThe;

  function stopRun() {
    if (run) {
      run.controller.abort();
      run = null;
    }
    audio.stop();
    stage?.stop();
  }

  // ---- opbouw ---------------------------------------------------------------

  function renderSound() {
    const on = sound.on();
    // De zichtbare tekst ("Geluid aan") is de naam; aria-checked draagt de stand.
    soundButton.setAttribute('aria-checked', String(on));
    soundButton.title = copy.meeting.sound;
    soundButton.querySelector('.soundbtn__icon').replaceChildren(icon(on ? 'soundOn' : 'soundOff', 17));
    soundLabel.textContent = copy.meeting.soundLabel;
    soundState.textContent = on ? copy.meeting.soundOn : copy.meeting.soundOff;
    mutedNote.hidden = on || !data?.statements.length;
    mutedNote.firstChild.replaceChildren(icon('soundOff', 15));
    mutedNote.children[1].textContent = copy.meeting.soundOffNote;
    turnOn.textContent = copy.meeting.soundTurnOn;
    log.querySelectorAll('[data-listen]').forEach((b) => { b.disabled = !on; });
  }

  // De knoppen worden één keer per taal gebouwd en daarna ter plekke bijgewerkt,
  // zodat de focus op de aangeklikte uitspraak blijft staan.
  function renderStatements() {
    const list = data?.statements || [];
    statements.replaceChildren(...list.map((s) => h('button', {
      type: 'button',
      class: 'stmt',
      'data-statement': s.id,
      onclick: () => say(s.id),
    },
    h('span', { class: 'stmt__speaker' }, s.speaker[lang]),
    h('span', { class: 'stmt__text' }, s.text[lang]),
    h('span', { class: 'sr-only', 'data-said-note': '' }))));
    statements.hidden = !list.length;
    askLabel.hidden = !list.length;
    unavailable.hidden = Boolean(list.length) || status === 'laden';
    unavailable.textContent = copy.meeting.unavailable;
    // Tijdens het laden staat er nog niets om te kiezen: dan ook geen lege voet.
    foot.hidden = !list.length && status === 'laden';
    intro.hidden = !list.length;
    restart.lastChild.textContent = copy.meeting.restart;
    refreshStatements();
  }

  function refreshStatements() {
    statements.querySelectorAll('[data-statement]').forEach((button) => {
      const done = said.includes(button.dataset.statement);
      button.classList.toggle('is-said', done);
      button.setAttribute('aria-disabled', String(done));
      button.querySelector('[data-said-note]').textContent = done ? ` (${copy.meeting.said})` : '';
    });
    restart.hidden = !said.length;
    root.dataset.busy = String(Boolean(run));
  }

  function renderLog() {
    log.replaceChildren();
    if (!said.length && status === 'laden') log.append(h('p', { class: 'chat__loading', 'data-meeting-loading': '' }, copy.meeting.loading));
    // De lege-staattekst noemt de uitspraken hieronder; zonder uitspraken zwijgt hij.
    if (!said.length && data?.statements.length) log.append(h('p', { class: 'meeting__empty' }, copy.meeting.empty));
    for (const id of said) {
      const s = statement(id);
      if (!s) continue;
      log.append(saidRow(s));
      const r = s.reactions[lang][pigeon];
      if (!r) log.append(h('p', { class: 'chat__note' }, copy.meeting.missing));
      else if (r.type !== 'silence') log.append(reactionRow(r));
    }
    renderSound();
    toBottom(true);
  }

  function saidRow(s) {
    return h('div', { class: 'mrow mrow--said', 'data-said': s.id },
      h('p', { class: 'mrow__speaker' }, s.speaker[lang]),
      h('p', { class: 'mrow__text' }, s.text[lang]),
    );
  }

  function reactionRow(r, live = false) {
    if (r.type === 'signal') {
      // Geen tekst, geen label: een neutrale, stilstaande markering. Alleen
      // tijdens het signaal zelf, en niet bij minder beweging, klopt hij even.
      return h('div', { class: `mrow mrow--signal${live && !prefersReducedMotion() ? ' is-live' : ''}`, 'data-reaction': 'signal', 'data-author': pigeon },
        avatar(pigeon, { class: 'msg__avatar', width: 28, height: 28 }),
        h('span', { class: 'signal-mark', 'aria-hidden': 'true' }, icon('signal', 22)),
        h('span', { class: 'sr-only' }, fill(copy.meeting.signal, { nameThe: nameThe(pigeon) })),
      );
    }
    const text = h('div', { class: 'msg__text' });
    renderCited(text, r.text, {
      sources: r.sources,
      label: (n) => fill(copy.chat.cite, { n }),
      onCite: (n) => openSources({ sources: r.sources, author: pigeon, focus: n }),
    });
    const foot = h('div', { class: 'msg__foot' });
    if (r.sources.length) {
      const label = r.sources.length === 1 ? copy.chat.sourcesOne : fill(copy.chat.sourcesMany, { count: r.sources.length });
      foot.append(h('button', {
        type: 'button',
        class: 'chip chip--sources',
        'aria-haspopup': 'dialog',
        onclick: () => openSources({ sources: r.sources, author: pigeon, focus: r.sources[0].n }),
      }, icon('book', 15), label));
    }
    if (r.audio) {
      const listen = h('button', { type: 'button', class: 'chip chip--listen', 'data-listen': '' }, h('span', { class: 'chip__icon' }), h('span', {}));
      const setState = (playing) => {
        listen.querySelector('.chip__icon').replaceChildren(icon(playing ? 'stop' : 'play', 14));
        listen.lastChild.textContent = playing ? copy.meeting.stop : copy.meeting.listen;
        listen.dataset.playing = String(playing);
      };
      setState(false);
      listen.addEventListener('click', () => {
        if (listen.dataset.playing === 'true') audio.stop();
        else if (sound.on()) audio.play(r.audio, { onState: setState });
      });
      listen.disabled = !sound.on();
      listen._setState = setState;
      foot.append(listen);
    }
    return h('div', { class: `msg msg--pigeon mrow mrow--speech${live && !prefersReducedMotion() ? ' is-live' : ''}`, 'data-reaction': 'speech', 'data-author': pigeon },
      h('p', { class: 'msg__author' }, avatar(pigeon, { class: 'msg__avatar', width: 28, height: 28 }), copy.pigeons[pigeon].name),
      h('div', { class: 'msg__bubble' }, text),
      foot.childNodes.length ? foot : null,
    );
  }

  // ---- afspelen -------------------------------------------------------------

  // Een nieuwe uitspraak onderbreekt de lopende reactie (stem, clip) en begint
  // meteen: in een vergadering wacht niemand tot de duif uitgepraat is.
  async function say(id) {
    const s = statement(id);
    if (!s || said.includes(id)) return;
    stopRun();
    // Nog binnen de klik: het fragment van de komende reactie alvast ontgrendelen.
    const next = s.reactions[lang][pigeon];
    const src = next?.type === 'speech' ? next.audio : next?.type === 'signal' ? next.sound : null;
    if (sound.on()) audio.prime(src);
    const controller = new AbortController();
    run = { controller, src };
    said.push(id);
    log.querySelector('.meeting__empty')?.remove();
    log.append(saidRow(s));
    toBottom(true);
    refreshStatements();
    const at = pigeon;
    try {
      const r = s.reactions[lang][at];
      if (!r) {
        log.append(h('p', { class: 'chat__note' }, copy.meeting.missing));
        return;
      }
      await wait(Math.min(r.delayMs, MAX_DELAY), controller.signal);
      if (r.type === 'silence') return;
      const row = reactionRow(r, true);
      log.append(row);
      toBottom(true);
      const tasks = [stage.play(r.gesture, controller.signal)];
      if (r.type === 'speech') {
        announcer.textContent = `${copy.pigeons[at].name}: ${stripCitations(r.text)}`;
        const listen = row.querySelector('[data-listen]');
        if (sound.on() && r.audio) tasks.push(audio.play(r.audio, { onState: listen?._setState }));
      } else {
        announcer.textContent = fill(copy.meeting.signal, { nameThe: nameThe(at) });
        if (sound.on() && r.sound) tasks.push(audio.play(r.sound));
      }
      const longest = r.type === 'speech' && r.audioMs ? r.audioMs + 2000 : 8000;
      await Promise.race([Promise.all(tasks), wait(longest, controller.signal)]);
    } catch (err) {
      if (!isAbort(err)) console.error(err);
    } finally {
      log.querySelectorAll('.is-live').forEach((row) => row.classList.remove('is-live'));
      if (run?.controller === controller) {
        run = null;
        refreshStatements();
      }
    }
  }

  return {
    // status: 'laden' zolang opnames.json onderweg is, 'klaar' of 'ontbreekt'.
    setData(nextData, nextStatus = nextData ? 'klaar' : 'ontbreekt') {
      data = nextData;
      status = nextStatus;
      if (copy) {
        stopRun();
        renderStatements();
        renderLog();
      }
    },
    setCopy(nextCopy, nextLang) {
      stopRun();
      copy = nextCopy;
      lang = nextLang;
      askLabel.textContent = copy.meeting.statements;
      intro.textContent = copy.meeting.intro;
      scroller.setAttribute('aria-label', copy.meeting.log);
      renderTable();
      renderStatements();
      renderLog();
    },
    show(id) {
      stopRun();
      const changed = id !== pigeon || !stage;
      pigeon = id;
      if (changed) seat();
      renderTable();
      refreshStatements();
      renderLog();
    },
    hide() {
      stopRun();
      refreshStatements();
      renderLog();
    },
    // Loopt er een reactie of klinkt er een stem (ook via Beluister)?
    busy: () => Boolean(run) || audio.isPlaying(),
  };

  function renderTable() {
    tableName.textContent = fill(copy.meeting.atTable, { name: copy.pigeons[pigeon].name });
  }

  // Het podium aan tafel: dezelfde plaat en duivenlaag als de kaart.
  function seat() {
    stage?.stop();
    stage = createStage(pigeon);
    tableCard.dataset.pigeon = pigeon;
    tableCard.replaceChildren(...stage.nodes);
    meeting.style.setProperty('--card-aspect', String(cardAspect()));
    meeting.style.setProperty('--breakout', String(breakout()));
    meeting.style.setProperty('--sideout', String(sideout()));
  }
}
