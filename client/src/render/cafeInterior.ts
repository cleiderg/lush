import * as THREE from 'three';
import areasRaw from '../../../shared/map/areas.json?raw';
import { MeshBuilder } from './campusMap';
import { DOWN, UP, quad } from './facade';
import { box } from './gameAreas';
import type { LightSource } from './lights';
import { addLampGlow, lump, prism } from './propKit';
import { parseAreas } from './mapData';

/**
 * The Student Center café, furnished (docs/specs/visual-style.md, Buildings and interiors): a counter,
 * menu board, tables, chairs, plates and cups, plants, rugs, a fireplace and a big window, under warm
 * lights. Visual only: furniture has no collision (#405). Placement is a fixed list, so client and
 * server never disagree and nothing needs a random generator.
 */

/** The café's walkable floor, metres: x0, z0, x1, z1 (areas.mjs CAFE.floor). */
export const CAFE_FLOOR = [40, -355, 50, -347] as const;
/** The cells cut through the south wall, metres: x0, z0, x1, z1 (areas.mjs CAFE.door). */
export const CAFE_DOOR = [44, -347, 46, -346] as const;
/** Height of the door opening in the Student Center's wall, and of the café's inner walls. */
export const DOOR_OPENING_HEIGHT = 2.6;
export const CAFE_WALL_HEIGHT = 3.2;
/** Draw calls the furnished café adds, however many props it holds: the shell and the props. */
export const CAFE_DRAW_CALLS = 2;

// Flat colours: a wood floor and plaster walls, nothing like the sandstone outside.
export const FLOOR_WOOD = 0x9a6a3e;
export const FLOOR_WOOD_DARK = 0x86592f;
export const PLASTER = 0xefe6d2;
export const PLASTER_DADO = 0xc9b48e;
const WOOD = 0x8a5a34;
const WOOD_DARK = 0x5e3a20;
const WOOD_LIGHT = 0xb07a48;
const BRICK = 0xa4553c;
const STONE = 0x6e6a64;
const SLATE = 0x2d3a34;
const CHALK = 0xf2eee4;
const CREAM = 0xf4efe0;
const CERAMIC = 0xe9e4d8;
const COFFEE = 0x5a3a24;
const LEAF = 0x4f8a3a;
const LEAF_LIGHT = 0x6fa84a;
const TERRACOTTA = 0xb8643c;
const RUG_RED = 0x9a3a32;
const RUG_TRIM = 0xd9b45a;
const RUG_BLUE = 0x2f5578;
const BRASS = 0xc89a3c;
const CUSHION = 0xb84a3a;
const GLASS_DAY = 0x9cc3d6;
const GLOW = 0xffd27a;
const FLAME = 0xffa23c;

type Add = (b: MeshBuilder) => void;
interface PropType { body: Add; glass?: Add }

const round = (b: MeshBuilder, cx: number, cz: number, r: number, y0: number, y1: number, color: number, caps = true) =>
  prism(b, cx, cz, r, r, y0, y1, 8, color, caps);

/** Table-top height; plates, cups and candles stand on it. */
export const TABLE_Y = 0.75;

// ---------- The kit. Each model stands on y = 0, centred on the origin, front toward +z. ----------

