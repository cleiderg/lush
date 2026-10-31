import { execFileSync } from 'node:child_process';
import { mkdtempSync, readFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join, resolve } from 'node:path';
import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { MeshBuilder } from './campusMap';
import { LampLights, MAX_POINT_LIGHTS, GLASS_GLOW, lampSources } from './lights';
import { PROP_BUDGET } from './modelBudget';
import { segmentDistance2 } from './nature';
import {
  LAMP_HEADS, PROP_KIT, Props, STRING_BULBS, STRING_SPAN, TABLETOP_Y, buildProp, glassGeometry, glowsAtNight, loadProps, propGeometry,
} from './propKit';
import { isBlocked, parseAreas, parseCampus, parseCardTables, parseCollision, parseProps, type Prop, type PropsData } from './mapData';
import { WorldScene, countDrawCalls } from './scene';

// Food-and-fun props (#428): food carts, string lights, coffee cups, plates and pizza boxes.
const mapDir = resolve(__dirname, '../../../shared/map');
const read = (f: string) => readFileSync(join(mapDir, f), 'utf8');
const props = parseProps(read('props.json'));
const areas = parseAreas(read('areas.json'));
const campus = parseCampus(read('campus.json'));
const cards = parseCardTables(read('cardtables.json'));
const grid = parseCollision(new Uint8Array(readFileSync(join(mapDir, 'collision.bin'))));

const FOOD = ['food_cart', 'string_lights', 'coffee_cups', 'plates', 'pizza_box'];
const TOPS = ['coffee_cups', 'plates', 'pizza_box'];
/** Footprint radii in metres, from shared/map/props.mjs (PROP_TYPES). */
const R: Record<string, number> = { food_cart: 1.3, string_lights: 0.3, coffee_cups: 0.15, plates: 0.2, pizza_box: 0.25 };

const all = props.props.map(([t, x, z, rot]) => ({ type: props.types[t], x: x / 10, z: z / 10, rot }));
const food = all.filter((p) => FOOD.includes(p.type));
const ofType = (t: string) => all.filter((p) => p.type === t);

/** A point in a prop's own frame in world metres; rotation is clockwise seen from above, as Props draws it. */
function toWorld(p: { x: number; z: number; rot: number }, lx: number, lz: number) {
  const a = (-p.rot * Math.PI) / 180;
  return { x: p.x + lx * Math.cos(a) + lz * Math.sin(a), z: p.z - lx * Math.sin(a) + lz * Math.cos(a) };
}
/** A world point in a prop's own frame. */
function toLocal(p: { x: number; z: number; rot: number }, x: number, z: number) {
  const a = (-p.rot * Math.PI) / 180, dx = x - p.x, dz = z - p.z;
  return { x: dx * Math.cos(a) - dz * Math.sin(a), z: dx * Math.sin(a) + dz * Math.cos(a) };
}
/** Where a prop stands on the ground: a string light on its two poles, the rest on their centre. */
const feet = (p: (typeof all)[number]) =>
  p.type === 'string_lights' ? [toWorld(p, -STRING_SPAN / 2, 0), toWorld(p, STRING_SPAN / 2, 0)] : [{ x: p.x, z: p.z }];

const plazaCentre = (() => {
  const n = cards.plaza.length / 2;
  let x = 0, z = 0;
  for (let i = 0; i < n; i++) { x += cards.plaza[2 * i] / 10; z += cards.plaza[2 * i + 1] / 10; }
  return { x: x / n, z: z / n };
})();
const spawn = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const shop = { x: areas.shop.x / 10, z: areas.shop.z / 10 };
/** The 30 x 30 m busy block (3 x 3 cells of 10 m) around a centre. */
const inBusy = (c: { x: number; z: number }, x: number, z: number) => Math.abs(x - c.x) <= 15 && Math.abs(z - c.z) <= 15;

