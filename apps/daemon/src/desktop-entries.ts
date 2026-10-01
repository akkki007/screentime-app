import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';

export type DesktopEntryInfo = { name?: string; icon?: string; startupWMClass?: string };

/** Directories searched for `<appId>.desktop`, per the XDG base directory spec plus Flatpak/Snap exports. */
export function applicationDirs(env: NodeJS.ProcessEnv = process.env): string[] {
  const home = env.HOME ?? '';
  const dataHome = env.XDG_DATA_HOME || join(home, '.local', 'share');
  const dataDirs = (env.XDG_DATA_DIRS || '/usr/local/share:/usr/share').split(':').filter(Boolean);

  return [
    join(dataHome, 'applications'),
    ...dataDirs.map((d) => join(d, 'applications')),
    join(home, '.local/share/flatpak/exports/share/applications'),
    '/var/lib/flatpak/exports/share/applications',
    '/var/lib/snapd/desktop/applications',
  ];
}

/** Reads the unlocalised Name and Icon from the `[Desktop Entry]` group of a .desktop file. */
export function parseDesktopEntry(text: string): DesktopEntryInfo {
  const info: DesktopEntryInfo = {};
  let inEntry = false;

  for (const raw of text.split('\n')) {
    const line = raw.trim();
    if (line.startsWith('[')) {
      inEntry = line === '[Desktop Entry]';
      continue;
    }
    if (!inEntry || line.startsWith('#')) continue;

    const eq = line.indexOf('=');
    if (eq === -1) continue;
    const key = line.slice(0, eq);
    const value = line.slice(eq + 1).trim();
    if (key === 'Name' && info.name === undefined) info.name = value;
    if (key === 'Icon' && info.icon === undefined) info.icon = value;
    if (key === 'StartupWMClass' && info.startupWMClass === undefined) info.startupWMClass = value;
  }

  return info;
}

export function lookupDesktopEntry(
  appId: string,
  dirs: string[] = applicationDirs(),
): DesktopEntryInfo | undefined {
  // App IDs come from the compositor; never let one climb out of the search dirs.
  if (appId.includes('/') || appId.includes('..')) return undefined;

  for (const dir of dirs) {
    const path = join(dir, `${appId}.desktop`);
    if (!existsSync(path)) continue;
    try {
      return parseDesktopEntry(readFileSync(path, 'utf8'));
    } catch {
      // unreadable entry: try the next directory
    }
  }
  return undefined;
}

/**
 * Maps `StartupWMClass` (lowercased) to the desktop-entry ID, so an X11 window
 * identified only by its WM_CLASS can be resolved to the same app ID that
 * Wayland reports. Earlier directories win, matching XDG precedence.
 */
export function buildWmClassIndex(dirs: string[] = applicationDirs()): Map<string, string> {
  const index = new Map<string, string>();
  for (const dir of dirs) {
    let files: string[];
    try {
      files = readdirSync(dir).filter((f) => f.endsWith('.desktop'));
    } catch {
      continue; // directory doesn't exist
    }
    for (const file of files) {
      try {
        const { startupWMClass } = parseDesktopEntry(readFileSync(join(dir, file), 'utf8'));
        const key = startupWMClass?.toLowerCase();
        if (key && !index.has(key)) index.set(key, file.slice(0, -'.desktop'.length));
      } catch {
        // unreadable entry
      }
    }
  }
  return index;
}
