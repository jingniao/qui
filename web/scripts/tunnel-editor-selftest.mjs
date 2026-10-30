import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(path) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const m = await load('../src/modules/proxy/components/relay/tunnelEditorModel.ts');
const server = (id, name, publicIp) => ({ id, name, publicIp, status: 'online', capabilities: { core_name: 'xray' } });
const servers = [server('a', '源服务器', '192.0.2.1'), server('b', '目标服务器', '2001:db8::1'), server('c', '第三台', '192.0.2.3')];
const node = (id, resourceId, extra = {}) => ({ instanceId: id, resourceId, resourceType: 'server', name: servers.find(s => s.id === resourceId)?.name,
  protocol: 'server', serverAddress: servers.find(s => s.id === resourceId)?.publicIp, status: 'online', isManageable: true, x: 0, y: 0, role: 'relay', ...extra });
const old = { id: 'old', sourceInstanceId: 'ia', targetInstanceId: 'ib', type: 'port_forward', supportTcp: true, supportUdp: false, status: 'valid' };
const base = { id: 'scheme', name: '方案', executionLocation: 'server', nodes: [node('ia', 'a', { listenPort: 10081 }), node('ib', 'b', { targetPort: 8080 })],
  connections: [old, { ...old, id: 'other' }], published: false, updatedAt: 'now' };
const frozen = JSON.stringify(base);
const draft = m.createTunnelDraft(base, old);
assert.equal(draft.listenPort, 10081);
assert.equal(draft.targetPort, 8080);
assert.equal(draft.targetHost, '');
assert.equal(draft.tcp, true);
assert.equal(draft.udp, false);
const result = m.applyTunnel(base, { ...draft, listenPort: 10082, targetHost: ' custom.example ' }, servers);
const saved = result.connections.find(c => c.id === 'old');
assert.equal(saved.listenPort, 10082);
assert.equal(saved.targetHost, 'custom.example');
assert.equal(saved.targetPort, 8080);
assert.equal(result.nodes[0].listenPort, 10081, 'legacy fallback shared by another forward remains intact');
assert.equal(result.nodes[1].targetPort, 8080);
assert.deepEqual(result.connections.find(c => c.id === 'other'), base.connections[1]);
assert.equal(JSON.stringify(base), frozen, 'preview never mutates the live draft');
const explicit = m.createTunnelDraft(base, { ...old, listenPort: 30000, targetHost: 'example.com', targetPort: 8443 });
assert.equal(explicit.listenPort, 30000);
assert.equal(explicit.targetPort, 8443);
assert.equal(explicit.targetHost, 'example.com');
assert.equal(m.defaultPort('tls'), 8443);
assert.equal(m.defaultPort('wireguard'), 51820);
assert.equal(m.createTunnelDraft(base).port, 8443);
assert.equal(m.createTunnelDraft(base, { ...old, type: 'tunnel', tunnelProtocol: 'wireguard' }).port, 51820);
assert.equal(m.validateTunnel(base, draft, servers), '');
for (const port of [0, 65536, 2.5, NaN, '']) assert.match(m.validateTunnel(base, { ...draft, listenPort: port }, servers), /端口/);
assert.match(m.validateTunnel(base, { ...draft, tcp: false, udp: false }, servers), /TCP/);
assert.match(m.validateTunnel(base, { ...draft, target: 'ia' }, servers), /相同/);
assert.match(m.validateTunnel(base, { ...draft, target: '' }, servers), /请选择/);
assert.match(m.validateTunnel(base, { ...draft, kind: 'encrypted', protocol: 'gost' }, servers), /GOST/);
const outside = { ...base, nodes: [base.nodes[0], node('ib', 'b', { isManageable: false, resourceType: 'node' })] };
assert.match(m.validateTunnel(outside, { ...draft, kind: 'encrypted' }, servers), /可管理/);
assert.equal(m.validateTunnel({ ...outside, executionLocation: 'client' }, { ...draft, kind: 'encrypted' }, servers), '');
const encrypted = m.applyTunnel(base, { ...draft, kind: 'encrypted' }, servers).connections.find(c => c.id === old.id);
assert.equal(encrypted.type, 'tunnel');
assert.equal(encrypted.listenPort, undefined);
assert.equal(encrypted.targetHost, undefined);
assert.equal(encrypted.targetPort, undefined);
const chain = { ...old, type: 'proxy_chain' };
const chainScheme = { ...base, connections: [chain] };
assert.equal(m.createTunnelDraft(chainScheme, chain).kind, 'encrypted');
assert.equal(m.applyTunnel(chainScheme, m.createTunnelDraft(chainScheme, chain), servers).connections[0].type, 'proxy_chain');
const added = m.applyTunnel(base, { ...draft, id: 'new', source: 'server:c' }, servers);
assert.equal(added.nodes.length, 3);
assert.equal(added.connections.length, 3);
assert.equal(added.nodes[2].resourceId, 'c');
assert.equal(added.connections[2].sourceInstanceId, added.nodes[2].instanceId);
assert.ok(added.nodes[2].x > 0 && added.nodes[2].y > 0);
assert.equal(m.tunnelChoices(base, servers).length, 3);
assert.equal(m.tunnelChoices(base, servers).find(c => c.key === 'ia').core, 'xray');
const row = m.tunnelSummary(base, old);
assert.equal(row.title, '源服务器 → 目标服务器');
assert.equal(row.kind, '端口转发 :10081→[2001:db8::1]:8080');
assert.equal(row.network, 'TCP');
assert.match(m.tunnelSummary(chainScheme, chain).kind, /加密中转 TLS :8443/);
const plan = { ok: true, servers: [{ nodeId: 'a', name: 'HK-01', creates: [{ kind: 'inbound', name: '[方案]', protocol: 'trojan', port: 8443 }],
  updates: [{ kind: 'outbound', name: '中转', target: '目标服务器' }], removes: [{ kind: 'chain', name: '旧链' }] }],
  connections: [{ id: 'old', status: 'ok', message: '' }], conflicts: [], warnings: [] };
