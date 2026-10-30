import type { CanvasConnection, CanvasNodeInstance, RelayScheme } from '../../proxyNodes';

export interface PlanObject {
  kind: string;
  name: string;
  protocol?: string;
  port?: number;
  target?: string;
}

export interface PlanServer {
  nodeId: string;
  name: string;
  creates: PlanObject[] | null;
  updates: PlanObject[] | null;
  removes: PlanObject[] | null;
}

export interface TunnelPlan {
  ok: boolean;
  servers: PlanServer[] | null;
  connections: { id: string; status: string; message: string }[] | null;
  conflicts: { nodeId: string; port: number; with: string; connectionId: string }[] | null;
  warnings: string[] | null;
  message?: string;
}

export interface TunnelServer {
  id: string;
  name: string;
  publicIp?: string;
  status?: string;
  capabilities?: { core_name?: string; core_version?: string };
}

export interface TunnelDraft {
  id: string;
  source: string;
  target: string;
  kind: 'encrypted' | 'forward';
  protocol: NonNullable<CanvasConnection['tunnelProtocol']>;
  port: number;
  listenPort: number;
  targetHost: string;
  targetPort: number;
  tcp: boolean;
  udp: boolean;
}

export function defaultPort(protocol: string) {
  return protocol === 'wireguard' ? 51820 : 8443;
}

