import { request } from '../../app/apiClient';

export interface RetentionPolicy { allowedDays: number; blockedDays: number; hourlyDays: number; }
export interface RetentionSettings { policy: RetentionPolicy; revision: number; updatedAt: string; updatedBy: string; }
export interface BaseSettings { platformName: string; timezone: string; revision: number; updatedAt: string; updatedBy: string; }
export interface BackupPolicy { enabled: boolean; time: string; dailyKeep: number; weeklyKeep: number; monthlyKeep: number; }
export interface BackupSchedule { policy: BackupPolicy; revision: number; updatedAt: string; updatedBy: string; passwordConfigured: boolean; timezone: string; }
export interface BackupRetentionPreview {
  policy:BackupPolicy;scheduleRevision:number;timezone:string;observedAt:string;metadataOnly:boolean;executionAvailable:boolean;candidates:number;candidateBytes:number;
  categories:{kind:'daily'|'weekly'|'monthly';keep:number;existing:number;retained:number;candidates:number;candidateBytes:number;sample:{id:string;createdAt:string;fileBytes:number}[];sampleTruncated:boolean}[];
}
export interface RuntimePolicy { sessionMinutes:number; subscriptionFormat:'uri'|'base64'|'sing-box'; }
export interface RuntimeSettings { policy:RuntimePolicy; revision:number; updatedAt:string; updatedBy:string; }
export interface RuntimeInfo {
 generatedAt:string; startedAt:string; uptimeSeconds:number; goVersion:string; buildVersion:string; os:string; arch:string; goroutines:number; heapBytes:number; databaseBytes:number; sqliteVersion:string; journalMode:string; migration:string;
 network:{development:boolean; httpAddress:string; grpcAddress:string; tlsEnabled:boolean; minimumTLS:string; certificate:{configured:boolean;available:boolean;notBefore?:string;notAfter?:string;validNow?:boolean}};
 security:{secureCookies:boolean;httpOnlyCookies:boolean;sameSite:string;csrfRequired:boolean;passwordMinCharacters:number;passwordMaxCharacters:number;passwordAlgorithm:string;passwordIterations:number;loginAttempts:number;loginWindowMinutes:number;requestBytes:number};
}
export const settingsAPI = {
  runtime:()=>request<RuntimeInfo>('/system/runtime'),
  runtimePolicy:()=>request<RuntimeSettings>('/system/runtime-policy'),
  saveRuntimePolicy:(policy:RuntimePolicy,expectedRevision:number)=>request<RuntimeSettings>('/system/runtime-policy','PUT',{policy,expectedRevision}),
  backupSchedule: () => request<BackupSchedule>('/system/backup-schedule'),
  backupRetentionPreview: (policy:BackupPolicy,expectedRevision:number)=>request<BackupRetentionPreview>('/system/backup-retention-preview','POST',{policy,expectedRevision}),
  saveBackupSchedule: (policy: BackupPolicy, expectedRevision: number, password: string) => request<BackupSchedule>('/system/backup-schedule', 'PUT', { policy, expectedRevision, password }),
  base: () => request<BaseSettings>('/system/base'),
  saveBase: (platformName: string, timezone: string, expectedRevision: number) => request<BaseSettings>('/system/base', 'PUT', { platformName, timezone, expectedRevision }),
  retention: () => request<RetentionSettings>('/system/access-retention'),
  saveRetention: (policy: RetentionPolicy, expectedRevision: number) => request<RetentionSettings>('/system/access-retention', 'PUT', { policy, expectedRevision })
};
