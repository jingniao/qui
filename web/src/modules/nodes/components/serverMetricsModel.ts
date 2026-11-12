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
  retransRate?: number | null;
  memoryBytes: number | null;
  swapBytes: number | null;
  diskBytes: number | null;
}
export function formatBytes(bytes: number | null | undefined, decimals = 1): string {
  if (bytes == null) return '—';
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  const k = 1024;
  const i = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(k)));
  const val = bytes / Math.pow(k, i);
  return `${val.toFixed(i === 0 ? 0 : decimals)} ${units[i]}`;
}

export function formatRate(bytesPerSec: number | null | undefined, decimals = 1): string {
  if (bytesPerSec == null) return '—';
  if (!Number.isFinite(bytesPerSec) || bytesPerSec <= 0) return '0 B/s';
  const units = ['B/s', 'KiB/s', 'MiB/s', 'GiB/s'];
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

/** Tooltip title: full local time in the panel timezone. */
export function formatTimeFull(timestamp: number): string {
  const p = timeParts(timestamp);
  return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`;
}

/** x-axis tick: time only up to a day, date and time beyond. */
export function formatXTick(timestamp: number, spanMs: number): string {
  return spanMs > 86400000 ? `${formatTimeMD(timestamp)} ${formatTimeHM(timestamp)}` : formatTimeHM(timestamp);
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
  if (key === 'memoryBytes' || key === 'swapBytes' || key === 'diskBytes') return formatBytes(value);
  if (key === 'retransRate') return value.toFixed(2) + '%';
  if (key === 'load') return value.toFixed(2);
  if (['cpu', 'memory', 'disk'].includes(key)) return value.toFixed(1) + '%';
  return Number.isInteger(value) ? String(value) : value.toFixed(1);
}

export type AxisUnit = 'percent' | 'bytes' | 'rate' | 'count' | 'load' | 'retrans' | 'ms';
/** Compact y-axis tick label. */
export function formatAxisTick(unit: AxisUnit, value: number): string {
  if (unit === 'percent') return `${value}%`;
  if (unit === 'ms') return `${Number(value.toFixed(2))} ms`;
  if (unit === 'bytes') return formatBytes(value);
  if (unit === 'rate') return formatRate(value);
  if (unit === 'retrans') return `${Number(value.toFixed(2))}%`;
  return String(Number(value.toFixed(2)));
}

/** "最低 18.2% · 最高 31.5%" for the visible series; "—" when nothing was measured. */
export function metricRangeLabel(points: SystemMetricPoint[], key: MetricKey): string {
  const values = points.map(point => point[key]).filter((value): value is number => value !== null && value !== undefined && Number.isFinite(value));
  if (!values.length) return '最低 — · 最高 —';
  return `最低 ${formatMetricValue(key, Math.min(...values))} · 最高 ${formatMetricValue(key, Math.max(...values))}`;
}

/**
 * Chart.js draws on a canvas and cannot use Tailwind classes, so chart colours
 * live here as hex copies of the semantic `colors.ios` values in
 * tailwind.config.js. This is the only place chart colours are defined.
 */
export const CHART_COLORS = {
  blue: '#0071E3', green: '#34C759', orange: '#FF9500', purple: '#AF52DE', red: '#FF3B30', teal: '#5AC8FA', indigo: '#5856D6',
  tick: '#6E6E73', grid: 'rgba(0, 0, 0, 0.08)', tooltipBg: '#1D1D1F'
} as const;

export interface MetricSeries { key: MetricKey; label: string; color: 'blue' | 'green' | 'orange' | 'purple' | 'red' | 'teal' | 'indigo'; fill?: boolean; axis?: 'y' | 'y1'; optional?: boolean }
export interface MetricCard {
  id: string; title: string; series: MetricSeries[];
  axes: { y: { unit: AxisUnit; max?: number }; y1?: { unit: AxisUnit } };
  /** Key whose min/max summary is shown in the footer; omitted for multi-series cards. */
  footerKey?: MetricKey;
}
export const metricCards: MetricCard[] = [
  { id: 'cpu', title: 'CPU', footerKey: 'cpu', axes: { y: { unit: 'percent', max: 100 }, y1: { unit: 'load' } }, series: [
    { key: 'cpu', label: 'CPU', color: 'blue', fill: true }, { key: 'load', label: '1 分钟负载', color: 'orange', axis: 'y1' }] },
  { id: 'memory', title: '内存', footerKey: 'memoryBytes', axes: { y: { unit: 'bytes' } }, series: [
    { key: 'memoryBytes', label: '已用', color: 'green', fill: true }, { key: 'swapBytes', label: '交换已用', color: 'orange', optional: true }] },
  { id: 'disk', title: '磁盘', footerKey: 'diskBytes', axes: { y: { unit: 'bytes' } }, series: [
    { key: 'diskBytes', label: '已用', color: 'purple', fill: true }] },
  { id: 'network', title: '网络', axes: { y: { unit: 'rate' } }, series: [
    { key: 'rxRate', label: '下载', color: 'blue' }, { key: 'txRate', label: '上传', color: 'green' }] },
  { id: 'connections', title: '连接', axes: { y: { unit: 'count' } }, series: [
    { key: 'tcp', label: 'TCP（全部状态）', color: 'blue' }, { key: 'udp', label: 'UDP 套接字', color: 'teal' },
    { key: 'connections', label: '代理活跃会话', color: 'orange', optional: true }] },
  { id: 'processes', title: '进程数', footerKey: 'processes', axes: { y: { unit: 'count' } }, series: [
    { key: 'processes', label: '进程数', color: 'indigo', fill: true }] },
  { id: 'retrans', title: 'TCP 重传率', footerKey: 'retransRate', axes: { y: { unit: 'retrans' } }, series: [
    { key: 'retransRate', label: 'TCP 重传率', color: 'red' }] }
];

const isMeasured = (value: unknown): value is number => typeof value === 'number' && Number.isFinite(value);
/** Series worth drawing: required ones always, optional ones only when measured in range. */
export function visibleSeries(card: MetricCard, points: SystemMetricPoint[]): MetricSeries[] {
  return card.series.filter(s => !s.optional || points.some(p => isMeasured(p[s.key])));
}

/**
 * Chart.js line data with "缺测处断线": a null y is emitted for unmeasured
 * points and between neighbours more than 1.5 x the sampling step apart.
 */
export function seriesData(points: SystemMetricPoint[], key: MetricKey, intervalSeconds: number): { x: number; y: number | null }[] {
  const limit = intervalSeconds * 1500;
  const out: { x: number; y: number | null }[] = [];
  let previous: number | null = null;
  for (const p of points) {
    const v = p[key];
    if (!isMeasured(v)) { out.push({ x: p.time, y: null }); previous = null; continue; }
    if (previous !== null && p.time - previous > limit) out.push({ x: (previous + p.time) / 2, y: null });
    out.push({ x: p.time, y: v });
    previous = p.time;
  }
  return out;
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
    rxRate: rates('receive'), txRate: rates('transmit'), tcp: decimal(hb?.tcpConnections), udp: decimal(hb?.udpSockets), processes: decimal(hb?.processCount), load: measured(hb?.load1), retransRate: null,
    memoryBytes: decimal(hb?.memoryUsedBytes), swapBytes: decimal(hb?.swapUsedBytes), diskBytes: decimal(hb?.diskUsedBytes) };
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
  return {
    time: Date.parse(p.at),
    cpu: measured(p.cpuPercent),
    memory: measured(p.memoryPercent),
    disk: measured(p.diskPercent),
    connections: measured(p.activeConnections),
    rxRate: measured(p.rxBytesPerSecond),
    txRate: measured(p.txBytesPerSecond),
    tcp: measured(p.tcpConnections),
    udp: measured(p.udpSockets),
    processes: measured(p.processCount),
    load: measured(p.load1),
    retransRate: measured(p.tcpRetransPercent),
    memoryBytes: measured(p.memoryUsedBytes),
    swapBytes: measured(p.swapUsedBytes),
    diskBytes: measured(p.diskUsedBytes)
  };
}
export type MetricKey = Exclude<keyof SystemMetricPoint, 'time'>;
