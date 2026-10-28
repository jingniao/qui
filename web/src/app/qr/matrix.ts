// QR Code matrix construction: function patterns, data placement, masking.
// Convention: modules[row][col], row 0 is the top row, col 0 the left column.

import { ECL_FORMAT_BITS, EclLevel, getAlignmentPatternPositions } from './tables';
import { buildCodewords, chooseVersion } from './data';
import { penaltyScore } from './penalty';

function makeGrid(size: number, fill: boolean): boolean[][] {
  const grid: boolean[][] = [];
  for (let r = 0; r < size; r++) {
    const row = new Array<boolean>(size);
    row.fill(fill);
    grid.push(row);
  }
  return grid;
}

function maskCondition(mask: number, row: number, col: number): boolean {
  switch (mask) {
    case 0:
      return (row + col) % 2 === 0;
    case 1:
      return row % 2 === 0;
    case 2:
      return col % 3 === 0;
    case 3:
      return (row + col) % 3 === 0;
    case 4:
      return (Math.floor(col / 3) + Math.floor(row / 2)) % 2 === 0;
    case 5:
      return ((row * col) % 2) + ((row * col) % 3) === 0;
    case 6:
      return (((row * col) % 2) + ((row * col) % 3)) % 2 === 0;
    default:
      return (((row + col) % 2) + ((row * col) % 3)) % 2 === 0;
  }
}

/**
 * Encodes text in 8-bit byte mode and returns the module matrix
 * (true = dark). Throws `Error('QR data too long')` when it does not fit
 * into version 40 at the requested error correction level.
 */
export function encodeQR(text: string, ecl: EclLevel = 'M'): boolean[][] {
  const bytes = new TextEncoder().encode(text);
  const version = chooseVersion(bytes.length, ecl);
  const size = version * 4 + 17;
  const modules = makeGrid(size, false);
  const isFunction = makeGrid(size, false);

  const setFn = (row: number, col: number, dark: boolean): void => {
    modules[row][col] = dark;
    isFunction[row][col] = true;
  };

  // Timing patterns (overwritten by the finder patterns in the corners).
  for (let i = 0; i < size; i++) {
    setFn(6, i, i % 2 === 0);
    setFn(i, 6, i % 2 === 0);
  }

  // Finder patterns including their light separators.
  const drawFinder = (centerRow: number, centerCol: number): void => {
    for (let dr = -4; dr <= 4; dr++) {
      for (let dc = -4; dc <= 4; dc++) {
        const row = centerRow + dr;
        const col = centerCol + dc;
        if (row < 0 || row >= size || col < 0 || col >= size) continue;
        const dist = Math.max(Math.abs(dr), Math.abs(dc));
        setFn(row, col, dist !== 2 && dist !== 4);
      }
    }
  };
  drawFinder(3, 3);
  drawFinder(3, size - 4);
  drawFinder(size - 4, 3);

  // Alignment patterns.
  const alignPositions = getAlignmentPatternPositions(version);
  const numAlign = alignPositions.length;
  for (let i = 0; i < numAlign; i++) {
    for (let j = 0; j < numAlign; j++) {
      if (
        (i === 0 && j === 0) ||
        (i === 0 && j === numAlign - 1) ||
        (i === numAlign - 1 && j === 0)
      ) {
        continue;
      }
      const centerRow = alignPositions[i];
      const centerCol = alignPositions[j];
      for (let dr = -2; dr <= 2; dr++) {
        for (let dc = -2; dc <= 2; dc++) {
          setFn(centerRow + dr, centerCol + dc, Math.max(Math.abs(dr), Math.abs(dc)) !== 1);
        }
      }
    }
  }

  // Format information: BCH(15,5) with generator 0x537, XOR mask 0x5412.
  const drawFormatBits = (mask: number): void => {
    const data = (ECL_FORMAT_BITS[ecl] << 3) | mask;
    let rem = data;
    for (let i = 0; i < 10; i++) {
      rem = (rem << 1) ^ ((rem >>> 9) * 0x537);
    }
    const bits = ((data << 10) | rem) ^ 0x5412;
    const bitAt = (i: number): boolean => ((bits >>> i) & 1) !== 0;

    for (let i = 0; i <= 5; i++) setFn(i, 8, bitAt(i));
    setFn(8, 7, bitAt(6));
    setFn(8, 8, bitAt(7));
    setFn(7, 8, bitAt(8));
    for (let i = 9; i < 15; i++) setFn(8, 14 - i, bitAt(i));

    for (let i = 0; i < 8; i++) setFn(8, size - 1 - i, bitAt(i));
    for (let i = 8; i < 15; i++) setFn(size - 15 + i, 8, bitAt(i));
    setFn(size - 8, 8, true);
  };
  drawFormatBits(0); // reserves the format area before data placement

  // Version information for v7+: BCH(18,6) with generator 0x1F25, two copies.
  if (version >= 7) {
    let rem = version;
    for (let i = 0; i < 12; i++) {
      rem = (rem << 1) ^ ((rem >>> 11) * 0x1f25);
    }
    const bits = (version << 12) | rem;
    for (let i = 0; i < 18; i++) {
      const dark = ((bits >>> i) & 1) !== 0;
      const a = size - 11 + (i % 3);
      const b = Math.floor(i / 3);
      setFn(b, a, dark);
      setFn(a, b, dark);
    }
  }

  // Data placement in the zig-zag order, skipping function modules.
  const codewords = buildCodewords(bytes, version, ecl);
  const totalBits = codewords.length * 8;
  let bitIndex = 0;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right = 5; // skip the vertical timing column
    const upward = ((right + 1) & 2) === 0;
    for (let vert = 0; vert < size; vert++) {
      for (let j = 0; j < 2; j++) {
        const col = right - j;
        const row = upward ? size - 1 - vert : vert;
        if (!isFunction[row][col] && bitIndex < totalBits) {
          const dark =
            ((codewords[bitIndex >>> 3] >>> (7 - (bitIndex & 7))) & 1) !== 0;
          modules[row][col] = dark;
          bitIndex++;
        }
      }
    }
  }

  // Try all eight masks, keep the one with the lowest penalty.
  const applyMask = (mask: number): void => {
    for (let row = 0; row < size; row++) {
      for (let col = 0; col < size; col++) {
        if (!isFunction[row][col] && maskCondition(mask, row, col)) {
          modules[row][col] = !modules[row][col];
        }
      }
    }
  };

  let bestMask = 0;
  let bestScore = Infinity;
  for (let mask = 0; mask < 8; mask++) {
    applyMask(mask);
    drawFormatBits(mask);
    const score = penaltyScore(modules);
    if (score < bestScore) {
      bestScore = score;
      bestMask = mask;
    }
    applyMask(mask);
  }
  applyMask(bestMask);
  drawFormatBits(bestMask);

  return modules;
}
