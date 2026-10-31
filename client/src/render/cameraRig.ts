import * as THREE from 'three';

export const FOV = 55; // vertical, degrees
export const DEFAULT_PITCH = THREE.MathUtils.degToRad(55); // above the horizon (visual-style: Camera)
export const MIN_PITCH = 0.6;
export const MAX_PITCH = 1.3;
/** Ground width framed across the player's position at default zoom on a portrait phone (visual-style: Camera). */
export const FRAMING_WIDTH = 20;
export const PORTRAIT_ASPECT = 390 / 844;
/** Distance that frames FRAMING_WIDTH across at PORTRAIT_ASPECT. */
export const CAMERA_DISTANCE =
  FRAMING_WIDTH / 2 / (Math.tan(THREE.MathUtils.degToRad(FOV) / 2) * PORTRAIT_ASPECT);
/** Zoom limits as multiples of CAMERA_DISTANCE (PRD 13, Q3 placeholders). */
export const CAMERA_ZOOM_MIN = 0.5;
export const CAMERA_ZOOM_MAX = 1.5;
const DRAG_SENSITIVITY = 0.008;
const WHEEL_SENSITIVITY = 0.001; // zoom factor e^(deltaY * this)
const TARGET_HEIGHT = 1.2;

/** Third-person orbit camera. `yaw` is the camera's position angle around the player. */
export class CameraRig {
  readonly camera: THREE.PerspectiveCamera;
  yaw = 0;
  pitch = DEFAULT_PITCH;
  /** Multiple of CAMERA_DISTANCE; 1 is the default framing. */
  zoom = 1;

  constructor(aspect = 1) {
    this.camera = new THREE.PerspectiveCamera(FOV, aspect, 0.1, 500);
  }

  get distance(): number {
    return CAMERA_DISTANCE * this.zoom;
  }

  /** Apply a pointer drag in pixels. */
  drag(dx: number, dy: number): void {
    this.yaw -= dx * DRAG_SENSITIVITY;
    this.pitch = Math.min(MAX_PITCH, Math.max(MIN_PITCH, this.pitch + dy * DRAG_SENSITIVITY));
  }

  /** Multiply the zoom by `factor` (>1 moves further away), clamped to the zoom limits. */
  zoomBy(factor: number): void {
    if (!(factor > 0)) return;
    this.zoom = Math.min(CAMERA_ZOOM_MAX, Math.max(CAMERA_ZOOM_MIN, this.zoom * factor));
  }

  follow(x: number, z: number): void {
    const d = this.distance;
    const horizontal = Math.cos(this.pitch) * d;
    this.camera.position.set(
      x - Math.sin(this.yaw) * horizontal,
      TARGET_HEIGHT + Math.sin(this.pitch) * d,
      z - Math.cos(this.yaw) * horizontal,
    );
    this.camera.lookAt(x, TARGET_HEIGHT, z);
  }

  /**
   * Listen for gestures on the canvas: one pointer drags to rotate, two pointers pinch to zoom,
   * and the mouse wheel zooms. Returns a function that removes the listeners.
   */
  attach(el: HTMLElement): () => void {
    const pointers = new Map<number, { x: number; y: number }>();
    const spread = () => {
      const [a, b] = [...pointers.values()];
      return Math.hypot(a.x - b.x, a.y - b.y);
    };
    const down = (e: PointerEvent) => {
      pointers.set(e.pointerId, { x: e.clientX, y: e.clientY });
      el.setPointerCapture?.(e.pointerId);
    };
    const move = (e: PointerEvent) => {
      const p = pointers.get(e.pointerId);
      if (!p) return;
      if (pointers.size === 1) {
        this.drag(e.clientX - p.x, e.clientY - p.y);
      } else if (pointers.size === 2) {
        const before = spread();
        p.x = e.clientX;
        p.y = e.clientY;
        const after = spread();
        if (before > 0 && after > 0) this.zoomBy(before / after);
      }
      p.x = e.clientX;
      p.y = e.clientY;
    };
    const up = (e: PointerEvent) => {
      pointers.delete(e.pointerId);
    };
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      // Normalise line/page deltas to roughly pixels.
      const scale = e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? 400 : 1;
      this.zoomBy(Math.exp(e.deltaY * scale * WHEEL_SENSITIVITY));
    };
    el.addEventListener('pointerdown', down);
    el.addEventListener('pointermove', move);
    el.addEventListener('pointerup', up);
    el.addEventListener('pointercancel', up);
    el.addEventListener('wheel', wheel, { passive: false });
    return () => {
      el.removeEventListener('pointerdown', down);
      el.removeEventListener('pointermove', move);
      el.removeEventListener('pointerup', up);
      el.removeEventListener('pointercancel', up);
      el.removeEventListener('wheel', wheel);
    };
  }
}
