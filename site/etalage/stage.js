// De duif in beeld: een plaat binnen de kaart en een duivenlaag die buiten de
// kaart mag steken (manifest van de beeldstroom). Laadt dat manifest niet, dan
// staat het portret netjes binnen de kaart.
//
// Dezelfde lagen staan op twee plekken: op de kaart in de waaier en aan de
// vergadertafel. Elke plek heeft haar eigen podium (createStage), zodat een
// reactie speelt waar de bezoeker kijkt.
//
// Een reactie speelt een vooraf gerenderde clip op precies het canvas van het
// rustbeeld: elke clip begint en eindigt op dat rustbeeld, dus het wisselen
// tussen beeld en video is naadloos. Bij minder beweging speelt er geen clip.
import { h, prefersReducedMotion, wait } from './dom.js';
import { FALLBACK } from './pigeons.js';

let manifest = null;

export function setManifest(m) {
  manifest = m;
}

export const hasArt = (id) => Boolean(manifest?.pigeons[id]);

export function avatarFor(id) {
  return manifest?.pigeons[id]?.avatar || FALLBACK[id].head;
}

// Een kopje van de duif. Laadt het beeld uit het manifest niet, dan valt het
// terug op het kopportret in plaats van een leeg rondje.
export function avatar(id, props) {
  const img = h('img', { ...props, src: avatarFor(id), alt: '', decoding: 'async' });
  fallBack(img, FALLBACK[id].head);
  return img;
}

export function setAvatar(img, id) {
  img.src = avatarFor(id);
  fallBack(img, FALLBACK[id].head);
}

function fallBack(img, src) {
  img.onerror = () => {
    img.onerror = null;
    if (!img.src.endsWith(src)) img.src = src;
  };
}

// De ruimte boven de kaart die de duivenlaag nodig heeft, als fractie van de
// kaarthoogte. Waaier en tafel houden die ruimte vrij, zodat een reactie niet
// over de tekst erboven valt.
export function breakout() {
  if (!manifest) return 0;
  return Math.max(0, ...Object.values(manifest.pigeons).map((p) => -p.layer.top));
}

// Hetzelfde opzij: hoe ver de breedste duif links of rechts buiten de kaart
// steekt, als fractie van de kaartbreedte.
export function sideout() {
  if (!manifest) return 0;
  return Math.max(0, ...Object.values(manifest.pigeons).map((p) => Math.max(-p.layer.left, p.layer.left + p.layer.width - 1)));
}

export function cardAspect() {
  return manifest?.aspect || 0.82;
}

// Safari speelt HEVC met alfa; de rest WebM VP9 met alfa. GestureEvent bestaat
// alleen in WebKit, en canPlayType bevestigt dat de decoder er is.
let hevcAlpha;
function prefersHevc() {
  if (hevcAlpha === undefined) {
    const probe = document.createElement('video');
    hevcAlpha = 'GestureEvent' in window && probe.canPlayType('video/mp4; codecs="hvc1"') !== '';
  }
  return hevcAlpha;
}

// Alleen het formaat met alfa voor deze browser; ontbreekt dat, dan geen clip.
// Het andere formaat zou de duif in een ondoorzichtig vlak zetten.
function pickClip(art, gesture) {
  const clip = (gesture && art.clips[gesture]) || (art.defaultClip && art.clips[art.defaultClip]);
  if (!clip) return null;
  const src = prefersHevc() ? clip.hevc : clip.webm;
  return src ? { src, ms: clip.ms } : null;
}

