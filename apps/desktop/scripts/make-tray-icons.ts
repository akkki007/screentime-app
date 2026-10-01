/**
 * Generates the tray icons in src/assets from geometry, so there is no binary
 * artwork to hand-maintain:  bun run scripts/make-tray-icons.ts
 *
 * Each icon is a bold disc (it has to read at ~22 px on a dark top bar) with
 * its glyph knocked out: tracking = clock hands, paused = pause bars, focus =
 * a bolt, offline = a grey slashed ring.
 */
import { writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { deflateSync } from 'node:zlib';

const SIZE = 96;
const SS = 4; // supersampling per axis, for smooth edges
const U = SIZE / 64; // geometry below is authored on a 64-unit grid

type Shape = (x: number, y: number) => boolean;
const disc =
  (cx: number, cy: number, r: number): Shape =>
  (x, y) =>
    (x - cx) ** 2 + (y - cy) ** 2 <= r * r;
const ring = (cx: number, cy: number, r: number, w: number): Shape => {
  const outer = disc(cx, cy, r);
  const inner = disc(cx, cy, r - w);
  return (x, y) => outer(x, y) && !inner(x, y);
};
/** A line segment with round caps. */
const capsule =
  (x0: number, y0: number, x1: number, y1: number, r: number): Shape =>
  (x, y) => {
    const dx = x1 - x0;
    const dy = y1 - y0;
    const t = Math.max(0, Math.min(1, ((x - x0) * dx + (y - y0) * dy) / (dx * dx + dy * dy)));
    return (x - (x0 + t * dx)) ** 2 + (y - (y0 + t * dy)) ** 2 <= r * r;
  };
const union =
  (...shapes: Shape[]): Shape =>
  (x, y) =>
    shapes.some((s) => s(x, y));
const minus =
  (a: Shape, b: Shape): Shape =>
  (x, y) =>
    a(x, y) && !b(x, y);

type Rgb = [number, number, number];

function render(shape: Shape, [r, g, b]: Rgb): Buffer {
  const rows: Buffer[] = [];
  for (let py = 0; py < SIZE; py++) {
    const row = Buffer.alloc(1 + SIZE * 4); // filter byte 0, then RGBA
    for (let px = 0; px < SIZE; px++) {
      let hit = 0;
      for (let sy = 0; sy < SS; sy++) {
        for (let sx = 0; sx < SS; sx++) {
          if (shape((px + (sx + 0.5) / SS) / U, (py + (sy + 0.5) / SS) / U)) hit++;
        }
      }
      const o = 1 + px * 4;
      row[o] = r;
      row[o + 1] = g;
      row[o + 2] = b;
      row[o + 3] = Math.round((hit / (SS * SS)) * 255);
    }
    rows.push(row);
  }
  return png(Buffer.concat(rows));
}

function crc32(buf: Buffer): number {
  let c = ~0;
  for (const byte of buf) {
    c ^= byte;
    for (let k = 0; k < 8; k++) c = c & 1 ? (c >>> 1) ^ 0xedb88320 : c >>> 1;
  }
  return ~c >>> 0;
}

function png(raw: Buffer): Buffer {
  const chunk = (type: string, data: Buffer) => {
    const body = Buffer.concat([Buffer.from(type), data]);
    const out = Buffer.alloc(8 + data.length + 4);
    out.writeUInt32BE(data.length, 0);
    body.copy(out, 4);
    out.writeUInt32BE(crc32(body), 8 + data.length);
    return out;
  };
  const header = Buffer.alloc(13);
  header.writeUInt32BE(SIZE, 0);
  header.writeUInt32BE(SIZE, 4);
  header.set([8, 6, 0, 0, 0], 8); // 8-bit RGBA
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

const body = disc(32, 32, 29);
const hands = union(capsule(32, 32, 32, 14, 3), capsule(32, 32, 45, 39, 3));
const pauseBars = union(capsule(24.5, 22, 24.5, 42, 3.4), capsule(39.5, 22, 39.5, 42, 3.4));
const bolt = union(
  capsule(37, 14, 26, 34, 2.8),
  capsule(26, 34, 38, 34, 2.8),
  capsule(38, 34, 27, 52, 2.8),
);

const icons: Record<string, { shape: Shape; color: Rgb }> = {
  tray: { shape: minus(body, hands), color: [255, 143, 69] },
  'tray-paused': { shape: minus(body, pauseBars), color: [255, 196, 66] },
  'tray-focus': { shape: minus(body, bolt), color: [242, 238, 44] },
  'tray-offline': {
    shape: union(ring(32, 32, 28, 5), capsule(17, 47, 47, 17, 2.8)),
    color: [150, 140, 132],
  },
};

const assets = join(import.meta.dir, '..', 'src', 'assets');
for (const [name, { shape, color }] of Object.entries(icons)) {
  writeFileSync(join(assets, `${name}.png`), render(shape, color));
  console.log(`wrote ${name}.png`);
}
