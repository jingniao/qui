import type { DeletionPreview, DeletionResult, PendingCleanupNode } from '../../app/deletion';
import { reactive } from 'vue';
export interface RouteSummary {
  type: 'direct' | 'relay' | 'custom' | 'tunnel';
  schemeId?: string;
  schemeName?: string;
  bindingMode?: 'create' | 'reuse';
  path: string[];
  exitNodeId?: string;
  // The drawing's route could not be resolved; path holds only the entry server.
  pathIncomplete?: boolean;
}
// A forward target pool member (G2). The list is ordered: GOST dials it in that
// order for `fifo` and alternates over it for `round`, whatever the listening port.
export interface ForwardTarget { host: string; port: number; }
// The TLS material of a GOST tunnel's exit listener. The panel seals it, masks it
// on the way out and keeps the stored copy when the editor sends it back.
export interface GostRelayTLS { certificate_pem?: string; private_key_pem?: string; client_ca_pem?: string; }
// Port-forward settings. `host`/`port` are the original single-target form; the
// remaining fields are the GOST-only options of settings contract 2 and stay
// absent when unset, so an xray or sing-box forward never carries them.
export interface ForwardSettings {
  host: string; port: number; network: string;
  targets?: ForwardTarget[];
  strategy?: 'round' | 'fifo';
  max_fails?: number;
  fail_timeout?: string;
  port_end?: number;
  bandwidth_in_bps?: number;
  bandwidth_out_bps?: number;
  max_connections?: number;
  // `mode: 'relay'` is the exit half of a GOST tunnel: a listener the canvas
  // generated. It has no target, network or port of its own to edit.
  mode?: 'relay';
  relay?: GostRelayTLS;
}
import { request, type Page } from '../../app/apiClient';
// Types only: the canvas module owns these shapes and imports this file for the
// API, so a type-only import keeps the runtime dependency one-way.
import type { CanvasConnection, CanvasNodeInstance, ExecutionLocation, ProxyNodeItem, RelayScheme } from './proxyNodes';
import type { CoreName, NodeView } from '../nodes/api';
import { serverStatus } from '../nodes/components/serverListModel';
export interface WireGuardPeer { public_key: string; endpoint?: string; allowed_ips: string[]; keep_alive?: number; credential_id?: string; }
// Reality private keys stay on the server; this editor accepts only public data.
export interface RealitySettings { public_key?: string; handshake_target: string; server_names: string[]; short_ids: string[]; }
export interface XHTTPSettings { host?: string; path: string; mode: string; }
// The TLS certificate sources an inbound can record (v1.4.7 section 3). `acme` is
// reserved for the automatic issuance of section 4 and is never offered.
export type TLSSource = '' | 'paste' | 'self_signed' | 'file' | 'acme';
// The panel's read-only view of the certificate it stores for an inbound. It is
// null for a node-side file source and for an inbound without a certificate,
// because the panel cannot read either.
export interface CertificateInfo { subject: string; dnsNames: string[]; ipAddresses: string[]; notAfter: string; selfSigned: boolean; coverageWarning: string; }
// v1.5.0 section 6: a read-only lookup of the certificates covering one domain.
// Node files are paths only; the panel never returns key material.
export interface CertificateFileMatch { certificatePath: string; keyPath: string; dnsNames: string[]; notAfter: string; expired: boolean; }
export interface CertificateLibraryMatch { id: string; domains: string[]; notAfter: string; }
export interface CertificateMatches { domain: string; inventorySupported: boolean; nodeFiles: CertificateFileMatch[]; library: CertificateLibraryMatch[]; cloudflareConfigured: boolean; }
export interface ProtocolSettings { reality?: RealitySettings; xhttp?: XHTTPSettings; forward?: ForwardSettings; quic?: { up_mbps?: number; down_mbps?: number; congestion_control?: string; udp_relay_mode?: string; hop_port_start?: number; hop_port_end?: number; hop_interval_seconds?: number }; transport?: string; security?: string; server_name?: string; method?: string; flow?: string; certificate_pem?: string; private_key_pem?: string; trusted_ca_pem?: string; certificate_path?: string; private_key_path?: string; path?: string; service_name?: string; wireguard?: { secret_key: string; addresses: string[]; peers: WireGuardPeer[]; mtu?: number }; }
// `core` is the core that runs this inbound (K3b); an empty string follows the
// node's current primary core and is never rewritten to a concrete name. The
// last two flags are derived from that core and are null while it is not
// reported (an official sing-box build without with_v2ray_api, for example).
// `tlsSource` is the certificate source the row records; `certificateInfo` is the
// panel's read-only summary of the certificate it holds for it.
export interface Inbound { readonly routeSummary?: RouteSummary; readonly managedBy?: { kind: string; id: string; name: string }; id: string; name: string; nodeId: string; protocol: string; core?: CoreName | ''; listen: string; port: number; enabled: boolean; subscriptionVisible: boolean; internalRelay: boolean; defaultOutbound: string; egressSourceIp?: string; allowedUserIds: string[]; settings: ProtocolSettings; relayCredentialId?: string; tlsSource?: TLSSource; certificateId?: string; certificateInfo?: CertificateInfo | null; hasTlsKey: boolean; hasRealityKey?: boolean; hasWireGuardKey: boolean; wireGuardPublicKey?: string; userTrafficSupported?: boolean | null; deviceGateSupported?: boolean | null; createdAt: string; }
export interface Outbound { id: string; name: string; nodeId: string; protocol: string; address: string; port: number; enabled: boolean; targetNodeId: string; targetInboundId: string; settings: ProtocolSettings; supportTcp: boolean; supportUdp: boolean; failurePolicy: string; backupOutboundId: string; hasCredential: boolean; hasWireGuardKey: boolean; wireGuardPublicKey?: string; createdAt: string; }
export interface OutboundHealth { healthy: boolean; latencyMs: number; checkedAt: string; }
export interface Outbound { sourceIp?: string; sourceInterface?: string; probeUrl: string; probeInterval: number; health?: OutboundHealth; }
export interface GroupMember { outbound_id: string; priority: number; weight: number; }
export interface OutboundGroup { id: string; name: string; nodeId: string; strategy: string; enabled: boolean; members: GroupMember[]; probeUrl: string; probeInterval: number; failurePolicy: string; backupOutboundId: string; createdAt: string; }
export interface ChainHop { position: number; sourceNodeId: string; targetNodeId: string; outboundId: string; targetInboundId: string; credentialId: string; protocol: string; failurePolicy: string; }
export interface ProxyChain { id: string; name: string; nodeIds: string[]; outboundIds: string[]; hops: ChainHop[]; finalOutboundId: string; supportTcp: boolean; supportUdp: boolean; enabled: boolean; failurePolicy: string; backupTargetId: string; createdAt: string; }
export type InboundInput = Pick<Inbound, 'name' | 'nodeId' | 'protocol' | 'listen' | 'port' | 'enabled' | 'subscriptionVisible' | 'internalRelay' | 'defaultOutbound' | 'egressSourceIp' | 'allowedUserIds' | 'settings'> & { core?: CoreName | ''; rotateRealityKey?: boolean; tlsSource?: TLSSource; certificateId?: string; regenerateCertificate?: boolean };
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
  const result = await request<{ protocols: ProtocolSupport[] }>('/protocol-support');
  protocolOptions.inbound = result.protocols.filter(item => item.direction === 'inbound' || item.direction === 'both').map(item => item.protocol);
  const rest = result.protocols.filter(item => item.direction === 'outbound' || item.direction === 'both').map(item => item.protocol).filter(item => item !== 'direct' && item !== 'block');
  protocolOptions.outbound = ['direct', 'block', ...rest];
  protocolOptions.loaded = true;
}

