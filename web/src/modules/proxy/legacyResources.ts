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
    // A group that cannot be restored as a whole (for example two channels between
    // the same servers) can still be handled part by part: the server accepts any
    // connected, linear subset and reports why a selection is refused.
    partial: !group.canReattach && !group.empty && group.bindings.length + group.tunnels.length > 1,
    reasons: [
      ...(!group.canReattach ? group.reattachReasons.map(reason => `无法重新挂接：${reason}`) : []),
      ...(!group.canRemove ? group.removeReasons.map(reason => `无法完整撤除：${reason}`) : []),
    ],
  };
}

// The suggested part: every entry plus the channels an entry currently routes to
// (its default outbound is one of the channel's objects); without entries, the first channel.
export function legacyDefaultSelection(group: CanvasLegacyGroup): string[] {
  const routes = new Set(group.bindings.map(binding => binding.currentDefaultOutbound));
  const tunnels = group.tunnels.filter(tunnel => tunnel.objects.some(object => object.kind === 'outbound' && routes.has(object.id)));
  const chosen = group.bindings.length ? tunnels : group.tunnels.slice(0, 1);
  return [...group.bindings.map(binding => `binding:${binding.id}`), ...chosen.map(tunnel => `tunnel:${tunnel.id}`)];
}

export function legacyEmptyTunnelToDelete(group: CanvasLegacyGroup): string | undefined {
  return group.empty && group.canRemove ? group.tunnelIds[0] : undefined;
}

export function legacyReferrersText(referrers: CanvasRemoveReferrer[]): string {
  const labels: Record<string, string> = { scheme: '方案', inbound: '入站', outbound: '出站', chain: '代理链', group: '出口组', policy: '分流策略', tunnel_rule: '隧道规则', restore: '恢复入口' };
  return referrers.map(item => `${labels[item.kind] ?? item.kind}：${item.name || item.id}`).join('；');
}
