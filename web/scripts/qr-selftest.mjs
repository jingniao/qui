// Self-test for the QR encoder in web/src/app.
//
// The TypeScript sources are transpiled in memory (ts.transpileModule), written
// to a fresh temporary directory and imported from there; every matrix is then
// rendered to RGBA pixels and decoded again with jsQR.
//
// Usage: node scripts/qr-selftest.mjs   (exit code 1 on any failure)

import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import ts from 'typescript';

const require = createRequire(import.meta.url);
const jsQR = require('jsqr');

const here = path.dirname(fileURLToPath(import.meta.url));
const appDir = path.resolve(here, '..', 'src', 'app');
const outDir = mkdtempSync(path.join(tmpdir(), 'qr-selftest-'));

const SOURCES = [
  'qr/tables.ts',
  'qr/reedsolomon.ts',
  'qr/data.ts',
  'qr/penalty.ts',
  'qr/matrix.ts',
  'qrcode.ts',
];

for (const rel of SOURCES) {
  const source = readFileSync(path.join(appDir, rel), 'utf8');
  const js = ts.transpileModule(source, {
    compilerOptions: {
      target: ts.ScriptTarget.ES2020,
      module: ts.ModuleKind.ESNext,
    },
    fileName: rel,
  }).outputText;
  const rewritten = js.replace(/(from\s+['"])(\.[^'"]*)(['"])/g, (_match, head, spec, tail) => {
    const withExt = spec.endsWith('.js') || spec.endsWith('.mjs') ? spec : `${spec}.mjs`;
    return `${head}${withExt}${tail}`;
  });
  const target = path.join(outDir, rel.replace(/\.ts$/, '.mjs'));
  mkdirSync(path.dirname(target), { recursive: true });
  writeFileSync(target, rewritten, 'utf8');
}

const { encodeQR } = await import(pathToFileURL(path.join(outDir, 'qr', 'matrix.mjs')).href);
const { generateQRCodeSVG } = await import(pathToFileURL(path.join(outDir, 'qrcode.mjs')).href);

const SCALE = 4;
const QUIET = 4;
const SVG_QUIET = 2;

function renderRGBA(matrix) {
  const size = matrix.length;
  const dim = (size + QUIET * 2) * SCALE;
  const data = new Uint8ClampedArray(dim * dim * 4).fill(255);
  for (let row = 0; row < size; row++) {
    for (let col = 0; col < size; col++) {
      if (!matrix[row][col]) continue;
      const x0 = (col + QUIET) * SCALE;
      const y0 = (row + QUIET) * SCALE;
      for (let dy = 0; dy < SCALE; dy++) {
        for (let dx = 0; dx < SCALE; dx++) {
          const index = ((y0 + dy) * dim + (x0 + dx)) * 4;
          data[index] = 0;
          data[index + 1] = 0;
          data[index + 2] = 0;
          data[index + 3] = 255;
        }
      }
    }
  }
  return { data, width: dim, height: dim };
}

function decode(text) {
  const matrix = encodeQR(text, 'M');
  const { data, width, height } = renderRGBA(matrix);
  return { matrix, result: jsQR(data, width, height) };
}

function versionOf(matrix) {
  return (matrix.length - 17) / 4;
}

function svgToMatrix(svg, moduleCount) {
  const cells = new Set();
  const pathMatch = /<path d="([^"]*)"/.exec(svg);
  if (!pathMatch) return null;
  for (const piece of pathMatch[1].trim().split(/\s+/)) {
    const m = /^M(\d+),(\d+)h1v1h-1z$/.exec(piece);
    if (!m) return null;
    const x = Number(m[1]) - SVG_QUIET;
    const y = Number(m[2]) - SVG_QUIET;
    if (x < 0 || y < 0 || x >= moduleCount || y >= moduleCount) return null;
    cells.add(`${y},${x}`);
  }
  const matrix = [];
  for (let row = 0; row < moduleCount; row++) {
    const line = [];
    for (let col = 0; col < moduleCount; col++) line.push(cells.has(`${row},${col}`));
    matrix.push(line);
  }
  return matrix;
}


