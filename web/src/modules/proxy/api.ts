import { reactive } from 'vue';
export interface ProtocolSettings { forward?: {host:string;port:number;network:string}; }
import { request, type Page } from '../../app/apiClient';
// Types only: the canvas module owns these shapes and imports this file for the
// API, so a type-only import keeps the runtime dependency one-way.
import type { CanvasConnection, CanvasNodeInstance, ExecutionLocation, ProxyNodeItem, RelayScheme } from './proxyNodes';
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

// ----------------- Relay canvas schemes (U2b) -----------------
// The server stores a drawing, not a deployment: instances reference a node, an
// inbound or a browser-local import, and connections describe the arrows. Every
// display field the canvas shows is re-derived from those references when the
// page loads.
export type CanvasExecution = 'server' | 'client';
export type CanvasInstanceRole = 'entry' | 'relay' | 'exit';
export type CanvasConnectionKind = 'proxy_chain' | 'tunnel' | 'port_forward';
export type CanvasTunnelProtocol = 'tls' | 'wireguard' | 'gost' | 'brook' | 'nftables';
export interface CanvasSchemeInstance { instanceId: string; ref: string; x: number; y: number; role: CanvasInstanceRole; listenPort?: number; targetPort?: number; }
export interface CanvasSchemeConnection { id: string; source: string; target: string; type: CanvasConnectionKind; tunnelProtocol?: CanvasTunnelProtocol; tunnelPort?: number; supportTcp: boolean; supportUdp: boolean; }
export interface CanvasSchemeLayout { instances: CanvasSchemeInstance[]; connections: CanvasSchemeConnection[]; }
export interface CanvasScheme { id: string; name: string; execution: CanvasExecution; layout: CanvasSchemeLayout; createdAt: string; updatedAt: string; publishedAt: string; publishedBatchId: string; }
export interface CanvasSchemeCreate { name: string; execution: CanvasExecution; layout: CanvasSchemeLayout; }
// updatedAt is the optimistic-concurrency token: an update must send the value
// the last read returned, otherwise the server answers 409 stale_scheme.
export type CanvasSchemeUpdate = CanvasSchemeCreate & { updatedAt: string; };

// Publishing a server scheme creates the relay objects it draws and deploys
// them through the normal batches. The result lists each node's revision and
// status so the canvas can show per-node progress.
export interface CanvasPublishNode { nodeId: string; name: string; revision: number; status: string; }
export interface CanvasPublishResult { batchId?: string; nodes: CanvasPublishNode[]; warnings: string[]; }

export const canvasAPI = {
  list: (page = 1, pageSize = 50) => request<Page<CanvasScheme>>(`/canvas-schemes?page=${page}&pageSize=${pageSize}`),
  get: (id: string) => request<CanvasScheme>(`/canvas-schemes/${encodeURIComponent(id)}`),
  create: (value: CanvasSchemeCreate) => request<CanvasScheme>('/canvas-schemes', 'POST', value),
  update: (id: string, value: CanvasSchemeUpdate) => request<CanvasScheme>(`/canvas-schemes/${encodeURIComponent(id)}`, 'PUT', value),
  // Deleting a scheme only deletes the drawing; the objects it draws stay.
  delete: (id: string) => request<void>(`/canvas-schemes/${encodeURIComponent(id)}`, 'DELETE'),
  publish: (id: string) => request<CanvasPublishResult>(`/canvas-schemes/${encodeURIComponent(id)}/publish`, 'POST', {})
};

// Display data the canvas page has already loaded. Schemas stay loose so the
// node list and the canvas can both pass what they hold.
export interface CanvasServerRef { id: string; name: string; publicIp?: string; hostname?: string; status?: string; }
export interface CanvasSchemeContext { servers: CanvasServerRef[]; inbounds: Inbound[]; imported: ProxyNodeItem[]; }

// A self-hosted node is identified as self-<nodeId>-<inboundId>. Base64url ids
// contain '-', so the string cannot be split reliably; matching the inbound the
// panel lists is exact.
export function selfHostedResourceId(inbound: Pick<Inbound, 'nodeId' | 'id'>): string {
  return `self-${inbound.nodeId}-${inbound.id}`;
}

export function canvasInstanceRef(instance: CanvasNodeInstance, context: CanvasSchemeContext): string {
  if (instance.resourceType === 'server') return `server:${instance.resourceId}`;
  const inbound = context.inbounds.find(item => selfHostedResourceId(item) === instance.resourceId);
  if (inbound) return `inbound:${inbound.id}`;
  return `import:${instance.resourceId}`;
}

