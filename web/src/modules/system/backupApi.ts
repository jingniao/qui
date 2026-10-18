import { request, upload, type Page } from '../../app/apiClient';

export interface Backup {
  id: string; createdAt: string; createdBy: string; kind: string;
  formatVersion: number; fileBytes: number; sha256: string; verifiedAt: string;
  origin: 'local' | 'imported';
}

export interface BackupRun { id: string; kind: string; periodKey: string; scheduleRevision: number; status: string; attempts: number; backupId: string; lastAttemptAt: string; retryAt: string; errorCode: string; }
export interface RestoreTotals { admins: number; activeOwners: number; sessions: number; users: number; devices: number; nodes: number; nodeGroups: number; policies: number; revisions: number; pendingOperations: number; backupFileOperations: number; runningBackupJobs: number; }
export interface RestoreImpact { usersRemoved: number; nodesRemoved: number; nodesAdded: number; sequenceRegressions: number; runtimeDifferences: number; certificateDifferences: number; revocationRegressions: number; usageRegressions: number; userReactivations: number; }
export interface RestorePreview { backupId: string; archiveSha256: string; checkedAt: string; snapshot: RestoreTotals; current: RestoreTotals; impact: RestoreImpact; sameKeyMaterial: boolean; sameNodeCA: boolean; issues: string[]; executionAvailable: boolean; }
export interface RestoreRecord { runtimeEnteredAt?: string; id: string; backupId: string; archiveSha256: string; status: 'active' | 'stopped' | 'failed' | 'interrupted'; lastStage: string; sequence: number; createdAt: string; updatedAt: string; errorCode: string; }

export const backupAPI = {
  restore: (id: string, password: string, archiveSha256: string) => request<RestoreRecord>(`/backups/${encodeURIComponent(id)}/restore`, 'POST', { password, archiveSha256, confirmBackupId: id }, { timeoutMs: 100000 }),
  restoreRecords: (page: number) => request<Page<RestoreRecord>>(`/restore-records?page=${page}&pageSize=20`),
  restorePreview: (id: string, password: string) => request<RestorePreview>(`/backups/${encodeURIComponent(id)}/restore-preview`, 'POST', { password }, { timeoutMs: 100000 }),
  import: (file: File, password: string) => { const body = new FormData(); body.append('password', password); body.append('archive', file); return upload<Backup>('/backups/import', body); },
  runs: (page: number) => request<Page<BackupRun>>(`/backup-runs?page=${page}&pageSize=20`),
  list: (page: number) => request<Page<Backup>>(`/backups?page=${page}&pageSize=20`),
  create: (password: string) => request<Backup>('/backups', 'POST', { password }, { timeoutMs: 100000 }),
  verify: (id: string, password: string) => request<Backup>(`/backups/${encodeURIComponent(id)}/verify`, 'POST', { password }, { timeoutMs: 100000 }),
  remove: (id: string) => request(`/backups/${encodeURIComponent(id)}`, 'DELETE'),
  downloadUrl: (id: string) => `/api/v1/backups/${encodeURIComponent(id)}/download`,
};
