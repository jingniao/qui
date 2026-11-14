import type { HostMetrics } from '../metrics';
import { serverStatus } from './serverListModel';
import type { Ref } from 'vue';
import type { NodeView } from '../api';
import { getNodeMeta, resolveCountryInfo, countryTitle, formatRenewPrice, calculateRemainingDays } from '../nodeMeta';

export function useServerCardValues(clockNow: Ref<number>) {
function statusCapsuleText(node: NodeView): string { return serverStatus(node, clockNow.value).label; }
function statusCapsuleClass(node: NodeView): string {
  const tone = serverStatus(node, clockNow.value).tone;
  if (tone === 'error') return 'bg-ios-red/10 text-ios-red';
  if (tone === 'warning') return 'bg-ios-orange/10 text-ios-orange';
  if (tone === 'online') return 'bg-ios-green/10 text-ios-green';
  if (tone === 'applying') return 'bg-ios-blue/10 text-ios-blue';
  return 'bg-ios-fill text-ios-secondary';
}
function statusDotClass(node: NodeView): string {
  const tone = serverStatus(node, clockNow.value).tone;
  return ({ error: 'bg-ios-red', warning: 'bg-ios-orange', online: 'bg-ios-green', applying: 'bg-ios-blue' } as Record<string, string>)[tone] || 'bg-ios-tertiary';
}
function calculateRunningTime(node: NodeView): string {
  const age = clockNow.value - Date.parse(node.lastHeartbeat);
  const seconds = (node.heartbeat as HostMetrics)?.host_uptime_seconds;
  if (!Number.isFinite(age) || age < -5000 || age >= 45000 || typeof seconds !== 'number' || !Number.isFinite(seconds) || seconds < 0) return '—';
  if (seconds >= 86400) return Math.floor(seconds / 86400) + ' 天';
  if (seconds >= 3600) return Math.floor(seconds / 3600) + ' 小时';
  return Math.floor(seconds / 60) + ' 分钟';
}

function formatPercent(val?: number): string {
  return typeof val === 'number' ? `${Math.round(val)}%` : '—';
}

function barColor(val?: number): string {
  if (typeof val !== 'number') return 'bg-neutral-300';
  if (val >= 90) return 'bg-rose-500';
  if (val >= 75) return 'bg-amber-500';
  return 'bg-[#12B76A]';
}

function formatSpeedParts(rateStr: string): { value: string; unit: string } {
  if (!rateStr || rateStr === '—') return { value: '—', unit: '' };
  const parts = rateStr.trim().split(/\s+/);
  return { value: parts[0] || '—', unit: parts[1] || '' };
}

// Country flag shown before the server name (v1.5.5 item 19).
function getNodeCountry(node: NodeView) {
  const meta = getNodeMeta(node.id);
  return resolveCountryInfo({ region: meta.region, countryCode: node.countryCode, name: node.name, hostname: node.hostname });
}

function getNodeCountryTitle(node: NodeView): string {
  return countryTitle(getNodeCountry(node));
}

function getNodeRegion(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return meta.region || '';
}

// Renewal date & price helpers
function getRenewalDate(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return meta.renewDate || '';
}

function getRenewalPriceDisplay(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return formatRenewPrice(meta);
}

function getRenewalInfo(node: NodeView) {
  const meta = getNodeMeta(node.id);
  const info = calculateRemainingDays(meta.renewDate);
  return { ...info, isNear: info.days !== null && info.days <= (meta.renewNoticeDays ?? 7) };
}

// Monthly machine traffic lives in MachineTrafficUsage (T1), not in the card model.

return {
  statusCapsuleText,
  statusCapsuleClass,
  statusDotClass,
  calculateRunningTime,
  formatPercent,
  barColor,
  formatSpeedParts,
  getNodeCountry,
  getNodeCountryTitle,
  getNodeRegion,
  getRenewalDate,
  getRenewalPriceDisplay,
  getRenewalInfo
};
}
