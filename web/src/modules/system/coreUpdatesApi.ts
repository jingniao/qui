import { request, type Page } from '../../app/apiClient';
export interface ReleaseEntry { id: string; createdAt: string; signed: { manifest: { component: string; version: string; os: string; architecture: string; sequence: number; sha256: string } }; }
export interface CoreUpdateResult { code: string; installed_version?: string; }
export interface CoreUpdateTask { adding:boolean; id: string; nodeId: string; component: string; version: string; url: string; sha256: string; sequence: number; revision: number; commandId: string; status: string; updatedAt: string; result?: CoreUpdateResult; }
export const coreUpdatesAPI = {
 remove: (nodeId:string,component:CoreName,revision:number) => request<CoreRemoveTask>(`/nodes/${encodeURIComponent(nodeId)}/cores/${component}/remove`,"POST",{confirmNodeId:nodeId,revision}),
 removals: (nodeId:string,component:CoreName) => request<{items:CoreRemoveTask[]}>(`/nodes/${encodeURIComponent(nodeId)}/cores/${component}/removals`),
  // Signed catalogue endpoints. Core updates no longer use them; they remain
  // for Agent self-update.
  releases: (page: number, pageSize = 20) => request<Page<ReleaseEntry> & { configured: boolean }>(`/releases?page=${page}&pageSize=${pageSize}`),
  register: (signed: unknown) => request<ReleaseEntry>('/releases', 'POST', signed),
  tasks: (page: number, pageSize = 20) => request<Page<CoreUpdateTask>>(`/core-updates?page=${page}&pageSize=${pageSize}`),
  queue: (input: { nodeId: string; revision: number; component: string; version: string; url: string; sha256: string }) => request<CoreUpdateTask>('/core-updates', 'POST', { ...input, confirmNodeId: input.nodeId }),
  clear: (id: string) => request<CoreUpdateTask>(`/core-updates/${encodeURIComponent(id)}/clear`, 'POST', {})
};

export type CoreName = 'xray' | 'sing-box';
export interface CoreRemoveTask { id:string;nodeId:string;component:CoreName;revision:number;status:'queued'|'running'|'success'|'rejected'|'unconfirmed';code:string;createdBy:string;createdAt:string;updatedAt:string;result?:{id:string;component:CoreName;revision:number;success?:boolean;code:string}; }
