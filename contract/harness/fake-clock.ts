/**
 * Preloaded into the Bun daemon by the harness (`bun --preload`): freezes
 * Date.now() at SCREENTIME_FAKE_NOW so every fixture sees the same clock.
 * Timers still run in real time. Other implementations provide the same
 * behaviour their own way (see contract/README.md).
 */
const now = Number(process.env.SCREENTIME_FAKE_NOW);
if (!Number.isSafeInteger(now)) {
  throw new Error('fake-clock: SCREENTIME_FAKE_NOW must be Unix ms');
}
Date.now = () => now;