assert.deepEqual(m.tunnelStatus(plan, 'old'), { error: false, text: '可发布' });
assert.equal(m.tunnelStatus(plan, 'old', true), null, 'client plans must not assign row statuses');
assert.equal(m.tunnelStatus(null, 'old').text, '待检查');
const bad = { ...plan, ok: false, connections: [{ id: 'old', status: 'error', message: '路径成环' }], conflicts: [{ nodeId: 'a', port: 8443, with: '现有入站', connectionId: 'old' }] };
assert.equal(m.tunnelStatus(bad, 'old').error, true);
assert.match(m.tunnelStatus(bad, 'old').text, /路径成环.*端口冲突 :8443.*现有入站/);
assert.equal(m.tunnelStatus({ ...bad, message: '全局失败' }, 'missing').text, '全局失败');
assert.deepEqual(m.planLines(plan), ['HK-01：新增入站 [方案] trojan :8443', 'HK-01：更新出站 中转 → 目标服务器', 'HK-01：移除代理链 旧链']);
assert.deepEqual(m.planLines(plan, ['b']), []);
assert.deepEqual(m.planLines({ ok: false, servers: null, conflicts: null, warnings: null, connections: null }), []);

// K4: conflicts carry a reason, a shared tunnel lists affected schemes and a tunnel
// resource is edited through the same draft without depending on one drawing.
assert.equal(m.tunnelConflictText({ nodeId: 'a', port: 8443, with: '旧入口', connectionId: 'old', reason: 'legacy_object', objectId: 'OBJ-1' }), '历史对象未认领：旧入口（对象 OBJ-1）');
assert.equal(m.tunnelConflictText({ nodeId: '', port: 0, with: '旧出站', connectionId: '', reason: 'object_name' }), '对象名称冲突：旧出站');
assert.equal(m.tunnelConflictText({ nodeId: 'a', port: 8443, with: '现有入站', connectionId: 'old' }), '端口冲突 :8443（现有入站）');
assert.equal(m.tunnelConflictText({ nodeId: 'a', port: 0, with: '线索', connectionId: '', reason: 'tunnel_object' }), '共享隧道对象冲突：线索');
assert.deepEqual(m.tunnelPlanLines(
  { ...plan, tunnels: [{ tunnelId: 't1', affectedSchemes: [{ id: 's1', name: '方案一' }, { id: 's2', name: '方案二' }] }] },
  id => (id === 't1' ? 'A to B' : '')
), ['共享隧道 A to B：影响 2 个方案（方案一、方案二）']);
assert.deepEqual(m.tunnelPlanLines(plan, () => ''), []);
const resource = {
  id: 't1', name: 'A to B TLS', type: 'tunnel', source: 'server:a', target: 'server:b',
  tunnelProtocol: 'wireguard', tunnelPort: 51820, supportTcp: true, supportUdp: true,
  createdAt: 'then', updatedAt: 'now', schemes: [], objects: [], publishStatus: 'published'
};
const resourceDraft = m.createTunnelResourceDraft(resource);
assert.equal(resourceDraft.source, 'server:a');
assert.equal(resourceDraft.target, 'server:b');
assert.equal(resourceDraft.protocol, 'wireguard');
assert.equal(resourceDraft.port, 51820);
assert.equal(resourceDraft.kind, 'encrypted');
assert.equal(m.createTunnelResourceDraft({ ...resource, type: 'port_forward', listenPort: 10080, targetHost: 'example.com', targetPort: 443 }).kind, 'forward');
assert.equal(m.validateTunnelResource(resourceDraft, servers), '');
assert.match(m.validateTunnelResource({ ...resourceDraft, target: 'server:gone' }, servers), /不存在/);
assert.match(m.validateTunnelResource({ ...resourceDraft, source: 'server:a', target: 'server:a' }, servers), /相同/);
assert.match(m.validateTunnelResource({ ...resourceDraft, tcp: false, udp: false }, servers), /TCP/);
assert.match(m.validateTunnelResource({ ...resourceDraft, protocol: 'gost' }, servers), /GOST/);
assert.match(m.validateTunnelResource({ ...resourceDraft, port: 70000 }, servers), /端口/);
assert.deepEqual(
  m.tunnelResourceInput({ ...resourceDraft, kind: 'forward', listenPort: 10081, targetHost: ' a.example ', targetPort: 443 }, ' A to B forward ', 'tunnel'),
  { name: 'A to B forward', source: 'server:a', target: 'server:b', type: 'port_forward', supportTcp: true, supportUdp: true, listenPort: 10081, targetHost: 'a.example', targetPort: 443 }
);
const tlsInput = m.tunnelResourceInput(resourceDraft, ' A to B TLS ', 'proxy_chain');
assert.deepEqual(tlsInput, { name: 'A to B TLS', source: 'server:a', target: 'server:b', type: 'proxy_chain', supportTcp: true, supportUdp: true, tunnelProtocol: 'wireguard', tunnelPort: 51820 });

