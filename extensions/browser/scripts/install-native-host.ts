/**
 * Registers the native messaging host with your browsers so the extension can
 * reach the daemon. The host is `screentimed native-host`: the browsers launch
 * a symlink to the screentimed binary (build it with
 * `go build -o bin/screentimed ./cmd/screentimed`), so no Bun is needed once
 * this has run. Run it from the repo:
 *
 *   bun run install:host                       # Firefox only
 *   bun run install:host -- --browser chrome --chromium-id <32-letter id>
 *   bun run install:host -- --daemon /path/to/screentimed
 *   bun run install:host -- --uninstall
 *
 * Chromium-family browsers derive an unpacked extension's ID from its path, so
 * it can't be guessed: load the extension, copy its ID from the extensions
 * page, and pass it as --chromium-id.
 */
import { existsSync } from 'node:fs';
import { mkdir, rm, symlink, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { parseArgs } from 'node:util';
import { dataDir } from '@screentime/shared';
import { type Browser, HOST_LINK_NAME, buildHostManifest, hostManifestPath } from './lib';

const { values } = parseArgs({
  options: {
    browser: { type: 'string', multiple: true },
    'chromium-id': { type: 'string' },
    daemon: { type: 'string' },
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
const linkPath = join(dataDir(), 'native-host', HOST_LINK_NAME);
const dry = values['dry-run'];

function findDaemon(): string | undefined {
  if (values.daemon) return existsSync(values.daemon) ? resolve(values.daemon) : undefined;
  const candidates = [
    process.env.SCREENTIME_DAEMON,
    join(import.meta.dir, '..', '..', '..', 'bin', 'screentimed'),
    '/usr/lib/screentime/screentimed',
    Bun.which('screentimed') ?? undefined,
  ];
  const found = candidates.find((c) => c && existsSync(c));
  return found ? resolve(found) : undefined;
}

let daemon: string | undefined;
if (!values.uninstall) {
  daemon = findDaemon();
  if (!daemon) {
    console.error(
      'screentimed not found. Build it with `go build -o bin/screentimed ./cmd/screentimed`, or pass --daemon <path>.',
    );
    process.exit(2);
  }
}

for (const browser of browsers) {
  const manifestPath = hostManifestPath(browser, home);

  if (values.uninstall) {
    if (!dry) await rm(manifestPath, { force: true });
    console.log(`${dry ? '[dry run] would remove' : 'removed'} ${manifestPath}`);
    continue;
  }

  let manifest: Record<string, unknown>;
  try {
    manifest = buildHostManifest(browser, linkPath, values['chromium-id']);
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

if (!values.uninstall && !dry && daemon) {
  await mkdir(dirname(linkPath), { recursive: true });
  await rm(linkPath, { force: true });
  await symlink(daemon, linkPath);
  console.log(`linked ${linkPath} -> ${daemon}`);
}
if (values.uninstall && !dry) await rm(linkPath, { force: true });
