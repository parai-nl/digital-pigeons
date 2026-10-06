// De waaier: Kennispartner vooraan, links de
// Signalerende duif, rechts de Stakeholder; wisselen met pijlen, zijkaart,
// pijltjestoetsen en vegen; de kaart kantelt alleen met een echte muis. Tot de
// bezoeker iets doet, draait de waaier vanzelf.
import { test, expect, openPage, card, slot, clips, COPY, OPNAMES, choosePigeon } from './helpers.js';

const nl = COPY.nl;

test('bij binnenkomst staat de Kennispartner vooraan, met Signalerend links en Stakeholder rechts', async ({ page }) => {
  await openPage(page);
  await expect(slot(page, 'kennispartner')).toHaveAttribute('data-rel', 'center');
  await expect(slot(page, 'signalerend')).toHaveAttribute('data-rel', 'left');
  await expect(slot(page, 'stakeholder')).toHaveAttribute('data-rel', 'right');
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
  await expect(page.locator('[data-talk-name]')).toHaveText(nl.pigeons.kennispartner.name);
  await expect(page.locator('[data-talk-chip]')).toHaveText(nl.pigeons.kennispartner.chip);
  // Alleen de Stakeholder draagt de badge.
  await expect(page.locator('.card__badge')).toHaveCount(1);
  await expect(card(page, 'stakeholder').locator('.card__badge')).toHaveText(nl.badge);
});

test('de pijlen draaien de waaier en de tekst onder de kaart volgt', async ({ page }) => {
  await openPage(page);
  await page.getByRole('button', { name: nl.carousel.next }).click();
  await expect(slot(page, 'stakeholder')).toHaveAttribute('data-rel', 'center');
  await expect(slot(page, 'kennispartner')).toHaveAttribute('data-rel', 'left');
  await expect(page.locator('[data-talk-name]')).toHaveText(nl.pigeons.stakeholder.name);
  await expect(page.locator('[data-talk-blurb]')).toHaveText(nl.pigeons.stakeholder.blurb);

  await page.getByRole('button', { name: nl.carousel.prev }).click();
  await page.getByRole('button', { name: nl.carousel.prev }).click();
  await expect(slot(page, 'signalerend')).toHaveAttribute('data-rel', 'center');
  await expect(page.locator('[data-fan-status]')).toHaveText(nl.carousel.current.replace('{name}', nl.pigeons.signalerend.name));
});

test('klikken op een zijkaart zet die duif vooraan', async ({ page }) => {
  await openPage(page);
  await card(page, 'signalerend').click();
  await expect(card(page, 'signalerend')).toHaveAttribute('aria-current', 'true');
  await expect(card(page, 'kennispartner')).not.toHaveAttribute('aria-current', 'true');
});

test('met de pijltjestoetsen wissel je van duif en blijft de focus op de kaart vooraan', async ({ page }) => {
  await openPage(page);
  await card(page, 'kennispartner').focus();
  await page.keyboard.press('ArrowRight');
  await expect(card(page, 'stakeholder')).toHaveAttribute('aria-current', 'true');
  await expect(card(page, 'stakeholder')).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('ArrowLeft');
  await expect(card(page, 'signalerend')).toHaveAttribute('aria-current', 'true');
  await expect(card(page, 'signalerend')).toBeFocused();
});

test('vegen op een touchscherm wisselt van duif', async ({ page }) => {
  await openPage(page);
  const box = await page.locator('.fan__track').boundingBox();
  const y = box.y + box.height / 2;
  const x = box.x + box.width / 2;
  await page.locator('.fan__track').dispatchEvent('pointerdown', { pointerType: 'touch', clientX: x, clientY: y, isPrimary: true });
  await page.locator('.fan__track').dispatchEvent('pointerup', { pointerType: 'touch', clientX: x - 120, clientY: y + 10, isPrimary: true });
  await expect(card(page, 'stakeholder')).toHaveAttribute('aria-current', 'true');
});

test('de Signalerende duif heeft alleen de vergadering', async ({ page }) => {
  await openPage(page);
  await choosePigeon(page, 'signalerend');
  await expect(page.locator('#tab-chat')).toBeHidden();
  await expect(page.locator('#tab-meeting')).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('#panel-meeting')).toBeVisible();
  await expect(page.locator('#panel-chat')).toBeHidden();
  await expect(page.locator('[data-talk-chip]')).toHaveText(nl.pigeons.signalerend.chip);
});

