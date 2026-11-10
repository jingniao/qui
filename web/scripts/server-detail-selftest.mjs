import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source = readFileSync(new URL('../src/modules/nodes/components/detail/serverDetailModel.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { filterRevisions, defaultRevisionFilter, revisionStrip, basicFacts, detailState, isDirty, revisionResult } = await import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));
const rows = [{ id: 5, summary: 'auto: quota' }, { id: 4, summary: 'manual' }, { id: 3, summary: 'auto: user' }];
assert.deepEqual(filterRevisions(rows, 'all'), rows);
assert.deepEqual(filterRevisions(rows, 'manual').map(r => r.id), [4]);
assert.deepEqual(filterRevisions(rows, 'auto').map(r => r.id), [5, 3]);
assert.equal(defaultRevisionFilter(rows), 'manual');
assert.equal(defaultRevisionFilter(rows.slice(0, 2)), 'all');
assert.equal(defaultRevisionFilter([]), 'all');
assert.equal(defaultRevisionFilter([{ summary: 'auto: only' }]), 'manual');
assert.equal(defaultRevisionFilter([{ summary: 'manual' }]), 'all');
assert.equal(filterRevisions([{ summary: 'automatic manual text' }], 'manual').length, 1);
assert.deepEqual(revisionStrip(42, 42), { running: 42, latest: 42, state: 'latest' });
assert.equal(revisionStrip(40, 42).state, 'unapplied');
assert.equal(revisionStrip(undefined, 42).state, 'unknown');
assert.equal(revisionStrip(0, 0).state, 'unknown');
assert.deepEqual(basicFacts([['blank', ' '], ['nil', null], ['missing', undefined], ['dash', '—'], ['host', 'node-a'], ['zero', 0]]), [['host', 'node-a'], ['zero', 0]]);
const now = Date.parse('2026-10-28T12:00:00Z');
const node = { status: 'online', agentVersion: '1.1', lastHeartbeat: new Date(now).toISOString() };
assert.equal(detailState(node, now), 'ready');
assert.equal(detailState({ ...node, status: 'pending' }, now), 'pending');
assert.equal(detailState({ ...node, agentVersion: '' }, now), 'pending');
assert.equal(detailState({ ...node, status: 'offline' }, now), 'offline');
assert.equal(detailState(node, now + 44999), 'ready');
assert.equal(detailState(node, now + 45000), 'offline');
assert.equal(detailState({ ...node, lastHeartbeat: '' }, now), 'offline');
assert.equal(detailState(node, now - 5001), 'offline');
assert.equal(isDirty({ a: 1, b: { x: 2 } }, { b: { x: 2 }, a: 1 }), false);
assert.equal(isDirty({ name: 'new' }, { name: 'old' }), true);
assert.equal(isDirty({ unset: undefined }, {}), false);
assert.equal(isDirty({ enabled: false }, { enabled: true }), true);
assert.equal(isDirty({ renewal: '' }, { renewal: '' }), false);
assert.equal(isDirty({ ids: [1, 2] }, { ids: [2, 1] }), true);
assert.equal(revisionResult('success'), '已应用且健康');
assert.equal(revisionResult('future_code'), '其他结果');
console.log('All server-detail self-tests passed');

// F2: K2 host observations, nullable historical buckets and tuning contract.
async function loadF2Model(path, transform = s => s) {
  const input = transform(readFileSync(new URL('../src/modules/nodes/components/' + path, import.meta.url), 'utf8'));
  const js = ts.transpileModule(input, {compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.ESNext}}).outputText;
  return import('data:text/javascript;base64,' + Buffer.from(js).toString('base64'));
}
const host = await loadF2Model('hostObservationModel.ts');
assert.equal(host.uint64('18446744073709551615'), Number('18446744073709551615'));
assert.equal(host.uint64('0'), 0);
for (const value of ['', undefined, null, '-1', '1.5', 'abc']) assert.equal(host.uint64(value), null);
assert.equal(host.storageDisplay('0','1024'), '0 B / 1.0 KiB');
assert.equal(host.storageDisplay('12','0',0), '占用率 0.0%');
assert.equal(host.storageDisplay(undefined,'1024',42.5), '占用率 42.5%');
assert.equal(host.storageDisplay('0','0'), '—');
assert.equal(host.observed('0'), '0'); assert.equal(host.observed(undefined), '—');
const history = await loadF2Model('serverMetricsModel.ts', s => s.replace("import { authState } from '../../../app/auth';", "const authState = { timezone: 'Asia/Shanghai' };").replace("import { hostMetrics } from '../metrics';", 'const hostMetrics = node => node.heartbeat;'));
const point = (at, cpuPercent) => history.metricPoint({at:new Date(at).toISOString(),cpuPercent,processCount:0.5,rxBytesPerSecond:0});
const minute = 60000;
const samples = [point(0,1),point(minute,null),point(2*minute,2),point(3*minute,0),point(6*minute,3)];
assert.equal(samples[1].cpu,null); assert.equal(samples[0].processes,0.5); assert.equal(samples[0].rxRate,0); assert.equal(samples[0].tcp,null);
const pathF2 = history.metricPath(samples,'cpu',0,6*minute,60);
assert.equal((pathF2.match(/M/g)||[]).length,3,'null and missing time buckets break the line');
assert.equal((pathF2.match(/L/g)||[]).length,1);
assert.equal(history.metricPath([point(0,null)],'cpu',0,minute,60).trim(),'');
const tuning = await loadF2Model('detail/serverTuningModel.ts');
for (const [status,label] of Object.entries({pending:'等待 Agent 执行',applied:'已应用',cleared:'已清除',failed:'执行失败',unconfirmed:'结果未确认'})) assert.equal(tuning.tuningStatus(status),label);
assert.equal(tuning.tuningStatus('future'),'未知状态');
assert.deepEqual(tuning.tuningRequest('node-a',{'net.core.somaxconn':'128','net.core.default_qdisc':''}),{settings:{'net.core.somaxconn':'128'},clear:false,confirmNodeId:'node-a'});
assert.deepEqual(tuning.tuningRequest('node-a',{'ignored':'bad'},true),{settings:{},clear:true,confirmNodeId:'node-a'});
for (const field of tuning.tuningFields) {
  const valid = field.choices || [String(field.min),String(field.max)];
  for (const value of valid) assert.equal(tuning.tuningRequest('n',{[field.key]:value}).settings[field.key],value);
  for (const value of field.choices ? ['bbrv3','unknown'] : [String(field.min-1),String(field.max+1),'1.5','1e6','-1','1 2 3']) assert.throws(()=>tuning.tuningRequest('n',{[field.key]:value}));
}
assert.throws(()=>tuning.tuningRequest('n',{'net.ipv4.ip_forward':'1'}));
assert.throws(()=>tuning.tuningRequest('n',{}));
for (const preset of tuning.tuningPresets) tuning.tuningRequest('n',preset.settings);
const apiSourceF2 = readFileSync(new URL('../src/modules/nodes/api.ts',import.meta.url),'utf8').replace("import { request, type Page } from '../../app/apiClient';",'const request = (...args) => globalThis.f2Request(...args);');
const apiCodeF2 = ts.transpileModule(apiSourceF2,{compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.ESNext}}).outputText;
const nodeF2 = await import('data:text/javascript;base64,'+Buffer.from(apiCodeF2).toString('base64'));
let calledF2;
globalThis.f2Request = (...args) => {calledF2=args;return Promise.resolve({});};
await nodeF2.nodeAPI.saveTuning('a/b',tuning.tuningRequest('a/b',{'net.core.somaxconn':'128'}));
assert.deepEqual(calledF2,['/nodes/a%2Fb/tuning','PUT',{settings:{'net.core.somaxconn':'128'},clear:false,confirmNodeId:'a/b'}]);
await nodeF2.nodeAPI.tuning('a/b'); assert.equal(calledF2[0],'/nodes/a%2Fb/tuning');
for (const hours of [1,6,12,24,168]) {await nodeF2.nodeAPI.metrics('a/b',hours);assert.equal(calledF2[0],'/nodes/a%2Fb/metrics?hours='+hours);}
delete globalThis.f2Request;
console.log('PASS F2: uint64 strings/zero/unknown, storage fallback, null/time gaps, fractional averages, tuning bounds/statuses/request/API routes');