export const CAFE_KIT: Readonly<Record<string, PropType>> = {
  // A 4 m counter with a stone top, panelled front and a till.
  counter: {
    body: (b) => {
      box(b, -2, 0, -0.45, 2, 0.95, 0.45, WOOD_DARK);
      box(b, -2.08, 0.95, -0.52, 2.08, 1.03, 0.52, STONE);
      for (let i = 0; i < 4; i++) box(b, -1.9 + i * 1, 0.15, 0.45, -1.1 + i * 1, 0.85, 0.5, WOOD);
      box(b, 1.1, 1.03, -0.2, 1.6, 1.3, 0.15, SLATE); // till
      box(b, -1.6, 1.03, -0.15, -1.1, 1.08, 0.15, CHALK);
    },
  },
  // A chalkboard menu on a wall, 2.4 m wide, with a wooden frame and chalk lines.
  menu_board: {
    body: (b) => {
      box(b, -1.2, 1.9, -0.04, 1.2, 2.9, 0.02, WOOD_LIGHT);
      box(b, -1.12, 1.97, 0, 1.12, 2.83, 0.04, SLATE);
      for (let i = 0; i < 5; i++) box(b, -0.9, 2.65 - i * 0.14, 0.04, i % 2 ? 0.3 : 0.8, 2.69 - i * 0.14, 0.05, CHALK);
    },
  },
  // Shelves of cups and jars for behind the counter.
  shelf: {
    body: (b) => {
      box(b, -1, 0, -0.18, 1, 0.1, 0.18, WOOD_DARK);
      for (const y of [1.2, 1.7, 2.2]) {
        box(b, -1, y, -0.2, 1, y + 0.05, 0.1, WOOD);
        for (let i = 0; i < 4; i++) box(b, -0.8 + i * 0.5, y + 0.05, -0.12, -0.6 + i * 0.5, y + 0.25, 0.02, i % 2 ? CERAMIC : COFFEE);
      }
      box(b, -1.02, 0.1, -0.2, -0.94, 2.3, 0.1, WOOD);
      box(b, 0.94, 0.1, -0.2, 1.02, 2.3, 0.1, WOOD);
    },
  },
  // A brick fireplace on the wall, 1.8 m wide, with a mantel, a firebox and flames that glow.
  fireplace: {
    body: (b) => {
      box(b, -0.9, 0, -0.35, 0.9, 1.1, 0.25, BRICK);
      box(b, -0.5, 0.1, 0.25, 0.5, 0.8, 0.27, SLATE);
      box(b, -0.5, 0.1, -0.2, 0.5, 0.18, 0.25, STONE);
      box(b, -0.4, 1.1, -0.3, 0.4, 2.8, 0.0, BRICK); // chimney breast
      box(b, -1.05, 1.1, -0.4, 1.05, 1.2, 0.4, WOOD_DARK); // mantel
      box(b, -0.8, 1.2, -0.05, -0.55, 1.5, 0.1, CERAMIC);
      box(b, 0.4, 1.2, -0.05, 0.6, 1.7, 0.1, BRASS);
    },
    glass: (b) => {
      lump(b, -0.15, 0.18, 0, 0.18, 0.45, 0.1, FLAME);
      lump(b, 0.2, 0.18, 0.02, 0.14, 0.35, 0.08, GLOW);
    },
  },
  // A big window in a wall, 2.6 m wide, with a deep frame, a sill and a pair of panes that glow at night.
  window: {
    body: (b) => {
      box(b, -1.5, 0.7, -0.1, -1.3, 2.9, 0.08, WOOD_LIGHT);
      box(b, 1.3, 0.7, -0.1, 1.5, 2.9, 0.08, WOOD_LIGHT);
      box(b, -1.5, 2.9, -0.1, 1.5, 3.05, 0.08, WOOD_LIGHT);
      box(b, -1.5, 0.7, -0.1, 1.5, 0.82, 0.08, WOOD_LIGHT);
      box(b, -1.65, 0.62, -0.1, 1.65, 0.7, 0.22, WOOD_LIGHT); // sill
      box(b, -0.04, 0.82, -0.09, 0.04, 2.9, 0.07, WOOD_LIGHT); // mullion
    },
    glass: (b) => {
      box(b, -1.3, 0.82, -0.02, -0.04, 2.9, 0.03, GLASS_DAY);
      box(b, 0.04, 0.82, -0.02, 1.3, 2.9, 0.03, GLASS_DAY);
    },
  },
  // A flat rug, 2.8 m by 1.8 m, with a trim border.
  rug: {
    body: (b) => {
      box(b, -1.4, 0, -0.9, 1.4, 0.02, 0.9, RUG_TRIM);
      box(b, -1.25, 0.02, -0.75, 1.25, 0.03, 0.75, RUG_RED);
      box(b, -0.7, 0.03, -0.35, 0.7, 0.035, 0.35, RUG_TRIM);
    },
  },
  // A runner of the same make, 1.6 m by 0.9 m, in blue.
  rug_small: {
    body: (b) => {
      box(b, -0.8, 0, -0.45, 0.8, 0.02, 0.45, RUG_TRIM);
      box(b, -0.7, 0.02, -0.35, 0.7, 0.03, 0.35, RUG_BLUE);
    },
  },
  // A round table, 1.1 m across, on a single pedestal.
  table: {
    body: (b) => {
      round(b, 0, 0, 0.55, TABLE_Y - 0.05, TABLE_Y, WOOD_LIGHT);
      round(b, 0, 0, 0.08, 0.05, TABLE_Y - 0.05, WOOD_DARK, false);
      round(b, 0, 0, 0.28, 0, 0.05, WOOD_DARK);
    },
  },
  // A café chair: four legs, a seat and a ladder back.
  chair: {
    body: (b) => {
      for (const [x, z] of [[-0.2, -0.2], [0.14, -0.2], [-0.2, 0.14], [0.14, 0.14]]) box(b, x, 0, z, x + 0.06, 0.45, z + 0.06, WOOD_DARK);
      box(b, -0.22, 0.45, -0.22, 0.22, 0.5, 0.22, WOOD);
      box(b, -0.2, 0.5, -0.22, -0.14, 0.95, -0.17, WOOD_DARK);
      box(b, 0.14, 0.5, -0.22, 0.2, 0.95, -0.17, WOOD_DARK);
      box(b, -0.2, 0.72, -0.22, 0.2, 0.78, -0.18, WOOD);
      box(b, -0.2, 0.86, -0.22, 0.2, 0.92, -0.18, WOOD);
    },
  },
  // A bar stool with a cushion, for the counter.
  stool: {
    body: (b) => {
      round(b, 0, 0, 0.03, 0, 0.65, WOOD_DARK, false);
      round(b, 0, 0, 0.2, 0, 0.04, WOOD_DARK);
      round(b, 0, 0, 0.19, 0.65, 0.72, CUSHION);
    },
  },
  // A plate with a pastry on it, for a table top.
  plate: {
    body: (b) => {
      prism(b, 0, 0, 0.13, 0.1, 0, 0.025, 8, CERAMIC);
      lump(b, 0, 0.025, 0, 0.07, 0.04, 0.06, 0xd8a05a);
    },
  },
  // A coffee cup on a saucer.
  cup: {
    body: (b) => {
      prism(b, 0, 0, 0.09, 0.09, 0, 0.012, 8, CERAMIC);
      prism(b, 0, 0, 0.05, 0.055, 0.012, 0.09, 8, CREAM);
      prism(b, 0, 0, 0.045, 0.045, 0.085, 0.09, 8, COFFEE);
      box(b, 0.05, 0.04, -0.01, 0.09, 0.07, 0.01, CREAM);
    },
  },
  // A candle in a holder that stands on a table; its flame lights up.
  candle: {
    body: (b) => {
      prism(b, 0, 0, 0.05, 0.05, 0, 0.02, 6, BRASS);
      prism(b, 0, 0, 0.025, 0.025, 0.02, 0.1, 6, CREAM);
    },
    glass: (b) => lump(b, 0, 0.1, 0, 0.018, 0.04, 0.018, FLAME),
  },
  // A potted plant, 1.1 m tall.
  plant: {
    body: (b) => {
      prism(b, 0, 0, 0.2, 0.3, 0, 0.5, 8, TERRACOTTA);
      lump(b, 0, 0.5, 0, 0.32, 0.4, 0.32, LEAF);
      lump(b, 0.1, 0.8, 0.05, 0.22, 0.3, 0.22, LEAF_LIGHT);
    },
  },
  // A pendant lamp hanging from the ceiling on a cord; the shade's glass lights up.
  pendant: {
    body: (b) => {
      prism(b, 0, 0, 0.01, 0.01, 2.6, CAFE_WALL_HEIGHT, 4, WOOD_DARK, false);
      prism(b, 0, 0, 0.07, 0.3, 2.45, 2.6, 8, BRASS);
    },
    glass: (b) => prism(b, 0, 0, 0.06, 0.06, 2.4, 2.45, 8, GLOW),
  },
};

