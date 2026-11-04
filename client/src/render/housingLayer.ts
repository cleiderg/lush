import * as THREE from 'three';
import housingRaw from '../../../shared/map/housing.json?raw';
import { loadCampus, MeshBuilder } from './campusMap';
import { box } from './gameAreas';
import { type Building, type HousingData, parseHousing } from './mapData';

const DM = 0.1;
/** How far the sign stands beside the door, along the wall. Under the 1 m the door-point test allows. */
/** Metres from the nearest door or stall within which the layer is drawn (past the far plane of the camera). */
const VISIBLE_RADIUS = 120;
const SIGN_OFFSET = 0.65;
const DOOR_RED = 0xb8402f;
const DOOR_TRIM = 0xf2e6c8;
const WOOD = 0x8a5a34;
const WOOD_DARK = 0x6b4426;
const BOARD = 0xe8d7a3;
const CLOTH = 0x2f6fa8;
const METAL = 0x8a9097;

/** A door marker: a red slab with a pale frame and a step, facing +z, 0.9 m wide and 2 m tall. */
export function doorGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  box(b, -0.55, 0, -0.1, 0.55, 0.08, 0.35, DOOR_TRIM);
  box(b, -0.5, 0.08, -0.05, 0.5, 2.1, 0.05, DOOR_TRIM);
  box(b, -0.4, 0.08, 0.05, 0.4, 2.0, 0.09, DOOR_RED);
  return b.geometry();
}

/** An auction sign: a post with a board, facing +z, about 1.5 m tall. */
export function signGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  box(b, -0.05, 0, -0.05, 0.05, 1.5, 0.05, WOOD_DARK);
  box(b, -0.35, 0.85, 0.05, 0.35, 1.45, 0.1, BOARD);
  box(b, -0.35, 0.85, 0.1, 0.35, 0.9, 0.12, WOOD);
  return b.geometry();
}

/** Campus Thrift stall: a counter under a striped awning on four posts, facing +z, 2 m wide. */
export function stallGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  box(b, -1, 0, -0.4, 1, 0.9, 0.4, WOOD);
  box(b, -1.05, 0.9, -0.45, 1.05, 0.95, 0.45, WOOD_DARK);
  for (const x of [-1, 0.94]) for (const z of [-0.45, 0.39]) box(b, x, 0.95, z, x + 0.06, 2.1, z + 0.06, WOOD_DARK);
  for (let i = 0; i < 4; i++) {
    box(b, -1.1 + i * 0.55, 2.1, -0.55, -0.55 + i * 0.55, 2.2, 0.55, i % 2 ? DOOR_TRIM : CLOTH);
  }
  box(b, -0.7, 0.95, -0.2, -0.3, 1.15, 0.1, DOOR_RED);
  box(b, 0.2, 0.95, -0.2, 0.7, 1.1, 0.1, BOARD);
  return b.geometry();
}

/** A workbench: a thick top on trestle legs with a vice and a plank, 1.6 m wide, facing +z. */
export function workbenchGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  box(b, -0.8, 0.85, -0.4, 0.8, 0.95, 0.4, WOOD);
  for (const x of [-0.75, 0.65]) for (const z of [-0.35, 0.25]) box(b, x, 0, z, x + 0.1, 0.85, z + 0.1, WOOD_DARK);
  box(b, -0.7, 0.3, -0.3, 0.7, 0.35, 0.3, WOOD_DARK);
  box(b, 0.5, 0.95, -0.3, 0.7, 1.1, -0.1, METAL);
  box(b, -0.5, 0.95, -0.1, 0.1, 1.0, 0.2, DOOR_TRIM);
  return b.geometry();
}

function instanced(geo: THREE.BufferGeometry, name: string, count: number): THREE.InstancedMesh {
  const m = new THREE.InstancedMesh(geo, new THREE.MeshLambertMaterial({ vertexColors: true }), count);
  m.name = name;
  m.castShadow = true;
  m.receiveShadow = true;
  m.matrixAutoUpdate = false;
  return m;
}

/** Yaw (about +y) that turns a +z-facing model to look away from the nearest wall of `p`, seen from (x, z). */
function yawAwayFromWall(x: number, z: number, p: number[]): number {
  let best = Infinity, ox = 0, oz = 1;
  for (let i = 0; i < p.length; i += 2) {
    const ax = p[i], az = p[i + 1], bx = p[(i + 2) % p.length], bz = p[(i + 3) % p.length];
    const dx = bx - ax, dz = bz - az;
    const t = Math.max(0, Math.min(1, ((x - ax) * dx + (z - az) * dz) / (dx * dx + dz * dz || 1)));
    const cx = ax + t * dx, cz = az + t * dz;
    const d = Math.hypot(x - cx, z - cz);
    if (d < best && d > 0) {
      best = d;
      ox = (x - cx) / d;
      oz = (z - cz) / d;
    }
  }
  return Math.atan2(ox, oz);
}