// Exercise the real serialization functions without their HTTP/store imports.
const apiSource = readFileSync(new URL('../src/modules/proxy/api.ts', import.meta.url), 'utf8');
const inputFunction = apiSource.slice(apiSource.indexOf('export function relaySchemeToCanvasInput'), apiSource.indexOf('// The status a canvas box'));
const displayFunction = apiSource.slice(apiSource.indexOf('export function canvasSchemeToRelayScheme'));
const serializerCode = ts.transpileModule(`const canvasInstanceRef = n => 'server:' + n.resourceId;
const canvasReferenceDisplay = ref => ({ resourceId: ref.split(':')[1], resourceType: 'server' });
${inputFunction}\n${displayFunction}`, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(serializerCode).toString('base64')}`);
const wire = api.relaySchemeToCanvasInput(result, {});
assert.equal(wire.layout.connections.find(c => c.id === 'old').listenPort, 10082);
const reloaded = api.canvasSchemeToRelayScheme({ id: 'saved', name: '方案', execution: 'server', layout: wire.layout, updatedAt: 'today', publishedAt: '' }, {});
assert.equal(reloaded.connections.find(c => c.id === 'old').targetHost, 'custom.example');
assert.equal(reloaded.connections.find(c => c.id === 'old').targetPort, 8080);

// K4 references: tunnelId survives a save and a hydrated GET, and the client-format
// fields the panel shows come back from the resource, not from the drawing.
const reference = { id: 'edge', tunnelId: 'T1', sourceInstanceId: 'ia', targetInstanceId: 'ib', type: 'tunnel',
  tunnelProtocol: 'tls', tunnelPort: 8443, supportTcp: true, supportUdp: true, status: 'valid' };
const referenceWire = api.relaySchemeToCanvasInput({ ...base, connections: [reference] }, {});
assert.equal(referenceWire.layout.connections[0].tunnelId, 'T1');
assert.equal(referenceWire.layout.connections[0].source, 'ia');
assert.equal(referenceWire.layout.connections[0].target, 'ib');
assert.equal(referenceWire.layout.connections[0].tunnelPort, 8443);
const hydrated = api.canvasSchemeToRelayScheme({
  id: 'saved', name: '方案', execution: 'server', updatedAt: 'today', publishedAt: '',
  layout: { instances: [], connections: [{ id: 'edge', source: 'a', target: 'b', tunnelId: 'T1', type: 'port_forward',
    listenPort: 10080, targetHost: 'example.com', targetPort: 443, supportTcp: true, supportUdp: false }] }
}, {});
const hydratedEdge = hydrated.connections[0];
assert.equal(hydratedEdge.tunnelId, 'T1');
assert.equal(hydratedEdge.type, 'port_forward');
assert.equal(hydratedEdge.listenPort, 10080);
assert.equal(hydratedEdge.targetHost, 'example.com');
assert.equal(hydratedEdge.targetPort, 443);
assert.equal(hydratedEdge.supportUdp, false);
assert.equal(api.relaySchemeToCanvasInput(hydrated, {}).layout.connections[0].tunnelId, 'T1', 'a hydration round-trip keeps the reference');

// Library rows and reference builders are pure, so the page only resolves names.
assert.equal(api.canvasTunnelNodeId('server:NODE_A'), 'NODE_A');
assert.equal(api.canvasTunnelNodeId('inbound:x'), 'x');
assert.equal(api.canvasTunnelRef('NODE_A'), 'server:NODE_A');
assert.equal(api.canvasTunnelKindLabel('port_forward'), '端口转发');
assert.equal(api.canvasTunnelKindLabel('proxy_chain'), '加密中转');
assert.equal(api.canvasTunnelNetworkLabel(true, false), 'TCP');
assert.equal(api.canvasTunnelNetworkLabel(false, true), 'UDP');
assert.equal(api.canvasTunnelNetworkLabel(false, false), '—');
const libraryTunnel = { ...resource, type: 'tunnel', tunnelProtocol: 'tls', tunnelPort: 8443,
  schemes: [{ id: 's1', name: '方案一' }], objects: [{ kind: 'inbound', id: 'o1', nodeId: 'b', name: 'n', port: 8443 }], publishStatus: 'modified' };
const libraryItem = api.canvasTunnelLibraryItem(libraryTunnel, id => (id === 'a' ? '源服务器' : '目标服务器'));
assert.equal(libraryItem.title, '源服务器 → 目标服务器');
assert.equal(libraryItem.subtitle, '加密隧道 TLS :8443 · TCP / UDP');
assert.deepEqual(libraryItem.schemeNames, ['方案一']);
assert.equal(libraryItem.objectCount, 1);
assert.equal(libraryItem.publishStatus, 'modified');
assert.equal(api.canvasTunnelLibraryItem({ ...libraryTunnel, source: 'server:gone' }, id => (id === 'b' ? '目标服务器' : '')).title, '已删除的服务器 → 目标服务器');
assert.equal(api.canvasTunnelPublishStatusLabel('published'), '已发布');
assert.equal(api.canvasTunnelPublishStatusLabel('draft'), '草稿');
assert.equal(api.canvasTunnelAffectedText([{ id: 's1', name: '方案一' }, { id: 's2', name: '方案二' }]), '将影响 2 个方案：方案一、方案二');
assert.equal(api.canvasTunnelAffectedText([]), '当前没有方案引用此隧道，保存只修改资源本身');
const referenceBody = api.canvasTunnelReference(libraryTunnel, 'edge', 'ia', 'ib');
assert.deepEqual(referenceBody, { id: 'edge', tunnelId: 't1', source: 'ia', target: 'ib', type: 'tunnel',
  tunnelProtocol: 'tls', tunnelPort: 8443, supportTcp: true, supportUdp: true });
const forwardBody = api.canvasTunnelReference({ ...libraryTunnel, type: 'port_forward', listenPort: 10080, targetHost: 'example.com', targetPort: 443, tunnelProtocol: undefined, tunnelPort: undefined }, 'edge', 'ia', 'ib');
assert.equal(forwardBody.type, 'port_forward');
assert.equal(forwardBody.listenPort, 10080);
assert.equal(forwardBody.targetHost, 'example.com');
assert.equal(forwardBody.targetPort, 443);
assert.equal(forwardBody.tunnelProtocol, undefined, 'a forwarding reference sends no tunnel protocol');

console.log('PASS tunnel-editor self-test: defaults, migration, identity, validation, preview, summaries, statuses, K4 shared-tunnel references (tunnelId round-trip/hydration, library rows, resource input) and API round-trip');

// Run the actual request coordinator with deterministic timers and watch/ref stubs.
const coordinatorSource = readFileSync(new URL('../src/modules/proxy/components/relay/useTunnelPlan.ts', import.meta.url), 'utf8');
const coordinatorCode = ts.transpileModule(`
let callback, cleanup, clock = 0, counter = 0;
const timers = new Map();
const ref = value => ({ value });
const watch = (_key, fn) => { callback = fn; fn(); };
const onBeforeUnmount = fn => { cleanup = fn; };
const setTimeout = (fn, delay) => { const id = ++counter; timers.set(id, { fn, at: clock + delay }); return id; };
const clearTimeout = id => timers.delete(id);
export const edit = () => callback();
export const dispose = () => cleanup();
export const tick = ms => { clock += ms; for (const [id, t] of [...timers]) if (t.at <= clock) { timers.delete(id); t.fn(); } };
${coordinatorSource.replace(/^import .*;\r?\n/gm, '')}
`, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const coordinator = await import(`data:text/javascript;base64,${Buffer.from(coordinatorCode).toString('base64')}`);
const pending = [];
const state = coordinator.useTunnelPlan(() => '', () => new Promise((resolve, reject) => pending.push({ resolve, reject })));
coordinator.tick(599);
assert.equal(pending.length, 0);
coordinator.edit();
coordinator.tick(599);
assert.equal(pending.length, 0, 'each edit restarts the 600 ms window');
coordinator.tick(1);
assert.equal(pending.length, 1);
coordinator.edit();
pending[0].resolve(plan);
await Promise.resolve();
assert.equal(state.plan.value, null, 'edited layouts ignore old success responses');
assert.equal(state.checking.value, true);
coordinator.tick(600);
pending[1].reject(new Error('offline'));
await Promise.resolve();
assert.match(state.error.value, /offline/);
assert.equal(state.checking.value, false);
const retry = state.run();
pending[2].resolve(plan);
await retry;
assert.equal(state.plan.value, plan);
assert.equal(state.error.value, '');
coordinator.edit();
const immediate = state.run();
coordinator.tick(600);
assert.equal(pending.length, 4, 'immediate check cancels its pending debounced request');
coordinator.dispose();
pending[3].resolve(bad);
await immediate;
assert.equal(state.plan.value, null, 'unmounted editor ignores in-flight replies');
console.log('PASS TUNNEL_EDITOR_PLAN: 600 ms debounce, immediate check, stale replies, error/retry and unmount');
