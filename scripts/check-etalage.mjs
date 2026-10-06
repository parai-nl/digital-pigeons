// Controles op de etalage zonder browser:
//   1. NL en EN hebben precies dezelfde sleutels en vormen, en geen lege tekst.
//   2. De pagina blijft offline: geen script, stylesheet, letter of beeld van een
//      andere oorsprong; alleen gewone links mogen naar buiten.
//   3. Geen microfoon, geen andere opslag dan de keuzes voor taal, geluid en
//      licht/donker, geen HTML uit data, geen runtime van het ontwerpprototype.
//   4. Geen testtekst ("[fixture]") in site/, en elk lokaal pad in de HTML bestaat.
//   5. De hosting-eisen: alleen index.html en etalage/,
//      toegestane bestandstypen, geen dotfiles of symlinks, en niets wat de CSP
//      tegenhoudt (inline script of stijl, style-attributen, blob:-media, eval).
//
// Gebruik: node scripts/check-etalage.mjs
import { readFileSync, readdirSync, lstatSync, existsSync } from 'node:fs';
import path from 'node:path';

const root = path.resolve(import.meta.dirname, '..');
const site = path.join(root, 'site');
let failures = 0;
const fail = (msg) => {
  failures += 1;
  console.error(`FOUT: ${msg}`);
};

const links = [];
function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const full = path.join(dir, name);
    const info = lstatSync(full);
    if (info.isSymbolicLink()) return links.push(full), [];
    return info.isDirectory() ? [full + path.sep, ...walk(full)] : [full];
  });
}
const entries = walk(site);
const files = entries.filter((f) => !f.endsWith(path.sep));
const rel = (file) => path.relative(root, file);
const text = (file) => readFileSync(file, 'utf8');
const TEXT_EXT = /\.(html|js|css|json|md|txt|vtt|svg)$/;

// ---- 1. taalpariteit --------------------------------------------------------

const { COPY } = await import(path.join(site, 'etalage/i18n.js'));
const SAMPLE = { publications: '118', works: '118', sources: '120', claims: '4.067', claimsWithPassage: '4.067', passages: '4.225', subthemes: '149', themes: '26', domains: '3', perspectives: '297' };
const CONTEXT = { facts: SAMPLE, recording: { model: 'vertex_ai/gemini-3.5-flash', month: 'september 2026' } };
const EMPTY_CONTEXT = { facts: {}, recording: {} };

function shape(value, at) {
  if (typeof value === 'function') {
    for (const arg of [SAMPLE, CONTEXT, {}, EMPTY_CONTEXT]) {
      let out;
      try { out = value(arg); } catch { continue; }
      if (typeof out !== 'string') fail(`${at} geeft geen tekst terug`);
    }
    return 'fn';
  }
  if (Array.isArray(value)) return value.map((v, i) => shape(v, `${at}[${v?.id ?? i}]`));
  if (value && typeof value === 'object') {
    return Object.fromEntries(Object.keys(value).sort().map((k) => [k, shape(value[k], `${at}.${k}`)]));
  }
  return typeof value;
}
const nlShape = JSON.stringify(shape(COPY.nl, 'nl'));
const enShape = JSON.stringify(shape(COPY.en, 'en'));
if (nlShape !== enShape) fail('NL en EN hebben niet dezelfde sleutels of vorm (i18n.js)');

function strings(value, at, out = []) {
  if (typeof value === 'string') out.push([at, value]);
  else if (typeof value === 'function') {
    const res = (() => { try { return value(SAMPLE); } catch { return value(CONTEXT); } })();
    if (typeof res === 'string') out.push([at, res]);
  } else if (value && typeof value === 'object') {
    for (const [k, v] of Object.entries(value)) strings(v, `${at}.${k}`, out);
  }
  return out;
}
for (const lang of ['nl', 'en']) {
  for (const [at, s] of strings(COPY[lang], lang)) {
    const allowedEmpty = at.endsWith('.signalerend.placeholder');
    if (!s.trim() && !allowedEmpty) fail(`${at} is leeg`);
    if (/[—]/.test(s)) fail(`${at} bevat een gedachtestreepje (—); schrijf de zin om`);
    if (/\[fixture\]/i.test(s)) fail(`${at} bevat testtekst`);
    if (/\s{2,}/.test(s.replace(/\n/g, ' ').trim())) fail(`${at} bevat een dubbele spatie`);
  }
}
// De vaste publieksnamen van de duiven.
const NAMES = { nl: ['Kennispartner', 'Stakeholder', 'Signalerende duif'], en: ['Knowledge Partner', 'Stakeholder', 'Signaling Pigeon'] };
for (const lang of ['nl', 'en']) {
  const got = ['kennispartner', 'stakeholder', 'signalerend'].map((id) => COPY[lang].pigeons[id].name);
  if (JSON.stringify(got) !== JSON.stringify(NAMES[lang])) fail(`${lang}: duivennamen wijken af van de vaste namen: ${got.join(', ')}`);
}
if (COPY.nl.faq.items.length !== 7 || COPY.en.faq.items.length !== 7) fail('de FAQ heeft niet zeven vragen');
if (COPY.nl.method.items.length !== 6) fail('de methode heeft niet zes onderwerpen');

