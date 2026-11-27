import { request, type Page } from '../../app/apiClient';

export interface Revision {
  id: number; author: string; summary: string; status: string; createdAt: string;
  nodeId: string; resultCode: string; batchId?: string;
}
export interface RevisionChange {
  category: string; id: string; kind: 'added' | 'removed' | 'modified';
  field: string; before: string; after: string; hidden: boolean;
}
export interface RevisionDiff {
  nodeId: string; baseRevision: number; revision: number; changes: RevisionChange[];
}
export interface NodePublication {
  revision: number;
  status: string;
  batchId?: string;
}

export type InboundPublishState = 'effective' | 'modified' | 'pending' | 'unpublished';
export interface InboundStateItem { inboundId: string; state: InboundPublishState; code?: string; revision?: number; }

export const deploymentAPI = {
  inboundStatesBatch: (nodeIds: string[]) => request<{ items: { nodeId: string; items: InboundStateItem[]; error?: string }[] }>(`/nodes/inbound-states?nodeIds=${encodeURIComponent(nodeIds.join(','))}`),
  diff: (revision: number, baseRevision?: number) => request<RevisionDiff>(`/revisions/${revision}/diff${baseRevision === undefined ? '' : '?baseRevision=' + baseRevision}`),
  rollbackNode: (nodeId: string, revision: number) => request(`/nodes/${encodeURIComponent(nodeId)}/rollback`, 'POST', { revision }),
  rollbackBatch: (batchId: string) => request(`/deployment-batches/${encodeURIComponent(batchId)}/rollback`, 'POST'),
  batch: (batchId: string) => request<{ status: string }>(`/deployment-batches/${encodeURIComponent(batchId)}`),
  nodeRevisions: (nodeId: string, page = 1, pageSize = 20) => request<Page<Revision>>(`/revisions?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  // Creates the node's revision from its business data and publishes it in one
  // step; a refused deploy still leaves the draft and names it in the error.
  publishNode: (nodeId: string, summary: string) => request<NodePublication>(`/nodes/${encodeURIComponent(nodeId)}/publish`, 'POST', { summary }),
  deployRevision: (id: number) => request(`/revisions/${id}/deploy`, 'POST'),
  // Publishes an empty baseline to an online node that never ran a configuration,
  // so web upgrades (bound to a confirmed revision) can start (v1.5.5 item 11).
  publishBaseline: (nodeId: string) => request<{ revision: number; status: string }>(`/nodes/${encodeURIComponent(nodeId)}/baseline`, 'POST', { confirmNodeId: nodeId }),
};