// Only words of the request the server validates are sent: display fields are
// never stored, and unset ports stay absent instead of becoming zero.
export function relaySchemeToCanvasInput(scheme: RelayScheme, context: CanvasSchemeContext): CanvasSchemeCreate {
  const instances = scheme.nodes.map(node => ({
    instanceId: node.instanceId,
    ref: canvasInstanceRef(node, context),
    x: Math.round(node.x) || 0,
    y: Math.round(node.y) || 0,
    role: node.role || 'relay',
    ...(node.listenPort ? { listenPort: node.listenPort } : {}),
    ...(node.targetPort ? { targetPort: node.targetPort } : {})
  }));
  const connections = scheme.connections.map(connection => ({
    id: connection.id,
    source: connection.sourceInstanceId,
    target: connection.targetInstanceId,
    type: connection.type || 'proxy_chain',
    ...(connection.tunnelProtocol ? { tunnelProtocol: connection.tunnelProtocol } : {}),
    ...(connection.tunnelPort ? { tunnelPort: connection.tunnelPort } : {}),
    supportTcp: !!connection.supportTcp,
    supportUdp: !!connection.supportUdp
  }));
  return { name: scheme.name, execution: scheme.executionLocation as CanvasExecution, layout: { instances, connections } };
}

// The status a canvas box shows comes from the node it references, not from the
// scheme: the server never stores it.
export function canvasDisplayStatus(status?: string): CanvasNodeInstance['status'] {
  if (status === 'online' || status === 'degraded' || status === 'applying') return 'online';
  if (status === 'offline' || status === 'disabled' || status === 'pending') return 'offline';
  if (status === 'error' || status === 'rollback') return 'error';
  return 'untested';
}

interface CanvasReferenceDisplay {
  resourceId: string;
  resourceType: 'server' | 'node';
  name: string;
  protocol: string;
  serverAddress: string;
  status: CanvasNodeInstance['status'];
  isManageable: boolean;
}

function canvasReferenceDisplay(ref: string, context: CanvasSchemeContext): CanvasReferenceDisplay {
  const separator = ref.indexOf(':');
  const kind = separator < 0 ? '' : ref.slice(0, separator);
  const identifier = separator < 0 ? '' : ref.slice(separator + 1);
  if (kind === 'server') {
    const server = context.servers.find(item => item.id === identifier);
    return {
      resourceId: identifier,
      resourceType: 'server',
      name: server?.name || '已删除的服务器',
      protocol: 'server',
      serverAddress: server?.publicIp || server?.hostname || '未分配 IP',
      status: canvasDisplayStatus(server?.status),
      isManageable: true
    };
  }
  if (kind === 'inbound') {
    const inbound = context.inbounds.find(item => item.id === identifier);
    const server = inbound ? context.servers.find(item => item.id === inbound.nodeId) : undefined;
    if (!inbound) {
      return { resourceId: `inbound-${identifier}`, resourceType: 'node', name: '已删除的入站', protocol: '', serverAddress: '', status: 'offline', isManageable: true };
    }
    let label = (inbound.name || '').trim();
    if (server?.name && label.startsWith(server.name)) label = label.slice(server.name.length).replace(/^[·\s\-_]+/, '').trim();
    const base = label || inbound.protocol.toUpperCase();
    const name = inbound.internalRelay ? `${base} (内部中转)` : inbound.enabled ? base : `${base} (已停用)`;
    return {
      resourceId: selfHostedResourceId(inbound),
      resourceType: 'node',
      name: server?.name ? `${server.name} · ${name}` : name,
      protocol: inbound.protocol,
      serverAddress: `${server?.publicIp || server?.hostname || '127.0.0.1'}:${inbound.port}`,
      status: inbound.enabled ? canvasDisplayStatus(server?.status) : 'offline',
      isManageable: true
    };
  }
  const imported = context.imported.find(item => item.id === identifier);
  return {
    resourceId: identifier,
    resourceType: 'node',
    name: imported?.name || '未导入的节点',
    protocol: imported?.protocol || '',
    serverAddress: imported ? `${imported.server}:${imported.port}` : '',
    status: imported?.status || 'untested',
    isManageable: false
  };
}

// Rebuilds the canvas shape from a stored scheme, filling every display field
// from the reference so a renamed node or inbound shows its current name.
export function canvasSchemeToRelayScheme(scheme: CanvasScheme, context: CanvasSchemeContext): RelayScheme {
  const nodes: CanvasNodeInstance[] = scheme.layout.instances.map(instance => {
    const display = canvasReferenceDisplay(instance.ref, context);
    return {
      instanceId: instance.instanceId,
      ...display,
      x: instance.x,
      y: instance.y,
      role: instance.role,
      listenPort: instance.listenPort,
      targetPort: instance.targetPort
    };
  });
  const connections: CanvasConnection[] = scheme.layout.connections.map(connection => ({
    id: connection.id,
    sourceInstanceId: connection.source,
    targetInstanceId: connection.target,
    type: connection.type,
    tunnelProtocol: connection.tunnelProtocol,
    tunnelPort: connection.tunnelPort,
    supportTcp: connection.supportTcp,
    supportUdp: connection.supportUdp,
    status: 'valid'
  }));
  const published = scheme.publishedAt !== '';
  return {
    id: scheme.id,
    name: scheme.name,
    executionLocation: scheme.execution as ExecutionLocation,
    nodes,
    connections,
    published,
    hasUnpublishedChanges: published && scheme.updatedAt > scheme.publishedAt,
    publishedAt: scheme.publishedAt,
    updatedAt: scheme.updatedAt,
    serverUpdatedAt: scheme.updatedAt
  };
}

