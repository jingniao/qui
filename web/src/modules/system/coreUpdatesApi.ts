import { request, type Page } from '../../app/apiClient';
export interface ReleaseEntry { id: string; createdAt: string; signed: { manifest: { component: string; version: string; os: string; architecture: string; sequence: number; sha256: string } }; }
export interface CoreUpdateResult { code: string; installed_version?: string; }
export interface CoreUpdateTask { id: string; nodeId: string; component: string; version: string; url: string; sha256: string; sequence: number; revision: number; commandId: string; status: string; updatedAt: string; result?: CoreUpdateResult; }
export const coreUpdatesAPI = {
  // Signed catalogue endpoints. Core updates no longer use them; they remain
  // for Agent self-update.
  releases: (page: number) => request<Page<ReleaseEntry> & { configured: boolean }>(`/releases?page=${page}&pageSize=20`),
  register: (signed: unknown) => request<ReleaseEntry>('/releases', 'POST', signed),
  tasks: (page: number) => request<Page<CoreUpdateTask>>(`/core-updates?page=${page}&pageSize=20`),
  queue: (input: { nodeId: string; revision: number; component: string; version: string; url: string; sha256: string }) => request<CoreUpdateTask>('/core-updates', 'POST', { ...input, confirmNodeId: input.nodeId }),
  clear: (id: string) => request<CoreUpdateTask>(`/core-updates/${encodeURIComponent(id)}/clear`, 'POST', {})
};
