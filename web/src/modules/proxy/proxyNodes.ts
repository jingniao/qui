// proxyNodes.ts: Data management for unified proxy nodes & relay canvas schemas
import { reactive } from 'vue';
import { canvasAPI, canvasSchemeToRelayScheme, choices, relaySchemeToCanvasInput, type CanvasSchemeContext } from './api';

export type NodeSourceType = 'self_hosted' | 'subscription' | 'manual';

export interface ProxyNodeItem {
  id: string;
  name: string;
  sourceType: NodeSourceType;
  sourceName: string; // e.g. "香港 HKT" or "AmyTelecom 订阅" or "单独导入"
  sourceId?: string;
  proxyKey?: string;
  inboundId?: string;   // Associated server ID or subscription ID
  server: string;
  port: number;
  protocol: string;    // 'vless' | 'vmess' | 'shadowsocks' | 'trojan' | 'hysteria2' | 'tuic' | 'wireguard' | 'socks5' | 'http'
  status: 'online' | 'offline' | 'untested' | 'error';
  latencyMs?: number;
  lastChecked?: string;
  supportTcp: boolean;
  supportUdp: boolean;
  settings?: Record<string, any>;
  rawLink?: string;
  link?: string;
  onlyViaRelay?: boolean; // If true, only reachable via relay, hide public direct entrance
  createdAt: string;
}

export interface SubscriptionItem {
	userIds?: string[];
	intervalSeconds?: number;
	lastStatus?: string;
	lastError?: string;
  id: string;
  name: string;
  url: string;
  nodeCount: number;
  autoUpdate: boolean;
  lastUpdated?: string;
  expireDate?: string;
  usedTraffic?: string;
  totalTraffic?: string;
}

// ----------------- Canvas Scheme Types -----------------
export type ExecutionLocation = 'client' | 'server';
export type ConnectionType = 'tunnel' | 'port_forward' | 'proxy_chain';

export interface CanvasNodeInstance {
  instanceId: string;
  resourceId: string;     // Ref to ProxyNodeItem.id or NodeView.id (server)
  resourceType: 'server' | 'node';
  name: string;
  protocol: string;
  serverAddress: string;
  status: 'online' | 'offline' | 'untested' | 'error';
  isManageable: boolean;  // True if it's an owned server with Agent; False if external/imported
  x: number;
  y: number;
  role: 'entry' | 'relay' | 'exit';
  listenPort?: number;
  targetPort?: number;
}

export interface CanvasConnection {
  id: string;
  sourceInstanceId: string;
  targetInstanceId: string;
  type: ConnectionType;
  tunnelProtocol?: 'gost' | 'tls' | 'wireguard' | 'brook' | 'nftables';
  tunnelPort?: number;
  listenPort?: number;
  targetHost?: string;
  targetPort?: number;
  supportTcp: boolean;
  supportUdp: boolean;
  status: 'valid' | 'invalid' | 'active';
  invalidReason?: string;
}

export interface RelayScheme {
  id: string;
  name: string;
  executionLocation: ExecutionLocation;
  nodes: CanvasNodeInstance[];
  connections: CanvasConnection[];
  published: boolean;
  hasUnpublishedChanges?: boolean;
  publishedAt?: string;
  updatedAt: string;
  // Version token the server returned with the last save. It is what the next
  // update sends so a second editor cannot overwrite the first one; `updatedAt`
  // stays the local "last touched" stamp the canvas shows.
  serverUpdatedAt?: string;
  clientType?: 'clash' | 'surge' | 'singbox';
}

// Route info attached to an exit node
export interface NodeRelayRoute {
  schemeId: string;
  schemeName: string;
  connectionId: string;
  targetInstanceId: string;
  pathDisplay: string;         // e.g. "香港 → 日本" or "上海 → 香港 → 日本"
  pathNodes: string[];         // e.g. ["香港入口", "日本中转", "日本出口"]
  executionLocation: ExecutionLocation;
  published: boolean;
  hasUnpublishedChanges: boolean;
  statusDisplay: 'published' | 'draft' | 'modified';
  canRestoreToCanvas: boolean;
  unrestorableReason?: string;
}

const STORAGE_KEY_SUBS = 'qui_proxy_subscriptions';
const STORAGE_KEY_MANUAL_NODES = 'qui_proxy_manual_nodes';
// Schemes moved to the server in U2b. The key is only read once, by the canvas,
// to offer importing what an earlier build stored in this browser.
export const LEGACY_SCHEMES_KEY = 'qui_relay_schemes_v3';