describe('food props build (AC1)', () => {
  it('is written by build.mjs byte-identically on two runs, matching the committed files, with collision.bin unchanged', () => {
    const a = mkdtempSync(join(tmpdir(), 'food-a-'));
    const b = mkdtempSync(join(tmpdir(), 'food-b-'));
    for (const out of [a, b]) execFileSync('node', [join(mapDir, 'build.mjs'), out], { encoding: 'utf8' });
    for (const f of ['props.json', 'parknature.json', 'collision.bin']) {
      expect(readFileSync(join(a, f)).equals(readFileSync(join(b, f))), f).toBe(true);
      expect(readFileSync(join(a, f)).equals(readFileSync(join(mapDir, f))), f).toBe(true);
    }
  }, 60_000);

  it('uses its own seeded generator, not Math.random', () => {
    const src = read('build.mjs').replace(/\/\/.*$/gm, '');
    expect(src).not.toMatch(/Math\.random/);
    expect(src).toMatch(/rng\(FOOD_SEED\)/);
  });

  it('places 2+ food carts and string lights over both the shop and the card table plaza', () => {
    const counts = Object.fromEntries(FOOD.map((t) => [t, ofType(t).length]));
    console.log('food props:', counts);
    expect(ofType('food_cart').length).toBeGreaterThanOrEqual(2);
    const lights = ofType('string_lights');
    for (const [name, c] of [['shop', shop], ['card plaza', plazaCentre]] as const) {
      expect(lights.some((p) => inBusy(c, p.x, p.z)), `string lights over the ${name}`).toBe(true);
    }
  });

  it('puts cups, plates and pizza boxes on picnic tables or cart serving ledges', () => {
    const hosts = all.filter((p) => p.type === 'picnic_table' || p.type === 'food_cart');
    for (const t of TOPS) expect(ofType(t).length, t).toBeGreaterThan(0);
    for (const item of all.filter((p) => TOPS.includes(p.type))) {
      const on = hosts.some((h) => {
        const l = toLocal(h, item.x, item.z);
        // Picnic table top: x ±0.9, z ±0.4. Cart ledge: x ±0.85, z 0.5 to 0.95. Keep the item's footprint on it.
        const r = R[item.type] * 0.6;
        return h.type === 'picnic_table'
          ? Math.abs(l.x) <= 0.9 - r && Math.abs(l.z) <= 0.4 - r + 0.05
          : Math.abs(l.x) <= 0.85 - r && l.z >= 0.5 && l.z <= 0.95;
      });
      expect(on, `${item.type} at (${item.x}, ${item.z})`).toBe(true);
    }
  });

  it('builds table-top items standing on the table top and cart ledge height', () => {
    const box = (t: string) => {
      const g = propGeometry(t);
      g.computeBoundingBox();
      return g.boundingBox!;
    };
    expect(box('picnic_table').max.y).toBeCloseTo(TABLETOP_Y);
    for (const t of TOPS) expect(box(t).min.y, t).toBeCloseTo(TABLETOP_Y);
    // The cart has a flat ledge at TABLETOP_Y out in front of its body (z > 0.5).
    const p = propGeometry('food_cart').getAttribute('position');
    let ledge = 0;
    for (let i = 0; i < p.count; i++) if (Math.abs(p.getY(i) - TABLETOP_Y) < 1e-6 && p.getZ(i) > 0.9) ledge++;
    expect(ledge).toBeGreaterThan(0);
  });
});

describe('food prop placement (AC2)', () => {
  const walls: number[][] = campus.buildings.flatMap((b) =>
    Array.from({ length: b.p.length / 2 }, (_, i) => {
      const j = (i + 1) % (b.p.length / 2);
      return [b.p[2 * i], b.p[2 * i + 1], b.p[2 * j], b.p[2 * j + 1]];
    }));
  const wallDist = (x: number, z: number) =>
    Math.sqrt(Math.min(...walls.map((w) => segmentDistance2(x * 10, z * 10, w[0], w[1], w[2], w[3])))) / 10;
  /** Places to keep 3 m clear of: spawn and shop from areas.json, bank booth and grill (M9) from docs/specs/map.md. */
  const CLEAR = { spawn, shop, 'bank booth': { x: -12, z: -96 }, grill: { x: -32, z: -96 } };
  /** The shop stall's box relative to the shop point (props.mjs, SHOP_CLEAR). */
  const STALL = { x0: -3, z0: -2, x1: 3, z1: 4 };

  it('places food props', () => {
    expect(food.length).toBeGreaterThan(0);
  });

  it('never puts a food prop on a blocked cell or on a door cell (every door sits on a building wall)', () => {
    for (const p of food) {
      for (const f of feet(p)) {
        const at = `${p.type} at (${f.x.toFixed(1)}, ${f.z.toFixed(1)})`;
        expect(isBlocked(grid, f.x, f.z), at).toBe(false);
        // The 1 m cell holding a door touches its wall; a footprint 1.5 m+ from every wall can't be on that cell.
        expect(wallDist(f.x, f.z) - R[p.type], at).toBeGreaterThanOrEqual(1.5);
      }
    }
  });

  it('keeps 3 m clear of the spawn point, the shop stall and the M9 bank booth and grill spots', () => {
    for (const p of food) {
      for (const f of feet(p)) {
        const at = `${p.type} at (${f.x.toFixed(1)}, ${f.z.toFixed(1)})`;
        for (const [name, c] of Object.entries(CLEAR)) {
          expect(Math.hypot(f.x - c.x, f.z - c.z) - R[p.type], `${at} near the ${name}`).toBeGreaterThanOrEqual(3);
        }
        const sx = f.x - shop.x, sz = f.z - shop.z, m = 3 + R[p.type];
        const inStall = sx > STALL.x0 - m && sx < STALL.x1 + m && sz > STALL.z0 - m && sz < STALL.z1 + m;
        expect(inStall, `${at} within 3 m of the shop stall`).toBe(false);
      }
    }
  });
});

