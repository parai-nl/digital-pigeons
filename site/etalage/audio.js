// Eén speler voor alle geluid van de vergadering. Er klinkt nooit iets zonder
// dat de bezoeker eerst klikte, en Stop of de geluidsknop zet het meteen stil.
//
// Safari laat een <audio> alleen spelen als hij binnen een klik gestart is. De
// reactie komt pas een moment na de klik op een uitspraak; prime() start het
// fragment daarom al binnen die klik, zonder geluid, en zet het meteen weer
// stil. Geen blob: of data:-bron, zodat de CSP (media-src 'self') het toestaat.
const player = new Audio();
player.preload = 'auto';
let current = null;
let priming = null;

const same = (src) => player.src === new URL(src, document.baseURI).href;

export function prime(src) {
  if (!src || current) return;
  const token = {};
  priming = token;
  player.muted = true;
  if (!same(src)) player.src = src;
  const settle = () => {
    // Is het fragment intussen echt gestart, dan blijft het gewoon spelen.
    if (priming !== token || current) return;
    priming = null;
    player.pause();
    player.currentTime = 0;
    player.muted = false;
  };
  player.play().then(settle, settle);
}

// Speelt een fragment af. De belofte resolvet als het fragment klaar is, gestopt
// wordt of niet te spelen blijkt: geluid is nooit iets waarop de pagina blijft hangen.
export function play(src, { onState } = {}) {
  stop();
  priming = null;
  const token = {};
  current = token;
  player.muted = false;
  if (same(src)) player.currentTime = 0;
  else player.src = src;
  return new Promise((resolve) => {
    const done = () => {
      player.removeEventListener('ended', done);
      player.removeEventListener('error', done);
      if (current === token) {
        current = null;
        onState?.(false);
      }
      token.done = null;
      resolve();
    };
    token.done = done;
    player.addEventListener('ended', done);
    player.addEventListener('error', done);
    onState?.(true);
    player.play().catch(done);
  });
}

export function stop() {
  if (!current) return;
  player.pause();
  current.done?.();
}

export const isPlaying = () => current !== null;
