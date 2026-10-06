// De FAQ als echte dialoog: zeven vragen, focus binnen het venster, Escape, en
// de focus terug naar de knop die hem opende.
import { test, expect, openPage, OPNAMES, COPY } from './helpers.js';

const nl = COPY.nl;

test('de ?-knop opent een dialoog met zeven vragen; de eerste staat open', async ({ page }) => {
  await openPage(page);
  const opener = page.getByRole('button', { name: nl.hero.faq });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: nl.faq.title });
  await expect(dialog).toBeVisible();
  const questions = dialog.locator('.faq__q button');
  await expect(questions).toHaveCount(7);
  await expect(questions).toHaveText(nl.faq.items.map((item) => item.q));
  await expect(questions.first()).toHaveAttribute('aria-expanded', 'true');
  await expect(questions.first()).toBeFocused();

  // Eén vraag tegelijk open, zoals in het ontwerp.
  await questions.nth(5).click();
  await expect(questions.nth(5)).toHaveAttribute('aria-expanded', 'true');
  await expect(questions.first()).toHaveAttribute('aria-expanded', 'false');
  await expect(dialog.locator('#faq-a-straat')).toHaveText(nl.faq.items[5].a());
});

test('Tab blijft binnen het venster, Escape sluit en de focus gaat terug', async ({ page }) => {
  await openPage(page);
  const opener = page.getByRole('button', { name: nl.hero.faq });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: nl.faq.title });
  for (let i = 0; i < 14; i++) {
    await page.keyboard.press('Tab');
    expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true);
  }
  await page.keyboard.press('Shift+Tab');
  expect(await dialog.evaluate((el) => el.contains(document.activeElement))).toBe(true);
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
});

test('Sluiten en een klik naast het venster sluiten ook', async ({ page }) => {
  await openPage(page);
  const opener = page.getByRole('button', { name: nl.hero.faq });
  await opener.click();
  const dialog = page.getByRole('dialog', { name: nl.faq.title });
  await dialog.locator('.faq__foot').getByRole('button', { name: nl.faq.close }).click();
  await expect(dialog).toBeHidden();
  await expect(opener).toBeFocused();
  await opener.click();
  await page.mouse.click(8, 450);
  await expect(dialog).toBeHidden();
});

test('"Zijn de antwoorden echt?" noemt de maand uit de opname, niet het model', async ({ page }) => {
  await openPage(page);
  await page.getByRole('button', { name: nl.honest.how }).click();
  const answer = page.locator('#faq-a-echt');
  await expect(answer).toContainText('september 2026');
  await expect(answer).not.toContainText(OPNAMES.recording.model);
});

test('zonder opnames blijft de FAQ bruikbaar, zonder getallen of model die er niet zijn', async ({ page }) => {
  await openPage(page, { data: false });
  await page.getByRole('button', { name: nl.hero.faq }).click();
  await page.getByRole('button', { name: nl.faq.items[2].q }).click();
  await expect(page.locator('#faq-a-deelname')).not.toContainText('118');
  await page.getByRole('button', { name: nl.faq.items[4].q }).click();
  await expect(page.locator('#faq-a-echt')).not.toContainText('undefined');
  await expect(page.locator('#faq-a-echt')).not.toContainText('null');
});

test('tekst selecteren en de muis naast het venster loslaten sluit het niet', async ({ page }) => {
  await openPage(page);
  await page.getByRole('button', { name: nl.hero.faq }).click();
  const answer = page.locator('#faq-a-waarom p');
  const box = await answer.boundingBox();
  await page.mouse.move(box.x + box.width - 10, box.y + 8);
  await page.mouse.down();
  await page.mouse.move(8, box.y + 12, { steps: 6 });
  await page.mouse.up();
  await expect(page.getByRole('dialog', { name: nl.faq.title })).toBeVisible();
});

test('zolang config.contact leeg is, noemt de FAQ geen adres; met een adres staat het er als link', async ({ page }) => {
  await openPage(page);
  await page.getByRole('button', { name: nl.hero.faq }).click();
  await expect(page.locator('.faq__contact')).toHaveCount(0);
  await page.keyboard.press('Escape');

  const config = await page.evaluate(() => import('./etalage/config.js').then((m) => JSON.stringify({ ...m.CONFIG, contact: 'mailto:duiven@example.org' })));
  await page.route('**/etalage/config.js', (route) => route.fulfill({ contentType: 'text/javascript', body: `export const CONFIG = ${config};` }));
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await page.getByRole('button', { name: nl.hero.faq }).click();
  const dialog = page.getByRole('dialog', { name: nl.faq.title });
  for (const id of ['toegang', 'representatief']) {
    await dialog.locator(`[data-faq-item="${id}"] .faq__q button`).click();
    const line = dialog.locator(`[data-faq-item="${id}"] .faq__contact`);
    await expect(line).toHaveText(`${nl.faq.contact[id]} duiven@example.org.`);
    await expect(line.locator('a')).toHaveAttribute('href', 'mailto:duiven@example.org');
  }
  await expect(dialog.locator('.faq__contact')).toHaveCount(2);
});
