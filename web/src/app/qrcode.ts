// QR Code SVG renderer.
// The encoder lives in ./qr (8-bit byte mode, versions 1-40, levels L/M/Q/H).

import { encodeQR } from './qr/matrix';

export function generateQRCodeSVG(text: string, size = 200): string {
  const matrix = encodeQR(text);
  const moduleCount = matrix.length;
  const quietZone = 2;
  const totalGrid = moduleCount + quietZone * 2;
  let pathD = '';
  for (let r = 0; r < moduleCount; r++) {
    for (let c = 0; c < moduleCount; c++) {
      if (matrix[r][c]) {
        const x = c + quietZone;
        const y = r + quietZone;
        pathD += `M${x},${y}h1v1h-1z `;
      }
    }
  }

  return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${totalGrid} ${totalGrid}" width="${size}" height="${size}" class="rounded-xl shadow-xs"><rect width="100%" height="100%" fill="white"/><path d="${pathD}" fill="#1C1C1E"/></svg>`;
}
