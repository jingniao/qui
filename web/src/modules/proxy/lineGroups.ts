import type { GroupInput, GroupImport } from '../nodes/groupsApi';
export function isUserGroup(group: Pick<GroupInput, 'purpose'>): boolean {
  return group.purpose === 'subscription' || group.purpose === 'mixed';
}
export function groupInput(group: GroupInput): GroupInput {
  return {
    name: group.name, purpose: group.purpose, enabled: group.enabled,
    subscriptionVisible: group.subscriptionVisible, remark: group.remark,
    inboundIds: [...(group.inboundIds || [])], imports: (group.imports || []).map(item => ({ ...item })),
    ...(group.serverIds !== undefined ? { serverIds: [...group.serverIds] } : {})
  };
}
export const importMemberId = (item: GroupImport) => 'import:'+JSON.stringify([item.sourceId, item.proxyKey]);
export const groupPurposeNames = {
  subscription: '用户订阅入口', mixed: '混合用途', relay: '内部中转', egress: '系统出口'
};
