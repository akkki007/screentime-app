import type { FocusProvider } from '@screentime/shared';

/**
 * Reports nothing: no focus changes, never idle. Selected only explicitly
 * (SCREENTIME_FOCUS_PROVIDER=none), so the RPC contract can be exercised
 * without a desktop session, e.g. by the fixture harness in contract/.
 */
export class NoFocusProvider implements FocusProvider {
  readonly id = 'none';

  async isAvailable(): Promise<boolean> {
    return true;
  }

  onFocusChange(): () => void {
    return () => {};
  }

  onIdleChange(): () => void {
    return () => {};
  }
}
