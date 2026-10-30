import type { CanvasConnection, CanvasNodeInstance, RelayScheme } from '../../proxyNodes';

export interface PlanObject { kind: string; name: string; protocol?: string; port?: number; target?: string; }
export interface PlanServer { nodeId: string; name: string; creates: PlanObject[] | null; updates: PlanObject[] | null; removes: PlanObject[] | null; }
export interface TunnelPlan {
  ok: boolean;
  servers: PlanServer[] | null;
  connections: { id: string; status: string; message: string }[] | null;
  conflicts: { nodeId: string; port: number; with: string; connectionId: string }[] | null;
  warnings: string[] | null;
  message?: string;
}
export interface TunnelServer { id: string; name: string; publicIp?: string; status?: string; capabilities?: { core_name?: string; core_version?: string }; }
export interface TunnelDraft {
  id: string; source: string; target: string; kind: 'encrypted' | 'forward';
  protocol: NonNullable<CanvasConnection['tunnelProtocol']>; port: number;
  listenPort: number; targetHost: string; targetPort: number; tcp: boolean; udp: boolean;
}
export function defaultPort(protocol: string) { return protocol === 'wireguard' ? 51820 : 8443; }
export function createTunnelDraft(scheme: RelayScheme, connection?: CanvasConnection, source = '', target = ''): TunnelDraft {
  const c = connection;
  const protocol = c?.tunnelProtocol || 'tls';
  const from = scheme.nodes.find(n => n.instanceId === c?.sourceInstanceId);
  const to = scheme.nodes.find(n => n.instanceId === c?.targetInstanceId);
  return {
    id: c?.id || `conn-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    source: c?.sourceInstanceId || source, target: c?.targetInstanceId || target,
    kind: c?.type === 'port_forward' ? 'forward' : 'encrypted', protocol,
    port: c?.tunnelPort ?? defaultPort(protocol), listenPort: c?.listenPort ?? from?.listenPort ?? 10080,
    targetHost: c?.targetHost || '', targetPort: c?.targetPort ?? to?.targetPort ?? 443,
    tcp: c?.supportTcp ?? true, udp: c?.supportUdp ?? true
  };
}
// Selection keys refer either to an existing instance or a server not yet drawn.
export function tunnelChoices(scheme: RelayScheme, servers: TunnelServer[]) {
  return [
    ...scheme.nodes.map(n => ({ key: n.instanceId, name: n.name, address: n.serverAddress, status: n.status,
      core: servers.find(s => n.resourceType === 'server' && s.id === n.resourceId)?.capabilities?.core_name || '—' })),
    ...servers.filter(s => !scheme.nodes.some(n => n.resourceType === 'server' && n.resourceId === s.id)).map(s => ({
      key: `server:${s.id}`, name: s.name, address: s.publicIp || '', status: s.status || '未知', core: s.capabilities?.core_name || '—'
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
    const instance: CanvasNodeInstance = { instanceId: `inst-${draft.id}-${role}`, resourceId: server.id, resourceType: 'server',
      name: server.name, protocol: 'server', serverAddress: server.publicIp || '',
      status: server.status === 'online' ? 'online' : 'offline', isManageable: true,
      x: 60 + (index % 3) * 300, y: 60 + Math.floor(index / 3) * 160, role };
    nodes.push(instance);
    return instance.instanceId;
  }
  const source = resolve(draft.source, 'entry'), target = resolve(draft.target, 'exit');
  const previous = scheme.connections.find(c => c.id === draft.id);
  const connection: CanvasConnection = {
    id: draft.id, sourceInstanceId: source, targetInstanceId: target,
    type: draft.kind === 'forward' ? 'port_forward' : previous?.type === 'proxy_chain' ? 'proxy_chain' : 'tunnel',
    supportTcp: draft.tcp, supportUdp: draft.udp, status: 'valid',
    ...(draft.kind === 'forward' ? { listenPort: draft.listenPort, targetPort: draft.targetPort, targetHost: draft.targetHost.trim() }
      : { tunnelProtocol: draft.protocol, tunnelPort: draft.port })
  };
  // Keep instance ports: other legacy forwards may still fall back to them.
  return { ...scheme, nodes, connections: [...scheme.connections.filter(c => c.id !== draft.id), connection] };
}
export function validateTunnel(scheme: RelayScheme, draft: TunnelDraft, servers: TunnelServer[]): string {
  if (!draft.source || !draft.target) return '请选择起点与终点';
  const next = applyTunnel(scheme, draft, servers);
  const c = next.connections.find(c => c.id === draft.id)!;
  if (c.sourceInstanceId === c.targetInstanceId) return '起点与终点不能相同';
  const from = next.nodes.find(n => n.instanceId === c.sourceInstanceId);
  const to = next.nodes.find(n => n.instanceId === c.targetInstanceId);
  if (!from || !to) return '节点已不存在，请重新选择';
  if ((scheme.executionLocation === 'server' || draft.kind === 'forward') && (!from.isManageable || !to.isManageable))
    return '服务器隧道要求两端均为可管理服务器。导入节点不能部署 Agent 或安装服务器隧道。';
  if (!draft.tcp && !draft.udp) return '至少选择 TCP 或 UDP';
  const ports = draft.kind === 'forward' ? [draft.listenPort, draft.targetPort] : [draft.port];
  if (ports.some(p => !Number.isInteger(p) || p < 1 || p > 65535)) return '端口必须为 1–65535 的整数';
  if (draft.kind === 'encrypted' && !['tls', 'wireguard'].includes(draft.protocol)) return '需要 GOST 核心（后续阶段）';
  return '';
}
export function tunnelStatus(plan: TunnelPlan | null, id: string, client = false) {
  if (client) return null;
  if (!plan) return { error: false, text: '待检查' };
  const conflicts = (plan.conflicts || []).filter(c => c.connectionId === id);
  const row = plan.connections?.find(c => c.id === id);
  const messages = [...(row?.status === 'error' ? [row.message || '检查失败'] : []),
    ...conflicts.map(c => `端口冲突 :${c.port}（${c.with}）`)];
  if (messages.length) return { error: true, text: messages.join('；') };
  if (row?.status === 'ok') return { error: false, text: '可发布' };
  return { error: !plan.ok, text: plan.message || '待检查' };
}
export function tunnelSummary(scheme: RelayScheme, c: CanvasConnection) {
  const from = scheme.nodes.find(n => n.instanceId === c.sourceInstanceId);
  const to = scheme.nodes.find(n => n.instanceId === c.targetInstanceId);
  const host = c.targetHost || to?.serverAddress || '目标公网 IP';
  const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host;
  return { title: `${from?.name || '未知节点'} → ${to?.name || '未知节点'}`,
    kind: c.type === 'port_forward' ? `端口转发 :${c.listenPort ?? from?.listenPort ?? '—'}→${address}:${c.targetPort ?? to?.targetPort ?? '—'}`
      : `加密中转 ${c.tunnelProtocol === 'wireguard' ? 'WireGuard' : (c.tunnelProtocol || 'tls').toUpperCase()} :${c.tunnelPort ?? defaultPort(c.tunnelProtocol || 'tls')}`,
    network: [c.supportTcp ? 'TCP' : '', c.supportUdp ? 'UDP' : ''].filter(Boolean).join('/') || '未选择传输' };
}
export function planLines(plan: TunnelPlan | null, nodeIds?: string[]): string[] {
  const kinds: Record<string, string> = { inbound: '入站', outbound: '出站', chain: '代理链', forward: '端口转发' };
  return (plan?.servers || []).filter(s => !nodeIds || nodeIds.includes(s.nodeId)).flatMap(s =>
    (['creates', 'updates', 'removes'] as const).flatMap(action => (s[action] || []).map(o =>
      `${s.name || s.nodeId}：${({ creates: '新增', updates: '更新', removes: '移除' })[action]}${kinds[o.kind] || o.kind} ${o.name}${o.protocol ? ` ${o.protocol}` : ''}${o.port ? ` :${o.port}` : ''}${o.target ? ` → ${o.target}` : ''}`)));
}