const merged = tuning.applyPreset({ 'net.core.somaxconn': '4096', 'net.ipv4.tcp_congestion_control': '' }, tuning.tuningPresets[0]);
assert.equal(merged['net.core.somaxconn'], '4096', 'a preset keeps keys it does not name');
assert.equal(merged['net.ipv4.tcp_congestion_control'], 'bbr');
assert.deepEqual(tuning.revertedKeys({ 'net.core.somaxconn': '4096' }, { 'net.core.somaxconn': '' }), ['net.core.somaxconn']);
assert.ok(tuning.tuningFields[0].choices.includes('bbr3'), 'legacy fallback follows the shared contract');
console.log('PASS P1 TUNING: presets merge, reverted keys listed, contract algorithm fallback');

// P7 Task 7 assertions
// 1. tuningRequest with available: Set<string>
const avail = new Set(['net.ipv4.tcp_congestion_control']);
const reqWithAvail = tuning.tuningRequest('n', { 'net.ipv4.tcp_congestion_control': 'bbr', 'net.core.default_qdisc': 'fq' }, false, avail);
assert.deepEqual(reqWithAvail.settings, { 'net.ipv4.tcp_congestion_control': 'bbr' });

// 2. keyStatus 5 cases
const mockTuning = {
  desired: { 'net.ipv4.tcp_congestion_control': 'bbr', 'net.core.default_qdisc': 'fq' },
  current: { 'net.ipv4.tcp_congestion_control': 'bbr', 'net.core.default_qdisc': 'pfifo_fast', 'net.ipv4.tcp_slow_start_after_idle': '0', 'net.ipv4.tcp_ecn': '2' }
};
const mockHost = {
  tuningKeys: [
    { key: 'net.ipv4.tcp_congestion_control', present: true, writable: true },
    { key: 'net.core.default_qdisc', present: true, writable: true },
    { key: 'net.ipv4.tcp_slow_start_after_idle', present: true, writable: true },
    { key: 'net.ipv4.tcp_ecn', present: true, writable: true },
    { key: 'net.core.rmem_max', present: false, writable: false }
  ],
  sysctlSources: [
    { key: 'net.ipv4.tcp_slow_start_after_idle', file: '50-default.conf', value: '0' }
  ]
};
assert.equal(tuning.keyStatus('net.ipv4.tcp_congestion_control', mockTuning, mockHost), 'panel');
assert.equal(tuning.keyStatus('net.core.default_qdisc', mockTuning, mockHost), 'drift');
assert.equal(tuning.keyStatus('net.ipv4.tcp_slow_start_after_idle', mockTuning, mockHost), 'external');
assert.equal(tuning.keyStatus('net.core.rmem_max', mockTuning, mockHost), 'unavailable');
assert.equal(tuning.keyStatus('net.ipv4.tcp_ecn', mockTuning, mockHost), 'default');