/** One placement: type, x and z in metres, rotation in whole degrees (0 faces south, +z; 90 faces west), height of its base. */
export type Placement = readonly [type: string, x: number, z: number, rot: number, y: number];

/** Degrees of rotation that make a prop at (x, z) face (tx, tz). */
function faceTo(x: number, z: number, tx: number, tz: number): number {
  return Math.round((Math.atan2(-(tx - x), tz - z) * 180) / Math.PI);
}

function build(): Placement[] {
  const list: Placement[] = [];
  const put = (type: string, x: number, z: number, rot = 0, y = 0) => list.push([type, x, z, rot, y]);
  // North wall: the counter in front of a menu board and shelves, bar stools in front.
  put('counter', 45, -353.9);
  put('menu_board', 45, -354.9);
  put('shelf', 42.2, -354.8);
  put('shelf', 47.8, -354.8);
  for (const x of [43.4, 45, 46.6]) put('stool', x, -352.5);
  put('cup', 43.6, -353.9, 0, 1.03);
  put('plate', 46.3, -353.5, 0, 1.03);
  // East wall: the fireplace, with a plant either side. West wall: a big window, and one more on the south wall.
  put('fireplace', 49.55, -351, 90);
  put('window', 40.35, -351, 270);
  put('window', 40.35, -348.6, 270);
  // Rugs.
  put('rug', 45, -350.3);
  put('rug_small', 48.2, -351);
  // Round tables with chairs, plates, cups and candles.
  const tables: [number, number][] = [[41.9, -352.9], [42.2, -349.9], [47.9, -348.6], [47.9, -353.2]];
  const chairs = [[-0.85, 0], [0.85, 0], [0, 0.85]];
  tables.forEach(([x, z], i) => {
    put('table', x, z);
    for (const [dx, dz] of chairs) {
      if (i === 0 && dz === 0 && dx < 0) continue; // the corner table keeps its west side free
      put('chair', x + dx, z + dz, faceTo(x + dx, z + dz, x, z));
    }
    put('plate', x - 0.2, z + 0.1, 0, TABLE_Y);
    put('cup', x + 0.2, z - 0.1, 0, TABLE_Y);
    if (i % 2 === 0) put('candle', x, z, 0, TABLE_Y);
  });
  // Plants in the corners.
  for (const [x, z] of [[40.7, -354.3], [49.3, -354.3], [40.7, -347.7], [49.3, -347.7]]) put('plant', x, z);
  // Pendant lamps over the middle of the room.
  for (const [x, z] of [[42.5, -351], [47.5, -351], [45, -352.2], [45, -349]]) put('pendant', x, z);
  return list;
}

