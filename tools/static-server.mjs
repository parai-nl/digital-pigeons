// Statische bestandsserver voor de publieke site — alleen ontwikkel- en
// testgereedschap, geen bouwstap. De pagina blijft buildless en wordt exact
// geserveerd zoals ze in de repo staat: deze server voegt niets toe, injecteert
// geen script en herschrijft geen enkele byte.
//
// Vervangt `python3 -m http.server`. Die serveert HTTP/1.0 zonder keep-alive en
// houdt een luisterwachtrij van vijf verbindingen aan (request_queue_size);
// zodra Playwright met meerdere workers tegelijk verbindt, loopt die wachtrij
// vol en krijgt Chromium ERR_SOCKET_NOT_CONNECTED. Node's http-server houdt
// verbindingen open, accepteert een ruimere wachtrij en verwerkt de aanvragen
// parallel.
//
// Gebruik:
//   node tools/static-server.mjs [poort] [hoofdmap]
//   node tools/static-server.mjs 8770 site      (de etalage, zoals op "/")
//   DUIVEN_E2E_PORT=8769 node tools/static-server.mjs
// Poort 0 (de standaard) laat het besturingssysteem een vrije poort kiezen; de
// gekozen URL wordt één keer op stdout gezet.

import { createServer } from 'node:http';
import { createReadStream } from 'node:fs';
import { stat } from 'node:fs/promises';
import path from 'node:path';

// Alles wat de site zelf laadt, plus de bestandstypen die in print- en
// posterroutes kunnen opduiken. Onbekende extensies gaan als
// application/octet-stream de deur uit: liever een bestand dat de browser niet
// uitvoert dan een verkeerd geraden type.
const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.txt': 'text/plain; charset=utf-8',
  '.md': 'text/markdown; charset=utf-8',
  '.svg': 'image/svg+xml',
  '.webp': 'image/webp',
  '.png': 'image/png',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.gif': 'image/gif',
  '.ico': 'image/x-icon',
  '.woff2': 'font/woff2',
  '.woff': 'font/woff',
  '.ttf': 'font/ttf',
  '.map': 'application/json; charset=utf-8',
  // De vergadermedia die python's server al goed typeerde.
  '.glb': 'model/gltf-binary',
  '.mp3': 'audio/mpeg',
  '.wav': 'audio/wav',
  // De clips, de uitlegvideo en haar ondertitels op de etalage.
  '.webm': 'video/webm',
  '.mov': 'video/quicktime',
  '.mp4': 'video/mp4',
  '.vtt': 'text/vtt; charset=utf-8',
  // De ontworpen posters en kaartjes om te downloaden.
  '.pdf': 'application/pdf',
};

const root = path.resolve(
  process.argv[3] || process.env.DUIVEN_E2E_ROOT || path.join(import.meta.dirname, '..'),
);
const port = Number(process.argv[2] || process.env.DUIVEN_E2E_PORT || 0);

// Met DUIVEN_ETALAGE_HEADERS=1 stuurt de server dezelfde
// beveiligingskoppen mee als de webserver van de etalage op pigeon.parai.nl,
// zodat de e2e elke CSP-overtreding ziet die daar zou gebeuren.
const ETALAGE_HEADERS = process.env.DUIVEN_ETALAGE_HEADERS === '1' ? {
  'Content-Security-Policy': "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; media-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'",
  'Permissions-Policy': 'camera=(), microphone=(), geolocation=()',
} : {};

/**
 * Zet een aanvraag-URL om in een absoluut pad binnen de hoofdmap.
 *
 * Geeft null terug zodra het pad buiten de hoofdmap zou wijzen. Dat vangt zowel
 * letterlijke `../`-segmenten als ge-escapete varianten (`%2e%2e`), omdat er
 * pas ná decoderen én normaliseren wordt vergeleken. Een nulbyte in het pad is
 * eveneens reden tot weigeren.
 */
function resolveInsideRoot(requestUrl) {
  let pathname;
  try {
    ({ pathname } = new URL(requestUrl, 'http://127.0.0.1'));
    pathname = decodeURIComponent(pathname);
  } catch {
    return null; // stukke percent-escapes
  }
  if (pathname.includes('\0')) return null;

  const resolved = path.resolve(root, '.' + path.posix.normalize(pathname));
  if (resolved !== root && !resolved.startsWith(root + path.sep)) return null;
  return { resolved, pathname };
}

