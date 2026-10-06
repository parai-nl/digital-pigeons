// Gedeelde hulp voor de etalagetests. De pagina haalt twee bestanden op die van
// andere werkstromen zijn (opnames.json, manifest.json); de tests spuiten
// fixtures in met page.route, zodat de suite niet op hun levering wacht. Alle
// fixturetekst begint met "[fixture]".
//
// Teksten van de pagina zelf komen uit site/etalage/i18n.js: een copywijziging
// maakt de suite niet rood, een verdwenen of verwisselde tekst wel.
import { test as base, expect } from '@playwright/test';
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';

// Elke test draait onder de CSP en Permissions-Policy van de hosting (de server
// stuurt ze mee, zie playwright.config.js) en faalt bij elke overtreding: een
// securitypolicyviolation-gebeurtenis of een melding in de console.
export const test = base.extend({
  page: async ({ page }, use) => {
    const problems = [];
    await page.addInitScript(() => {
      window.__policyViolations = [];
      document.addEventListener('securitypolicyviolation', (event) => {
        window.__policyViolations.push(`${event.violatedDirective} ${event.blockedURI}`);
      });
    });
    page.on('console', (message) => {
      const text = message.text();
      if (/Content Security Policy|Refused to|Permissions policy/i.test(text)) problems.push(text);
    });
    await use(page);
    try {
      problems.push(...(await page.evaluate(() => window.__policyViolations || [])));
    } catch { /* pagina al dicht */ }
    expect(problems, 'CSP- of Permissions-Policy-overtredingen').toEqual([]);
  },
});
export { expect };

export { COPY, fill } from '../../site/etalage/i18n.js';
export { CONFIG } from '../../site/etalage/config.js';

const FIXTURES = path.join(import.meta.dirname, 'fixtures');
export const OPNAMES = JSON.parse(readFileSync(path.join(FIXTURES, 'opnames.json'), 'utf8'));
export const MANIFEST = JSON.parse(readFileSync(path.join(FIXTURES, 'manifest.json'), 'utf8'));

const MEDIA = {
  'plaat.webp': 'plaat.webp',
  'rust.webp': 'rust.webp',
  'kop.webp': 'kop.webp',
  'peck.webm': 'clip.webm',
  'ruffle.webm': 'clip.webm',
};

// Het echte beeld van de beeldstroom: standaard site/etalage/media/duiven als
// daar een manifest.json staat. ETALAGE_BEELD_DIR (een map met manifest.json en
// de mappen per duif) gaat daar als override voor. Zonder manifest slaan de
// beeldtests over.
const SITE_BEELD = path.join(import.meta.dirname, '../../site/etalage/media/duiven');
export const BEELD_DIR = process.env.ETALAGE_BEELD_DIR
  || (existsSync(path.join(SITE_BEELD, 'manifest.json')) ? SITE_BEELD : '');

/**
 * Opent de etalage.
 * data:     true (fixture), false (404), 'kapot' (geen JSON), 'schema' (verkeerd schema),
 *           'echt' (het opnames.json in site/, met echte stem en gekoer),
 *           of een eigen object (bijvoorbeeld de fixture met een beurt minder)
 * manifest: false (404, terugval op portretten), true (fixture), 'kapot',
 *           'echt' (het echte beeld: BEELD_DIR hierboven)
 * gate:     een Promise; opnames.json komt pas als die vervult (een trage
 *           verbinding). openPage wacht dan op de schil, niet op de data.
 */
