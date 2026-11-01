import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import { CAFE_DOOR } from './cafeInterior';
import { loadCampus } from './campusMap';
import { CUTAWAY_FADE_MS, CafeZone, Cutaway, crosses } from './cutaway';
import { STUDENT_CENTER } from './landmarks';
import { BUILDING_BUDGET } from './modelBudget';
import { WorldScene } from './scene';

const INSIDE = { x: 45, z: -351 };
const OUTSIDE = { x: 45, z: -330 };

function rig() {
  let t = 0;
  const scene = new WorldScene(() => t);
  const cut = scene.map.cutaway!;
  const go = (p: { x: number; z: number }, ms = 0, yaw?: number) => {
    t += ms;
    if (yaw !== undefined) scene.rig.yaw = yaw;
    scene.update({ local: { id: 1, x: p.x, z: p.z, heading: 0 }, remotes: [] });
  };
  /** Draw calls: visible meshes that have something to draw. */
  const drawCalls = () => {
    let n = 0;
    scene.scene.traverseVisible((o) => {
      const g = (o as THREE.Mesh).geometry as THREE.BufferGeometry | undefined;
      if ((o as THREE.Mesh).isMesh && (g?.getAttribute('position')?.count ?? 0) > 0) n++;
    });
    return n;
  };
  return { scene, cut, go, drawCalls };
}

describe('Student Center cut-away roof (AC1)', () => {
  it('exists for the Student Center', () => {
    expect(rig().cut).toBeInstanceOf(Cutaway);
  }, 60_000);

  it('hides the roof with the player inside the café polygon and shows it outside', () => {
    const { cut, go } = rig();
    go(OUTSIDE);
    expect(cut.roof).toBe(1);
    go(INSIDE);
    go(INSIDE, CUTAWAY_FADE_MS);
    expect(cut.roof).toBe(0);
    go(OUTSIDE, 0);
    go(OUTSIDE, CUTAWAY_FADE_MS);
    expect(cut.roof).toBe(1);
  });
});

describe('Student Center cut-away walls (AC2)', () => {
  let crossed = 0;
  it.each([0, 0.8, 1.6, 2.4, 3.2, 4.0, 4.8, 5.6])('fades exactly the segments between camera and player at yaw %f', (yaw) => {
    const { scene, cut, go } = rig();
    go(INSIDE, 0, yaw);
    go(INSIDE, CUTAWAY_FADE_MS, yaw);
    const cam = scene.rig.camera.position;
    let faded = 0, solid = 0;
    cut.segments.forEach((s, i) => {
      if (crosses(s, cam.x, cam.z, INSIDE.x, INSIDE.z)) {
        expect(cut.wall(i)).toBe(0);
        faded++;
      } else {
        expect(cut.wall(i)).toBe(1);
        solid++;
      }
    });
    expect(solid).toBeGreaterThan(0);
    crossed += faded;
  });

  it('fades some wall at most rig angles', () => {
    expect(crossed).toBeGreaterThanOrEqual(6);
  });

  it('leaves walls opaque while the player is outside', () => {
    const { cut, go } = rig();
    go(OUTSIDE, 0, 1);
    go(OUTSIDE, CUTAWAY_FADE_MS, 1);
    expect(Array.from(cut.fade.alpha).every((a) => a === 1)).toBe(true);
  });

  it('tags every wall triangle with a segment and every roof triangle with 0', () => {
    const { cut } = rig();
    const cutAttr = cut.mesh.geometry.getAttribute('cut');
    const pos = cut.mesh.geometry.getAttribute('position');
    const top = loadCampus().buildings.find((b) => b.id === STUDENT_CENTER)!.h * 0.1 - 1;
    let roof = 0, wall = 0;
    for (let i = 0; i < pos.count; i++) {
      if (cutAttr.getX(i) === 0) { roof++; expect(pos.getY(i)).toBeGreaterThanOrEqual(top - 0.02); } else wall++;
    }
    expect(roof).toBeGreaterThan(0);
    expect(wall).toBeGreaterThan(0);
  });
});

