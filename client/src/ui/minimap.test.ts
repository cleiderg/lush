// @vitest-environment happy-dom
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { hudLayout } from './hudLayout';
import {
  BigMap, buildMapData, featuresInRange, mapRotation, Minimap, MINIMAP_CSS, MINIMAP_RANGE_M,
  northMarkerAngle, toMinimap, type MapData,
} from './minimap';
import { THEME } from './theme';

const repo = resolve(__dirname, '../../..');
const campus = JSON.parse(readFileSync(resolve(repo, 'shared/map/campus.json'), 'utf8'));
const areas = JSON.parse(readFileSync(resolve(repo, 'shared/map/areas.json'), 'utf8'));
const SPAWN = { x: areas.spawn.x / 10, z: areas.spawn.z / 10 };
const skills = JSON.parse(readFileSync(resolve(repo, 'shared/map/skills.json'), 'utf8'));
const BANK = { x: skills.bank.x / 10, z: skills.bank.z / 10 };
const CAMPUS = buildMapData(campus, areas, skills);
/** Near the buildings south of Aldrich Park (the spawn, in the park, has none within 60 m). */
const TOWN = { x: -22.6, z: 50 };

let minimap: Minimap | null = null;
let bigMap: BigMap | null = null;
beforeEach(() => document.body.replaceChildren());
afterEach(() => {
  minimap?.dispose();
  bigMap?.dispose();
  minimap = bigMap = null;
});

interface Setup {
  pos?: { x: number; z: number };
  yaw?: number;
  remotes?: { id: number; x: number; z: number }[];
  data?: MapData;
}

function mount(s: Setup = {}) {
  const state = { pos: s.pos ?? { ...SPAWN }, yaw: s.yaw ?? Math.PI, remotes: s.remotes ?? [] };
  let opened = 0;
  minimap = new Minimap({
    parent: document.body, position: () => state.pos, yaw: () => state.yaw, remotes: () => state.remotes,
    data: s.data ?? CAMPUS, onOpen: () => opened++, viewport: () => ({ width: 390, height: 844 }),
  });
  return { state, m: minimap, opened: () => opened };
}

function drawnIds(m: Minimap, kind: string): string[] {
  return [...m.features.querySelectorAll(`.lush-map-${kind}`)].map((e) => e.getAttribute('data-id') ?? '');
}

/** Every vertex of a feature, in metres. */
function vertices(p: number[]): [number, number][] {
  const out: [number, number][] = [];
  for (let i = 0; i + 1 < p.length; i += 2) out.push([p[i] / 10, p[i + 1] / 10]);
  return out;
}

const near = (pts: [number, number][], pos: { x: number; z: number }, r: number) => pts.some(([x, z]) => Math.hypot(x - pos.x, z - pos.z) <= r);