test('met een echte muis kantelt de kaart vooraan; bij minder beweging niet', async ({ page }) => {
  await openPage(page);
  const front = card(page, 'kennispartner');
  const box = await front.boundingBox();
  await page.mouse.move(box.x + box.width * 0.9, box.y + box.height * 0.2);
  await page.mouse.move(box.x + box.width * 0.95, box.y + box.height * 0.15);
  await expect(front).toHaveClass(/is-tilting/);
  const tilt = Number(await front.evaluate((el) => el.style.getPropertyValue('--tilt-x')));
  expect(tilt).toBeGreaterThan(0.5);
  await page.mouse.move(5, 5);
  await expect(front).not.toHaveClass(/is-tilting/);

  await page.emulateMedia({ reducedMotion: 'reduce' });
  await page.mouse.move(box.x + box.width * 0.9, box.y + box.height * 0.2);
  await page.mouse.move(box.x + box.width * 0.95, box.y + box.height * 0.15);
  await expect(front).not.toHaveClass(/is-tilting/);
});

test('met het manifest staat de duif als eigen laag op de plaat en steekt zij boven de kaart uit', async ({ page }) => {
  await openPage(page, { manifest: true });
  await expect(page.locator('body')).toHaveAttribute('data-beeld', 'manifest');
  const front = card(page, 'kennispartner');
  await expect(front.locator('.card__plate-img')).toHaveAttribute('src', 'etalage/media/duiven/kennispartner/plaat.webp');
  await expect(front.locator('.card__rest')).toHaveAttribute('src', 'etalage/media/duiven/kennispartner/rust.webp');
  const cardBox = await front.boundingBox();
  const layerBox = await front.locator('.card__pigeon').boundingBox();
  expect(layerBox.y).toBeLessThan(cardBox.y);
  // De ruimte boven de kaarten is vrijgehouden: de laag valt niet over de kop erboven.
  const line = await page.locator('.hero__line').boundingBox();
  expect(layerBox.y).toBeGreaterThanOrEqual(line.y + line.height);
});

test('zonder manifest staan de portretten binnen de kaart', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('body')).toHaveAttribute('data-beeld', 'portretten');
  const front = card(page, 'kennispartner');
  await expect(front.locator('.card__portrait')).toHaveAttribute('src', 'etalage/media/portretten/kennispartner.webp');
  await expect(front.locator('.card__pigeon')).toHaveCount(0);
  const loaded = await front.locator('.card__portrait').evaluate((img) => img.complete && img.naturalWidth > 0);
  expect(loaded).toBe(true);
});

test('na een veeg zonder klik werkt de volgende tik gewoon', async ({ page }) => {
  await openPage(page);
  const track = page.locator('.fan__track');
  const box = await track.boundingBox();
  const y = box.y + box.height / 2;
  const x = box.x + box.width / 2;
  await track.dispatchEvent('pointerdown', { pointerType: 'touch', clientX: x, clientY: y, isPrimary: true });
  await track.dispatchEvent('pointerup', { pointerType: 'touch', clientX: x - 120, clientY: y, isPrimary: true });
  await expect(card(page, 'stakeholder')).toHaveAttribute('aria-current', 'true');
  await page.waitForTimeout(500);
  await card(page, 'signalerend').click();
  await expect(card(page, 'signalerend')).toHaveAttribute('aria-current', 'true');
});

// De duif vooraan groet met één gebaar op haar kaart: bij binnenkomst, en elke
// keer dat de bezoeker een duif naar voren haalt. Bij minder beweging nooit. De
// chat maakt geen gebaren.
test('de duif vooraan maakt één gebaar, bij binnenkomst en als de bezoeker haar naar voren haalt; in de chat niet', async ({ page }) => {
  await openPage(page, { manifest: true });
  await expect.poll(() => clips(page)).toHaveLength(1);
  expect((await clips(page))[0]).toMatch(/\/kennispartner\/\w+\.webm$/);

  await page.getByRole('button', { name: nl.carousel.next }).click();
  await expect.poll(() => clips(page)).toHaveLength(2);
  expect((await clips(page))[1]).toMatch(/\/stakeholder\/\w+\.webm$/);
  await expect(card(page, 'stakeholder').locator('.card__pigeon')).toHaveClass(/is-playing/);

  await page.locator('[data-question]').first().click();
  await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
  expect(await clips(page)).toHaveLength(2);

  await page.keyboard.press('Escape');
  await card(page, 'kennispartner').click();
  await expect.poll(() => clips(page)).toHaveLength(3);
});

