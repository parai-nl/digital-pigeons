// Vorm en toegankelijkheid: geen horizontaal scrollen van 360 tot 1920 px in
// beide talen, toegankelijke namen, geen verzoeken naar buiten, #methode, de
// feiten uit opnames.json en de secties die later gevuld worden.
import { test, expect, openPage, ask, chooseMode, COPY, CONFIG, OPNAMES, fill } from './helpers.js';

const WIDTHS = [360, 768, 1280, 1920];

for (const lang of ['nl', 'en']) {
  for (const width of WIDTHS) {
    test(`geen horizontale overloop op ${width} px (${lang})`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await openPage(page, { url: `/?lang=${lang}`, manifest: width === 768 });
      await ask(page, 'afschot');
      await page.locator('[data-ask-other]').click();
      await expect(page.locator('#panel-chat')).toHaveAttribute('data-busy', 'false', { timeout: 15_000 });
      await chooseMode(page, 'meeting');
      await page.locator('[data-method] summary').click();
      const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
      expect(overflow).toBe(0);
    });
  }
}

test('op 200% zoom (640 px breed) blijft alles bereikbaar zonder horizontaal scrollen', async ({ page }) => {
  await page.setViewportSize({ width: 640, height: 450 });
  await openPage(page);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
  expect(overflow).toBe(0);
  await expect(page.locator('[data-login]')).toBeVisible();
  await expect(page.locator('[data-faq-open]')).toBeVisible();
});

test('elke knop en link heeft een toegankelijke naam, elk beeld een alt', async ({ page }) => {
  await openPage(page, { manifest: true });
  await ask(page, 'afschot');
  const unnamed = await page.evaluate(() => [...document.querySelectorAll('button, a[href], [role="switch"], [role="tab"]')]
    .filter((el) => el.getClientRects().length)
    .filter((el) => {
      const label = el.getAttribute('aria-label') || el.textContent || '';
      const labelledBy = el.getAttribute('aria-labelledby');
      return !label.trim() && !labelledBy;
    })
    .map((el) => el.outerHTML.slice(0, 120)));
  expect(unnamed).toEqual([]);
  const noAlt = await page.evaluate(() => [...document.querySelectorAll('img')].filter((img) => !img.hasAttribute('alt')).map((img) => img.src));
  expect(noAlt).toEqual([]);
  // Oriëntatiepunten en één h1.
  await expect(page.locator('header.topbar')).toHaveCount(1);
  await expect(page.locator('main')).toHaveCount(1);
  await expect(page.locator('footer.footer')).toHaveCount(1);
  await expect(page.locator('h1')).toHaveCount(1);
});

test('de kop: titel, NL/EN, een ingetogen Inloggen en de logo\'s', async ({ page }) => {
  await openPage(page);
  const nl = COPY.nl;
  await expect(page.locator('.topbar__title')).toHaveText(nl.header.title);
  await expect(page.getByRole('link', { name: nl.header.login })).toHaveAttribute('href', CONFIG.loginUrl);
  await expect(page.locator('header [data-brands]')).toHaveAttribute('aria-label', nl.header.brands);
  const logos = await page.locator('header [data-brands] img').evaluateAll((imgs) => imgs.map((img) => img.complete && img.naturalWidth > 0));
  expect(logos).toEqual([true, true]);
});

test('de pagina doet geen enkel verzoek buiten de eigen oorsprong', async ({ page, baseURL }) => {
  const outside = [];
  page.on('request', (request) => {
    const url = request.url();
    if (!url.startsWith(baseURL) && !url.startsWith('data:') && !url.startsWith('blob:')) outside.push(url);
  });
  await openPage(page, { manifest: true });
  await ask(page, 'afschot');
  await page.locator('.chip--sources').first().click();
  await page.keyboard.press('Escape');
  await chooseMode(page, 'meeting');
  await page.locator('[data-statement="poep"]').click();
  await expect(page.locator('[data-reaction]')).toBeVisible({ timeout: 6000 });
  await page.locator('[data-method] summary').click();
  await page.getByRole('button', { name: COPY.nl.hero.faq }).click();
  await page.keyboard.press('Escape');
  await page.goto('/etalage/print/?kind=cards');
  await expect(page.locator('.print-card')).toHaveCount(6);
  expect(outside).toEqual([]);
});

