import { describe, expect, test } from 'bun:test';
import { MOTION, Spring } from './spring.js';

/** Run a spring at 60 fps until it rests; returns the frames taken and the largest value seen. */
function run(spring, maxFrames = 600) {
  let peak = spring.value;
  for (let i = 1; i <= maxFrames; i++) {
    const done = spring.step(1 / 60);
    peak = Math.max(peak, spring.value);
    if (done) return { frames: i, peak };
  }
  return { frames: Number.POSITIVE_INFINITY, peak };
}

describe('Spring', () => {
  test('playful overshoots a little and rests within about half a second', () => {
    const s = new Spring(100, MOTION.playful);
    s.setTarget(600);
    const { frames, peak } = run(s);
    expect(s.value).toBe(600);
    expect(peak).toBeGreaterThan(600);
    expect(peak).toBeLessThan(600 + 500 * 0.06);
    expect(frames).toBeLessThan(60);
  });

  test('calm never passes the target', () => {
    const s = new Spring(100, MOTION.calm);
    s.setTarget(600);
    const { peak } = run(s);
    expect(peak).toBeLessThanOrEqual(600);
    expect(s.value).toBe(600);
  });

  test('retargeting mid-flight keeps the velocity', () => {
    const s = new Spring(0, MOTION.playful);
    s.setTarget(500);
    for (let i = 0; i < 6; i++) s.step(1 / 60);
    const v = s.velocity;
    expect(v).toBeGreaterThan(0);
    s.setTarget(0);
    expect(s.velocity).toBe(v);
    s.step(1 / 60);
    expect(s.value).toBeGreaterThan(0);
  });

  test('a long stall is clamped, so it never flies off', () => {
    const s = new Spring(0, MOTION.playful);
    s.setTarget(400);
    s.step(5);
    expect(Math.abs(s.value)).toBeLessThan(800);
  });

  test('snap jumps and rests', () => {
    const s = new Spring(0);
    s.snap(42);
    expect(s.value).toBe(42);
    expect(s.settled).toBe(true);
  });
});
