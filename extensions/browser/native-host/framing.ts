/**
 * Chrome/Firefox native messaging framing: each message is a 4-byte
 * little-endian length followed by that many bytes of UTF-8 JSON.
 */

/** Browsers cap messages well below this; anything larger is a corrupt stream. */
export const MAX_MESSAGE_BYTES = 1024 * 1024;

export class FrameDecoder {
  private buffer: Buffer = Buffer.alloc(0);

  /**
   * Feeds a chunk and returns every complete message it finished. Malformed
   * JSON is skipped; an impossible length throws, since the stream can't be
   * resynchronised after it.
   */
  push(chunk: Buffer): unknown[] {
    this.buffer = Buffer.concat([this.buffer, chunk]);
    const messages: unknown[] = [];

    while (this.buffer.length >= 4) {
      const length = this.buffer.readUInt32LE(0);
      if (length > MAX_MESSAGE_BYTES) throw new Error(`native message too large: ${length} bytes`);
      if (this.buffer.length < 4 + length) break;

      const payload = this.buffer.subarray(4, 4 + length).toString('utf8');
      this.buffer = this.buffer.subarray(4 + length);
      try {
        messages.push(JSON.parse(payload));
      } catch {
        // skip a malformed message, keep the stream going
      }
    }
    return messages;
  }
}

export function encodeFrame(message: unknown): Buffer {
  const payload = Buffer.from(JSON.stringify(message), 'utf8');
  const header = Buffer.alloc(4);
  header.writeUInt32LE(payload.length, 0);
  return Buffer.concat([header, payload]);
}
