// De mini-vergadering: één gedeeld verslag,
// reacties per duif (spraak, signaal, stilte), geluid alleen na een klik en een
// geluidsknop die zijn keuze onthoudt.
import { test, expect, openPage, chooseMode, choosePigeon, statement, shown, played, primed, clips, storageWrites, COPY, fill } from './helpers.js';

const nl = COPY.nl;
const done = (page) => expect(page.locator('#panel-meeting')).toHaveAttribute('data-busy', 'false', { timeout: 12_000 });

async function say(page, id) {
  await page.locator(`[data-statement="${id}"]`).click();
  await expect(page.locator(`[data-said="${id}"]`)).toBeVisible();
  await done(page);
}

test('er klinkt niets zolang de bezoeker niet klikt', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await page.waitForTimeout(500);
  expect(await played(page)).toEqual([]);
});

test('een uitspraak komt met spreker in het verslag en de Kennispartner reageert met tekst, bronnen en stem', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  const s = statement('poep');
  await say(page, 'poep');
  const row = page.locator('[data-said="poep"]');
  await expect(row.locator('.mrow__speaker')).toHaveText(s.speaker.nl);
  await expect(row.locator('.mrow__text')).toHaveText(s.text.nl);
  const reaction = page.locator('[data-reaction="speech"][data-author="kennispartner"]');
  await expect(reaction.locator('.msg__text')).toHaveText(shown(s.reactions.nl.kennispartner.text));
  await expect(reaction.locator('.chip--sources')).toHaveText(nl.chat.sourcesOne);
  expect((await played(page)).some((src) => src.includes('etalage/media/geluid/stem/fixture-poep-nl-kennispartner.mp3'))).toBe(true);
  // Een gezegde uitspraak kan niet nog eens.
  await expect(page.locator('[data-statement="poep"]')).toHaveAttribute('aria-disabled', 'true');
});

// De geluidsknop is een schakelaar, geen opdracht: "Geluid" met een pil die de
// stand toont (gevuld = aan) en de stand klein ernaast. Wie hem uitzet, ziet bij
// de uitspraken dat het geluid uit staat, met een knop die het weer aanzet.
const track = (page) => page.locator('[data-sound] .soundbtn__track').evaluate((el) => {
  const knob = getComputedStyle(el, '::after');
  return { filled: getComputedStyle(el).backgroundColor !== 'rgba(0, 0, 0, 0)', knobRight: knob.transform !== 'none' };
});

test('de geluidsschakelaar toont zijn stand in beeld en in aria-checked, en onthoudt hem', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  // De naam is alleen "Geluid"; de stand zit in aria-checked.
  const toggle = page.getByRole('switch', { name: nl.meeting.soundLabel, exact: true });
  await expect(toggle).toHaveAttribute('aria-checked', 'true');
  await expect(toggle.locator('.soundbtn__state')).toHaveText(nl.meeting.soundOn);
  await expect.poll(() => track(page)).toEqual({ filled: true, knobRight: true });
  await expect(page.locator('[data-sound-note]')).toBeHidden();

  await toggle.click();
  await expect(toggle).toHaveAttribute('aria-checked', 'false');
  await expect(toggle.locator('.soundbtn__state')).toHaveText(nl.meeting.soundOff);
  await expect.poll(() => track(page)).toEqual({ filled: false, knobRight: false });
  expect(await storageWrites(page)).toContainEqual(['duif_etalage_geluid', 'uit']);

  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await chooseMode(page, 'meeting');
  await expect(page.getByRole('switch', { name: nl.meeting.soundLabel, exact: true })).toHaveAttribute('aria-checked', 'false');
  await expect(page.locator('[data-sound-note]')).toBeVisible();

  // Weer aan via de regel, en ook dat blijft na herladen.
  await page.locator('[data-sound-note]').getByRole('button', { name: nl.meeting.soundTurnOn }).click();
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await chooseMode(page, 'meeting');
  await expect(page.getByRole('switch', { name: nl.meeting.soundLabel, exact: true })).toHaveAttribute('aria-checked', 'true');
  await expect(page.locator('[data-sound-note]')).toBeHidden();
});

test('met het geluid uit staat er een rustige regel en klinkt er niets; "Zet aan" herstelt het', async ({ page }) => {
  await openPage(page, { storage: { duif_etalage_geluid: 'uit' } });
  await chooseMode(page, 'meeting');
  const note = page.locator('[data-sound-note]');
  await expect(note).toBeVisible();
  await expect(note).toContainText(nl.meeting.soundOffNote);

  await say(page, 'poep');
  await expect(page.locator('[data-reaction="speech"]')).toBeVisible();
  expect(await played(page)).toEqual([]);
  expect(await primed(page)).toEqual([]);

  await note.getByRole('button', { name: nl.meeting.soundTurnOn }).click();
  const toggle = page.getByRole('switch', { name: nl.meeting.soundLabel, exact: true });
  await expect(toggle).toHaveAttribute('aria-checked', 'true');
  await expect(toggle).toBeFocused();
  await expect(note).toBeHidden();
  expect(await storageWrites(page)).toContainEqual(['duif_etalage_geluid', 'aan']);

  await say(page, 'afschot');
  expect((await played(page)).some((src) => src.includes('fixture-afschot-nl-kennispartner.mp3'))).toBe(true);
});

