export interface Vec2 {
  x: number;
  y: number;
}

/**
 * Converts a thumb offset (screen pixels, y down) to a stick vector with
 * magnitude in [0, 1]. Inside the dead zone (fraction of radius) it is zero.
 */
export function stickVector(dx: number, dy: number, radius: number, deadZone = 0.15): Vec2 {
  const len = Math.hypot(dx, dy);
  if (radius <= 0 || len === 0) return { x: 0, y: 0 };
  const mag = Math.min(len / radius, 1);
  if (mag < deadZone) return { x: 0, y: 0 };
  // Rescale so the output ramps from 0 at the dead-zone edge to 1 at the rim.
  const scaled = (mag - deadZone) / (1 - deadZone);
  return { x: (dx / len) * scaled, y: (dy / len) * scaled };
}

/**
 * Rotates a screen-space stick/key vector (x right, y down) into world space
 * (x east, z south) relative to the camera. Camera yaw 0 looks north (-z);
 * positive yaw turns the camera clockwise seen from above. Pushing the stick
 * up always moves away from the camera.
 */
export function toWorldMove(stick: Vec2, cameraYaw: number): Vec2 {
  const s = Math.sin(cameraYaw);
  const c = Math.cos(cameraYaw);
  const forward = -stick.y;
  const right = stick.x;
  return {
    x: right * c + forward * s,
    y: right * s - forward * c,
  };
}

/**
 * Pointer-event joystick. Tracks a single pointerId so other simultaneous
 * touches (action buttons, camera drag) never move or release the stick.
 */
export class Joystick {
  private pointerId: number | null = null;
  private originX = 0;
  private originY = 0;
  value: Vec2 = { x: 0, y: 0 };

  constructor(
    private readonly radius: number,
    private readonly onChange: (v: Vec2) => void = () => {},
  ) {}

  get active(): boolean {
    return this.pointerId !== null;
  }

  down(pointerId: number, x: number, y: number): boolean {
    if (this.pointerId !== null) return false;
    this.pointerId = pointerId;
    this.originX = x;
    this.originY = y;
    this.set({ x: 0, y: 0 });
    return true;
  }

  move(pointerId: number, x: number, y: number): void {
    if (pointerId !== this.pointerId) return;
    this.set(stickVector(x - this.originX, y - this.originY, this.radius));
  }

  /** Handles pointerup and pointercancel. */
  up(pointerId: number): void {
    if (pointerId !== this.pointerId) return;
    this.pointerId = null;
    this.set({ x: 0, y: 0 });
  }

  private set(v: Vec2): void {
    this.value = v;
    this.onChange(v);
  }
}
