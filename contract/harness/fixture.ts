/**
 * Fixture files and how a replay is compared with them.
 *
 * Comparison rules (also in contract/README.md):
 * - The response to each request must match exactly: id, result or
 *   error.code, and error.message too unless `looseMessages` is set.
 *   With it, a message of the form "field: reason" only needs the same
 *   field; any other message only needs the same error code.
 * - Notifications sent around a request must match as a multiset: their
 *   order relative to the response and to each other is not part of the
 *   contract.
 * - A step that gets no response in the fixture must get none in a replay.
 */
import { readFileSync, readdirSync } from 'node:fs';
import { join } from 'node:path';
import { FIXTURES_DIR, type FixtureEnv, type Message } from './daemon';

export type Exchange = { send: Message | string; receive: Message[] };
export type Fixture = {
  scenario: string;
  description: string;
  env: FixtureEnv;
  exchanges: Exchange[];
};

export function fixturePath(name: string): string {
  return join(FIXTURES_DIR, `${name}.json`);
}

export function loadFixtures(): Fixture[] {
  return readdirSync(FIXTURES_DIR)
    .filter((f) => f.endsWith('.json'))
    .sort()
    .map((f) => JSON.parse(readFileSync(join(FIXTURES_DIR, f), 'utf8')) as Fixture);
}

const isResponse = (m: Message) => 'id' in m && !('method' in m);

/** Canonical JSON with sorted keys, so notifications compare as strings. */
function canonical(value: unknown): string {
  return JSON.stringify(value, (_k, v) =>
    v && typeof v === 'object' && !Array.isArray(v)
      ? Object.fromEntries(Object.entries(v).sort(([a], [b]) => a.localeCompare(b)))
      : v,
  );
}

function loosen(m: Message): Message {
  const error = m.error as { code: number; message: string } | undefined;
  if (!error) return m;
  const field = error.message.includes(': ') ? error.message.split(': ')[0] : '';
  return { ...m, error: { code: error.code, message: field } };
}

/** Returns a list of differences; empty means the replay matches. */
export function compare(
  expected: Message[],
  actual: Message[],
  { looseMessages = false } = {},
): string[] {
  const prep = (ms: Message[]) => ms.map((m) => (looseMessages ? loosen(m) : m));
  const exp = prep(expected);
  const act = prep(actual);
  const diffs: string[] = [];

  const expResponses = exp.filter(isResponse).map(canonical);
  const actResponses = act.filter(isResponse).map(canonical);
  if (canonical(expResponses) !== canonical(actResponses)) {
    diffs.push(
      `response\n  expected ${expResponses.join(', ') || '(none)'}\n  actual   ${actResponses.join(', ') || '(none)'}`,
    );
  }

  const expNotes = exp
    .filter((m) => !isResponse(m))
    .map(canonical)
    .sort();
  const actNotes = act
    .filter((m) => !isResponse(m))
    .map(canonical)
    .sort();
  if (canonical(expNotes) !== canonical(actNotes)) {
    diffs.push(
      `notifications\n  expected ${expNotes.join(', ') || '(none)'}\n  actual   ${actNotes.join(', ') || '(none)'}`,
    );
  }
  return diffs;
}
