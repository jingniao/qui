/// <reference types="vite/client" />
import { request, APIError } from '../../app/apiClient';
import { nodeAPI, type NodeView } from '../nodes/api';
import { choices, proxyAPI, type Inbound } from '../proxy/api';

export type PublishStatus = 'draft' | 'modified' | 'published' | 'needs_publish' | 'pending_delete';
export interface TunnelHop {
  role: 'entry' | 'mid' | 'exit'; nodeId: string; nodeName: string;
  listenPort: number | null; portMode: 'auto' | 'manual'; connectHost: string;
}
export interface Tunnel {
  id: string; name: string; transport: 'tls'; hops: TunnelHop[]; ruleCount: number; status: PublishStatus;
  credentials: { generation: number; serverName: string; notAfter: string; fingerprint: string } | null;
  createdAt: string; updatedAt: string; publishedAt: string | null;
}
export interface TunnelInput {
  name: string; transport: 'tls';
  hops: { role: 'entry' | 'mid' | 'exit'; nodeId: string; portMode: 'auto' | 'manual'; listenPort?: number; connectHost?: string }[];
  updatedAt?: string;
}
export interface TunnelRule {
  id: string; name: string; entryNodeId: string; entryNodeName: string;
  listenPort: number; portMode: 'auto' | 'manual'; network: 'tcp' | 'udp' | 'both';
  tunnelId: string | null; tunnelName: string | null;
  target: { mode: 'linked'; nodeId: string; nodeName: string; inboundId: string; inboundName: string; protocol: string; host: string; port: number }
    | { mode: 'manual'; host: string; port: number };
  ownerNodeId: string | null; subscriptionVisible: boolean; enabled: boolean; status: PublishStatus;
  createdAt: string; updatedAt: string; publishedAt: string | null;
}
export interface TunnelRuleInput {
  name: string; entryNodeId: string; portMode: 'auto' | 'manual'; listenPort?: number;
  network: 'tcp' | 'udp' | 'both'; tunnelId: string | null;
  target: { mode: 'linked'; inboundId: string } | { mode: 'manual'; host: string; port: number };
  subscriptionVisible: boolean; enabled: boolean; updatedAt?: string;
}
export interface PortCheck { nodeId: string; port: number; free: boolean; usedBy?: { kind: 'inbound' | 'tunnel' | 'rule' | 'canvas'; id: string; name: string } }
export interface PublishSelection { tunnelIds: string[]; ruleIds: string[] }
export interface PublishPlan {
  ok: boolean; message?: string;
  servers: { nodeId: string; nodeName: string; order: number;
    listeners: { port: number; network: 'tcp' | 'udp' | 'both'; purpose: string }[];
    routes: string[]; changes: { action: 'create' | 'update' | 'remove'; kind: string; name: string }[] }[];
  conflicts: { nodeId: string; port: number; usedBy: string }[]; warnings: string[];
}
export interface PublishResult {
  batchId?: string;
  servers: { nodeId: string; nodeName: string; revision: number | null; status: 'applied' | 'unchanged' | 'failed' | 'rolled_back' | 'skipped'; message?: string }[];
}
export interface NodeRelay { rule: TunnelRule; shareLink: string | null }
export interface NodeRelayInput { tunnelId: string; inboundId: string; portMode: 'auto' | 'manual'; listenPort?: number; subscriptionVisible: boolean; name?: string }

// Contract assumptions (no additional endpoints):
// - ports/check can report the object's own claim; editors match usedBy.kind/id.
// - an auto suggestion is sent as listenPort with portMode=auto, then checked on save.
// - pending_delete objects remain listable and retain claims until published.
// - the linked-target preview uses IPv4 loopback when exit=destination; the saved
//   linked input contains only inboundId, so the server remains authoritative.

export function mockEnabled() { return import.meta.env.DEV && localStorage['qui.mock.tunnels'] === '1'; }
async function call<T>(path: string, method = 'GET', body?: unknown): Promise<T> {
  if (import.meta.env.DEV && mockEnabled()) return (await import('./mock')).mockRequest(path, method, body) as Promise<T>;
  return request<T>(path, method, body, { timeoutMs: path === '/tunnels/publish' ? 310000 : 15000 });
}
const idPath = (id: string) => encodeURIComponent(id);
export const tunnelAPI = {
  list: () => call<{ items: Tunnel[]; total: number }>('/tunnels'),
  create: (input: TunnelInput) => call<Tunnel>('/tunnels', 'POST', input),
  update: (id: string, input: TunnelInput) => call<Tunnel>(`/tunnels/${idPath(id)}`, 'PUT', input),
  remove: (id: string) => call<void>(`/tunnels/${idPath(id)}`, 'DELETE'),
  rotate: (id: string, updatedAt: string) => call<Tunnel>(`/tunnels/${idPath(id)}/rotate-credentials`, 'POST', { updatedAt }),
  plan: (selection: PublishSelection) => call<PublishPlan>('/tunnels/plan', 'POST', selection),
  publish: (selection: PublishSelection) => call<PublishResult>('/tunnels/publish', 'POST', selection),
};
export const tunnelRuleAPI = {
  list: (tunnelId = '', nodeId = '') => call<{ items: TunnelRule[]; total: number }>(`/tunnel-rules?${new URLSearchParams({ tunnelId, nodeId })}`),
  create: (input: TunnelRuleInput) => call<TunnelRule>('/tunnel-rules', 'POST', input),
  update: (id: string, input: TunnelRuleInput) => call<TunnelRule>(`/tunnel-rules/${idPath(id)}`, 'PUT', input),
  remove: (id: string) => call<void>(`/tunnel-rules/${idPath(id)}`, 'DELETE'),
};
export const portAPI = {
  check: (nodeId: string, port: number) => call<PortCheck>(`/nodes/${idPath(nodeId)}/ports/check?port=${port}`),
  allocate: (nodeId: string, count?: number) => call<{ ports: number[] }>(`/nodes/${idPath(nodeId)}/ports/allocate`, 'POST', count === undefined ? {} : { count }),
};
export const nodeRelayAPI = {
  list: (nodeId: string) => call<{ items: NodeRelay[] }>(`/nodes/${idPath(nodeId)}/relays`),
  create: (nodeId: string, input: NodeRelayInput) => call<NodeRelay>(`/nodes/${idPath(nodeId)}/relays`, 'POST', input),
};
export async function loadTunnelChoices(includeInbounds = true): Promise<{ nodes: Pick<NodeView, 'id' | 'name' | 'publicIp'>[]; inbounds: Pick<Inbound, 'id' | 'name' | 'nodeId' | 'port' | 'protocol'>[] }> {
  if (import.meta.env.DEV && mockEnabled()) return (await import('./mock')).mockChoices();
  const [nodes, inbounds] = await Promise.all([choices((page) => nodeAPI.list(page, '')), includeInbounds ? choices((page, size) => proxyAPI.inbounds('', page, size)) : Promise.resolve([])]);
  return { nodes, inbounds };
}
export function tunnelError(error: unknown): string {
  if (error instanceof APIError && error.code === 'stale') return '数据已被修改，请刷新页面后重新编辑。';
  if (error instanceof APIError && error.code === 'port_in_use') return `端口已被 ${(error.details.usedBy as { name?: string } | undefined)?.name || '其他对象'} 占用`;
  return error instanceof Error ? error.message : '操作失败，请重试。';
}
