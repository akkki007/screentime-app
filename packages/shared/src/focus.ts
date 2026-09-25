/**
 * The interface every desktop adapter implements. The daemon picks one
 * at startup based on $XDG_SESSION_TYPE / $XDG_CURRENT_DESKTOP.
 * See docs/adapters.md for how to add a new one.
 */

export type Unsubscribe = () => void;

export type FocusedWindow = {
  /** `.desktop` file ID (e.g. "org.mozilla.firefox"), WM_CLASS as fallback. */
  appId: string;
  /** Only populated when the user has opted in to title tracking. */
  title?: string;
  pid?: number;
  /** Unix ms, UTC. */
  ts: number;
};

export interface FocusProvider {
  /** e.g. "gnome-wayland", "x11", "kde-wayland", "hyprland" */
  readonly id: string;

  /** Whether this adapter applies to the current session (checked at startup). */
  isAvailable(): Promise<boolean>;

  onFocusChange(cb: (w: FocusedWindow) => void): Unsubscribe;

  onIdleChange(cb: (idle: boolean) => void, thresholdMs: number): Unsubscribe;
}
