import { request, type Page } from '../../app/apiClient';

export interface AccessRecord {
  id: string; bucket: string; node_id: string; revision: number; user_id?: string; device_id?: string;
  inbound_protocol: string; domain?: string; target_ip?: string; target_port: number; network: string;
  rule_id?: string; action: string; outbound_id?: string; outbound_protocol?: string; chain_id?: string;
  final_egress_node_id?: string; egress_known: boolean; allowed: boolean; connections: number;
  upload_bytes: number | null; download_bytes: number | null; traffic_known: boolean;
}
export interface DomainRecord { domain: string; targetIp: string; connections: number; blocked: number; uploadBytes: number | null; downloadBytes: number | null; unknownTrafficConnections: number; }
export interface TrafficSummary { connections: number; blocked: number; uploadBytes: number; downloadBytes: number; unknownTrafficConnections: number; }
export interface AccountSummary { uploadBytes: number; downloadBytes: number; observations: number; }
export interface AccessFilter { nodeId?: string; userId?: string; deviceId?: string; domain?: string; from?: string; to?: string; }

function query(filter: AccessFilter, page?: number) { const params = new URLSearchParams(); for (const [key,value] of Object.entries(filter)) if(value) params.set(key,value); if(page){params.set('page',String(page));params.set('pageSize','20');} return params.toString(); }
export const accessAPI = {
  accounts: (filter: AccessFilter) => request<AccountSummary>(`/traffic/accounts/summary?${query(filter)}`),
  list: (filter: AccessFilter, page: number, blocked = false) => request<Page<AccessRecord>>(`/access-records${blocked ? '/blocked' : ''}?${query(filter,page)}`),
  domains: (filter: AccessFilter, page: number) => request<Page<DomainRecord>>(`/access-records/domains?${query(filter,page)}`),
  summary: (filter: AccessFilter) => request<TrafficSummary>(`/traffic/summary?${query(filter)}`),
};
