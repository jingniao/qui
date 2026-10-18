import { reactive } from 'vue';
import { request, type Page } from '../../app/apiClient';
export interface WireGuardPeer { public_key: string; endpoint?: string; allowed_ips: string[]; keep_alive?: number; credential_id?: string; }
export interface ProtocolSettings { quic?: { up_mbps?: number; down_mbps?: number; congestion_control?: string; udp_relay_mode?: string }; transport?: string; security?: string; server_name?: string; method?: string; flow?: string; certificate_pem?: string; private_key_pem?: string; trusted_ca_pem?: string; path?: string; service_name?: string; wireguard?: { secret_key: string; addresses: string[]; peers: WireGuardPeer[]; mtu?: number }; }
export interface Inbound { id: string; name: string; nodeId: string; protocol: string; listen: string; port: number; enabled: boolean; subscriptionVisible: boolean; internalRelay: boolean; defaultOutbound: string; allowedUserIds: string[]; settings: ProtocolSettings; relayCredentialId?: string; hasTlsKey: boolean; hasWireGuardKey: boolean; wireGuardPublicKey?: string; createdAt: string; }
export interface Outbound { id: string; name: string; nodeId: string; protocol: string; address: string; port: number; enabled: boolean; targetNodeId: string; targetInboundId: string; settings: ProtocolSettings; supportTcp: boolean; supportUdp: boolean; failurePolicy: string; backupOutboundId: string; hasCredential: boolean; hasWireGuardKey: boolean; wireGuardPublicKey?: string; createdAt: string; }
export interface OutboundHealth { healthy: boolean; latencyMs: number; checkedAt: string; }
export interface Outbound { probeUrl: string; probeInterval: number; health?: OutboundHealth; }
export interface GroupMember { outbound_id: string; priority: number; weight: number; }
export interface OutboundGroup { id: string; name: string; nodeId: string; strategy: string; enabled: boolean; members: GroupMember[]; probeUrl: string; probeInterval: number; failurePolicy: string; backupOutboundId: string; createdAt: string; }
export type GroupInput = Omit<OutboundGroup, 'id' | 'createdAt'>;
export interface ChainHop { position: number; sourceNodeId: string; targetNodeId: string; outboundId: string; targetInboundId: string; credentialId: string; protocol: string; failurePolicy: string; }
export interface ProxyChain { id: string; name: string; nodeIds: string[]; outboundIds: string[]; hops: ChainHop[]; finalOutboundId: string; supportTcp: boolean; supportUdp: boolean; enabled: boolean; failurePolicy: string; backupTargetId: string; createdAt: string; }
export type ChainInput = Omit<ProxyChain, 'id' | 'createdAt' | 'hops'>;
export type InboundInput = Pick<Inbound, 'name' | 'nodeId' | 'protocol' | 'listen' | 'port' | 'enabled' | 'subscriptionVisible' | 'internalRelay' | 'defaultOutbound' | 'allowedUserIds' | 'settings'>;
export type OutboundInput = Pick<Outbound, 'name' | 'nodeId' | 'protocol' | 'address' | 'port' | 'enabled' | 'targetNodeId' | 'targetInboundId' | 'settings' | 'supportTcp' | 'supportUdp' | 'failurePolicy' | 'backupOutboundId' | 'probeUrl' | 'probeInterval'> & { username: string; secret: string; };
export const inboundProtocols = ['http', 'socks5', 'shadowsocks', 'vless', 'trojan', 'wireguard', 'hysteria2', 'tuic'];
export const outboundProtocols = ['direct', 'block', ...inboundProtocols];
export interface ProtocolSupport { protocol: string; direction: string; transport: string[]; address_family: string[]; security: string[]; implemented: string[]; pending: string[]; not_applicable: string[]; source: string; }
// The server publishes the same capability basis it validates against. The page
// option lists below start from the static fallback and are replaced by the
// server table once it loads, so an unsupported protocol can never be offered
// where the server would refuse it. The fallback list below is kept equal to the matrix.
export const protocolOptions = reactive({ inbound: [...inboundProtocols], outbound: [...outboundProtocols], loaded: false });
export async function loadProtocolSupport() {
  if (protocolOptions.loaded) return;
  try {
    const result = await request<{ protocols: ProtocolSupport[] }>('/protocol-support');
    const inbound = result.protocols.filter(item => item.direction === 'inbound' || item.direction === 'both').map(item => item.protocol);
    const rest = result.protocols.filter(item => item.direction === 'outbound' || item.direction === 'both').map(item => item.protocol).filter(item => item !== 'direct' && item !== 'block');
    const outbound = ['direct', 'block', ...rest];
    if (inbound.length) protocolOptions.inbound = inbound;
    if (outbound.length > 2) protocolOptions.outbound = outbound;
    protocolOptions.loaded = true;
  } catch {
    // Keep the static fallback when the basis cannot be read.
  }
}

