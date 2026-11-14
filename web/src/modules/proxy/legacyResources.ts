import type { CanvasLegacyGroup, CanvasLegacyResources, CanvasRemoveReferrer } from './api';

export function legacyResourceSections(resources: CanvasLegacyResources) {
  return [
    { kind: 'detached' as const, label: '已脱离方案的入口', items: resources.detached },
    { kind: 'orphanTunnels' as const, label: '无方案引用的通道', items: resources.orphanTunnels },
    { kind: 'unowned' as const, label: '归属待核实', items: resources.unowned },
  ];
}

export function legacyResourceCount(resources: CanvasLegacyResources): number {
  return resources.detached.length + resources.orphanTunnels.length + resources.unowned.length;
}

export function legacyResourceActions(group: CanvasLegacyGroup) {
  return {
    showReattach: group.canReattach,
    showRemove: group.canReattach && group.canRemove,
    reasons: [
      ...(!group.canReattach ? group.reattachReasons.map(reason => `无法重新挂接：${reason}`) : []),
      ...(!group.canRemove ? group.removeReasons.map(reason => `无法完整撤除：${reason}`) : []),
    ],
  };
}

export function legacyEmptyTunnelToDelete(group: CanvasLegacyGroup): string | undefined {
  return group.empty && group.canRemove ? group.tunnelIds[0] : undefined;
}

export function legacyReferrersText(referrers: CanvasRemoveReferrer[]): string {
  const labels: Record<string, string> = { scheme: '方案', inbound: '入站', outbound: '出站', chain: '代理链', group: '出口组', policy: '分流策略', tunnel_rule: '隧道规则', restore: '恢复入口' };
  return referrers.map(item => `${labels[item.kind] ?? item.kind}：${item.name || item.id}`).join('；');
}