const cases = [
  { name: 'hello', text: 'hello' },
  { name: '60-char URL', text: 'https://sub.example.com/x'.padEnd(60, 'a') },
  { name: '150-char URL', text: 'https://sub.example.com/sub/'.padEnd(150, 'b'), minVersion: 7 },
  { name: '300-byte URL', text: 'https://sub.example.com/api/v1/subscription?token='.padEnd(300, 'c') },
  { name: 'CJK text', text: '订阅链接测试中文内容' },
  { name: 'emoji text', text: 'test 😀 ok' },
  { name: '1000-char string', text: 'x'.repeat(1000) },
];

let failures = 0;
const pass = (message) => console.log(`PASS ${message}`);
const fail = (message) => {
  failures++;
  console.log(`FAIL ${message}`);
};

const encoder = new TextEncoder();

for (const item of cases) {
  try {
    const expectedBytes = Array.from(encoder.encode(item.text));
    const { matrix, result } = decode(item.text);
    const version = versionOf(matrix);
    const label = `${item.name} (v${version}, ${matrix.length}x${matrix.length}, M)`;
    if (!result) {
      fail(`${label}: not detected by jsQR`);
      continue;
    }
    const bytes = result.binaryData;
    const bytesMatch =
      bytes.length === expectedBytes.length && bytes.every((value, index) => value === expectedBytes[index]);
    if (!bytesMatch) {
      fail(`${label}: byte payload differs from the input`);
      continue;
    }
    if (result.data !== item.text) {
      fail(`${label}: decoded text differs from the input`);
      continue;
    }
    if (item.minVersion && version < item.minVersion) {
      fail(`${label}: expected version >= ${item.minVersion}`);
      continue;
    }
    pass(`${label}: decoded payload matches`);
  } catch (error) {
    fail(`${item.name}: threw ${error && error.message}`);
  }
}

// Every error correction level must round-trip as well (tables L/M/Q/H).
for (const ecl of ['L', 'M', 'Q', 'H']) {
  for (const length of [10, 100, 500]) {
    const text = `ecl-${ecl}-${length}-`.padEnd(length, 'z');
    try {
      const matrix = encodeQR(text, ecl);
      const { data, width, height } = renderRGBA(matrix);
      const result = jsQR(data, width, height);
      if (!result || result.data !== text) {
        fail(`level ${ecl}, ${length} bytes: round-trip failed`);
      } else {
        pass(`level ${ecl}, ${length} bytes (v${versionOf(matrix)}): decoded payload matches`);
      }
    } catch (error) {
      fail(`level ${ecl}, ${length} bytes: threw ${error && error.message}`);
    }
  }
}

// The SVG renderer must draw exactly the matrix produced by the encoder.
try {
  const text = 'https://sub.example.com/qr/svg-check';
  const matrix = encodeQR(text, 'M');
  const moduleCount = matrix.length;
  const grid = moduleCount + SVG_QUIET * 2;
  const svg = generateQRCodeSVG(text, 200);
  const parsed = svgToMatrix(svg, moduleCount);
  const problems = [];
  if (!svg.startsWith('<svg ')) problems.push('missing <svg>');
  if (!svg.includes(`viewBox="0 0 ${grid} ${grid}"`)) problems.push('unexpected viewBox');
  if (!svg.includes('width="200"') || !svg.includes('height="200"')) problems.push('unexpected size');
  if (!svg.includes('fill="white"') || !svg.includes('fill="#1C1C1E"')) problems.push('unexpected colors');
  if (!parsed) problems.push('unparseable path');
  else if (parsed.some((line, row) => line.some((value, col) => value !== matrix[row][col]))) {
    problems.push('path does not match the matrix');
  }
  if (problems.length > 0) fail(`generateQRCodeSVG: ${problems.join(', ')}`);
  else pass(`generateQRCodeSVG (v${versionOf(matrix)}): SVG matches the matrix`);
} catch (error) {
  fail(`generateQRCodeSVG: threw ${error && error.message}`);
}

// 3000 bytes do not fit into version 40 at level M (about 2331 bytes).
try {
  encodeQR('y'.repeat(3000), 'M');
  fail('3000-char string: expected Error("QR data too long")');
} catch (error) {
  if (error && error.message === 'QR data too long') pass('3000-char string: throws "QR data too long"');
  else fail(`3000-char string: wrong error ${error && error.message}`);
}

if (failures > 0) {
  console.log(`\n${failures} QR self-test failure(s)`);
  process.exit(1);
}
console.log('\nAll QR self-tests passed');