/** Every prop in the café. */
export const CAFE_PLACEMENTS: readonly Placement[] = build();

/** Light given off by a prop type, as an offset in the prop's own frame (metres, before rotation). */
const LIGHT_AT: Readonly<Record<string, { y: number; flame: boolean }>> = {
  pendant: { y: 2.3, flame: false },
  fireplace: { y: 0.55, flame: true },
  candle: { y: 0.2, flame: true },
};

/** Warm light sources for the café, in world metres, to register with LampLights (the nearest 8 shine). */
export function cafeLightSources(placements: readonly Placement[] = CAFE_PLACEMENTS): LightSource[] {
  const out: LightSource[] = [];
  for (const [t, x, z, , y] of placements) {
    const l = LIGHT_AT[t];
    if (l) out.push({ x, y: y + l.y, z, flame: l.flame });
  }
  return out;
}

/** The merged model of every prop in `placements` (body glow 0, glass glow 1), in world metres. */
export function cafePropsGeometry(placements: readonly Placement[] = CAFE_PLACEMENTS): THREE.BufferGeometry {
  const body = new MeshBuilder();
  const m = new THREE.Matrix4();
  const v = new THREE.Vector3();
  const place = (src: MeshBuilder, dst: MeshBuilder, x: number, y: number, z: number, rot: number) => {
    m.makeRotationY((-rot * Math.PI) / 180).setPosition(x, y, z);
    const pos = src.pos, col = src.col;
    const pt = (i: number) => {
      v.set(pos[i], pos[i + 1], pos[i + 2]).applyMatrix4(m);
      return [v.x, v.y, v.z];
    };
    for (let i = 0; i < pos.length; i += 9) {
      const a = pt(i), b = pt(i + 3), c = pt(i + 6);
      const n = [(b[1] - a[1]) * (c[2] - a[2]) - (b[2] - a[2]) * (c[1] - a[1]), (b[2] - a[2]) * (c[0] - a[0]) - (b[0] - a[0]) * (c[2] - a[2]), (b[0] - a[0]) * (c[1] - a[1]) - (b[1] - a[1]) * (c[0] - a[0])];
      dst.tri(a, b, c, n, new THREE.Color(col[i], col[i + 1], col[i + 2]).getHex());
    }
  };
  const models = new Map<string, { body: MeshBuilder; glass: MeshBuilder }>();
  for (const [t, def] of Object.entries(CAFE_KIT)) {
    const bo = new MeshBuilder(), gl = new MeshBuilder();
    def.body(bo);
    def.glass?.(gl);
    models.set(t, { body: bo, glass: gl });
  }
  for (const [t, x, z, rot, y] of placements) {
    const model = models.get(t);
    if (!model) throw new Error(`unknown café prop ${t}`);
    place(model.body, body, x, y, z, rot);
    place(model.glass, body.glass, x, y, z, rot);
  }
  return body.geometry(true);
}

