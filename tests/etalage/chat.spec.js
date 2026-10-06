// Het gesprek: opgenomen antwoorden, de vraag typt
// zich in de balk, stappen, bronnen, facilitator, eigen vragen en een eigen
// gesprek per duif.
import { test, expect, openPage, ask, question, shown, choosePigeon, COPY, CONFIG, fill, storageWrites } from './helpers.js';

const nl = COPY.nl;
const turn = (qid, key, lang = 'nl') => question(qid).turns[lang][key];
const pigeonText = (qid, key) => turn(qid, key).messages.find((m) => m.author !== 'facilitator').text;

test('een lege chat toont een neutrale hint en geen duiventekst', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('.chat__empty')).toHaveText(nl.chat.empty);
  await expect(page.locator('.msg')).toHaveCount(0);
  // Vier vraagknoppen, dezelfde voor beide pratende duiven.
  const kp = await page.locator('[data-question]').allTextContents();
  expect(kp).toHaveLength(4);
  await choosePigeon(page, 'stakeholder');
  expect(await page.locator('[data-question]').allTextContents()).toEqual(kp);
});

test('een vraagknop typt de vraag in de balk, verstuurt hem en speelt de opname af', async ({ page }) => {
  await openPage(page);
  const q = question('afschot');
  const input = page.locator('[data-chat-input]');
  const started = Date.now();
  await page.locator('[data-question="afschot"]').click();

  // De vraag verschijnt zichtbaar in de balk, en is daarna verstuurd.
  await expect(input).toHaveValue(/\[fixture\] Wat/);
  await expect(page.locator('.msg--user .msg__text')).toContainText(q.text.nl);
  await expect(input).toHaveValue('');

  // De stappen van een beurt staan kort in beeld.
  await expect(page.locator('[data-step="pigeon_grounding"]')).toContainText(nl.chat.steps.pigeon_grounding);
  await expect(page.locator('[data-chat-step-status]')).toContainText(nl.chat.steps.pigeon_grounding);

  // Het eerste woord van de duif komt snel: typen plus ingekorte stappen.
  const answer = page.locator('.msg--pigeon[data-author="kennispartner"]:not([data-thinking])');
  await expect(answer.locator('.msg__text')).not.toBeEmpty({ timeout: 6000 });
  expect(Date.now() - started).toBeLessThan(4500);

  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  await expect(answer.locator('.msg__text')).toHaveText(shown(pigeonText('afschot', 'kennispartner')));
  await expect(answer.locator('.chip--sources')).toHaveText(fill(nl.chat.sourcesMany, { count: 2 }));
  // Na de beurt zijn de stappen weg en is de vraag gedimd.
  await expect(page.locator('[data-step]')).toHaveCount(0);
  await expect(page.locator('[data-question="afschot"]')).toHaveAttribute('aria-disabled', 'true');
  await expect(page.locator('[data-chat-announce]')).toHaveText(fill(nl.chat.answered, { nameThe: 'De Kennispartner' }));
});

test('het antwoord groeit in brokken', async ({ page }) => {
  await openPage(page);
  const full = shown(pigeonText('afschot', 'kennispartner'));
  await page.locator('[data-question="afschot"]').click();
  const text = page.locator('.msg--pigeon[data-author="kennispartner"]:not([data-thinking]) .msg__text');
  await expect(text).not.toBeEmpty({ timeout: 6000 });
  const partial = await text.textContent();
  expect(partial.length).toBeLessThan(full.length);
  expect(full.startsWith(partial.trim().slice(0, 20))).toBe(true);
  await expect(text).toHaveText(full, { timeout: 10_000 });
});

test('de bronchip opent het bronpaneel met passage, link en de kennisgraaf achter Inloggen', async ({ page }) => {
  await openPage(page);
  await ask(page, 'afschot');
  const chip = page.locator('.msg--pigeon .chip--sources').first();
  await chip.click();
  const dialog = page.getByRole('dialog', { name: nl.sources.title });
  await expect(dialog).toBeVisible();
  const sources = turn('afschot', 'kennispartner').messages[0].sources;
  await expect(dialog.locator('.source')).toHaveCount(sources.length);
  const first = dialog.locator('.source').first();
  await expect(first.locator('.source__title')).toHaveText(sources[0].title);
  await expect(first.locator('.source__meta')).toHaveText(`${sources[0].authors} · ${sources[0].year}`);
  await expect(first.locator('blockquote')).toHaveText(sources[0].passage);
  await expect(first.locator('blockquote')).toHaveAttribute('lang', 'en');
  await expect(first.getByRole('link', { name: nl.sources.open })).toHaveAttribute('href', sources[0].url);
  await expect(dialog.getByRole('link', { name: nl.sources.graph })).toHaveAttribute('href', CONFIG.loginUrl);

  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(chip).toBeFocused();
});

test('een verwijzing in de tekst opent het paneel op die bron', async ({ page }) => {
  await openPage(page);
  await ask(page, 'afschot');
  await page.getByRole('button', { name: fill(nl.chat.cite, { n: 4 }) }).first().click();
  const dialog = page.getByRole('dialog', { name: nl.sources.title });
  await expect(dialog.locator('[data-source="4"]')).toBeFocused();
  await expect(dialog.locator('[data-source="4"]')).toHaveClass(/is-focus/);
});

