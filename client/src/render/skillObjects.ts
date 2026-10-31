import * as THREE from 'three';
import skillsRaw from '../../../shared/map/skills.json?raw';
import { MeshBuilder } from './campusMap';
import { box, polygonMetres } from './gameAreas';
import { parseSkills, type SkillTree, type SkillsData } from './mapData';
import { blob, limb } from './parkNature';
import { lump, prism } from './propKit';
import { mulberry32 } from './rng';

const DM = 0.1;
/** Pond water: above the green areas and paths, below the PvP border. */
export const Y_WATER = 0.06;

// Flat colours after the reference style (visual-style.md): warm woods, soft greens, a calm blue pond.
const BARK = 0x6e4c2f;
const BARK_DARK = 0x573b24;
const TREE_LEAF = [0x6aa440, 0x78b24a, 0x5e9a3a];
const OAK_LEAF = [0x3f7a2c, 0x4a8832, 0x356c26];
const EUCA_BARK = 0xd8cdb4;
const EUCA_STREAK = 0xa99a7c;
const EUCA_LEAF = [0x86a88a, 0x76997c, 0x93b494];
const CUT = 0xd9b77e;
const RING = 0xb8935a;
const WOOD = 0x8a5a34;
const WOOD_DARK = 0x5e3d22;
const COUNTER = 0xa8743f;
const ROOF = [0x2f5f8a, 0xe8dcc0];
const GOLD = 0xe0b040;
const IRON = 0x3a3a3e;
const IRON_LIGHT = 0x5a5a60;
const BRICK = 0x9a5a42;
const STONE = [0x8d8a83, 0x9e9a92, 0x7c7972];
const EMBER = 0xff7a2a;
const COAL = 0x2a2220;
const WATER = 0x4f8fb8;
const WATER_EDGE = 0x2e5f80;
const MUD = 0x6b5a40;

/** Tree kinds in skills.json, each drawn with its own model. */
export const SKILL_TREE_KINDS = ['tree', 'oak', 'eucalyptus'] as const;
/** The water's darker edge band, as a share of the distance from the centre to the shore. */
export const POND_EDGE = 0.22;
/** Seed for each tree's facing. Cosmetic, but seeded so every client looks the same. */
const FACING_SEED = 0x451;
/** Per-tree culling sphere (metres): centred this high over the trunk, with this radius. Covers the tallest crown and some of its shadow. */
const CULL_Y = 3;
const CULL_RADIUS = 9;

// ---------- Models. Each stands on y = 0, centred on the origin, in metres. ----------

/** A plain tree: straight trunk and a round, light green crown, about 4 m tall. */
function plainTree(b: MeshBuilder): void {
  limb(b, [0, 0, 0], [0, 1.9, 0], 0.2, 0.14, 6, BARK);
  blob(b, 0, 2.6, 0, 1.15, 1.0, 1.1, TREE_LEAF[0], 41);
  blob(b, 0.45, 3.3, 0.15, 0.75, 0.65, 0.7, TREE_LEAF[1], 42);
  blob(b, -0.4, 2.3, -0.3, 0.7, 0.55, 0.7, TREE_LEAF[2], 43);
}

/** An oak: thick, flared trunk, low spreading limbs and a broad, dark crown, about 5 m tall. */
function oakTree(b: MeshBuilder): void {
  prism(b, 0, 0, 0.42, 0.3, 0, 0.3, 7, BARK_DARK, false);
  limb(b, [0, 0.2, 0], [0, 1.7, 0], 0.32, 0.22, 7, BARK);
  limb(b, [0, 1.4, 0], [1.0, 2.3, 0.3], 0.15, 0.08, 5, BARK_DARK);
  limb(b, [0, 1.5, 0], [-0.9, 2.4, -0.4], 0.14, 0.08, 5, BARK_DARK);
  limb(b, [0, 1.5, 0], [-0.2, 2.3, 0.95], 0.13, 0.07, 5, BARK_DARK);
  blob(b, 0, 3.0, 0, 2.0, 1.1, 1.9, OAK_LEAF[0], 51);
  blob(b, 1.25, 2.6, 0.4, 1.1, 0.8, 1.0, OAK_LEAF[1], 52);
  blob(b, -1.15, 2.7, -0.5, 1.05, 0.8, 1.1, OAK_LEAF[2], 53);
  blob(b, -0.2, 2.6, 1.2, 1.0, 0.7, 0.9, OAK_LEAF[1], 54);
  blob(b, 0.2, 3.8, -0.1, 1.1, 0.7, 1.0, OAK_LEAF[2], 55);
}