// 3. algorithmOptions: {ccAvail:[reno,cubic], ccLoad:[bbr]} -> 3 items with bbr having （可加载）, no bbr3
const ccOpts = tuning.algorithmOptions('cc', { congestionAvailable: ['reno', 'cubic'], congestionLoadable: ['bbr'] });
assert.deepEqual(ccOpts, [
  { value: 'reno', label: 'reno' },
  { value: 'cubic', label: 'cubic' },
  { value: 'bbr', label: 'bbr（可加载）' }
]);
assert.ok(!ccOpts.some(o => o.value === 'bbr3'));

// 4. applyAdvice does not clear other draft keys
const existingDraft = { 'net.core.somaxconn': '1024', 'net.ipv4.tcp_congestion_control': 'cubic' };
const advResult = tuning.applyAdvice(existingDraft, [{ key: 'net.ipv4.tcp_congestion_control', value: 'bbr' }]);
assert.equal(advResult['net.core.somaxconn'], '1024');
assert.equal(advResult['net.ipv4.tcp_congestion_control'], 'bbr');

// 5. 4 presets and LXC filtering
assert.equal(tuning.tuningPresets.length, 4);
const presetNames = tuning.tuningPresets.map(p => p.name);
assert.deepEqual(presetNames, ['通用代理', '高带宽长距离', 'QUIC/Hysteria2', '小内存 VPS']);
const lxcAvail = new Set(['net.ipv4.tcp_congestion_control', 'net.ipv4.tcp_slow_start_after_idle', 'net.ipv4.tcp_notsent_lowat']);
const lxcApplied = tuning.applyPreset({}, tuning.tuningPresets[1], lxcAvail);
assert.ok(!Object.keys(lxcApplied).some(k => k.startsWith('net.core.')));

