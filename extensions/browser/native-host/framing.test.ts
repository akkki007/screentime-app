import { describe, expect, test } from 'bun:test';
import { FrameDecoder, MAX_MESSAGE_BYTES, encodeFrame } from './framing';

describe('FrameDecoder', () => {
  test('decodes a message delivered in one chunk', () => {
    const decoder = new FrameDecoder();
    expect(decoder.push(encodeFrame({ domain: 'a.com', active: true }))).toEqual([
      { domain: 'a.com', active: true },
    ]);
  });

  test('reassembles a message split at any byte boundary', () => {
    const frame = encodeFrame({ domain: 'example.org', active: true });
    for (let cut = 1; cut < frame.length; cut++) {
      const decoder = new FrameDecoder();
      const first = decoder.push(frame.subarray(0, cut));
      const second = decoder.push(frame.subarray(cut));
      expect([...first, ...second]).toEqual([{ domain: 'example.org', active: true }]);
    }
  });

  test('decodes several messages in one chunk, including multi-byte text', () => {
    const decoder = new FrameDecoder();
    const chunk = Buffer.concat([
      encodeFrame({ n: 1 }),
      encodeFrame({ n: 'é日本' }),
      encodeFrame({ n: 3 }),
    ]);
    expect(decoder.push(chunk)).toEqual([{ n: 1 }, { n: 'é日本' }, { n: 3 }]);
  });

  test('skips malformed JSON without losing later messages', () => {
    const decoder = new FrameDecoder();
    const bad = Buffer.from('{oops', 'utf8');
    const header = Buffer.alloc(4);
    header.writeUInt32LE(bad.length, 0);
    expect(decoder.push(Buffer.concat([header, bad, encodeFrame({ ok: true })]))).toEqual([
      { ok: true },
    ]);
  });

  test('rejects an impossible length instead of buffering it', () => {
    const decoder = new FrameDecoder();
    const header = Buffer.alloc(4);
    header.writeUInt32LE(MAX_MESSAGE_BYTES + 1, 0);
    expect(() => decoder.push(header)).toThrow('too large');
  });
});
