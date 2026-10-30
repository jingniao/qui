import type { NodeView } from '../api';
import { hostMetrics } from '../metrics';
export interface SystemMetricPoint {
  time: number;
  cpu: number | null;
  memory: number | null;
  disk: number | null;
  connections: number | null;
  rxRate: number | null;
  txRate: number | null;
}
export function formatBytes(bytes: number | null | undefined, decimals = 1): string {
  if (bytes == null) return '—';
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  const k = 1024;
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
  const val = bytes / Math.pow(k, i);
  return `${val.toFixed(i === 0 ? 0 : decimals)} ${units[i]}`;
}

export function formatRate(bytesPerSec: number | null | undefined, decimals = 1): string {
  if (bytesPerSec == null) return '—';
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return '0 B/s';
  const units = ['B/s', 'KB/s', 'MB/s', 'GB/s'];
  const k = 1024;
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytesPerSec) / Math.log(k)));
  const val = bytesPerSec / Math.pow(k, i);
  return `${val.toFixed(i === 0 ? 0 : decimals)} ${units[i]}`;
}

export function formatTimeHMS(timestamp: number): string {
  const d = new Date(timestamp);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

export function formatTimeHM(timestamp: number): string {
  const d = new Date(timestamp);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export function formatTimeMD(timestamp: number): string {
  const d = new Date(timestamp);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())}`;
}

export function scaleLinear(val: number, inMin: number, inMax: number, outMin: number, outMax: number): number {
  if (inMax <= inMin) return outMax;
  const clamped = Math.max(inMin, Math.min(inMax, val));
  return outMin + ((clamped - inMin) / (inMax - inMin)) * (outMax - outMin);
}

export function buildSvgPath(points: { x: number; y: number }[]): string {
  if (!points.length) return '';
  return points.map((p, i) => `${i === 0 ? 'M' : 'L'} ${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' ');
}

export function buildAreaSvgPath(points: { x: number; y: number }[], baseY: number): string {
  if (!points.length) return '';
  const line = buildSvgPath(points);
  const first = points[0];
  const last = points[points.length - 1];
  return `${line} L ${last.x.toFixed(1)},${baseY} L ${first.x.toFixed(1)},${baseY} Z`;
}

export function niceStepCeil(maxVal: number, steps = 5): number {
  if (!Number.isFinite(maxVal) || maxVal <= 0) return 10;
  const raw = maxVal / steps;
  const mag = Math.pow(10, Math.floor(Math.log10(raw)));
  const norm = raw / mag;
  let niceNorm = 10;
  if (norm <= 1) niceNorm = 1;
  else if (norm <= 2) niceNorm = 2;
  else if (norm <= 2.5) niceNorm = 2.5;
  else if (norm <= 5) niceNorm = 5;
  return niceNorm * mag * steps;
}


const measured = (value: unknown): number | null => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : null;
export function extractCurrentValues(node: NodeView, now = Date.now()): Omit<SystemMetricPoint, 'time'> {
  const fresh = hostMetrics(node, now);
  const hb = Object.keys(fresh).length ? node.heartbeat : undefined;
  const rates = (direction: 'receive' | 'transmit') => {
    const values = fresh.interfaces?.map(nic => measured(nic[direction === 'receive' ? 'receive_bytes_per_second' : 'transmit_bytes_per_second']));
    return values?.length && values.every(v => v !== null) ? values.reduce<number>((sum, v) => sum + (v ?? 0), 0) : null;
  };
  return { cpu: measured(hb?.cpu_percent), memory: measured(hb?.memory_percent), disk: measured(hb?.disk_percent),
    connections: hb?.active_connections_measured === true ? measured(hb.active_connections) : null,
    rxRate: rates('receive'), txRate: rates('transmit') };
}
// Adapter boundary: a future GET /api/v1/nodes/{id}/metrics can return these points.
// Only append a fresh, distinct heartbeat observed while this component is open.
export function sampleHeartbeat(node: NodeView, now: number): SystemMetricPoint | null {
  const time = Date.parse(node.lastHeartbeat);
  if (!Number.isFinite(time) || now - time < -5000 || now - time >= 45000) return null;
  return { time, ...extractCurrentValues(node, now) };
}
