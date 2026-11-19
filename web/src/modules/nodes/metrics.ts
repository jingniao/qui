export interface HostMetrics {
  host_uptime_seconds?: number;
  receive_bytes_per_second?: number;
  transmit_bytes_per_second?: number;
  core_started_at?: { seconds?: number; nanos?: number };
  interfaces?: { name: string; receive_bytes?: number; transmit_bytes?: number; receive_bytes_per_second?: number; transmit_bytes_per_second?: number }[];
  tcp_congestion_control?: string;
  default_qdisc?: string;
}

export function uptime(value?: number): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return '—';
  return `${Math.floor(value / 86400)}天 ${Math.floor(value / 3600) % 24}小时 ${Math.floor(value / 60) % 60}分钟`;
}

export function nicRate(metrics: HostMetrics, direction: 'receive' | 'transmit'): string {
  const aggregate = direction === 'receive' ? metrics.receive_bytes_per_second : metrics.transmit_bytes_per_second;
  if (aggregate !== undefined) return `${(aggregate / 1048576).toFixed(2)} MiB/s`;
  const values = metrics.interfaces?.map(nic => direction === 'receive' ? nic.receive_bytes_per_second : nic.transmit_bytes_per_second);
  if (!values?.length || values.some(value => value === undefined || !Number.isFinite(value))) return '—';
  return `${(values.reduce<number>((total, value) => total + (value ?? 0), 0) / 1048576).toFixed(2)} MiB/s`;
}

export function hostMetrics(node: { lastHeartbeat: string; heartbeat: unknown }, now: number): HostMetrics {
  const age = now - Date.parse(node.lastHeartbeat);
  return age >= -5000 && age < 45000 ? node.heartbeat as HostMetrics : {};
}