export const proxyAPI = {
  groups: (nodeId = '', page = 1, pageSize = 20) => request<Page<OutboundGroup>>(`/outbound-groups?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  saveGroup: (id: string | null, value: GroupInput) => request<OutboundGroup>(id ? `/outbound-groups/${encodeURIComponent(id)}` : '/outbound-groups', id ? 'PUT' : 'POST', value),
  deleteGroup: (id: string) => request(`/outbound-groups/${encodeURIComponent(id)}`, 'DELETE'),
  chains: (nodeId = '', page = 1, pageSize = 20) => request<Page<ProxyChain>>(`/proxy-chains?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  chain: (id: string) => request<ProxyChain>(`/proxy-chains/${encodeURIComponent(id)}`),
  saveChain: (id: string | null, value: ChainInput) => request<ProxyChain>(id ? `/proxy-chains/${encodeURIComponent(id)}` : '/proxy-chains', id ? 'PUT' : 'POST', value),
  deleteChain: (id: string) => request(`/proxy-chains/${encodeURIComponent(id)}`, 'DELETE'),
  inbounds: (nodeId = '', page = 1, pageSize = 20) => request<Page<Inbound>>(`/inbounds?nodeId=${nodeId}&page=${page}&pageSize=${pageSize}`),
  outbounds: (nodeId = '', page = 1, pageSize = 20) => request<Page<Outbound>>(`/outbounds?nodeId=${nodeId}&page=${page}&pageSize=${pageSize}`),
  saveInbound: (id: string | null, value: InboundInput) => request<Inbound>(id ? `/inbounds/${id}` : '/inbounds', id ? 'PUT' : 'POST', value),
  saveOutbound: (id: string | null, value: OutboundInput) => request<Outbound>(id ? `/outbounds/${id}` : '/outbounds', id ? 'PUT' : 'POST', value),
  deleteInbound: (id: string) => request(`/inbounds/${id}`, 'DELETE'),
  deleteOutbound: (id: string) => request(`/outbounds/${id}`, 'DELETE'),
  revision: (nodeId: string, summary: string) => request<{ id: number }>('/revisions/proxy', 'POST', { nodeId, summary })
};

export async function choices<T>(read: (page: number, size: number) => Promise<Page<T>>): Promise<T[]> {
  const items: T[] = [];
  for (let page = 1; ; page++) {
    const result = await read(page, 100);
    items.push(...result.items);
    if (items.length >= result.total) return items;
    if (!result.items.length) throw new Error('选项列表发生变化，请刷新后重试');
  }
}
export const failureNames: Record<string, string> = { Block: '阻断', Direct: '直连', BackupOutbound: '备用出站', BackupOutboundGroup: '备用出口组', BackupProxyChain: '备用代理链', BackupChain: '备用代理链' };
export const strategyNames: Record<string, string> = { primary: '主备', round_robin: '轮询', latency: '延迟优先' };
export function healthLabel(outbound?: Outbound, interval = 30) {
  if (!outbound?.health) return '尚无探测结果';
  const checked = Date.parse(outbound.health.checkedAt);
  if (!Number.isFinite(checked) || Date.now() - checked > Math.max(interval * 3, 15) * 1000) return '探测结果已过期';
  return outbound.health.healthy ? `可用 · ${outbound.health.latencyMs} ms` : '探测失败';
}


