// Host scalar zero means unknown; optional heartbeat zero is measured.
export function uint64(value: unknown): number | null {
  if (typeof value !== 'string' || !/^\d+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 0 ? parsed : null;
}
export function observed(value: unknown): string {
  if (typeof value === 'number') return Number.isFinite(value) ? String(value) : '—';
  const parsed = uint64(value);
  return parsed === null ? '—' : String(parsed);
}
function bytes(value: number): string {
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const i = value === 0 ? 0 : Math.min(5, Math.floor(Math.log(value) / Math.log(1024)));
  return (value / 1024 ** i).toFixed(i ? 1 : 0) + ' ' + units[i];
}
export function storageDisplay(used: unknown, total: unknown, percent?: number): string {
  const u = uint64(used), t = uint64(total);
  if (u !== null && t !== null && t > 0) return bytes(u) + ' / ' + bytes(t);
  return typeof percent === 'number' && Number.isFinite(percent) ? '占用率 ' + percent.toFixed(1) + '%' : '—';
}
