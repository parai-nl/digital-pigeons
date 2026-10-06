// DigiPigie: een knipoog, als dank aan iemand die het project veel geholpen
// heeft. Vijf keer snel op het AI-MAPS-logo en de Stakeholder staat groot in
// beeld, met confetti en vuurwerk boven Rotterdam en "DigiPigie" eronder. Een
// klik in de lucht schiet vuurwerk af, een klik op de duif laat de duif
// schudden.
//
// Het is een extra en gedraagt zich ook zo. app.js laadt dit bestand en de stijl
// pas bij de vijfde klik. Het bewaart niets, speelt geen geluid en raakt het
// gesprek, de vergadering en de waaier niet aan. Bij het sluiten stopt alle
// beweging. Wie minder beweging vraagt, krijgt een stil beeld zonder vuurwerk.
import { h, prefersReducedMotion } from './dom.js';
import { createDialog } from './dialog.js';
import { createStage } from './stage.js';
import { icon } from './icons.js';

const PIGEON = 'stakeholder';
// De kleuren van de Stakeholder zelf: het goud van de das, het groen en paars
// van de hals, en het rood van Parai. Wit en lichtblauw voor het vuurwerk.
const COLORS = ['#e5c473', '#f0a92e', '#3fb68b', '#9d6fd6', '#ea5544', '#ffffff', '#8fb8ff'];

// Eén belofte voor stijl en opbouw: wie nog eens vijf keer klikt terwijl die
// laden, krijgt dezelfde dialoog en geen tweede.
let ready = null;

export async function openDigiPigie(copy) {
  ready ||= loadStyle().then(build).catch((err) => {
    ready = null;
    throw err;
  });
  (await ready).open(copy);
}

function loadStyle() {
  return new Promise((resolve) => {
    const link = h('link', { rel: 'stylesheet', href: new URL('./styles/digipigie.css', import.meta.url).href });
    link.addEventListener('load', resolve, { once: true });
    link.addEventListener('error', resolve, { once: true });
    document.head.append(link);
  });
}

function build() {
  const dialog = createDialog({ className: 'dialog--digipigie', labelledBy: 'digipigie-title' });
  const canvas = h('canvas', { class: 'digipigie__sky', 'aria-hidden': 'true' });
  const sky = createSky(canvas);

  // Het podium van de kaart, zonder de plaat: dezelfde duif, dezelfde clips en
  // dezelfde terugval op het portret als het beeld niet laadt.
  const stage = createStage(PIGEON);
  const bird = h('div', { class: 'digipigie__bird', 'aria-hidden': 'true' }, stage.nodes);
  if (stage.mode === 'art') {
    bird.style.setProperty('--lift', String(Math.max(0, -stage.art.layer.top)));
    // Laadt het rustbeeld niet, dan staat het portret binnen het vak en hoeft
    // er boven niets vrij.
    stage.layer.querySelector('.card__rest').addEventListener('error', () => bird.style.removeProperty('--lift'), { once: true });
  }

  const title = h('h2', { class: 'digipigie__title', id: 'digipigie-title' });
  const thanks = h('p', { class: 'digipigie__thanks' });
  const close = h('button', { type: 'button', class: 'digipigie__close', onclick: () => dialog.close() }, icon('close', 20));

  dialog.panel.replaceChildren(
    canvas,
    h('div', { class: 'digipigie__rays', 'aria-hidden': 'true' }),
    h('div', { class: 'digipigie__center' }, bird, title, thanks),
    close,
  );

  // Alles wat één keer openstaan in gang zet, hangt aan dit signaal; sluiten
  // breekt het af en de lopende clip keert terug naar rust.
  let session = null;
  let gestures = null;
  let kickoff = null;
  let shaking = false;

  // Een klik op de duif: schudden en een handvol confetti. Ergens anders in de
  // lucht: vuurwerk op die plek. De sluitknop doet alleen dat.
  dialog.panel.addEventListener('click', (event) => {
    if (close.contains(event.target)) return;
    const rect = canvas.getBoundingClientRect();
    const x = event.clientX - rect.left;
    const y = event.clientY - rect.top;
    if (bird.contains(event.target)) {
      sky.confetti(x, y);
      shake();
    } else sky.burst(x, y);
  });

  async function shake() {
    if (shaking) return;
    const { signal } = session;
    shaking = true;
    await stage.play('ruffle', signal);
    if (!signal.aborted) shaking = false;
  }

  function open(copy) {
    if (dialog.isOpen()) return;
    session = new AbortController();
    const c = copy.digipigie;
    // Elke letter springt apart binnen; een schermlezer hoort één woord.
    title.replaceChildren(
      h('span', { class: 'sr-only' }, c.title),
      h('span', { 'aria-hidden': 'true' }, [...c.title].map((letter, i) => h('span', { class: 'digipigie__letter', style: { '--i': String(i) } }, letter))),
    );
    thanks.textContent = c.thanks;
    close.setAttribute('aria-label', c.close);
    close.title = c.close;
    dialog.open(close);
    sky.start();
    // Om de zoveel tijd een gebaar, afwisselend pikken en schudden (schudden
    // speelt al bij binnenkomst).
    let turn = 0;
    const { signal } = session;
    gestures = setInterval(() => {
      if (!shaking) stage.play(turn++ % 2 ? 'ruffle' : 'peck', signal);
    }, 4200);
    kickoff = setTimeout(shake, 700);
  }

  dialog.onClose(() => {
    clearInterval(gestures);
    clearTimeout(kickoff);
    session.abort();
    // Een clip die nog laadt, luistert niet naar het signaal; pauzeren breekt
    // haar af, zodat het opruimen van het podium meteen loopt.
    if (stage.mode === 'art') stage.video.pause();
    shaking = false;
    sky.stop();
  });

  return { open };
}

