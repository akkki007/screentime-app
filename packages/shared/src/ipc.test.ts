import { describe, expect, test } from 'bun:test';
import { RpcMethods, UsageSummaryRequestSchema } from './ipc';

describe('UsageSummaryRequestSchema', () => {
  test('accepts a valid request', () => {
    const result = UsageSummaryRequestSchema.safeParse({
      from: 0,
      to: 1_000,
      groupBy: 'app',
    });
    expect(result.success).toBe(true);
  });

  test('rejects an invalid groupBy', () => {
    const result = UsageSummaryRequestSchema.safeParse({
      from: 0,
      to: 1_000,
      groupBy: 'not-a-real-option',
    });
    expect(result.success).toBe(false);
  });
});

describe('RpcMethods', () => {
  test('usage.summary response schema validates a row list', () => {
    const result = RpcMethods['usage.summary'].response.safeParse([
      { key: 'org.mozilla.firefox', ms: 60_000 },
    ]);
    expect(result.success).toBe(true);
  });
});
