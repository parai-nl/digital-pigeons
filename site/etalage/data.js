// Laadt de twee bestanden van de opname- en de beeldstroom en controleert ze tegen het
// contract (tools/etalage-opnames/README.md en tools/etalage-beeld/README.md).
// Wat niet klopt, valt weg in plaats van de pagina te breken: een ontbrekend
// of kapot bestand wordt `null`, een kapotte beurt of reactie wordt
// overgeslagen. De pagina toont dan een menselijke melding; de reden
// staat alleen in de console.
//
// opnames.json — opnamestroom: echte, vooraf opgenomen modeloutput.
// manifest.json — beeldstroom: platen, rustbeelden, clips, avatars, credits.

export const OPNAMES_URL = 'etalage/data/opnames.json';
export const MANIFEST_URL = 'etalage/media/duiven/manifest.json';

const PIGEONS = ['kennispartner', 'stakeholder', 'signalerend'];
const TALKING = ['kennispartner', 'stakeholder'];
const AUTHORS = ['facilitator', ...TALKING];
const PHASES = ['facilitator', 'pigeon_grounding', 'pigeon_related_search', 'pigeon', 'mediation'];
const KINDS = ['grounded', 'gap', 'standpoint'];
const REACTIONS = ['speech', 'signal', 'silence'];
const LANGS = ['nl', 'en'];

const isObj = (v) => v !== null && typeof v === 'object' && !Array.isArray(v);
const isStr = (v) => typeof v === 'string' && v.trim() !== '';
const isNum = (v) => typeof v === 'number' && Number.isFinite(v);

function warn(what) {
  console.warn(`[etalage] ${what} overgeslagen`);
}

// Media staan altijd onder de eigen map; nooit een ander domein, nooit omhoog.
export function safeMedia(path) {
  return typeof path === 'string' && /^etalage\/media\/[\w\-./]+$/.test(path) && !path.includes('..') ? path : null;
}

// Links naar een paper: alleen https.
export function safeHttps(url) {
  if (typeof url !== 'string') return null;
  try {
    const parsed = new URL(url);
    return parsed.protocol === 'https:' && !parsed.username && !parsed.password ? parsed.href : null;
  } catch {
    return null;
  }
}

// Een bestand dat niet binnen acht seconden komt, telt als ontbrekend: de
// pagina wacht er niet langer op.
async function getJson(url) {
  const signal = typeof AbortSignal.timeout === 'function' ? AbortSignal.timeout(8000) : undefined;
  const res = await fetch(url, { cache: 'no-cache', signal });
  if (!res.ok) throw new Error(`${url} gaf ${res.status}`);
  return res.json();
}

// De twee bestanden laden los van elkaar: het kleine manifest bepaalt het beeld
// van kop en waaier en komt eerst; de opnames (±350 KB) vullen daarna het gesprek.
export const loadManifest = () => load(MANIFEST_URL, readManifest, 'manifest.json');
export const loadOpnames = () => load(OPNAMES_URL, readOpnames, 'opnames.json');

async function load(url, reader, name) {
  let raw;
  try {
    raw = await getJson(url);
  } catch (err) {
    console.info(`[etalage] ${name} niet geladen: ${err?.message || err}`);
    return null;
  }
  try {
    return reader(raw);
  } catch (err) {
    console.warn(`[etalage] ${name} klopt niet met het contract: ${err.message}`);
    return null;
  }
}

// ---- opnames.json -----------------------------------------------------------

export function readOpnames(raw) {
  if (!isObj(raw) || raw.schema !== 1) throw new Error('schema is niet 1');
  const recording = readRecording(raw.recording);
  const facts = {};
  if (isObj(raw.facts)) {
    for (const [key, value] of Object.entries(raw.facts)) if (isNum(value) && value >= 0) facts[key] = value;
  }
  const questions = (Array.isArray(raw.chat?.questions) ? raw.chat.questions : [])
    .map(readQuestion)
    .filter(Boolean);
  const statements = (Array.isArray(raw.meeting?.statements) ? raw.meeting.statements : [])
    .map(readStatement)
    .filter(Boolean);
  return { recording, facts, questions, statements };
}

function readRecording(r) {
  if (!isObj(r)) return {};
  return {
    model: isStr(r.model) ? r.model : null,
    recordedFrom: isStr(r.recordedFrom) ? r.recordedFrom : null,
    recordedTo: isStr(r.recordedTo) ? r.recordedTo : null,
    knowledgeBase: isStr(r.knowledgeBase) ? r.knowledgeBase : null,
    placeholder: r.placeholder === true,
  };
}