/** A eucalyptus: tall, pale, streaked trunk forking high, with sparse blue-green clumps, about 7 m tall. */
function eucalyptusTree(b: MeshBuilder): void {
  limb(b, [0, 0, 0], [0.2, 3.4, 0.05], 0.22, 0.14, 6, EUCA_BARK);
  // Peeling bark streaks, just proud of the trunk.
  limb(b, [0.21, 0.3, 0], [0.32, 2.2, 0.03], 0.035, 0.025, 3, EUCA_STREAK);
  limb(b, [-0.19, 0.6, 0.08], [-0.05, 2.6, 0.12], 0.035, 0.025, 3, EUCA_STREAK);
  limb(b, [0.2, 3.3, 0.05], [0.95, 5.4, 0.35], 0.12, 0.06, 5, EUCA_BARK);
  limb(b, [0.2, 3.3, 0.05], [-0.55, 5.8, -0.3], 0.11, 0.05, 5, EUCA_BARK);
  blob(b, 1.0, 5.7, 0.4, 0.9, 0.5, 0.8, EUCA_LEAF[0], 61, 0.28);
  blob(b, -0.6, 6.1, -0.35, 0.85, 0.5, 0.85, EUCA_LEAF[1], 62, 0.28);
  blob(b, 0.2, 6.6, 0.05, 0.7, 0.45, 0.7, EUCA_LEAF[2], 63, 0.28);
  blob(b, 0.6, 4.7, -0.45, 0.55, 0.35, 0.5, EUCA_LEAF[1], 64, 0.28);
}

/** A felled tree's stump: low trunk with a pale cut face, a growth ring, roots and a few chips. */
function stump(b: MeshBuilder): void {
  prism(b, 0, 0, 0.36, 0.3, 0, 0.45, 8, BARK, false);
  prism(b, 0, 0, 0.29, 0.29, 0.45, 0.46, 8, CUT);
  prism(b, 0, 0, 0.16, 0.16, 0.46, 0.465, 8, RING);
  prism(b, 0, 0, 0.08, 0.08, 0.465, 0.47, 6, CUT);
  for (const a of [0.3, 2.2, 4.1]) {
    limb(b, [Math.cos(a) * 0.2, 0.18, Math.sin(a) * 0.2], [Math.cos(a) * 0.6, 0, Math.sin(a) * 0.6], 0.1, 0.04, 4, BARK_DARK);
  }
  lump(b, 0.6, 0, 0.25, 0.08, 0.03, 0.05, CUT);
  lump(b, -0.45, 0, 0.5, 0.06, 0.03, 0.08, CUT);
}

/** The bank booth: a wooden kiosk about 2.4 m wide, with a counter facing +z, barred window, striped roof and a gold coin sign. */
function bankBooth(b: MeshBuilder): void {
  const w = 1.2, d = 0.8;
  box(b, -w, 0, -d, w, 2.1, d - 0.25, WOOD);
  box(b, -w - 0.05, 0, d - 0.25, w + 0.05, 1.05, d, WOOD_DARK);
  box(b, -w - 0.1, 1.05, d - 0.35, w + 0.1, 1.12, d + 0.15, COUNTER);
  // Window bars above the counter.
  for (let x = -0.9; x <= 0.91; x += 0.3) box(b, x - 0.025, 1.12, d - 0.27, x + 0.025, 1.95, d - 0.22, IRON);
  box(b, -w, 1.95, d - 0.3, w, 2.1, d - 0.2, WOOD_DARK);
  // Corner posts and a striped roof.
  for (const x of [-w - 0.05, w - 0.1]) box(b, x, 0, d - 0.3, x + 0.15, 2.3, d - 0.15, WOOD_DARK);
  const stripes = 6, sw = (2 * w + 0.4) / stripes;
  for (let i = 0; i < stripes; i++) {
    const x0 = -w - 0.2 + i * sw;
    box(b, x0, 2.3, -d - 0.2, x0 + sw, 2.45, d + 0.3, ROOF[i % 2]);
  }
  // Sign: a coin on a board above the roof.
  box(b, -0.55, 2.45, 0.05, 0.55, 2.95, 0.15, WOOD_DARK);
  coin(b, 0, 2.7, 0.16, 0.18);
  // Sacks of coins at the side.
  lump(b, w + 0.35, 0, 0.1, 0.25, 0.25, 0.25, 0xc8a670);
  lump(b, w + 0.3, 0.42, 0.1, 0.08, 0.06, 0.08, GOLD);
}