// ---- 2. offline --------------------------------------------------------------

// Links naar buiten mogen, als link. Deze bestanden mogen daarom een https-adres
// dragen; nergens anders staat er een.
const URL_ALLOWED = new Set(['site/etalage/config.js', 'site/etalage/pigeons.js', 'site/index.html']);
for (const file of files.filter((f) => TEXT_EXT.test(f))) {
  const body = text(file);
  const name = rel(file);
  if (/\.css$/.test(file)) {
    if (/@import\s+(url\()?["']?https?:/i.test(body)) fail(`${name} importeert een externe stylesheet`);
    if (/url\(\s*["']?(https?:)?\/\//i.test(body)) fail(`${name} laadt iets van een andere oorsprong`);
  }
  if (/\.html$/.test(file)) {
    for (const m of body.matchAll(/<(script|link|img|source|video|audio|track|iframe)\b[^>]*\s(src|href|srcset)=["']([^"']+)["']/gi)) {
      const [, tag, , value] = m;
      if (/^(https?:)?\/\//i.test(value)) fail(`${name}: <${tag}> laadt ${value}`);
      if (value.startsWith('/')) fail(`${name}: <${tag}> verwijst absoluut naar ${value}; gebruik een relatief pad`);
    }
    for (const m of body.matchAll(/\s(?:src|href|srcset)=["']([^"'#?]+)[^"']*["']/gi)) {
      const value = m[1];
      if (/^(https?:|mailto:|data:)/i.test(value) || value === './' || value.endsWith('/')) continue;
      const target = path.resolve(path.dirname(file), value);
      if (!existsSync(target)) fail(`${name}: ${value} bestaat niet`);
    }
  }
  if (/\.js$/.test(file)) {
    const urls = body.match(/https?:\/\/[^\s'"`)]+/g) || [];
    if (urls.length && !URL_ALLOWED.has(name)) fail(`${name} bevat een adres naar buiten: ${urls[0]}`);
    if (/\bfetch\(\s*['"`]https?:/.test(body)) fail(`${name} haalt iets van een andere oorsprong`);
    if (/import\s*\(?\s*['"]https?:/.test(body)) fail(`${name} importeert van een andere oorsprong`);
  }
}

// ---- 3. gedrag ---------------------------------------------------------------

// Opslag gaat via prefs.js. Alleen theme-early.js leest daarnaast, vóór de
// eerste verf, de themasleutel (zie hieronder).
const STORAGE = new Set(['site/etalage/prefs.js', 'site/etalage/theme-early.js']);
for (const file of files.filter((f) => /\.(js|html)$/.test(f))) {
  const body = text(file);
  const name = rel(file);
  if (/getUserMedia|mediaDevices|webkitSpeechRecognition|SpeechRecognition/.test(body)) fail(`${name} gebruikt de microfoon`);
  if (/sessionStorage|indexedDB|document\.cookie/.test(body)) fail(`${name} gebruikt andere opslag dan localStorage`);
  if (/localStorage/.test(body) && !STORAGE.has(name)) fail(`${name} gebruikt localStorage buiten prefs.js`);
  if (/innerHTML|outerHTML|insertAdjacentHTML/.test(body) && name !== 'site/etalage/icons.js') fail(`${name} zet HTML als tekst in de pagina`);
  if (/support\.js|image-slot|_ds_bundle|DCLogic/.test(body)) fail(`${name} verwijst naar de runtime van het ontwerpprototype`);
  if (/\beval\(|new Function\(/.test(body)) fail(`${name} voert tekst uit als code`);
}
const prefs = text(path.join(site, 'etalage/prefs.js'));
const keys = [...prefs.matchAll(/_KEY\s*=\s*'([^']+)'/g)].map((m) => m[1]).sort();
if (JSON.stringify(keys) !== JSON.stringify(['duif_etalage_geluid', 'duif_etalage_lang', 'duif_etalage_thema', 'mode-watcher-mode'])) fail(`prefs.js bewaart andere sleutels: ${keys.join(', ')}`);
// theme-early.js leest alleen de twee themasleutels, en schrijft niets.
const early = text(path.join(site, 'etalage/theme-early.js'));
const earlyUse = [...early.matchAll(/localStorage\.(\w+)\(([^)]*)\)/g)].map((m) => `${m[1]}(${m[2]})`);
if (JSON.stringify(earlyUse) !== JSON.stringify(["getItem('mode-watcher-mode')", "getItem('duif_etalage_thema')"])) fail(`theme-early.js leest andere opslag dan de themakeuze: ${earlyUse.join(', ')}`);
if (/setItem|removeItem|clear\(|localStorage\s*(\[|=[^=])|=\s*(window\.)?localStorage\s*[;,)\n]/.test(early)) fail('theme-early.js schrijft in de opslag of geeft haar door');

// ---- 4. geen testtekst -------------------------------------------------------

for (const file of files.filter((f) => TEXT_EXT.test(f))) {
  if (/\[fixture\]/i.test(text(file))) fail(`${rel(file)} bevat testtekst ([fixture])`);
}
for (const file of files) {
  if (/fixture/i.test(path.basename(file))) fail(`${rel(file)} is een testbestand in site/`);
}

// ---- 5. hosting en CSP --------------------------------------------------------

// pdf: de ontworpen posters en kaartjes om te downloaden (de webserver typeert .pdf zelf als application/pdf).
const ALLOWED = /\.(html|js|css|json|svg|png|jpg|webp|webm|mov|mp4|mp3|woff2|vtt|pdf)$/;
const top = readdirSync(site).sort();
if (JSON.stringify(top) !== JSON.stringify(['etalage', 'index.html'])) fail(`site/ bevat meer dan index.html en etalage/: ${top.join(', ')}`);
for (const link of links) fail(`${rel(link)} is een symlink`);
for (const entry of entries) {
  if (path.basename(entry.replace(/\/$/, '')).startsWith('.')) fail(`${rel(entry)} is een dotfile`);
}
for (const file of files) {
  if (!ALLOWED.test(file)) fail(`${rel(file)} heeft een bestandstype dat de hosting niet serveert`);
}
for (const file of files.filter((f) => /\.(js|html|css)$/.test(f))) {
  const body = text(file);
  const name = rel(file);
  if (/\.html$/.test(file)) {
    if (/<script(?![^>]*\ssrc=)[^>]*>/i.test(body)) fail(`${name} bevat een inline script`);
    if (/<style[\s>]/i.test(body)) fail(`${name} bevat een <style>-blok`);
    if (/\sstyle=["']/i.test(body)) fail(`${name} bevat een style-attribuut`);
  }
  if (/\.js$/.test(file)) {
    if (/setAttribute\(\s*['"]style['"]|\.style\.cssText|style=["'][^"']/.test(body)) fail(`${name} zet een style-attribuut`);
    if (/createObjectURL|['"`]blob:|data:(audio|video)/.test(body)) fail(`${name} maakt blob:- of data:-media`);
  }
  if (/['"(]\/(_app|images|modules|api)\//.test(body)) fail(`${name} verwijst naar een pad van de module`);
}

const index = text(path.join(site, 'index.html'));
// De statische beschrijving (voor wie de pagina zonder script leest of deelt) is
// dezelfde tekst als in i18n.js.
for (const key of ['description', 'og:description', 'twitter:description', 'og:title', 'twitter:title']) {
  const m = index.match(new RegExp(`<meta\\s+(?:name|property)="${key}"\\s+content="([^"]*)"`));
  const want = key.endsWith('title') ? COPY.nl.meta.title : COPY.nl.meta.description;
  if (!m) fail(`index.html mist <meta ${key}>`);
  else if (m[1] !== want) fail(`index.html: ${key} wijkt af van i18n.js`);
}

// Een gedeelde link toont het deelbeeld: een absoluut adres onder /etalage/ van
// pigeon.parai.nl, naar een bestand dat er echt staat.
const shareImages = [...index.matchAll(/<meta\s+(?:property|name)=["'](?:og:image|twitter:image)["']\s+content=["']([^"']+)["']/gi)].map((m) => m[1]);
if (shareImages.length !== 2) fail('index.html heeft niet precies één og:image en één twitter:image');
for (const url of shareImages) {
  const local = url.replace(/^https:\/\/pigeon\.parai\.nl\/etalage\//, 'etalage/');
  if (local === url) fail(`deelbeeld ${url} staat niet onder https://pigeon.parai.nl/etalage/`);
  else if (!existsSync(path.join(site, local))) fail(`deelbeeld ${url} bestaat niet in site/`);
}

if (failures) {
  console.error(`\ncheck-etalage: ${failures} fout(en)`);
  process.exit(1);
}
console.log(`check-etalage: in orde (${files.length} bestanden in site/, NL en EN gelijk, hosting- en CSP-regels)`);