// Demo records seeded by earlier builds. Browsers that ran those builds still hold them in
// localStorage, so drop them once on load and persist the cleaned list.
const DEMO_SUBSCRIPTION_IDS = new Set(['sub-amy-telecom']);
// 'manual-2' is the older demo Japan node id; earlier builds renamed it to '日本节点 · VLESS'
// but kept the id, so cached copies survive with that id.
const DEMO_NODE_IDS = new Set(['manual-jp', 'manual-2', 'manual-relay-only', 'manual-1', 'sub-node-1', 'sub-node-2']);

function isSeededDemoNode(node: ProxyNodeItem): boolean {
  if (DEMO_NODE_IDS.has(node.id)) return true;
  return typeof node.sourceId === 'string' && DEMO_SUBSCRIPTION_IDS.has(node.sourceId);
}

export const proxyNodeStore = reactive({
  subscriptions: [] as SubscriptionItem[],
  manualNodes: [] as ProxyNodeItem[],
  schemes: [] as RelayScheme[]
});

export function loadStoredData() {
  try {
    const rawSubs = localStorage.getItem(STORAGE_KEY_SUBS);
    if (rawSubs) {
      const storedSubs: SubscriptionItem[] = JSON.parse(rawSubs);
      const keptSubs = storedSubs.filter(sub => !DEMO_SUBSCRIPTION_IDS.has(sub.id));
      proxyNodeStore.subscriptions = keptSubs;
      if (keptSubs.length !== storedSubs.length) saveSubscriptions();
    } else {
      proxyNodeStore.subscriptions = [];
    }

    const rawManual = localStorage.getItem(STORAGE_KEY_MANUAL_NODES);
    if (rawManual) {
      const storedNodes: ProxyNodeItem[] = JSON.parse(rawManual);
      const keptNodes = storedNodes.filter(node => !isSeededDemoNode(node));
      proxyNodeStore.manualNodes = keptNodes;
      if (keptNodes.length !== storedNodes.length) saveManualNodes();
    } else {
      proxyNodeStore.manualNodes = [];
    }

    // Relay schemes live on the server (U2b). A browser that still holds the
    // copy an earlier build left in localStorage keeps it until the canvas
    // offers to import it, so nothing here reads or writes scheme storage.
    proxyNodeStore.schemes = [];
  } catch (e) {
    console.error('Error loading proxy stored data:', e);
  }
}

export function saveSubscriptions() {
  localStorage.setItem(STORAGE_KEY_SUBS, JSON.stringify(proxyNodeStore.subscriptions));
}

export function saveManualNodes() {
  localStorage.setItem(STORAGE_KEY_MANUAL_NODES, JSON.stringify(proxyNodeStore.manualNodes));
}

// The canvas keeps its drawings on the server since U2b. These helpers are the
// only path from the running canvas to that storage, so a page can never leave
// a scheme behind in one browser only.

export async function loadCanvasSchemes(context: CanvasSchemeContext): Promise<void> {
  const schemes = await choices((page, size) => canvasAPI.list(page, size));
  proxyNodeStore.schemes = schemes.map(item => canvasSchemeToRelayScheme(item, context));
}

// persistCanvasScheme creates the scheme the first time and updates it after
// that, sending the version the server returned so a second editor is told to
// reload instead of overwriting it.
export async function persistCanvasScheme(scheme: RelayScheme, context: CanvasSchemeContext): Promise<void> {
  const input = relaySchemeToCanvasInput(scheme, context);
  const saved = scheme.serverUpdatedAt
    ? await canvasAPI.update(scheme.id, { ...input, updatedAt: scheme.serverUpdatedAt })
    : await canvasAPI.create(input);
  scheme.id = saved.id;
  scheme.updatedAt = saved.updatedAt;
  scheme.serverUpdatedAt = saved.updatedAt;
  scheme.publishedAt = saved.publishedAt;
  scheme.published = saved.publishedAt !== '';
  scheme.hasUnpublishedChanges = scheme.published && saved.updatedAt > saved.publishedAt;
}

export async function createCanvasScheme(name: string, executionLocation: ExecutionLocation, context: CanvasSchemeContext): Promise<RelayScheme> {
  const saved = await canvasAPI.create({ name, execution: executionLocation, layout: { instances: [], connections: [] } });
  const scheme = canvasSchemeToRelayScheme(saved, context);
  proxyNodeStore.schemes.push(scheme);
  return scheme;
}

export async function deleteCanvasScheme(schemeId: string): Promise<void> {
  await canvasAPI.delete(schemeId);
  proxyNodeStore.schemes = proxyNodeStore.schemes.filter(item => item.id !== schemeId);
}

