import { request, type Page } from '../../app/apiClient';
export interface AgentUpdateTask {
  id: string; nodeId: string; releaseId: string; revision: number; status: string; updatedAt: string; generation: number; commandId: string;
  result?: { code: string; installed_version?: string };
}
export interface AgentUpdateAttempt { commandId: string; generation: number; operation: string; current: boolean; status: string; createdAt: string; result?: AgentUpdateTask['result']; }
export const agentUpdatesAPI = {
  tasks: (page: number, pageSize = 20) => request<Page<AgentUpdateTask>>(`/agent-updates?page=${page}&pageSize=${pageSize}`),
  queue: (nodeId: string, releaseId: string, revision: number) => request<AgentUpdateTask>('/agent-updates', 'POST', { nodeId, confirmNodeId: nodeId, releaseId, revision }),
  recover: (task: AgentUpdateTask) => request<AgentUpdateTask>(`/agent-updates/${encodeURIComponent(task.id)}/recover`, 'POST', { confirmNodeId: task.nodeId, revision: task.revision, commandId: task.commandId, generation: task.generation, unconfirmed: task.status === 'running' }),
  attempts: (id: string, page: number) => request<Page<AgentUpdateAttempt>>(`/agent-updates/${encodeURIComponent(id)}/attempts?page=${page}&pageSize=20`)
};
