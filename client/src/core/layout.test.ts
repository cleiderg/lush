import { readFileSync, existsSync } from 'node:fs';
import { describe, expect, it } from 'vitest';

const root = new URL('../../', import.meta.url);

describe('scaffold', () => {
  it('has the src folders owned by the client role (protocol and render are created by their own owners)', () => {
    for (const d of ['core', 'net', 'state', 'input', 'ui', 'audio']) {
      expect(existsSync(new URL(`src/${d}/`, root)), d).toBe(true);
    }
  });

  it('has a portrait layout with a canvas and an empty control tray', () => {
    const html = readFileSync(new URL('index.html', root), 'utf8');
    expect(html).toMatch(/<canvas id="game-canvas"/);
    expect(html).toMatch(/<div id="control-tray"><\/div>/);
    expect(html).toMatch(/flex-direction: column/);
  });

  it('pins three to an exact version', () => {
    const pkg = JSON.parse(readFileSync(new URL('package.json', root), 'utf8'));
    expect(pkg.dependencies.three).toMatch(/^\d+\.\d+\.\d+$/);
  });
});
