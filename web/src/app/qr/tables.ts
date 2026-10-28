// QR Code capacity tables and geometry helpers.
// Values follow the Nayuki QR Code generator reference implementation
// (https://www.nayuki.io/page/qr-code-generator-library).

export type EclLevel = 'L' | 'M' | 'Q' | 'H';

/** Format-information value (2 bits) of each error correction level. */
export const ECL_FORMAT_BITS: Readonly<Record<EclLevel, number>> = {
  L: 1,
  M: 0,
  Q: 3,
  H: 2,
};

/** Error correction codewords per block, indexed by version (index 0 unused). */
export const EC_CODEWORDS_PER_BLOCK: Readonly<Record<EclLevel, readonly number[]>> = {
  //       0   1   2   3   4   5   6   7   8   9  10  11  12  13  14  15  16  17  18  19  20  21  22  23  24  25  26  27  28  29  30  31  32  33  34  35  36  37  38  39  40
  L: [-1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  M: [-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28],
  Q: [-1, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
  H: [-1, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30],
};

/** Number of error correction blocks, indexed by version (index 0 unused). */
export const NUM_EC_BLOCKS: Readonly<Record<EclLevel, readonly number[]>> = {
  //       0   1   2   3   4   5   6   7   8   9  10  11  12  13  14  15  16  17  18  19  20  21  22  23  24  25  26  27  28  29  30  31  32  33  34  35  36  37  38  39  40
  L: [-1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25],
  M: [-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49],
  Q: [-1, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68],
  H: [-1, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81],
};

/**
 * Number of data + error correction modules of a version, excluding all
 * function patterns and format/version information areas.
 */
export function getNumRawDataModules(version: number): number {
  let result = (16 * version + 128) * version + 64;
  if (version >= 2) {
    const numAlign = Math.floor(version / 7) + 2;
    result -= (25 * numAlign - 10) * numAlign - 55;
    if (version >= 7) {
      result -= 36;
    }
  }
  return result;
}

/** Number of usable data codewords (all blocks) for a version and level. */
export function getNumDataCodewords(version: number, ecl: EclLevel): number {
  const eccPerBlock = EC_CODEWORDS_PER_BLOCK[ecl][version];
  const numBlocks = NUM_EC_BLOCKS[ecl][version];
  return Math.floor(getNumRawDataModules(version) / 8) - eccPerBlock * numBlocks;
}

/** Character count indicator width for 8-bit byte mode. */
export function byteModeCharCountBits(version: number): number {
  return version <= 9 ? 8 : 16;
}

/** Alignment pattern centre coordinates, ascending, for a version. */
export function getAlignmentPatternPositions(version: number): number[] {
  if (version === 1) {
    return [];
  }
  const size = version * 4 + 17;
  const numAlign = Math.floor(version / 7) + 2;
  const step = version === 32 ? 26 : Math.floor((version * 4 + numAlign * 2 + 1) / (numAlign * 2 - 2)) * 2;
  const result: number[] = [6];
  for (let i = numAlign - 1, pos = size - 7; i >= 1; i--, pos -= step) {
    result[i] = pos;
  }
  return result;
}
