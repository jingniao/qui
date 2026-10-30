import type { GroupInput } from '../nodes/groupsApi';

export function isUserGroup(group: Pick<GroupInput, 'purpose'>): boolean {
  return group.purpose === 'subscription' || group.purpose === 'mixed';
}

export function groupInput(group: GroupInput, nodeIds = group.nodeIds): GroupInput {
  return {
    name: group.name, purpose: group.purpose, enabled: group.enabled,
    subscriptionVisible: group.subscriptionVisible, remark: group.remark, nodeIds: [...nodeIds]
  };
}

export const groupPurposeNames = {
  subscription: '用户订阅入口', mixed: '混合用途', relay: '内部中转', egress: '系统出口'
};
