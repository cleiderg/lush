import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { describe, expect, it } from 'vitest';
import {
  budgetForModel,
  checkModelBudget,
  countGlbTriangles,
  DEFAULT_MODEL_BUDGET,
  PROP_BUDGET,
} from './modelBudget';

/** Build a GLB with one mesh of `triangles` triangles (indexed), plus optional padding bytes. */
function makeGlb(triangles: number, padBytes = 0, nodeCount = 1): Uint8Array {
  const json = JSON.stringify({
    asset: { version: '2.0' },
    scene: 0,
    scenes: [{ nodes: Array.from({ length: nodeCount }, (_, i) => i) }],
    nodes: Array.from({ length: nodeCount }, () => ({ mesh: 0 })),
    meshes: [{ primitives: [{ attributes: { POSITION: 0 }, indices: 1 }] }],
    accessors: [
      { componentType: 5126, count: triangles * 3, type: 'VEC3' },
      { componentType: 5125, count: triangles * 3, type: 'SCALAR' },
    ],
  });
  const enc = new TextEncoder().encode(json);
  const jsonPadded = new Uint8Array(Math.ceil(enc.length / 4) * 4).fill(0x20);
  jsonPadded.set(enc);
  const binLen = Math.ceil(padBytes / 4) * 4;
  const total = 12 + 8 + jsonPadded.length + (binLen ? 8 + binLen : 0);
  const out = new Uint8Array(total);
  const v = new DataView(out.buffer);
  v.setUint32(0, 0x46546c67, true);
  v.setUint32(4, 2, true);
  v.setUint32(8, total, true);
  v.setUint32(12, jsonPadded.length, true);
  v.setUint32(16, 0x4e4f534a, true);
  out.set(jsonPadded, 20);
  if (binLen) {
    const o = 20 + jsonPadded.length;
    v.setUint32(o, binLen, true);
    v.setUint32(o + 4, 0x004e4942, true);
  }
  return out;
}

describe('countGlbTriangles', () => {
  it('is exact for a known fixture', () => {
    expect(countGlbTriangles(makeGlb(123))).toBe(123);
    expect(countGlbTriangles(makeGlb(10, 0, 3))).toBe(30);
  });

  it('rejects non-GLB data', () => {
    expect(() => countGlbTriangles(new Uint8Array(32))).toThrow();
  });
});

describe('budgetForModel', () => {
  it('picks the budget from the subfolder', () => {
    expect(budgetForModel('props/tree.glb')).toBe(PROP_BUDGET);
    expect(budgetForModel('misc/x.glb')).toBe(DEFAULT_MODEL_BUDGET);
    expect(budgetForModel('x.glb')).toBe(DEFAULT_MODEL_BUDGET);
  });
});

describe('checkModelBudget', () => {
  it('passes a model under both budgets', () => {
    expect(checkModelBudget('props/ok.glb', makeGlb(500, 1000))).toEqual([]);
  });

  it('fails a model over the triangle budget', () => {
    const problems = checkModelBudget('props/big.glb', makeGlb(PROP_BUDGET.maxTriangles + 1));
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('props/big.glb');
    expect(problems[0]).toContain(String(PROP_BUDGET.maxTriangles + 1));
  });

  it('fails a model over the file-size budget', () => {
    const glb = makeGlb(10, PROP_BUDGET.maxBytes);
    const problems = checkModelBudget('props/heavy.glb', glb);
    expect(problems).toHaveLength(1);
    expect(problems[0]).toContain('props/heavy.glb');
    expect(problems[0]).toContain(String(glb.byteLength));
  });
});

function findGlbs(dir: string, rel = ''): string[] {
  let entries;
  try {
    entries = readdirSync(join(dir, rel), { withFileTypes: true });
  } catch {
    return [];
  }
  return entries.flatMap((e) => {
    const r = rel ? `${rel}/${e.name}` : e.name;
    if (e.isDirectory()) return findGlbs(dir, r);
    return e.name.toLowerCase().endsWith('.glb') ? [r] : [];
  });
}

describe('models under client/public/models', () => {
  const root = join(__dirname, '../../public/models');
  it('are all within budget', () => {
    const problems = findGlbs(root).flatMap((rel) =>
      checkModelBudget(rel, new Uint8Array(readFileSync(join(root, rel)))),
    );
    expect(problems).toEqual([]);
  });
});
