import type { LatencyPoint } from '../api';

export function loss(point: Pick<LatencyPoint, 'sent' | 'failed'>): number | null {
  return point.sent > 0 ? Math.max(0, Math.min(1, point.failed / point.sent)) : null;
}
// The Agent reports the median connect time of each sample; avgMs is only a
// fallback for a panel that fills it.
export function latencyMs(point: LatencyPoint): number | null {
  const value = point.medianMs ?? point.avgMs;
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
}
export function usable(point: LatencyPoint): boolean {
  return point.sent > point.failed && latencyMs(point) !== null;
}
export function segments(points: LatencyPoint[], intervalSeconds: number, start: number, end: number): LatencyPoint[][] {
  const result: LatencyPoint[][] = [];
  let current: LatencyPoint[] = [];
  let previous = -Infinity;
  for (const point of [...points].sort((a, b) => Date.parse(a.at) - Date.parse(b.at))) {
    const at = Date.parse(point.at);
    if (!Number.isFinite(at) || at < start || at > end) continue;
    if (!usable(point) || at - previous > Math.max(1, intervalSeconds) * 1500) {
      if (current.length) result.push(current);
      current = [];
    }
    if (usable(point)) current.push(point);
    previous = at;
  }
  if (current.length) result.push(current);
  return result;
}
export function niceTicks(maximum: number): number[] {
  const max = Number.isFinite(maximum) && maximum > 0 ? maximum : 1;
  const raw = max / 4;
  const base = 10 ** Math.floor(Math.log10(raw));
  const factor = [1, 2, 2.5, 5, 10].find(value => value * base >= raw) ?? 10;
  const step = base * factor;
  const top = Math.ceil(max / step);
  return Array.from({ length: top + 1 }, (_, i) => Number((i * step).toPrecision(12)));
}
export function scale(value: number, from: number, to: number, low: number, high: number): number {
  return to > from ? low + (value - from) / (to - from) * (high - low) : low;
}
export function hourTicks(start: number, end: number, timezone: string, every = 4) {
  const format = new Intl.DateTimeFormat('zh-CN', { timeZone: timezone, hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
  // Align to local whole hours, including zones offset by 30 or 45 minutes.
  const parts = new Intl.DateTimeFormat('en', { timeZone: timezone, minute: '2-digit', second: '2-digit' }).formatToParts(start);
  const minutes = Number(parts.find(part => part.type === 'minute')?.value ?? 0);
  const seconds = Number(parts.find(part => part.type === 'second')?.value ?? 0);
  let at = start - minutes * 60000 - seconds * 1000 - (start % 1000);
  if (at < start) at += 3600000;
  const result = [];
  for (; at <= end; at += Math.max(1, every) * 3600000) result.push({ at, label: format.format(at) });
  return result;
}