/** A flat coin facing +z, centred on (x, y, z). */
function coin(b: MeshBuilder, x: number, y: number, z: number, r: number): void {
  const n = 10;
  for (let i = 0; i < n; i++) {
    const a0 = (i / n) * Math.PI * 2, a1 = ((i + 1) / n) * Math.PI * 2;
    b.tri([x, y, z], [x + Math.cos(a0) * r, y + Math.sin(a0) * r, z], [x + Math.cos(a1) * r, y + Math.sin(a1) * r, z], [0, 0, 1], GOLD);
  }
}

/** The grill: a brick base about 1.6 m wide with glowing coals under an iron grate, a chimney and a log pile. */
function grill(b: MeshBuilder): void {
  const w = 0.8, d = 0.5;
  box(b, -w, 0, -d, w, 0.8, d, BRICK);
  box(b, -w - 0.05, 0.8, -d - 0.05, w + 0.05, 0.86, d + 0.05, STONE[1]);
  box(b, -w + 0.1, 0.86, -d + 0.1, w - 0.1, 0.9, d - 0.1, COAL);
  const rnd = mulberry32(77);
  for (let i = 0; i < 9; i++) {
    lump(b, (rnd() * 2 - 1) * (w - 0.2), 0.88, (rnd() * 2 - 1) * (d - 0.2), 0.08, 0.04, 0.07, i % 3 ? EMBER : COAL);
  }
  for (let x = -w + 0.12; x <= w - 0.11; x += 0.13) box(b, x - 0.015, 1.0, -d + 0.08, x + 0.015, 1.03, d - 0.08, IRON);
  box(b, -w + 0.08, 0.95, -d + 0.08, w - 0.08, 1.0, -d + 0.12, IRON_LIGHT);
  box(b, -w + 0.08, 0.95, d - 0.12, w - 0.08, 1.0, d - 0.08, IRON_LIGHT);
  // Chimney at the back.
  box(b, w - 0.35, 0.86, -d, w, 2.0, -d + 0.3, BRICK);
  box(b, w - 0.4, 2.0, -d - 0.05, w + 0.05, 2.08, -d + 0.35, STONE[2]);
  // Log pile beside it.
  for (const [y, z] of [[0.12, -0.15], [0.12, 0.15], [0.33, 0]]) {
    limb(b, [-w - 0.75, y, z], [-w - 0.15, y, z], 0.12, 0.12, 6, BARK);
  }
}

function modelGeometry(build: (b: MeshBuilder) => void): THREE.BufferGeometry {
  const b = new MeshBuilder();
  build(b);
  return b.geometry();
}

const TREE_MODELS: Record<SkillTree['kind'], (b: MeshBuilder) => void> = { tree: plainTree, oak: oakTree, eucalyptus: eucalyptusTree };

export function skillTreeGeometry(kind: SkillTree['kind']): THREE.BufferGeometry {
  return modelGeometry(TREE_MODELS[kind]);
}
export function stumpGeometry(): THREE.BufferGeometry {
  return modelGeometry(stump);
}
export function bankBoothGeometry(): THREE.BufferGeometry {
  return modelGeometry(bankBooth);
}
export function grillGeometry(): THREE.BufferGeometry {
  return modelGeometry(grill);
}

