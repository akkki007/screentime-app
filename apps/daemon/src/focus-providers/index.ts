import type { FocusProvider } from '@screentime/shared';
import { GnomeWaylandFocusProvider } from './gnome-wayland';
import { NoFocusProvider } from './none';
import { X11FocusProvider } from './x11';

/**
 * Adapters in priority order. The first one whose isAvailable() resolves
 * true is used. Add new adapters here as they're implemented — see
 * docs/adapters.md.
 */
const CANDIDATES: FocusProvider[] = [new GnomeWaylandFocusProvider(), new X11FocusProvider()];

/** Every adapter by id, for SCREENTIME_FOCUS_PROVIDER. */
const BY_ID: Record<string, () => FocusProvider> = {
  none: () => new NoFocusProvider(),
  'gnome-wayland': () => new GnomeWaylandFocusProvider(),
  x11: () => new X11FocusProvider(),
};

/**
 * Picks the adapter. SCREENTIME_FOCUS_PROVIDER forces one by id (`none`
 * reports nothing, for tests); otherwise the first available candidate wins.
 */
export async function selectFocusProvider(
  forced = process.env.SCREENTIME_FOCUS_PROVIDER,
): Promise<FocusProvider> {
  if (forced) {
    const make = Object.hasOwn(BY_ID, forced) ? BY_ID[forced] : undefined;
    if (!make) throw new Error(`unknown SCREENTIME_FOCUS_PROVIDER=${forced}`);
    return make();
  }
  for (const candidate of CANDIDATES) {
    if (await candidate.isAvailable()) {
      return candidate;
    }
  }
  throw new Error(
    `No focus provider available for XDG_SESSION_TYPE=${process.env.XDG_SESSION_TYPE} ` +
      `XDG_CURRENT_DESKTOP=${process.env.XDG_CURRENT_DESKTOP}`,
  );
}
