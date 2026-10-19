// Generates build/icon.png (1024x1024) with no dependencies: a rounded square with an ember gradient and an "L".
import { deflateSync, crc32 } from 'node:zlib';
import { mkdirSync, writeFileSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const N = 1024;
const out = path.join(path.dirname(fileURLToPath(import.meta.url)), '..', 'build', 'icon.png');

const sdRect = (x, y, cx, cy, hw, hh, r) => {
  const qx = Math.abs(x - cx) - (hw - r);
  const qy = Math.abs(y - cy) - (hh - r);
  return Math.hypot(Math.max(qx, 0), Math.max(qy, 0)) + Math.min(Math.max(qx, qy), 0) - r;
};
const mix = (a, b, t) => a.map((v, i) => Math.round(v + (b[i] - v) * t));
const A = [242, 96, 60];
const B = [255, 180, 84];
const C = [120, 40, 40];

const px = Buffer.alloc(N * N * 4);
for (let y = 0; y < N; y++) {
  for (let x = 0; x < N; x++) {
    const o = (y * N + x) * 4;
    const body = Math.min(1, Math.max(0, 0.5 - sdRect(x + 0.5, y + 0.5, 512, 512, 424, 424, 200)));
    if (body === 0) continue;
    const t = (x + y) / (2 * N);
    let col = mix(mix(A, B, Math.min(1, t * 1.4)), C, Math.max(0, t - 0.55) * 0.9);
    const glow = Math.max(0, 1 - Math.hypot(x - 300, y - 260) / 520);
    col = mix(col, [255, 255, 255], glow * 0.25);
    const stem = Math.min(1, Math.max(0, 0.5 - sdRect(x + 0.5, y + 0.5, 430, 510, 60, 220, 24)));
    const base = Math.min(1, Math.max(0, 0.5 - sdRect(x + 0.5, y + 0.5, 560, 670, 190, 50, 24)));
    const ink = Math.max(stem, base);
    col = mix(col, [255, 246, 236], ink);
    px[o] = col[0];
    px[o + 1] = col[1];
    px[o + 2] = col[2];
    px[o + 3] = Math.round(body * 255);
  }
}

const raw = Buffer.alloc((N * 4 + 1) * N);
for (let y = 0; y < N; y++) px.copy(raw, y * (N * 4 + 1) + 1, y * N * 4, (y + 1) * N * 4);
const chunk = (type, data) => {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body) >>> 0);
  return Buffer.concat([len, body, crc]);
};
const ihdr = Buffer.alloc(13);
ihdr.writeUInt32BE(N, 0);
ihdr.writeUInt32BE(N, 4);
ihdr[8] = 8;
ihdr[9] = 6;
mkdirSync(path.dirname(out), { recursive: true });
writeFileSync(
  out,
  Buffer.concat([
    Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]),
);
console.log('wrote', out);
