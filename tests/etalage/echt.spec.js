// De etalage met de ECHTE opnames (site/etalage/data/opnames.json van de
// opnamestroom), niet met fixtures: elke vraag, vergelijking en uitspraak in NL
// en EN, de overloop op 360 en 1280 px, en het echte beeld van de beeldstroom
// (site/etalage/media/duiven, of ETALAGE_BEELD_DIR): de duif steekt uit de kaart
// en de clips spelen.
// De CSP-controle uit helpers.js geldt ook hier.
import { existsSync, readFileSync } from 'node:fs';
import path from 'node:path';
import { test, expect, openPage, chooseMode, card, COPY, fill, BEELD_DIR } from './helpers.js';

const FILE = path.join(import.meta.dirname, '../../site/etalage/data/opnames.json');
const REAL = existsSync(FILE) ? JSON.parse(readFileSync(FILE, 'utf8')) : null;
const questions = REAL?.chat.questions ?? [];
const statements = REAL?.meeting.statements ?? [];

test.skip(!REAL, 'geen opnames.json in site/');
test.describe.configure({ timeout: 120_000 });

// Wat de pagina van een duiftekst toont: citaties als losse cijfers, alinea's
// achter elkaar. Voor de vergelijking volstaat het begin, tot de eerste citatie.
const opening = (text) => text.split(/\n\s*\n/)[0].split('[')[0].trim().slice(0, 40);
const owner = (key) => (key.includes('>') ? key.split('>')[1] : key);
const answerOf = (turn) => turn.messages.find((m) => m.author !== 'facilitator');
const chipLabel = (lang, msg) => {
  const c = COPY[lang].chat;
  const n = msg.sources.length;
  if (msg.related) return n === 1 ? c.relatedOne : fill(c.relatedMany, { count: n });
  return n === 1 ? c.sourcesOne : fill(c.sourcesMany, { count: n });
};

async function waitIdle(page, panel = '#panel-chat') {
  await expect(page.locator(panel)).toHaveAttribute('data-busy', 'false', { timeout: 30_000 });
}