/**
 * Find all relay routes targeting the given exit node.
 * Traces back incoming connections to create path: e.g. "香港 → 日本", "上海 → 香港 → 日本"
 */
export function getNodeRelayRoutes(node: ProxyNodeItem, schemes: RelayScheme[]): NodeRelayRoute[] {
  const routes: NodeRelayRoute[] = [];

  for (const scheme of schemes) {
    // Find all node instances matching this node (by resourceId or by server/port/inbound)
    const exitInstances = scheme.nodes.filter(
      inst =>
        inst.resourceId === node.id ||
        (node.inboundId && inst.resourceId === node.inboundId) ||
        (!!node.server && node.port > 0 && inst.serverAddress === `${node.server}:${node.port}`)
    );

    for (const exitInst of exitInstances) {
      // Find incoming connections targeting this exit instance
      const inConns = scheme.connections.filter(c => c.targetInstanceId === exitInst.instanceId);

      for (const inConn of inConns) {
        // Trace back paths from inConn.sourceInstanceId to form complete paths
        const paths = traceBackwardPaths(scheme, inConn.sourceInstanceId);

        for (const p of paths) {
          const allInstIds = [...p, exitInst.instanceId];
          const nodeNames = allInstIds.map(id => {
            const inst = scheme.nodes.find(n => n.instanceId === id);
            return inst ? inst.name : '未知';
          });
          const pathDisplay = nodeNames.join(' → ');

          let statusDisplay: 'published' | 'draft' | 'modified' = 'draft';
          if (scheme.published) {
            statusDisplay = scheme.hasUnpublishedChanges ? 'modified' : 'published';
          } else {
            statusDisplay = 'draft';
          }

          routes.push({
            schemeId: scheme.id,
            schemeName: scheme.name,
            connectionId: inConn.id,
            targetInstanceId: exitInst.instanceId,
            pathDisplay,
            pathNodes: nodeNames,
            executionLocation: scheme.executionLocation,
            published: scheme.published,
            hasUnpublishedChanges: !!scheme.hasUnpublishedChanges,
            statusDisplay,
            canRestoreToCanvas: true
          });
        }
      }
    }
  }

  return routes;
}

// Helper: backtrack all paths ending at node currentInstId
function traceBackwardPaths(scheme: RelayScheme, currentInstId: string, visited: Set<string> = new Set()): string[][] {
  if (visited.has(currentInstId)) {
    return [[currentInstId]];
  }
  const newVisited = new Set(visited);
  newVisited.add(currentInstId);

  const inConns = scheme.connections.filter(c => c.targetInstanceId === currentInstId);
  if (!inConns.length) {
    return [[currentInstId]];
  }

  const result: string[][] = [];
  for (const conn of inConns) {
    const subPaths = traceBackwardPaths(scheme, conn.sourceInstanceId, newVisited);
    for (const sub of subPaths) {
      result.push([...sub, currentInstId]);
    }
  }
  return result;
}

/**
 * Creates or retrieves a draft scheme targeting the node as exit target. The
 * scheme is created on the server first: the canvas loads nothing else, so a
 * draft that only existed here would be lost on the next page load.
 */
export async function createDraftSchemeForTargetNode(node: ProxyNodeItem, context: CanvasSchemeContext): Promise<{ scheme: RelayScheme; exitInstanceId: string }> {
  // Check if there is already an existing draft targeting this node
  const existingDraft = proxyNodeStore.schemes.find(
    s => !s.published && s.nodes.some(n => n.resourceId === node.id)
  );

  if (existingDraft) {
    const exitInst = existingDraft.nodes.find(n => n.resourceId === node.id)!;
    return { scheme: existingDraft, exitInstanceId: exitInst.instanceId };
  }

  const exitInstId = `inst-exit-${Date.now()}`;
  const exitNodeInstance: CanvasNodeInstance = {
    instanceId: exitInstId,
    resourceId: node.id,
    resourceType: 'node',
    name: node.name,
    protocol: node.protocol,
    serverAddress: `${node.server}:${node.port}`,
    status: node.status,
    isManageable: node.sourceType === 'self_hosted',
    x: 680,
    y: 200,
    role: 'exit',
    targetPort: node.port
  };

  const newScheme = await createCanvasScheme(`${node.name} 中转方案 (草稿)`, 'server', context);
  newScheme.nodes = [exitNodeInstance];
  newScheme.hasUnpublishedChanges = true;
  await persistCanvasScheme(newScheme, context);

  return { scheme: newScheme, exitInstanceId: exitInstId };
}