// Eén podium: plaat en duif voor één duif op één plek. `nodes` gaan in een
// element met position: relative en de maat van de kaart. Beelden zijn
// decoratief: het element eromheen draagt de naam.
export function createStage(id) {
  const stage = { id, token: 0, nodes: [], play, stop };
  const art = manifest?.pigeons[id];
  if (art) buildArt(art);
  else buildFallback();
  return stage;

  function buildArt(a) {
    const plateImg = h('img', { class: 'card__plate-img', src: a.plate.src, alt: '', width: a.plate.width, height: a.plate.height, decoding: 'async', draggable: 'false' });
    const plate = h('span', { class: 'card__plate' }, plateImg);
    const rest = h('img', { class: 'card__rest', src: a.rest.src, alt: '', width: a.rest.width, height: a.rest.height, decoding: 'async', draggable: 'false' });
    // Laadt een beeld uit het manifest niet: zonder plaat de studiogloed, zonder
    // duif het portret. Nooit een lege kaart.
    plateImg.addEventListener('error', () => {
      plateImg.remove();
      plate.classList.add('card__plate--studio');
    }, { once: true });
    rest.addEventListener('error', degrade, { once: true });
    // De video is onzichtbaar (visibility, niet display) zodat hij beelden kan
    // tonen; de wissel met het rustbeeld gebeurt met één klasse op de laag.
    const video = h('video', { class: 'card__clip', muted: true, playsinline: true, preload: 'none', 'aria-hidden': 'true', tabindex: '-1' });
    video.muted = true;
    const layer = h('span', {
      class: 'card__pigeon',
      style: {
        left: `${a.layer.left * 100}%`,
        top: `${a.layer.top * 100}%`,
        width: `${a.layer.width * 100}%`,
        height: `${a.layer.height * 100}%`,
      },
    }, rest, video);
    Object.assign(stage, { mode: 'art', nodes: [plate, layer], art: a, video, layer });
  }

  function buildFallback() {
    const fb = FALLBACK[id];
    const img = h('img', { class: 'card__portrait', src: fb.portrait, alt: '', width: 512, height: 512, decoding: 'async', draggable: 'false', style: { 'object-position': fb.focus } });
    const plate = h('span', { class: 'card__plate card__plate--studio' }, img);
    Object.assign(stage, { mode: 'fallback', nodes: [plate], portrait: img });
  }

  // Vervangt de beeldlagen door het portret, op dezelfde plek in de pagina.
  function degrade() {
    if (stage.mode !== 'art') return;
    stage.token++;
    const [first, ...others] = stage.nodes;
    buildFallback();
    first.replaceWith(...stage.nodes);
    others.forEach((node) => node.remove());
  }

  // Speelt één gebaar en keert terug naar het rustbeeld. Resolvet altijd; een
  // clip die niet laadt laat de duif gewoon in rust.
  async function play(gesture, signal) {
    if (prefersReducedMotion()) return;
    const token = ++stage.token;
    if (stage.mode === 'fallback') {
      const img = stage.portrait;
      img.classList.remove('is-reacting');
      void img.offsetWidth;
      img.classList.add('is-reacting');
      try { await wait(1100, signal); } catch { /* afgebroken */ }
      if (stage.token === token) img.classList.remove('is-reacting');
      return;
    }
    const clip = pickClip(stage.art, gesture);
    if (!clip) return;
    const { video, layer } = stage;
    // De wissel tussen rustbeeld en video is atomair: in hetzelfde beeld waarin
    // de video haar eerste frame toont, verdwijnt het rustbeeld, en bij 'ended'
    // komt het rustbeeld in één keer terug. Anders staan twee halfdoorzichtige
    // schaduwen even over elkaar.
    const toRest = () => {
      if (stage.token === token) layer.classList.remove('is-playing');
    };
    const finish = () => {
      if (stage.token !== token) return;
      toRest();
      video.pause();
      video.removeAttribute('src');
      video.load();
    };
    try {
      video.src = clip.src;
      video.currentTime = 0;
      const shown = new Promise((resolve) => {
        const show = () => {
          if (stage.token === token) layer.classList.add('is-playing');
          resolve();
        };
        if ('requestVideoFrameCallback' in video) video.requestVideoFrameCallback(show);
        else video.addEventListener('playing', show, { once: true });
      });
      const ended = new Promise((resolve) => video.addEventListener('ended', () => {
        toRest();
        resolve();
      }, { once: true }));
      await video.play();
      await Promise.race([shown, wait(1500, signal)]);
      await Promise.race([ended, wait(clip.ms + 1500, signal)]);
    } catch {
      /* geen clip of afgebroken: terug naar rust */
    } finally {
      finish();
    }
  }

  function stop() {
    stage.token++;
    if (stage.mode === 'fallback') stage.portrait.classList.remove('is-reacting');
    else {
      stage.layer.classList.remove('is-playing');
      stage.video.pause();
    }
  }
}