for (const lang of ['nl', 'en']) {
  for (const pigeon of ['kennispartner', 'stakeholder']) {
    test(`chat (${lang}, ${pigeon}): elke opgenomen vraag speelt af met de juiste duif en bronnen`, async ({ page }) => {
      await openPage(page, { data: 'echt', url: `/?lang=${lang}` });
      if (pigeon === 'stakeholder') await card(page, 'stakeholder').click();
      await expect(page.locator('[data-question]')).toHaveCount(questions.length);
      for (const [i, q] of questions.entries()) {
        const turn = q.turns[lang][pigeon];
        await page.locator(`[data-question="${q.id}"]`).click();
        await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'true');
        await waitIdle(page);
        const block = page.locator('.chat__thread .turn').nth(i);
        await expect(block.locator('.msg--user .msg__text')).toHaveText(q.text[lang]);
        const msg = answerOf(turn);
        const answer = block.locator(`.msg--pigeon[data-author="${pigeon}"]`);
        await expect(answer.locator('.msg__author')).toHaveText(COPY[lang].pigeons[pigeon].name);
        await expect(answer.locator('.msg__text')).toContainText(opening(msg.text));
        if (msg.sources.length) await expect(answer.locator('.chip--sources')).toHaveText(chipLabel(lang, msg));
        const facilitator = turn.messages.filter((m) => m.author === 'facilitator').length;
        await expect(block.locator('.msg--facilitator')).toHaveCount(facilitator);
      }
      // Het bronpaneel van het laatste antwoord toont alle bronnen, met hun passages.
      const last = answerOf(questions.at(-1).turns[lang][pigeon]);
      if (last.sources.length) {
        await page.locator('.chat__thread .turn').last().locator('.chip--sources').click();
        const dialog = page.getByRole('dialog', { name: COPY[lang].sources.title });
        await expect(dialog.locator('.source')).toHaveCount(last.sources.length);
        const passages = last.sources.reduce((n, s) => n + (s.passages?.length || (s.passage ? 1 : 0)), 0);
        await expect(dialog.locator('blockquote')).toHaveCount(passages);
        await page.keyboard.press('Escape');
      }
    });
  }

  test(`vergelijken (${lang}): herkomst, het antwoord van de eerste duif zonder haar facilitator, en het vergelijkantwoord`, async ({ page }) => {
    await openPage(page, { data: 'echt', url: `/?lang=${lang}` });
    for (const [from, to] of [['kennispartner', 'stakeholder'], ['stakeholder', 'kennispartner']]) {
      const q = questions.find((item) => item.turns[lang][`${from}>${to}`]);
      await card(page, from).click();
      await page.locator(`[data-question="${q.id}"]`).click();
      await waitIdle(page);
      await page.locator('.chat__thread .turn').last().locator('[data-ask-other]').click();
      await expect(card(page, to)).toHaveAttribute('aria-current', 'true');
      await waitIdle(page);
      const block = page.locator('.chat__thread .turn').last();
      await expect(block.locator(`[data-chat-origin="${from}"]`)).toBeVisible();
      await expect(block.locator(`.msg--pigeon[data-author="${from}"] .msg__text`)).toContainText(opening(answerOf(q.turns[lang][from]).text));
      const second = q.turns[lang][`${from}>${to}`];
      expect(owner(`${from}>${to}`)).toBe(to);
      await expect(block.locator(`.msg--pigeon[data-author="${to}"] .msg__text`)).toContainText(opening(answerOf(second).text));
      // Alleen de facilitator van de tweede beurt; die van de eerste reist niet mee.
      await expect(block.locator('.msg--facilitator')).toHaveCount(second.messages.filter((m) => m.author === 'facilitator').length);
    }
  });

  for (const pigeon of ['kennispartner', 'stakeholder', 'signalerend']) {
    test(`vergadering (${lang}, ${pigeon}): elke uitspraak krijgt de opgenomen reactie`, async ({ page }) => {
      await openPage(page, { data: 'echt', url: `/?lang=${lang}` });
      if (pigeon !== 'kennispartner') await card(page, pigeon).click();
      if (pigeon !== 'signalerend') await chooseMode(page, 'meeting');
      for (const s of statements) {
        const r = s.reactions[lang][pigeon];
        await page.locator(`[data-statement="${s.id}"]`).click();
        await expect(page.locator(`[data-said="${s.id}"] .mrow__text`)).toHaveText(s.text[lang]);
        const rows = page.locator(`[data-said="${s.id}"] ~ [data-reaction]`);
        if (r.type === 'speech') {
          const row = page.locator('[data-reaction="speech"]').last();
          await expect(row.locator('.msg__text')).toContainText(opening(r.text), { timeout: 15_000 });
          await expect(row.locator('[data-listen]')).toBeVisible();
        } else if (r.type === 'signal') {
          const row = page.locator('[data-reaction="signal"]').last();
          await expect(row).toBeVisible({ timeout: 15_000 });
          const visible = await row.evaluate((el) => {
            const copy = el.cloneNode(true);
            copy.querySelectorAll('.sr-only').forEach((node) => node.remove());
            return copy.textContent.trim();
          });
          expect(visible).toBe('');
        } else {
          await expect(page.locator('#panel-meeting')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
          await expect(page.locator('.meeting__log > *').last()).toHaveAttribute('data-said', s.id);
        }
        expect(await rows.count()).toBeLessThanOrEqual(1);
      }
      // De Signalerende duif spreekt nooit.
      if (pigeon === 'signalerend') await expect(page.locator('[data-reaction="speech"]')).toHaveCount(0);
    });
  }

  for (const width of [360, 1280]) {
    test(`geen horizontale overloop met echte opnames op ${width} px (${lang})`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await openPage(page, { data: 'echt', manifest: BEELD_DIR ? 'echt' : false, url: `/?lang=${lang}` });
      await page.locator('[data-question]').first().click();
      await waitIdle(page);
      await page.locator('[data-ask-other]').first().click();
      await waitIdle(page);
      await page.locator('.chip--sources').last().click();
      const overflowWithSheet = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      await page.keyboard.press('Escape');
      await chooseMode(page, 'meeting');
      await page.locator('[data-statement]').first().click();
      await expect(page.locator('[data-reaction]').first()).toBeVisible({ timeout: 15_000 });
      await page.locator('[data-method] summary').click();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect([overflowWithSheet, overflow]).toEqual([0, 0]);
    });
  }
}

