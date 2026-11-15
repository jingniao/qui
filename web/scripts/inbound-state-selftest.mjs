import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');
const list = read('modules/proxy/pages/ProxyNodeListPage.vue');

// The node list labels each inbound from its own publish state, not from the
// server's latest revision.
assert.match(list, /deploymentAPI\.inboundStates\(/);
assert.doesNotMatch(list, /serverPublishState|nodeRevisions/);
for (const label of ['已生效', '有未发布修改', '发布中', '未生效']) assert.ok(list.includes(`'${label}'`), label);
assert.match(list, /最近一次发布失败/);
assert.match(read('modules/system/deploymentApi.ts'), /inboundStates: .*\/inbound-states/);

// Hysteria2 and TUIC are QUIC, whatever the TCP transport field defaults to.
assert.doesNotMatch(list, /settings\?\.transport \|\| 'tcp'\}\}/);
const transport = list.match(/function transportLabel[\s\S]*?\n}\n/)[0];
assert.match(transport, /hysteria2/);
assert.match(transport, /tuic/);
assert.match(transport, /udp \+ quic/);

// One result-code label list, used by the node list, the history and the server page.
const shared = read('modules/system/revisionResult.ts');
const { outputText } = ts.transpileModule(shared, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } });
const labels = await import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
assert.equal(labels.revisionResult('apply_failed'), '应用失败');
assert.equal(labels.revisionResult('whatever'), 'whatever');
assert.equal(labels.revisionResult(''), '—');
for (const path of ['modules/proxy/pages/ProxyNodeListPage.vue', 'modules/system/pages/RevisionPage.vue', 'modules/nodes/components/detail/serverDetailModel.ts']) {
  assert.match(read(path), /from '(\.\.\/)+(system\/)?revisionResult'/, path);
  assert.doesNotMatch(read(path), /已应用且健康/, path);
}

const stateBlock = list.slice(list.indexOf('const inboundStateMap ='), list.indexOf('watch([pagedNodes'));
const compiledState = ts.transpileModule(stateBlock, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const item = { sourceType: 'self_hosted', sourceId: 's', inboundId: 'i', status: 'online' };
let fail = false;
const stateAPI = { inboundStates: async () => { if (fail) throw new Error('read failed'); return { items: [{ inboundId: 'i', state: 'unpublished' }] }; } };
const state = new Function('ref', 'document', 'displayMode', 'pagedNodes', 'filteredNodes', 'deploymentAPI', 'revisionResult', 'showToast', compiledState + '\nreturn { loadServerPublishStates, inboundState, inboundBadge, notRunning, inboundStatusUnknown };')(
  value => ({ value }), { hidden: false }, { value: 'list' }, { value: [item] }, { value: [item] }, stateAPI, value => value, () => {});
assert.equal(state.inboundBadge(item), undefined, 'not-yet-loaded inbound has no unknown badge');
assert.equal(state.notRunning(item), false, 'not-yet-loaded inbound retains server status');
await state.loadServerPublishStates();
assert.equal(state.inboundState(item).state, 'unpublished');
fail = true;
await state.loadServerPublishStates();
assert.equal(state.inboundState(item)?.state, 'unpublished', 'read failure retains old state');
assert.equal(state.inboundBadge(item).label, '状态未知');
assert.equal(state.inboundStatusUnknown(item), true, 'read failure marks status unknown');
fail = false;
await state.loadServerPublishStates();
assert.equal(state.inboundBadge(item).label, '未生效');
assert.match(list, /serverStatus\(srv,\s*now\.value\)/, 'proxy list shares heartbeat expiry with server list');
const refresh = list.match(/async function refreshServersOnNodeStatus\(\)[\s\S]*?\n}/)?.[0] || '';
assert.match(refresh, /nodeAPI\.list/);
assert.match(list, /throttleTrailing\([\s\S]*refreshServersOnNodeStatus/);
console.log('PASS inbound read failure retention, recovery, shared status and server refresh');

// Run the shared status function at the expiry boundary, then the actual mapping.
const statusSource = read('modules/nodes/components/serverListModel.ts').match(/export function serverStatus[\s\S]*?\n}/)[0];
const serverStatus = new Function('unhealthyCores', ts.transpileModule(statusSource.replace('export ', '') + '\nreturn serverStatus;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(() => []);
const clock = Date.parse('2026-11-14T12:00:00Z');
const mapping = list.match(/const status = .*ib.enabled.*;/)[0];
const mapStatus = new Function('srv', 'now', 'ib', 'serverStatus', mapping + '\nreturn status;');
for (const [age, expected] of [[0, 'online'], [44999, 'online'], [45000, 'offline'], [-5001, 'offline']]) {
  const srv = { enabled: true, status: 'online', lastHeartbeat: new Date(clock - age).toISOString() };
  assert.equal(mapStatus(srv, { value: clock }, { enabled: true }, serverStatus), expected);
}
const servers = { value: [{ id: 'old' }] };
const refreshErrors = [];
let refreshFail = false, reads = 0;
const refreshState = new Function('document', 'choices', 'nodeAPI', 'servers', 'showToast', ts.transpileModule('let serverRefreshGeneration = 0;\n' + refresh + '\nreturn refreshServersOnNodeStatus;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(
  { hidden: false }, async fetch => (await fetch(1, 100)).items,
  { list: async () => { reads++; if (refreshFail) throw new Error('servers failed'); return { items: [{ id: 'new' }] }; } }, servers, error => refreshErrors.push(error));
await refreshState();
assert.equal(reads, 1);
assert.deepEqual(servers.value, [{ id: 'new' }]);
refreshFail = true;
await refreshState();
assert.deepEqual(servers.value, [{ id: 'new' }]);
assert.ok(refreshErrors[0].includes('servers failed'));
console.log('PASS heartbeat boundary and live server refresh/failure behavior');

// Review P2: loadData has an old server result but is still waiting on inbounds.
const loadData = list.match(/async function loadData\(\)[\s\S]*?\n}/)[0];
const pendingInbounds = [];
let snapshot = [{ id: 's', status: 'online' }];
const interleavedServers = { value: [] };
const interleaved = new Function('loading', 'proxyNodeStore', 'choices', 'nodeAPI', 'proxyAPI', 'servers', 'serverInbounds', 'loadNodeRelays', 'canvasAPI', 'isOwner', 'loadCanvasSchemes', 'showToast', 'document', ts.transpileModule('let serverRefreshGeneration = 0;\n' + loadData + '\n' + refresh + '\nreturn { loadData, refreshServersOnNodeStatus };', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(
  { value: false }, {}, async fetch => (await fetch()).items,
  { list: async () => ({ items: snapshot }) }, { inbounds: () => new Promise(resolve => pendingInbounds.push(resolve)) },
  interleavedServers, { value: [] }, () => {}, { list: async () => ({ items: [] }) }, { value: false }, async () => {}, error => { throw new Error(error); }, { hidden: false });
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
const initialLoad = interleaved.loadData();
await settle();
snapshot = [{ id: 's', status: 'offline' }];
await interleaved.refreshServersOnNodeStatus();
assert.equal(interleavedServers.value[0].status, 'offline');
pendingInbounds[0]({ items: [] });
await initialLoad;
assert.equal(interleavedServers.value[0].status, 'offline', 'late loadData must not overwrite event server snapshot');
console.log('PASS loadData/event server snapshot interleaving');
