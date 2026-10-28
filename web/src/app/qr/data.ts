// Byte-mode data encoding, block splitting and interleaving for QR Codes.

import {
  EclLevel,
  EC_CODEWORDS_PER_BLOCK,
  NUM_EC_BLOCKS,
  byteModeCharCountBits,
  getNumDataCodewords,
  getNumRawDataModules,
} from './tables';
import { rsDivisor, rsRemainder } from './reedsolomon';

const MODE_BYTE = 0x4;
const TERMINATOR_MAX_BITS = 4;

/** Smallest version 1-40 holding the byte payload, or throws. */
export function chooseVersion(byteLength: number, ecl: EclLevel): number {
  for (let version = 1; version <= 40; version++) {
    const capacityBits = getNumDataCodewords(version, ecl) * 8;
    const requiredBits = 4 + byteModeCharCountBits(version) + byteLength * 8;
    if (requiredBits <= capacityBits) {
      return version;
    }
  }
  throw new Error('QR data too long');
}

/** Builds the data codewords (no error correction) of one version. */
export function buildDataCodewords(bytes: Uint8Array, version: number, ecl: EclLevel): number[] {
  const capacityBits = getNumDataCodewords(version, ecl) * 8;
  const bits: number[] = [];

  const appendBits = (value: number, length: number): void => {
    for (let i = length - 1; i >= 0; i--) {
      bits.push((value >>> i) & 1);
    }
  };

  appendBits(MODE_BYTE, 4);
  appendBits(bytes.length, byteModeCharCountBits(version));
  for (let i = 0; i < bytes.length; i++) {
    appendBits(bytes[i], 8);
  }

  // Terminator, then bit padding up to a whole codeword.
  appendBits(0, Math.min(TERMINATOR_MAX_BITS, capacityBits - bits.length));
  appendBits(0, (8 - (bits.length % 8)) % 8);

  // Pad codewords.
  for (let pad = 0xec; bits.length < capacityBits; pad ^= 0xec ^ 0x11) {
    appendBits(pad, 8);
  }

  const codewords: number[] = [];
  for (let i = 0; i < bits.length; i += 8) {
    let value = 0;
    for (let j = 0; j < 8; j++) {
      value = (value << 1) | bits[i + j];
    }
    codewords.push(value);
  }
  return codewords;
}

/**
 * Splits the data codewords into blocks, appends Reed-Solomon codewords to each
 * block and interleaves everything into the final codeword sequence.
 */
export function buildCodewords(bytes: Uint8Array, version: number, ecl: EclLevel): number[] {
  const data = buildDataCodewords(bytes, version, ecl);
  const numBlocks = NUM_EC_BLOCKS[ecl][version];
  const eccPerBlock = EC_CODEWORDS_PER_BLOCK[ecl][version];
  const rawCodewords = Math.floor(getNumRawDataModules(version) / 8);
  const numShortBlocks = numBlocks - (rawCodewords % numBlocks);
  const shortBlockLen = Math.floor(rawCodewords / numBlocks);

  const divisor = rsDivisor(eccPerBlock);
  const blocks: number[][] = [];
  for (let i = 0, offset = 0; i < numBlocks; i++) {
    const dataLen = shortBlockLen - eccPerBlock + (i < numShortBlocks ? 0 : 1);
    const blockData = data.slice(offset, offset + dataLen);
    offset += dataLen;
    const ecc = rsRemainder(blockData, divisor);
    const block = blockData.slice();
    // Every block has shortBlockLen + 1 slots: data, one padding slot for short
    // blocks, then the ECC codewords at the end.
    while (block.length < shortBlockLen + 1 - eccPerBlock) {
      block.push(0);
    }
    for (let j = 0; j < eccPerBlock; j++) {
      block.push(ecc[j]);
    }
    blocks.push(block);
  }

  const result: number[] = [];
  for (let i = 0; i < shortBlockLen + 1; i++) {
    for (let j = 0; j < blocks.length; j++) {
      if (i !== shortBlockLen - eccPerBlock || j >= numShortBlocks) {
        result.push(blocks[j][i]);
      }
    }
  }
  return result;
}
