import type { LatencyPoint } from '../api';
import { latencyMs, loss, rollingMedian, usable } from './latencySeries';
import type { AxisUnit } from './serverMetricsModel';

/** Target colours (hex: Chart.js draws on a canvas and cannot use Tailwind classes). */
export const TARGET_COLORS = [
  { stroke: '#f43f5e', bar: 'bg-rose-500' },
  { stroke: '#06b6d4', bar: 'bg-teal-500' },
  { stroke: '#8b5cf6', bar: 'bg-purple-500' },
  { stroke: '#3b82f6', bar: 'bg-blue-500' },
  { stroke: '#f59e0b', bar: 'bg-amber-500' }
] as const;

export function targetColor(index: number) {
  return TARGET_COLORS[(index >= 0 ? index : 0) % TARGET_COLORS.length];
}

export interface LatencyChartDataset {
  label: string;
  color: string;
  fill: boolean;
  axis: 'y' | 'y1';
  dash?: number[];
  data: { x: number; y: number | null }[];
  format: (value: number) => string;
}

/** One chart line with a break (null) at unusable samples and across gaps over 1.5 x the step. */
export function chartLine(
  points: LatencyPoint[],
  intervalSeconds: number,
  value: (point: LatencyPoint) => number | null
): { x: number; y: number | null }[] {
  const limit = Math.max(1, intervalSeconds) * 1500;
  const out: { x: number; y: number | null }[] = [];
  let previous: number | null = null;
  for (const point of [...points].sort((a, b) => Date.parse(a.at) - Date.parse(b.at))) {
    const at = Date.parse(point.at);
    if (!Number.isFinite(at)) continue;
    const v = value(point);
    if (v === null) { out.push({ x: at, y: null }); previous = null; continue; }
    if (previous !== null && at - previous > limit) out.push({ x: (previous + at) / 2, y: null });
    out.push({ x: at, y: v });
    previous = at;
  }
  return out;
}

export const formatLatencyValue = (v: number) => `${Number.isInteger(v) ? v : v.toFixed(1)} ms`;
export const formatLossValue = (v: number) => `${Number.isInteger(v) ? v : v.toFixed(1)}%`;

export interface LatencyChartOptions {
  showLatency: boolean;
  showLoss: boolean;
  showSmooth: boolean;
}

/**
 * Chart datasets for the visible targets. Latency lines use the left axis; loss
 * (percent of failed probes per sample) is a dashed line on the right axis.
 * Smoothing only changes the latency line: loss and the card statistics always
 * come from the raw samples.
 */
export function buildLatencyDatasets(
  targets: { target: { id: string; label: string }; colorIndex: number; points: LatencyPoint[] }[],
  intervalSeconds: number,
  options: LatencyChartOptions
): LatencyChartDataset[] {
  const out: LatencyChartDataset[] = [];
  for (const { target, colorIndex, points } of targets) {
    const color = targetColor(colorIndex).stroke;
    if (options.showLatency) {
      const shown = options.showSmooth ? rollingMedian(points, 5) : points;
      out.push({
        label: options.showLoss ? `${target.label} 延迟` : target.label, color, fill: !options.showLoss, axis: 'y',
        data: chartLine(shown, intervalSeconds, p => (usable(p) ? latencyMs(p) : null)), format: formatLatencyValue
      });
    }
    if (options.showLoss) {
      out.push({
        label: options.showLatency ? `${target.label} 丢包` : target.label, color, fill: false, axis: options.showLatency ? 'y1' : 'y', dash: [4, 3],
        data: chartLine(points, intervalSeconds, p => { const l = loss(p); return l === null ? null : l * 100; }), format: formatLossValue
      });
    }
  }
  return out;
}

export function latencyAxes(options: LatencyChartOptions): { y: { unit: AxisUnit; max?: number }; y1?: { unit: AxisUnit; max?: number } } {
  if (options.showLatency && options.showLoss) return { y: { unit: 'ms' }, y1: { unit: 'percent', max: 100 } };
  if (options.showLoss) return { y: { unit: 'percent', max: 100 } };
  return { y: { unit: 'ms' } };
}
