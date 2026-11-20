import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { nextTick, ref, watch, shallowReactive } from 'vue';

const read = path => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');
const list = read('modules/proxy/pages/ProxyNodeListPage.vue');
const inventorySource = read('modules/proxy/proxyPageInventory.ts').replace(/^import .*;\r?\n/gm, '').replace('export function', 'function');
const makeInventory = new Function('shallowReactive', 'nodeAPI', 'groupAPI', 'proxyAPI', 'choices', 'applyRuntimeSnapshot',
  ts.transpileModule(inventorySource, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText + '\nreturn proxyPageInventory;')(
  shallowReactive, {}, {}, {}, () => {}, (base, update) => ({ ...base, heartbeat: update.heartbeat }));
const liveInventory = makeInventory(), liveServers = [{ id: 'n1', heartbeat: {} }];
const liveEvent = { generation: 'g1', inventoryRevision: 2, node: { nodeId: 'n1', revision: 1, heartbeat: {} } };
assert.equal(liveInventory.applyEvent(liveServers, liveEvent), true, 'first event calibrates inventory');
for (let revision = 2; revision < 14; revision++) {
  assert.equal(liveInventory.applyEvent(liveServers, { ...liveEvent, node: { ...liveEvent.node, revision } }), false, 'ordinary runtime updates do not reload choices');
}
assert.equal(liveInventory.applyEvent(liveServers, { ...liveEvent, inventoryRevision: 3, node: { ...liveEvent.node, revision: 14 } }), true, 'new Agent capabilities invalidate choices');

// Exercise the mounted heartbeat listener. A normal heartbeat supplies the
// runtime update separately and must not invalidate configuration reads.
const listener = list.match(/window\.addEventListener\('proxy-node-status', (\w+)\)/)[1];
const nodeStatusHandler = list.match(/function onNodeStatus\(event: Event\)[\s\S]*?\n}/)?.[0] || '';
let heartbeatsRead = 0;
const countRead = () => { heartbeatsRead++; };
const handleStatus = new Function('nodeStatusEventChangesInventory', 'onInventoryChanged', ts.transpileModule(nodeStatusHandler, {}).outputText + '\nreturn ' + listener)(
  event => event.detail.inventoryChanged !== false, countRead);
