import type { NodeView } from '../api';
import { hostMetrics, nicRate } from '../metrics';
import { calculateRemainingDays, formatRenewPrice, getNodeMeta, parseRenewPrice, resolveCountryInfo, type CountryInfo, type NodeMeta } from '../nodeMeta';
import { coreSummaryLabel, unhealthyCores } from '../coreSummary';

export function serverStatus(node: NodeView, now: number) {
  if (node.deletedAt) return { key: 'deleted', tone: 'disabled', label: '已删除' };
  if (!node.enabled || node.status === 'disabled') return { key: 'disabled', tone: 'disabled', label: '已禁用' };
  if (node.maintenance) return { key: 'maintenance', tone: 'maintenance', label: '维护中' };
  if (node.status === 'pending') return { key: 'pending', tone: 'disabled', label: '待登记' };
  const fact = node.liveness;
  const valid = !!fact?.sessionId && now >= Date.parse(fact.evaluatedAt) - 5000 && now < Date.parse(fact.validUntil);
  if (fact?.connected && fact.inFlightCount > 0 && !valid) return { key: 'reconnecting', tone: 'warning', label: '面板存活状态未更新' };
  const received = valid ? Date.parse(fact!.lastReceivedAt) : NaN;
  const durable = Date.parse(node.lastHeartbeat);
  const age = now - (Number.isFinite(received) && (!Number.isFinite(durable) || received > durable) ? received : durable);
  const disconnected = valid && Date.parse(fact!.disconnectedAt) > 0;
  const transport = valid && fact!.connected && (fact!.inFlightCount > 0 || age >= -5000 && age < 45000);
  const state = node.persistedStatus || node.status;
  const fresh = Number.isFinite(age) && age >= -5000 && age < 45000;
  if (!transport && (state === 'offline' || (!fresh && state !== 'error') || disconnected)) {
    if (Number.isFinite(age) && age >= -5000 && age < node.offlineAfterSeconds * 1000) {
      return { key: 'reconnecting', tone: 'warning', label: '连接中断，等待恢复' };
    }
    return { key: 'offline', tone: 'offline', label: '离线' };
  }
  if ((fresh || transport) && (node.heartbeat?.core_healthy === false || unhealthyCores(node).length > 0)) return { key: 'core_error', tone: 'error', label: '核心异常' };
  if (state === 'error') return { key: 'error', tone: 'error', label: '异常' };
  const labels = { online: '在线', degraded: '降级', applying: '应用配置中', rollback: '回滚中' };
  const status = transport && state === 'offline' ? 'online' : state;
  return { key: status, tone: status === 'degraded' ? 'warning' : status === 'rollback' ? 'applying' : status,
    label: labels[status as keyof typeof labels] || '离线' };
}
export function processingNotice(node: NodeView, now: number): string {
 const fact = node.liveness;
 if (!fact?.sessionId || fact.inFlightCount === 0) return '';
 if (now >= Date.parse(fact.validUntil)) return '面板存活状态未更新，等待校准';
 const age = now - Date.parse(fact.oldestInFlightAt);
 return age >= 10000 ? `面板处理延迟 ${Math.floor(age / 1000)} 秒（${fact.processingStage === 'sending_ack' ? '发送确认' : '等待数据库'}）` : '';
}
export function relativeHeartbeat(value: string, now: number) {
  const age = now - Date.parse(value);
  if (!Number.isFinite(age)) return '—';
  if (age < -5000) return '时钟超前';
  const seconds = Math.max(0, Math.floor(age / 1000));
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86400)} 天前`;
}
export interface ServerField { key: string; label: string; value: string; bytes?: number | null; warning?: boolean; }
export interface ServerRow { node: NodeView; region: string; country: CountryInfo; status: ReturnType<typeof serverStatus>; processingNotice: string; fields: ServerField[]; }
const percent = (value?: number) => typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value)}%` : '—';
const text = (value?: string | number) => value === undefined || value === null || value === '' ? '—' : String(value);
export function serverRow(node: NodeView, now: number, today?: number, month?: number): ServerRow {
  const meta = getNodeMeta(node.id);
  const metrics = hostMetrics(node, now);
  const hb = Object.keys(metrics).length ? node.heartbeat : undefined;
  const remaining = calculateRemainingDays(meta.renewDate);
  const receive = nicRate(metrics, 'receive'), transmit = nicRate(metrics, 'transmit');
  const fields: ServerField[] = [
    { key: 'ip', label: '公网 IP', value: text(node.publicIp) },
    { key: 'tags', label: '分组', value: node.tags?.length ? node.tags.join('、') : '—' },
    { key: 'cpu', label: 'CPU', value: percent(hb?.cpu_percent) },
    { key: 'memory', label: '内存', value: percent(hb?.memory_percent) },
    { key: 'disk', label: '磁盘', value: percent(hb?.disk_percent) },
    { key: 'connections', label: '连接数', value: hb?.active_connections_measured === true ? text(hb.active_connections) : '—' },
    { key: 'today', label: '今日流量', value: today === undefined ? '—' : String(today), bytes: today },
    { key: 'month', label: '本月流量', value: month === undefined ? '—' : String(month), bytes: month },
    { key: 'nic', label: '网卡速率', value: receive === '—' && transmit === '—' ? '—' : `↓ ${receive} / ↑ ${transmit}` },
    { key: 'core', label: '核心 / 版本', value: coreSummaryLabel(node) },
    { key: 'agent', label: 'Agent 版本', value: text(node.agentVersion) },
    { key: 'revision', label: '当前配置', value: hb?.current_revision ? `#${hb.current_revision}` : '—' },
    { key: 'heartbeat', label: '最后心跳', value: relativeHeartbeat(node.lastHeartbeat, now) },
    { key: 'renewal', label: '续费到期日', value: remaining.days === null ? '—' : `${meta.renewDate}（${remaining.label}）`,
      warning: remaining.days !== null && remaining.days <= (meta.renewNoticeDays ?? 7) },
    { key: 'price', label: '续费价格', value: formatRenewPrice(meta) || '—' },
    { key: 'os', label: '操作系统', value: text(node.capabilities?.host?.osPrettyName || node.os) },
    { key: 'remark', label: '备注', value: text(meta.remark) }
  ];
  const country = resolveCountryInfo({ region: meta.region, countryCode: node.countryCode, name: node.name, hostname: node.hostname });
  return { node, region: meta.region || '', country, status: serverStatus(node, now), processingNotice: processingNotice(node, now), fields };
}

