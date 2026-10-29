import { existsSync, mkdtempSync, readdirSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterAll, describe, expect, it } from 'vitest';
import { build } from 'vite';
import config from '../../vite.config';

const root = resolve(fileURLToPath(new URL('../..', import.meta.url)));

function files(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) =>
    e.isDirectory() ? files(join(dir, e.name)) : [join(dir, e.name)],
  );
}

describe('vite config', () => {
  it('emits hidden source maps', () => {
    expect(config.build?.sourcemap).toBe('hidden');
  });

  it('keeps the test settings', () => {
    expect(config.test?.environment).toBe('node');
    expect(config.test?.include).toEqual(['src/**/*.test.ts']);
  });
});

describe('production build', () => {
  const out = mkdtempSync(join(tmpdir(), 'lush-maps-'));
  afterAll(() => rmSync(out, { recursive: true, force: true }));

  it('writes a map for every js file and links none', async () => {
    await build({ root, logLevel: 'silent', build: { outDir: out, emptyOutDir: true } });
    const js = files(out).filter((f) => f.endsWith('.js'));
    expect(js.length).toBeGreaterThan(0);
    for (const f of js) {
      expect(existsSync(`${f}.map`), `${f}.map`).toBe(true);
      expect(readFileSync(f, 'utf8'), f).not.toMatch(/sourceMappingURL/);
    }
  }, 120000);
});
