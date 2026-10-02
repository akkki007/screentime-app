/**
 * Replays every fixture against a daemon (the Bun one by default, or
 * CONTRACT_DAEMON_CMD) and checks the generated schema is current.
 *
 *   bun test contract
 *   CONTRACT_DAEMON_CMD="./screentimed" CONTRACT_LOOSE_MESSAGES=1 bun test contract
 */
import { describe, expect, test } from 'bun:test';
import { readFileSync } from 'node:fs';
import { RpcMethods, RpcNotifications } from '@screentime/shared';
import { startDaemon } from './daemon';
import { SCHEMA_PATH, renderSchema } from './export-schema';
import { compare, loadFixtures } from './fixture';
import { SCENARIOS } from './scenarios';

const looseMessages = process.env.CONTRACT_LOOSE_MESSAGES === '1';
const fixtures = loadFixtures();

/** Notifications the fixtures can't produce: they need a desktop session or the 5 s rules tick. */
const NOT_IN_FIXTURES = new Set(['event.focus', 'event.limitHit', 'event.reminder']);

describe('contract', () => {
  test('schema/rpc.schema.json is up to date (bun run --cwd contract schema)', () => {
    expect(readFileSync(SCHEMA_PATH, 'utf8')).toBe(renderSchema());
  });

  test('every scenario has a fixture (bun run --cwd contract record)', () => {
    expect(fixtures.map((f) => f.scenario).sort()).toEqual(SCENARIOS.map((s) => s.name).sort());
  });

  test('fixtures cover every method and the notifications they can', () => {
    const sent = new Set<string>();
    const received = new Set<string>();
    for (const f of fixtures) {
      for (const e of f.exchanges) {
        if (typeof e.send !== 'string') sent.add(String(e.send.method));
        for (const m of e.receive) if (typeof m.method === 'string') received.add(m.method);
      }
    }
    for (const method of ['version', 'browser.activeTab', ...Object.keys(RpcMethods)]) {
      expect(sent).toContain(method);
    }
    for (const name of Object.keys(RpcNotifications)) {
      if (!NOT_IN_FIXTURES.has(name)) expect(received).toContain(name);
    }
  });

  for (const fixture of fixtures) {
    test(`replay ${fixture.scenario}: ${fixture.description}`, async () => {
      const daemon = await startDaemon(fixture.env);
      try {
        for (const [i, exchange] of fixture.exchanges.entries()) {
          const actual = await daemon.exchange(exchange.send);
          const diffs = compare(exchange.receive, actual, { looseMessages });
          if (diffs.length) {
            const sent =
              typeof exchange.send === 'string' ? exchange.send : JSON.stringify(exchange.send);
            throw new Error(
              `step ${i + 1} ${sent}\n${diffs.join('\n')}\n--- daemon log ---\n${daemon.log()}`,
            );
          }
        }
      } finally {
        await daemon.stop();
      }
    }, 15_000);
  }
});