describe('food prop models and draw calls (AC3)', () => {
  it('keeps each new prop type within PROP_BUDGET triangles', () => {
    for (const t of FOOD) {
      expect(PROP_KIT).toContain(t);
      expect(props.types).toContain(t);
      const b = new MeshBuilder();
      buildProp(b, t);
      console.log(`prop ${t}: ${b.triangles} triangles`);
      expect(b.triangles, t).toBeGreaterThan(0);
      expect(b.triangles, t).toBeLessThanOrEqual(PROP_BUDGET.maxTriangles);
    }
  });

  it('costs at most one draw call per new type, and doubling the placements changes nothing', () => {
    const foodIds = new Set(FOOD.map((t) => props.types.indexOf(t)));
    const without: PropsData = { ...props, props: props.props.filter(([t]) => !foodIds.has(t)) };
    const extra = props.props.filter(([t]) => foodIds.has(t)).map(([t, x, z, r]) => [t, x + 5, z, r] as Prop);
    const doubled: PropsData = { ...props, props: [...props.props, ...extra] };
    const [base, one, two] = [without, props, doubled].map((d) => new Props(d));
    const c0 = countDrawCalls(base.group), c1 = countDrawCalls(one.group), c2 = countDrawCalls(two.group);
    console.log(`food props: ${c0} prop draw calls without, ${c1} with ${food.length} placements, ${c2} with ${2 * food.length}`);
    expect(c1 - c0).toBeLessThanOrEqual(FOOD.length);
    expect(c2).toBe(c1);
    for (const p of [base, one, two]) p.dispose();
  });
});

describe('string-light bulbs (AC4)', () => {
  const lightsOnly: PropsData = { ...props, props: props.props.filter(([t]) => props.types[t] === 'string_lights') };

  it('draws the bulbs with the emissive lamp-glass term, lit from sunset to sunrise like the lamp heads', () => {
    expect(glowsAtNight('string_lights')).toBe(true);
    expect(STRING_BULBS.length).toBeGreaterThan(4);
    const p = new Props(lightsOnly);
    const bulbTris = glassGeometry('string_lights').getAttribute('position').count / 3;
    let glowing = 0;
    for (const c of p.group.children) {
      const g = (c as THREE.Mesh).geometry.getAttribute('glow');
      for (let i = 0; i < g.count; i++) glowing += g.getX(i);
    }
    expect(glowing / 3).toBe(lightsOnly.props.length * bulbTris);
    // The same uniform as the lamp heads: on at night, off by day.
    const l = new LampLights(lampSources(loadProps()), p.glow);
    l.update(-0.2, new THREE.Vector3(), 0);
    expect(p.glow.value.getHex()).toBe(GLASS_GLOW);
    l.update(0.3, new THREE.Vector3(), 100);
    expect(p.glow.value.getHex()).toBe(0);
    l.update(0, new THREE.Vector3(), 200);
    expect(p.glow.value.getHex()).toBe(GLASS_GLOW);
    p.dispose();
  });

  it('adds no point lights: no light sources of their own, and the scene keeps MAX_POINT_LIGHTS', () => {
    expect(LAMP_HEADS.string_lights).toBeUndefined();
    expect(lampSources(lightsOnly)).toHaveLength(0);
    const data = loadProps();
    const lamps = data.props.filter(([t]) => LAMP_HEADS[data.types[t]]).length;
    expect(lampSources(data)).toHaveLength(lamps);
    const w = new WorldScene(() => 0);
    let pointLights = 0;
    w.scene.traverse((o) => { if ((o as THREE.PointLight).isPointLight) pointLights++; });
    expect(pointLights).toBeLessThanOrEqual(MAX_POINT_LIGHTS);
  });
});
