/**
 * Application identity, shared by every package so nothing hardcodes
 * the app ID or data paths in more than one place.
 *
 * Open question (see docs/architecture.md): finalize app name / ID
 * before the first public release.
 */
export const APP_ID = 'io.github.akkki007.screentime';
export const APP_NAME = 'screentime';

/** IPC protocol major version. Daemon rejects clients on a different major version. */
export const IPC_VERSION = 1;

export function dataDir(xdgDataHome = process.env.XDG_DATA_HOME): string {
  const base = xdgDataHome || `${process.env.HOME}/.local/share`;
  return `${base}/${APP_NAME}`;
}

export function runtimeSocketPath(xdgRuntimeDir = process.env.XDG_RUNTIME_DIR): string {
  const base = xdgRuntimeDir || `/run/user/${process.getuid?.() ?? 0}`;
  return `${base}/${APP_NAME}/daemon.sock`;
}
