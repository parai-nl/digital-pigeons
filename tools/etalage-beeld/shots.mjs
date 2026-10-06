// Browsercontrole en beelden van de duivenlaag.
//
//   node tools/etalage-beeld/shots.mjs [--out <map>] [--port 8780]
//
// 1. Alfa in de browser: elke clip speelt in Chromium over een fel gekleurde
//    achtergrond; gemeten wordt dat de hoeken exact die kleur houden, dat de
//    duif er dekkend in staat en welke bron de browser koos (moet .webm zijn).
//    Hetzelfde in WebKit (Playwright, niet Safari zelf) met de HEVC-bron.
// 2. Naad: het rustbeeld (img) tegen het eerste en laatste videobeeld, op
//    dezelfde plek in dezelfde pagina.
// 3. preview.html op licht en donker, in rust en midden in elke clip, plus een
//    korte opname waarin alle clips na elkaar spelen.
// Resultaat: browser-check.json en de beelden in --out.
import { chromium, webkit } from 'playwright';
import { mkdir, writeFile, readdir, rename, rm } from 'node:fs/promises';
import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { serve } from './serve.mjs';

const HERE = path.dirname(fileURLToPath(import.meta.url));
const arg = (name, fallback) => { const i = process.argv.indexOf(name); return i > 0 ? process.argv[i + 1] : fallback; };
const OUT = path.resolve(arg('--out', path.join(HERE, 'out', 'shots')));
const PORT = Number(arg('--port', 8780));
const BASE = `http://127.0.0.1:${PORT}`;
const GPU = ['--use-angle=metal', '--enable-gpu', '--ignore-gpu-blocklist'];
const KEY = [255, 0, 214];   // magenta: komt nergens in duif of plaat voor

await mkdir(OUT, { recursive: true });
const server = await serve(PORT);
const manifest = await (await fetch(`${BASE}/site/etalage/media/duiven/manifest.json`)).json();
const report = { chromium: {}, webkit: {}, seams: {} };

// Een testpagina per clip: gekleurde achtergrond, img en video op dezelfde plek.
function testPage(p, kind) {
  const c = manifest.pigeons[p].clips[kind], r = manifest.pigeons[p].rest;
  const w = 400, h = Math.round(400 * r.height / r.width);
  return `<!doctype html><body style="margin:0;background:rgb(${KEY})">
  <img id="i" src="/site/${r.src}" style="position:absolute;left:0;top:0;width:${w}px;height:${h}px">
  <video id="v" muted playsinline preload="auto" style="position:absolute;left:${w + 20}px;top:0;width:${w}px;height:${h}px">
    <source src="/site/${c.hevc}" type='video/quicktime; codecs="hvc1"'><source src="/site/${c.webm}" type="video/webm"></video></body>`;
}

async function pixels(page, clip) {
  const png = await page.screenshot({ clip });
  const file = path.join(OUT, '.tmp.png');
  await writeFile(file, png);
  const raw = execFileSync('ffmpeg', ['-v', 'error', '-i', file, '-f', 'rawvideo', '-pix_fmt', 'rgb24', '-']);
  return { data: raw, w: clip.width, h: clip.height };
}

function measure(img, vid) {
  const n = img.w * img.h;
  let keyCorner = 0, cornerCount = 0, bird = 0, diff = 0, both = 0;
  const k = 12;
  for (let y = 0; y < img.h; y++) for (let x = 0; x < img.w; x++) {
    const i = (y * img.w + x) * 3;
    const corner = (x < k || x >= img.w - k) && (y < k || y >= img.h - k);
    const isKey = j => Math.abs(vid.data[j] - KEY[0]) + Math.abs(vid.data[j + 1] - KEY[1]) + Math.abs(vid.data[j + 2] - KEY[2]) <= 6;
    if (corner) { cornerCount++; if (isKey(i)) keyCorner++; }
    if (!isKey(i)) bird++;
    const imgKey = Math.abs(img.data[i] - KEY[0]) + Math.abs(img.data[i + 1] - KEY[1]) + Math.abs(img.data[i + 2] - KEY[2]) <= 6;
    if (!imgKey || !isKey(i)) { both++; diff += Math.abs(img.data[i] - vid.data[i]) + Math.abs(img.data[i + 1] - vid.data[i + 1]) + Math.abs(img.data[i + 2] - vid.data[i + 2]); }
  }
  return { cornersKeyColour: +(keyCorner / cornerCount).toFixed(3), nonKeyShare: +(bird / n).toFixed(3), meanDiffVsRest: +(diff / Math.max(1, both) / 3).toFixed(2) };
}

