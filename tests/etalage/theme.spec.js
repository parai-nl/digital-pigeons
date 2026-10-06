// Licht of donker: zonder keuze volgt de pagina het systeem; de schakelaar in de
// kop kiest en onthoudt. Die keuze staat onder de sleutel van de werkomgeving op
// /duif (mode-watcher in de parai-core-schil), dus ze geldt in beide richtingen.
// De werkomgeving schrijft daar uit zichzelf 'light' (haar standaard); dat is
// geen keuze, en daarvoor houdt de etalage haar eigen sleutel bij.
import { test, expect, openPage, storageWrites, COPY } from './helpers.js';

const KEY = 'mode-watcher-mode';
const OWN = 'duif_etalage_thema';
const themeSwitch = (page) => page.getByRole('switch', { name: COPY.nl.header.theme });
const ink = (page) => page.evaluate(() => getComputedStyle(document.body).color);
const logo = (page) => page.locator('header [data-brands] img.brands__aimaps');
const logoSrc = (page) => logo(page).evaluate((img) => img.currentSrc);

test.describe('met een donker systeem', () => {
  test.use({ colorScheme: 'dark' });

  test('volgt de pagina het systeem tot de bezoeker kiest', async ({ page }) => {
    await openPage(page);
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(themeSwitch(page)).toHaveAttribute('aria-checked', 'true');
    await expect.poll(() => logoSrc(page)).toContain('ai-maps-donker.png');
    const dark = await ink(page);

    await themeSwitch(page).click();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
    await expect(themeSwitch(page)).toHaveAttribute('aria-checked', 'false');
    await expect.poll(() => logoSrc(page)).toMatch(/ai-maps\.png$/);
    expect(await ink(page)).not.toBe(dark);
    expect(await storageWrites(page)).toEqual([[OWN, 'light'], [KEY, 'light']]);
  });

  test('de standaard die de werkomgeving schrijft is geen keuze: de pagina blijft het systeem volgen', async ({ page, context }) => {
    // Zo staat het er na één bezoek aan /duif of de loginpagina, zonder klik.
    await openPage(page, { storage: { [KEY]: 'light' } });
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
    await expect(themeSwitch(page)).toHaveAttribute('aria-checked', 'true');
    // En ook niet als de werkomgeving dat schrijft terwijl de etalage openstaat.
    const other = await context.newPage();
    await other.goto('/etalage/data/opnames.json');
    await other.evaluate((key) => localStorage.setItem(key, 'light'), KEY);
    await other.close();
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });

  test('koos je hier donker en daarna in de werkomgeving licht, dan telt licht', async ({ page }) => {
    await openPage(page, { storage: { [OWN]: 'dark', [KEY]: 'light' } });
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  });

  test('"system" uit de werkomgeving betekent: het systeem volgen', async ({ page }) => {
    await openPage(page, { storage: { [KEY]: 'system' } });
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  });
});

test('de schakelaar kiest donker, onthoudt dat en bewaart verder niets', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await themeSwitch(page).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(await storageWrites(page)).toEqual([[OWN, 'dark'], [KEY, 'dark']]);

  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(themeSwitch(page)).toHaveAttribute('aria-checked', 'true');

  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await expect(page.getByRole('switch', { name: COPY.en.header.theme })).toHaveAttribute('aria-checked', 'true');
});

test('een keuze uit de werkomgeving geldt hier, ook tegen het systeem in', async ({ page }) => {
  await openPage(page, { storage: { [KEY]: 'dark' } });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect.poll(() => logoSrc(page)).toContain('ai-maps-donker.png');
  await page.locator('[data-method] summary').click();
  await expect(page.locator('[data-method-graph] source[data-dark]')).toHaveAttribute('media', 'screen');
});

test('een wissel in een ander tabblad komt meteen door, ook het leegmaken van de opslag', async ({ page, context }) => {
  await openPage(page);
  const other = await context.newPage();
  await openPage(other);
  await themeSwitch(other).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await expect(themeSwitch(page)).toHaveAttribute('aria-checked', 'true');
  await other.evaluate(() => localStorage.clear());
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await other.close();
});

test('zonder keuze loopt de pagina mee als het systeem wisselt, met een keuze niet', async ({ page }) => {
  await openPage(page);
  await page.emulateMedia({ colorScheme: 'dark' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await themeSwitch(page).click();
  await page.emulateMedia({ colorScheme: 'dark' });
  await page.emulateMedia({ colorScheme: 'light' });
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
});

test('met geblokkeerde opslag werkt de schakelaar voor dit bezoek, zonder fouten', async ({ page }) => {
  const errors = [];
  page.on('pageerror', (error) => errors.push(error.message));
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new DOMException('geblokkeerd', 'SecurityError'); } });
  });
  await openPage(page);
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light');
  await themeSwitch(page).click();
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark');
  expect(errors).toEqual([]);
});

test('stand en icoon staan er al voordat de teksten er zijn', async ({ page }) => {
  let release;
  const held = new Promise((resolve) => { release = resolve; });
  await page.route('**/etalage/media/duiven/manifest.json', async (route) => { await held; await route.fulfill({ status: 404 }); });
  await page.addInitScript((key) => localStorage.setItem(key, 'dark'), KEY);
  await page.goto('/', { waitUntil: 'domcontentloaded' });
  const button = page.locator('[data-theme-switch]');
  await expect(button).toHaveAttribute('aria-checked', 'true');
  await expect(button.locator('svg')).toHaveCount(1);
  await expect(button).toHaveAttribute('aria-label', COPY.nl.header.theme);
  release();
  await expect(page.locator('body')).toHaveAttribute('data-shell', 'true');
});

// Vóór app.js laadt, beslist theme-early.js alleen; dezelfde regel als prefs.js.
const EARLY = [
  { name: 'een keuze voor donker', scheme: 'light', seed: { [KEY]: 'dark' }, want: 'dark' },
  { name: 'de standaard van de werkomgeving', scheme: 'dark', seed: { [KEY]: 'light' }, want: 'dark' },
  { name: 'een keuze hier voor licht', scheme: 'dark', seed: { [OWN]: 'light', [KEY]: 'light' }, want: 'light' },
];
for (const { name, scheme, seed, want } of EARLY) {
  test(`de kleur staat er al voordat app.js laadt (${name}): geen flits`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.addInitScript((entries) => { for (const [k, v] of entries) localStorage.setItem(k, v); }, Object.entries(seed));
    let release;
    const held = new Promise((resolve) => { release = resolve; });
    await page.route('**/etalage/app.js', async (route) => { await held; await route.continue(); });
    await page.goto('/', { waitUntil: 'commit' });
    await expect(page.locator('html')).toHaveAttribute('data-theme', want);
    expect(await page.evaluate(() => getComputedStyle(document.documentElement).colorScheme)).toBe(want);
    release();
    await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  });
}

test('op papier blijft de pagina licht, ook als donker gekozen is', async ({ page }) => {
  await openPage(page);
  const light = await ink(page);
  await themeSwitch(page).click();
  const dark = await ink(page);
  expect(dark).not.toBe(light);
  await page.emulateMedia({ media: 'print' });
  expect(await ink(page)).toBe(light);
  await expect(themeSwitch(page)).toBeHidden();
});