// 6. Every tuningField has non-empty help string
assert.equal(tuning.tuningFields.length, 19);
for (const field of tuning.tuningFields) {
  assert.ok(typeof field.help === 'string' && field.help.trim().length > 0, `field ${field.key} missing help`);
}

// 7. ServerCardGrid renders tuningAdviceCount
const gridSource = readFileSync(new URL('../src/modules/nodes/components/ServerCardGrid.vue', import.meta.url), 'utf8');
assert.ok(gridSource.includes('tuningAdviceCount'), 'ServerCardGrid must reference tuningAdviceCount');

console.log('PASS P7 TASK 7: tuningRequest available filter, keyStatus 5 states, algorithmOptions, applyAdvice, 4 presets with LXC filter, 19 fields with help, ServerCardGrid badge');

// A06: observed/source TCP vectors represent the maximum submitted by the panel.
for (const key of ['net.ipv4.tcp_rmem', 'net.ipv4.tcp_wmem']) {
  const current = { [key]: '4096 131072 8388608' };
  assert.equal(tuning.keyStatus(key, { current, desired: { [key]: '8388608' } }, {}), 'panel');
  assert.equal(tuning.keyStatus(key, { current, desired: { [key]: '16777216' } }, {}), 'drift');
  for (const value of ['4096 131072 8388608', '8388608']) {
    const host = { sysctlSources: [{ key, file: '50-external.conf', value }] };
    assert.equal(tuning.keyStatus(key, { current }, host), 'external');
    const adopted = tuning.adoptExternal({}, { current }, host);
    assert.equal(adopted[key], '8388608');
    assert.equal(tuning.tuningRequest('n', adopted).settings[key], '8388608');
  }
}

