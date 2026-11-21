const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'] as const;
export type ByteUnit = typeof units[number];

/** Binary storage sizes; callers can retain a fixed unit or display precision. */
export function byteParts(bytes: number | null | undefined, decimals = 1, unit?: ByteUnit, maxUnit: ByteUnit = 'PiB'): { value: string; unit: string } {
  if (bytes == null || !Number.isFinite(bytes)) return { value: '—', unit: '' };
  const index = unit ? units.indexOf(unit) : bytes <= 0 ? 0 : Math.min(units.indexOf(maxUnit), Math.floor(Math.log(bytes) / Math.log(1024)));
  return { value: (bytes / 1024 ** index).toFixed(index === 0 ? 0 : decimals), unit: units[index] };
}

export function formatBytes(bytes: number | null | undefined, decimals = 1, unit?: ByteUnit, maxUnit: ByteUnit = 'PiB'): string {
  const parts = byteParts(bytes, decimals, unit, maxUnit);
  return parts.unit ? parts.value + ' ' + parts.unit : parts.value;
}
