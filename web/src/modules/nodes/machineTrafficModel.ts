import { authState } from '../../app/auth';
import type { MachineTrafficMethod, MachineTrafficSettings, MachineTrafficSummary } from './api';
import { formatBytes } from './components/serverMetricsModel';

// Machine traffic (T1) is the whole server's network usage on one interface,
// not the proxy bytes the cores count. Every display and the operations manual
// carry the same limits; the tooltip may abbreviate, `docs/operations.md` §18
// states them in full.
export const MACHINE_TRAFFIC_LIMITS =
  '整机流量按所选单张网卡统计，包含所有服务；不等于代理流量或供应商账单。' +
  'LXC 只能看到容器自己的网卡，不能统计物理宿主机。首次采样仅建立基线；' +
  'Agent 离线期间若主机重启，跨重启的流量无法补计。同次启动的离线流量可由' +
  '累计计数补计，但跨日分配按时间比例估算。网卡计数重置会重新建立基线。' +
  '自动网卡来自 IPv4 默认路由；策略路由或 IPv6-only 请手动选择。' +
  '修改网卡会重新计算所选网卡的本期数据；修改面板时区不会重新分桶历史数据。';

export const MACHINE_TRAFFIC_METHODS: { value: MachineTrafficMethod; label: string }[] = [
  { value: 'sum', label: '入站 + 出站' },
  { value: 'outbound', label: '仅出站' },
  { value: 'max', label: '入站与出站取较大值' }
];

export const MACHINE_TRAFFIC_UNITS = ['GiB', 'TiB'] as const;
export type MachineTrafficUnit = (typeof MACHINE_TRAFFIC_UNITS)[number];
const UNIT_BYTES: Record<MachineTrafficUnit, number> = { GiB: 1024 ** 3, TiB: 1024 ** 4 };

/** Editable settings form; the allowance is entered as a number plus a binary unit. */
export interface MachineTrafficForm {
  method: MachineTrafficMethod;
  interfaceName: string;
  allowanceValue: string;
  allowanceUnit: MachineTrafficUnit;
  /**
   * The stored allowance. The value above is only its rounded label, so an
   * untouched field is written back byte for byte instead of the rounded number.
   */
  allowanceBytes: number | null;
  resetDay: number;
}

// Byte totals are decimal strings in the API so an int64 never passes through a
// rounded Number; a Number is only ever used for display and progress width.
export function machineBytes(value: string | null | undefined): number | null {
  return typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : null;
}

export function methodLabel(method: MachineTrafficMethod): string {
  return MACHINE_TRAFFIC_METHODS.find(item => item.value === method)?.label || method;
}

function instantLabel(iso: string, timeZone: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '—';
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23'
  }).format(date);
}

/** "11 月 15 日重置" for the period boundary after `iso`, in the panel zone. */
export function resetLabel(iso: string, timeZone?: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return '';
  const parts = new Intl.DateTimeFormat('zh-CN', { timeZone: timeZone || authState.timezone, month: 'numeric', day: 'numeric' })
    .formatToParts(date);
  const part = (type: string) => parts.find(item => item.type === type)?.value || '';
  return `${part('month')} 月 ${part('day')} 日重置`;
}

/** Progress width: only with a known usage and an allowance, clamped to 0..100. */
export function usagePercent(summary?: MachineTrafficSummary | null): number | null {
  const used = machineBytes(summary?.usedBytes);
  const allowance = summary?.settings.allowanceBytes ?? null;
  if (used === null || !allowance) return null;
  return Math.min(100, Math.max(0, Math.round((used / allowance) * 100)));
}

export function usageTooltip(summary?: MachineTrafficSummary | null): string {
  if (!summary) return MACHINE_TRAFFIC_LIMITS;
  const zone = summary.timezone || authState.timezone;
  return [
    `本期 ${instantLabel(summary.periodStart, zone)} – ${instantLabel(summary.periodEnd, zone)}（不含端点，${zone}）`,
    `网卡 ${summary.interfaceName || '—'} · 统计方式 ${methodLabel(summary.settings.method)}`,
    `入站 ${formatBytes(machineBytes(summary.rxBytes))} · 出站 ${formatBytes(machineBytes(summary.txBytes))}`,
    `最后采样 ${summary.lastObservedAt ? instantLabel(summary.lastObservedAt, zone) : '—'}`,
    MACHINE_TRAFFIC_LIMITS
  ].join('\n');
}

function unitFor(bytes: number): MachineTrafficUnit {
  return bytes >= UNIT_BYTES.TiB ? 'TiB' : 'GiB';
}

function numberLabel(value: number): string {
  return String(Number(value.toFixed(3)));
}

export function formFromSettings(settings?: MachineTrafficSettings | null): MachineTrafficForm {
  const allowance = settings?.allowanceBytes ?? null;
  const unit = allowance ? unitFor(allowance) : 'GiB';
  return {
    method: settings?.method ?? 'sum',
    interfaceName: settings?.interfaceName ?? '',
    allowanceValue: allowance ? numberLabel(allowance / UNIT_BYTES[unit]) : '',
    allowanceUnit: unit,
    allowanceBytes: allowance,
    resetDay: settings?.resetDay ?? 1
  };
}

/** Full replacement payload; blank allowance clears it. Throws on invalid input. */
export function payloadFromForm(form: MachineTrafficForm): MachineTrafficSettings {
  const resetDay = Number(form.resetDay);
  if (!Number.isInteger(resetDay) || resetDay < 1 || resetDay > 31) {
    throw new Error('流量重置日需为 1～31 的整数');
  }
  const value = form.allowanceValue.trim();
  let allowanceBytes: number | null = null;
  // The label of a very small allowance reads "0", and the label of a large one
  // loses its last digits. While the inputs still describe the stored value, its
  // exact bytes are kept, so saving an unrelated field cannot move the quota.
  if (form.allowanceBytes !== null && value === numberLabel(form.allowanceBytes / UNIT_BYTES[form.allowanceUnit])) {
    allowanceBytes = form.allowanceBytes;
  } else if (value) {
    const amount = Number(value);
    if (!Number.isFinite(amount) || amount <= 0) throw new Error('额度需为大于 0 的数值，留空表示不设置');
    const bytes = Math.round(amount * UNIT_BYTES[form.allowanceUnit]);
    if (!Number.isSafeInteger(bytes) || bytes < 1) throw new Error('额度需在 1 字节到 9007199254740991 字节之间');
    allowanceBytes = bytes;
  }
  return { method: form.method, interfaceName: form.interfaceName.trim(), allowanceBytes, resetDay };
}