/**
 * The pond from its outline (decimetres): water with a darker band along the edge, a muddy rim just
 * outside it, and stones scattered along the shore.
 */
export function pondGeometry(outline: number[]): THREE.BufferGeometry {
  const pts = polygonMetres(outline);
  let cx = 0, cz = 0;
  for (const [x, z] of pts) { cx += x; cz += z; }
  cx /= pts.length; cz /= pts.length;
  const toward = (p: [number, number], k: number): [number, number] => [cx + (p[0] - cx) * k, cz + (p[1] - cz) * k];
  const inner = pts.map((p) => toward(p, 1 - POND_EDGE));
  const up = [0, 1, 0];
  const b = new MeshBuilder();
  // Lighter open water: the inner outline, triangulated in case it isn't convex.
  const shape = inner.map(([x, z]) => new THREE.Vector2(x, z));
  for (const [i, j, k] of THREE.ShapeUtils.triangulateShape(shape, [])) {
    b.tri([inner[i][0], Y_WATER, inner[i][1]], [inner[j][0], Y_WATER, inner[j][1]], [inner[k][0], Y_WATER, inner[k][1]], up, WATER);
  }
  // Darker band between the inner outline and the shore, and a muddy rim a little outside it.
  const rim = pts.map(([x, z]): [number, number] => {
    const dx = x - cx, dz = z - cz, l = Math.hypot(dx, dz) || 1;
    return [x + (dx / l) * 0.35, z + (dz / l) * 0.35];
  });
  const ring = (a: [number, number][], c: [number, number][], y: number, color: number) => {
    for (let i = 0; i < a.length; i++) {
      const j = (i + 1) % a.length;
      b.tri([a[i][0], y, a[i][1]], [a[j][0], y, a[j][1]], [c[j][0], y, c[j][1]], up, color);
      b.tri([a[i][0], y, a[i][1]], [c[j][0], y, c[j][1]], [c[i][0], y, c[i][1]], up, color);
    }
  };
  ring(inner, pts, Y_WATER, WATER_EDGE);
  ring(pts, rim, Y_WATER - 0.005, MUD);
  // Stones along the shore, on alternate outline points.
  const rnd = mulberry32(399);
  for (let i = 0; i < rim.length; i += 2) {
    const [x, z] = rim[i];
    const s = 0.18 + rnd() * 0.18;
    blob(b, x, s * 0.4, z, s, s * 0.6, s * 0.9, STONE[i % STONE.length], 70 + i, 0.3);
  }
  return b.geometry();
}

function lambert(): THREE.MeshLambertMaterial {
  return new THREE.MeshLambertMaterial({ vertexColors: true });
}

/**
 * Milestone 9 skill objects from skills.json: woodcutting trees (one instanced mesh per kind, plus one for
 * stumps of felled trees), the bank booth, the grill and the pond.
 */
export class SkillObjects {
  readonly group = new THREE.Group();
  readonly trees: Record<SkillTree['kind'], THREE.InstancedMesh>;
  readonly stumps: THREE.InstancedMesh;
  readonly bank: THREE.Mesh;
  readonly grill: THREE.Mesh;
  readonly pond: THREE.Mesh;
  private readonly facing: number[];
  /** Per tree: felled (drawn as a stump), and inside the camera's view as of the last cull. */
  private readonly felled: boolean[];
  private readonly visible: boolean[];
  private readonly spheres: THREE.Sphere[];
  private readonly m = new THREE.Matrix4();
  private readonly frustum = new THREE.Frustum();
  private readonly viewProj = new THREE.Matrix4();

