// Mask penalty scoring for QR Codes (ISO/IEC 18004 rules N1-N4).

const N1 = 3;
const N2 = 3;
const N3 = 40;
const N4 = 10;

// 1:1:3:1:1 finder-like run preceded or followed by four light modules,
// expressed over an 11 module window.
const FINDER_DARK_FIRST = [true, false, true, true, true, false, true, false, false, false, false];
const FINDER_LIGHT_FIRST = [false, false, false, false, true, false, true, true, true, false, true];

function finderLikeCount(line: boolean[]): number {
  let count = 0;
  for (let i = 0; i + 11 <= line.length; i++) {
    let matchDarkFirst = true;
    let matchLightFirst = true;
    for (let j = 0; j < 11; j++) {
      const value = line[i + j];
      if (value !== FINDER_DARK_FIRST[j]) matchDarkFirst = false;
      if (value !== FINDER_LIGHT_FIRST[j]) matchLightFirst = false;
      if (!matchDarkFirst && !matchLightFirst) break;
    }
    if (matchDarkFirst) count++;
    if (matchLightFirst) count++;
  }
  return count;
}

function runPenalty(line: boolean[]): number {
  let result = 0;
  let run = 1;
  for (let i = 1; i < line.length; i++) {
    if (line[i] === line[i - 1]) {
      run++;
    } else {
      if (run >= 5) result += N1 + (run - 5);
      run = 1;
    }
  }
  if (run >= 5) result += N1 + (run - 5);
  return result;
}

/** Total penalty of a finished (masked, format info drawn) matrix. */
export function penaltyScore(modules: boolean[][]): number {
  const size = modules.length;
  let result = 0;
  const column: boolean[] = new Array<boolean>(size);

  for (let r = 0; r < size; r++) {
    const row = modules[r];
    result += runPenalty(row) + N3 * finderLikeCount(row);
    for (let c = 0; c < size; c++) {
      column[c] = modules[c][r];
    }
    result += runPenalty(column) + N3 * finderLikeCount(column);
  }

  let dark = 0;
  for (let r = 0; r < size; r++) {
    for (let c = 0; c < size; c++) {
      const value = modules[r][c];
      if (value) dark++;
      if (
        r + 1 < size &&
        c + 1 < size &&
        value === modules[r][c + 1] &&
        value === modules[r + 1][c] &&
        value === modules[r + 1][c + 1]
      ) {
        result += N2;
      }
    }
  }

  const total = size * size;
  result += N4 * Math.floor(Math.abs(dark * 20 - total * 10) / total);
  return result;
}
