import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { MARKER_LIFT, type NpcMarker } from './npcMarkers';
import { NpcType, npcHeight } from './npcs';
import { WorldScene, countDrawCalls } from './scene';
import type { NpcView, WorldView } from './worldView';

const librarian: NpcView = { id: 20, npcType: NpcType.Librarian, x: 4, z: 6, heading: 0 };
const ranger: NpcView = { id: 21, npcType: NpcType.ParkRanger, x: 8, z: 2, heading: 0 };
const base: WorldView = { local: { id: 1, x: 0, z: 0, heading: 0 }, remotes: [], npcs: [librarian, ranger] };

function counts(w: WorldScene): { start: number; finish: number } {
  const count = (name: string) => (w.scene.getObjectByName(name) as THREE.InstancedMesh | undefined)?.count ?? 0;
  return { start: count('npcMarker-start'), finish: count('npcMarker-finish') };
}

describe('npc quest markers (AC3)', () => {
  it('adds nothing until an NPC has a marker', () => {
    const w = new WorldScene(() => 0);
    w.update(base);
    w.update({ ...base, npcMarkers: new Map() });
    expect(w.scene.getObjectByName('npcMarkers')).toBeUndefined();
  });

  it('shows, changes and removes a marker as the input changes', () => {
    const w = new WorldScene(() => 0);
    const markers = (m: [number, NpcMarker][]): WorldView => ({ ...base, npcMarkers: new Map(m) });
    w.update(markers([[20, 'start']]));
    expect(counts(w)).toEqual({ start: 1, finish: 0 });
    const mesh = w.scene.getObjectByName('npcMarker-start') as THREE.InstancedMesh;
    const m = new THREE.Matrix4();
    mesh.getMatrixAt(0, m);
    const p = new THREE.Vector3().setFromMatrixPosition(m);
    expect(p.x).toBe(4);
    expect(p.z).toBe(6);
    expect(p.y).toBeGreaterThan(npcHeight(NpcType.Librarian));
    expect(p.y).toBeLessThan(npcHeight(NpcType.Librarian) + MARKER_LIFT + 0.1);

    w.update(markers([[20, 'finish']]));
    expect(counts(w)).toEqual({ start: 0, finish: 1 });

    w.update(markers([[20, 'finish'], [21, 'start']]));
    expect(counts(w)).toEqual({ start: 1, finish: 1 });
    expect(countDrawCalls(w.scene.getObjectByName('npcMarkers')!)).toBe(2);

    w.update(markers([[21, 'start']]));
    expect(counts(w)).toEqual({ start: 1, finish: 0 });

    w.update(base);
    expect(counts(w)).toEqual({ start: 0, finish: 0 });
  });

  it('draws nothing for a marker whose NPC is out of view, and grows past its capacity', () => {
    const w = new WorldScene(() => 0);
    w.update({ ...base, npcMarkers: new Map([[999, 'start' as const]]) });
    expect(counts(w).start).toBe(0);
    const many = Array.from({ length: 30 }, (_, i) => ({ id: 100 + i, npcType: NpcType.Guide, x: i, z: 0, heading: 0 }));
    w.update({ ...base, npcs: many, npcMarkers: new Map(many.map((n) => [n.id, 'start' as const])) });
    expect(counts(w).start).toBe(30);
    expect(countDrawCalls(w.scene.getObjectByName('npcMarkers')!)).toBe(1);
  });
});
