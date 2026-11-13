// Small shared helpers for the user portal pages.

/** Binary units with automatic scaling: B / KiB / MiB / GiB / TiB. */
export function formatBytes(value: number | null | undefined, decimals = 2): string {
  if (value == null || !Number.isFinite(value)) return '—';
  if (value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  const i = Math.min(units.length - 1, Math.floor(Math.log(value) / Math.log(1024)));
  const scaled = value / Math.pow(1024, i);
  return `${scaled.toFixed(i === 0 ? 0 : decimals)} ${units[i]}`;
}

/** Whole days from `now` until `value` (rounded up); null when there is no date. Negative when past. */
export function daysLeft(value: string | null | undefined, now = Date.now()): number | null {
  if (!value) return null;
  const time = new Date(value).getTime();
  if (Number.isNaN(time)) return null;
  return Math.ceil((time - now) / 86400000);
}

// Portal sessions do not expose a timezone; use zh-CN in the browser timezone.
export function formatDateTime(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'short', timeStyle: 'medium', hour12: false }).format(date);
}

export function formatDate(value: string | null | undefined): string {
  if (!value) return '—';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? '—' : new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium' }).format(date);
}

export function resetCycleLabel(cycle: string | null | undefined): string {
  if (cycle === 'monthly') return '每月重置';
  if (cycle === '30days') return '每 30 天重置';
  return '';
}

export function quotaPercent(used: number, limit: number): number {
  return limit > 0 ? Math.min(100, Math.max(0, used / limit * 100)) : 0;
}

export const PASSWORD_RULE = '密码长度为 12–128 个字符。';

export const PROTOCOL_LABELS: Record<string, string> = {
  vless: 'VLESS', trojan: 'Trojan', shadowsocks: 'Shadowsocks', socks5: 'SOCKS5', http: 'HTTP',
  hysteria2: 'Hysteria2', tuic: 'TUIC', wireguard: 'WireGuard', forward: '端口转发'
};
export function protocolLabel(id: string): string { return PROTOCOL_LABELS[id.toLowerCase()] ?? id; }

/** Online nodes first, stable otherwise. */
export function sortNodes<T extends { online: boolean }>(items: T[]): T[] {
  return items.map((item, index) => ({ item, index }))
    .sort((a, b) => Number(b.item.online) - Number(a.item.online) || a.index - b.index).map(entry => entry.item);
}
