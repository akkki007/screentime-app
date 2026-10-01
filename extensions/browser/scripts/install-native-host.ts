/**
 * Registers the native messaging host with your browsers so the extension can
 * reach the daemon. Run it from the repo:
 *
 *   bun run install:host                       # Firefox only
 *   bun run install:host -- --browser chrome --chromium-id <32-letter id>
 *   bun run install:host -- --uninstall
 *
 * Chromium-family browsers derive an unpacked extension's ID from its path, so
 * it can't be guessed: load the extension, copy its ID from the extensions
 * page, and pass it as --chromium-id.
 */
import { chmod, mkdir, rm, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, join } from 'node:path';
import { parseArgs } from 'node:util';
import { dataDir } from '@screentime/shared';
import { type Browser, buildHostManifest, hostManifestPath, hostWrapperScript } from './lib';

const { values } = parseArgs({
  options: {
    browser: { type: 'string', multiple: true },
    'chromium-id': { type: 'string' },
    uninstall: { type: 'boolean', default: false },
    'dry-run': { type: 'boolean', default: false },
  },
});

const ALL: Browser[] = ['firefox', 'chrome', 'chromium', 'brave'];
const requested = (values.browser?.length ? values.browser : ['firefox']) as string[];
const browsers = requested.includes('all') ? ALL : (requested as Browser[]);
for (const b of browsers) {
  if (!ALL.includes(b)) {
    console.error(`unknown browser "${b}" (expected one of: ${ALL.join(', ')}, all)`);
    process.exit(2);
  }
}

const home = homedir();
const wrapperPath = join(dataDir(), 'native-host', 'screentime-native-host');
const hostScript = join(import.meta.dir, '..', 'native-host', 'host.ts');
const dry = values['dry-run'];

for (const browser of browsers) {
  const manifestPath = hostManifestPath(browser, home);

  if (values.uninstall) {
    if (!dry) await rm(manifestPath, { force: true });
    console.log(`${dry ? '[dry run] would remove' : 'removed'} ${manifestPath}`);
    continue;
  }

  let manifest: Record<string, unknown>;
  try {
    manifest = buildHostManifest(browser, wrapperPath, values['chromium-id']);
  } catch (err) {
    console.error(`${browser}: ${(err as Error).message}`);
    process.exitCode = 2;
    continue;
  }

  console.log(`${dry ? '[dry run] would write' : 'wrote'} ${manifestPath}`);
  if (dry) continue;
  await mkdir(dirname(manifestPath), { recursive: true });
  await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
}

if (!values.uninstall && !dry) {
  await mkdir(dirname(wrapperPath), { recursive: true });
  await writeFile(wrapperPath, hostWrapperScript(process.execPath, hostScript));
  await chmod(wrapperPath, 0o755);
  console.log(`wrote ${wrapperPath}`);
}
if (values.uninstall && !dry) await rm(wrapperPath, { force: true });