test('#methode opent het methodeblok en scrolt ernaartoe', async ({ page }) => {
  await openPage(page, { url: '/#methode' });
  const method = page.locator('#methode');
  await expect(method).toHaveAttribute('open', '');
  await expect(method.locator('summary')).toBeInViewport();
  await expect(method.locator('dt')).toHaveText(COPY.nl.method.items.map((item) => item.title));
});

test('de methode leest de getallen uit opnames.json, per taal opgemaakt', async ({ page }) => {
  await openPage(page);
  await page.locator('[data-method] summary').click();
  await expect(page.locator('#methode')).toContainText('4.067 uitspraken en 4.225 geciteerde passages');
  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await expect(page.locator('#methode')).toContainText('4,067 statements and 4,225 quoted passages');
});

test('het kennisgraafbeeld volgt de taal; zonder beeld staat er geen kapot beeld', async ({ page }) => {
  await openPage(page);
  await page.locator('[data-method] summary').click();
  const img = page.locator('[data-method-graph] img');
  await expect(img).toHaveAttribute('src', 'etalage/media/beeld/kennisgraaf-licht.webp');
  await expect.poll(() => img.evaluate((el) => el.complete && el.naturalWidth)).toBeGreaterThan(0);
  await page.getByRole('button', { name: 'EN', exact: true }).click();
  await expect(page.locator('[data-method-graph] img')).toHaveAttribute('src', 'etalage/media/beeld/kennisgraaf-licht-en.webp');

  await page.route('**/etalage/media/beeld/**', (route) => route.fulfill({ status: 404 }));
  await page.reload();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await page.locator('[data-method] summary').click();
  await expect(page.locator('[data-method-graph]')).toHaveCount(0);
});

test('video, posters en thesis staan op "binnenkort" zolang de config leeg is', async ({ page }) => {
  // De lege stand zet de test zelf op: de echte config.js heeft een video,
  // posters en kaartjes.
  const config = await page.request.get('/etalage/config.js').then((r) => r.text());
  await page.route('**/etalage/config.js', (route) => route.fulfill({
    contentType: 'text/javascript',
    body: config
      .replace(/^(\s*)video: .*,$/m, '$1video: null,')
      .replace(/^(\s*)posters: \[[\s\S]*?\n\1\],$/m, '$1posters: [],')
      .replace(/^(\s*)cards: \[[\s\S]*?\n\1\],$/m, '$1cards: [],'),
  }));
  await openPage(page);
  const nl = COPY.nl;
  await expect(page.locator('[data-video-soon]')).toHaveAttribute('aria-label', nl.video.soonLabel);
  await expect(page.locator('[data-poster-soon]')).toHaveCount(nl.research.posters.length);
  await expect(page.locator('.poster__title')).toHaveText(nl.research.posters.map((p) => p.title));
  // Zonder kaartjes verdwijnt het hele blok, kop en uitleg inbegrepen.
  await expect(page.locator('[data-cards-block]')).toBeHidden();
  // De thesis en de paper staan er zonder doel; Over en print zijn gewone links.
  await expect(page.locator('[data-link="thesis"]')).toHaveText(nl.research.links.thesis);
  await expect(page.locator('a[data-link="thesis"]')).toHaveCount(0);
  await expect(page.locator('[data-link="paper"]')).toHaveText(nl.research.links.paper);
  await expect(page.getByRole('link', { name: nl.research.links.about })).toHaveAttribute('href', CONFIG.aboutUrl);
  // Het materiaal voor groepssessies: de ontworpen posters en kaartjes als pdf om te downloaden.
  for (const kind of ['posters', 'cards']) {
    const file = CONFIG.downloads[kind];
    const size = new Intl.NumberFormat('nl', { maximumFractionDigits: 1 }).format(file.bytes / 1e6);
    const link = page.getByRole('link', { name: fill(nl.research.links.download[kind], { size }) });
    await expect(link).toHaveAttribute('href', file.src);
    await expect(link).toHaveAttribute('download', file.src.split('/').pop());
    const response = await page.request.get(`/${file.src}`);
    expect(response.status()).toBe(200);
    expect(response.headers()['content-type']).toBe('application/pdf');
    expect((await response.body()).byteLength).toBe(file.bytes);
  }
});