export function createTunnelDraft(scheme: RelayScheme, connection?: CanvasConnection, source = '', target = ''): TunnelDraft {
  const c = connection;
  const protocol = c?.tunnelProtocol || 'tls';
  const from = scheme.nodes.find(n => n.instanceId === c?.sourceInstanceId);
  const to = scheme.nodes.find(n => n.instanceId === c?.targetInstanceId);

  return {
    id: c?.id || `conn-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    source: c?.sourceInstanceId || source,
    target: c?.targetInstanceId || target,
    kind: c?.type === 'port_forward' ? 'forward' : 'encrypted',
    protocol,
    port: c?.tunnelPort ?? (protocol === 'wireguard' ? 51820 : 8443),
    listenPort: c?.listenPort ?? from?.listenPort ?? 10080,
    targetHost: c?.targetHost || '',
    targetPort: c?.targetPort ?? to?.targetPort ?? 443,
    tcp: c?.supportTcp ?? true,
    udp: c?.supportUdp ?? (protocol === 'wireguard' ? true : false)
  };
}

// Selection keys refer either to an existing instance or a server not yet drawn.
export function tunnelChoices(scheme: RelayScheme, servers: TunnelServer[]) {
  return [
    ...scheme.nodes.map(n => ({
      key: n.instanceId,
      name: n.name,
      address: n.serverAddress,
      status: n.status,
      core: servers.find(s => n.resourceType === 'server' && s.id === n.resourceId)?.capabilities?.core_name || '—'
    })),
    ...servers.filter(s => !scheme.nodes.some(n => n.resourceType === 'server' && n.resourceId === s.id)).map(s => ({
      key: `server:${s.id}`,
      name: s.name,
      address: s.publicIp || '',
      status: s.status || '未知',
      core: s.capabilities?.core_name || '—'
    }))
  ];
}

export function applyTunnel(scheme: RelayScheme, draft: TunnelDraft, servers: TunnelServer[]): RelayScheme {
  const nodes = scheme.nodes.map(n => ({ ...n }));

  function resolve(key: string, role: 'entry' | 'exit') {
    const existing = nodes.find(n => n.instanceId === key);
    if (existing) return existing.instanceId;
    const server = servers.find(s => `server:${s.id}` === key);
    if (!server) return key;
    const reused = nodes.find(n => n.resourceType === 'server' && n.resourceId === server.id);
    if (reused) return reused.instanceId;
    const index = nodes.length;
    const instance: CanvasNodeInstance = {
      instanceId: `inst-${draft.id}-${role}`,
      resourceId: server.id,
      resourceType: 'server',
      name: server.name,
      protocol: 'server',
      serverAddress: server.publicIp || '',
      status: server.status === 'online' ? 'online' : 'offline',
      isManageable: true,
      x: 60 + (index % 3) * 300,
      y: 60 + Math.floor(index / 3) * 160,
      role
    };
    nodes.push(instance);
    return instance.instanceId;
  }

  const source = resolve(draft.source, 'entry');
  const target = resolve(draft.target, 'exit');
  const previous = scheme.connections.find(c => c.id === draft.id);

  const connection: CanvasConnection = {
    id: draft.id,
    sourceInstanceId: source,
    targetInstanceId: target,
    type: draft.kind === 'forward' ? 'port_forward' : previous?.type === 'proxy_chain' ? 'proxy_chain' : 'tunnel',
    supportTcp: draft.tcp,
    supportUdp: draft.udp,
    status: 'valid',
    ...(draft.kind === 'forward'
      ? {
          listenPort: draft.listenPort,
          targetPort: draft.targetPort,
          targetHost: draft.targetHost.trim()
        }
      : {
          tunnelProtocol: draft.protocol,
          tunnelPort: draft.port
        })
  };

  // Keep instance ports: other legacy forwards may still fall back to them.
  return {
    ...scheme,
    nodes,
    connections: [...scheme.connections.filter(c => c.id !== draft.id), connection]
  };
}

export function validateTunnel(scheme: RelayScheme, draft: TunnelDraft, servers: TunnelServer[]): string {
  if (!draft.source) return '请选择起点服务器';
  if (!draft.target) return '请选择终点服务器';
  if (draft.source === draft.target) return '起点和终点不能是相同服务器';
  if (!draft.tcp && !draft.udp) return '至少启用 TCP 或 UDP';
  if (draft.kind === 'encrypted') {
    if (['gost', 'brook', 'nftables'].includes(draft.protocol)) return `协议 ${draft.protocol} 需要 GOST 核心，当前不可用`;
    if (!Number.isInteger(draft.port) || draft.port < 1 || draft.port > 65535) return '端口必须为 1–65535';
    const target = scheme.nodes.find(n => n.instanceId === draft.target);
    if (target && !target.isManageable && scheme.executionLocation !== 'client') return '加密隧道终点必须是可管理服务器（或客户端方案）';
  } else {
    if (!Number.isInteger(draft.listenPort) || draft.listenPort < 1 || draft.listenPort > 65535) return '端口必须为 1–65535';
    if (!Number.isInteger(draft.targetPort) || draft.targetPort < 1 || draft.targetPort > 65535) return '端口必须为 1–65535';
  }
  return '';
}

export function tunnelSummary(scheme: RelayScheme, connection: CanvasConnection): { title: string; kind: string; network: string } {
  const src = scheme.nodes.find(n => n.instanceId === connection.sourceInstanceId);
  const tgt = scheme.nodes.find(n => n.instanceId === connection.targetInstanceId);
  const title = src && tgt ? `${src.name} → ${tgt.name}` : '';
  const network = connection.supportTcp && connection.supportUdp ? 'TCP / UDP' : connection.supportTcp ? 'TCP' : connection.supportUdp ? 'UDP' : '—';

  let kind = '';
  if (connection.type === 'port_forward') {
    const listen = connection.listenPort ?? src?.listenPort ?? '';
    const host = connection.targetHost || tgt?.serverAddress || '';
    const targetDisplay = host.includes(':') ? `[${host}]` : host;
    const targetP = connection.targetPort ?? tgt?.targetPort ?? '';
    kind = `端口转发 :${listen}→${targetDisplay ? targetDisplay + ':' : ''}${targetP}`;
  } else {
    const proto = (connection.tunnelProtocol || 'tls').toUpperCase();
    const port = connection.tunnelPort ?? defaultPort(connection.tunnelProtocol || 'tls');
    kind = `${connection.type === 'proxy_chain' ? '加密中转' : '隧道'} ${proto} :${port}`;
  }

  return { title, kind, network };
}

export function tunnelStatus(
  plan: TunnelPlan | null | undefined,
  connectionId: string,
  isClient = false
): { error: boolean; text: string } | null {
  if (isClient) return null;
  if (!plan) return { text: '待检查', error: false };

  const conn = plan.connections?.find(c => c.id === connectionId);
  const conflicts = plan.conflicts?.filter(c => c.connectionId === connectionId) || [];

  if (conn) {
    if (conn.status === 'ok') {
      return { error: false, text: '可发布' };
    }
    const msgs = [conn.message];
    for (const conf of conflicts) {
      msgs.push(`端口冲突 :${conf.port} (${conf.with})`);
    }
    return { error: true, text: msgs.filter(Boolean).join('；') };
  }

  if (conflicts.length > 0) {
    const conflictMsgs = conflicts.map(c => `端口冲突 :${c.port} (${c.with})`).join('；');
    return { error: true, text: conflictMsgs };
  }

  if (!plan.ok && plan.message) {
    return { error: true, text: plan.message };
  }

  return { error: false, text: '可发布' };
}

export function planLines(plan: TunnelPlan | null | undefined, nodeIds?: string[]): string[] {
  if (!plan || !plan.servers) return [];
  const lines: string[] = [];
  const filtered = nodeIds ? plan.servers.filter(s => nodeIds.includes(s.nodeId)) : plan.servers;

  for (const s of filtered) {
    if (s.creates) {
      for (const obj of s.creates) {
        lines.push(`${s.name}：新增${obj.kind === 'inbound' ? '入站' : obj.kind} ${obj.name}${obj.protocol ? ' ' + obj.protocol : ''}${obj.port ? ' :' + obj.port : ''}`);
      }
    }
    if (s.updates) {
      for (const obj of s.updates) {
        lines.push(`${s.name}：更新${obj.kind === 'outbound' ? '出站' : obj.kind} ${obj.name}${obj.target ? ' → ' + obj.target : ''}`);
      }
    }
    if (s.removes) {
      for (const obj of s.removes) {
        lines.push(`${s.name}：移除${obj.kind === 'chain' ? '代理链' : obj.kind} ${obj.name}`);
      }
    }
  }

  return lines;
}
