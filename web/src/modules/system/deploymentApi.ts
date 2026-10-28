import { request, type Page } from '../../app/apiClient';

export interface Revision {
  id: number; author: string; summary: string; status: string; createdAt: string;
  nodeId: string; resultCode: string; batchId?: string;
}
export interface BatchItem {
  position: number; nodeId: string; revision: number; previousRevision: number;
  status: string; rollbackCommandId?: string;
}
export interface DeploymentBatch {
  id: string; chainId: string; name: string; author: string; status: string;
  createdAt: string; items: BatchItem[];
}
export interface ChainChoice { id: string; name: string; enabled: boolean; nodeIds: string[]; }
export interface RevisionChange {
  category: string; id: string; kind: 'added' | 'removed' | 'modified';
  field: string; before: string; after: string; hidden: boolean;
}
export interface RevisionDiff {
  nodeId: string; baseRevision: number; revision: number; changes: RevisionChange[];
}

export const deploymentAPI = {
  diff: (revision: number, baseRevision?: number) => request<RevisionDiff>(`/revisions/${revision}/diff${baseRevision === undefined ? '' : '?baseRevision=' + baseRevision}`),
  batches: (page: number) => request<Page<DeploymentBatch>>(`/deployment-batches?page=${page}&pageSize=20`),
  chains: () => request<Page<ChainChoice>>('/proxy-chains?page=1&pageSize=100'),
  createBatch: (chainId: string, summary: string) => request<DeploymentBatch>('/deployment-batches', 'POST', { chainId, summary }),
  createRelayBatch: (nodeId: string, summary: string) => request<DeploymentBatch>('/deployment-batches/relay', 'POST', { nodeId, summary }),
  deployBatch: (id: string) => request<DeploymentBatch>(`/deployment-batches/${encodeURIComponent(id)}/deploy`, 'POST'),
  rollbackBatch: (id: string) => request<DeploymentBatch>(`/deployment-batches/${encodeURIComponent(id)}/rollback`, 'POST'),
  rollbackNode: (nodeId: string, revision: number) => request(`/nodes/${encodeURIComponent(nodeId)}/rollback`, 'POST', { revision }),
  nodeRevisions: (nodeId: string, page = 1, pageSize = 20) => request<Page<Revision>>(`/revisions?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  // Creates the node's revision from its business data and publishes it in one
  // step; a refused deploy still leaves the draft and names it in the error.
  publishNode: (nodeId: string, summary: string) => request<{ revision: number; status: string }>(`/nodes/${encodeURIComponent(nodeId)}/publish`, 'POST', { summary }),
  deployRevision: (id: number) => request(`/revisions/${id}/deploy`, 'POST'),
};
