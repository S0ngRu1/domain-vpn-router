const fs = require("fs");

const path = "cmd/router/app.ico";
const size = 32;
const pixels = Buffer.alloc(size * size * 4);

for (let y = 0; y < size; y++) {
  for (let x = 0; x < size; x++) {
    const i = (y * size + x) * 4;
    const dx = x - 16;
    const dy = y - 16;
    if (dx * dx + dy * dy > 15 * 15) {
      pixels[i + 3] = 0;
      continue;
    }
    pixels[i] = 153;
    pixels[i + 1] = 211;
    pixels[i + 2] = 52;
    pixels[i + 3] = 255;
    const cross = (x >= 9 && x <= 22 && y >= 14 && y <= 17) || (x >= 16 && x <= 19 && y >= 9 && y <= 22);
    const arrows = (x === 22 && y >= 12 && y <= 15) || (y === 12 && x >= 19 && x <= 22) || (x === 9 && y >= 18 && y <= 21) || (y === 21 && x >= 9 && x <= 12);
    if (cross || arrows) {
      pixels[i] = 45;
      pixels[i + 1] = 55;
      pixels[i + 2] = 15;
    }
  }
}

const headerSize = 40;
const xorSize = pixels.length;
const andSize = size * size / 8;
const imageSize = headerSize + xorSize + andSize;
const buf = Buffer.alloc(6 + 16 + imageSize);
let o = 0;

buf.writeUInt16LE(0, o); o += 2;
buf.writeUInt16LE(1, o); o += 2;
buf.writeUInt16LE(1, o); o += 2;
buf[o++] = size;
buf[o++] = size;
buf[o++] = 0;
buf[o++] = 0;
buf.writeUInt16LE(1, o); o += 2;
buf.writeUInt16LE(32, o); o += 2;
buf.writeUInt32LE(imageSize, o); o += 4;
buf.writeUInt32LE(22, o); o += 4;

buf.writeUInt32LE(headerSize, o); o += 4;
buf.writeInt32LE(size, o); o += 4;
buf.writeInt32LE(size * 2, o); o += 4;
buf.writeUInt16LE(1, o); o += 2;
buf.writeUInt16LE(32, o); o += 2;
buf.writeUInt32LE(0, o); o += 4;
buf.writeUInt32LE(xorSize + andSize, o); o += 4;
buf.writeInt32LE(0, o); o += 4;
buf.writeInt32LE(0, o); o += 4;
buf.writeUInt32LE(0, o); o += 4;
buf.writeUInt32LE(0, o); o += 4;

for (let y = size - 1; y >= 0; y--) {
  pixels.copy(buf, o, y * size * 4, (y + 1) * size * 4);
  o += size * 4;
}

fs.writeFileSync(path, buf);
