// npm run shots: screenshots for checking the look against the references
// (docs/specs/visual-style.md, "Checking against the references").
//
// Starts the Vite dev server, opens the client offline (?offline: fixed-seed local bots, no
// WebSocket) with the ?debug time control in headless Chromium, and saves one PNG per scene and
// viewport to client/.shots/. Each UI panel is also shot open (panel-<name>-<viewport>.png), filled
// with the offline panel data (?offline&panel=<name>, src/net/offlinePanels.ts). Exits non-zero if
// any shot is missing or blank (a single colour), or the page logs an error.
//
// Uses Playwright's Chromium (`npx playwright install chromium`); where that can't run (older
// macOS), it falls back to the installed Google Chrome. LUSH_SHOTS_CHANNEL=chrome forces that.

import { mkdirSync, readFileSync, rmSync, existsSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { chromium } from 'playwright';
import { createServer } from 'vite';

const root = join(dirname(fileURLToPath(import.meta.url)), '..');
const outDir = join(root, '.shots');
const SEED = 1;

const SCENES = [
  { name: 'spawn-noon', place: 'spawn', time: 'noon' },
  { name: 'spawn-night', place: 'spawn', time: 'night' },
  { name: 'park-dusk', place: 'park', time: 'dusk' },
  // One per UI panel, at noon, wherever offline mode puts that panel (src/net/offline.ts PANEL_PLACES).
  ...['inventory', 'shop', 'bank', 'trade', 'card-table', 'chat-log', 'big-map'].map((panel) => ({ name: `panel-${panel}`, panel, time: 'noon' })),
];

// LUSH_SHOTS_ONLY=<text> shoots only the scenes whose name contains it (for working on one shot).
const ONLY = process.env.LUSH_SHOTS_ONLY ?? '';
const SHOTS = SCENES.filter((s) => s.name.includes(ONLY));

const VIEWPORTS = [
  { label: '390x844', width: 390, height: 844, mobile: true },
  { label: '1600x1000', width: 1600, height: 1000, mobile: false },
];

/** Software WebGL can be slow to hand over a frame late in a long run, so screenshots get longer than Playwright's 30 s. */
const SHOT_TIMEOUT_MS = 60000;

/** Time for models, textures and the sky to settle after the preset is set. */
const SETTLE_MS = 2500;

// Software WebGL, so the shots also work on machines (and CI runners) without a GPU.
const ARGS = ['--use-angle=swiftshader', '--enable-unsafe-swiftshader', '--ignore-gpu-blocklist'];

async function launch() {
  const channel = process.env.LUSH_SHOTS_CHANNEL;
  if (channel) return chromium.launch({ channel, args: ARGS });
  try {
    return await chromium.launch({ args: ARGS });
  } catch (err) {
    console.warn(`Playwright Chromium unavailable (${String(err).split('\n')[0]}); trying installed Chrome`);
    return chromium.launch({ channel: 'chrome', args: ARGS });
  }
}

const fileFor = (scene, vp) => join(outDir, `${scene.name}-${vp.label}.png`);

async function shoot(browser, base, scene, vp) {
  const context = await browser.newContext({
    viewport: { width: vp.width, height: vp.height },
    deviceScaleFactor: 1,
    isMobile: vp.mobile,
    hasTouch: vp.mobile,
  });
  const errors = [];
  try {
    const page = await context.newPage();
    page.on('pageerror', (err) => errors.push(String(err)));
    page.on('response', (res) => {
      if (res.status() >= 400) errors.push(`HTTP ${res.status()}: ${res.url()}`);
    });
    page.on('console', (msg) => {
      if (msg.type() === 'error') errors.push(`console error: ${msg.text()}`);
    });
    page.on('websocket', (ws) => {
      // Vite's own hot-reload socket goes to the dev server; offline, the game must open none.
      if (!ws.url().startsWith(base.replace(/^http/, 'ws'))) errors.push(`unexpected WebSocket: ${ws.url()}`);
    });
    const where = scene.panel ? `panel=${scene.panel}` : `place=${scene.place}`;
    await page.goto(`${base}/?offline&debug&seed=${SEED}&${where}`, { waitUntil: 'load' });
    // The client marks the page once the panel's data is in and the panel is open.
    if (scene.panel) await page.waitForSelector(`body[data-offline-panel="${scene.panel}"]`, { state: 'attached', timeout: 15000 });
    await page.waitForSelector(`.lush-time-panel button[data-preset="${scene.time}"]`);
    await page.click(`.lush-time-panel button[data-preset="${scene.time}"]`);
    await page.click('.lush-time-panel button[data-action="pause"]');
    await page.waitForLoadState('networkidle');
    await page.waitForTimeout(SETTLE_MS);
    await page.screenshot({ path: fileFor(scene, vp), timeout: SHOT_TIMEOUT_MS });
  } finally {
    // Also after a failure, so a stuck page doesn't keep rendering under the next shots.
    await context.close();
  }
  if (errors.length) throw new Error(`${scene.name} ${vp.label}: ${errors.join('; ')}`);
}

/** Number of distinct colours in a PNG (stops counting at 2), decoded by the browser. */
async function distinctColours(browser, png) {
  const page = await browser.newPage();
  try {
    return await page.evaluate(async (b64) => {
      const img = new Image();
      img.src = `data:image/png;base64,${b64}`;
      await img.decode();
      const c = document.createElement('canvas');
      c.width = img.naturalWidth;
      c.height = img.naturalHeight;
      const ctx = c.getContext('2d');
      ctx.drawImage(img, 0, 0);
      const d = ctx.getImageData(0, 0, c.width, c.height).data;
      for (let i = 4; i < d.length; i += 4) {
        if (d[i] !== d[0] || d[i + 1] !== d[1] || d[i + 2] !== d[2] || d[i + 3] !== d[3]) return 2;
      }
      return d.length > 0 ? 1 : 0;
    }, png.toString('base64'));
  } finally {
    await page.close();
  }
}

async function main() {
  rmSync(outDir, { recursive: true, force: true });
  mkdirSync(outDir, { recursive: true });
  const server = await createServer({ root, logLevel: 'warn', server: { port: 0, host: '127.0.0.1' } });
  await server.listen();
  const base = server.resolvedUrls.local[0].replace(/\/$/, '');
  const browser = await launch();
  const failures = [];
  try {
    for (const scene of SHOTS) {
      for (const vp of VIEWPORTS) {
        try {
          // A timeout (slow software rendering) gets one more try; page errors fail at once.
          await shoot(browser, base, scene, vp).catch((err) => {
            if (err?.name !== 'TimeoutError') throw err;
            console.warn(`${scene.name} ${vp.label}: ${String(err).split('\n')[0]}; retrying`);
            return shoot(browser, base, scene, vp);
          });
        } catch (err) {
          failures.push(`${scene.name} ${vp.label}: ${String(err).split('\n')[0]}`);
        }
      }
    }
    for (const scene of SHOTS) {
      for (const vp of VIEWPORTS) {
        const file = fileFor(scene, vp);
        if (!existsSync(file)) {
          failures.push(`missing: ${file}`);
          continue;
        }
        if ((await distinctColours(browser, readFileSync(file))) < 2) failures.push(`blank (one colour): ${file}`);
        else console.log(`ok ${file}`);
      }
    }
  } finally {
    await browser.close();
    await server.close();
  }
  if (failures.length) {
    for (const f of failures) console.error(`shots: ${f}`);
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
