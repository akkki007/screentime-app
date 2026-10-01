/**
 * Builds the extension for Chromium and Firefox into dist/<target>/, plus the
 * native messaging host into dist/native-host/.
 */
import { cp, mkdir, rm } from 'node:fs/promises';
import { join } from 'node:path';
import { type Target, buildManifest } from './lib';

const root = join(import.meta.dir, '..');
const dist = join(root, 'dist');
const baseManifest = await Bun.file(join(root, 'manifest.json')).json();

await rm(dist, { recursive: true, force: true });

async function bundle(entry: string, outdir: string, target: 'browser' | 'bun') {
  const result = await Bun.build({ entrypoints: [join(root, entry)], outdir, target });
  if (!result.success) {
    for (const log of result.logs) console.error(log);
    process.exit(1);
  }
}

for (const target of ['chromium', 'firefox'] as Target[]) {
  const outdir = join(dist, target);
  await mkdir(outdir, { recursive: true });
  await bundle('src/background.ts', outdir, 'browser');
  await Bun.write(
    join(outdir, 'manifest.json'),
    `${JSON.stringify(buildManifest(baseManifest, target), null, 2)}\n`,
  );
  console.log(`built dist/${target}`);
}

await bundle('native-host/host.ts', join(dist, 'native-host'), 'bun');
await cp(join(root, 'manifest.json'), join(dist, 'manifest.base.json'));
console.log('built dist/native-host');