// Execute the component's actual host-derived computed values with fixture data.
const tuningSfc = readFileSync(new URL('../src/modules/nodes/components/detail/ServerTuning.vue', import.meta.url), 'utf8');
const hostComputeds = tuningSfc.slice(tuningSfc.indexOf('const hostInfo ='), tuningSfc.indexOf('const essentialFields ='));
const hostJs = ts.transpileModule(hostComputeds, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const { computed } = await import('vue');
function tuningHost(host) {
  return new Function('computed', 'props', hostJs + '\nreturn { availableKeys: availableKeys.value, fdLimits: fdLimits.value };')(computed, { node: { capabilities: { host } } });
}
const cc = 'net.ipv4.tcp_congestion_control', qdisc = 'net.core.default_qdisc', rmem = 'net.core.rmem_max';
const restricted = { tuningKeys: [
  { key: cc, present: true, writable: true }, { key: qdisc, present: true, writable: true },
  { key: rmem, present: true, writable: false }, { key: 'net.core.wmem_max', present: false, writable: true }
], congestionAvailable: ['cubic'], congestionLoadable: ['bbr3'], qdiscAvailable: ['fq_codel'] };
const available = tuningHost(restricted).availableKeys;
assert.deepEqual([...available], [cc, qdisc]);
assert.equal(tuning.keyStatus(rmem, {}, restricted), 'unavailable');
assert.equal(tuning.keyStatus('net.core.wmem_max', {}, restricted), 'unavailable');
assert.deepEqual(tuning.tuningRequest('n', { [cc]: 'bbr3', [rmem]: '8388608' }, false, available, restricted).settings, { [cc]: 'bbr3' });
assert.throws(() => tuning.tuningRequest('n', { [cc]: 'bbr' }, false, available, restricted));
assert.throws(() => tuning.tuningRequest('n', { [cc]: 'reno' }, false, available, { congestionAvailable: [] }));
assert.equal(tuning.tuningRequest('n', { [cc]: 'bbr3' }, false, undefined, {}).settings[cc], 'bbr3');
assert.ok(tuning.algorithmOptions('cc', {}).some(o => o.value === 'bbr3'));
assert.deepEqual(tuning.applyPreset({}, tuning.tuningPresets[1], available, restricted), {});
assert.deepEqual(tuning.applyAdvice({}, [{ key: cc, value: 'bbr' }, { key: qdisc, value: 'fq' }, { key: rmem, value: '8388608' }], available, restricted), {});
assert.deepEqual(tuning.applyAdvice({}, [{ key: cc, value: 'bbr3' }], available, restricted), { [cc]: 'bbr3' });
assert.deepEqual(tuning.adoptExternal({}, { current: { [rmem]: '8388608' } }, { ...restricted, sysctlSources: [{ key: rmem, value: '8388608', file: '50-other.conf' }] }), {});
const limits = [{ name: 'agent', soft: '524287', hard: '524287' }, { name: 'xray', soft: '18446744073709551615', hard: '18446744073709551615' }];
assert.deepEqual(tuningHost({ fdLimits: limits }).fdLimits, limits);
assert.deepEqual(tuningHost({ fd_limits: limits }).fdLimits, limits);
assert.deepEqual(tuningHost({}).fdLimits, []);
assert.match(tuningSfc, /v-for="limit in fdLimits"/);
assert.match(tuningSfc, /limit\.name/);
assert.match(tuningSfc, /limit\.soft/);
assert.match(tuningSfc, /limit\.hard/);
assert.match(tuningSfc, /v-if="!fdLimits.length"[^>]*>未知</);
assert.doesNotMatch(tuningSfc, /重启后保持|persistAcrossReboot/);
assert.match(tuningSfc, /当前与期望一致/);
assert.match(tuningSfc, /<p v-if="error"[^>]*text-ios-red[^>]*>\{\{ error \}\}<\/p>/);
assert.equal((tuningSfc.match(/v-if="!isKeyAvailable\(field.key\)"/g) || []).length, 2, 'both tables hide unavailable inputs');
assert.match(tuningSfc, /v-if="canEdit && adoptableAdviceKeys\.has\(item.key\)"/);
assert.equal((tuningSfc.match(/v-if="canEdit && adoptableAdviceKeys\.has\(field.key\)"/g) || []).length, 2, 'both tables hide unavailable recommendation actions');

// A15: validation, save and recommendation failures all reach the rendered error.
const script = tuningSfc.split('<script setup lang="ts">')[1].split('</script>')[0];
const tree = ts.createSourceFile('ServerTuning.ts', script, ts.ScriptTarget.Latest, true);
const handlers = tree.statements.filter(s => ts.isFunctionDeclaration(s) && ['confirm', 'submit', 'dismissAdvice'].includes(s.name?.text)).map(s => s.getText(tree)).join('\n');
const handlersJs = ts.transpileModule(handlers, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const errors = { value: '' }, errorDraft = { value: { 'net.core.somaxconn': 'bad' } };
const handlerHarness = new Function('error', 'draft', 'tuningRequest', handlersJs.replace(/^/, 'let confirmed = null; const props = {node: {id: "n"}}, canEdit = {value: true}, clearing = {}, dialog = {}, availableKeys = {}, hostInfo = {}, sending = {}, lifecycle = 0, state = {value: null}; const stopPoll = () => {}, refresh = async () => {}, loadRecommendations = async () => {}, showToast = () => {}; const nodeAPI = {saveTuning: async () => {throw new Error("save rejected");}, dismissTuningRecommendation: async () => {throw new Error("advice rejected");}};\n') + '\nreturn {confirm, submit, dismissAdvice};')(errors, errorDraft, tuning.tuningRequest);
handlerHarness.confirm(false); assert.match(errors.value, /参数超出允许范围/);
errorDraft.value = { 'net.core.somaxconn': '128' };
handlerHarness.confirm(false); await handlerHarness.submit(); assert.match(errors.value, /save rejected/);
await handlerHarness.dismissAdvice({ key: cc, value: 'bbr3' }); assert.match(errors.value, /advice rejected/);
console.log('PASS AUDIT A06–A10/A15: vector maximum, writable filter, host algorithms, filtered merges, process fd limits, honest consistency status and visible errors');