test('de voet noemt afzender en de naamsvermelding van het beeld, met bron- en licentielink', async ({ page }) => {
  await openPage(page);
  await expect(page.locator('.footer__line')).toHaveText(COPY.nl.footer.line);
  const links = page.locator('[data-credits] a');
  await expect(links).toHaveCount(2);
  await expect(links.nth(0)).toHaveAttribute('href', 'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg');
  await expect(links.nth(1)).toHaveText('CC BY-SA 3.0');
  await expect(links.nth(1)).toHaveAttribute('href', 'https://creativecommons.org/licenses/by-sa/3.0/');
});

test('met een manifest komt de naamsvermelding uit het manifest, licentie als eigen link', async ({ page }) => {
  await openPage(page, { manifest: true });
  await expect(page.locator('[data-credits]')).toHaveText('[fixture] Beeldvermelding NL · CC BY 4.0');
  await expect(page.locator('[data-credits] a').nth(1)).toHaveAttribute('href', 'https://example.org/fixture/license');
});

test('het printmateriaal is printbaar in NL en EN, met poster en zes kaarten', async ({ page }) => {
  await page.goto('/etalage/print/?lang=en&kind=poster');
  await expect(page.locator('html')).toHaveAttribute('lang', 'en');
  await expect(page.locator('.print-pigeon')).toHaveCount(3);
  await expect(page.locator('.print-pigeon h2')).toHaveText(['Knowledge Partner', 'Signaling Pigeon', 'Stakeholder']);
  const portraits = await page.locator('.print-pigeon img').evaluateAll((imgs) => Promise.all(imgs.map((img) => img.decode().then(() => img.naturalWidth > 0, () => false))));
  expect(portraits).toEqual([true, true, true]);
  // De koppen zijn beelden van het 3D-model: de poster draagt dezelfde
  // naamsvermelding als de voet van de etalage, met bron en licentie.
  const credit = page.locator('[data-print-credits]');
  await expect(credit).toContainText('3D pigeon: Digitale Duiven, photo texture by Luc Viatour / Lucnix.be');
  expect(await credit.locator('a').evaluateAll((as) => as.map((a) => a.href))).toEqual([
    'https://commons.wikimedia.org/wiki/File:Columba_livia_Luc_Viatour.jpg',
    'https://creativecommons.org/licenses/by-sa/3.0/',
  ]);
  await page.getByRole('button', { name: 'NL', exact: true }).click();
  await expect(credit).toContainText('3D-duif: Digitale Duiven, fototextuur Luc Viatour / Lucnix.be');
  // Met de vermelding blijft de poster één A4, ook in het langere Nederlands.
  const pages = (pdf) => (pdf.toString('latin1').match(/\/Type\s*\/Page[^s]/g) || []).length;
  expect(pages(await page.pdf({ format: 'A4', printBackground: true }))).toBe(1);
  await page.getByRole('button', { name: COPY.nl.print.kinds.cards }).click();
  await expect(page.locator('.print-card h2')).toHaveText(COPY.nl.print.cards.map((c) => c.title));
  // De reflectiekaarten hebben geen beeld, dus ook geen beeldvermelding.
  await expect(page.locator('[data-print-credits]')).toHaveCount(0);
  await expect(page).toHaveURL(/kind=cards/);
  const pdf = await page.pdf({ format: 'A4' });
  expect(pdf.byteLength).toBeGreaterThan(10_000);
});