for (let i = 0; i < 12; i++) handleStatus({ detail: { nodeId: 'server', inventoryChanged: false } });
assert.equal(heartbeatsRead, 0, 'ordinary heartbeats must cause zero static/configuration reads');
handleStatus({ detail: { nodeId: 'server' } });
assert.equal(heartbeatsRead, 1, 'configuration inventory change refreshes choices');
const keyCode = ['visibleServerIds', 'visibleServerKey', 'serverRevisionKey'].map(name => list.match(new RegExp('function ' + name + '\\(\\)[\\s\\S]*?\\n}'))[0]).join('\n');
const bindWatch = list.match(/watch\(\[visibleServerKey, serverRevisionKey, serverInbounds\][\s\S]*?\{ immediate: true \}\);/)[0];
const watchServers = ref([{ id: 'server', heartbeat: { current_revision: 7 } }]);
const visibleNodes = ref([{ sourceId: 'server', sourceType: 'self_hosted' }]);
const draftRows = ref([]);
let configurationReads = 0, configurationInvalidations = 0;
new Function('displayMode', 'pagedNodes', 'filteredNodes', 'servers', 'serverInbounds', 'watch', 'loadServerPublishStates', 'publishStateReads',
  ts.transpileModule(keyCode + '\n' + bindWatch, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(
  ref('list'), visibleNodes, visibleNodes, watchServers, draftRows, watch, () => configurationReads++, { invalidate: () => configurationInvalidations++ });
for (let beat = 0; beat < 12; beat++) {
  watchServers.value = [{ id: 'server', heartbeat: { current_revision: 7, cpu_percent: beat } }];
  visibleNodes.value = [...visibleNodes.value];
  await nextTick();
}
assert.equal(configurationReads, 1, 'only the initial status read runs while live metrics change');
watchServers.value[0].heartbeat.current_revision = 8;
await nextTick();
draftRows.value = [{ id: 'draft' }];
await nextTick();
assert.equal(configurationReads, 3, 'confirmed revision and changed drafts each refresh status');
visibleNodes.value = [];
await nextTick();
assert.equal(configurationReads, 4, 'changing the selection updates the visible nodes');
assert.equal(configurationInvalidations, 3, 'selection changes leave the current data generation intact');

// The node list labels each inbound from its own publish state, not from the
// server's latest revision.
assert.match(list, /deploymentAPI\.inboundStatesBatch\(/);
assert.doesNotMatch(list, /serverPublishState|nodeRevisions/);
for (const label of ['已生效', '有未发布修改', '发布中', '未生效']) assert.ok(list.includes(`'${label}'`), label);
assert.match(list, /最近一次发布失败/);
assert.match(read('modules/system/deploymentApi.ts'), /inboundStatesBatch: .*\/nodes\/inbound-states/);

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

const stateBlock = list.slice(list.indexOf('const inboundStateMap ='), list.indexOf('let serverRefreshGeneration'));
const publishEvent = list.match(/function onDeploymentStatus\(\)[\s\S]*?\n}/)[0];
const draftEvent = list.match(/let draftTimer:[\s\S]*?\n}/)[0];
const transpile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const schedulerSource = read('modules/proxy/inboundStateReads.ts').replace(/^import type .*;\r?\n/m, '').replace('export function', 'function');
const createInboundStateReads = new Function(transpile(schedulerSource) + '\nreturn createInboundStateReads;')();
const buildState = new Function('ref', 'document', 'displayMode', 'pagedNodes', 'filteredNodes', 'servers', 'serverInbounds',
  'deploymentAPI', 'revisionResult', 'createInboundStateReads', 'watch', 'setTimeout', 'clearTimeout', 'loadData',
  transpile(stateBlock + '\n' + publishEvent + '\n' + draftEvent) + `
  const refreshDeployment = () => { void loadServerPublishStates(); };
  return { loadServerPublishStates, inboundState, inboundBadge, notRunning, inboundStatusUnknown, onDeploymentStatus, queueDraftRefresh,
    dispose: publishStateReads.dispose };
  `);
const stateHarness = (nodes, api, { serverList, drafts, schedule = () => {} } = {}) => buildState(
  ref, { hidden: false }, ref('list'), nodes, nodes, serverList, drafts, api, value => value, createInboundStateReads,
  serverList ? watch : () => {}, schedule, () => {}, async () => {});

const item = { sourceType: 'self_hosted', sourceId: 's', inboundId: 'i', status: 'online' };
let fail = false;
const stateAPI = { inboundStatesBatch: async () => { if (fail) throw new Error('read failed'); return { items: [{ nodeId: 's', items: [{ inboundId: 'i', state: 'unpublished' }] }] }; } };
const state = stateHarness(ref([item]), stateAPI);
assert.equal(state.inboundBadge(item), undefined, 'not-yet-loaded inbound has no unknown badge');
assert.equal(state.notRunning(item), false, 'not-yet-loaded inbound retains server status');
await state.loadServerPublishStates();
assert.equal(state.inboundState(item).state, 'unpublished');
fail = true;
state.onDeploymentStatus();
await Promise.resolve();
assert.equal(state.inboundState(item)?.state, 'unpublished', 'read failure retains old state');
assert.equal(state.inboundBadge(item).label, '状态未知');
assert.equal(state.inboundStatusUnknown(item), true, 'read failure marks status unknown');
fail = false;
await state.loadServerPublishStates(['s']);
assert.equal(state.inboundBadge(item).label, '未生效');

const selectedNodes = ref(Array.from({ length: 10 }, (_, index) => ({
  sourceType: 'self_hosted', sourceId: 'node-' + index, inboundId: 'inbound',
})));
const deferredBatches = requests => ({ inboundStatesBatch: ids => new Promise((resolve, reject) => requests.push({ ids, resolve, reject })) });
const requests = [];
const scheduleState = stateHarness(selectedNodes, deferredBatches(requests));
const resolveBatch = (request, state = 'effective') => request.resolve({ items: request.ids.map(nodeId => ({ nodeId, items: [{ inboundId: 'inbound', state }] })) });
const originalNodes = selectedNodes.value;
const ten = scheduleState.loadServerPublishStates();
selectedNodes.value = originalNodes.slice(0, 9);
await scheduleState.loadServerPublishStates();
resolveBatch(requests[0]);
await ten;
assert.equal(requests.length, 1, 'shrinking an in-flight 10-node read to nine does not send a second request');
assert.equal(scheduleState.inboundState(originalNodes[8])?.state, 'effective', 'a visible node receives the in-flight result');
const extra = { sourceType: 'self_hosted', sourceId: 'node-10', inboundId: 'inbound' };
selectedNodes.value = [...originalNodes, extra];
const expanded = scheduleState.loadServerPublishStates();
assert.deepEqual(requests[1].ids, ['node-10'], 'expanding the selection requests only a new node');
const other = { ...extra, sourceId: 'node-11' };
selectedNodes.value = [extra, other];
await scheduleState.loadServerPublishStates();
requests[1].resolve({ items: [{ nodeId: extra.sourceId, items: null, error: 'read failed for one node' }] });
await expanded;
assert.equal(scheduleState.inboundBadge(extra).label, '状态未知');
assert.deepEqual(requests[2].ids, ['node-11'], 'an expansion during a request reads only the uncovered node');
resolveBatch(requests[2]);
await Promise.resolve();
assert.equal(requests.length, 3, 'per-node failures wait for an explicit retry');
const retry = scheduleState.loadServerPublishStates([extra.sourceId]);
assert.deepEqual(requests[3].ids, ['node-10'], 'retry leaves the successful node alone');
resolveBatch(requests[3]);
await retry;
assert.equal(scheduleState.inboundBadge(extra).label, '已生效');

for (const [label, invalidate] of [
  ['confirmed revision', (_state, servers) => { servers.value[0].heartbeat.current_revision++; }],
  ['draft snapshot', (_state, _servers, drafts) => { drafts.value = [{ id: 'changed-draft' }]; }],
  ['draft or certificate event', state => { state.queueDraftRefresh(); }],
  ['deployment event', state => { state.onDeploymentStatus(); }],
]) {
  const pending = [];
  const serverList = ref([{ id: extra.sourceId, heartbeat: { current_revision: 7 } }]);
  const drafts = ref([]);
  const timers = [];
  const harness = stateHarness(ref([extra]), deferredBatches(pending), { serverList, drafts, schedule: cb => timers.push(cb) });
  assert.equal(pending.length, 1, label);
  invalidate(harness, serverList, drafts);
  await nextTick();
  resolveBatch(pending[0], 'pending');
  await Promise.resolve();
  assert.equal(harness.inboundBadge(extra), undefined, label + ' drops an obsolete response');
  if (timers.length) { timers[0](); await Promise.resolve(); }
  assert.equal(pending.length, 2, label + ' schedules one fresh response');
  resolveBatch(pending[1]);
  await Promise.resolve();
  assert.equal(harness.inboundBadge(extra).label, '已生效', label);
  harness.dispose();
}

const ending = [];
const unmounted = stateHarness(ref([extra]), deferredBatches(ending));
const first = unmounted.loadServerPublishStates();
unmounted.onDeploymentStatus();
unmounted.dispose();
ending[0].reject(new Error('response after unmount'));
await first;
assert.equal(unmounted.inboundBadge(extra), undefined, 'an obsolete failure cannot be applied after unmount');
assert.equal(ending.length, 1, 'disposing cancels queued reads');
assert.match(list, /onUnmounted\(\(\) => \{[\s\S]*?publishStateReads\.dispose\(\)/);

assert.match(list, /serverStatus\(srv,\s*now\.value\)/, 'proxy list shares heartbeat expiry with server list');
const refresh = list.match(/async function refreshServersOnNodeStatus\(\)[\s\S]*?\n}/)?.[0] || '';
assert.match(refresh, /inventory\.readServers/);
assert.match(list, /throttleTrailing\([\s\S]*refreshServersOnNodeStatus/);
console.log('PASS inbound read failure retention, recovery, shared status and server refresh');

// Run the shared status function at the expiry boundary, then the actual mapping.
const statusSource = read('modules/nodes/components/serverListModel.ts').match(/export function serverStatus[\s\S]*?\n}/)[0];
const serverStatus = new Function('unhealthyCores', ts.transpileModule(statusSource.replace('export ', '') + '\nreturn serverStatus;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(() => []);
const clock = Date.parse('2026-11-14T12:00:00Z');
const mapping = list.match(/const state = serverStatus.*;\s*const status = .*ib.enabled.*;/)[0];
const mapStatus = new Function('srv', 'now', 'ib', 'serverStatus', mapping + '\nreturn status;');
for (const [age, expected] of [[0, 'online'], [44999, 'online'], [45000, 'offline'], [-5001, 'offline']]) {
  const srv = { enabled: true, status: 'online', lastHeartbeat: new Date(clock - age).toISOString() };
  assert.equal(mapStatus(srv, { value: clock }, { enabled: true }, serverStatus), expected);
}
const servers = { value: [{ id: 'old' }] };
const refreshErrors = [];
let refreshFail = false, reads = 0;
const refreshState = new Function('document', 'inventory', 'servers', 'showToast', ts.transpileModule('let serverRefreshGeneration = 0;\n' + refresh + '\nreturn refreshServersOnNodeStatus;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(
  { hidden: false }, { readServers: async () => { reads++; if (refreshFail) throw new Error('servers failed'); return [{ id: 'new' }]; } }, servers, error => refreshErrors.push(error));
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
const interleaved = new Function('loading', 'proxyNodeStore', 'choices', 'inventory', 'servers', 'serverInbounds', 'loadNodeRelays', 'canvasAPI', 'isOwner', 'loadCanvasSchemes', 'showToast', 'document', ts.transpileModule('let serverRefreshGeneration = 0, loadAgain = false, publishStateDisposed = false;\n' + loadData + '\n' + refresh + '\nreturn { loadData, refreshServersOnNodeStatus };', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)(
  { value: false }, {}, async fetch => (await fetch()).items,
  { readServers: async () => snapshot, readInbounds: () => new Promise(resolve => pendingInbounds.push(resolve)) },
  interleavedServers, { value: [] }, () => {}, { list: async () => ({ items: [] }) }, { value: false }, async () => {}, error => { throw new Error(error); }, { hidden: false });
const settle = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
const initialLoad = interleaved.loadData();
await settle();
snapshot = [{ id: 's', status: 'offline' }];
await interleaved.refreshServersOnNodeStatus();
assert.equal(interleavedServers.value[0].status, 'offline');
pendingInbounds[0]([]);
await initialLoad;
assert.equal(interleavedServers.value[0].status, 'offline', 'late loadData must not overwrite event server snapshot');
console.log('PASS loadData/event server snapshot interleaving');

// Execute the actual combined-status helper with its inbound display state.
const statusBlock = list.slice(list.indexOf('const statusLook ='), list.indexOf('function copyAddress'));
const displayState = stateBlock.slice(0, stateBlock.indexOf('let publishStateDisposed'));
const combined = new Function('ref', 'revisionResult', transpile(displayState + '\n' + statusBlock + '\nreturn { nodeStatus, inboundStateMap, inboundStateErrors };'))(value => ({ value }), value => value);
for (const [reach, publish, failed, expected] of [
  ['offline', 'unpublished', true, '状态未知'],
  ['online', 'effective', true, '状态未知'],
  ['offline', 'unpublished', false, '离线'],
  ['offline', 'pending', false, '离线'],
  ['offline', 'modified', false, '离线'],
  ['error', 'effective', false, '离线'],
  ['online', 'unpublished', false, '未生效'],
  ['online', 'pending', false, '发布中'],
  ['online', 'modified', false, '在线·有修改'],
  ['online', 'effective', false, '在线'],
]) {
  combined.inboundStateMap.value.set('s/i', { state: publish });
  combined.inboundStateErrors.value = failed ? new Map([['s', '读取失败']]) : new Map();
  const result = combined.nodeStatus({ ...item, name: '入口', status: reach });
  assert.equal(result.label, expected, `${reach}/${publish}/${failed}`);
  assert.match(result.title, /服务器：/);
  assert.ok(result.capsule && result.dot);
}
for (const sourceType of ['manual', 'subscription']) {
  for (const [status, expected] of [['online', '在线'], ['untested', '未检测'], ['offline', '离线'], ['error', '离线']]) {
    assert.equal(combined.nodeStatus({ ...item, sourceType, status }).label, expected);
  }
}
const card = list.slice(list.indexOf('<!-- ================= Mode 2:'), list.indexOf('<!-- Row 3:'));
const titleRow = card.slice(card.indexOf('<!-- Row 1:'), card.indexOf('<!-- Row 2:'));
assert.doesNotMatch(titleRow, /inboundBadge|nodeStatus/);
assert.match(card, /<EntryRouteSummary inline\s/);
assert.equal((card.match(/nodeStatus\(node\)\.label/g) || []).length, 1);
const table = list.slice(0, list.indexOf('<!-- ================= Mode 2:'));
assert.doesNotMatch(table, /inboundBadge\(node\)/);
assert.match(table, /nodeStatus\(node\)\.label/);
console.log('PASS combined node status priorities and imported states; card merged line and table share one badge');
