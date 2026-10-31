// @vitest-environment happy-dom
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';
import { beforeEach, describe, expect, it } from 'vitest';
import { addPressFeedback, applyTheme, FONT_FACE_CSS, FONT_FILES, THEME, TITLE_FONT_FAMILY } from './theme';

const root = resolve(__dirname, '../..');

beforeEach(() => {
  document.head.replaceChildren();
  document.documentElement.removeAttribute('style');
});

describe('applyTheme', () => {
  it('defines panel background, gold border, radius, shadow, title font and spacing', () => {
    for (const name of ['--lush-panel-bg', '--lush-gold', '--lush-border', '--lush-radius', '--lush-shadow',
      '--lush-font-title', '--lush-space-2', '--lush-text-md']) {
      expect(THEME[name], name).toBeTruthy();
    }
    expect(THEME['--lush-font-title']).toContain(TITLE_FONT_FAMILY);
  });

  it('sets every property on the document root', () => {
    applyTheme();
    const style = document.documentElement.style;
    for (const [name, value] of Object.entries(THEME)) {
      expect(style.getPropertyValue(name), name).toBe(value);
    }
  });

  it('adds the font faces and shared classes once', () => {
    applyTheme();
    applyTheme();
    const styles = document.head.querySelectorAll('style#lush-theme');
    expect(styles).toHaveLength(1);
    expect(styles[0].textContent).toContain('@font-face');
    expect(styles[0].textContent).toContain('.lush-panel');
  });
});

describe('title font', () => {
  it('@font-face sources are local paths to files in public/ with the OFL licence', () => {
    const urls = [...FONT_FACE_CSS.matchAll(/url\('([^']+)'\)/g)].map((m) => m[1]);
    expect(urls).toHaveLength(FONT_FILES.length);
    for (const u of urls) {
      expect(u).toMatch(/^\/(?!\/)/);
      expect(u).not.toMatch(/^[a-z]+:/i);
      const file = join(root, 'public', u);
      expect(existsSync(file), u).toBe(true);
      expect(statSync(file).size).toBeGreaterThan(10_000);
    }
    const dir = join(root, 'public', urls[0].slice(0, urls[0].lastIndexOf('/')), 'OFL.txt');
    expect(readFileSync(dir, 'utf8')).toContain('SIL Open Font License');
  });

  it('no stylesheet or font is requested from a third-party host', () => {
    const html = readFileSync(join(root, 'index.html'), 'utf8');
    for (const tag of html.match(/<link[^>]*>/g) ?? []) {
      expect(tag, tag).not.toMatch(/href="(https?:)?\/\//);
    }
    expect(html).not.toMatch(/@import|url\(\s*['"]?(https?:)?\/\//);
    const files: string[] = [];
    const walk = (d: string): void => {
      for (const name of readdirSync(d)) {
        const u = join(d, name);
        if (statSync(u).isDirectory()) walk(u);
        else if (/\.(ts|css)$/.test(name) && !name.endsWith('.test.ts')) files.push(readFileSync(u, 'utf8'));
      }
    };
    walk(join(root, 'src'));
    for (const src of files) {
      expect(src).not.toMatch(/fonts\.googleapis|fonts\.gstatic|use\.typekit|@import\s+url\(\s*['"]?https?:/);
      expect(src).not.toMatch(/url\(\s*['"]?https?:\/\//);
    }
  });
});

describe('addPressFeedback', () => {
  it('adds pressed while the pointer is down and removes it on release', () => {
    const b = document.createElement('button');
    addPressFeedback(b);
    b.dispatchEvent(new PointerEvent('pointerdown'));
    expect(b.classList.contains('pressed')).toBe(true);
    b.dispatchEvent(new PointerEvent('pointerup'));
    expect(b.classList.contains('pressed')).toBe(false);
    b.dispatchEvent(new PointerEvent('pointerdown'));
    b.dispatchEvent(new PointerEvent('pointercancel'));
    expect(b.classList.contains('pressed')).toBe(false);
  });
});