export const proxyAPI = {
  groups: (nodeId = '', page = 1, pageSize = 20) => request<Page<OutboundGroup>>(`/outbound-groups?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  chains: (nodeId = '', page = 1, pageSize = 20) => request<Page<ProxyChain>>(`/proxy-chains?nodeId=${encodeURIComponent(nodeId)}&page=${page}&pageSize=${pageSize}`),
  inbounds: (nodeId = '', page = 1, pageSize = 20) => request<Page<Inbound>>(`/inbounds?nodeId=${nodeId}&page=${page}&pageSize=${pageSize}`),
  outbounds: (nodeId = '', page = 1, pageSize = 20) => request<Page<Outbound>>(`/outbounds?nodeId=${nodeId}&page=${page}&pageSize=${pageSize}`),
  saveInbound: (id: string | null, value: InboundInput) => request<Inbound>(id ? `/inbounds/${id}` : '/inbounds', id ? 'PUT' : 'POST', value),
  saveOutbound: (id: string | null, value: OutboundInput & Pick<Outbound, 'sourceIp' | 'sourceInterface'>) => request<Outbound>(id ? `/outbounds/${id}` : '/outbounds', id ? 'PUT' : 'POST', { ...value, sourceIp: value.protocol === 'block' ? '' : value.sourceIp || '', sourceInterface: value.protocol === 'block' ? '' : value.sourceInterface || '' }),
  inboundDeletionPreview: (id: string) => request<DeletionPreview>(`/inbounds/${encodeURIComponent(id)}/deletion-preview`),
  deleteInbound: (id: string) => request<DeletionResult | undefined>(`/inbounds/${encodeURIComponent(id)}`, 'DELETE')
};

// Section 6: read-only lookup of the certificates covering one domain on one node.
export function certificateMatches(nodeId: string, domain: string): Promise<CertificateMatches> {
  return request<CertificateMatches>(`/nodes/${encodeURIComponent(nodeId)}/certificate-matches?domain=${encodeURIComponent(domain)}`);
}

export async function choices<T>(read: (page: number, size: number) => Promise<Page<T>>): Promise<T[]> {
  for (let attempt = 0; attempt <= 2; attempt++) {
    const items: T[] = [];
    let total: number | undefined;
    for (let page = 1; ; page++) {
      const result = await read(page, 100);
      if (total !== undefined && result.total !== total) break;
      total = result.total;
      items.push(...result.items);
      if (items.length === total) return items;
      if (!result.items.length || items.length > total) break;
    }
  }
  throw new Error('选项列表发生变化，请刷新后重试');
}
export const failureNames: Record<string, string> = { Block: '阻断', Direct: '直连', BackupOutbound: '备用出站', BackupOutboundGroup: '备用出口组', BackupProxyChain: '备用代理链', BackupChain: '备用代理链' };
export const strategyNames: Record<string, string> = { primary: '主备', round_robin: '轮询', latency: '延迟优先' };

// ----------------- Relay canvas schemes (U2b) -----------------
// The server stores a drawing, not a deployment: instances reference a node, an
// inbound or a browser-local import, and connections describe the arrows. Every
// display field the canvas shows is re-derived from those references when the
// page loads.
export type CanvasExecution = 'server' | 'client';
export type CanvasInstanceRole = 'entry' | 'relay' | 'exit';
export type CanvasConnectionKind = 'proxy_chain' | 'tunnel';
export type LegacyCanvasConnectionKind = 'port_forward';
export type CanvasTunnelProtocol = 'tls' | 'wireguard';
export type LegacyCanvasTunnelProtocol = 'gost' | 'brook' | 'nftables';
export type CanvasInstanceRef = `server:${string}` | `inbound:${string}` | `import:${string}` | `outbound:${string}`;
export interface CanvasEntrySecurity {
  security?: '' | 'none' | 'tls' | 'reality';
  transport?: '' | 'tcp' | 'xhttp';
  flow?: '' | 'xtls-rprx-vision';
  realityTarget?: string;
  realityServerName?: string;
}
export interface CanvasEntryBinding extends CanvasEntrySecurity {
  mode: 'create'; inboundId?: string; protocol?: string; core?: string;
  portMode: 'auto' | 'manual'; port?: number;
  groupMode: 'exit_groups' | 'explicit' | 'none'; groupIds?: string[];
}
export interface CanvasPlanEntry {
  instanceId: string; mode: string; nodeId: string; nodeName: string;
  inboundId?: string; protocol: string; port: number; name: string; route: string;
  groups: { id: string; name: string }[] | null; previousRoute?: string;
}
export interface CanvasSchemeInstance { instanceId: string; ref: CanvasInstanceRef; x: number; y: number; role: CanvasInstanceRole; entryBinding?: CanvasEntryBinding; relayPortMode?: 'auto' | 'manual'; relayPort?: number; listenPort?: number; targetPort?: number; }
export interface CanvasSchemeConnection { tunnelId?: string; id: string; source: string; target: string; type: CanvasConnectionKind | LegacyCanvasConnectionKind; tunnelProtocol?: CanvasTunnelProtocol | LegacyCanvasTunnelProtocol; tunnelPort?: number; listenPort?: number; targetHost?: string; targetPort?: number; supportTcp: boolean; supportUdp: boolean; }
export interface CanvasSchemeLayout { instances: CanvasSchemeInstance[]; connections: CanvasSchemeConnection[]; }
export interface CanvasScheme { id: string; name: string; execution: CanvasExecution; layout: CanvasSchemeLayout; createdAt: string; updatedAt: string; publishedAt: string; publishedBatchId: string; }
export interface CanvasSchemeWriteLayout { instances: CanvasSchemeInstance[]; connections: CanvasSchemeConnection[]; }
export interface CanvasSchemeCreate { name: string; execution: CanvasExecution; layout: CanvasSchemeWriteLayout; }
// updatedAt is the optimistic-concurrency token: an update must send the value
// the last read returned, otherwise the server answers 409 stale_scheme.
export type CanvasSchemeUpdate = CanvasSchemeCreate & { updatedAt: string; };

// Publishing a server scheme creates the relay objects it draws and deploys
// them through the normal batches. The result lists each node's revision and
// status so the canvas can show per-node progress.
export interface CanvasPublishNode { nodeId: string; name: string; revision: number; status: string; }
export interface CanvasPublishResult { batchId?: string; nodes: CanvasPublishNode[]; warnings: string[]; }

export interface CanvasSchemeStatus {
  draft: { dirty: boolean; updatedAt: string; publishedAt: string; reasons?: string[] };
  batch: { id?: string; status?: string; items: { nodeId: string; nodeName: string; status: string; revision?: number }[] };
  nodes: { nodeId: string; nodeName: string; confirmedRevision?: number; expectedRevision?: number; confirmed: boolean }[];
  entries: { instanceId: string; inboundId?: string; name?: string; mode?: string; state?: string; route?: string }[];
  subscription: { ready: boolean; reasons: string[] };
  probe: { status: 'untested' | 'reachable' | 'unreachable' | 'incomplete'; at?: string; results?: { inboundId: string; nodeId: string; status: string; at: string; attempted: number; lost: number; medianMs?: number; code: string }[] };
}

export interface CanvasRemoveInput { updatedAt: string; }
export interface CanvasRemoveResult { schemeId: string; removed: true; pendingNodes: PendingCleanupNode[]; }

export interface CanvasTunnelSchemeRef { id: string; name: string; }
export interface CanvasTunnelPlanObject { kind: string; name: string; protocol?: string; port?: number; target?: string; }
export interface CanvasTunnelPlan { ok: boolean; entries?: CanvasPlanEntry[]; message?: string; servers: { nodeId: string; name: string; creates: CanvasTunnelPlanObject[]; updates: CanvasTunnelPlanObject[]; removes: CanvasTunnelPlanObject[] }[]; connections: { id: string; status: string; message: string }[]; conflicts: { nodeId: string; port: number; with: string; connectionId: string; reason?: string; objectId?: string }[]; warnings: string[]; tunnels: { tunnelId: string; affectedSchemes: CanvasTunnelSchemeRef[] }[]; }

export const canvasAPI = {
  status: (id: string) => request<CanvasSchemeStatus>(`/canvas-schemes/${encodeURIComponent(id)}/status`),
  remove: (id: string, value: CanvasRemoveInput) => request<CanvasRemoveResult>(`/canvas-schemes/${encodeURIComponent(id)}/remove`, 'POST', value),
  plan: (id: string, layout?: CanvasSchemeWriteLayout) => request<CanvasTunnelPlan>(`/canvas-schemes/${encodeURIComponent(id)}/plan`, 'POST', { layout }),
  list: (page = 1, pageSize = 50) => request<Page<CanvasScheme>>(`/canvas-schemes?page=${page}&pageSize=${pageSize}`),
  get: (id: string) => request<CanvasScheme>(`/canvas-schemes/${encodeURIComponent(id)}`),
  create: (value: CanvasSchemeCreate) => request<CanvasScheme>('/canvas-schemes', 'POST', value),
  update: (id: string, value: CanvasSchemeUpdate) => request<CanvasScheme>(`/canvas-schemes/${encodeURIComponent(id)}`, 'PUT', value),
  publish: (id: string) => request<CanvasPublishResult>(`/canvas-schemes/${encodeURIComponent(id)}/publish`, 'POST', {}),
  // Which imported nodes can end a server scheme; reason is empty when one can.
  importExits: () => request<{ items: { id: string; reason: string; nodeIds: string[] }[] }>('/canvas-import-exits')
};

// Display data the canvas page has already loaded. Schemas stay loose so the
// node list and the canvas can both pass what they hold.
export type CanvasServerRef = Pick<NodeView, 'id' | 'name' | 'publicIp' | 'hostname' | 'status' | 'enabled' | 'maintenance' | 'deletedAt' | 'lastHeartbeat' | 'offlineAfterSeconds' | 'heartbeat' | 'capabilities' | 'persistedStatus' | 'liveness'>;
export interface CanvasSchemeContext { servers: CanvasServerRef[]; inbounds: Inbound[]; imported: ProxyNodeItem[]; outbounds?: Outbound[]; statusNow?: number; }

// A self-hosted node is identified as self-<nodeId>-<inboundId>. Base64url ids
// contain '-', so the string cannot be split reliably; matching the inbound the
// panel lists is exact.
export function selfHostedResourceId(inbound: Pick<Inbound, 'nodeId' | 'id'>): string {
  return `self-${inbound.nodeId}-${inbound.id}`;
}

export function canvasInstanceRef(instance: CanvasNodeInstance, context: CanvasSchemeContext): CanvasInstanceRef {
  if (instance.resourceId.startsWith('outbound:')) return instance.resourceId as CanvasInstanceRef;
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
    ...(node.entryBinding ? { entryBinding: { ...node.entryBinding, ...(node.entryBinding.groupIds ? { groupIds: [...node.entryBinding.groupIds] } : {}) } } : {}),
    ...(node.resourceType === 'server' && (node.role === 'relay' || node.role === 'exit') ? {
      ...(node.relayPortMode ? { relayPortMode: node.relayPortMode } : {}),
      ...(node.relayPort !== undefined ? { relayPort: node.relayPort } : {})
    } : {}),
    ...(node.listenPort ? { listenPort: node.listenPort } : {}),
    ...(node.targetPort ? { targetPort: node.targetPort } : {})
  }));
  const connections = scheme.connections.map(connection => ({
    id: connection.id,
    ...(connection.tunnelId ? { tunnelId: connection.tunnelId } : {}),
    source: connection.sourceInstanceId,
    target: connection.targetInstanceId,
    type: connection.type || 'proxy_chain',
    ...(connection.tunnelProtocol ? { tunnelProtocol: connection.tunnelProtocol } : {}),
    ...(connection.tunnelPort ? { tunnelPort: connection.tunnelPort } : {}),
    ...(connection.listenPort !== undefined ? { listenPort: connection.listenPort } : {}),
    ...(connection.targetHost ? { targetHost: connection.targetHost } : {}),
    ...(connection.targetPort !== undefined ? { targetPort: connection.targetPort } : {}),
    supportTcp: !!connection.supportTcp,
    supportUdp: !!connection.supportUdp
  }));
  return { name: scheme.name, execution: scheme.executionLocation as CanvasExecution, layout: { instances, connections } };
}

// The status a canvas box shows comes from the node it references, not from the
// scheme: the server never stores it.
export function canvasDisplayStatus(status?: string): CanvasNodeInstance['status'] {
  if (status === 'online' || status === 'offline' || status === 'degraded' || status === 'core_error' || status === 'error' || status === 'applying' || status === 'rollback' || status === 'pending' || status === 'deleted' || status === 'reconnecting' || status === 'maintenance' || status === 'disabled') return status;
  return 'untested';
}

export function canvasServerDisplayStatus(server: CanvasServerRef | undefined, now: number): CanvasNodeInstance['status'] {
  return server ? canvasDisplayStatus(serverStatus(server as NodeView, now).key) : 'deleted';
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
      // Canvas and relay endpoints dial the public address, never the address a
      // client uses (AGENTS.md §14).
      serverAddress: (server?.publicIp || '').trim() || '未设置公网 IP',
      status: canvasServerDisplayStatus(server, context.statusNow ?? Date.now()),
      isManageable: true
    };
  }
  if (kind === 'outbound') {
    const outbound = context.outbounds?.find(item => item.id === identifier);
    return { resourceId: ref, resourceType: 'node', name: outbound?.name || '已删除的出站', protocol: outbound?.protocol || '', serverAddress: outbound ? `${outbound.address}:${outbound.port}` : '', status: outbound?.enabled ? 'untested' : 'offline', isManageable: false };
  }
  if (kind === 'inbound') {
    const inbound = context.inbounds.find(item => item.id === identifier);
    const server = inbound ? context.servers.find(item => item.id === inbound.nodeId) : undefined;
    if (!inbound) {
      return { resourceId: `inbound-${identifier}`, resourceType: 'node', name: '已删除的入站', protocol: '', serverAddress: '', status: 'deleted', isManageable: true };
    }
    let label = (inbound.name || '').trim();
    if (server?.name && label.startsWith(server.name)) label = label.slice(server.name.length).replace(/^[·\s\-_]+/, '').trim();
    const base = label || inbound.protocol.toUpperCase();
    const name = inbound.internalRelay ? `${base} (内部中转)` : inbound.enabled ? base : `${base} (已停用)`;
    const address = (server?.publicIp || '').trim();
    return {
      resourceId: selfHostedResourceId(inbound),
      resourceType: 'node',
      name: server?.name ? `${server.name} · ${name}` : name,
      protocol: inbound.protocol,
      serverAddress: address ? `${address}:${inbound.port}` : '未设置公网 IP',
      status: !server ? 'deleted' : inbound.enabled ? canvasServerDisplayStatus(server, context.statusNow ?? Date.now()) : 'disabled',
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
  const nodes: CanvasNodeInstance[] = (scheme.layout?.instances ?? []).map(instance => {
    const display = canvasReferenceDisplay(instance.ref, context);
    return {
      instanceId: instance.instanceId,
      ...display,
      x: instance.x,
      y: instance.y,
      role: instance.role,
      ...(instance.entryBinding ? { entryBinding: { ...instance.entryBinding, ...(instance.entryBinding.groupIds ? { groupIds: [...instance.entryBinding.groupIds] } : {}) } } : {}),
      relayPortMode: instance.relayPortMode,
      relayPort: instance.relayPort,
      listenPort: instance.listenPort,
      targetPort: instance.targetPort
    };
  });
  const connections: CanvasConnection[] = (scheme.layout?.connections ?? []).map(connection => ({
    id: connection.id,
    ...(connection.tunnelId ? { tunnelId: connection.tunnelId } : {}),
    sourceInstanceId: connection.source,
    targetInstanceId: connection.target,
    type: connection.type,
    tunnelProtocol: connection.tunnelProtocol,
    tunnelPort: connection.tunnelPort,
    listenPort: connection.listenPort,
    targetHost: connection.targetHost,
    targetPort: connection.targetPort,
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