function plain(res, status, body) {
  res.writeHead(status, {
    'Content-Type': 'text/plain; charset=utf-8',
    'Content-Length': Buffer.byteLength(body),
    'Cache-Control': 'no-store',
  });
  res.end(body);
}

const server = createServer(async (req, res) => {
  if (req.method !== 'GET' && req.method !== 'HEAD') {
    res.setHeader('Allow', 'GET, HEAD');
    plain(res, 405, 'Method Not Allowed\n');
    return;
  }

  const target = resolveInsideRoot(req.url);
  if (!target) {
    plain(res, 403, 'Forbidden\n');
    return;
  }

  let file = target.resolved;
  let info;
  try {
    info = await stat(file);
    if (info.isDirectory()) {
      // Een map zonder afsluitende slash zou relatieve verwijzingen in de
      // geserveerde HTML één niveau te hoog laten uitkomen; daarom eerst
      // omleiden en pas daarna index.html serveren.
      if (!target.pathname.endsWith('/')) {
        res.writeHead(301, { Location: target.pathname + '/', 'Cache-Control': 'no-store' });
        res.end();
        return;
      }
      file = path.join(file, 'index.html');
      info = await stat(file);
    }
    if (!info.isFile()) throw new Error('geen gewoon bestand');
  } catch {
    plain(res, 404, 'Not Found\n');
    return;
  }

  const headers = {
    'Content-Type': MIME[path.extname(file).toLowerCase()] || 'application/octet-stream',
    'Accept-Ranges': 'bytes',
    // Tests moeten altijd de bestanden zien die nu op schijf staan, nooit een
    // eerdere versie uit de browsercache.
    'Cache-Control': 'no-store',
    ...ETALAGE_HEADERS,
  };
  // Safari speelt video alleen af als de server een deel van het bestand kan
  // leveren (206). Eén bereik volstaat; meerdere bereiken vraagt geen browser.
  let start = 0;
  let end = info.size - 1;
  let status = 200;
  const range = /^bytes=(\d*)-(\d*)$/.exec(req.headers.range || '');
  if (range && (range[1] || range[2])) {
    if (range[1]) {
      start = Number(range[1]);
      end = range[2] ? Math.min(Number(range[2]), info.size - 1) : info.size - 1;
    } else {
      start = Math.max(0, info.size - Number(range[2]));
    }
    if (start > end || start >= info.size) {
      res.writeHead(416, { 'Content-Range': `bytes */${info.size}`, ...ETALAGE_HEADERS });
      res.end();
      return;
    }
    status = 206;
    headers['Content-Range'] = `bytes ${start}-${end}/${info.size}`;
  }
  headers['Content-Length'] = end - start + 1;
  res.writeHead(status, headers);
  if (req.method === 'HEAD') {
    res.end();
    return;
  }

  const stream = createReadStream(file, { start, end });
  // Een afgebroken aanvraag (de browser navigeert weg) mag het proces niet
  // omleggen; de verbinding is dan al weg en er valt niets meer te melden.
  stream.on('error', () => res.destroy());
  res.on('close', () => stream.destroy());
  stream.pipe(res);
});

// Een half verstuurde aanvraag van een afgebroken tab is normaal tijdens een
// testrun en mag de server niet stoppen.
server.on('clientError', (err, socket) => {
  if (!socket.writableEnded) socket.end('HTTP/1.1 400 Bad Request\r\n\r\n');
});

// Ruimer dan Node's standaard vijf seconden: bij veel parallelle workers kan een
// hergebruikte verbinding even stilliggen, en een dichtgevallen socket is precies
// de fout die we hier weghalen.
server.keepAliveTimeout = 60_000;
server.headersTimeout = 65_000;

// 511 is de gebruikelijke bovengrens voor de luisterwachtrij; python's
// http.server neemt er vijf, en daar liep de suite op stuk.
server.listen(port, '127.0.0.1', 511, () => {
  process.stdout.write(`http://127.0.0.1:${server.address().port}\n`);
});
