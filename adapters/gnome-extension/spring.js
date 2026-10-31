/**
 * Damped springs for the island's shape (width, height, corner radius). A
 * spring can be retargeted mid-flight and keeps its velocity, so a change of
 * mind (the pointer leaving while it opens) bends the motion instead of
 * restarting it. Pure JavaScript, no GNOME imports, so Bun can test it.
 */

/** How the island moves, chosen in Settings → Motion. */
export const MOTION = {
  // A little overshoot: about 3% past the target, at rest in ~0.5 s.
  playful: { stiffness: 170, damping: 19 },
  // Critically damped: no overshoot, about as quick.
  calm: { stiffness: 170, damping: 26 },
};

const STEP = 1 / 240; // fixed substeps keep it stable whatever the frame rate
// At rest once the motion is below half a pixel: the shape is drawn in whole
// pixels, so a longer tail would only spend frames that change nothing.
const REST_DISTANCE = 0.5; // px
const REST_SPEED = 10; // px per second

export class Spring {
  /** @param {number} value @param {{stiffness: number, damping: number}} params */
  constructor(value, params = MOTION.playful) {
    this.value = value;
    this.target = value;
    this.velocity = 0;
    this.params = params;
  }

  /** Move towards a new target, keeping the current velocity. */
  setTarget(target) {
    this.target = target;
  }

  /** Jump straight to `value` (reduced motion, or the first layout). */
  snap(value) {
    this.value = value;
    this.target = value;
    this.velocity = 0;
  }

  get settled() {
    return (
      Math.abs(this.target - this.value) < REST_DISTANCE && Math.abs(this.velocity) < REST_SPEED
    );
  }

  /** Advance by `dt` seconds. Returns true once at rest (and snaps exactly onto the target). */
  step(dt) {
    let left = Math.min(dt, 0.1); // a long stall (a busy frame) must not fling it
    const { stiffness, damping } = this.params;
    while (left > 0) {
      const h = Math.min(STEP, left);
      const force = stiffness * (this.target - this.value) - damping * this.velocity;
      this.velocity += force * h;
      this.value += this.velocity * h;
      left -= h;
    }
    if (this.settled) {
      this.value = this.target;
      this.velocity = 0;
      return true;
    }
    return false;
  }
}