/** Triangles in one prop type's model, glass included. */
export function cafeTypeTriangles(type: string): number {
  const bo = new MeshBuilder(), gl = new MeshBuilder();
  CAFE_KIT[type].body(bo);
  CAFE_KIT[type].glass?.(gl);
  return bo.triangles + gl.triangles;
}

/** Whether a point (metres) is on one of the café's door cells. */
export function onDoorCell(x: number, z: number): boolean {
  return x >= CAFE_DOOR[0] && x < CAFE_DOOR[2] && z >= CAFE_DOOR[1] && z < CAFE_DOOR[3];
}

/**
 * The café's floor and inner walls: wood boards under plaster walls with a panelled dado, a gap in the
 * south wall where the door is. Own colours and material, apart from the sandstone outside.
 */
export function cafeShellGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  const [x0, z0, x1, z1] = CAFE_FLOOR;
  const [dx0, , dx1] = CAFE_DOOR;
  const y = 0.02;
  // Floor boards, alternating tone.
  for (let i = 0, x = x0; x < x1; i++, x += 1) {
    quad(b, [x, y, z0], [x + 1, y, z0], [x + 1, y, z1], [x, y, z1], UP, i % 2 ? FLOOR_WOOD_DARK : FLOOR_WOOD);
  }
  const H = CAFE_WALL_HEIGHT, dado = 1;
  // A wall face from (ax, az) to (bx, bz), facing `n` (into the room), with the dado below.
  const wall = (ax: number, az: number, bx: number, bz: number, n: number[], top = H, from = 0) => {
    quad(b, [ax, from, az], [bx, from, bz], [bx, Math.min(top, dado), bz], [ax, Math.min(top, dado), az], n, PLASTER_DADO);
    if (top > dado) quad(b, [ax, dado, az], [bx, dado, bz], [bx, top, bz], [ax, top, az], n, PLASTER);
  };
  wall(x0, z0, x1, z0, [0, 0, 1]);
  wall(x0, z0, x0, z1, [1, 0, 0]);
  wall(x1, z0, x1, z1, [-1, 0, 0]);
  wall(x0, z1, dx0, z1, [0, 0, -1]);
  wall(dx1, z1, x1, z1, [0, 0, -1]);
  // Over the door.
  wall(dx0, z1, dx1, z1, [0, 0, -1], H, DOOR_OPENING_HEIGHT);
  // Door jambs, a step in the plaster.
  for (const x of [dx0, dx1]) {
    quad(b, [x, 0, z1], [x, 0, z1 + 1], [x, DOOR_OPENING_HEIGHT, z1 + 1], [x, DOOR_OPENING_HEIGHT, z1], [x === dx0 ? 1 : -1, 0, 0], PLASTER_DADO);
  }
  quad(b, [dx0, y, z1], [dx1, y, z1], [dx1, y, z1 + 1], [dx0, y, z1 + 1], UP, FLOOR_WOOD_DARK);
  // Ceiling beams: dark wood across the room.
  for (let x = x0 + 2.5; x < x1; x += 2.5) box(b, x - 0.1, H - 0.2, z0, x + 0.1, H, z1, WOOD_DARK);
  quad(b, [x0, H, z0], [x1, H, z0], [x1, H, z1], [x0, H, z1], DOWN, PLASTER);
  return b.geometry(false);
}