test('video, poster en thesis wisselen met één regel config', async ({ page }) => {
  // De config zoals hij er straks uitziet; de bestanden zelf zijn fixtures.
  await page.route('**/etalage/config.js', (route) => route.fulfill({
    contentType: 'text/javascript',
    body: `export const CONFIG = {
      loginUrl: 'https://pigeon.parai.nl/duif',
      thesisUrl: 'https://example.org/fixture/thesis.pdf',
      aboutUrl: null,
      video: { src: 'etalage/media/video/uitleg.webm', poster: 'etalage/media/video/uitleg.webp', vtt: { nl: 'etalage/media/video/uitleg.nl.vtt', en: 'etalage/media/video/uitleg.en.vtt' } },
      posters: ['etalage/media/posters/poster-01.webp', null, null],
    };`,
  }));
  await page.route('**/etalage/media/video/uitleg.webm', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/clip.webm' }));
  await page.route('**/etalage/media/video/uitleg.webp', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/plaat.webp' }));
  await page.route('**/etalage/media/video/uitleg.nl.vtt', (route) => route.fulfill({ contentType: 'text/vtt', body: 'WEBVTT\n\n00:00.000 --> 00:01.000\n[fixture] ondertitel\n' }));
  await page.route('**/etalage/media/posters/poster-01.webp', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/plaat.webp' }));
  await openPage(page);
  const nl = COPY.nl;

  await expect(page.locator('[data-video-soon]')).toHaveCount(0);
  await page.getByRole('button', { name: nl.video.play }).click();
  const video = page.locator('.band__video');
  await expect(video).toHaveAttribute('controls', '');
  await expect(video.locator('track')).toHaveAttribute('src', 'etalage/media/video/uitleg.nl.vtt');
  await expect(video.locator('track')).toHaveAttribute('srclang', 'nl');

  await expect(page.getByRole('button', { name: fill(nl.research.view, { title: nl.research.posters[0].title }) })).toBeVisible();
  await expect(page.locator('[data-poster-soon]')).toHaveCount(2);
  await expect(page.locator('[data-cards-block]')).toBeHidden();
  await expect(page.getByRole('link', { name: nl.research.links.thesis })).toHaveAttribute('href', 'https://example.org/fixture/thesis.pdf');
  await expect(page.locator('a.rlink', { hasText: nl.research.links.about })).toHaveCount(0);
});

test('de posters en kaartjes gaan groot open, met bladeren, Escape en de focus terug', async ({ page }) => {
  await openPage(page);
  const r = COPY.nl.research;
  await expect(page.locator('[data-poster]')).toHaveCount(CONFIG.posters.length);
  await expect(page.locator('[data-card]')).toHaveCount(CONFIG.cards.length);
  const thumbs = await page.locator('[data-poster] img, [data-card] img').evaluateAll((imgs) => Promise.all(imgs.map((img) => img.decode().then(() => img.naturalWidth > 0, () => false))));
  expect(thumbs.every(Boolean)).toBe(true);

  const viewer = page.getByRole('dialog', { name: r.posters[1].title });
  const second = page.getByRole('button', { name: fill(r.view, { title: r.posters[1].title }) });
  await second.click();
  await expect(viewer).toBeVisible();
  await expect(viewer.getByRole('img', { name: r.posters[1].alt })).toBeVisible();
  await expect(page.locator('.viewer__count')).toHaveText(fill(COPY.nl.viewer.count, { n: 2, total: r.posters.length }));
  // Een poster heeft kleine letters: het beeld op ware grootte opent in een eigen tab.
  await expect(page.getByRole('link', { name: COPY.nl.viewer.full })).toHaveAttribute('href', CONFIG.posters[1].src);
  // Alt+pijl is "terug" in de browser en bladert niet; de kale pijltjes bladeren
  // binnen de posters en lopen rond.
  await page.keyboard.press('Alt+ArrowRight');
  await expect(viewer).toBeVisible();
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('dialog', { name: r.posters[2].title })).toBeVisible();
  const dialog = page.locator('.dialog--viewer');
  await dialog.getByRole('button', { name: COPY.nl.viewer.next, exact: true }).click();
  await expect(page.getByRole('dialog', { name: r.posters[0].title })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(second).toBeFocused();

  // Een liggend kaartje: voor- en achterkant samen, met de vraag van de achterkant als tekst.
  const card = page.locator('[data-card="5"]');
  await expect(card).toHaveAttribute('aria-label', fill(r.cardView, { n: 5, question: r.cards[4].front }));
  await card.click();
  const sides = page.locator('.viewer__side img');
  await expect(sides).toHaveCount(2);
  await expect(sides.nth(0)).toHaveAttribute('alt', `${r.cardFront}: ${r.cards[4].front}`);
  await expect(sides.nth(1)).toHaveAttribute('alt', `${r.cardBack}: ${r.cards[4].back}`);
  expect(await sides.evaluateAll((imgs) => Promise.all(imgs.map((img) => img.decode().then(() => img.naturalWidth > img.naturalHeight, () => false))))).toEqual([true, true]);
  await expect(page.locator('.viewer__text')).toHaveText(r.cards[4].back);
  await expect(page.getByRole('link', { name: COPY.nl.viewer.full })).toBeHidden();
  await expect(page.locator('.viewer__count')).toHaveText(fill(COPY.nl.viewer.count, { n: 5, total: r.cards.length }));
  await page.keyboard.press('ArrowLeft');
  await expect(page.getByRole('dialog', { name: r.cards[3].front })).toBeVisible();
  await dialog.getByRole('button', { name: COPY.nl.viewer.close, exact: true }).click();
  await expect(card).toBeFocused();
});

// De CSP waaronder de etalage op pigeon.parai.nl komt (de hosting-eisen).
// De testserver stuurt hem mee; de fixture in helpers.js laat elke test falen
// bij een overtreding. Deze test loopt de hele pagina af en controleert de koppen.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'";

test('de hele pagina werkt onder de CSP en Permissions-Policy van de hosting', async ({ page }) => {
  const response = await page.request.get('/');
  expect(response.headers()['content-security-policy']).toBe(CSP);
  expect(response.headers()['permissions-policy']).toBe('camera=(), microphone=(), geolocation=()');

  await openPage(page, { manifest: true });
  await ask(page, 'afschot');
  await page.locator('.chip--sources').first().click();
  await page.keyboard.press('Escape');
  await chooseMode(page, 'meeting');
  await page.locator('[data-statement="poep"]').click();
  await expect(page.locator('[data-reaction="speech"]')).toBeVisible({ timeout: 6000 });
  await page.locator('[data-listen]').click();
  await page.locator('[data-slot="signalerend"] .card').click();
  await page.locator('[data-statement="afschot"]').click();
  await expect(page.locator('[data-reaction="signal"]')).toBeVisible({ timeout: 6000 });
  await page.getByRole('button', { name: COPY.nl.hero.faq }).click();
  await page.keyboard.press('Escape');
  await page.locator('[data-method] summary').click();
  await page.goto('/etalage/print/?kind=poster');
  await expect(page.locator('.print-pigeon')).toHaveCount(3);
});

test('ondertitels in de andere taal houden hun eigen taal en label', async ({ page }) => {
  await page.route('**/etalage/config.js', (route) => route.fulfill({
    contentType: 'text/javascript',
    body: `export const CONFIG = { loginUrl: 'https://pigeon.parai.nl/duif', thesisUrl: null, aboutUrl: null,
      video: { src: 'etalage/media/video/uitleg.webm', poster: null, vtt: { nl: 'etalage/media/video/uitleg.nl.vtt' } },
      posters: [null, null, null] };`,
  }));
  await page.route('**/etalage/media/video/uitleg.webm', (route) => route.fulfill({ path: 'tests/etalage/fixtures/media/clip.webm' }));
  await page.route('**/etalage/media/video/uitleg.nl.vtt', (route) => route.fulfill({ contentType: 'text/vtt', body: 'WEBVTT\n' }));
  await openPage(page, { url: '/?lang=en' });
  await page.getByRole('button', { name: COPY.en.video.play }).click();
  const track = page.locator('.band__video track');
  await expect(track).toHaveAttribute('srclang', 'nl');
  await expect(track).toHaveAttribute('label', 'Nederlands');
});

// Een trage verbinding: de schil (kop, hero, waaier) staat er zodra het manifest
// binnen is; gesprek en vergadering wachten netjes op opnames.json.
test('met trage opnames staat de schil er meteen en wachten gesprek en vergadering netjes', async ({ page }) => {
  const nl = COPY.nl;
  let release;
  const gate = new Promise((resolve) => { release = resolve; });
  await openPage(page, { gate });
  await expect(page.locator('body')).toHaveAttribute('data-opnames', 'laden');
  await expect(page.locator('.hero__eyebrow')).toHaveText(nl.hero.eyebrow);
  await expect(page.locator('#hero-title')).toHaveText(nl.hero.title);
  await expect(page.locator('.card')).toHaveCount(3);

  await expect(page.locator('[data-chat-loading]')).toHaveText(nl.chat.loading);
  await expect(page.locator('.chat__empty')).toHaveCount(0);
  await expect(page.locator('[data-chat-unavailable]')).toBeHidden();
  await expect(page.locator('[data-question]')).toHaveCount(0);

  await chooseMode(page, 'meeting');
  await expect(page.locator('[data-meeting-loading]')).toHaveText(nl.meeting.loading);
  await expect(page.locator('.meeting__empty')).toHaveCount(0);
  await expect(page.locator('[data-meeting-unavailable]')).toBeHidden();
  await expect(page.locator('.meeting__foot')).toBeHidden();
  await expect(page.locator('[data-table-card]')).toBeVisible();

  release();
  await expect(page.locator('body')).toHaveAttribute('data-ready', 'true');
  await expect(page.locator('[data-meeting-loading]')).toHaveCount(0);
  await expect(page.locator('[data-statement]')).toHaveCount(OPNAMES.meeting.statements.length);
  await expect(page.locator('.meeting__empty')).toHaveText(nl.meeting.empty);
  await chooseMode(page, 'chat');
  await expect(page.locator('[data-chat-loading]')).toHaveCount(0);
  await expect(page.locator('[data-question]')).not.toHaveCount(0);
});

test('op een telefoon staat Inloggen voluit en de vraagknop op de regel van de bovenkop', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openPage(page);
  const nl = COPY.nl;
  await expect(page.locator('.login__text')).toHaveText(nl.header.login);
  expect(await page.locator('.login__text').evaluate((el) => el.getBoundingClientRect().width)).toBeGreaterThan(20);
  const faq = await page.getByRole('button', { name: nl.hero.faq }).boundingBox();
  const eyebrow = await page.locator('.hero__eyebrow').boundingBox();
  const title = await page.locator('#hero-title').boundingBox();
  expect(faq.y + faq.height).toBeLessThanOrEqual(title.y);
  expect(Math.abs((faq.y + faq.height / 2) - (eyebrow.y + eyebrow.height / 2))).toBeLessThan(4);
});

test('op 360 px past de titel met de themaschakelaar erbij nog op twee regels, met Inloggen voluit', async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await openPage(page);
  const title = await page.locator('.topbar__title').evaluate((el) => ({ lines: el.clientHeight / parseFloat(getComputedStyle(el).lineHeight), clipped: el.scrollWidth > el.clientWidth }));
  expect(Math.round(title.lines)).toBeLessThanOrEqual(2);
  expect(title.clipped).toBe(false);
  await expect(page.locator('.login__text')).toHaveText(COPY.nl.header.login);
  expect(await page.locator('.login__text').evaluate((el) => el.getBoundingClientRect().width)).toBeGreaterThan(20);
  await expect(page.getByRole('switch', { name: COPY.nl.header.theme })).toBeVisible();
});

test('in een laag venster (200% zoom) loopt de kop niet mee', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 480 });
  await openPage(page);
  await page.evaluate(() => window.scrollTo(0, 900));
  const box = await page.locator('.topbar').boundingBox();
  expect(box.y + box.height).toBeLessThan(0);
});

