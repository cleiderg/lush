import { ESLint } from 'eslint';
import { describe, expect, it } from 'vitest';

async function lint(code: string, filePath: string) {
  const eslint = new ESLint({ cwd: new URL('../../', import.meta.url).pathname });
  const [res] = await eslint.lintText(code, { filePath });
  return res.messages.filter((m) => m.ruleId === 'no-restricted-imports');
}

describe('state import boundary', () => {
  it.each([
    "import * as THREE from 'three';",
    "import { Vector3 } from 'three/examples/jsm/Addons.js';",
    "import { x } from '../render/scene';",
    "import { x } from '../render';",
  ])('rejects %s in src/state', async (code) => {
    expect((await lint(code, 'src/state/fixture.ts')).length).toBe(1);
  });

  it('allows normal imports in src/state', async () => {
    expect(await lint("import { x } from '../core/loop';", 'src/state/fixture.ts')).toEqual([]);
  });

  it('allows three in src/render', async () => {
    expect(await lint("import * as THREE from 'three';", 'src/render/fixture.ts')).toEqual([]);
  });
});
