// npm run shots: screenshots for checking the look against the references
// (docs/specs/visual-style.md, "Checking against the references").
//
// Starts the Vite dev server, opens the client offline (?offline: fixed-seed local bots, no
// WebSocket) with the ?debug time control in headless Chromium, and saves one PNG per scene and
// viewport to client/.shots/. Exits non-zero if any shot is missing or blank (a single colour).
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
];

const VIEWPORTS = [
  { label: '390x844', width: 390, height: 844, mobile: true },
  { label: '1600x1000', width: 1600, height: 1000, mobile: false },
];

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
  const page = await context.newPage();
  const errors = [];
  page.on('pageerror', (err) => errors.push(String(err)));
  page.on('websocket', (ws) => {
    // Vite's own hot-reload socket goes to the dev server; offline, the game must open none.
    if (!ws.url().startsWith(base.replace(/^http/, 'ws'))) errors.push(`unexpected WebSocket: ${ws.url()}`);
  });
  await page.goto(`${base}/?offline&debug&seed=${SEED}&place=${scene.place}`, { waitUntil: 'load' });
  await page.waitForSelector(`.lush-time-panel button[data-preset="${scene.time}"]`);
  await page.click(`.lush-time-panel button[data-preset="${scene.time}"]`);
  await page.click('.lush-time-panel button[data-action="pause"]');
  await page.waitForLoadState('networkidle');
  await page.waitForTimeout(SETTLE_MS);
  await page.screenshot({ path: fileFor(scene, vp) });
  await context.close();
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
    for (const scene of SCENES) {
      for (const vp of VIEWPORTS) {
        try {
          await shoot(browser, base, scene, vp);
        } catch (err) {
          failures.push(String(err));
        }
      }
    }
    for (const scene of SCENES) {
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
