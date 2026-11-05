import type { Tunnel, TunnelRule, PortCheck, PublishPlan } from './api';
export const publishStatuses = {
  pending_delete: { label: '待删除', status: 'pending' },
  draft: { label: '草稿', status: 'draft' }, modified: { label: '已修改', status: 'pending' },
  needs_publish: { label: '待发布', status: 'pending' }, published: { label: '已发布', status: 'success' },
};
export const resultLabels = { applied: '已应用', unchanged: '无变更', failed: '失败', rolled_back: '已回滚', skipped: '已跳过' };
export function routeLabel(tunnel: Pick<Tunnel, 'hops'>) { return tunnel.hops.map(hop => hop.nodeName).join(' → '); }
export function endpoint(host: string, port: number) { return `${host.includes(':') ? `[${host}]` : host}:${port}`; }
export function groupRules(rules: TunnelRule[], tunnels: Tunnel[]) {
  const groups = new Map<string | null, { id: string | null; name: string; route: string; rules: TunnelRule[] }>();
  for (const rule of rules) {
    const tunnel = tunnels.find(t => t.id === rule.tunnelId);
    if (!groups.has(rule.tunnelId)) groups.set(rule.tunnelId, { id: rule.tunnelId, name: tunnel?.name || rule.tunnelName || '直接转发', route: tunnel ? routeLabel(tunnel) : '', rules: [] });
    groups.get(rule.tunnelId)!.rules.push(rule);
  }
  return [...groups.values()];
}
export function validPort(port: unknown): port is number { return typeof port === 'number' && Number.isInteger(port) && port >= 1 && port <= 65535; }
export function portValidation(nodeId: string, port: unknown, check?: PortCheck, owner?: { id: string; kind: string }) {
  if (!nodeId) return '请先选择服务器';
  if (!validPort(port)) return '端口必须为 1–65535 的整数';
  if (check && check.nodeId === nodeId && check.port === port && !check.free && !(owner && check.usedBy?.id === owner.id && check.usedBy.kind === owner.kind)) return `已被 ${check.usedBy?.name || '其他对象'} 占用`;
  return '';
}
export function planToRows(plan: PublishPlan) {
  const actions = { create: '新增', update: '更新', remove: '移除' };
  return [...plan.servers].sort((a, b) => a.order - b.order).map(s => ({ ...s,
    listenerText: s.listeners.map(l => `${l.port} / ${l.network.toUpperCase()} / ${l.purpose}`).join('；'),
    changeText: s.changes.map(c => `${actions[c.action]} ${c.kind} · ${c.name}`).join('；'),
  }));
}