describe('Minimap contents (AC1)', () => {
  it('draws the campus.json buildings and paths within MINIMAP_RANGE_M of the player and none far beyond it', () => {
    expect(MINIMAP_RANGE_M).toBe(60);
    const { m } = mount({ pos: TOWN });
    const buildings = new Set(drawnIds(m, 'building'));
    const paths = new Set(drawnIds(m, 'path'));
    expect(buildings.size).toBeGreaterThan(0);
    expect(paths.size).toBeGreaterThan(0);
    for (const b of campus.buildings) {
      const pts = vertices(b.p);
      // Any corner in range means it's drawn; nothing with every corner more than range + its own size away is.
      if (near(pts, TOWN, MINIMAP_RANGE_M)) expect(buildings.has(`building-${b.id}`), `building ${b.id}`).toBe(true);
      const xs = pts.map((p) => p[0]), zs = pts.map((p) => p[1]);
      const size = Math.hypot(Math.max(...xs) - Math.min(...xs), Math.max(...zs) - Math.min(...zs));
      if (!near(pts, TOWN, MINIMAP_RANGE_M + size)) expect(buildings.has(`building-${b.id}`), `building ${b.id}`).toBe(false);
    }
    for (const w of campus.paths) {
      const pts = vertices(w.p);
      if (near(pts, TOWN, MINIMAP_RANGE_M)) expect(paths.has(`path-${w.id}`), `path ${w.id}`).toBe(true);
    }
    // Every drawn shape lies (at least partly) within range.
    expect(new Set(featuresInRange(CAMPUS, TOWN).map((f) => f.id))).toEqual(
      new Set([...m.features.children].map((e) => e.getAttribute('data-id'))));
  });

  it('uses exact distances: a feature 59 m away is drawn, one 61 m away is not', () => {
    const data = buildMapData({
      bounds: { minX: -2000, minZ: -2000, maxX: 2000, maxZ: 2000 },
      buildings: [
        { id: 1, p: [590, -50, 650, -50, 650, 50, 590, 50] },
        { id: 2, p: [-650, -50, -610, -50, -610, 50, -650, 50] },
        // The player stands inside this one.
        { id: 3, p: [-20, -20, 20, -20, 20, 20, -20, 20] },
      ],
      paths: [{ id: 7, w: 30, p: [-1000, 590, 1000, 590] }, { id: 8, w: 30, p: [-1000, -610, 1000, -610] }],
      plazas: [],
    }, { ...areas, areas: [] });
    const { m } = mount({ pos: { x: 0, z: 0 }, data });
    expect(drawnIds(m, 'building').sort()).toEqual(['building-1', 'building-3']);
    expect(drawnIds(m, 'path')).toEqual(['path-7']);
  });

  it('puts the player at the centre, each remote player in range as a dot, and the shop as an icon', () => {
    const pos = { ...SPAWN };
    const remotes = [
      { id: 2, x: pos.x + 10, z: pos.z },
      { id: 3, x: pos.x, z: pos.z - 30 },
      { id: 4, x: pos.x + 500, z: pos.z },
    ];
    const { m, state } = mount({ pos, remotes, yaw: Math.PI });
    const player = m.svg.querySelector('.lush-map-player circle')!;
    expect([player.getAttribute('cx'), player.getAttribute('cy')]).toEqual(['0', '0']);
    const dots = [...m.dots.querySelectorAll('.lush-map-remote')];
    expect(dots.map((d) => d.getAttribute('data-id'))).toEqual(['2', '3']);
    expect(Number(dots[0].getAttribute('cx'))).toBeCloseTo(pos.x + 10);
    expect(Number(dots[1].getAttribute('cy'))).toBeCloseTo(pos.z - 30);
    // The dots live in the world layer, which is centred on the player.
    expect(m.world.getAttribute('transform')).toBe(`rotate(0.00) translate(${(-pos.x).toFixed(2)} ${(-pos.z).toFixed(2)})`);

    // The shop (20 m north of the spawn) is an icon, upright, north-up at yaw π: straight above the centre.
    expect(m.shop.style.display).toBe('');
    expect(m.shop.getAttribute('transform')).toBe('translate(0.00 -20.00)');
    expect(m.shop.querySelector('.lush-map-shop-awning')).not.toBeNull();

    // Players and the shop drop off when out of range.
    state.remotes = [{ id: 5, x: pos.x + 1, z: pos.z }];
    state.pos = { x: pos.x + 300, z: pos.z };
    m.update();
    expect(m.dots.childElementCount).toBe(0);
    expect(m.shop.style.display).toBe('none');
  });

  it('draws the PvP zone from areas.json in red when it is in range', () => {
    const pvp = areas.areas.find((a: { kind: string }) => a.kind === 'pvp');
    const pts = vertices(pvp.p);
    const inside = { x: pts.reduce((s, p) => s + p[0], 0) / pts.length, z: pts.reduce((s, p) => s + p[1], 0) / pts.length };
    const { m, state } = mount({ pos: inside });
    const zone = m.features.querySelector('.lush-map-pvp')!;
    expect(zone.getAttribute('data-id')).toBe(pvp.id);
    expect(MINIMAP_CSS).toMatch(/\.lush-map-pvp \{[^}]*stroke: var\(--lush-danger\)/);
    expect(MINIMAP_CSS).toMatch(/\.lush-map-pvp \{[^}]*fill: rgba\(229, 83, 61/);
    expect(THEME['--lush-danger']).toBe('#e5533d');
    // Far away (the spawn is in the safe area), it isn't drawn.
    state.pos = { x: inside.x + 2000, z: inside.z };
    m.update();
    expect(m.features.querySelector('.lush-map-pvp')).toBeNull();
  });

  it('has a gold rim and an N marker', () => {
    const { m } = mount();
    expect(m.svg.querySelector('.lush-minimap-rim')).not.toBeNull();
    expect(MINIMAP_CSS).toMatch(/\.lush-minimap-rim \{[^}]*stroke: var\(--lush-gold\)/);
    expect(m.north.querySelector('text')?.textContent).toBe('N');
  });
});

describe('Minimap rotation (AC2)', () => {
  it('is north-up when the camera looks north and turns with the camera', () => {
    // The rig at yaw π looks along (sin π, cos π) = north.
    expect(mapRotation(Math.PI)).toBeCloseTo(0);
    const ahead = toMinimap(Math.sin(1.1) * 10, Math.cos(1.1) * 10, 1.1);
    // Whatever way the camera looks is straight up the minimap.
    expect(ahead.x).toBeCloseTo(0);
    expect(ahead.y).toBeCloseTo(-10);
  });

  it('turning the camera by 90° moves the N marker by 90° around the rim, always toward world north', () => {
    const { m, state } = mount({ yaw: Math.PI });
    expect(m.northAngle).toBeCloseTo(0);
    const angles: number[] = [];
    for (let k = 0; k < 4; k++) {
      state.yaw = Math.PI + (k * Math.PI) / 2;
      m.update();
      angles.push(m.northAngle);
      // The marker's direction from the centre is where world north (-z) lands on the map.
      const n = toMinimap(0, -1, state.yaw);
      const rad = (m.northAngle * Math.PI) / 180;
      expect(Math.sin(rad)).toBeCloseTo(n.x);
      expect(-Math.cos(rad)).toBeCloseTo(n.y);
      expect(m.north.getAttribute('transform')).toBe(`rotate(${m.northAngle.toFixed(2)})`);
      expect(m.world.getAttribute('transform')).toMatch(new RegExp(`^rotate\\(${m.northAngle.toFixed(2)}\\)`));
    }
    expect(angles).toEqual([0, 90, 180, 270]);
    expect(northMarkerAngle(-Math.PI / 2)).toBeCloseTo(90);
  });
});

describe('Minimap tap and big map (AC4)', () => {
  it('sits at the minimap slot of hudLayout', () => {
    const { m } = mount();
    const r = hudLayout(390, 844).minimap;
    expect(m.root.style.left).toBe(`${r.x}px`);
    expect(m.root.style.top).toBe(`${r.y}px`);
    expect(m.root.style.width).toBe(`${r.w}px`);
  });

  it('draws a bank icon at the booth, placed like the shop icon, hidden beyond the range', () => {
    const pos = { x: BANK.x + 10, z: BANK.z + 20 };
    const { m, state } = mount({ pos, yaw: Math.PI });
    expect(m.bank!.style.display).toBe('');
    expect(m.bank!.getAttribute('transform')).toBe('translate(-10.00 -20.00)');
    expect(m.bank!.querySelector('.lush-map-bank-door')).not.toBeNull();
    expect(m.bank!.querySelector('.lush-map-shop-awning')).toBeNull();
    state.yaw = 0;
    m.update();
    expect(m.bank!.getAttribute('transform')).toBe('translate(10.00 20.00)');

    state.pos = { x: BANK.x + MINIMAP_RANGE_M - 1, z: BANK.z };
    m.update();
    expect(m.bank!.style.display).toBe('');
    state.pos = { x: BANK.x + MINIMAP_RANGE_M + 1, z: BANK.z };
    m.update();
    expect(m.bank!.style.display).toBe('none');
  });

  it('tapping the minimap opens the big map panel in the shared theme; its close button closes it', () => {
    bigMap = new BigMap({
      parent: document.body, data: CAMPUS, position: () => SPAWN, remotes: () => [{ id: 2, x: 0, z: 0 }], viewport: () => ({ width: 390, height: 844 }),
    });
    const map = bigMap;
    minimap = new Minimap({
      parent: document.body, data: CAMPUS, position: () => SPAWN, yaw: () => Math.PI, remotes: () => [], onOpen: () => map.open(),
      viewport: () => ({ width: 390, height: 844 }),
    });
    expect(map.isOpen).toBe(false);
    minimap.root.click();
    expect(map.isOpen).toBe(true);
    expect(map.panel.classList.contains('lush-panel')).toBe(true);
    expect(map.panel.getAttribute('role')).toBe('dialog');
    expect(map.panel.querySelector('.lush-title')?.textContent).toBe('Campus map');
    const r = hudLayout(390, 844).map;
    expect(map.panel.style.top).toBe(`${r.y}px`);
    expect(map.panel.style.height).toBe(`${r.h}px`);
    // The whole campus is drawn, with the player, the other players, the shop and the PvP zone.
    expect(map.svg!.querySelectorAll('.lush-map-building').length).toBe(campus.buildings.length);
    expect(map.svg!.querySelector('.lush-map-pvp')).not.toBeNull();
    expect(map.svg!.querySelector('.lush-map-shop')).not.toBeNull();
    expect(map.svg!.querySelector('.lush-map-bank')!.getAttribute('transform')).toBe(`translate(${BANK.x} ${BANK.z})`);
    expect(map.svg!.querySelector('.lush-map-player')!.getAttribute('transform')).toBe(`translate(${SPAWN.x.toFixed(1)} ${SPAWN.z.toFixed(1)})`);
    expect(map.svg!.querySelectorAll('.lush-map-remote').length).toBe(1);

    map.closeButton.click();
    expect(map.isOpen).toBe(false);
    expect(map.panel.hasAttribute('inert')).toBe(true);
    minimap.root.click();
    expect(map.isOpen).toBe(true);
  });

  it('calls onOpen when it opens so other panels close', () => {
    let calls = 0;
    bigMap = new BigMap({ parent: document.body, data: CAMPUS, position: () => SPAWN, remotes: () => [], onOpen: () => calls++ });
    bigMap.open();
    bigMap.open();
    expect(calls).toBe(1);
  });
});
