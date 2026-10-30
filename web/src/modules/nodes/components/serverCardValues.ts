import { ref, onMounted, onUnmounted, type Ref } from 'vue';
import type { NodeView } from '../api';
import { getNodeMeta, resolveCountryInfo, onGeoResolved, formatRenewPrice, calculateRemainingDays } from '../nodeMeta';

export function useServerCardValues(clockNow: Ref<number>, nodeMonthTraffic: Ref<Record<string, { uploadBytes: number; downloadBytes: number }>>) {
function isAgentOnline(node: NodeView): boolean {
  if (!node.lastHeartbeat || !node.enabled || node.deletedAt) return false;
  const hbTime = Date.parse(node.lastHeartbeat);
  if (isNaN(hbTime)) return false;
  if (clockNow.value - hbTime >= 45000) return false;
  return node.status === 'online' || node.status === 'degraded' || node.status === 'applying' || node.status === 'rollback';
}

function statusCapsuleText(node: NodeView): string {
  if (node.deletedAt) return '已删除';
  if (!node.enabled) return '已停用';
  if (node.maintenance) return '维护中';
  if (node.status === 'pending') return '等待登记';
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') return '核心异常';
    if (node.status === 'applying') return '发布中';
    if (node.status === 'rollback') return '回滚中';
    return '在线';
  }
  if (node.status === 'error') return '异常';
  return '离线';
}

function statusCapsuleClass(node: NodeView): string {
  if (node.deletedAt || !node.enabled) {
    return 'bg-neutral-100 text-neutral-500';
  }
  if (node.maintenance) {
    return 'bg-[#FEF6EE] text-[#F79009]';
  }
  if (node.status === 'pending') {
    return 'bg-neutral-100 text-neutral-500';
  }
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') {
      return 'bg-[#FEF6EE] text-[#F79009]';
    }
    if (node.status === 'applying' || node.status === 'rollback') {
      return 'bg-blue-50 text-ios-blue';
    }
    return 'bg-[#E8F8F0] text-[#12B76A]';
  }
  if (node.status === 'error') {
    return 'bg-rose-50 text-ios-red';
  }
  return 'bg-neutral-100 text-neutral-500';
}

function statusDotClass(node: NodeView): string {
  if (node.deletedAt || !node.enabled) return 'bg-neutral-400';
  if (node.maintenance) return 'bg-[#F79009]';
  if (node.status === 'pending') return 'bg-neutral-400';
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') return 'bg-[#F79009]';
    if (node.status === 'applying' || node.status === 'rollback') return 'bg-ios-blue';
    return 'bg-[#12B76A]';
  }
  if (node.status === 'error') return 'bg-ios-red';
  return 'bg-neutral-400';
}

function calculateRunningTime(node: NodeView): string {
  return '—';
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

function formatCompactBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null) return '—';
  if (bytes === 0) return '0M';
  if (bytes < 1024 * 1024 * 1024) return `${Math.round(bytes / 1024 / 1024)}M`;
  return `${Math.round(bytes / 1024 / 1024 / 1024)}G`;
}

function formatSpeedParts(rateStr: string): { value: string; unit: string } {
  if (!rateStr || rateStr === '—') return { value: '—', unit: '' };
  const parts = rateStr.trim().split(/\s+/);
  return { value: parts[0] || '—', unit: parts[1] || '' };
}

// Geo Country Flag & Region helpers
const geoTick = ref(0);
let unsubGeo: (() => void) | undefined;

function getNodeCountry(node: NodeView) {
  void geoTick.value;
  const meta = getNodeMeta(node.id);
  return resolveCountryInfo(meta.region, node.publicIp, node.name, node.hostname);
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
  return calculateRemainingDays(meta.renewDate);
}

// Monthly traffic
function getNodeMonthTraffic(node: NodeView): number | null {
  const item = nodeMonthTraffic.value[node.id];
  return item ? item.uploadBytes + item.downloadBytes : null;
}

onMounted(() => { unsubGeo = onGeoResolved(() => { geoTick.value++; }); });
onUnmounted(() => { unsubGeo?.(); });

return {
  statusCapsuleText,
  statusCapsuleClass,
  statusDotClass,
  calculateRunningTime,
  formatPercent,
  barColor,
  formatCompactBytes,
  formatSpeedParts,
  getNodeCountry,
  getNodeRegion,
  getRenewalDate,
  getRenewalPriceDisplay,
  getRenewalInfo,
  getNodeMonthTraffic
};
}
