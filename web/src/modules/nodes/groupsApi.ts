import { request, type Page } from '../../app/apiClient';
export interface GroupImport { sourceId: string; proxyKey: string }
export interface NodeGroup { id: string; name: string; purpose: 'subscription' | 'relay' | 'egress' | 'mixed'; enabled: boolean; subscriptionVisible: boolean; nodeIds: string[]; inboundIds: string[]; imports: GroupImport[]; grantedUserIds: string[]; serverIds?: string[]; remark: string; createdAt: string; }
export type GroupInput = Pick<NodeGroup, 'name' | 'purpose' | 'enabled' | 'subscriptionVisible' | 'remark' | 'inboundIds' | 'imports'> & { serverIds?: string[] };
export const groupAPI = {
  list: (page = 1, size = 20) => request<Page<NodeGroup>>('/node-groups?page='+page+'&pageSize='+size),
  save: (id: string | null, value: GroupInput) => request<NodeGroup>(id ? '/node-groups/'+id : '/node-groups', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request('/node-groups/'+id, 'DELETE')
};
// No single-group GET exists; read pages before a full-record PUT.
export async function freshGroup(id: string): Promise<NodeGroup> {
  for (let page = 1; ; page++) {
    const result = await groupAPI.list(page, 100);
    const group = result.items.find(item => item.id === id);
    if (group) return group;
    if (!result.items.length || page * result.pageSize >= result.total) break;
  }
  throw new Error('节点分组已不存在，请刷新后重试');
}
