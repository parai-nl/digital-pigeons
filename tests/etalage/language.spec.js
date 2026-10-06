// Taal: de browser beslist (Nederlands wordt NL, al het andere
// EN), de knop onthoudt een keuze, en ?lang= forceert.
import { test, expect, openPage, ask, question, storageWrites, COPY } from './helpers.js';

test.describe('een Engelstalige browser', () => {
  test.use({ locale: 'en-GB' });

  test('krijgt de Engelse pagina', async ({ page }) => {
    await openPage(page);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page).toHaveTitle(COPY.en.meta.title);
    await expect(page.locator('h1')).toHaveText(COPY.en.hero.title);
    await expect(page.locator('.topbar__title')).toHaveText(COPY.en.header.title);
    await expect(page.getByRole('button', { name: 'EN', exact: true })).toHaveAttribute('aria-pressed', 'true');
  });

  test('?lang=nl forceert Nederlands, ook boven een eerdere keuze', async ({ page }) => {
    await openPage(page, { url: '/?lang=nl', storage: { duif_etalage_lang: 'en' } });
    await expect(page.locator('html')).toHaveAttribute('lang', 'nl');
    await expect(page.locator('h1')).toHaveText(COPY.nl.hero.title);
  });
});

test.describe('een Duitstalige browser', () => {
  test.use({ locale: 'de-DE' });
  test('krijgt Engels', async ({ page }) => {
    await openPage(page);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  });
});

test('een Nederlandstalige browser krijgt Nederlands; de knop wisselt en onthoudt', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('html')).toHaveAttribute('lang', 'nl');
  await expect(page.locator('h1')).toHaveText(COPY.nl.hero.title);

  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('h1')).toHaveText(COPY.en.hero.title);
  await expect(page.locator('[data-talk-name]')).toHaveText(COPY.en.pigeons.kennispartner.name);
  await expect(page.locator('#tab-meeting')).toHaveText(COPY.en.modes.meeting);
  expect(await storageWrites(page)).toContainEqual(['duif_etalage_lang', 'en']);

  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
});

test('een gesprek wordt in de nieuwe taal opnieuw opgebouwd uit de opnames van die taal', async ({ page }) => {
  await openPage(page);
  await ask(page, 'nest');
  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await expect(page.locator('.msg--user .msg__text')).toHaveText(question('nest').text.en);
  await expect(page.locator('[data-question="nest"]')).toHaveText(new RegExp(question('nest').text.en.replace(/[[\]?]/g, '\\$&')));
});

test('?lang=en in de adresbalk wordt bijgewerkt als je met de knop wisselt', async ({ page }) => {
  await openPage(page, { url: '/?lang=en' });
  await page.getByRole('button', { name: 'NL', exact: true }).click();
  await expect(page).toHaveURL(/lang=nl/);
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('html')).toHaveAttribute('lang', 'nl');
});

test('de pagina bewaart alleen taal-, geluid- en themakeuze', async ({ page }) => {
  await openPage(page);
  await ask(page, 'nest');
  await page.locator('[data-chat-input]').fill('iets van mij');
  await page.keyboard.press('Enter');
  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await page.locator('#tab-meeting').click();
  await page.getByRole('switch', { name: COPY.en.meeting.soundLabel }).click();
  await page.getByRole('switch', { name: COPY.en.header.theme }).click();
  const keys = [...new Set((await storageWrites(page)).map(([key]) => key))].sort();
  expect(keys).toEqual(['duif_etalage_geluid', 'duif_etalage_lang', 'duif_etalage_thema', 'mode-watcher-mode']);
});
