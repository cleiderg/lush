import * as THREE from 'three';
import { describe, expect, it } from 'vitest';
import {
  CAMERA_DISTANCE,
  CAMERA_ZOOM_MAX,
  CAMERA_ZOOM_MIN,
  CameraRig,
  DEFAULT_PITCH,
  PORTRAIT_ASPECT,
} from './cameraRig';

/** Minimal stand-in for the canvas: records listeners so tests can fire events at them. */
function fakeCanvas() {
  const listeners = new Map<string, ((e: unknown) => void)[]>();
  const el = {
    addEventListener: (type: string, fn: (e: unknown) => void) => {
      listeners.set(type, [...(listeners.get(type) ?? []), fn]);
    },
    removeEventListener: (type: string, fn: (e: unknown) => void) => {
      listeners.set(type, (listeners.get(type) ?? []).filter((f) => f !== fn));
    },
  };
  const fire = (type: string, e: object) => {
    for (const fn of listeners.get(type) ?? []) fn({ preventDefault: () => {}, ...e });
  };
  const pointer = (type: string, pointerId: number, clientX: number, clientY: number) =>
    fire(type, { pointerId, clientX, clientY });
  return { el: el as unknown as HTMLElement, fire, pointer, listeners };
}

/** Width of ground (y = 0) visible across the screen at the height where the player stands. */
function groundWidthAtPlayer(rig: CameraRig): number {
  const player = new THREE.Vector3(0, 0, 0);
  rig.follow(player.x, player.z);
  rig.camera.updateMatrixWorld();
  const ndcY = player.clone().project(rig.camera).y;
  const ground = new THREE.Plane(new THREE.Vector3(0, 1, 0), 0);
  const ray = new THREE.Raycaster();
  const hit = (ndcX: number) => {
    ray.setFromCamera(new THREE.Vector2(ndcX, ndcY), rig.camera);
    return ray.ray.intersectPlane(ground, new THREE.Vector3())!;
  };
  return hit(-1).distanceTo(hit(1));
}

describe('camera framing', () => {
  it('defaults to 55° above the horizon', () => {
    expect(THREE.MathUtils.radToDeg(DEFAULT_PITCH)).toBeGreaterThanOrEqual(54);
    expect(THREE.MathUtils.radToDeg(DEFAULT_PITCH)).toBeLessThanOrEqual(56);
    expect(new CameraRig().pitch).toBe(DEFAULT_PITCH);
  });

  it('frames about 20 m of ground across the player on a portrait phone', () => {
    const rig = new CameraRig(PORTRAIT_ASPECT);
    expect(rig.zoom).toBe(1);
    const width = groundWidthAtPlayer(rig);
    expect(width).toBeGreaterThanOrEqual(18);
    expect(width).toBeLessThanOrEqual(22);
  });
});

describe('camera zoom', () => {
  it('wheel down zooms out and wheel up zooms in', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.fire('wheel', { deltaY: 100, deltaMode: 0 });
    expect(rig.distance).toBeGreaterThan(CAMERA_DISTANCE);
    c.fire('wheel', { deltaY: -300, deltaMode: 0 });
    expect(rig.distance).toBeLessThan(CAMERA_DISTANCE);
  });

  it('wheel zoom clamps to the near and far limits', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.fire('wheel', { deltaY: 1e5, deltaMode: 0 });
    expect(rig.distance).toBeCloseTo(CAMERA_DISTANCE * CAMERA_ZOOM_MAX);
    c.fire('wheel', { deltaY: -1e5, deltaMode: 0 });
    expect(rig.distance).toBeCloseTo(CAMERA_DISTANCE * CAMERA_ZOOM_MIN);
  });

  it('wheel prevents page scrolling', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    let prevented = false;
    c.fire('wheel', { deltaY: 10, deltaMode: 0, preventDefault: () => (prevented = true) });
    expect(prevented).toBe(true);
  });

  it('pinching out zooms in and pinching in zooms out, without rotating', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.pointer('pointerdown', 1, 100, 300);
    c.pointer('pointerdown', 2, 200, 300);
    c.pointer('pointermove', 2, 300, 300); // spread 100 -> 200
    expect(rig.zoom).toBeCloseTo(0.5);
    c.pointer('pointermove', 1, 200, 300); // spread 200 -> 100
    expect(rig.zoom).toBeCloseTo(1);
    c.pointer('pointermove', 2, 240, 300); // spread 100 -> 40
    expect(rig.zoom).toBeGreaterThan(1);
    expect(rig.yaw).toBe(0);
    expect(rig.pitch).toBe(DEFAULT_PITCH);
  });

  it('pinch zoom clamps to the near and far limits', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.pointer('pointerdown', 1, 0, 0);
    c.pointer('pointerdown', 2, 100, 0);
    c.pointer('pointermove', 2, 10000, 0);
    expect(rig.zoom).toBe(CAMERA_ZOOM_MIN);
    c.pointer('pointermove', 2, 1, 0);
    expect(rig.zoom).toBe(CAMERA_ZOOM_MAX);
  });

  it('moves the camera further from the player when zoomed out', () => {
    const rig = new CameraRig();
    rig.follow(0, 0);
    const near = rig.camera.position.length();
    rig.zoomBy(1.4);
    rig.follow(0, 0);
    expect(rig.camera.position.length()).toBeGreaterThan(near);
  });
});

describe('camera drag', () => {
  it('one pointer drag rotates yaw and pitch', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.pointer('pointerdown', 1, 100, 100);
    c.pointer('pointermove', 1, 150, 120);
    expect(rig.yaw).not.toBe(0);
    expect(rig.pitch).toBeGreaterThan(DEFAULT_PITCH);
    expect(rig.zoom).toBe(1);
  });

  it('a second pointer during a drag stops rotation', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    rig.attach(c.el);
    c.pointer('pointerdown', 1, 100, 100);
    c.pointer('pointermove', 1, 120, 100);
    const { yaw, pitch } = rig;
    c.pointer('pointerdown', 2, 300, 300);
    c.pointer('pointermove', 1, 200, 50);
    c.pointer('pointermove', 2, 350, 380);
    expect(rig.yaw).toBe(yaw);
    expect(rig.pitch).toBe(pitch);
    // Lifting the second finger lets the first resume rotating without a jump.
    c.pointer('pointerup', 2, 350, 380);
    c.pointer('pointermove', 1, 210, 50);
    expect(rig.yaw).toBeCloseTo(yaw - 10 * 0.008);
  });

  it('detach removes every listener', () => {
    const rig = new CameraRig();
    const c = fakeCanvas();
    const detach = rig.attach(c.el);
    detach();
    for (const fns of c.listeners.values()) expect(fns).toHaveLength(0);
  });
});