describe('Student Center cut-away timing (AC3)', () => {
  it('takes CUTAWAY_FADE_MS to fade out and to fade back in', () => {
    const { cut, go } = rig();
    go(INSIDE);
    go(INSIDE, CUTAWAY_FADE_MS / 2);
    expect(cut.roof).toBeCloseTo(0.5, 5);
    go(INSIDE, CUTAWAY_FADE_MS / 2 - 1);
    expect(cut.roof).toBeGreaterThan(0);
    go(INSIDE, 1);
    expect(cut.roof).toBe(0);
    go(OUTSIDE);
    go(OUTSIDE, CUTAWAY_FADE_MS / 2);
    expect(cut.roof).toBeCloseTo(0.5, 5);
    go(OUTSIDE, CUTAWAY_FADE_MS / 2);
    expect(cut.roof).toBe(1);
  });

  it('keeps its state on the door cells, entering or leaving', () => {
    const zone = new CafeZone();
    const [dx0, dz0, dx1] = CAFE_DOOR;
    const door = { x: (dx0 + dx1) / 2, z: dz0 + 0.5 };
    expect(zone.update(door.x, door.z)).toBe(false);
    expect(zone.update(INSIDE.x, INSIDE.z)).toBe(true);
    expect(zone.update(door.x, door.z)).toBe(true);
    expect(zone.update(door.x, door.z + 3)).toBe(false);
    expect(zone.update(door.x, door.z)).toBe(false);
  });

  it('does not change target every frame when stepping back and forth over the door cells', () => {
    const zone = new CafeZone();
    const [dx0, dz0, dx1, dz1] = CAFE_DOOR;
    const x = (dx0 + dx1) / 2;
    zone.update(INSIDE.x, INSIDE.z);
    let flips = 0, last = zone.inside;
    for (let i = 0; i < 200; i++) {
      // Oscillate between the floor's edge and the far side of the door cells.
      const z = i % 2 ? dz0 - 0.1 : dz1 - 0.05;
      const now = zone.update(x, z);
      if (now !== last) flips++;
      last = now;
    }
    expect(flips).toBe(0);
    // Standing on the door cells from outside, over many frames, never turns it on.
    const outside = new CafeZone();
    for (let i = 0; i < 100; i++) expect(outside.update(x, dz0 + 0.5)).toBe(false);
  });

  it('changes alpha smoothly, never by more than one frame of fade', () => {
    const { cut, go } = rig();
    go(INSIDE);
    let prev = cut.roof;
    for (let i = 0; i < 30; i++) {
      go(INSIDE, 16);
      expect(prev - cut.roof).toBeLessThanOrEqual(16 / CUTAWAY_FADE_MS + 1e-6);
      prev = cut.roof;
    }
  });
});

describe('Student Center cut-away cost (AC4)', () => {
  it('adds no draw calls while the player is outside, and one while it is cut away', () => {
    const { cut, go, drawCalls } = rig();
    go(OUTSIDE);
    const base = drawCalls();
    expect(cut.mesh.visible).toBe(false);
    go(INSIDE);
    go(INSIDE, CUTAWAY_FADE_MS);
    expect(cut.mesh.visible).toBe(true);
    expect(drawCalls()).toBeLessThanOrEqual(base + 1);
    go(OUTSIDE);
    go(OUTSIDE, CUTAWAY_FADE_MS);
    expect(cut.mesh.visible).toBe(false);
    expect(drawCalls()).toBe(base);
  });

  it('keeps the Student Center within BUILDING_BUDGET.maxTriangles', () => {
    const { cut } = rig();
    expect(cut.mesh.geometry.getAttribute('position').count / 3).toBeLessThanOrEqual(BUILDING_BUDGET.maxTriangles);
  });
});
