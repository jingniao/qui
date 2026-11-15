export interface PendingCleanupNode { nodeId: string; name: string; reason: string; }
export interface DeletionResult { pendingNodes: PendingCleanupNode[]; }
export interface DeletionPreview {
  schemes: { id: string; name: string }[];
  tunnels: { id: string; name: string }[];
  rules: { id: string; name: string }[];
  inbounds: { id: string; name: string; nodeName: string }[];
  blockers: { kind: string; id: string; name: string }[];
}
export function deletionGroups(preview: DeletionPreview) {
  return [
    { label: '中转方案', items: preview.schemes },
    { label: '隧道', items: preview.tunnels },
    { label: '隧道规则', items: preview.rules },
    { label: '入站', items: preview.inbounds.map(item => ({ ...item, name: `${item.nodeName} · ${item.name}` })) },
  ].filter(group => group.items.length);
}
export function pendingCleanupMessage(pendingNodes: PendingCleanupNode[] | undefined): string {
  return pendingNodes?.length ? `；以下节点离线或应用失败，将在恢复后自动清理：${pendingNodes.map(node => node.name).join('、')}` : '';
}
