// Vergelijken: de hoekknop brengt dezelfde vraag naar de andere duif.
// Haar gesprek toont één herkomstregel, het antwoord van de eerste duif onder
// diens eigen naam en bronnen, en dan haar eigen opgenomen vergelijkantwoord.
import { test, expect, openPage, ask, question, shown, card, COPY, fill } from './helpers.js';

const nl = COPY.nl;
const text = (qid, key) => question(qid).turns.nl[key].messages.find((m) => m.author !== 'facilitator').text;

test('"Vraag het ook aan de Stakeholder" draait de waaier en toont herkomst, auteurschap en het vergelijkantwoord', async ({ page }) => {
  await openPage(page);
  await ask(page, 'afschot');
  const compare = page.getByRole('button', { name: fill(nl.chat.compare, { nameThe: 'de Stakeholder' }) });
  await expect(compare).toBeVisible();
  await compare.click();

  await expect(card(page, 'stakeholder')).toHaveAttribute('aria-current', 'true');
  const origin = page.locator('[data-chat-origin="kennispartner"]');
  await expect(origin).toContainText(fill(nl.chat.origin, { nameThe: 'de Kennispartner' }));
  await expect(origin).toBeFocused();
  await expect(page.locator('.msg--user .msg__text').first()).toContainText(question('afschot').text.nl);

  // Het antwoord van de Kennispartner reist mee onder haar eigen naam en bronnen.
  const first = page.locator('.msg--pigeon[data-author="kennispartner"]');
  await expect(first.locator('.msg__author')).toHaveText(nl.pigeons.kennispartner.name);
  await expect(first.locator('.msg__text')).toHaveText(shown(text('afschot', 'kennispartner')));
  await expect(first.locator('.chip--sources')).toBeVisible();
  await expect(page.locator('.chat__repeated')).toHaveText(fill(nl.chat.repeated, { nameThe: 'de Stakeholder' }));

  // Daarna het opgenomen vergelijkantwoord van de Stakeholder zelf.
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  const second = page.locator('.msg--pigeon[data-author="stakeholder"]');
  await expect(second.locator('.msg__text')).toHaveText(shown(text('afschot', 'kennispartner>stakeholder')));
  await expect(second.locator('.msg__author')).toHaveText(nl.pigeons.stakeholder.name);

  // Terug brengt je naar het oorspronkelijke gesprek, waar de knop nu weg is.
  await origin.getByRole('button', { name: nl.chat.back }).click();
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
  await expect(page.locator('[data-chat-origin]')).toHaveCount(0);
  await expect(page.locator('[data-ask-other]')).toBeHidden();
});

test('de andere kant op: van de Stakeholder naar de Kennispartner', async ({ page }) => {
  await openPage(page);
  await card(page, 'stakeholder').click();
  await ask(page, 'nest');
  await page.getByRole('button', { name: fill(nl.chat.compare, { nameThe: 'de Kennispartner' }) }).click();
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
  await expect(page.locator('[data-chat-origin="stakeholder"]')).toBeVisible();
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  await expect(page.locator('.msg--pigeon[data-author="kennispartner"] .msg__text').last()).toHaveText(shown(text('nest', 'stakeholder>kennispartner')));
  // Zoals in de module reist alleen het antwoord van de Stakeholder mee, niet de
  // facilitatorberichten uit haar gesprek.
  await expect(page.locator('.msg--pigeon[data-author="stakeholder"] .msg__text')).toHaveText(shown(text('nest', 'stakeholder')));
  await expect(page.locator('.msg--facilitator')).toHaveCount(0);
});

test('zonder opgenomen vergelijkbeurt staat er geen hoekknop', async ({ page }) => {
  await openPage(page);
  await ask(page, 'vuurwerk');
  await expect(page.locator('[data-ask-other]')).toHaveCount(0);
});

test('tijdens een beurt staat de hoekknop er nog niet', async ({ page }) => {
  await openPage(page);
  await page.locator('[data-question="afschot"]').click();
  await expect(page.locator('.msg--user')).toBeVisible();
  await expect(page.locator('[data-ask-other]')).toBeHidden();
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  await expect(page.locator('[data-ask-other]')).toBeVisible();
});