for (const [engine, type, args] of [['chromium', chromium, GPU], ['webkit', webkit, []]]) {
  const browser = await type.launch({ args });
  const page = await browser.newPage({ viewport: { width: 840, height: 600 }, deviceScaleFactor: 1 });
  for (const p of Object.keys(manifest.pigeons)) for (const kind of Object.keys(manifest.pigeons[p].clips)) {
    await page.goto(`${BASE}/tools/etalage-beeld/preview.html`);   // zelfde oorsprong
    await page.setContent(testPage(p, kind), { waitUntil: 'load' });
    const r = manifest.pigeons[p].rest, h = Math.round(400 * r.height / r.width);
    const state = await page.evaluate(() => new Promise(resolve => {
      const v = document.getElementById('v');
      const ready = () => resolve({ src: v.currentSrc.split('/').pop(), readyState: v.readyState, error: v.error && v.error.code, duration: v.duration });
      if (v.readyState >= 2) ready(); else { v.addEventListener('loadeddata', ready, { once: true }); v.addEventListener('error', ready, { once: true }); setTimeout(ready, 10000); }
    }));
    const imgClip = { x: 0, y: 0, width: 400, height: h }, vidClip = { x: 420, y: 0, width: 400, height: h };
    const rest = await pixels(page, imgClip);
    // Eerste beeld (gepauzeerd op 0), dan echt afspelen tot halverwege, dan het einde.
    await page.evaluate(() => new Promise(r => { const v = document.getElementById('v'); v.currentTime = 0; v.addEventListener('seeked', r, { once: true }); setTimeout(r, 1500); }));
    const first = measure(rest, await pixels(page, vidClip));
    const played = await page.evaluate(() => new Promise(resolve => {
      const v = document.getElementById('v'); let frames = 0;
      const count = () => { frames++; if (v.requestVideoFrameCallback) v.requestVideoFrameCallback(count); };
      if (v.requestVideoFrameCallback) v.requestVideoFrameCallback(count);
      v.play().then(() => setTimeout(() => { v.pause(); resolve({ time: v.currentTime, presentedFrames: frames }); }, v.duration * 500)).catch(e => resolve({ error: String(e) }));
    }));
    const midShot = await pixels(page, vidClip);
    const mid = measure(rest, midShot);
    await page.screenshot({ path: path.join(OUT, `alfa-${engine}-${p}-${kind}.png`), clip: { x: 0, y: 0, width: 820, height: h } });
    await page.evaluate(() => new Promise(r => { const v = document.getElementById('v'); v.currentTime = v.duration; v.addEventListener('seeked', r, { once: true }); setTimeout(r, 1500); }));
    const last = measure(rest, await pixels(page, vidClip));
    report[engine][`${p}/${kind}`] = { ...state, played, first, mid, last };
    console.log(engine, p, kind, state.src, JSON.stringify({ first, mid, last }));
  }
  await browser.close();
}
await rm(path.join(OUT, '.tmp.png'), { force: true });

// preview.html: stilstaand en midden in elke clip, licht en donker, 2× pixels.
{
  const browser = await chromium.launch({ args: GPU });
  const context = await browser.newContext({ viewport: { width: 1180, height: 1320 }, deviceScaleFactor: 2, recordVideo: { dir: OUT, size: { width: 1180, height: 1320 } } });
  const page = await context.newPage();
  await page.goto(`${BASE}/tools/etalage-beeld/preview.html`);
  await page.waitForFunction(() => window.etalagePreview && [...document.images].every(i => i.complete));
  await page.waitForTimeout(800);
  await page.screenshot({ path: path.join(OUT, 'preview-rust.png') });
  for (const p of ['kennispartner', 'stakeholder', 'signalerend']) for (const kind of Object.keys(manifest.pigeons[p].clips)) {
    await page.evaluate(({ p, kind }) => {
      for (const s of window.etalagePreview.sections) s.querySelector(`button[data-pigeon="${p}"][data-clip="${kind}"]`).click();
    }, { p, kind });
    const ms = manifest.pigeons[p].clips[kind].ms;
    await page.waitForTimeout(350 + ms * 0.33);
    await page.screenshot({ path: path.join(OUT, `preview-${p}-${kind}.png`) });
    await page.waitForTimeout(ms * 0.67 + 700);
  }
  await page.screenshot({ path: path.join(OUT, 'preview-eind.png') });
  // Kantelsimulatie: de uiterste stand van de muis, voor elke duif vooraan.
  for (const [i, p] of ['signalerend', 'kennispartner', 'stakeholder'].entries()) {
    await page.evaluate(i => { for (const s of window.etalagePreview.sections) s.select(i); }, i);
    await page.waitForTimeout(700);
    await page.evaluate(() => window.etalagePreview.tilt(5, -6));
    await page.waitForTimeout(100);
    await page.screenshot({ path: path.join(OUT, `preview-kantel-${p}.png`) });
    await page.evaluate(() => window.etalagePreview.tilt(0, 0));
  }
  const vid = page.video();
  await context.close();
  await browser.close();
  const src = await vid.path();
  const mp4 = path.join(OUT, 'preview-opname.mp4');
  execFileSync('ffmpeg', ['-v', 'error', '-y', '-i', src, '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-crf', '20', mp4]);
  await rm(src, { force: true });
}

server.close();
await writeFile(path.join(OUT, 'browser-check.json'), JSON.stringify(report, null, 1));
const bad = [];
for (const engine of ['chromium', 'webkit']) for (const [k, r] of Object.entries(report[engine])) {
  const want = engine === 'chromium' ? '.webm' : '.mov';
  if (!r.src?.endsWith(want)) bad.push(`${engine} ${k}: bron ${r.src}, verwacht ${want}`);
  if (r.mid.cornersKeyColour < 0.99) bad.push(`${engine} ${k}: hoeken niet transparant (${r.mid.cornersKeyColour})`);
  if (r.mid.nonKeyShare < 0.1) bad.push(`${engine} ${k}: geen duif in beeld`);
  if (!(r.played.presentedFrames > 5) && engine === 'chromium') bad.push(`${engine} ${k}: speelde niet af`);
  for (const w of ['first', 'last']) if (r[w].meanDiffVsRest > 4) bad.push(`${engine} ${k}: ${w} wijkt ${r[w].meanDiffVsRest} af van rust.webp`);
}
console.log(bad.length ? 'PROBLEMEN\n' + bad.join('\n') : 'browsercontrole in orde', '\n' + OUT);
process.exit(bad.length ? 1 : 0);