function readText(t) {
  if (!isObj(t) || !LANGS.every((l) => isStr(t[l]))) return null;
  return { nl: t.nl, en: t.en };
}

function readQuestion(q) {
  const text = readText(q?.text);
  if (!isStr(q?.id) || !text) return warn(`vraag ${q?.id ?? '?'}`), null;
  const turns = { nl: {}, en: {} };
  for (const lang of LANGS) {
    const byKey = isObj(q.turns?.[lang]) ? q.turns[lang] : {};
    for (const [key, turn] of Object.entries(byKey)) {
      if (!isTurnKey(key)) continue;
      const read = readTurn(turn, key);
      if (read) turns[lang][key] = read;
      else warn(`beurt ${q.id}/${lang}/${key}`);
    }
  }
  return { id: q.id, text, turns };
}

function isTurnKey(key) {
  if (TALKING.includes(key)) return true;
  const [from, to] = key.split('>');
  return TALKING.includes(from) && TALKING.includes(to) && from !== to;
}

// Een beurt hoort bij één duif: de sleutel zelf, of bij een vergelijking het
// deel na '>'. Spreekt er een andere duif in, dan zou haar tekst onder de
// verkeerde naam staan; zo'n beurt valt weg en de pagina zegt dat eerlijk.
function readTurn(turn, key) {
  if (!isObj(turn) || !Array.isArray(turn.messages)) return null;
  const owner = key.includes('>') ? key.split('>')[1] : key;
  const messages = turn.messages.map(readMessage).filter(Boolean);
  if (!messages.some((m) => m.author !== 'facilitator')) return null;
  if (messages.some((m) => m.author !== 'facilitator' && m.author !== owner)) return null;
  const phases = (Array.isArray(turn.phases) ? turn.phases : []).filter((p) => PHASES.includes(p));
  return { take: isStr(turn.take) ? turn.take : '', phases, messages };
}

function readMessage(m) {
  if (!isObj(m) || !AUTHORS.includes(m.author) || !isStr(m.text)) return null;
  if (m.author === 'facilitator') return { author: 'facilitator', text: m.text };
  return {
    author: m.author,
    text: m.text,
    kind: KINDS.includes(m.kind) ? m.kind : 'grounded',
    // Verwante bronnen na een kennisgat: de chip zegt dat erbij.
    related: m.related === true,
    sources: readSources(m.sources),
    chunks: readChunks(m.chunks, m.text),
  };
}

// De brokken moeten samen precies de tekst zijn; anders speelt de pagina de
// tekst gelijkmatig af in plaats van iets te tonen dat niet opgenomen is.
function readChunks(chunks, text) {
  if (!Array.isArray(chunks) || !chunks.length) return null;
  let last = -1;
  const out = [];
  for (const c of chunks) {
    if (!Array.isArray(c) || !isNum(c[0]) || typeof c[1] !== 'string' || c[0] < last) return null;
    last = c[0];
    out.push([c[0], c[1]]);
  }
  return out.map((c) => c[1]).join('') === text ? out : null;
}

function readSources(list) {
  if (!Array.isArray(list)) return [];
  return list
    .map((s) => {
      if (!isObj(s) || !Number.isInteger(s.n) || !isStr(s.title)) return null;
      const lang = isStr(s.passageLang) ? s.passageLang : '';
      // Meerdere passages als de module die toont; anders de ene passage.
      const passages = Array.isArray(s.passages)
        ? s.passages.filter((p) => isObj(p) && isStr(p.text)).map((p) => ({ text: p.text, lang: isStr(p.lang) ? p.lang : lang }))
        : [];
      if (!passages.length && isStr(s.passage)) passages.push({ text: s.passage, lang });
      return {
        n: s.n,
        title: s.title,
        authors: isStr(s.authors) ? s.authors : '',
        year: isNum(s.year) || isStr(s.year) ? String(s.year) : '',
        url: safeHttps(s.url),
        lang,
        passages,
      };
    })
    .filter(Boolean)
    .sort((a, b) => a.n - b.n);
}

