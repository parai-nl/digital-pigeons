// Terugval: een ontbrekend of kapot opnames- of beeldbestand geeft een
// menselijke melding, nooit technische fouttekst, en de rest van de pagina werkt.
import { test, expect, openPage, chooseMode, choosePigeon, COPY, OPNAMES } from './helpers.js';

const nl = COPY.nl;
const TECHNICAL = /undefined|null|NaN|\[object|Error|JSON|schema|404|fetch|etalage\]/;

for (const data of [false, 'kapot', 'schema']) {
  test(`zonder bruikbare opnames (${data || 'ontbreekt'}): een menselijke melding en een werkende pagina`, async ({ page }) => {
    const errors = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await openPage(page, { data });
    await expect(page.locator('body')).toHaveAttribute('data-opnames', 'ontbreekt');
    await expect(page.locator('[data-chat-unavailable]')).toHaveText(nl.chat.unavailable);
    await expect(page.locator('[data-question]')).toHaveCount(0);
    await expect(page.locator('[data-honest]')).toBeHidden();
    // Zonder opnames blijft de duif beschreven.
    await expect(page.locator('[data-talk-blurb]')).toHaveText(nl.pigeons.kennispartner.blurb);

    await chooseMode(page, 'meeting');
    await expect(page.locator('[data-meeting-unavailable]')).toHaveText(nl.meeting.unavailable);

    // Zelf typen werkt nog en krijgt de gewone neutrale regel.
    await chooseMode(page, 'chat');
    await page.locator('[data-chat-input]').fill('Hallo?');
    await page.keyboard.press('Enter');
    await expect(page.locator('[data-chat-own]')).toBeVisible();

    await choosePigeon(page, 'stakeholder');
    const visible = await page.locator('main').innerText();
    expect(visible).not.toMatch(TECHNICAL);
    expect(errors).toEqual([]);
  });
}

for (const manifest of [false, 'kapot']) {
  test(`zonder bruikbaar manifest (${manifest || 'ontbreekt'}) staan de portretten er netjes`, async ({ page }) => {
    const errors = [];
    page.on('pageerror', (err) => errors.push(err.message));
    await openPage(page, { manifest });
    await expect(page.locator('body')).toHaveAttribute('data-beeld', 'portretten');
    await expect(page.locator('[data-carousel] .card__portrait')).toHaveCount(3);
    await expect(page.locator('.card__pigeon')).toHaveCount(0);
    // Ook aan de vergadertafel zit dan het portret.
    await expect(page.locator('[data-table-card] .card__portrait')).toHaveCount(1);
    expect(errors).toEqual([]);
  });
}

test('ontbreekt een beurt of reactie, dan zegt de pagina dat eerlijk en verzint zij niets', async ({ page }) => {
  const data = structuredClone(OPNAMES);
  delete data.chat.questions.find((q) => q.id === 'nest').turns.nl.kennispartner;
  delete data.meeting.statements.find((s) => s.id === 'poep').reactions.nl.kennispartner;
  // Een kapotte beurt valt weg in plaats van de pagina te breken.
  data.chat.questions.find((q) => q.id === 'afschot').turns.nl.kennispartner = { messages: 'kapot' };
  await openPage(page, { data });
  await page.locator('[data-question="nest"]').click();
  await expect(page.locator('.turn .chat__note')).toHaveText(nl.chat.missing, { timeout: 6000 });
  await expect(page.locator('.msg--pigeon')).toHaveCount(0);
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false');
  await page.locator('[data-question="afschot"]').click();
  await expect(page.locator('.turn .chat__note')).toHaveCount(2, { timeout: 6000 });

  await chooseMode(page, 'meeting');
  await page.locator('[data-statement="poep"]').click();
  await expect(page.locator('.meeting__log .chat__note')).toHaveText(nl.meeting.missing);
  await expect(page.locator('[data-reaction]')).toHaveCount(0);
});

test('de pagina toont geen duif onder een verkeerde naam en de Signalerende duif spreekt nooit', async ({ page }) => {
  const data = structuredClone(OPNAMES);
  // Een beurt van de Kennispartner waarin de Stakeholder praat.
  const nest = data.chat.questions.find((q) => q.id === 'nest').turns.nl;
  nest.kennispartner = structuredClone(nest.stakeholder);
  // Een gesproken reactie van de Signalerende duif, die nooit spreekt.
  data.meeting.statements.find((s) => s.id === 'afschot').reactions.nl.signalerend = {
    type: 'speech', delayMs: 100, text: '[fixture] Dit mag nooit te zien zijn.', sources: [],
  };
  await openPage(page, { data });
  await page.locator('[data-question="nest"]').click();
  await expect(page.locator('.turn .chat__note')).toHaveText(nl.chat.missing, { timeout: 6000 });
  await expect(page.locator('.msg--pigeon')).toHaveCount(0);
  await choosePigeon(page, 'signalerend');
  await page.locator('[data-statement="afschot"]').click();
  await expect(page.locator('.meeting__log .chat__note')).toHaveText(nl.meeting.missing);
  await expect(page.locator('main')).not.toContainText('Dit mag nooit');
});

test('laadt een beeld uit het manifest niet, dan staat er het portret en geen lege kaart', async ({ page }) => {
  await openPage(page, { manifest: true });
  // Later geregistreerd gaat voor: nu ontbreekt elk beeld van de Kennispartner.
  await page.route('**/etalage/media/duiven/kennispartner/*', (route) => route.fulfill({ status: 404 }));
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  const front = page.locator('.card[data-pigeon="kennispartner"]');
  await expect(front.locator('.card__portrait')).toHaveAttribute('src', 'etalage/media/portretten/kennispartner.webp');
  await expect(front.locator('.card__pigeon')).toHaveCount(0);
  // De andere duiven houden hun eigen beeld.
  await expect(page.locator('.card[data-pigeon="stakeholder"] .card__rest')).toBeAttached();
  await page.locator('[data-question="nest"]').click();
  const avatar = page.locator('.msg--pigeon[data-author="kennispartner"] .msg__avatar').first();
  await expect(avatar).toHaveAttribute('src', /portretten\/kennispartner-kop\.webp$/, { timeout: 8000 });
});
