import * as THREE from 'three';
import cardTablesRaw from '../../../shared/map/cardtables.json?raw';
import { MeshBuilder } from './campusMap';
import { box, polygonMetres } from './gameAreas';
import { parseCardTables, type CardTablesData } from './mapData';

const DM = 0.1;
/** Above the campus paths, below the PvP border. */
const Y_PLAZA = 0.065;
const PAVING = 0xc9bfa8;
const FELT = 0x2f7d4a;
const WOOD = 0x8a5a34;
const WOOD_DARK = 0x6b4426;

/**
 * A card table about 1.4 m square and 0.8 m tall with a green top, and a stool on each side,
 * centred on the origin. Fits inside a 3 m square.
 */
export function cardTableGeometry(): THREE.BufferGeometry {
  const b = new MeshBuilder();
  const h = 0.7, top = 0.8;
  box(b, -h, 0.72, -h, h, top, h, WOOD_DARK);
  box(b, -h + 0.08, top, -h + 0.08, h - 0.08, top + 0.01, h - 0.08, FELT);
  for (const [x, z] of [[-h, -h], [h - 0.1, -h], [-h, h - 0.1], [h - 0.1, h - 0.1]]) {
    box(b, x, 0, z, x + 0.1, 0.72, z + 0.1, WOOD);
  }
  // Stools, one on each side.
  const s = 0.22, d = 1.1;
  for (const [x, z] of [[0, -d], [0, d], [-d, 0], [d, 0]]) {
    box(b, x - s, 0, z - s, x + s, 0.45, z + s, WOOD);
  }
  return b.geometry();
}

/** Flat convex polygon (metres) at height y, as a triangle fan. */
function plazaGeometry(pts: [number, number][], y: number): THREE.BufferGeometry {
  const b = new MeshBuilder();
  for (let i = 1; i + 1 < pts.length; i++) {
    const [ax, az] = pts[0], [bx, bz] = pts[i], [cx, cz] = pts[i + 1];
    b.tri([ax, y, az], [bx, y, bz], [cx, y, cz], [0, 1, 0], PAVING);
  }
  return b.geometry();
}

/** The card table plaza paving and its tables (one instanced mesh), from cardtables.json. */
export class CardTables {
  readonly group = new THREE.Group();
  readonly plaza: THREE.Mesh;
  readonly tables: THREE.InstancedMesh;

  constructor(data: CardTablesData) {
    this.group.name = 'card-tables';
    this.plaza = new THREE.Mesh(
      plazaGeometry(polygonMetres(data.plaza), Y_PLAZA),
      new THREE.MeshLambertMaterial({ vertexColors: true, polygonOffset: true, polygonOffsetFactor: -1 }),
    );
    this.plaza.name = 'card-plaza';
    this.plaza.matrixAutoUpdate = false;
    this.plaza.receiveShadow = true;

    this.tables = new THREE.InstancedMesh(
      cardTableGeometry(),
      new THREE.MeshLambertMaterial({ vertexColors: true }),
      data.tables.length,
    );
    this.tables.name = 'card-table';
    this.tables.castShadow = true;
    this.tables.receiveShadow = true;
    const m = new THREE.Matrix4();
    data.tables.forEach((t, i) => this.tables.setMatrixAt(i, m.makeTranslation(t.x * DM, 0, t.z * DM)));
    this.tables.instanceMatrix.needsUpdate = true;
    this.tables.computeBoundingSphere();
    this.tables.matrixAutoUpdate = false;
    this.group.add(this.plaza, this.tables);
  }

  dispose(): void {
    for (const m of [this.plaza, this.tables]) {
      m.geometry.dispose();
      (m.material as THREE.Material).dispose();
    }
  }
}

let cached: CardTablesData | null = null;
/** The committed cardtables.json, parsed once. */
export function loadCardTables(): CardTablesData {
  return (cached ??= parseCardTables(cardTablesRaw));
}
