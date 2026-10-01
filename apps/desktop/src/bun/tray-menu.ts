import type { TrackerStatus } from '@screentime/shared';

export type TrayItem =
  | { type: 'separator' }
  | { type: 'normal'; label: string; action: string; enabled?: boolean };

export type TrayAction =
  | { kind: 'open' }
  | { kind: 'widget' }
  | { kind: 'pause'; minutes: number }
  | { kind: 'resume' }
  | { kind: 'focus'; minutes: number }
  | { kind: 'focus-stop' }
  | { kind: 'quit' };

/** The tray menu for the current daemon state. Controls are disabled while the daemon is down. */
export function buildTrayMenu(
  status: TrackerStatus | undefined,
  connected: boolean,
  info: TrayInfo = {},
): TrayItem[] {
  const live = connected && status !== undefined;
  const items: TrayItem[] = [];

  // Live status as disabled rows: the native menu can't be styled, but it can inform.
  items.push({
    type: 'normal',
    label: statusLine(status, connected, info),
    action: 'noop',
    enabled: false,
  });
  if (info.todayLabel && connected) {
    items.push({
      type: 'normal',
      label: `Today  ${info.todayLabel}`,
      action: 'noop',
      enabled: false,
    });
  }
  items.push(
    { type: 'separator' },
    { type: 'normal', label: 'Quick panel', action: 'widget' },
    { type: 'normal', label: 'Open dashboard', action: 'open' },
    { type: 'separator' },
  );

  if (status?.paused) {
    items.push({ type: 'normal', label: 'Resume tracking', action: 'resume', enabled: live });
  } else {
    items.push(
      { type: 'normal', label: 'Pause tracking for 15 minutes', action: 'pause:15', enabled: live },
      { type: 'normal', label: 'Pause tracking for 1 hour', action: 'pause:60', enabled: live },
    );
  }

  items.push({ type: 'separator' });
  if (status?.focusMode.active) {
    items.push({ type: 'normal', label: 'Stop focus mode', action: 'focus-stop', enabled: live });
  } else {
    items.push({
      type: 'normal',
      label: 'Start 25-minute focus mode',
      action: 'focus:25',
      enabled: live,
    });
  }

  items.push({ type: 'separator' }, { type: 'normal', label: 'Quit Screentime', action: 'quit' });
  return items;
}

export type TrayInfo = {
  /** Display name of the app being tracked. */
  appName?: string;
  /** Today's total, already formatted (e.g. "4h 47m"). */
  todayLabel?: string;
  /** Formats a resume time for the paused line. */
  resumeLabel?: string;
};

function statusLine(status: TrackerStatus | undefined, connected: boolean, info: TrayInfo): string {
  if (!connected) return 'Daemon not running';
  if (!status) return 'Connecting…';
  if (status.paused) return info.resumeLabel ? `Paused until ${info.resumeLabel}` : 'Paused';
  if (status.focusMode.active) return 'Focus mode on';
  if (status.idle) return 'Idle';
  return info.appName ? `Tracking  ${info.appName}` : 'Tracking';
}

/** Parses a menu action string, returning undefined for anything unrecognised. */
export function parseTrayAction(action: string | undefined): TrayAction | undefined {
  if (!action) return undefined;
  if (action === 'open' || action === 'widget' || action === 'resume' || action === 'quit') {
    return { kind: action };
  }
  if (action === 'focus-stop') return { kind: 'focus-stop' };

  const match = /^(pause|focus):(\d+)$/.exec(action);
  if (match) {
    const minutes = Number(match[2]);
    if (minutes > 0) return { kind: match[1] as 'pause' | 'focus', minutes };
  }
  return undefined;
}

/** Which tray icon (a file in `src/assets`, without extension) shows the current state. */
export function trayIconFor(status: TrackerStatus | undefined, connected: boolean): string {
  if (!connected || !status) return 'tray-offline';
  if (status.paused || status.focusMode.active) return status.paused ? 'tray-paused' : 'tray-focus';
  return 'tray';
}

/** Tray tooltip/title: what is being tracked right now. */
export function trayTitle(status: TrackerStatus | undefined, connected: boolean): string {
  if (!connected) return 'Screentime: daemon not running';
  if (!status) return 'Screentime';
  if (status.paused) return 'Screentime: paused';
  if (status.idle) return 'Screentime: idle';
  return 'Screentime: tracking';
}
