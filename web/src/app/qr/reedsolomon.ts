// GF(256) arithmetic and Reed-Solomon error correction for QR Codes.
// Primitive polynomial: x^8 + x^4 + x^3 + x^2 + 1 (0x11D).

const GF_EXP = new Uint8Array(512);
const GF_LOG = new Uint8Array(256);

{
  let x = 1;
  for (let i = 0; i < 255; i++) {
    GF_EXP[i] = x;
    GF_LOG[x] = i;
    x <<= 1;
    if (x & 0x100) {
      x ^= 0x11d;
    }
  }
  for (let i = 255; i < 512; i++) {
    GF_EXP[i] = GF_EXP[i - 255];
  }
}

/** Multiplies two field elements. */
export function gfMultiply(a: number, b: number): number {
  if (a === 0 || b === 0) {
    return 0;
  }
  return GF_EXP[GF_LOG[a] + GF_LOG[b]];
}

/**
 * Coefficient vector of the generator polynomial
 * (x - r^0)(x - r^1)...(x - r^(degree-1)), highest power first and with the
 * leading 1 omitted.
 */
export function rsDivisor(degree: number): Uint8Array {
  const result = new Uint8Array(degree);
  result[degree - 1] = 1;
  let root = 1;
  for (let i = 0; i < degree; i++) {
    for (let j = 0; j < result.length; j++) {
      result[j] = gfMultiply(result[j], root);
      if (j + 1 < result.length) {
        result[j] ^= result[j + 1];
      }
    }
    root = gfMultiply(root, 0x02);
  }
  return result;
}

/** Remainder of data * x^degree divided by the generator polynomial. */
export function rsRemainder(data: ArrayLike<number>, divisor: Uint8Array): Uint8Array {
  const result = new Uint8Array(divisor.length);
  for (let i = 0; i < data.length; i++) {
    const factor = (data[i] ^ result[0]) & 0xff;
    result.copyWithin(0, 1);
    result[result.length - 1] = 0;
    for (let j = 0; j < result.length; j++) {
      result[j] ^= gfMultiply(divisor[j], factor);
    }
  }
  return result;
}