test.describe('met het echte beeld van de beeldstroom', () => {
  test.skip(!BEELD_DIR, 'geen manifest.json in site/etalage/media/duiven en geen ETALAGE_BEELD_DIR');

  test('de duif steekt uit de kaart, zonder de kop erboven te raken', async ({ page }) => {
    await openPage(page, { data: 'echt', manifest: 'echt' });
    await expect(page.locator('body')).toHaveAttribute('data-beeld', 'manifest');
    for (const id of ['kennispartner', 'stakeholder', 'signalerend']) {
      const loaded = await card(page, id).locator('.card__rest, .card__plate-img').evaluateAll((imgs) => Promise.all(imgs.map((img) => img.decode().then(() => img.naturalWidth > 0, () => false))));
      expect(loaded).toEqual([true, true]);
    }
    const front = card(page, 'kennispartner');
    const cardBox = await front.boundingBox();
    const layerBox = await front.locator('.card__pigeon').boundingBox();
    expect(layerBox.y).toBeLessThan(cardBox.y - 20);
    const line = await page.locator('.hero__line').boundingBox();
    expect(layerBox.y).toBeGreaterThanOrEqual(line.y + line.height);
  });

  test('de clip speelt in het juiste formaat, met alfa, en keert terug naar het rustbeeld', async ({ page, browserName }) => {
    await openPage(page, { data: 'echt', manifest: 'echt' });
    await card(page, 'signalerend').click();
    // De reactie speelt aan tafel, in het vergaderpaneel.
    const layer = page.locator('[data-table-card] .card__pigeon');
    const statement = statements.find((s) => s.reactions.nl.signalerend?.type === 'signal');
    await page.locator(`[data-statement="${statement.id}"]`).click();
    await expect(layer).toHaveClass(/is-playing/, { timeout: 15_000 });
    const video = layer.locator('video');
    const src = await video.getAttribute('src');
    expect(src).toMatch(browserName === 'webkit' ? /\.mov$/ : /\.webm$/);
    // Het beeld heeft echt alfa: deels dekkend, deels doorzichtig (geen vlak).
    const alpha = await video.evaluate((v) => {
      const canvas = document.createElement('canvas');
      canvas.width = 80;
      canvas.height = 80;
      const ctx = canvas.getContext('2d');
      ctx.drawImage(v, 0, 0, 80, 80);
      const data = ctx.getImageData(0, 0, 80, 80).data;
      let opaque = 0;
      let clear = 0;
      for (let i = 3; i < data.length; i += 4) {
        if (data[i] > 200) opaque++;
        else if (data[i] < 20) clear++;
      }
      return { opaque, clear };
    });
    expect(alpha.opaque).toBeGreaterThan(200);
    expect(alpha.clear).toBeGreaterThan(1000);
    // Zolang de clip speelt is alleen de video zichtbaar, daarna alleen het rustbeeld.
    expect(await layer.evaluate((el) => [getComputedStyle(el.querySelector('video')).visibility, getComputedStyle(el.querySelector('.card__rest')).visibility])).toEqual(['visible', 'hidden']);
    await expect(layer).not.toHaveClass(/is-playing/, { timeout: 10_000 });
    expect(await layer.evaluate((el) => [getComputedStyle(el.querySelector('video')).visibility, getComputedStyle(el.querySelector('.card__rest')).visibility])).toEqual(['hidden', 'visible']);
  });

  // De tafelstrook loopt op een smal scherm mee boven in het paneel. De
  // toetsenbordfocus in het verslag mag daar nooit onder vallen (WCAG 2.4.11):
  // met Shift+Tab terug door twee echte reacties is elk gefocust element
  // volledig zichtbaar en door niets bedekt. 640×400 is 200% zoom.
  for (const [width, height] of [[390, 844], [640, 400]]) {
    test(`de toetsenbordfocus in het verslag valt nooit onder kop of tafelstrook (${width}×${height})`, async ({ page, browserName }) => {
      // Safari zet knoppen standaard niet in de Tab-volgorde; Option+Tab wel.
      const back = browserName === 'webkit' ? 'Alt+Shift+Tab' : 'Shift+Tab';
      await page.setViewportSize({ width, height });
      await openPage(page, { data: 'echt', manifest: 'echt' });
      await chooseMode(page, 'meeting');
      const speech = statements.filter((s) => s.reactions.nl.kennispartner?.type === 'speech').slice(0, 2);
      for (const [i, s] of speech.entries()) {
        await page.locator(`[data-statement="${s.id}"]`).click();
        await expect(page.locator('[data-reaction="speech"]')).toHaveCount(i + 1, { timeout: 10_000 });
      }
      await page.locator('[data-statement]').first().focus();
      const seen = [];
      for (let i = 0; i < 80; i++) {
        await page.keyboard.press(back);
        const state = await page.evaluate(() => {
          const el = document.activeElement;
          if (!document.querySelector('.meeting__main').contains(el)) return null;
          const r = el.getBoundingClientRect();
          const hits = [[0.5, 0.5], [0.2, 0.2], [0.8, 0.2], [0.2, 0.8], [0.8, 0.8]]
            .map(([fx, fy]) => document.elementFromPoint(r.left + r.width * fx, r.top + r.height * fy));
          return {
            element: `${el.tagName.toLowerCase()}.${el.className}`,
            inView: r.top >= 0 && r.bottom <= window.innerHeight && r.left >= 0 && r.right <= window.innerWidth,
            uncovered: hits.every((hit) => hit && el.contains(hit)),
          };
        });
        if (!state) break;
        seen.push(state);
      }
      // Citaties, bronnenknoppen, Beluister en het verslag zelf.
      expect(seen.length).toBeGreaterThan(6);
      for (const state of seen) expect(state, state.element).toMatchObject({ inView: true, uncovered: true });
    });
  }

  // De duif zit aan tafel en blijft in beeld terwijl de bezoeker een uitspraak
  // kiest: op een breed scherm in de kolom links, op een telefoon in de strook
  // boven in het paneel. Gemeten tijdens de clip, niet bedekt door de kop.
  for (const [width, height] of [[1440, 900], [1280, 720], [390, 844]]) {
    test(`de reagerende duif staat tijdens haar clip in beeld (${width}×${height})`, async ({ page }) => {
      await page.setViewportSize({ width, height });
      await openPage(page, { data: 'echt', manifest: 'echt' });
      await chooseMode(page, 'meeting');
      // Zoals een bezoeker: de laatste uitspraak met een reactie onderaan het scherm.
      const speech = statements.filter((s) => s.reactions.nl.kennispartner?.type === 'speech');
      const target = page.locator(`[data-statement="${speech.at(-1).id}"]`);
      await target.evaluate((el) => el.scrollIntoView({ block: 'end' }));
      await target.click();
      const layer = page.locator('[data-table-card] .card__pigeon');
      await expect(layer).toHaveClass(/is-playing/, { timeout: 15_000 });
      const inView = () => page.evaluate(() => {
        const tableCard = document.querySelector('[data-table-card]');
        const pigeon = tableCard.querySelector('.card__pigeon').getBoundingClientRect();
        const box = tableCard.getBoundingClientRect();
        const topbar = document.querySelector('.topbar');
        const below = getComputedStyle(topbar).position === 'sticky' ? topbar.getBoundingClientRect().bottom : 0;
        const hit = document.elementFromPoint(box.left + box.width / 2, box.top + box.height / 2);
        return {
          top: pigeon.top >= below,
          bottom: pigeon.bottom <= window.innerHeight,
          sides: pigeon.left >= 0 && pigeon.right <= window.innerWidth,
          uncovered: tableCard.contains(hit),
        };
      });
      const all = { top: true, bottom: true, sides: true, uncovered: true };
      expect(await inView()).toEqual(all);
      await page.waitForTimeout(1200);
      await expect(layer).toHaveClass(/is-playing/);
      expect(await inView()).toEqual(all);
    });
  }
});