/** The furnished café: one mesh for the floor and walls and one for every prop, two draw calls in all. */
export class CafeInterior {
  readonly group = new THREE.Group();
  readonly shell: THREE.Mesh;
  readonly props: THREE.Mesh;
  readonly propsMaterial = new THREE.MeshLambertMaterial({ vertexColors: true });

  /** `glow` is the lamp-glass uniform (Props.glow): the café's lamp, flame and window glass shine while it is lit. */
  constructor(glow: { value: THREE.Color }, placements: readonly Placement[] = CAFE_PLACEMENTS) {
    this.group.name = 'cafe';
    this.shell = new THREE.Mesh(cafeShellGeometry(), new THREE.MeshLambertMaterial({ vertexColors: true }));
    this.shell.name = 'cafe:shell';
    this.propsMaterial.onBeforeCompile = (shader) => addLampGlow(shader, glow);
    this.propsMaterial.customProgramCacheKey = () => 'props-lamp-glow';
    this.props = new THREE.Mesh(cafePropsGeometry(placements), this.propsMaterial);
    this.props.name = 'cafe:props';
    for (const m of [this.shell, this.props]) {
      m.matrixAutoUpdate = false;
      m.receiveShadow = true;
      this.group.add(m);
    }
    this.props.castShadow = true;
  }

  dispose(): void {
    for (const m of [this.shell, this.props]) {
      m.geometry.dispose();
      (m.material as THREE.Material).dispose();
    }
  }
}

/** Whether the café floor in areas.json is the one this module furnishes. */
export function cafeFloorMatches(): boolean {
  const c = parseAreas(areasRaw).interiors.find((a) => a.id === 'student_center_cafe');
  if (!c) return false;
  const xs = c.p.filter((_, i) => i % 2 === 0).map((v) => v / 10), zs = c.p.filter((_, i) => i % 2 === 1).map((v) => v / 10);
  return Math.min(...xs) === CAFE_FLOOR[0] && Math.min(...zs) === CAFE_FLOOR[1] && Math.max(...xs) === CAFE_FLOOR[2] && Math.max(...zs) === CAFE_FLOOR[3];
}
