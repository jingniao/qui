import { request, type Page } from '../../app/apiClient';
export interface NodeGroup { id: string; name: string; purpose: 'subscription' | 'relay' | 'egress' | 'mixed'; enabled: boolean; subscriptionVisible: boolean; nodeIds: string[]; remark: string; createdAt: string; }
export type GroupInput = Omit<NodeGroup, 'id' | 'createdAt'>;
export const groupAPI = {
  list: (page = 1, size = 20) => request<Page<NodeGroup>>(`/node-groups?page=${page}&pageSize=${size}`),
  save: (id: string | null, value: GroupInput) => request<NodeGroup>(id ? `/node-groups/${id}` : '/node-groups', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/node-groups/${id}`, 'DELETE')
};

