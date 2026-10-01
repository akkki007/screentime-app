import type { TrackerStatus } from '@screentime/shared';

export type TrayItem =
  | { type: 'separator' }
  | { type: 'normal'; label: string; action: string; enabled?: boolean };

export type TrayAction =
  | { kind: 'open' }
  | { kind: 'pause'; minutes: number }
  | { kind: 'resume' }
  | { kind: 'focus'; minutes: number }
  | { kind: 'focus-stop' }
  | { kind: 'quit' };

/** The tray menu for the current daemon state. Controls are disabled while the daemon is down. */
export function buildTrayMenu(status: TrackerStatus | undefined, connected: boolean): TrayItem[] {
  const live = connected && status !== undefined;
  const items: TrayItem[] = [
    { type: 'normal', label: 'Open Screentime', action: 'open' },
    { type: 'separator' },
  ];

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

/** Parses a menu action string, returning undefined for anything unrecognised. */
export function parseTrayAction(action: string | undefined): TrayAction | undefined {
  if (!action) return undefined;
  if (action === 'open' || action === 'resume' || action === 'quit') return { kind: action };
  if (action === 'focus-stop') return { kind: 'focus-stop' };

  const match = /^(pause|focus):(\d+)$/.exec(action);
  if (match) {
    const minutes = Number(match[2]);
    if (minutes > 0) return { kind: match[1] as 'pause' | 'focus', minutes };
  }
  return undefined;
}

/** Tray tooltip/title: what is being tracked right now. */
export function trayTitle(status: TrackerStatus | undefined, connected: boolean): string {
  if (!connected) return 'Screentime: daemon not running';
  if (!status) return 'Screentime';
  if (status.paused) return 'Screentime: paused';
  if (status.idle) return 'Screentime: idle';
  return 'Screentime: tracking';
}