// ---- de lucht --------------------------------------------------------------------

// Confetti, vuurwerk en de skyline van Rotterdam op één canvas. Tijd in seconden
// en afstanden in CSS-pixels, zodat alles op elk scherm even snel gaat.
function createSky(canvas) {
  const ctx = canvas.getContext('2d');
  let width = 0;
  let height = 0;
  let frame = 0;
  let last = 0;
  let untilRocket = 0;
  let untilRain = 0;
  let pieces = [];
  let rockets = [];
  let sparks = [];

  function resize() {
    const dpr = Math.min(window.devicePixelRatio || 1, 2);
    width = canvas.clientWidth;
    height = canvas.clientHeight;
    canvas.width = Math.round(width * dpr);
    canvas.height = Math.round(height * dpr);
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  }

  function start() {
    resize();
    window.addEventListener('resize', onResize);
    pieces = [];
    rockets = [];
    sparks = [];
    if (prefersReducedMotion()) {
      // Een stil beeld: confetti die blijft hangen, geen vuurwerk.
      for (let i = 0; i < 90; i++) pieces.push(piece(rand(0, width), rand(0, height), 0, 0));
      draw();
      return;
    }
    cannon(0, height, 1);
    cannon(width, height, -1);
    untilRocket = 0.4;
    untilRain = 0;
    last = performance.now();
    frame = requestAnimationFrame(tick);
  }

  function stop() {
    cancelAnimationFrame(frame);
    frame = 0;
    window.removeEventListener('resize', onResize);
    pieces = [];
    rockets = [];
    sparks = [];
    ctx.clearRect(0, 0, width, height);
  }

  function onResize() {
    resize();
    if (!frame) draw();
  }

  function tick(now) {
    // Na een verborgen tabblad niet in één keer seconden inhalen.
    const dt = Math.min(0.05, (now - last) / 1000);
    last = now;
    untilRocket -= dt;
    if (untilRocket <= 0 && rockets.length < 4) {
      launch(rand(0.12, 0.88) * width, rand(0.1, 0.42) * height);
      untilRocket = rand(0.45, 1.3);
    }
    untilRain -= dt;
    if (untilRain <= 0 && pieces.length < 260) {
      pieces.push(piece(rand(0, width), -12, rand(-30, 30), rand(40, 90)));
      untilRain = 0.05;
    }
    step(dt);
    draw();
    frame = requestAnimationFrame(tick);
  }

  function step(dt) {
    for (const r of rockets) {
      r.x += r.vx * dt;
      r.y += r.vy * dt;
      r.vy += 120 * dt;
      if (r.y <= r.top || r.vy >= 0) {
        r.done = true;
        explode(r.x, r.y);
      }
    }
    rockets = rockets.filter((r) => !r.done);

    const sparkDrag = Math.exp(-1.6 * dt);
    for (const s of sparks) {
      s.vx *= sparkDrag;
      s.vy = s.vy * sparkDrag + 150 * dt;
      s.x += s.vx * dt;
      s.y += s.vy * dt;
      s.life -= dt;
    }
    sparks = sparks.filter((s) => s.life > 0);

    // Confetti remt af naar een valsnelheid van 150 px/s en wiegt heen en weer.
    const drag = Math.exp(-2.8 * dt);
    for (const p of pieces) {
      p.vx *= drag;
      p.vy = p.vy * drag + 150 * (1 - drag);
      p.phase += p.sway * dt;
      p.x += (p.vx + Math.sin(p.phase) * 38) * dt;
      p.y += p.vy * dt;
      p.spin += p.vspin * dt;
    }
    pieces = pieces.filter((p) => p.y < height + 30);
  }

  function draw() {
    ctx.clearRect(0, 0, width, height);
    ctx.globalCompositeOperation = 'lighter';
    ctx.lineCap = 'round';
    for (const r of rockets) {
      ctx.strokeStyle = 'rgb(255 236 200 / .85)';
      ctx.lineWidth = 2.2;
      line(r.x, r.y, r.x - r.vx * 0.06, r.y - r.vy * 0.06);
    }
    for (const s of sparks) {
      ctx.globalAlpha = Math.min(1, s.life / s.span * 1.6);
      ctx.strokeStyle = s.color;
      ctx.lineWidth = 2;
      line(s.x, s.y, s.x - s.vx * 0.045, s.y - s.vy * 0.045);
    }
    ctx.globalAlpha = 1;
    ctx.globalCompositeOperation = 'source-over';
    skyline();
    for (const p of pieces) {
      ctx.save();
      ctx.translate(p.x, p.y);
      ctx.rotate(p.spin);
      ctx.scale(1, Math.cos(p.phase * 1.7));
      ctx.fillStyle = p.color;
      if (p.round) {
        ctx.beginPath();
        ctx.arc(0, 0, p.w / 2, 0, Math.PI * 2);
        ctx.fill();
      } else ctx.fillRect(-p.w / 2, -p.h / 2, p.w, p.h);
      ctx.restore();
    }
  }

  function line(x1, y1, x2, y2) {
    ctx.beginPath();
    ctx.moveTo(x1, y1);
    ctx.lineTo(x2, y2);
    ctx.stroke();
  }

  // Een silhouet van de Maasoever: de Erasmusbrug met haar geknikte pyloon, De
  // Rotterdam en de Euromast. Het vuurwerk gaat erachter op.
  function skyline() {
    const s = Math.min(height * 0.2, width * 0.2, 190);
    const base = height;
    const deck = base - s * 0.18;
    ctx.fillStyle = '#060817';
    ctx.strokeStyle = '#060817';

    // Kade en water.
    ctx.fillRect(0, deck + s * 0.06, width, base - deck);

    // Blokken aan de overkant.
    const blocks = [[0.0, 0.3, 0.05], [0.06, 0.42, 0.04], [0.11, 0.25, 0.06], [0.56, 0.34, 0.04], [0.9, 0.38, 0.05], [0.95, 0.22, 0.05]];
    for (const [x, hgt, w] of blocks) ctx.fillRect(x * width, deck - hgt * s, w * width, hgt * s + s * 0.1);

    // De Rotterdam: drie torens met verspringende bovenkant.
    const dr = 0.62 * width;
    const unit = Math.max(10, s * 0.14);
    ctx.fillRect(dr, deck - s * 0.78, unit, s * 0.8);
    ctx.fillRect(dr + unit * 0.85, deck - s * 0.92, unit, s * 0.94);
    ctx.fillRect(dr + unit * 1.7, deck - s * 0.7, unit, s * 0.72);
    ctx.fillRect(dr + unit * 0.4, deck - s * 0.5, unit * 2.2, s * 0.52);

    // De Euromast: schacht, uitkijkschijf en de spits erboven.
    const em = 0.82 * width;
    ctx.fillRect(em - s * 0.025, deck - s * 0.95, s * 0.05, s * 0.97);
    ctx.fillRect(em - s * 0.09, deck - s * 0.72, s * 0.18, s * 0.05);
    ctx.fillRect(em - s * 0.012, deck - s * 1.35, s * 0.024, s * 0.42);

    // De Erasmusbrug: het dek, de pyloon als omgekeerde Y met een knik, en de
    // tuien naar één kant.
    const left = 0.14 * width;
    const right = 0.52 * width;
    const px = left + (right - left) * 0.38;
    ctx.lineWidth = Math.max(3, s * 0.035);
    line(left, deck, right, deck);
    ctx.lineWidth = Math.max(2.5, s * 0.03);
    const knee = { x: px + s * 0.02, y: deck - s * 0.28 };
    const top = { x: px - s * 0.1, y: deck - s * 1.05 };
    line(px - s * 0.09, deck, knee.x, knee.y);
    line(px + s * 0.11, deck, knee.x, knee.y);
    ctx.beginPath();
    ctx.moveTo(knee.x, knee.y);
    ctx.quadraticCurveTo(knee.x + s * 0.03, knee.y - s * 0.45, top.x, top.y);
    ctx.stroke();
    ctx.lineWidth = 1;
    for (let i = 1; i <= 8; i++) line(top.x + s * 0.01 * i, top.y + s * 0.07 * i, px + (right - px) * (i / 8.4), deck);
    for (let i = 1; i <= 3; i++) line(top.x, top.y + s * 0.05 * i, px - (px - left) * (i / 3.2), deck);
  }

  function piece(x, y, vx, vy) {
    const w = rand(6, 11);
    return {
      x, y, vx, vy, w, h: w * rand(0.4, 0.65),
      round: Math.random() < 0.18,
      color: pick(COLORS),
      phase: rand(0, Math.PI * 2),
      sway: rand(2, 4.5),
      spin: rand(0, Math.PI * 2),
      vspin: rand(-6, 6),
    };
  }

  // Twee kanonnen uit de onderhoeken, schuin naar binnen.
  function cannon(x, y, dir) {
    for (let i = 0; i < 110; i++) {
      const angle = rand(-1.35, -0.55);
      const speed = rand(700, 1300);
      pieces.push(piece(x, y, Math.cos(angle) * speed * dir, Math.sin(angle) * speed));
    }
  }

  function launch(x, top) {
    rockets.push({ x, y: height, top, vx: rand(-40, 40), vy: -Math.sqrt(2 * 120 * (height - top)) - rand(60, 140) });
  }

  function explode(x, y) {
    const color = pick(COLORS);
    const second = pick(COLORS);
    const count = Math.round(rand(55, 85));
    const force = rand(170, 290);
    for (let i = 0; i < count && sparks.length < 900; i++) {
      const angle = (i / count) * Math.PI * 2 + rand(-0.05, 0.05);
      const speed = force * rand(0.55, 1);
      const span = rand(0.9, 1.6);
      sparks.push({ x, y, vx: Math.cos(angle) * speed, vy: Math.sin(angle) * speed, life: span, span, color: Math.random() < 0.7 ? color : second });
    }
  }

  // Een klik in de lucht: een pijl vanaf de kade naar die plek. Een klik op de
  // duif: een handvol confetti. Bij minder beweging gebeurt er niets.
  function burst(x, y) {
    if (!frame || rockets.length >= 12) return;
    launch(x, Math.min(y, height * 0.8));
  }

  function confetti(x, y) {
    if (!frame || pieces.length >= 600) return;
    for (let i = 0; i < 40; i++) {
      const angle = rand(0, Math.PI * 2);
      const speed = rand(200, 600);
      pieces.push(piece(x, y, Math.cos(angle) * speed, Math.sin(angle) * speed - 300));
    }
  }

  return { start, stop, burst, confetti };
}

function rand(min, max) {
  return min + Math.random() * (max - min);
}

function pick(list) {
  return list[Math.floor(Math.random() * list.length)];
}
