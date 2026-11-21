import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(path) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const m = await load('../src/modules/proxy/components/relay/tunnelEditorModel.ts');
const plan = { ok: true, servers: [{ nodeId: 'a', name: 'HK-01', creates: [{ kind: 'inbound', name: '[方案]', protocol: 'trojan', port: 8443 }],
  updates: [{ kind: 'outbound', name: '中转', target: '目标服务器' }], removes: [{ kind: 'chain', name: '旧链' }] }],
  connections: [{ id: 'old', status: 'ok', message: '' }], conflicts: [], warnings: [] };
const bad = { ...plan, ok: false, connections: [{ id: 'old', status: 'error', message: '路径成环' }], conflicts: [{ nodeId: 'a', port: 8443, with: '现有入站', connectionId: 'old' }] };
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

// Exercise the real serialization functions without their HTTP/store imports.
const node = (id, resourceId) => ({ instanceId: id, resourceId, resourceType: 'server', name: id, protocol: 'server', serverAddress: '', status: 'online', isManageable: true, x: 0, y: 0, role: 'relay' });
const base = { id: 'scheme', name: '方案', executionLocation: 'server', nodes: [node('ia', 'a'), node('ib', 'b')], connections: [], published: false, updatedAt: 'now' };
const apiSource = readFileSync(new URL('../src/modules/proxy/api.ts', import.meta.url), 'utf8');
const inputFunction = apiSource.slice(apiSource.indexOf('export function relaySchemeToCanvasInput'), apiSource.indexOf('// The status a canvas box'));
const displayFunction = apiSource.slice(apiSource.indexOf('export function canvasSchemeToRelayScheme'));
const serializerCode = ts.transpileModule(`const canvasInstanceRef = n => 'server:' + n.resourceId;
const canvasReferenceDisplay = ref => ({ resourceId: ref.split(':')[1], resourceType: 'server' });
${inputFunction}\n${displayFunction}`, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(serializerCode).toString('base64')}`);

// tunnelId survives a save and a hydrated GET.
const reference = { id: 'edge', tunnelId: 'T1', sourceInstanceId: 'ia', targetInstanceId: 'ib', type: 'tunnel',
  tunnelProtocol: 'tls', tunnelPort: 8443, supportTcp: true, supportUdp: true, status: 'valid' };
const referenceWire = api.relaySchemeToCanvasInput({ ...base, connections: [reference] }, {});
assert.equal(referenceWire.layout.connections[0].tunnelId, 'T1');
assert.equal(referenceWire.layout.connections[0].source, 'ia');
assert.equal(referenceWire.layout.connections[0].target, 'ib');
assert.equal(referenceWire.layout.connections[0].tunnelPort, 8443);
const hydrated = api.canvasSchemeToRelayScheme({ id: 'saved', name: '方案', execution: 'server', updatedAt: 'today', publishedAt: '', layout: referenceWire.layout }, {});
const hydratedEdge = hydrated.connections[0];
assert.equal(hydratedEdge.tunnelId, 'T1');
assert.equal(hydratedEdge.type, 'tunnel');
assert.equal(hydratedEdge.tunnelPort, 8443);
assert.equal(api.relaySchemeToCanvasInput(hydrated, {}).layout.connections[0].tunnelId, 'T1', 'a hydration round-trip keeps the reference');

console.log('PASS tunnel-editor self-test: plan lines, conflict texts, shared-tunnel plan lines and API round-trip');

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
