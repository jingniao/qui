import type { CanvasRemovePlan, CanvasRemoveReferrer, Outbound, ProxyChain } from './api';

const objectKinds: Record<string, string> = { inbound: '内部监听', outbound: '配套出站', chain: '代理链' };
const referrerKinds: Record<string, string> = { scheme: '方案', inbound: '入站', outbound: '出站', chain: '代理链', group: '出口组', policy: '分流策略', tunnel_rule: '隧道规则', restore: '恢复入口' };

function referrersText(referrers: CanvasRemoveReferrer[] | null): string {
  return (referrers ?? []).map(ref => `${referrerKinds[ref.kind] ?? ref.kind}：${ref.name || ref.id}`).join('；');
}

export function canvasRemoveView(plan: CanvasRemovePlan, nameOfNode: (id: string) => string = id => id, routeName: (route: string) => string = route => route === 'direct' ? '直连' : route === 'block' ? '阻断' : route) {
  const objects = plan.objects.map(object => ({
    ...object,
    nodeName: nameOfNode(object.nodeId),
    kindText: objectKinds[object.kind],
    referrersText: referrersText(object.referrers),
  }));
  return {
    entries: plan.entries.map(entry => ({
      ...entry,
      nodeName: nameOfNode(entry.nodeId),
      actionText: entry.action === 'delete' ? '删除该入站及其分组成员' : `恢复为原出站：${entry.restoreTarget ? routeName(entry.restoreTarget) : '须指定恢复目标'}`,
      groupsText: (entry.groups ?? []).map(group => group.name || group.id).join('、'),
      referrersText: referrersText(entry.referrers),
    })),
    removeObjects: objects.filter(object => !object.keep),
    keepObjects: objects.filter(object => object.keep),
    nodes: plan.nodes.map((id, index) => ({ id, name: nameOfNode(id), order: index + 1 })),
    changes: plan.entries.flatMap(entry => (entry.changes ?? []).map(change => `${entry.name}：${change}`)),
  };
}

export function canConfirmCanvasRemove(plan: CanvasRemovePlan, restoreTargets: Record<string, string>, acknowledgeChanges: boolean): boolean {
  if (!plan.entries.length && !plan.objects.length) return false;
  if (plan.entries.some(entry => (entry.referrers?.length ?? 0) > 0)) return false;
  if (plan.entries.some(entry => entry.needsRestoreTarget && !restoreTargets[entry.bindingId]?.trim())) return false;
  const hasChanges = plan.entries.some(entry => (entry.changes?.length ?? 0) > 0);
  if (hasChanges && !acknowledgeChanges) return false;
  // A filled target or acknowledged changes can resolve a blocked preview;
  // remove revalidates the complete restoration graph on the server.
  return plan.ok || hasChanges || plan.entries.some(entry => entry.needsRestoreTarget);
}

export function canvasRestoreOptions(nodeId: string, outbounds: Outbound[], chains: ProxyChain[]) {
  return [
    { value: 'direct', label: '直连（direct）' },
    { value: 'block', label: '阻断（block）' },
    ...outbounds.filter(outbound => outbound.enabled && outbound.nodeId === nodeId).map(outbound => ({ value: outbound.id, label: `出站：${outbound.name}` })),
    ...chains.filter(chain => chain.enabled && chain.nodeIds[0] === nodeId).map(chain => ({ value: `chain:${chain.id}`, label: `代理链：${chain.name}` })),
  ];
}
