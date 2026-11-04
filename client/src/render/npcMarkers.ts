import * as THREE from 'three';
import { mergeGeometries } from 'three/examples/jsm/utils/BufferGeometryUtils.js';
import { npcHeight } from './npcs';
import type { NpcView } from './worldView';

/** What a talking NPC offers: a quest to start (!) or one to finish (?). */
export type NpcMarker = 'start' | 'finish';

/** Gap between the top of an NPC's head and the marker's centre, in metres. */
export const MARKER_LIFT = 0.45;
/** Marker height, in metres. */
export const MARKER_SIZE = 0.4;
/** Peak of the slow hover, in metres. */
export const MARKER_BOB = 0.05;
const INITIAL_CAPACITY = 8;

type Rect = readonly [x: number, y: number, w: number, h: number];

/** Glyph strokes in a unit-high box centred on the origin (x right, y up). */
const BANG: Rect[] = [
  [0, 0.12, 0.2, 0.62],
  [0, -0.38, 0.2, 0.2],
];
const QUESTION: Rect[] = [
  [0, 0.4, 0.5, 0.18],
  [0.2, 0.2, 0.18, 0.3],
  [0.05, 0, 0.3, 0.16],
  [0, -0.14, 0.18, 0.22],
  [0, -0.4, 0.18, 0.18],
];

function glyph(rects: readonly Rect[]): THREE.BufferGeometry {
  const parts = rects.map(([x, y, w, h]) => new THREE.BoxGeometry(w, h, 0.1).translate(x, y, 0).toNonIndexed());
  const g = mergeGeometries(parts)!;
  for (const p of parts) p.dispose();
  g.deleteAttribute('uv');
  return g;
}

/**
 * Quest markers over NPCs: one InstancedMesh for "!" and one for "?", flat yellow, turned to face the camera
 * and hovering slowly. Each costs one draw call, however many NPCs show it.
 */
export class NpcMarkers {
  readonly group = new THREE.Group();
  private readonly meshes: Record<NpcMarker, THREE.InstancedMesh>;
  private readonly m = new THREE.Matrix4();
  private readonly pos = new THREE.Vector3();
  private readonly scale = new THREE.Vector3(MARKER_SIZE, MARKER_SIZE, MARKER_SIZE);

  constructor() {
    this.group.name = 'npcMarkers';
    const make = (kind: NpcMarker, rects: readonly Rect[], color: number) => {
      const mesh = new THREE.InstancedMesh(glyph(rects), new THREE.MeshBasicMaterial({ color }), INITIAL_CAPACITY);
      mesh.name = `npcMarker-${kind}`;
      mesh.count = 0;
      mesh.frustumCulled = false;
      this.group.add(mesh);
      return mesh;
    };
    this.meshes = { start: make('start', BANG, 0xf2d21c), finish: make('finish', QUESTION, 0xf2d21c) };
  }

  /** The InstancedMesh for a marker kind. */
  meshFor(kind: NpcMarker): THREE.InstancedMesh {
    return this.meshes[kind];
  }

  /**
   * Draws a marker over each NPC in `npcs` that has an entry in `markers` (by entity id); `facing` is the
   * camera's world rotation and `time` the render clock in seconds. NPCs not in view draw nothing.
   */
  update(npcs: readonly NpcView[], markers: ReadonlyMap<number, NpcMarker> | undefined, facing: THREE.Quaternion, time = 0): void {
    const counts: Record<NpcMarker, number> = { start: 0, finish: 0 };
    if (markers && markers.size > 0) {
      for (const n of npcs) {
        const kind = markers.get(n.id);
        if (!kind) continue;
        const mesh = this.ensure(kind, counts[kind] + 1);
        const bob = MARKER_BOB * Math.sin(time * 2.5 + n.id);
        this.pos.set(n.x, npcHeight(n.npcType) + MARKER_LIFT + bob, n.z);
        this.m.compose(this.pos, facing, this.scale);
        mesh.setMatrixAt(counts[kind]++, this.m);
      }
    }
    for (const kind of ['start', 'finish'] as const) {
      const mesh = this.meshes[kind];
      mesh.count = counts[kind];
      mesh.instanceMatrix.needsUpdate = true;
    }
  }

  private ensure(kind: NpcMarker, needed: number): THREE.InstancedMesh {
    let mesh = this.meshes[kind];
    if (mesh.instanceMatrix.count >= needed) return mesh;
    let cap = mesh.instanceMatrix.count;
    while (cap < needed) cap *= 2;
    const grown = new THREE.InstancedMesh(mesh.geometry, mesh.material, cap);
    grown.name = mesh.name;
    grown.count = 0;
    grown.frustumCulled = false;
    this.group.remove(mesh);
    mesh.dispose();
    this.group.add(grown);
    this.meshes[kind] = mesh = grown;
    return mesh;
  }
}
