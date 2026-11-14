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
assert.ok(detail.indexOf('node.value = server;', detail.indexOf('async function load()')) < detail.indexOf('const [groupResult'), 'render node before optional reads');
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
assert.match(list, /finally \{ refreshing = false; \}/, 'silent list refresh releases its lock');
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
