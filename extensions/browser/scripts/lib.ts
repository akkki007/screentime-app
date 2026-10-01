import { join } from 'node:path';

const HOST_NAME = 'io.github.akkki007.screentime';
const FIREFOX_EXTENSION_ID = 'screentime@akkki007.github.io';

export type Target = 'chromium' | 'firefox';
export type Browser = 'firefox' | 'chrome' | 'chromium' | 'brave';

type Manifest = Record<string, unknown>;

/**
 * Per-browser manifest from the shared base. Firefox MV3 runs the background
 * as an event page (`scripts`) while Chromium needs a `service_worker`, and
 * Chromium warns about Firefox's `browser_specific_settings`.
 */
export function buildManifest(base: Manifest, target: Target): Manifest {
  if (target === 'chromium') {
    const { browser_specific_settings: _geckoOnly, ...rest } = base;
    return { ...rest, background: { service_worker: 'background.js', type: 'module' } };
  }
  return { ...base, background: { scripts: ['background.js'], type: 'module' } };
}

/** Where each browser looks for native messaging host manifests. */
export function hostManifestPath(browser: Browser, home: string): string {
  const file = `${HOST_NAME}.json`;
  switch (browser) {
    case 'firefox':
      return join(home, '.mozilla', 'native-messaging-hosts', file);
    case 'chrome':
      return join(home, '.config', 'google-chrome', 'NativeMessagingHosts', file);
    case 'chromium':
      return join(home, '.config', 'chromium', 'NativeMessagingHosts', file);
    case 'brave':
      return join(home, '.config', 'BraveSoftware', 'Brave-Browser', 'NativeMessagingHosts', file);
  }
}

/** The native messaging host manifest, which also names the one extension allowed to use it. */
export function buildHostManifest(
  browser: Browser,
  hostPath: string,
  chromiumExtensionId?: string,
): Manifest {
  const base = {
    name: HOST_NAME,
    description: 'Screentime: forwards the active tab domain to the local daemon',
    path: hostPath,
    type: 'stdio',
  };

  if (browser === 'firefox') return { ...base, allowed_extensions: [FIREFOX_EXTENSION_ID] };

  if (!chromiumExtensionId || !/^[a-p]{32}$/.test(chromiumExtensionId)) {
    throw new Error('a valid 32-letter Chromium extension ID is required (see --chromium-id)');
  }
  return { ...base, allowed_origins: [`chrome-extension://${chromiumExtensionId}/`] };
}

/** The wrapper script browsers execute; they run with a minimal PATH, so bun is absolute. */
export function hostWrapperScript(bunPath: string, hostScript: string): string {
  const quote = (s: string) => `'${s.replaceAll("'", `'\\''`)}'`;
  return `#!/bin/sh\nexec ${quote(bunPath)} ${quote(hostScript)}\n`;
}
