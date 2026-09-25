/**
 * Entry point for the desktop UI process.
 *
 * TODO(Phase 0 spike, see docs/architecture.md#roadmap): replace this with
 * an actual Electrobun app (Bun + WebKitGTK) once the framework is
 * validated on Ubuntu/GNOME. For now this just proves the UI package can
 * talk to the daemon over the JSON-RPC socket, so `apps/daemon` can be
 * developed against a real client while the UI shell is being built.
 */
import { DaemonClient } from './ipc-client';

async function main() {
  const client = new DaemonClient();
  await client.connect();
  console.log('[desktop] connected to daemon');

  const now = Date.now();
  const summary = await client.call('usage.summary', {
    from: now - 24 * 60 * 60 * 1000,
    to: now,
    groupBy: 'app',
  });
  console.log('[desktop] usage.summary ->', summary);
}

main().catch((err) => {
  console.error('[desktop] failed to start:', err);
  process.exit(1);
});
