import { request } from '../../app/apiClient';
export interface StorageTableObservation { kind:string;retention:'policy'|'long_term'|'replay_protection';rows:number;atLeast:boolean; }
export interface SpaceMaintenanceJob {attempts:number;unconfirmedAttempts:number;failures:number;status:'idle'|'running'|'skipped'|'success'|'error';mode:'unknown'|'none'|'full'|'incremental';startedAt:string;updatedAt:string;lastSuccessAt:string;errorCode:string;}
export interface StorageObservation {observedAt:string;countLimit:number;mainDatabaseAllocatedBytes:number;mainDatabaseReusableBytes:number;mainDatabaseReclaimMode:'none'|'full'|'incremental';warningBytes:number;criticalBytes:number;warningLevel:'unknown'|'ok'|'warning'|'critical';tables:StorageTableObservation[];maintenance:SpaceMaintenanceJob;}
export interface StorageThresholds {warningBytes:number;criticalBytes:number;revision:number;updatedAt:string;updatedBy:string;}
export const storageObservationAPI=()=>request<StorageObservation>('/access-storage-observation');
export const storageThresholdsAPI=()=>request<StorageThresholds>('/system/storage-thresholds');
export const saveStorageThresholdsAPI=(input:{warningBytes:number;criticalBytes:number;expectedRevision:number})=>request<StorageThresholds>('/system/storage-thresholds','PUT',input);
