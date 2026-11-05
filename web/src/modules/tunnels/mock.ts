// Imported dynamically only behind import.meta.env.DEV in api.ts.
import { APIError } from '../../app/apiClient';
import type { Tunnel, TunnelInput, TunnelRule, TunnelRuleInput, PortCheck, PublishSelection, PublishPlan, PublishResult, NodeRelayInput } from './api';
import { endpoint, routeLabel } from './model';

const nodes = [
  { id: 'hk', name: 'HK', publicIp: '192.0.2.10' },
  { id: 'jp', name: 'JP', publicIp: '192.0.2.20' },
  { id: 'us', name: 'US', publicIp: '203.0.113.30' },
];
const inbounds = nodes.flatMap(node => [
  { id: `${node.id}-vless`, nodeId: node.id, name: `${node.name} VLESS`, protocol: 'vless', port: 443 },
  { id: `${node.id}-socks5`, nodeId: node.id, name: `${node.name} SOCKS5`, protocol: 'socks5', port: 1080 },
]);
const marker = 'QUI_TUNNELS_DEV_FIXTURES';
let sequence = 0;
const now = () => new Date(Date.now() + sequence++).toISOString();
const initialTime = now();
const tunnels: Tunnel[] = [{ id: 'hk-jp', name: '香港到日本', transport: 'tls',
  hops: [{ role: 'entry', nodeId: 'hk', nodeName: 'HK', listenPort: null, portMode: 'auto', connectHost: '' }, { role: 'exit', nodeId: 'jp', nodeName: 'JP', listenPort: 30000, portMode: 'auto', connectHost: '' }],
  ruleCount: 1, status: 'published', credentials: null, createdAt: initialTime, updatedAt: initialTime, publishedAt: initialTime }];
const rules: TunnelRule[] = [{ id: 'us-relay', name: '美国 VLESS 中转', entryNodeId: 'hk', entryNodeName: 'HK', listenPort: 30001, portMode: 'auto', network: 'both', tunnelId: 'hk-jp', tunnelName: '香港到日本',
  target: { mode: 'linked', nodeId: 'us', nodeName: 'US', inboundId: 'us-vless', inboundName: 'US VLESS', protocol: 'vless', host: '203.0.113.30', port: 443 },
  ownerNodeId: 'us', subscriptionVisible: true, enabled: true, status: 'modified', createdAt: initialTime, updatedAt: initialTime, publishedAt: initialTime },
{ id: 'direct-rule', name: '日本 SOCKS5 直转', entryNodeId: 'hk', entryNodeName: 'HK', listenPort: 30002, portMode: 'manual', network: 'tcp', tunnelId: null, tunnelName: null,
  target: { mode: 'manual', host: '192.0.2.20', port: 1080 }, ownerNodeId: null, subscriptionVisible: false, enabled: true, status: 'draft', createdAt: initialTime, updatedAt: initialTime, publishedAt: null }];