function readStatement(s) {
  const text = readText(s?.text);
  const speaker = readText(s?.speaker);
  if (!isStr(s?.id) || !text || !speaker) return warn(`uitspraak ${s?.id ?? '?'}`), null;
  const reactions = { nl: {}, en: {} };
  for (const lang of LANGS) {
    const byPigeon = isObj(s.reactions?.[lang]) ? s.reactions[lang] : {};
    for (const pigeon of PIGEONS) {
      if (!(pigeon in byPigeon)) continue;
      const read = readReaction(byPigeon[pigeon], pigeon);
      if (read) reactions[lang][pigeon] = read;
      else warn(`reactie ${s.id}/${lang}/${pigeon}`);
    }
  }
  return { id: s.id, text, speaker, reactions };
}

function readReaction(r, pigeon) {
  if (!isObj(r) || !REACTIONS.includes(r.type)) return null;
  // De Signalerende duif spreekt nooit: een gesproken reactie van haar
  // wordt niet getoond.
  if (pigeon === 'signalerend' && r.type === 'speech') return null;
  const base = {
    take: isStr(r.take) ? r.take : '',
    type: r.type,
    delayMs: isNum(r.delayMs) && r.delayMs >= 0 ? r.delayMs : 0,
    gesture: isStr(r.gesture) ? r.gesture : null,
  };
  if (r.type === 'speech') {
    if (!isStr(r.text)) return null;
    return {
      ...base,
      text: r.text,
      sources: readSources(r.sources),
      audio: safeMedia(r.audio),
      audioMs: isNum(r.audioMs) && r.audioMs > 0 ? r.audioMs : null,
    };
  }
  if (r.type === 'signal') return { ...base, sound: safeMedia(r.sound) };
  return base;
}

// ---- manifest.json ----------------------------------------------------------

export function readManifest(raw) {
  if (!isObj(raw) || raw.schema !== 1 || !isObj(raw.pigeons)) throw new Error('schema is niet 1');
  const aspect = isNum(raw.card?.aspect) && raw.card.aspect > 0.4 && raw.card.aspect < 1.6 ? raw.card.aspect : 0.82;
  const pigeons = {};
  for (const id of PIGEONS) {
    const p = readManifestPigeon(raw.pigeons[id]);
    if (p) pigeons[id] = p;
    else if (id in raw.pigeons) warn(`beeld van ${id}`);
  }
  if (!Object.keys(pigeons).length) throw new Error('geen bruikbare duif');
  const credits = (Array.isArray(raw.credits) ? raw.credits : [])
    .filter((c) => isObj(c) && isStr(c.nl) && isStr(c.en))
    .map((c) => ({
      nl: c.nl,
      en: c.en,
      url: safeHttps(c.url),
      // Optioneel: de licentie apart, met een eigen link.
      license: isStr(c.license) ? c.license : null,
      licenseUrl: safeHttps(c.licenseUrl),
    }));
  const model = isObj(raw.model) ? { version: isStr(raw.model.version) ? raw.model.version : '', final: raw.model.final === true } : null;
  return { aspect, pigeons, credits, model };
}

function readImage(img) {
  const src = safeMedia(img?.src);
  if (!src) return null;
  return { src, width: isNum(img.width) ? img.width : null, height: isNum(img.height) ? img.height : null };
}

function readManifestPigeon(p) {
  if (!isObj(p)) return null;
  const plate = readImage(p.plate);
  const rest = readImage(p.rest);
  const l = p.layer;
  const layerOk = isObj(l) && ['left', 'top', 'width', 'height'].every((k) => isNum(l[k])) && l.width > 0 && l.height > 0;
  if (!plate || !rest || !layerOk) return null;
  const clips = {};
  if (isObj(p.clips)) {
    for (const [name, c] of Object.entries(p.clips)) {
      const webm = safeMedia(c?.webm);
      const hevc = safeMedia(c?.hevc);
      if (webm || hevc) clips[name] = { webm, hevc, ms: isNum(c.ms) && c.ms > 0 ? c.ms : 2000 };
    }
  }
  return {
    plate,
    rest,
    layer: { left: l.left, top: l.top, width: l.width, height: l.height },
    clips,
    defaultClip: isStr(p.defaultClip) && clips[p.defaultClip] ? p.defaultClip : Object.keys(clips)[0] || null,
    avatar: safeMedia(p.avatar?.src),
    // Optioneel, als de duiven elk een eigen model hebben (v4).
    model: isObj(p.model) && isStr(p.model.version) ? { file: isStr(p.model.file) ? p.model.file : '', version: p.model.version } : null,
  };
}
