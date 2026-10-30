import { authState } from '../../../app/auth';
import type { NodeMetricPoint, NodeView } from '../api';
import { hostMetrics } from '../metrics';
export interface SystemMetricPoint {
  time: number;
  tcp: number | null;
  udp: number | null;
  processes: number | null;
  load: number | null;
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

function timeParts(timestamp: number): Record<string, string> {
  return Object.fromEntries(new Intl.DateTimeFormat('zh-CN', {
    timeZone: authState.timezone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23'
  }).formatToParts(new Date(timestamp)).map(part => [part.type, part.value]));
}

export function formatTimeHMS(timestamp: number): string {
  const p = timeParts(timestamp);
  return `${p.hour}:${p.minute}:${p.second}`;
}

export function formatTimeHM(timestamp: number): string {
  const p = timeParts(timestamp);
  return `${p.hour}:${p.minute}`;
}

export function formatTimeMD(timestamp: number): string {
  const p = timeParts(timestamp);
  return `${p.month}/${p.day}`;
}

/** Compact chart range: "10/30 09:34 – 10:34" (second date omitted on the same day). */
export function formatMetricRange(start: number, end: number): string {
  const a = timeParts(start), b = timeParts(end);
  const head = `${a.month}/${a.day} ${a.hour}:${a.minute}`;
  const sameDay = a.year === b.year && a.month === b.month && a.day === b.day;
  return sameDay ? `${head} – ${b.hour}:${b.minute}` : `${head} – ${b.month}/${b.day} ${b.hour}:${b.minute}`;
}

/** Single metric value with the same units the chart cards use for the live value. */
export function formatMetricValue(key: MetricKey, value: number | null): string {
  if (value === null || !Number.isFinite(value)) return '—';
  if (key === 'rxRate' || key === 'txRate') return formatRate(value);
  return ['cpu', 'memory', 'disk'].includes(key) ? value.toFixed(1) + '%' : String(value);
}

/** "最低 18.2% · 最高 31.5%" for the visible series; "—" when nothing was measured. */
export function metricRangeLabel(points: SystemMetricPoint[], key: MetricKey): string {
  const values = points.map(point => point[key]).filter((value): value is number => value !== null && Number.isFinite(value));
  if (!values.length) return '最低 — · 最高 —';
  return `最低 ${formatMetricValue(key, Math.min(...values))} · 最高 ${formatMetricValue(key, Math.max(...values))}`;
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
    rxRate: rates('receive'), txRate: rates('transmit'), tcp: decimal(hb?.tcpConnections), udp: decimal(hb?.udpSockets), processes: decimal(hb?.processCount), load: measured(hb?.load1) };
}
// Legacy live-heartbeat adapter retained for consumers.
// Only append a fresh, distinct heartbeat observed while this component is open.
export function sampleHeartbeat(node: NodeView, now: number): SystemMetricPoint | null {
  const time = Date.parse(node.lastHeartbeat);
  if (!Number.isFinite(time) || now - time < -5000 || now - time >= 45000) return null;
  return { time, ...extractCurrentValues(node, now) };
}

function decimal(value: unknown): number | null {
  return typeof value === 'string' && /^\d+$/.test(value) ? measured(Number(value)) : null;
}
export function metricPoint(p: NodeMetricPoint): SystemMetricPoint {
  return {time: Date.parse(p.at), cpu: measured(p.cpuPercent), memory: measured(p.memoryPercent), disk: measured(p.diskPercent), connections: measured(p.activeConnections), rxRate: measured(p.rxBytesPerSecond), txRate: measured(p.txBytesPerSecond), tcp: measured(p.tcpConnections), udp: measured(p.udpSockets), processes: measured(p.processCount), load: measured(p.load1)};
}
export type MetricKey = Exclude<keyof SystemMetricPoint, 'time'>;
export function metricPath(points: SystemMetricPoint[], key: MetricKey, start: number, end: number, intervalSeconds: number): string {
  const max = ['cpu', 'memory', 'disk'].includes(key) ? 100 : Math.max(1, ...points.map(p => p[key] ?? 0));
  let previous: number | null = null;
  return points.map(p => {
    if (p[key] === null) { previous = null; return ''; }
    const command = previous !== null && p.time - previous <= intervalSeconds * 1500 ? 'L' : 'M';
    previous = p.time;
    return command + (10 + (p.time - start) / Math.max(1, end - start) * 300).toFixed(1) + ',' + (90 - p[key]! / max * 80).toFixed(1);
  }).join(' ');
}
