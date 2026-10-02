/**
 * Records fixtures/<scenario>.json from the current daemon. Run it when the
 * contract changes on purpose, then review the diff: every changed line is a
 * behaviour change that other implementations must follow.
 *
 *   bun run --cwd contract record            # all scenarios
 *   bun run --cwd contract record limits     # just one
 */
import { writeFileSync } from 'node:fs';
import { startDaemon } from './daemon';
import { type Fixture, fixturePath } from './fixture';
import { ENV, SCENARIOS } from './scenarios';

/** The rules engine first ticks after 5 s; finish well before so it never fires mid-scenario. */
export const MAX_SCENARIO_MS = 4_000;

const only = new Set(process.argv.slice(2));
for (const scenario of SCENARIOS) {
  if (only.size && !only.has(scenario.name)) continue;
  const daemon = await startDaemon(ENV);
  const started = Date.now();
  const fixture: Fixture = {
    scenario: scenario.name,
    description: scenario.description,
    env: ENV,
    exchanges: [],
  };
  try {
    for (const send of scenario.steps) {
      fixture.exchanges.push({ send, receive: await daemon.exchange(send) });
    }
  } finally {
    await daemon.stop();
  }
  const elapsed = Date.now() - started;
  if (elapsed > MAX_SCENARIO_MS) {
    throw new Error(
      `${scenario.name} took ${elapsed} ms; split it so it stays under ${MAX_SCENARIO_MS} ms`,
    );
  }
  writeFileSync(fixturePath(scenario.name), `${JSON.stringify(fixture, null, 2)}\n`);
  console.log(`${scenario.name}: ${fixture.exchanges.length} exchanges (${elapsed} ms)`);
}