test('de Signalerende duif reageert met beweging en echt gekoer, zonder tekst of label', async ({ page }) => {
  await openPage(page, { manifest: true });
  await choosePigeon(page, 'signalerend');
  await say(page, 'afschot');
  const signal = page.locator('[data-reaction="signal"]');
  await expect(signal).toBeVisible();
  // Alleen een neutrale markering; de schermlezer hoort dat er een signaal was.
  const visibleText = await signal.evaluate((el) => {
    const copy = el.cloneNode(true);
    copy.querySelectorAll('.sr-only').forEach((node) => node.remove());
    return copy.textContent.trim();
  });
  expect(visibleText).toBe('');
  await expect(signal.locator('.sr-only')).toHaveText(fill(nl.meeting.signal, { nameThe: nl.pigeons.signalerend.nameThe }));
  expect((await played(page)).some((src) => src.includes('etalage/media/geluid/koer/'))).toBe(true);
  expect((await clips(page)).some((src) => /duiven\/signalerend\/(ruffle|peck)\.webm$/.test(src))).toBe(true);
});

test('stilte is echt niets', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await say(page, 'begroting');
  await page.waitForTimeout(400);
  await expect(page.locator('[data-reaction]')).toHaveCount(0);
  expect(await played(page)).toEqual([]);
});

test('zelfde vergadering, andere duif: het verslag blijft, de reacties zijn van de nieuwe duif en spelen niet af', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await say(page, 'poep');
  await say(page, 'voorjaar');
  // De Kennispartner zwijgt bij het voorjaar.
  await expect(page.locator('[data-reaction]')).toHaveCount(1);
  const before = (await played(page)).length;

  await choosePigeon(page, 'stakeholder');
  await expect(page.locator('#tab-meeting')).toHaveAttribute('aria-selected', 'true');
  await expect(page.locator('[data-said]')).toHaveCount(2);
  await expect(page.locator('[data-meeting-table]')).toHaveText(fill(nl.meeting.atTable, { name: nl.pigeons.stakeholder.name }));
  const reactions = page.locator('[data-reaction="speech"][data-author="stakeholder"] .msg__text');
  await expect(reactions).toHaveText([
    shown(statement('poep').reactions.nl.stakeholder.text),
    shown(statement('voorjaar').reactions.nl.stakeholder.text),
  ]);
  await page.waitForTimeout(300);
  expect((await played(page)).length).toBe(before);

  await choosePigeon(page, 'signalerend');
  await expect(page.locator('[data-said]')).toHaveCount(2);
  await expect(page.locator('[data-reaction="signal"]')).toHaveCount(1);
});

test('bij minder beweging speelt er geen clip, maar blijft het signaal als stille markering staan', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' });
  await openPage(page, { manifest: true });
  await choosePigeon(page, 'signalerend');
  await say(page, 'voorjaar');
  const signal = page.locator('[data-reaction="signal"]');
  await expect(signal).toBeVisible();
  await expect(signal).not.toHaveClass(/is-live/);
  expect(await clips(page)).toEqual([]);
});

test('opnieuw beginnen maakt het verslag leeg', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await say(page, 'begroting');
  await page.getByRole('button', { name: nl.meeting.restart }).click();
  await expect(page.locator('[data-said]')).toHaveCount(0);
  await expect(page.locator('.meeting__empty')).toHaveText(nl.meeting.empty);
  await expect(page.locator('[data-statement="begroting"]')).toHaveAttribute('aria-disabled', 'false');
});

test('de knop Beluister speelt de stem opnieuw af en Stop zet hem stil', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await say(page, 'afschot');
  const listen = page.locator('[data-listen]');
  await expect(listen).toHaveText(nl.meeting.listen);
  const before = (await played(page)).length;
  await listen.click();
  expect((await played(page)).length).toBe(before + 1);
  await expect(listen).toHaveText(nl.meeting.stop);
  await listen.click();
  await expect(listen).toHaveText(nl.meeting.listen);
});

test('de geluidsknop zet een klinkende stem meteen stil', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await say(page, 'afschot');
  const listen = page.locator('[data-listen]');
  await listen.click();
  await expect(listen).toHaveText(nl.meeting.stop);
  await page.getByRole('switch', { name: nl.meeting.soundLabel, exact: true }).click();
  await expect(listen).toHaveText(nl.meeting.listen);
  await expect(listen).toBeDisabled();
});

test('een nieuwe uitspraak onderbreekt de lopende reactie en begint meteen', async ({ page }) => {
  await openPage(page);
  await chooseMode(page, 'meeting');
  await page.locator('[data-statement="poep"]').click();
  const listen = page.locator('[data-reaction="speech"] [data-listen]');
  await expect(listen).toHaveText(nl.meeting.stop, { timeout: 6000 });
  // Tijdens de reactie blijven de andere uitspraken gewoon bruikbaar.
  await expect(page.locator('[data-statement="afschot"]')).toHaveAttribute('aria-disabled', 'false');
  await page.locator('[data-statement="afschot"]').click();
  await expect(page.locator('[data-said="afschot"]')).toBeVisible();
  await expect(listen.first()).toHaveText(nl.meeting.listen);
  await expect(page.locator('[data-reaction="speech"]')).toHaveCount(2, { timeout: 6000 });
  await expect(page.locator('#panel-meeting')).toHaveAttribute('data-busy', 'false', { timeout: 12_000 });
});
