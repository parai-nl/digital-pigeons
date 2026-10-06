// Playwright is uitsluitend ontwikkelgereedschap: de etalage zelf blijft
// buildless en wordt geserveerd zoals ze uitgeleverd wordt: de map `site/` op
// "/", via `tools/static-server.mjs`. De opnames en het beeldmanifest van de
// andere werkstromen spuiten de tests zelf in (tests/etalage/fixtures).
//
// Die server verving `python3 -m http.server`. Python serveert HTTP/1.0 zonder
// keep-alive en accepteert maar vijf wachtende verbindingen tegelijk; met de
// standaard vier workers liet dat per run zeven tot zeventien tests omvallen met
// net::ERR_SOCKET_NOT_CONNECTED en modules die niet laadden. Serieel slaagde
// alles, dus het lag aan de server, niet aan de pagina.
import { defineConfig, devices } from '@playwright/test';
import { execFileSync } from 'node:child_process';

// Vraag het besturingssysteem om een poort die niemand gebruikt. Dat gebeurt in
// een kindproces omdat deze config synchroon geladen wordt en `net` de poort pas
// in een callback prijsgeeft.
//
// De gekozen poort wordt in de omgeving geparkeerd: elke worker evalueert deze
// config in zijn eigen proces, en zonder dat zou elke worker een *andere* vrije
// poort kiezen en vervolgens de ene server die echt draait niet vinden. Workers
// erven de omgeving van de runner, dus de eerste evaluatie beslist.
// Zelf DUIVEN_E2E_PORT zetten pint de poort vast — handig tegen een server die
// je al draaien hebt.
function pickFreePort() {
  if (process.env.DUIVEN_E2E_PORT) return Number(process.env.DUIVEN_E2E_PORT);
  const script =
    "const s = require('net').createServer();" +
    "s.listen(0, '127.0.0.1', () => { process.stdout.write(String(s.address().port)); s.close(); });";
  const picked = execFileSync(process.execPath, ['-e', script], { encoding: 'utf8' }).trim();
  process.env.DUIVEN_E2E_PORT = picked;
  return Number(picked);
}

const port = pickFreePort();
const baseURL = `http://127.0.0.1:${port}`;

export default defineConfig({
  testDir: './tests/etalage',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: process.env.CI ? [['list'], ['html', { open: 'never' }]] : 'list',
  outputDir: './test-results',

  use: {
    baseURL,
    // De etalage kiest haar taal uit de browser; Nederlands is de standaard
    // van de suite, de taaltests zetten zelf een andere.
    locale: 'nl-NL',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    { name: 'chromium', use: { ...devices['Desktop Chrome'], locale: 'nl-NL' } },
    // Safari-engine voor de echte opnames en het echte beeld (HEVC met alfa):
    // alleen met ETALAGE_WEBKIT=1, want WebKit staat niet op elke machine.
    ...(process.env.ETALAGE_WEBKIT === '1'
      ? [{ name: 'webkit', testMatch: /echt\.spec\.js/, use: { ...devices['Desktop Safari'], locale: 'nl-NL' } }]
      : []),
  ],

  webServer: {
    // De server zet één regel op stdout — de URL waarop hij luistert. Die staat
    // hier al vast, dus de regel is ruis in de testuitvoer en blijft uit tenzij
    // je erom vraagt. Een server die niet opkomt, komt sowieso boven water als
    // een webServer-starttimeout.
    command: `${process.execPath} tools/static-server.mjs ${port} site`,
    // De koppen van de hosting (CSP, Permissions-Policy) gaan mee, zodat elke
    // overtreding in de e2e zichtbaar wordt (tests/etalage/helpers.js).
    env: { ...process.env, DUIVEN_ETALAGE_HEADERS: '1' },
    cwd: import.meta.dirname,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    stdout: process.env.DUIVEN_E2E_SERVER_LOG ? 'pipe' : 'ignore',
    stderr: 'pipe',
    timeout: 30_000,
  },
});
