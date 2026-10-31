import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import { describe, expect, it } from 'vitest';
import { pointInPolygon, segmentDistance2 } from './nature';
import {
  isBlocked, parseAreas, parseCampus, parseCardTables, parseCollision, parseParkNature, parseProps, parseSkills,
} from './mapData';

const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const skills = parseSkills(read('skills.json'));
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const cards = parseCardTables(read('cardtables.json'));
const props = parseProps(read('props.json'));
const nature = parseParkNature(read('parknature.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));
const park = areas.areas.find((a) => a.id === 'aldrich_park')!.p;
const spawn = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const safe = areas.safeRadius / 10;
const pond = skills.pond.p;
const m = (v: number) => v / 10;

/** Skill tree footprint radius in metres (shared/map/skills.mjs, SKILL_TREE_R). */
const SKILL_TREE_R = 0.8;
/** Prop footprint radii in metres, from shared/map/props.mjs (PROP_TYPES). */
const PROP_R: Record<string, number> = {
  bench: 0.9, lamp_post: 0.35, bin: 0.35, bike_rack: 1.1, sign: 0.4, planter: 0.6, picnic_table: 1.1, umbrella: 1.1,
  scooter: 0.6, fence: 1.0, low_wall: 1.0, lantern: 0.3, notice_board: 0.8, crate: 0.5, sack: 0.4,
  food_cart: 1.3, string_lights: 1.5, coffee_cups: 0.15, plates: 0.2, pizza_box: 0.25,
};
/** Nature footprint radius and, for trees, canopy radius in metres at scale 1, from shared/map/parknature.mjs (NATURE_TYPES). */
const NATURE_R: Record<string, { r: number; canopy?: number }> = {
  tree_oak: { r: 0.6, canopy: 2.6 }, tree_pine: { r: 0.5, canopy: 1.9 }, tree_eucalyptus: { r: 0.5, canopy: 2.2 },
  rock_boulder: { r: 0.7 }, rock_flat: { r: 0.9 }, bush: { r: 0.7 }, flower_bed: { r: 1.3 }, picnic_blanket: { r: 1.3 }, bird: { r: 0.2 },
};

const segs = campus.paths.flatMap((w) =>
  Array.from({ length: w.p.length / 2 - 1 }, (_, i) => ({ x0: w.p[2 * i], z0: w.p[2 * i + 1], x1: w.p[2 * i + 2], z1: w.p[2 * i + 3], h: w.w / 2 })));
/** On a path: within half its width of its centre line. */
const onPath = (x: number, z: number) => segs.some((s) => segmentDistance2(x * 10, z * 10, s.x0, s.z0, s.x1, s.z1) < s.h * s.h);
const onPaved = (x: number, z: number) => [...campus.plazas.map((q) => q.p), cards.plaza].some((p) => pointInPolygon(p, x * 10, z * 10));
/** Distance in metres from a point (metres) to the pond outline. */
function pondEdgeDist(x: number, z: number): number {
  let best = Infinity;
  for (let i = 0, j = pond.length - 2; i < pond.length; j = i, i += 2) {
    best = Math.min(best, Math.sqrt(segmentDistance2(x * 10, z * 10, pond[j], pond[j + 1], pond[i], pond[i + 1])) / 10);
  }
  return best;
}
/** Centre (metres) of the collision cell holding v. */
const cellCentre = (v: number, origin: number) => origin + (Math.floor((v - origin) / grid.cellSize) + 0.5) * grid.cellSize;

describe('skills.json contents (AC1)', () => {
  it('lists 12 trees, 6 oaks and 4 eucalyptus with unique ids, eucalyptus on the east side of the park', () => {
    const count = (k: string) => skills.trees.filter((t) => t.kind === k).length;
    expect(count('tree')).toBe(12);
    expect(count('oak')).toBe(6);
    expect(count('eucalyptus')).toBe(4);
    expect(skills.trees).toHaveLength(22);
    expect(new Set(skills.trees.map((t) => t.id)).size).toBe(22);
    for (const t of skills.trees) expect(Number.isInteger(t.x) && Number.isInteger(t.z), t.id).toBe(true);
    let cx = 0;
    for (let i = 0; i < park.length; i += 2) cx += park[i] / 10;
    cx /= park.length / 2;
    for (const t of skills.trees.filter((t) => t.kind === 'eucalyptus')) expect(m(t.x), t.id).toBeGreaterThan(cx + 30);
  });

  it('puts the bank booth near (-12, -96) and the grill near (-32, -96), both on walkable cells inside the safe circle', () => {
    for (const [name, p, want] of [['bank', skills.bank, [-12, -96]], ['grill', skills.grill, [-32, -96]]] as const) {
      expect(Math.hypot(m(p.x) - want[0], m(p.z) - want[1]), name).toBeLessThan(2);
      expect(Math.hypot(m(p.x) - spawn.x, m(p.z) - spawn.z), name).toBeLessThan(safe);
      expect(isBlocked(grid, m(p.x), m(p.z)), name).toBe(false);
    }
  });

  it('keeps props off the bank booth and grill', () => {
    for (const p of [skills.bank, skills.grill]) {
      for (const [t, x, z] of props.props) {
        expect(Math.hypot(m(x) - m(p.x), m(z) - m(p.z)), `${props.types[t]} at (${m(x)}, ${m(z)})`).toBeGreaterThanOrEqual(PROP_R[props.types[t]] + 1);
      }
    }
  });

  it('has a pond outline near (-90, -20) in the park, outside the safe circle', () => {
    expect(pond.length % 2).toBe(0);
    expect(pond.length).toBeGreaterThanOrEqual(12);
    let cx = 0, cz = 0;
    for (let i = 0; i < pond.length; i += 2) { cx += m(pond[i]); cz += m(pond[i + 1]); }
    cx /= pond.length / 2; cz /= pond.length / 2;
    console.log(`pond centre (${cx.toFixed(1)}, ${cz.toFixed(1)})`);
    expect(Math.hypot(cx + 90, cz + 20)).toBeLessThan(30);
    expect(pointInPolygon(pond, cx * 10, cz * 10)).toBe(true);
    for (let i = 0; i < pond.length; i += 2) {
      expect(pointInPolygon(park, pond[i], pond[i + 1])).toBe(true);
      expect(Math.hypot(m(pond[i]) - spawn.x, m(pond[i + 1]) - spawn.z)).toBeGreaterThan(safe);
    }
  });

  it('lists 2 active net and 2 active rod spots, and at least that many candidates of each kind with unique ids', () => {
    expect(skills.pond.active).toEqual({ net: 2, rod: 2 });
    const spots = skills.pond.spots;
    for (const k of ['net', 'rod'] as const) {
      expect(spots.filter((s) => s.kind === k).length, k).toBeGreaterThan(skills.pond.active[k]);
    }
    expect(spots.every((s) => s.kind === 'net' || s.kind === 'rod')).toBe(true);
    expect(new Set(spots.map((s) => s.id)).size).toBe(spots.length);
    expect(new Set([...spots.map((s) => s.id), ...skills.trees.map((t) => t.id)]).size).toBe(spots.length + skills.trees.length);
  });
});

describe('skill object placement (AC2)', () => {
  it('puts every tree on a walkable grass cell centre in the park, outside the safe circle, off paths and the pond', () => {
    for (const t of skills.trees) {
      const x = m(t.x), z = m(t.z);
      expect(x, t.id).toBeCloseTo(cellCentre(x, grid.originX), 5);
      expect(z, t.id).toBeCloseTo(cellCentre(z, grid.originZ), 5);
      expect(pointInPolygon(park, t.x, t.z), t.id).toBe(true);
      expect(isBlocked(grid, x, z), t.id).toBe(false);
      expect(Math.hypot(x - spawn.x, z - spawn.z), t.id).toBeGreaterThan(safe);
      expect(onPath(x, z), t.id).toBe(false);
      expect(onPaved(x, z), t.id).toBe(false);
      expect(pointInPolygon(pond, t.x, t.z), t.id).toBe(false);
    }
  });

  it("doesn't overlap a decorative tree, nature item or prop", () => {
    for (const t of skills.trees) {
      const x = m(t.x), z = m(t.z);
      for (const [ty, nx, nz, , scale] of nature.items) {
        const n = NATURE_R[nature.types[ty]];
        const need = SKILL_TREE_R + (n.canopy ?? n.r) * (scale / 100);
        expect(Math.hypot(x - m(nx), z - m(nz)), `${t.id} vs ${nature.types[ty]} at (${m(nx)}, ${m(nz)})`).toBeGreaterThanOrEqual(need);
      }
      for (const [ty, px, pz] of props.props) {
        const need = SKILL_TREE_R + PROP_R[props.types[ty]];
        expect(Math.hypot(x - m(px), z - m(pz)), `${t.id} vs ${props.types[ty]} at (${m(px)}, ${m(pz)})`).toBeGreaterThanOrEqual(need);
      }
    }
  });

  it('keeps decorative nature out of the pond', () => {
    for (const [ty, x, z] of nature.items) expect(pointInPolygon(pond, x, z), `${nature.types[ty]} at (${m(x)}, ${m(z)})`).toBe(false);
  });

  it('puts every fishing spot candidate on the pond edge, next to a walkable cell outside the pond', () => {
    for (const s of skills.pond.spots) {
      const x = m(s.x), z = m(s.z);
      expect(pondEdgeDist(x, z), s.id).toBeLessThan(0.1);
      const shore = [[-1, -1], [0, -1], [1, -1], [-1, 0], [1, 0], [-1, 1], [0, 1], [1, 1]].some(([dx, dz]) => {
        const cx = cellCentre(x + dx, grid.originX), cz = cellCentre(z + dz, grid.originZ);
        return !isBlocked(grid, cx, cz) && !pointInPolygon(pond, cx * 10, cz * 10) && !onPath(cx, cz);
      });
      expect(shore, s.id).toBe(true);
    }
  });
});

describe('skills.json build (AC1, AC3)', () => {
  it('is written by build.mjs byte-identically, and leaves collision.bin, areas.json and cardtables.json unchanged', () => {
    const out = mkdtempSync(join(tmpdir(), 'skills-'));
    execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    for (const f of ['skills.json', 'collision.bin', 'areas.json', 'cardtables.json']) {
      expect(readFileSync(join(out, f)).equals(readFileSync(join(mapDir, f))), f).toBe(true);
    }
  }, 60_000);

  it('uses the seeded generator, not Math.random', () => {
    const src = read('build.mjs').replace(/\/\/.*$/gm, '');
    expect(src).not.toMatch(/Math\.random/);
    expect(src).toMatch(/rng\(SKILLS_SEED\)/);
  });
});