// Tot de bezoeker iets doet draait de waaier vanzelf, en elke duif die vooraan
// komt maakt haar gebaar. De live-regio zwijgt daarbij.
test('zonder handeling draait de waaier vanzelf en groet elke duif; de tekst onder de kaart volgt', async ({ page }) => {
  await openPage(page, { manifest: true });
  await expect(slot(page, 'stakeholder')).toHaveAttribute('data-rel', 'center', { timeout: 10_000 });
  await expect(page.locator('[data-talk-name]')).toHaveText(nl.pigeons.stakeholder.name);
  await expect(slot(page, 'signalerend')).toHaveAttribute('data-rel', 'center', { timeout: 10_000 });
  await expect.poll(() => clips(page)).toHaveLength(3);
  expect((await clips(page)).map((src) => src.split('/').at(-2))).toEqual(['kennispartner', 'stakeholder', 'signalerend']);
  await expect(page.locator('[data-fan-status]')).toHaveText('');
});

const autoBtn = (page) => page.locator('[data-fan-auto]');

test('een klik van de bezoeker zet het vanzelf draaien uit; de knop toont dat en zet het weer aan', async ({ page }) => {
  await openPage(page);
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'on');
  await expect(autoBtn(page)).toHaveAccessibleName(nl.carousel.autoStop);
  await card(page, 'signalerend').click();
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'off');
  await expect(autoBtn(page)).toHaveAccessibleName(nl.carousel.autoStart);
  await page.waitForTimeout(6000);
  await expect(card(page, 'signalerend')).toHaveAttribute('aria-current', 'true');

  await autoBtn(page).click();
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'on');
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true', { timeout: 10_000 });

  await autoBtn(page).click();
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'off');
});

test('na twee minuten rust draait de waaier weer vanzelf', async ({ page }) => {
  await page.clock.install();
  await openPage(page);
  await card(page, 'signalerend').click();
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'off');
  await page.clock.runFor(119_000);
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'off');
  await expect(card(page, 'signalerend')).toHaveAttribute('aria-current', 'true');
  await page.clock.runFor(2_000);
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'on');
  await page.clock.runFor(6_000);
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
});

test('tabben naar de knop zet het vanzelf draaien niet uit; een andere toets wel', async ({ page }) => {
  await openPage(page);
  await page.keyboard.press('Tab');
  await page.keyboard.press('Shift+Tab');
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'on');
  await page.keyboard.press('ArrowDown');
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'off');
});

// Een wissel van duif breekt de reactie in het paneel eronder af. Zolang die
// loopt, wacht de waaier dus, ook als de bezoeker de knop weer aanzet. De stem
// is hier een minuut lang en komt nooit aan, zodat de reactie blijft lopen.
test('zolang de vergadering een reactie afspeelt wisselt de waaier niet vanzelf', async ({ page }) => {
  const data = structuredClone(OPNAMES);
  data.meeting.statements.find((s) => s.id === 'poep').reactions.nl.kennispartner.audioMs = 60_000;
  await openPage(page, { data });
  await page.route('**/etalage/media/geluid/**', () => {});
  await page.locator('#tab-meeting').click();
  await page.locator('[data-statement="poep"]').click();
  await autoBtn(page).click();
  await expect(autoBtn(page)).toHaveAttribute('data-auto', 'on');
  await page.waitForTimeout(7000);
  await expect(page.locator('#panel-meeting')).toHaveAttribute('data-busy', 'true');
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
});

test('bij minder beweging draait de waaier niet vanzelf', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openPage(page, { manifest: true });
  await page.waitForTimeout(6000);
  await expect(card(page, 'kennispartner')).toHaveAttribute('aria-current', 'true');
  expect(await clips(page)).toEqual([]);
  await expect(autoBtn(page)).toBeHidden();
});

test('bij minder beweging groet de duif vooraan niet', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openPage(page, { manifest: true });
  await page.getByRole('button', { name: nl.carousel.next }).click();
  await expect(card(page, 'stakeholder')).toHaveAttribute('aria-current', 'true');
  await page.waitForTimeout(900);
  expect(await clips(page)).toEqual([]);
});