test('bij de Stakeholder spreekt de facilitator zoals in de module: herkenbaar anders en zonder portret', async ({ page }) => {
  await openPage(page);
  await choosePigeon(page, 'stakeholder');
  await ask(page, 'afschot');
  const facilitator = page.locator('.msg--facilitator:not([data-thinking])');
  await expect(facilitator).toHaveCount(2);
  const [frame, , mediation] = turn('afschot', 'stakeholder').messages;
  await expect(facilitator.nth(0).locator('.msg__text')).toHaveText(frame.text);
  // Alinea's van de facilitator blijven alinea's.
  await expect(facilitator.nth(1).locator('.msg__text p')).toHaveText(mediation.text.split('\n\n'));
  await expect(facilitator.first().locator('.msg__author')).toHaveText(nl.chat.facilitator);
  await expect(facilitator.locator('img')).toHaveCount(0);
  // Volgorde: kader, duif, bemiddeling.
  const order = await page.locator('.turn .msg:not(.msg--user)').evaluateAll((els) => els.map((el) => el.dataset.author));
  expect(order).toEqual(['facilitator', 'stakeholder', 'facilitator']);
});

test('een kennisgat en een standpunt zonder onderzoek krijgen hun eigen markering', async ({ page }) => {
  await openPage(page);
  await ask(page, 'taal');
  await expect(page.locator('.msg--pigeon .chip--gap')).toHaveText(nl.chat.gap);
  await expect(page.locator('.msg--pigeon .chip--sources')).toHaveCount(0);
  await choosePigeon(page, 'stakeholder');
  await ask(page, 'taal');
  await expect(page.locator('.msg--pigeon .chip--stance')).toHaveText(nl.chat.stance);
});

test('zelf typen: je vraag staat in beeld met een neutrale regel en Inloggen, en gaat nergens heen', async ({ page }) => {
  const requests = [];
  page.on('request', (r) => requests.push(r.url()));
  await openPage(page);
  const mine = 'Mag ik je iets geheims vragen over zaterdag?';
  await page.locator('[data-chat-input]').fill(mine);
  await page.keyboard.press('Enter');
  await expect(page.locator('.msg--user .msg__text')).toHaveText(mine);
  const noteLine = page.locator('[data-chat-own]');
  await expect(noteLine).toContainText(nl.chat.own);
  await expect(noteLine.getByRole('link', { name: nl.chat.ownLogin })).toHaveAttribute('href', CONFIG.loginUrl);
  // Geen duif die praat, en de tekst verlaat de pagina niet.
  await expect(page.locator('.msg--pigeon')).toHaveCount(0);
  expect(requests.some((u) => u.includes('geheims') || u.includes('zaterdag'))).toBe(false);
  const writes = await storageWrites(page);
  expect(writes.some(([, v]) => String(v).includes('zaterdag'))).toBe(false);
});

test('elke duif houdt haar eigen gesprek; gewoon wisselen kopieert niets', async ({ page }) => {
  await openPage(page);
  await ask(page, 'nest');
  await choosePigeon(page, 'stakeholder');
  await expect(page.locator('.chat__empty')).toBeVisible();
  await expect(page.locator('.msg')).toHaveCount(0);
  await expect(page.locator('[data-question="nest"]')).toHaveAttribute('aria-disabled', 'false');
  await choosePigeon(page, 'kennispartner');
  await expect(page.locator('.msg--pigeon[data-author="kennispartner"] .msg__text')).toHaveText(shown(pigeonText('nest', 'kennispartner')));
});

test('herladen wist het gesprek', async ({ page }) => {
  await openPage(page);
  await ask(page, 'nest');
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('.msg')).toHaveCount(0);
});

test('de eerlijke regel opent de FAQ op "Zijn de antwoorden op deze pagina echt?"', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('[data-honest-text]')).toHaveText(`${nl.honest.recorded} · september 2026`);
  await page.getByRole('button', { name: nl.honest.how }).click();
  const q = nl.faq.items.find((item) => item.id === 'echt').q;
  const button = page.getByRole('dialog', { name: nl.faq.title }).getByRole('button', { name: q });
  await expect(button).toHaveAttribute('aria-expanded', 'true');
  await expect(button).toBeFocused();
});

test('bij minder beweging staat de vraag meteen in de balk en het antwoord in één keer', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openPage(page);
  await page.locator('[data-question="nest"]').click();
  await expect(page.locator('[data-chat-input]')).toHaveValue(question('nest').text.nl);
  const text = page.locator('.msg--pigeon:not([data-thinking]) .msg__text');
  await expect(text).toHaveText(shown(pigeonText('nest', 'kennispartner')), { timeout: 6000 });
});

test('verwante bronnen krijgen de neutrale verwant-markering, en een bron kan meer passages hebben', async ({ page }) => {
  await openPage(page);
  await ask(page, 'nest');
  const answer = page.locator('.msg--pigeon[data-author="kennispartner"]');
  await expect(answer.locator('.chip--sources')).toHaveText(nl.chat.relatedOne);
  await expect(answer.locator('[data-related]')).toHaveText(nl.chat.related);
  await answer.locator('.chip--sources').click();
  const passages = turn('nest', 'kennispartner').messages[0].sources[0].passages;
  await expect(page.getByRole('dialog', { name: nl.sources.title }).locator('blockquote')).toHaveText(passages.map((p) => p.text));
});

test('een verwijzing die tijdens het afspelen verschijnt, is pas een knop als het antwoord er staat', async ({ page }) => {
  await openPage(page);
  await page.locator('[data-question="afschot"]').click();
  const text = page.locator('.msg--pigeon[data-author="kennispartner"]:not([data-thinking]) .msg__text');
  await expect(text.locator('span.cite').first()).toBeAttached({ timeout: 8000 });
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  await expect(text.locator('span.cite')).toHaveCount(0);
  await expect(text.locator('button.cite')).toHaveCount(2);
});

test('na "Terug" staat de focus in het gesprek waar je naar terugkeert', async ({ page }) => {
  await openPage(page);
  await ask(page, 'afschot');
  await page.locator('[data-ask-other]').click();
  await page.locator('[data-chat-origin] button').click();
  await expect(page.locator('[data-chat-scroll]')).toBeFocused();
});
