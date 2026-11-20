import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { parse } from '@vue/compiler-sfc';
import { baseParse } from '@vue/compiler-dom';

const read = path => readFileSync(new URL('../src/modules/' + path, import.meta.url), 'utf8');
const detail = read('nodes/pages/NodeDetailPage.vue');
const heartbeat = detail.match(/function nodeStatusChanged\(event: Event\) \{([\s\S]*?)\n\}/)[1];
assert.doesNotMatch(heartbeat, /\bload\(/, 'heartbeat must not reload supplements');
assert.match(heartbeat, /refreshNode/);
assert.ok(detail.indexOf('node.value = server;', detail.indexOf('async function load()')) < detail.indexOf('void loadSupplements();', detail.indexOf('async function load()')), 'render node before optional reads');
const refreshSource = detail.slice(detail.indexOf('const nodeRequests ='), detail.indexOf('async function load()'));
const refreshCode = ts.transpileModule(refreshSource + '\nreturn { refreshNode, visibilityChanged, mount: () => mounted = true, unmount: () => mounted = false };', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
let calls = 0, resolveRead, supplementCalls = 0;
const visibility = { visibilityState: 'hidden' }, node = { value: { id: 'n1', name: '旧数据' } }, error = { value: '' };
const refresh = new Function('nodeAPI', 'document', 'loading', 'route', 'generation', 'node', 'now', 'error', 'loadSupplements', refreshCode)(
  { get: () => { calls++; return new Promise(resolve => { resolveRead = resolve; }); } }, visibility,
  { value: false }, { name: 'NodeDetail', params: { id: 'n1' } }, 1, node, { value: 0 }, error, () => { supplementCalls++; }
);
refresh.mount();
await refresh.refreshNode();
assert.equal(calls, 0, 'hidden page makes no heartbeat request');
visibility.visibilityState = 'visible';
const pending = refresh.refreshNode();
await refresh.refreshNode();
assert.equal(calls, 1, 'concurrent heartbeats share a read');
resolveRead({ id: 'n1', name: '新数据' });
await pending;
assert.equal(node.value.name, '新数据');
assert.equal(supplementCalls, 0, 'heartbeat never fetches supplements');
refresh.visibilityChanged();
assert.equal(calls, 2, 'returning to foreground refreshes immediately');
refresh.unmount();
resolveRead({ id: 'n1', name: '过期数据' });
await new Promise(resolve => setImmediate(resolve));
assert.equal(node.value.name, '新数据', 'unmounted page discards late response');
const tuning = read('nodes/components/detail/ServerTuning.vue');
assert.doesNotMatch(tuning, /watch\(\(\) => props.node.lastHeartbeat/);
assert.match(tuning, /reads\+\+; refreshing = false;/, 'saving invalidates old tuning reads');
const settings = read('nodes/components/detail/ServerSettings.vue');
assert.match(settings, /watch\(\(\) => props.node.id/);
assert.match(settings, /current !== machineGeneration/, 'old machine traffic reads cannot overwrite a save');
const mergeBody = settings.match(/watch\(\(\) => props.meta, \(meta\) => \{([\s\S]*?)\n\}\);/)[1];
const mergeCode = ts.transpileModule(mergeBody, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const merge = new Function('meta', 'metaForm', 'baseline', 'parseRenewPrice', mergeCode);
const draft = { region: '', remark: '用户编辑', renewDate: '' };
const baseline = { basic: { region: '', remark: '' }, renewal: { renewDate: '' } };
merge({ region: 'DE', remark: '服务器原备注', renewDate: '2027-01-01' }, draft, baseline, () => ({ currency: 'CNY', price: '', cycle: '/年' }));
assert.equal(draft.remark, '用户编辑', 'unsaved metadata survives refresh');
assert.equal(draft.region, 'DE', 'untouched fields receive late metadata');
assert.equal(draft.renewDate, '2027-01-01');
assert.equal(baseline.basic.region, 'DE', 'late metadata does not mark untouched fields dirty');
const machineBody = settings.match(/function mergeMachineSettings\([^]*?\) \{([^]*?)\n\}/)[0];
const machineCode = ts.transpileModule(machineBody + '\nreturn mergeMachineSettings;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const machineDraft = { method: 'interface', interfaceName: '用户编辑', resetDay: 1 };
const machineBaseline = { value: { method: 'interface', interfaceName: '', resetDay: 1 } };
const mergeMachine = new Function('machineForm', 'machineBaseline', machineCode)(machineDraft, machineBaseline);
mergeMachine({ method: 'interface', interfaceName: 'eth0', resetDay: 15 }, machineBaseline.value);
assert.equal(machineDraft.interfaceName, '用户编辑');
assert.equal(machineDraft.resetDay, 15);
const list = read('nodes/pages/NodeListPage.vue');
const listRefresh = list.match(/async function silentRefresh\(\) \{[\s\S]*?^}/m)?.[0];
const calibration = list.match(/async function calibrateRuntime\(\) \{[\s\S]*?^}/m)?.[0];
const confirmation = list.match(/function confirmInventory\(invalidation: number\) \{[\s\S]*?^}/m)?.[0];
assert.ok(listRefresh && calibration && confirmation, 'found the static and runtime refresh functions');
const listRefreshCode = ts.transpileModule(`
  const document = { visibilityState: 'visible' }, deleted = { value: false }, inventoryWarnings = { value: [] };
  const isOwner = { value: true };
  const now = { value: 0 }, statusFresh = { value: false };
  let refreshingInventory = false, calibrating = false;
  let inventoryInvalidation = 1, inventoryConfirmed = 0, lastInventorySuccess = 0;
  const staticInventoryInterval = 5 * 60 * 1000;
  let staticInventoryTimer;
  const recheckStaticInventory = () => {}, setTimeout = () => 1, clearTimeout = () => {};
  let inventoryRead = 0, visibilityGeneration = 0, runtimeRead = 0;
  let inventory = [], scheduledStatic;
  const inventoryScope = { value: false }, loading = { value: false }, invalidatesRead = () => false;
  const runtime = { generation: '', inventoryRevision: 0, calibrate: snapshot => snapshot.generation === 'generation' };
  const console = { error: logError };
  const ours = () => true, choices = reader => reader(1, 20);
  const refreshStaticInventory = () => {}, throttledSilentRefresh = () => { scheduledStatic = silentRefresh(); };
  const updateVisibleInventory = () => showInventory(inventory);
  ${listRefresh}
  ${calibration}
  ${confirmation}
  return { silentRefresh, calibrateRuntime, inventoryWarnings, statusFresh, waitForStatic: () => scheduledStatic };
`, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
let staticCalls = 0, metaCalls = 0, runtimeCalls = 0;
const shown = [], runtimeErrors = [];
const listReads = new Function('nodeAPI', 'request', 'loadNodeMetadata', 'showInventory', 'logError', listRefreshCode)(
  { list: async () => { if (++staticCalls === 1) throw new Error('static failure'); return [{ id: 'n1' }]; } },
  async path => {
    assert.equal(path, '/nodes/runtime');
    if (++runtimeCalls === 1) throw new Error('runtime failure');
    return { generation: 'generation', items: [] };
  },
  async () => { if (++metaCalls === 2) throw new Error('meta failure'); },
  inventory => shown.push(inventory),
  (_, error) => runtimeErrors.push(error.message),
);
await listReads.silentRefresh();
assert.match(listReads.inventoryWarnings.value[0], /static failure/);
assert.equal(staticCalls, 1, 'static failure does not retry immediately');
await listReads.calibrateRuntime();
assert.equal(listReads.statusFresh.value, false, 'failed calibration keeps the display clock frozen');
assert.deepEqual(runtimeErrors, ['runtime failure']);
await listReads.calibrateRuntime();
assert.equal(runtimeCalls, 2, 'runtime calibration can retry after a failed request');
assert.equal(listReads.statusFresh.value, true);
await listReads.waitForStatic();
assert.equal(staticCalls, 2, 'calibration retries a failed static request');
assert.deepEqual(shown.at(-1), [{ id: 'n1' }], 'successful list data may display before a metadata retry');
assert.match(listReads.inventoryWarnings.value[0], /meta failure/);
await listReads.calibrateRuntime();
await listReads.waitForStatic();
assert.equal(staticCalls, 3, 'failed metadata leaves the inventory refresh pending');
assert.deepEqual(listReads.inventoryWarnings.value, [], 'successful retry clears only inventory warnings');
await listReads.calibrateRuntime();
assert.equal(staticCalls, 3, 'a complete static refresh consumes the pending invalidation');
for (const path of ['nodes/components/ServerMetricsGrid.vue', 'nodes/components/LatencyChart.vue', 'nodes/components/NodeRestartPanel.vue', 'nodes/components/NodeHealthPanel.vue', 'nodes/components/ServerEnrollment.vue', 'nodes/components/detail/ServerTuning.vue', 'system/components/NodeVersionPanel.vue', 'system/components/CoreUpdatesPanel.vue']) {
  const source = read(path);
  assert.match(source, /document.visibilityState/, path + ' pauses hidden reads');
  assert.match(source, /addEventListener\('visibilitychange'/, path + ' resumes on return');
  assert.match(source, /removeEventListener\('visibilitychange'/, path + ' cleans up');
}
const changedVue = [
  'nodes/pages/NodeDetailPage.vue', 'nodes/pages/NodeListPage.vue',
  'nodes/components/detail/ServerSettings.vue', 'nodes/components/detail/ServerTuning.vue',
  'nodes/components/detail/ServerRevisions.vue', 'nodes/components/ServerMetricsGrid.vue',
  'nodes/components/LatencyChart.vue', 'nodes/components/NodeRestartPanel.vue',
  'nodes/components/NodeHealthPanel.vue', 'nodes/components/NodeCertificatePanel.vue',
  'nodes/components/ServerEnrollment.vue', 'system/components/NodeVersionPanel.vue',
  'system/components/CoreUpdatesPanel.vue'
];
for (const path of changedVue) {
  const source = read(path);
  const { descriptor, errors } = parse(source);
  assert.equal(errors.length, 0, path + ' parses');
  const visit = node => {
    if (node.type === 1 && node.tag === 'template') assert.ok(node.props.some(prop => prop.type === 7), path + ' has no bare nested template');
    for (const child of node.children || []) visit(child);
  };
  visit(baseParse(descriptor.template.content));
}
const self = readFileSync(new URL(import.meta.url));
assert.notEqual(self[0], 0xef, 'new selftest has no BOM');
assert.ok(!self.includes(13), 'new selftest uses LF');
console.log('Vue template / LF / BOM checks passed (13 components)');
console.log('server-perf-selftest passed');
