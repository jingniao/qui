import { request, type Page } from '../../app/apiClient';
export interface SystemUpdateStage { status: string; error: string; exitCode: number | null; }
export interface SystemUpdateVersion { version: string; error: string; }
export interface AgentSystemUpdateResult {
  schemaVersion: number; taskId: string; attemptId: string; nodeId: string; revision: number; targetVersion: string;
  status: string; phase: string; error: string; exitCode: number | null; commandExitCode: number | null; signal: string;
  bootstrapStage: SystemUpdateStage; agentStage: SystemUpdateStage;
  bootstrapVersion: SystemUpdateVersion; installedVersion: SystemUpdateVersion; runningVersion: SystemUpdateVersion;
  serviceState: string; serviceError: string; recovery: { attempted: boolean; success: boolean | null; error: string };
  startedAt: string; finishedAt: string; logBytes: number; logError: string; persistenceError: string;
}
export interface AgentUpdateTask {
  id: string; nodeId: string; releaseId?: string; revision: number; status: string; updatedAt: string; generation: number; commandId: string;
  mode?: 'system' | 'worker'; targetVersion?: string; phase?: string; dispatchError?: string; systemResult?: AgentSystemUpdateResult;
  result?: { code: string; installed_version?: string };
}
export interface AgentUpdateAttempt { commandId: string; generation: number; operation: string; current: boolean; status: string; createdAt: string; result?: AgentUpdateTask['result']; phase?: string; dispatchError?: string; systemResult?: AgentSystemUpdateResult; }
export const agentUpdatesAPI = {
  tasks: (page: number, pageSize = 20) => request<Page<AgentUpdateTask>>(`/agent-updates?page=${page}&pageSize=${pageSize}`),
  queue: (nodeId: string, targetVersion: string, revision: number) => request<AgentUpdateTask>('/agent-updates', 'POST', { nodeId, confirmNodeId: nodeId, targetVersion, revision }),
  recover: (task: AgentUpdateTask) => request<AgentUpdateTask>(`/agent-updates/${encodeURIComponent(task.id)}/recover`, 'POST', { confirmNodeId: task.nodeId, revision: task.revision, commandId: task.commandId, generation: task.generation, unconfirmed: task.status === 'running' }),
  retry: (task: AgentUpdateTask) => request<AgentUpdateTask>(`/agent-updates/${encodeURIComponent(task.id)}/retry`, 'POST', { confirmNodeId: task.nodeId, revision: task.revision, commandId: task.commandId, generation: task.generation }),
  attempts: (id: string, page: number) => request<Page<AgentUpdateAttempt>>(`/agent-updates/${encodeURIComponent(id)}/attempts?page=${page}&pageSize=20`),
  logs: (id: string, attemptId: string, offset: number) => request<{ output: string; nextOffset: number; complete: boolean; error: string }>(`/agent-updates/${encodeURIComponent(id)}/logs?attemptId=${encodeURIComponent(attemptId)}&offset=${offset}`),
  logDownload: (id: string, attemptId: string) => `/api/v1/agent-updates/${encodeURIComponent(id)}/logs?attemptId=${encodeURIComponent(attemptId)}&download=1`
};
