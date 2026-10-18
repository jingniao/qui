import { request, type Page } from '../../app/apiClient';
export interface Scope { user_ids?: string[]; device_ids?: string[]; node_ids?: string[]; group_ids?: string[]; }
export interface Match { domains?: string[]; suffixes?: string[]; cidrs?: string[]; ports?: string[]; network?: string; time_range?: string; service_ids?: string[]; }
export interface Action { type: string; target_id?: string; failover?: string; backup_id?: string; }
export interface PolicyLogging { log_domain: boolean; log_traffic: boolean; alert_on_block: boolean; }
export interface PolicyInput { name: string; priority: number; enabled: boolean; scope: Scope; match: Match; action: Action; logging: PolicyLogging; timezone: string; remark: string; }
export interface PolicyRecord extends PolicyInput { id: string; createdAt: string; updatedAt: string; }
export interface ServiceInput { name: string; enabled: boolean; description: string; domains: string[]; suffixes: string[]; cidrs: string[]; }
export interface ServiceSet extends ServiceInput { id: string; builtin: boolean; version: number; createdAt: string; updatedAt: string; }
export interface SimulationRequest { nodeId: string; userId: string; deviceId: string; inboundId: string; domain: string; targetIp: string; port: number; network: string; time?: string; }
export interface Decision { selectionStrategy?: string; candidatePaths?: string[][]; matched: boolean; policyId: string; policyName: string; priority: number; action: Action; reason: string; path?: string[]; outboundProtocol?: string; finalEgressNodeId?: string; logging?: PolicyLogging; }
export const policyAPI = {
  list: (page = 1, keyword = '', pageSize = 20) => request<Page<PolicyRecord>>(`/policies?page=${page}&pageSize=${pageSize}&keyword=${encodeURIComponent(keyword)}`),
  get: (id: string) => request<PolicyRecord>(`/policies/${id}`),
  save: (id: string | null, value: PolicyInput) => request<PolicyRecord>(id ? `/policies/${id}` : '/policies', id ? 'PUT' : 'POST', value),
  delete: (id: string) => request(`/policies/${id}`, 'DELETE'),
  services: (page = 1, size = 20) => request<Page<ServiceSet>>(`/service-sets?page=${page}&pageSize=${size}`),
  saveService: (id: string | null, value: ServiceInput) => request<ServiceSet>(id ? `/service-sets/${id}` : '/service-sets', id ? 'PUT' : 'POST', value),
  deleteService: (id: string) => request(`/service-sets/${id}`, 'DELETE'),
  simulate: (value: SimulationRequest) => request<Decision>('/policies/simulate', 'POST', value)
};
export function input(record: PolicyRecord): PolicyInput { return { name: record.name, priority: record.priority, enabled: record.enabled, scope: { user_ids: [...(record.scope.user_ids || [])], device_ids: [...(record.scope.device_ids || [])], node_ids: [...(record.scope.node_ids || [])], group_ids: [...(record.scope.group_ids || [])] }, match: { domains: [...(record.match.domains || [])], suffixes: [...(record.match.suffixes || [])], cidrs: [...(record.match.cidrs || [])], ports: [...(record.match.ports || [])], network: record.match.network || 'all', time_range: record.match.time_range || '', service_ids: [...(record.match.service_ids || [])] }, action: { ...record.action }, logging: record.logging ? { ...record.logging } : { log_domain: true, log_traffic: true, alert_on_block: false }, timezone: record.timezone, remark: record.remark }; }
export function defaults(): PolicyInput { return { name: '', priority: 10, enabled: true, scope: { user_ids: [], device_ids: [], node_ids: [], group_ids: [] }, match: { domains: [], suffixes: [], cidrs: [], ports: [], network: 'all', time_range: '', service_ids: [] }, action: { type: 'Block', target_id: '', failover: 'Block' }, logging: { log_domain: true, log_traffic: true, alert_on_block: false }, timezone: 'Asia/Shanghai', remark: '' }; }
export const actionNames: Record<string, string> = { Direct: '直连 / 允许', Block: '阻断', Outbound: '指定出站', OutboundGroup: '指定出口组', ProxyChain: '指定代理链' };


