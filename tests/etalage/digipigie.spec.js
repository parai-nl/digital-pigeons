// DigiPigie, de knipoog achter het AI-MAPS-logo: pas na vijf snelle klikken,
// en zonder sporen op de rest van de pagina (geen opslag, geen geluid, niets
// geladen voordat het nodig is).
import { test, expect, openPage, played, clips, storageWrites, COPY } from './helpers.js';

const nl = COPY.nl.digipigie;
const party = (page) => page.getByRole('dialog', { name: nl.title });

async function tapLogo(page, times, where = 'header') {
  const logo = page.locator(`${where} [data-brands] .brands__aimaps`);
  for (let i = 0; i < times; i++) await logo.click();
}

test('vijf snelle klikken op het logo openen DigiPigie; Escape sluit zonder sporen', async ({ page }) => {
  const loaded = [];
  page.on('request', (request) => { if (/digipigie\.(js|css)$/.test(request.url())) loaded.push(request.url()); });
  await openPage(page, { manifest: true });

  await tapLogo(page, 4);
  await expect(party(page)).toHaveCount(0);
  expect(loaded, 'niets geladen voor de vijfde klik').toEqual([]);

  await tapLogo(page, 1);
  await expect(party(page)).toBeVisible();
  await expect(party(page).getByText(nl.thanks)).toBeVisible();
  await expect(party(page).getByRole('button', { name: nl.close })).toBeFocused();
  expect(loaded).toHaveLength(2);

  await page.keyboard.press('Escape');
  await expect(party(page)).toBeHidden();
  await expect(page.locator('html')).not.toHaveClass(/has-dialog/);
  expect(await storageWrites(page)).toEqual([]);
  expect(await played(page)).toEqual([]);
});

test('te langzaam klikken telt opnieuw', async ({ page }) => {
  await openPage(page);
  await tapLogo(page, 4);
  await page.waitForTimeout(1200);
  await tapLogo(page, 1);
  await page.waitForTimeout(300);
  await expect(party(page)).toHaveCount(0);
  // Daarna vier snelle klikken erbij is weer vijf op rij.
  await tapLogo(page, 4);
  await expect(party(page)).toBeVisible();
});

test('de sluitknop sluit, en een tweede keer opent het opnieuw', async ({ page }) => {
  await openPage(page, { manifest: true });
  await tapLogo(page, 5);
  await party(page).getByRole('button', { name: nl.close }).click();
  await expect(party(page)).toBeHidden();
  await tapLogo(page, 5);
  await expect(party(page)).toBeVisible();
});

test('een klik op de kop, boven het vak van de duif, laat de duif schudden', async ({ page }) => {
  await openPage(page, { manifest: true });
  await tapLogo(page, 5);
  await expect(party(page)).toBeVisible();
  // Eerst het schudden bij binnenkomst laten uitspelen, dan pas meten.
  await expect.poll(() => clips(page)).toHaveLength(1);
  await page.waitForTimeout(1500);
  await page.evaluate(() => { window.__played = []; });

  const bird = await page.locator('.digipigie__bird').boundingBox();
  const layer = await page.locator('.digipigie__bird .card__pigeon').boundingBox();
  const y = layer.y + 4;
  const x = layer.x + layer.width / 2;
  expect(y, 'de klik valt boven het vak').toBeLessThan(bird.y);
  expect(await page.evaluate(([px, py]) => document.querySelector('.digipigie__bird').contains(document.elementFromPoint(px, py)), [x, y])).toBe(true);
  await page.mouse.click(x, y);
  await expect.poll(() => clips(page), { timeout: 1000 }).toEqual([expect.stringMatching(/stakeholder\/ruffle\.webm$/)]);
});

test('nog eens vijf klikken terwijl DigiPigie laadt, geeft één dialoog', async ({ page }) => {
  let release;
  const slow = new Promise((resolve) => { release = resolve; });
  await page.route('**/styles/digipigie.css', async (route) => {
    await slow;
    await route.continue();
  });
  await openPage(page, { manifest: true });
  await tapLogo(page, 5);
  await page.waitForTimeout(1100);
  await tapLogo(page, 5);
  // De tweede reeks moet binnen zijn voordat de stijl er is.
  await page.waitForTimeout(300);
  release();
  await expect(party(page)).toBeVisible();
  await page.waitForTimeout(300);
  await expect(page.locator('dialog.dialog--digipigie')).toHaveCount(1);
  await expect(page.locator('link[href$="digipigie.css"]')).toHaveCount(1);
  await page.keyboard.press('Escape');
  await expect(party(page)).toBeHidden();
  await expect(page.locator('html')).not.toHaveClass(/has-dialog/);
});

test('sluiten midden in een clip zet de duif terug in rust', async ({ page }) => {
  await openPage(page, { manifest: true });
  await tapLogo(page, 5);
  await expect.poll(() => clips(page)).toHaveLength(1);
  await page.keyboard.press('Escape');
  const video = page.locator('.digipigie__bird video');
  await expect.poll(() => video.evaluate((v) => v.getAttribute('src'))).toBeNull();
  await expect(page.locator('.digipigie__bird .card__pigeon')).not.toHaveClass(/is-playing/);
});

test.describe('op een telefoon', () => {
  test.use({ viewport: { width: 390, height: 844 } });

  test('staat het logo niet in de kop, maar werkt het logo in de voet', async ({ page }) => {
    await openPage(page, { manifest: true });
    await expect(page.locator('header [data-brands]')).toBeHidden();
    await tapLogo(page, 5, 'footer');
    await expect(party(page)).toBeVisible();
    const box = await page.locator('.digipigie__bird').boundingBox();
    expect(box.x).toBeGreaterThanOrEqual(0);
    expect(box.x + box.width).toBeLessThanOrEqual(390);
  });
});

test('met minder beweging een stil beeld: geen clips', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openPage(page, { manifest: true });
  await tapLogo(page, 5);
  await expect(party(page)).toBeVisible();
  await page.locator('.digipigie__bird').click();
  await page.waitForTimeout(1000);
  expect(await clips(page)).toEqual([]);
});