/**
 * What players walk up to in Mesa Court and Middle Earth, from housing.json: a door marker and an
 * auction sign at each house (plot), and a Campus Thrift stall and a workbench per area. Doors and
 * signs are one instanced mesh each, the stalls and workbenches one each: four draw calls in all.
 * Visual only; no collision.
 */
export class HousingLayer {
  readonly group = new THREE.Group();
  readonly doors: THREE.InstancedMesh;
  readonly signs: THREE.InstancedMesh;
  readonly stalls: THREE.InstancedMesh;
  readonly workbenches: THREE.InstancedMesh;
  /** Where to measure the distance to each area, in metres: its stall and workbench points and every door. */
  private readonly areaPoints: [number, number][];

  constructor(data: HousingData, buildings: readonly Building[] = []) {
    this.group.name = 'housing';
    this.areaPoints = [...data.houses, ...data.areas.flatMap((a) => [a.thrift, a.workbench])].map((p) => [p.x * DM, p.z * DM]);
    const byId = new Map(buildings.map((b) => [b.id, b]));
    const n = data.houses.length, a = data.areas.length;
    this.doors = instanced(doorGeometry(), 'house-door', n);
    this.signs = instanced(signGeometry(), 'house-sign', n);
    this.stalls = instanced(stallGeometry(), 'thrift-stall', a);
    this.workbenches = instanced(workbenchGeometry(), 'workbench', a);

    const m = new THREE.Matrix4(), q = new THREE.Quaternion(), s = new THREE.Vector3(1, 1, 1);
    const up = new THREE.Vector3(0, 1, 0), pos = new THREE.Vector3();
    data.houses.forEach((h, i) => {
      const wall = byId.get(h.osm);
      const yaw = wall ? yawAwayFromWall(h.x, h.z, wall.p) : 0;
      q.setFromAxisAngle(up, yaw);
      this.doors.setMatrixAt(i, m.compose(pos.set(h.x * DM, 0, h.z * DM), q, s));
      // Beside the door along the wall: the board's right-hand side as seen from outside.
      pos.set(h.x * DM + Math.cos(yaw) * SIGN_OFFSET, 0, h.z * DM - Math.sin(yaw) * SIGN_OFFSET);
      this.signs.setMatrixAt(i, m.compose(pos, q, s));
    });
    // Stalls face the area's workbench side by side; the workbench faces the same way.
    data.areas.forEach((area, i) => {
      const yaw = Math.atan2(area.thrift.x - area.workbench.x, area.thrift.z - area.workbench.z);
      q.setFromAxisAngle(up, yaw);
      this.stalls.setMatrixAt(i, m.compose(pos.set(area.thrift.x * DM, 0, area.thrift.z * DM), q, s));
      this.workbenches.setMatrixAt(i, m.compose(pos.set(area.workbench.x * DM, 0, area.workbench.z * DM), q, s));
    });
    this.group.visible = false;
    for (const mesh of [this.doors, this.signs, this.stalls, this.workbenches]) {
      mesh.instanceMatrix.needsUpdate = true;
      mesh.computeBoundingSphere();
      this.group.add(mesh);
    }
  }

  /** Show the layer only near a housing area: the two areas are far apart, so one instanced mesh spans the whole map. */
  update(x: number, z: number): void {
    this.group.visible = this.near(x, z);
  }

  private near(x: number, z: number): boolean {
    return this.areaPoints.some(([ax, az]) => Math.hypot(x - ax, z - az) < VISIBLE_RADIUS);
  }

  dispose(): void {
    for (const m of [this.doors, this.signs, this.stalls, this.workbenches]) {
      m.geometry.dispose();
      (m.material as THREE.Material).dispose();
    }
  }
}

let cached: HousingData | null = null;
/** The committed housing.json, parsed once. */
export function loadHousing(): HousingData {
  return (cached ??= parseHousing(housingRaw));
}

/** The housing layer for the committed map. */
export function createHousingLayer(): HousingLayer {
  return new HousingLayer(loadHousing(), loadCampus().buildings);
}
