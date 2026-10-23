import * as THREE from 'three';

export const CAMERA_DISTANCE = 9;
export const DEFAULT_PITCH = 0.9; // radians above horizon: high angle
export const MIN_PITCH = 0.6;
export const MAX_PITCH = 1.3;
const DRAG_SENSITIVITY = 0.008;
const TARGET_HEIGHT = 1.2;

/** Third-person orbit camera. `yaw` is the camera's position angle around the player. */
export class CameraRig {
  readonly camera: THREE.PerspectiveCamera;
  yaw = 0;
  pitch = DEFAULT_PITCH;

  constructor(aspect = 1) {
    this.camera = new THREE.PerspectiveCamera(55, aspect, 0.1, 500);
  }

  /** Apply a pointer drag in pixels. */
  drag(dx: number, dy: number): void {
    this.yaw -= dx * DRAG_SENSITIVITY;
    this.pitch = Math.min(MAX_PITCH, Math.max(MIN_PITCH, this.pitch + dy * DRAG_SENSITIVITY));
  }

  follow(x: number, z: number): void {
    const horizontal = Math.cos(this.pitch) * CAMERA_DISTANCE;
    this.camera.position.set(
      x - Math.sin(this.yaw) * horizontal,
      TARGET_HEIGHT + Math.sin(this.pitch) * CAMERA_DISTANCE,
      z - Math.cos(this.yaw) * horizontal,
    );
    this.camera.lookAt(x, TARGET_HEIGHT, z);
  }

  /** Listen for drag gestures on the canvas. Returns a function that removes the listeners. */
  attach(el: HTMLElement): () => void {
    let last: { x: number; y: number; id: number } | null = null;
    const down = (e: PointerEvent) => {
      last = { x: e.clientX, y: e.clientY, id: e.pointerId };
      el.setPointerCapture?.(e.pointerId);
    };
    const move = (e: PointerEvent) => {
      if (!last || last.id !== e.pointerId) return;
      this.drag(e.clientX - last.x, e.clientY - last.y);
      last.x = e.clientX;
      last.y = e.clientY;
    };
    const up = (e: PointerEvent) => {
      if (last && last.id === e.pointerId) last = null;
    };
    el.addEventListener('pointerdown', down);
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
    return () => {
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
    };
  }
}
