import type { GroupInput, GroupImport } from '../nodes/groupsApi';
import type { Inbound } from './api';
import { resolveImportMember } from './importIdentity';
import type { ProxyNodeItem, SubscriptionItem } from './proxyNodes';

export interface GroupMemberNode {
  id: string;
  name: string;
  protocol: string;
  port: number | string;
  server?: string;
  sourceType: 'self_hosted' | 'subscription' | 'manual' | 'tunnel_relay';
  sourceTypeName: string;
  sourceName: string;
  serverId?: string;
  sourceId?: string;
  proxyKey?: string;
  enabled: boolean;
  subscriptionVisible?: boolean;
  internalRelay?: boolean;
  routeSummary?: Inbound['routeSummary'];
  invalid?: boolean;
  ruleId?: string;
  pendingDelete?: boolean;
  note?: string;
}

export function isUserGroup(group: Pick<GroupInput, 'purpose'>): boolean {
  return group.purpose === 'subscription' || group.purpose === 'mixed';
}
export function groupInput(group: GroupInput): GroupInput {
  return {
    name: group.name, purpose: group.purpose, enabled: group.enabled,
    subscriptionVisible: group.subscriptionVisible, remark: group.remark,
    inboundIds: [...(group.inboundIds || [])], imports: (group.imports || []).map(item => ({ ...item })),
    ...(group.serverIds !== undefined ? { serverIds: [...group.serverIds] } : {}),
    // Omitted rule members are kept by the server; copy them when known.
    ...(group.relayIds !== undefined ? { relayIds: [...group.relayIds] } : {})
  };
}
export const importMemberId = (item: GroupImport) => 'import:'+JSON.stringify([item.sourceId, item.proxyKey]);
export const relayMemberId = (ruleId: string) => 'relay:' + ruleId;
export function invalidImportMembers(imports: readonly GroupImport[], sources: readonly SubscriptionItem[], nodes: readonly ProxyNodeItem[]): GroupImport[] {
  return imports.filter(member => resolveImportMember(member, sources, nodes).invalid);
}
export function assertValidImportMembers(imports: readonly GroupImport[], sources: readonly SubscriptionItem[], nodes: readonly ProxyNodeItem[]): void {
  const invalid = invalidImportMembers(imports, sources, nodes);
  if (invalid.length) throw new Error(`请先移除失效成员：${invalid.map(member => member.proxyKey || member.sourceId).join('、')}`);
}
export const groupPurposeNames = {
  subscription: '用户订阅入口', mixed: '混合用途', relay: '内部中转', egress: '系统出口'
};