export interface ServerEditFormState {
  name: string;
  region: string;
  remark: string;
  renewDate: string;
  renewCurrency: string;
  renewPrice: string | number;
  renewCycle: string;
  enabled: boolean;
  allowSubscription: boolean;
  maintenance: boolean;
  autoRenew: boolean;
  tags: string[];
}

// Merges latest node and meta into the current edit form, preserving any field
// that the user has already touched since the drawer was opened.
export function mergeLatestEditForm(
  current: ServerEditFormState,
  baseline: ServerEditFormState,
  latestNode: { name: string; enabled: boolean; allowSubscription: boolean; maintenance: boolean },
  latestMeta: Partial<NodeMeta>
): void {
  const parsed = parseRenewPrice(latestMeta.renewPrice, latestMeta.renewCurrency, latestMeta.renewCycle);
  if (current.name === baseline.name) current.name = latestNode.name;
  if (current.region === baseline.region) current.region = latestMeta.region || '';
  if (current.remark === baseline.remark) current.remark = latestMeta.remark || '';
  if (current.renewDate === baseline.renewDate) current.renewDate = latestMeta.renewDate || '';
  if (current.renewCurrency === baseline.renewCurrency) current.renewCurrency = parsed.currency;
  if (current.renewPrice === baseline.renewPrice) current.renewPrice = parsed.price;
  if (current.renewCycle === baseline.renewCycle) current.renewCycle = parsed.cycle;
  if (current.autoRenew === baseline.autoRenew) current.autoRenew = latestMeta.autoRenew ?? false;
  if (current.enabled === baseline.enabled) current.enabled = latestNode.enabled;
  if (current.allowSubscription === baseline.allowSubscription) current.allowSubscription = latestNode.allowSubscription;
  if (current.maintenance === baseline.maintenance) current.maintenance = latestNode.maintenance;
  // Tags are always replaced, never mutated, so an untouched list is the same array.
  if (current.tags === baseline.tags) current.tags = [...(latestMeta.tags ?? [])];
}

// Builds the saved metadata fields: a field the user changed since the drawer
// opened comes from the form, any other keeps the latest stored value. Price,
// currency and cycle are one value, because a legacy record keeps its currency
// and cycle inside the price text; changing any of them saves all three.
export function editedNodeMeta(form: ServerEditFormState, baseline: ServerEditFormState, latestMeta: Partial<NodeMeta>): Partial<NodeMeta> {
  const price = String(form.renewPrice).trim();
  const priceChanged = price !== String(baseline.renewPrice) || form.renewCurrency !== baseline.renewCurrency || form.renewCycle !== baseline.renewCycle;
  return {
    region: form.region.trim() !== baseline.region ? form.region.trim() : latestMeta.region,
    remark: form.remark.trim() !== baseline.remark ? form.remark.trim() : latestMeta.remark,
    renewDate: form.renewDate !== baseline.renewDate ? form.renewDate : latestMeta.renewDate,
    ...(priceChanged
      ? { renewPrice: price, renewCurrency: form.renewCurrency, renewCycle: form.renewCycle }
      : { renewPrice: latestMeta.renewPrice, renewCurrency: latestMeta.renewCurrency, renewCycle: latestMeta.renewCycle }),
    autoRenew: form.autoRenew !== baseline.autoRenew ? form.autoRenew : latestMeta.autoRenew,
    tags: JSON.stringify(form.tags) !== JSON.stringify(baseline.tags) ? form.tags : latestMeta.tags
  };
}