// Simple protocol parser helper for links
export function parseProxyLink(link: string): Partial<ProxyNodeItem> | null {
  const trimmed = link.trim();
  if (trimmed.startsWith('vmess://')) {
    try {
      const b64 = trimmed.slice(8);
      const json = JSON.parse(atob(b64));
      return {
        name: json.ps || 'VMess 节点',
        server: json.add,
        port: Number(json.port) || 443,
        protocol: 'vmess',
        status: 'untested',
        supportTcp: true,
        supportUdp: true,
        settings: json
      };
    } catch {}
  } else if (trimmed.startsWith('vless://') || trimmed.startsWith('trojan://') || trimmed.startsWith('hysteria2://') || trimmed.startsWith('hy2://')) {
    try {
      const url = new URL(trimmed);
      const proto = url.protocol.replace(':', '').replace('hy2', 'hysteria2');
      const hashName = decodeURIComponent(url.hash ? url.hash.slice(1) : `${proto.toUpperCase()} 节点`);
      return {
        name: hashName,
        server: url.hostname,
        port: Number(url.port) || 443,
        protocol: proto,
        status: 'untested',
        supportTcp: true,
        supportUdp: true
      };
    } catch {}
  } else if (trimmed.startsWith('ss://')) {
    try {
      const url = new URL(trimmed);
      const hashName = decodeURIComponent(url.hash ? url.hash.slice(1) : 'Shadowsocks 节点');
      return {
        name: hashName,
        server: url.hostname,
        port: Number(url.port) || 8388,
        protocol: 'shadowsocks',
        status: 'untested',
        supportTcp: true,
        supportUdp: true
      };
    } catch {}
  }
  return null;
}

export function buildProxyLink(node: ProxyNodeItem): string {
  if (node.sourceType === 'self_hosted') {
    return '';
  }
  if (node.rawLink) return node.rawLink;
  const nameEncoded = encodeURIComponent(node.name || 'Proxy Node');
  const host = node.server || '127.0.0.1';
  const port = node.port || 443;
  const proto = (node.protocol || 'vless').toLowerCase();

  const hasCredential =
    !!(node.settings as any)?.id ||
    !!(node.settings as any)?.uuid ||
    !!(node.settings as any)?.password ||
    !!(node.settings as any)?.method ||
    !!(node.settings as any)?.psk;

  if (!hasCredential) {
    return '';
  }

  if (proto === 'vless') {
    const sec = (node.settings?.security as string) || 'tls';
    const type = (node.settings?.transport as string) || 'tcp';
    const id = (node.settings as any)?.id || (node.settings as any)?.uuid;
    if (!id || id === 'auto') return '';
    return `vless://${id}@${host}:${port}?encryption=none&security=${sec}&type=${type}&sni=${host}#${nameEncoded}`;
  } else if (proto === 'trojan') {
    const type = (node.settings?.transport as string) || 'tcp';
    const password = (node.settings as any)?.password;
    if (!password || password === 'auto') return '';
    return `trojan://${password}@${host}:${port}?security=tls&type=${type}&sni=${host}#${nameEncoded}`;
  } else if (proto === 'hysteria2' || proto === 'hy2') {
    const password = (node.settings as any)?.password;
    if (!password || password === 'auto') return '';
    return `hysteria2://${password}@${host}:${port}/?insecure=0&sni=${host}#${nameEncoded}`;
  } else if (proto === 'vmess') {
    const id = (node.settings as any)?.id || (node.settings as any)?.uuid;
    if (!id || id === 'auto') return '';
    const vmessConfig: any = {
      v: '2',
      ps: node.name,
      add: host,
      port: String(port),
      id: id,
      aid: (node.settings as any)?.aid || '0',
      scy: (node.settings as any)?.scy || 'auto',
      net: (node.settings?.transport as string) || 'tcp',
      type: (node.settings as any)?.type || 'none',
      tls: (node.settings?.security as string) || ''
    };
    try {
      const b64Str = btoa(unescape(encodeURIComponent(JSON.stringify(vmessConfig))));
      return `vmess://${b64Str}`;
    } catch {
      return '';
    }
  } else if (proto === 'shadowsocks' || proto === 'ss') {
    const method = (node.settings as any)?.method || 'aes-256-gcm';
    const password = (node.settings as any)?.password;
    if (!password || password === 'auto') return '';
    const auth = btoa(unescape(encodeURIComponent(`${method}:${password}`)));
    return `ss://${auth}@${host}:${port}#${nameEncoded}`;
  }
  return '';
}
