import { readFileSync, existsSync, mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, describe, expect, it } from 'vitest';
import { build } from 'vite';

const root = resolve(fileURLToPath(new URL('../..', import.meta.url)));
const pub = join(root, 'public');

interface Icon { src: string; sizes: string; type: string }
const manifest = JSON.parse(readFileSync(join(pub, 'manifest.webmanifest'), 'utf8')) as {
  name: string; short_name: string; start_url: string; display: string;
  orientation: string; theme_color: string; background_color: string; icons: Icon[];
};
const html = readFileSync(join(root, 'index.html'), 'utf8');

function pngSize(path: string): string {
  const b = readFileSync(path);
  expect(b.subarray(1, 4).toString()).toBe('PNG');
  return `${b.readUInt32BE(16)}x${b.readUInt32BE(20)}`;
}

function tag(src: string, re: RegExp): string | undefined {
  return src.match(re)?.[0];
}
const attr = (t: string | undefined, a: string) => t?.match(new RegExp(`${a}="([^"]*)"`))?.[1];

describe('manifest', () => {
  it('has required fields', () => {
    expect(manifest.name).toBe('Lush');
    expect(manifest.short_name).toBe('Lush');
    expect(manifest.start_url).toBeTruthy();
    expect(manifest.display).toBe('standalone');
    expect(manifest.orientation).toBe('portrait');
    expect(manifest.theme_color).toMatch(/^#/);
    expect(manifest.background_color).toMatch(/^#/);
  });

  it('lists 192 and 512 PNG icons whose headers match', () => {
    expect(manifest.icons.map((i) => i.sizes).sort()).toEqual(['192x192', '512x512']);
    for (const i of manifest.icons) {
      expect(i.type).toBe('image/png');
      const f = join(pub, i.src);
      expect(existsSync(f)).toBe(true);
      expect(pngSize(f)).toBe(i.sizes);
    }
  });
});

describe('index.html', () => {
  it('has PWA and home-screen tags', () => {
    expect(attr(tag(html, /<link[^>]*rel="manifest"[^>]*>/), 'href')).toBe('/manifest.webmanifest');
    const touch = attr(tag(html, /<link[^>]*rel="apple-touch-icon"[^>]*>/), 'href');
    expect(touch).toBeTruthy();
    expect(pngSize(join(pub, touch!))).toBe('180x180');
    expect(attr(tag(html, /<meta[^>]*name="apple-mobile-web-app-capable"[^>]*>/), 'content')).toBe('yes');
    expect(attr(tag(html, /<meta[^>]*name="mobile-web-app-capable"[^>]*>/), 'content')).toBe('yes');
    expect(attr(tag(html, /<meta[^>]*name="viewport"[^>]*>/), 'content')).toContain('viewport-fit=cover');
    expect(attr(tag(html, /<meta[^>]*name="theme-color"[^>]*>/), 'content')).toBe(manifest.theme_color);
  });
});

describe('vite build', () => {
  const out = mkdtempSync(join(tmpdir(), 'lush-pwa-'));
  afterAll(() => rmSync(out, { recursive: true, force: true }));

  it('emits manifest, icons and linked html', async () => {
    await build({ root, logLevel: 'silent', build: { outDir: out, emptyOutDir: true } });
    expect(existsSync(join(out, 'manifest.webmanifest'))).toBe(true);
    for (const i of manifest.icons) expect(existsSync(join(out, i.src))).toBe(true);
    expect(existsSync(join(out, 'apple-touch-icon.png'))).toBe(true);
    expect(readFileSync(join(out, 'index.html'), 'utf8')).toContain('href="/manifest.webmanifest"');
  }, 120000);
});