const retiredClaims: { nodeId: string; port: number; usedBy: NonNullable<PortCheck['usedBy']> }[] = [];
function fail(code: string, message: string, details = {}, status = 409): never { throw new APIError(status, code, message, details); }
function node(id: string) { return nodes.find(n => n.id === id) || fail('not_found', '服务器不存在', {}, 404); }
export function mockChoices() { return structuredClone({ nodes, inbounds }); }
function claims() {
  return [
    ...inbounds.map(i => ({ nodeId: i.nodeId, port: i.port, usedBy: { kind: 'inbound' as const, id: i.id, name: i.name } })),
    ...tunnels.flatMap(t => t.hops.filter(h => h.listenPort !== null).map(h => ({ nodeId: h.nodeId, port: h.listenPort!, usedBy: { kind: 'tunnel' as const, id: t.id, name: t.name } }))),
    ...rules.map(r => ({ nodeId: r.entryNodeId, port: r.listenPort, usedBy: { kind: 'rule' as const, id: r.id, name: r.name } })), ...retiredClaims,
  ];
}
function check(nodeId: string, port: number): PortCheck { node(nodeId); const used = claims().find(c => c.nodeId === nodeId && c.port === port); return { nodeId, port, free: !used, ...(used ? { usedBy: used.usedBy } : {}) }; }
function allocate(nodeId: string, count = 1) {
  node(nodeId); const ports: number[] = [];
  for (let port = 30000; port <= 30100 && ports.length < count; port++) if (check(nodeId, port).free) ports.push(port);
  if (ports.length !== count) fail('no_free_port', '端口范围已用尽');
  return { ports };
}
function savedPort(nodeId: string, input: { listenPort?: number; portMode: string }, ownerId: string, kind: 'tunnel' | 'rule') {
  const port = input.listenPort ?? (input.portMode === 'auto' ? allocate(nodeId).ports[0]! : 0);
  if (!Number.isInteger(port) || port < 1 || port > 65535) fail('port_out_of_range', '端口超出范围', {}, 400);
  const result = check(nodeId, port);
  if (!result.free && !(result.usedBy?.id === ownerId && result.usedBy.kind === kind)) fail('port_in_use', '端口已占用', { usedBy: result.usedBy });
  return port;
}
function stale(old: { updatedAt: string } | undefined, updatedAt?: string) { if (old && old.updatedAt !== updatedAt) fail('stale', '数据已更新，请刷新'); }
function recount() { for (const t of tunnels) t.ruleCount = rules.filter(r => r.tunnelId === t.id).length; }
function saveTunnel(input: TunnelInput, id?: string): Tunnel {
  const old = id ? tunnels.find(t => t.id === id) || fail('not_found', '隧道不存在', {}, 404) : undefined;
  stale(old, input.updatedAt);
  if (old?.status === 'pending_delete') fail('pending_delete', '待删除对象只读');
  if (!input.name.trim() || input.transport !== 'tls' || ![2, 3].includes(input.hops.length) || input.hops[0]?.role !== 'entry' || input.hops.at(-1)?.role !== 'exit' || (input.hops.length === 3 && input.hops[1]?.role !== 'mid') || new Set(input.hops.map(h => h.nodeId)).size !== input.hops.length) fail('invalid', '隧道路径无效', {}, 400);
  const tunnelId = id || `tunnel-${sequence++}`;
  const hops = input.hops.map(h => ({ ...h, nodeName: node(h.nodeId).name, connectHost: h.connectHost || '', listenPort: h.role === 'entry' ? null : savedPort(h.nodeId, h, tunnelId, 'tunnel') }));
  const time = now();
  const value: Tunnel = { id: tunnelId, name: input.name, transport: 'tls', hops, ruleCount: old?.ruleCount || 0, status: old?.publishedAt ? 'modified' : 'draft', credentials: old?.credentials || null, createdAt: old?.createdAt || time, updatedAt: time, publishedAt: old?.publishedAt || null };
  if (old) {
    if (old.publishedAt) retainOldClaims('tunnel', old.id); tunnels.splice(tunnels.indexOf(old), 1, value);
    for (const r of rules.filter(r => r.tunnelId === old.id && r.status !== 'pending_delete')) { r.tunnelName = value.name; r.entryNodeId = hops[0]!.nodeId; r.entryNodeName = hops[0]!.nodeName; r.status = r.publishedAt ? 'modified' : 'draft'; r.updatedAt = now(); refreshTarget(r); }
  } else tunnels.push(value);
  return value;
}
function target(input: TunnelRuleInput): TunnelRule['target'] {
  if (input.target.mode === 'manual') return input.target;
  const inbound = inbounds.find(i => i.id === (input.target as { inboundId: string }).inboundId) || fail('not_found', '目标入站不存在', {}, 404);
  const destination = node(inbound.nodeId), exit = tunnels.find(t => t.id === input.tunnelId)?.hops.at(-1)?.nodeId;
  return { mode: 'linked', nodeId: destination.id, nodeName: destination.name, inboundId: inbound.id, inboundName: inbound.name, protocol: inbound.protocol, host: exit === destination.id ? '127.0.0.1' : destination.publicIp, port: inbound.port };
}
function refreshTarget(rule: TunnelRule) { if (rule.target.mode === 'linked') rule.target = target({ ...rule, target: { mode: 'linked', inboundId: rule.target.inboundId } }); }
function saveRule(input: TunnelRuleInput, id?: string): TunnelRule {
  const old = id ? rules.find(r => r.id === id) || fail('not_found', '规则不存在', {}, 404) : undefined;
  stale(old, input.updatedAt);
  if (old?.status === 'pending_delete') fail('pending_delete', '待删除对象只读');
  const tunnel = input.tunnelId ? tunnels.find(t => t.id === input.tunnelId) || fail('not_found', '隧道不存在', {}, 404) : undefined;
  if (tunnel?.status === 'pending_delete') fail('pending_delete', '隧道待删除，不能使用');
  if (!input.name.trim() || (tunnel && tunnel.hops[0]?.nodeId !== input.entryNodeId)) fail('invalid', '入口须与隧道一致，并填写名称', {}, 400);
  const ruleId = id || `rule-${sequence++}`, time = now();
  const value: TunnelRule = { ...input, id: ruleId, listenPort: savedPort(input.entryNodeId, input, ruleId, 'rule'), entryNodeName: node(input.entryNodeId).name, tunnelName: tunnel?.name || null,
    target: target(input), subscriptionVisible: input.target.mode === 'linked' && input.subscriptionVisible, ownerNodeId: old?.ownerNodeId || null,
    status: old?.publishedAt ? 'modified' : 'draft', createdAt: old?.createdAt || time, updatedAt: time, publishedAt: old?.publishedAt || null };
  if (old) { if (old.publishedAt) retainOldClaims('rule', old.id); rules.splice(rules.indexOf(old), 1, value); } else rules.push(value);
  recount(); return value;
}
function retainOldClaims(kind: 'tunnel' | 'rule', id: string) { for (const claim of claims().filter(c => c.usedBy.kind === kind && c.usedBy.id === id)) if (!retiredClaims.some(c => c.nodeId === claim.nodeId && c.port === claim.port && c.usedBy.id === id)) retiredClaims.push(claim); }
function selected(selection: PublishSelection) {
  const ts = tunnels.filter(t => selection.tunnelIds.includes(t.id) || rules.some(r => selection.ruleIds.includes(r.id) && r.tunnelId === t.id));
  const rs = rules.filter(r => selection.ruleIds.includes(r.id) || selection.tunnelIds.includes(r.tunnelId || ''));
  return { ts, rs };
}
function plan(selection: PublishSelection): PublishPlan {
  const { ts, rs } = selected(selection);
  const map = new Map<string, PublishPlan['servers'][number]>();
  const server = (id: string, order: number) => { if (!map.has(id)) map.set(id, { nodeId: id, nodeName: node(id).name, order, listeners: [], routes: [], changes: [] }); const value = map.get(id)!; value.order = Math.min(value.order, order); return value; };
  for (const t of ts) for (const [index, hop] of t.hops.entries()) {
    const s = server(hop.nodeId, t.hops.length - index), removed = t.status === 'pending_delete';
    if (hop.listenPort !== null && !removed) s.listeners.push({ port: hop.listenPort, network: 'both', purpose: `隧道 ${t.name}` });
    s.routes.push(routeLabel(t));
    if (t.status !== 'published' || removed) s.changes.push({ action: removed ? 'remove' : t.publishedAt ? 'update' : 'create', kind: '隧道', name: t.name });
  }
  for (const r of rs) {
    const s = server(r.entryNodeId, 4), removed = r.status === 'pending_delete';
    if (!removed && r.enabled) s.listeners.push({ port: r.listenPort, network: r.network, purpose: `规则 ${r.name}` });
    s.routes.push(`${r.entryNodeName}:${r.listenPort} → ${r.tunnelName || '直接转发'} → ${endpoint(r.target.host, r.target.port)}`);
    if (r.status !== 'published' || removed) s.changes.push({ action: removed ? 'remove' : r.publishedAt ? 'update' : 'create', kind: '规则', name: r.name });
  }
  const conflicts: PublishPlan['conflicts'] = [];
  for (const s of map.values()) for (const listener of s.listeners) {
    const owners = claims().filter(c => c.nodeId === s.nodeId && c.port === listener.port);
    if (new Set(owners.map(c => `${c.usedBy.kind}:${c.usedBy.id}`)).size > 1) conflicts.push({ nodeId: s.nodeId, port: listener.port, usedBy: owners.map(c => c.usedBy.name).join('、') });
  }
  return { ok: !conflicts.length, servers: [...map.values()].sort((a, b) => a.order - b.order), conflicts, warnings: ['演示发布不连接真实 Agent。'] };
}
function shareLink(rule: TunnelRule) { return rule.target.mode === 'linked' ? `${rule.target.protocol}://demo@${endpoint(node(rule.entryNodeId).publicIp, rule.listenPort)}#${encodeURIComponent(rule.name)}` : null; }
function dispatch(path: string, method: string, body: unknown): unknown {
  const url = new URL(path, 'https://mock.invalid'), parts = url.pathname.split('/').filter(Boolean).map(decodeURIComponent);
  if (parts[0] === 'nodes') {
    const nodeId = parts[1]!; node(nodeId);
    if (parts[2] === 'ports' && parts[3] === 'check') return check(nodeId, Number(url.searchParams.get('port')));
    if (parts[2] === 'ports' && parts[3] === 'allocate') return allocate(nodeId, (body as { count?: number }).count);
    if (parts[2] === 'relays') {
      if (method === 'GET') return { items: rules.filter(r => r.target.mode === 'linked' && r.target.nodeId === nodeId).map(rule => ({ rule, shareLink: shareLink(rule) })) };
      const input = body as NodeRelayInput, inbound = inbounds.find(i => i.id === input.inboundId && i.nodeId === nodeId) || fail('not_found', '该节点无此入站', {}, 404);
      const tunnel = tunnels.find(t => t.id === input.tunnelId) || fail('not_found', '隧道不存在', {}, 404);
      const rule = saveRule({ name: input.name || `${node(nodeId).name} 中转`, entryNodeId: tunnel.hops[0]!.nodeId, portMode: input.portMode, listenPort: input.listenPort, network: 'both', tunnelId: tunnel.id, target: { mode: 'linked', inboundId: inbound.id }, subscriptionVisible: input.subscriptionVisible, enabled: true });
      rule.ownerNodeId = nodeId; return { rule, shareLink: shareLink(rule) };
    }
  }
  if (parts[0] === 'tunnels') {
    if (parts[1] === 'plan') return plan(body as PublishSelection);
    if (parts[1] === 'publish') {
      const selection = body as PublishSelection, p = plan(selection), { ts, rs } = selected(selection);
      if (!p.ok) fail('port_in_use', '发布计划有冲突');
      const result: PublishResult = { batchId: `mock-${sequence++}`, servers: p.servers.map(s => ({ nodeId: s.nodeId, nodeName: s.nodeName, revision: s.changes.length ? sequence : null, status: s.changes.length ? 'applied' : 'unchanged' })) };
      for (const value of [...ts, ...rs].filter(v => v.status !== 'pending_delete')) { value.status = 'published'; value.publishedAt = now(); value.updatedAt = now(); }
      for (let i = retiredClaims.length - 1; i >= 0; i--) if ([...ts, ...rs].some(v => v.id === retiredClaims[i]!.usedBy.id)) retiredClaims.splice(i, 1);
      for (let i = tunnels.length - 1; i >= 0; i--) if (ts.includes(tunnels[i]!) && tunnels[i]!.status === 'pending_delete') tunnels.splice(i, 1);
      for (let i = rules.length - 1; i >= 0; i--) if (rs.includes(rules[i]!) && rules[i]!.status === 'pending_delete') rules.splice(i, 1);
      recount();
      return result;
    }
    if (!parts[1]) {
      if (method === 'GET') { recount(); return { items: tunnels, total: tunnels.length }; }
      return saveTunnel(body as TunnelInput);
    }
    const tunnel = tunnels.find(t => t.id === parts[1]) || fail('not_found', '隧道不存在', {}, 404);
    if (parts[2] === 'rotate-credentials') { if (tunnel.status === 'pending_delete') fail('pending_delete', '待删除对象只读'); stale(tunnel, (body as { updatedAt: string }).updatedAt); tunnel.credentials = { generation: (tunnel.credentials?.generation || 0) + 1, serverName: 'mock.tunnel', notAfter: '2027-11-03T00:00:00Z', fingerprint: marker }; tunnel.updatedAt = now(); tunnel.status = tunnel.publishedAt ? 'modified' : 'draft'; return tunnel; }
    if (method === 'PUT') return saveTunnel(body as TunnelInput, tunnel.id);
    if (method === 'DELETE') { const linked = rules.filter(r => r.tunnelId === tunnel.id).map(r => ({ id: r.id, name: r.name })); if (linked.length) fail('tunnel_in_use', '隧道仍被规则使用', { rules: linked }); if (tunnel.publishedAt) { tunnel.status = 'pending_delete'; tunnel.updatedAt = now(); } else tunnels.splice(tunnels.indexOf(tunnel), 1); return; }
  }
  if (parts[0] === 'tunnel-rules') {
    if (!parts[1]) {
      if (method === 'GET') { const items = rules.filter(r => (!url.searchParams.get('tunnelId') || r.tunnelId === url.searchParams.get('tunnelId')) && (!url.searchParams.get('nodeId') || r.entryNodeId === url.searchParams.get('nodeId'))); return { items, total: items.length }; }
      return saveRule(body as TunnelRuleInput);
    }
    const rule = rules.find(r => r.id === parts[1]) || fail('not_found', '规则不存在', {}, 404);
    if (method === 'PUT') return saveRule(body as TunnelRuleInput, rule.id);
    if (method === 'DELETE') { if (rule.publishedAt) { rule.status = 'pending_delete'; rule.updatedAt = now(); } else rules.splice(rules.indexOf(rule), 1); recount(); return; }
  }
  fail('not_found', '未定义的演示接口', {}, 404);
}
export async function mockRequest(path: string, method: string, body?: unknown): Promise<unknown> { return structuredClone(dispatch(path, method, body)); }