export async function openPage(page, { data = true, manifest = false, url = '/', storage = {}, gate = null } = {}) {
  await page.addInitScript((seed) => {
    try {
      for (const [k, v] of Object.entries(seed)) localStorage.setItem(k, v);
    } catch { /* opslag geblokkeerd */ }
    // Na het zaaien meten: alles hierna schreef de pagina zelf.
    window.__played = [];
    window.__storageWrites = [];
    const play = HTMLMediaElement.prototype.play;
    HTMLMediaElement.prototype.play = function patchedPlay() {
      // Een stille start (het ontgrendelen binnen de klik) telt niet als klinken.
      window.__played.push(`${this.muted ? 'stil:' : ''}${this.src || this.currentSrc || ''}`);
      return play.call(this);
    };
    const setItem = Storage.prototype.setItem;
    Storage.prototype.setItem = function patchedSetItem(key, value) {
      window.__storageWrites.push([key, value]);
      return setItem.call(this, key, value);
    };
  }, storage);

  if (data !== 'echt') await page.route('**/etalage/data/opnames.json', async (route) => {
    if (gate) await gate;
    if (data === true) return route.fulfill({ path: path.join(FIXTURES, 'opnames.json') });
    if (data && typeof data === 'object') return route.fulfill({ json: data });
    if (data === 'kapot') return route.fulfill({ body: '{"schema": 1, "chat": ', contentType: 'application/json' });
    if (data === 'schema') return route.fulfill({ json: { schema: 99, chat: { questions: [] } } });
    return route.fulfill({ status: 404, body: 'Not Found' });
  });
  if (manifest === 'echt') {
    await page.route('**/etalage/media/duiven/**', (route) => {
      const rel = new URL(route.request().url()).pathname.replace(/^.*\/etalage\/media\/duiven\//, '');
      return route.fulfill({ path: path.join(BEELD_DIR, rel) });
    });
  } else await page.route('**/etalage/media/duiven/manifest.json', (route) => {
    if (manifest === true) return route.fulfill({ path: path.join(FIXTURES, 'manifest.json') });
    if (manifest === 'kapot') return route.fulfill({ body: 'geen json', contentType: 'application/json' });
    return route.fulfill({ status: 404, body: 'Not Found' });
  });
  if (manifest !== 'echt') await page.route('**/etalage/media/duiven/*/*', (route) => {
    const file = MEDIA[path.basename(new URL(route.request().url()).pathname)];
    return file ? route.fulfill({ path: path.join(FIXTURES, 'media', file) }) : route.fulfill({ status: 404 });
  });
  if (data !== 'echt') await page.route('**/etalage/media/geluid/**', (route) => route.fulfill({ path: path.join(FIXTURES, 'media', 'stem.mp3'), contentType: 'audio/mpeg' }));

  await page.goto(url);
  await expect(page.locator('body')).toHaveAttribute(gate ? 'data-shell' : 'data-ready', 'true');
}

export const played = (page) => page.evaluate(() => window.__played.filter((src) => !src.startsWith('stil:') && src.includes('/etalage/')));
export const primed = (page) => page.evaluate(() => window.__played.filter((src) => src.startsWith('stil:')));
// Clips spelen altijd zonder geluid; voor beeld telt elke start.
export const clips = (page) => page.evaluate(() => window.__played.map((src) => src.replace(/^stil:/, '')).filter((src) => /\.(webm|mov)$/.test(src)));
export const storageWrites = (page) => page.evaluate(() => window.__storageWrites);

export const card = (page, id) => page.locator(`.card[data-pigeon="${id}"]`);
export const slot = (page, id) => page.locator(`[data-slot="${id}"]`);

export async function choosePigeon(page, id) {
  const target = card(page, id);
  if ((await target.getAttribute('aria-current')) === 'true') return;
  await target.click();
  await expect(target).toHaveAttribute('aria-current', 'true');
}

export async function chooseMode(page, mode) {
  const tab = page.locator(`#tab-${mode}`);
  await tab.click();
  await expect(tab).toHaveAttribute('aria-selected', 'true');
}

// Een vraagknop en wachten tot de beurt helemaal klaar is.
export async function ask(page, questionId) {
  await page.locator(`[data-question="${questionId}"]`).click();
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'true');
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
}

export const question = (id) => OPNAMES.chat.questions.find((q) => q.id === id);
export const statement = (id) => OPNAMES.meeting.statements.find((s) => s.id === id);

// De tekst zoals de pagina hem toont: "[4]" wordt een knop met "4".
export const shown = (text) => text.replace(/\[\s*(\d+(?:\s*,\s*\d+)*)\s*\]/g, (_, n) => n.split(/\s*,\s*/).join(''));
