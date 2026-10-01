import type { FocusProvider } from '@screentime/shared';
import { GnomeWaylandFocusProvider } from './gnome-wayland';
import { X11FocusProvider } from './x11';

/**
 * Adapters in priority order. The first one whose isAvailable() resolves
 * true is used. Add new adapters here as they're implemented — see
 * docs/adapters.md.
 */
const CANDIDATES: FocusProvider[] = [new GnomeWaylandFocusProvider(), new X11FocusProvider()];

export async function selectFocusProvider(): Promise<FocusProvider> {
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
