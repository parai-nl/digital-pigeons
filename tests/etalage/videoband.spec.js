// De videoband volgt de verhouding van de uitlegvideo: met width en
// height in CONFIG.video krijgen poster en speler die verhouding, zonder balken;
// zonder die velden blijft de band 16:9. De verhouding gaat via de CSSOM; de
// fixture in helpers.js laat de test falen bij elke CSP-melding.
import { test, expect, openPage, COPY, CONFIG } from './helpers.js';

async function withVideo(page, extra) {
  const config = await page.request.get('/etalage/config.js').then((r) => r.text());
  const video = { src: 'etalage/media/video/uitleg.webm', poster: 'etalage/media/video/uitleg.webp', vtt: { en: 'etalage/media/video/uitleg.en.vtt' }, ...extra };
  await page.route('**/etalage/config.js', (route) => route.fulfill({
    contentType: 'text/javascript',
    body: config.replace(/^(\s*)video: .*,$/m, `$1video: ${JSON.stringify(video)},`),
  }));
  await page.route('**/etalage/media/video/uitleg.webm', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/clip.webm' }));
  await page.route('**/etalage/media/video/uitleg.webp', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/plaat.webp' }));
  await page.route('**/etalage/media/video/uitleg.en.vtt', (route) => route.fulfill({ contentType: 'text/vtt', body: 'WEBVTT\n' }));
}

const ratio = (box) => box.width / box.height;

for (const [name, extra, expected] of [
  ['4:3 met eigen maat', { width: 1440, height: 1080 }, 4 / 3],
  ['16:9 met eigen maat', { width: 1920, height: 1080 }, 16 / 9],
  ['16:9 zonder maat', {}, 16 / 9],
]) {
  for (const width of [1280, 360]) {
    test(`videoband ${name} op ${width} px: poster en speler in dezelfde verhouding`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await withVideo(page, extra);
      await openPage(page);
      const frame = page.locator('[data-video-play]');
      await frame.scrollIntoViewIfNeeded();
      const box = await frame.boundingBox();
      expect(ratio(box)).toBeCloseTo(expected, 1);
      // Een 4:3-band wordt op een breed scherm niet hoger dan 420 px.
      expect(box.height).toBeLessThanOrEqual(421);
      await page.getByRole('button', { name: COPY.nl.video.play }).click();
      const player = page.locator('.band__video');
      await expect(player).toBeVisible();
      const played = await player.boundingBox();
      expect(ratio(played)).toBeCloseTo(expected, 1);
      expect(Math.abs(played.height - box.height)).toBeLessThanOrEqual(2);
      expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBe(0);
    });
  }
}

test('width en height in config.js zijn de echte maat van de uitlegvideo', async ({ page }) => {
  await openPage(page);
  const { src, width, height } = CONFIG.video;
  const real = await page.evaluate((url) => new Promise((resolve, reject) => {
    const video = document.createElement('video');
    video.preload = 'metadata';
    video.muted = true;
    video.addEventListener('loadedmetadata', () => resolve({ width: video.videoWidth, height: video.videoHeight }), { once: true });
    video.addEventListener('error', () => reject(new Error(`${url} laadt niet`)), { once: true });
    video.src = url;
  }), src);
  expect(real).toEqual({ width, height });
});