  constructor(private readonly data: SkillsData) {
    this.group.name = 'skill-objects';
    const n = data.trees.length;
    const inst = (name: string, geo: THREE.BufferGeometry) => {
      const mesh = new THREE.InstancedMesh(geo, lambert(), Math.max(n, 1));
      mesh.name = name;
      mesh.castShadow = true;
      mesh.receiveShadow = true;
      mesh.matrixAutoUpdate = false;
      // Culled per tree in cull(); the mesh as a whole spans the park.
      mesh.frustumCulled = false;
      mesh.count = 0;
      return mesh;
    };
    this.trees = {
      tree: inst('skill-tree', skillTreeGeometry('tree')),
      oak: inst('skill-oak', skillTreeGeometry('oak')),
      eucalyptus: inst('skill-eucalyptus', skillTreeGeometry('eucalyptus')),
    };
    this.stumps = inst('skill-stump', stumpGeometry());
    const rnd = mulberry32(FACING_SEED);
    this.facing = data.trees.map(() => rnd() * Math.PI * 2);
    this.felled = data.trees.map(() => false);
    this.visible = data.trees.map(() => true);
    this.spheres = data.trees.map((t) => new THREE.Sphere(new THREE.Vector3(t.x * DM, CULL_Y, t.z * DM), CULL_RADIUS));

    const single = (name: string, geo: THREE.BufferGeometry, x: number, z: number, mat: THREE.Material = lambert()) => {
      const mesh = new THREE.Mesh(geo, mat);
      mesh.name = name;
      mesh.position.set(x, 0, z);
      mesh.castShadow = true;
      mesh.receiveShadow = true;
      mesh.updateMatrix();
      mesh.matrixAutoUpdate = false;
      return mesh;
    };
    this.bank = single('bank-booth', bankBoothGeometry(), data.bank.x * DM, data.bank.z * DM);
    this.grill = single('grill', grillGeometry(), data.grill.x * DM, data.grill.z * DM);
    this.pond = single('pond', pondGeometry(data.pond.p), 0, 0,
      new THREE.MeshPhongMaterial({ vertexColors: true, shininess: 60, specular: 0x335566, polygonOffset: true, polygonOffsetFactor: -1 }));
    this.pond.castShadow = false;

    this.group.add(this.trees.tree, this.trees.oak, this.trees.eucalyptus, this.stumps, this.bank, this.grill, this.pond);
    this.redraw();
  }

  /** Draw the trees whose ids are in `felled` as stumps and the rest as standing trees. Cheap when nothing changed. */
  update(felled?: ReadonlySet<string>): void {
    let changed = false;
    this.data.trees.forEach((t, i) => {
      const f = felled?.has(t.id) ?? false;
      if (f !== this.felled[i]) {
        this.felled[i] = f;
        changed = true;
      }
    });
    if (changed) this.redraw();
  }

  /**
   * Draw only the trees near the camera's view, so a tree mesh with none in view costs no draw call.
   * The test sphere is generous, so trees just off screen still cast their shadows into it.
   */
  cull(camera: THREE.Camera): void {
    camera.updateMatrixWorld();
    this.frustum.setFromProjectionMatrix(this.viewProj.multiplyMatrices(camera.projectionMatrix, camera.matrixWorldInverse));
    let changed = false;
    this.spheres.forEach((s, i) => {
      const v = this.frustum.intersectsSphere(s);
      if (v !== this.visible[i]) {
        this.visible[i] = v;
        changed = true;
      }
    });
    if (changed) this.redraw();
  }

  private redraw(): void {
    const meshes = [this.trees.tree, this.trees.oak, this.trees.eucalyptus, this.stumps];
    for (const mesh of meshes) mesh.count = 0;
    this.data.trees.forEach((t, i) => {
      if (!this.visible[i]) return;
      const mesh = this.felled[i] ? this.stumps : this.trees[t.kind];
      this.m.makeRotationY(this.facing[i]).setPosition(t.x * DM, 0, t.z * DM);
      mesh.setMatrixAt(mesh.count++, this.m);
    });
    for (const mesh of meshes) {
      mesh.instanceMatrix.needsUpdate = true;
      mesh.computeBoundingSphere();
    }
  }

  dispose(): void {
    for (const mesh of [this.trees.tree, this.trees.oak, this.trees.eucalyptus, this.stumps, this.bank, this.grill, this.pond]) {
      mesh.geometry.dispose();
      (mesh.material as THREE.Material).dispose();
    }
  }
}

let cached: SkillsData | null = null;
/** The committed skills.json, parsed once. */
export function loadSkills(): SkillsData {
  return (cached ??= parseSkills(skillsRaw));
}