test('afgedrukt: geen knoppen, gesprek of video, de methode open', async ({ page }) => {
  await openPage(page);
  await page.emulateMedia({ media: 'print' });
  await page.evaluate(() => window.dispatchEvent(new Event('beforeprint')));
  await expect(page.locator('[data-method]')).toHaveAttribute('open', '');
  for (const selector of ['[data-panel]', '.faqbtn', '.fan__prev', '.fan__next', '[data-login]', '[data-video]']) {
    await expect(page.locator(selector)).toBeHidden();
  }
  await expect(page.locator('.card')).toHaveCount(3);
  for (const id of ['kennispartner', 'stakeholder', 'signalerend']) await expect(page.locator(`.card[data-pigeon="${id}"]`)).toBeVisible();
  await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
  await expect(page.locator('[data-method]')).not.toHaveAttribute('open', '');
});

test('op een telefoon sluit de meelopende tafelstrook zonder spleet op de kop aan', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await openPage(page, { manifest: true });
  await chooseMode(page, 'meeting');
  await page.locator('[data-statement]').last().evaluate((el) => el.scrollIntoView({ block: 'end' }));
  const [bar, strip] = await page.evaluate(() => ['.topbar', '.meeting__table'].map((s) => document.querySelector(s).getBoundingClientRect()));
  // De strook plakt: ze staat boven haar plek in het paneel, direct onder de kop.
  expect(strip.top - bar.bottom).toBeGreaterThanOrEqual(-2);
  expect(strip.top - bar.bottom).toBeLessThanOrEqual(0);
});
