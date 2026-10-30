import type { NodeView } from '../api';
import { hostMetrics, nicRate } from '../metrics';
import { calculateRemainingDays, formatRenewPrice, getNodeMeta } from '../nodeMeta';
import { coreSummaryLabel, unhealthyCores } from '../coreSummary';

export const statusOptions = [
  ['all', '全部状态'], ['online', '在线'], ['degraded', '降级'], ['core_error', '核心异常'],
  ['offline', '离线'], ['pending', '待登记'], ['applying', '应用配置中'], ['rollback', '回滚中'],
  ['disabled', '已禁用'], ['error', '异常'], ['maintenance', '维护中'], ['deleted', '已删除']
];
export function serverStatus(node: NodeView, now: number) {
  if (node.deletedAt) return { key: 'deleted', tone: 'disabled', label: '已删除' };
  if (!node.enabled || node.status === 'disabled') return { key: 'disabled', tone: 'disabled', label: '已禁用' };
  if (node.maintenance) return { key: 'maintenance', tone: 'maintenance', label: '维护中' };
  if (node.status === 'pending') return { key: 'pending', tone: 'disabled', label: '待登记' };
  const age = now - Date.parse(node.lastHeartbeat);
  const fresh = Number.isFinite(age) && age >= -5000 && age < 45000;
  if (fresh && (node.heartbeat?.core_healthy === false || unhealthyCores(node).length > 0)) return { key: 'core_error', tone: 'error', label: '核心异常' };
  if (node.status === 'error') return { key: 'error', tone: 'error', label: '异常' };
  if (!fresh || node.status === 'offline') return { key: 'offline', tone: 'offline', label: '离线' };
  const labels = { online: '在线', degraded: '降级', applying: '应用配置中', rollback: '回滚中' };
  return { key: node.status, tone: node.status === 'degraded' ? 'warning' : node.status === 'rollback' ? 'applying' : node.status,
    label: labels[node.status as keyof typeof labels] || '离线' };
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
export interface ServerRow { node: NodeView; region: string; status: ReturnType<typeof serverStatus>; fields: ServerField[]; }
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
  return { node, region: meta.region || '', status: serverStatus(node, now), fields };
}
