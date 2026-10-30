import type { NodeView } from '../../api';
import type { Revision } from '../../../system/deploymentApi';
export type RevisionFilter = 'all' | 'manual' | 'auto';
export function filterRevisions(items: Revision[], filter: RevisionFilter) {
  return items.filter(item => filter === 'all' || item.summary.startsWith('auto:') === (filter === 'auto'));
}
export function defaultRevisionFilter(items: Revision[]): RevisionFilter {
  return filterRevisions(items, 'auto').length > filterRevisions(items, 'manual').length ? 'manual' : 'all';
}
export function revisionStrip(running?: number, latest?: number) {
  return { running: running || undefined, latest: latest || undefined, state: !running || !latest ? 'unknown' : running === latest ? 'latest' : 'unapplied' };
}
export function basicFacts(fields: (readonly [string, unknown])[]) {
  return fields.filter((field): field is [string, string | number] => field[1] !== undefined && field[1] !== null && String(field[1]).trim() !== '' && field[1] !== '—');
}
export function detailState(node: Pick<NodeView, 'status' | 'agentVersion' | 'lastHeartbeat'>, now: number) {
  if (node.status === 'pending' || !node.agentVersion) return 'pending';
  const age = now - Date.parse(node.lastHeartbeat);
  return node.status === 'offline' || !Number.isFinite(age) || age < -5000 || age >= 45000 ? 'offline' : 'ready';
}
function stable(value: unknown): string {
  if (Array.isArray(value)) return '[' + value.map(stable).join(',') + ']';
  if (value && typeof value === 'object') return JSON.stringify(Object.entries(value).filter(([, v]) => v !== undefined).sort(([a], [b]) => a.localeCompare(b)).map(([k, v]) => [k, stable(v)]));
  return JSON.stringify(value) ?? '';
}
export function isDirty(current: unknown, saved: unknown) { return stable(current) !== stable(saved); }
export function revisionResult(code: string) {
  return ({ success: '已应用且健康', validation_failed: '核心校验失败', compile_failed: '配置编译失败', apply_failed: '应用失败', start_failed: '启动失败', rollback_failed: '回滚失败', stop_failed: '停止失败', persistence_failed: '保存成功清单失败，已恢复旧配置', failed_closed: '保护性停止', accounting_interrupted: '记账中断' } as Record<string, string>)[code] || (code ? '其他结果' : '—');
}
