/**
 * Builds the extension for Chromium and Firefox into dist/<target>/. The
 * native messaging host is `screentimed native-host` (Go), not built here.
 */
import { cp, mkdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { type Target, buildManifest } from './lib';

const root = join(import.meta.dir, '..');
const dist = join(root, 'dist');
const baseManifest = await Bun.file(join(root, 'manifest.json')).json();

await rm(dist, { recursive: true, force: true });

async function bundle(entry: string, outdir: string) {
  const result = await Bun.build({ entrypoints: [join(root, entry)], outdir, target: 'browser' });
  if (!result.success) {
    for (const log of result.logs) console.error(log);
    process.exit(1);
  }
}

for (const target of ['chromium', 'firefox'] as Target[]) {
  const outdir = join(dist, target);
  await mkdir(outdir, { recursive: true });
  await bundle('src/background.ts', outdir);
  await Bun.write(
    join(outdir, 'manifest.json'),
    `${JSON.stringify(buildManifest(baseManifest, target), null, 2)}\n`,
  );
  console.log(`built dist/${target}`);
}

await cp(join(root, 'manifest.json'), join(dist, 'manifest.base.json'));
