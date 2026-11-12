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

export interface SeriesStats {
  meanMs: number | null;
  lossPercent: number | null;
  jitterMs: number | null;
  totalSent: number;
  totalFailed: number;
  usableCount: number;
}

export function seriesStats(points: LatencyPoint[]): SeriesStats {
  const sorted = [...points].sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  let totalSent = 0;
  let totalFailed = 0;
  const usableMedians: number[] = [];

  for (const point of sorted) {
    totalSent += point.sent || 0;
    totalFailed += point.failed || 0;
    if (usable(point)) {
      const ms = latencyMs(point);
      if (ms !== null) usableMedians.push(ms);
    }
  }

  const usableCount = usableMedians.length;
  const meanMs = usableCount > 0 ? usableMedians.reduce((a, b) => a + b, 0) / usableCount : null;
  const lossPercent = totalSent > 0 ? Math.min(100, Math.max(0, (totalFailed / totalSent) * 100)) : null;

  let jitterMs: number | null = null;
  if (usableCount >= 2) {
    let diffSum = 0;
    for (let i = 1; i < usableCount; i++) {
      diffSum += Math.abs(usableMedians[i] - usableMedians[i - 1]);
    }
    jitterMs = diffSum / (usableCount - 1);
  }

  return {
    meanMs,
    lossPercent,
    jitterMs,
    totalSent,
    totalFailed,
    usableCount
  };
}

export const targetStats = seriesStats;

export function formatStatsMean(meanMs: number | null): string {
  if (meanMs === null) return '—';
  return `${Math.round(meanMs)}ms`;
}

export function formatStatsLoss(lossPercent: number | null): string {
  if (lossPercent === null) return '—';
  return `${lossPercent.toFixed(2)}%`;
}

export function formatStatsJitter(jitterMs: number | null): string {
  if (jitterMs === null) return '—';
  return jitterMs.toFixed(2);
}

export function rollingMedian(points: LatencyPoint[], windowSize = 5): LatencyPoint[] {
  const sorted = [...points].sort((a, b) => Date.parse(a.at) - Date.parse(b.at));
  const half = Math.floor(windowSize / 2);

  return sorted.map((point, index) => {
    if (!usable(point)) return { ...point };
    const windowValues: number[] = [];
    const startIdx = Math.max(0, index - half);
    const endIdx = Math.min(sorted.length - 1, index + half);

    for (let i = startIdx; i <= endIdx; i++) {
      const p = sorted[i];
      if (usable(p)) {
        const ms = latencyMs(p);
        if (ms !== null) windowValues.push(ms);
      }
    }

    if (windowValues.length === 0) return { ...point };
    windowValues.sort((a, b) => a - b);
    const len = windowValues.length;
    const smoothed = len % 2 === 1
      ? windowValues[Math.floor(len / 2)]
      : (windowValues[len / 2 - 1] + windowValues[len / 2]) / 2;

    return {
      ...point,
      medianMs: smoothed
    };
  });
}

export function nearestPoint(points: LatencyPoint[], targetMs: number): LatencyPoint | null {
  if (!points.length || !Number.isFinite(targetMs)) return null;
  let closest = points[0];
  let minDiff = Math.abs(Date.parse(points[0].at) - targetMs);
  for (let i = 1; i < points.length; i++) {
    const at = Date.parse(points[i].at);
    if (!Number.isFinite(at)) continue;
    const diff = Math.abs(at - targetMs);
    if (diff < minDiff) {
      minDiff = diff;
      closest = points[i];
    }
  }
  return closest;
}

export function describePointLatency(point: LatencyPoint, isBucket: boolean): string {
  const value = latencyMs(point);
  if (value === null) return '—';
  const msText = `${Number.isInteger(value) ? value : value.toFixed(2)} ms`;
  if (isBucket && point.minMs != null && point.maxMs != null) {
    const minText = Number.isInteger(point.minMs) ? String(point.minMs) : point.minMs.toFixed(2);
    const maxText = Number.isInteger(point.maxMs) ? String(point.maxMs) : point.maxMs.toFixed(2);
    return `平均中位 ${msText}（区间 ${minText}–${maxText}）`;
  }
  return `中位 ${msText}`;
}
