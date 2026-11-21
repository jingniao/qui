import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
// The model imports the shared result-code labels, so its imports are resolved too.
async function tsModuleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  let code = ts.transpileModule(readFileSync(url, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  for (const spec of new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(match => match[1]).filter(value => value.startsWith('.')))) {
    const target = new URL(spec, url);
    if (!/\.tsx?$/.test(target.pathname)) target.pathname += '.ts';
    code = code.split(`from '${spec}'`).join(`from '${await tsModuleUrl(target, cache)}'`);
  }
  const moduleUrl = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, moduleUrl);
  return moduleUrl;
}
const source = readFileSync(new URL('../src/modules/nodes/components/detail/serverDetailModel.ts', import.meta.url), 'utf8');
const { filterRevisions, defaultRevisionFilter, revisionStrip, basicFacts, detailState, isDirty, revisionResult } = await import(await tsModuleUrl(new URL('../src/modules/nodes/components/detail/serverDetailModel.ts', import.meta.url)));
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
assert.equal(detailState(node), 'ready');
assert.equal(detailState({ ...node, status: 'pending' }), 'pending');
assert.equal(detailState({ ...node, agentVersion: '' }), 'pending');
assert.equal(detailState({ ...node, status: 'offline' }), 'ready', 'a registered offline server keeps its detail tabs; serverStatus owns availability');
assert.equal(detailState({ ...node, lastHeartbeat: new Date(now - 44999).toISOString() }), 'ready');
assert.equal(detailState({ ...node, lastHeartbeat: new Date(now - 45000).toISOString() }), 'ready', 'the 45-second metric cutoff does not control detail registration state');
assert.equal(detailState({ ...node, lastHeartbeat: '' }), 'ready');
assert.equal(detailState({ ...node, lastHeartbeat: new Date(now + 5001).toISOString() }), 'ready');
assert.equal(isDirty({ a: 1, b: { x: 2 } }, { b: { x: 2 }, a: 1 }), false);
assert.equal(isDirty({ name: 'new' }, { name: 'old' }), true);
assert.equal(isDirty({ unset: undefined }, {}), false);
assert.equal(isDirty({ enabled: false }, { enabled: true }), true);
assert.equal(isDirty({ renewal: '' }, { renewal: '' }), false);
assert.equal(isDirty({ ids: [1, 2] }, { ids: [2, 1] }), true);
assert.equal(revisionResult('success'), '已应用且健康');
assert.equal(revisionResult('future_code'), 'future_code');
console.log('All server-detail self-tests passed');

// F2: K2 host observations, nullable historical buckets and tuning contract.
async function loadF2Model(path, transform = s => s) {
  const input = transform(readFileSync(new URL('../src/modules/nodes/components/' + path, import.meta.url), 'utf8'));
  const bytesCode = ts.transpileModule(readFileSync(new URL('../src/app/bytes.ts', import.meta.url), 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  const linked = input.replace("from '../../../app/bytes'", "from 'data:text/javascript;base64," + Buffer.from(bytesCode).toString('base64') + "'");
  const js = ts.transpileModule(linked, {compilerOptions:{target:ts.ScriptTarget.ES2020,module:ts.ModuleKind.ESNext}}).outputText;
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
const dataF2 = history.seriesData(samples,'cpu',60);
assert.deepEqual(dataF2.map(p=>p.y),[1,null,2,0,null,3],'null values and gaps over 1.5 x the step insert null, adjacent minutes stay joined');
assert.equal(dataF2[4].x,4.5*minute,'the gap marker sits between the neighbours');
assert.deepEqual(history.seriesData([point(0,null)],'cpu',60).map(p=>p.y),[null]);
assert.deepEqual(history.seriesData([point(0,1),point(90000,2),point(90000+90001,3)],'cpu',60).map(p=>p.y),[1,2,null,3],'exactly 1.5 x the step stays joined, beyond it breaks');
const tuning = await loadF2Model('detail/serverTuningModel.ts');
const moduleHost = {tuningProbeStatus:'ready', tuningModuleInstallSupported:true, congestionAvailable:['reno']};
assert.ok(tuning.algorithmOptions('cc', moduleHost, undefined, true).some(o => o.value === 'bbr' && o.label.includes('补装')));
assert.ok(!tuning.algorithmOptions('cc', moduleHost).some(o => o.value === 'bbr'));
assert.ok(!tuning.algorithmOptions('cc', moduleHost, undefined, true).some(o => o.value === 'bbr3'));
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
assert.ok(!gridSource.includes('coreSummaryLabel'), 'ServerCardGrid must not reference coreSummaryLabel');

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
// Run the compiled button bindings with deferred network replies. No browser
// timers or remote node are needed to distinguish a click, a 202 and a receipt.
{
  const Vue = await import('vue');
  const { parse, compileScript } = await import('@vue/compiler-sfc');
  const { renderToString } = await import('@vue/server-renderer');
  const source = compileScript(parse(tuningSfc).descriptor, { id: 'tuning', inlineTemplate: true }).content;
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
  const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
  const after = () => new Promise(resolve => setImmediate(resolve));
  const advice = deferred(), send = deferred(), status = deferred(), stale = deferred(), next = deferred();
  const key = 'net.ipv4.tcp_congestion_control', omitted = 'net.core.rmem_max';
  const previous = { supported: true, desired: { [key]: 'bbr', [omitted]: '8388608' }, requestId: 'old', status: 'failed', result: { requestId:'old', status:'failed', keys:[{ error:'old-error' }] }, current: {}, requestedAt: null, updatedAt: null };
  let statusReads = 0, adviceReads = 0, submitted, currentTree, backgroundRead, poll, now = Date.now();
  const delayedFailure = deferred();
  const api = { tuning: () => [Promise.resolve(previous), stale.promise, status.promise, delayedFailure.promise, next.promise][statusReads++],
    tuningRecommendations: () => { adviceReads++; return advice.promise; },
    saveTuning: (_, input) => { submitted = input; return send.promise; } };
  const dialog = { name:'ConfirmDialog', render: () => null };
  const apiClient = await import(await tsModuleUrl(new URL('../src/app/apiClient.ts', import.meta.url)));
  const require = name => ({ 'vue':Vue, '../../../../app/auth': { isOwner:Vue.ref(true), formatTime:v=>v },
    '../../../../app/api': { showToast:()=>{} }, '../../../../app/apiClient': apiClient, '../../api':{nodeAPI:api},
    './serverTuningModel': tuning, '../../../../components/ConfirmDialog.vue':{default:dialog} })[name];
  const exports = {};
  new Function('require','exports','document','setInterval','setTimeout','clearTimeout','Date',code)(require,exports,
    {visibilityState:'visible', addEventListener(){}}, callback=>{backgroundRead=callback;return 1;}, callback=>{poll=callback;return 1;}, ()=>{}, class extends Date { static now(){return now;} });
  const props = { node:{id:'n', name:'test', capabilities:{ host: { tuningKeys:[{ key, present:true, writable:true }], congestionAvailable:['reno'], tuningModuleInstallSupported:true, tuningProbeStatus:'ready' } } }, isOwner:true };
  let render;
  const instance = { setup() {
    render = exports.default.setup(props, {expose(){},emit(){}});
    return () => { currentTree = render({}, []); return currentTree; };
  }};
  async function view() {
    // Keep the original setup state between renders, including in-flight promises.
    const app = Vue.createSSRApp(render ? {render(){currentTree=render({},[]); return currentTree;}} : instance);
    const html = await renderToString(app);
    function find(v, predicate) {
      if (!v || typeof v !== 'object') return;
      if (predicate(v)) return v;
      if (Array.isArray(v.children)) for (const child of v.children) { const found=find(child,predicate); if(found)return found; }
    }
    return { html, find: pred => find(currentTree,pred) };
  }
  await view(); await after();
  let page = await view();
  const checkbox = page.find(v => v.type === 'input' && v.props?.type === 'checkbox' && v.props['onUpdate:modelValue']);
  assert.match(page.html, /本次允许按需补装 Ubuntu/);
  checkbox.props['onUpdate:modelValue'](true);
  page = await view();
  const button = page.find(v => v.type === 'button' && String(v.children).includes('一键应用'));
  assert.ok(button && !button.props.disabled, 'slow recommendations must not delay loaded state or its apply button');
  backgroundRead(); // This GET started before submission and must not overwrite its result.
  button.props.onClick(); page = await view();
  const confirmation = page.find(v => v.type === dialog);
  assert.equal(confirmation.props.modelValue, true);
  assert.match(confirmation.props.message, /允许安装与运行内核完全匹配/);
  assert.match(confirmation.props.message, /net\.core\.rmem_max/, 'filtered previous key will revert');
  confirmation.props.onConfirm(); page=await view();
  assert.match(page.html, /正在发送/);
  assert.deepEqual(submitted.settings, {[key]:'bbr'});
  assert.equal(submitted.installMissingModules,true);
  send.resolve({requestId:'new',status:'pending'}); await after(); page=await view();
  assert.match(page.html, /等待 Agent 执行/); assert.match(page.html, /new/);
  assert.doesNotMatch(page.html,/old-error/);
  stale.resolve(previous); await after(); page=await view();
  assert.match(page.html, /等待 Agent 执行/); assert.doesNotMatch(page.html, /old-error/);
  status.resolve({...previous, requestId:'owner-b',status:'pending',result:null,installMissingModules:true});
  await after(); page=await view();
  assert.match(page.html, /new[^<]*owner-b/, 'an authoritative later read must show which request superseded the pending one');
  now += (15*60+29)*1000;
  poll();
  delayedFailure.reject(new Error('node unavailable')); await after();
  assert.equal(statusReads,4,'a failed read is retried within the module and sysctl budget');
  now += 1000; poll(); backgroundRead(); await after();
  assert.equal(statusReads,4,'module timeout must stop short and periodic polling');
  page = await view();
  assert.match(page.html, /结果未确认/);
  page.find(v => v.type === 'button' && String(v.children).includes('刷新状态')).props.onClick();
  assert.equal(statusReads,5,'manual refresh can still obtain the late receipt');
  next.resolve({...previous, requestId:'owner-b', result:{requestId:'owner-b',status:'failed',keys:[{error:'actual kernel error'}]}});
  await after(); page=await view();
  assert.match(page.html.slice(0,page.html.indexOf('白名单参数')), /owner-b[^<]*actual kernel error/);
  assert.equal(page.find(v => v.type === 'button' && String(v.children).includes('一键应用')).props.disabled, false);
  assert.equal(adviceReads,1,'progress must not re-read recommendations');
  advice.reject(new Error('advice stalled')); await after(); page=await view();
  assert.match(page.html,/advice stalled/); assert.match(page.html.slice(0,page.html.indexOf('白名单参数')), /actual kernel error/);
  console.log('PASS tuning button: confirmed settings, accepted request, kernel error and independent advice');
}
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
assert.deepEqual([...(tuningHost({tuningProbeStatus: 'boot mismatch'}).availableKeys ?? ['all selectable'])], [], 'new worker with unusable probe must block editing');
assert.deepEqual(tuning.algorithmOptions('cc', {tuningProbeStatus:'ready'}), [], 'new worker must not invent available algorithms');
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
const errors = { value: '' }, adviceError = { value: '' }, errorDraft = { value: { 'net.core.somaxconn': 'bad' } };
const handlerHarness = new Function('error', 'adviceError', 'draft', 'tuningRequest', handlersJs.replace(/^/, 'let reads = 0, refreshing = false; const confirmed = {value:null}, readError = {value:""}, replacedRequest = {value:""}; const props = {node: {id: "n"}}, canEdit = {value: true}, clearing = {}, dialog = {}, availableKeys = {}, hostInfo = {}, installModules = {value:false}, sending = {}, lifecycle = 0, state = {value: null}; const stopPoll = () => {}, refresh = async () => {}, loadRecommendations = async () => {}, showToast = () => {}; const nodeAPI = {saveTuning: async () => {throw new Error("save rejected");}, dismissTuningRecommendation: async () => {throw new Error("advice rejected");}};\n') + '\nreturn {confirm, submit, dismissAdvice};')(errors, adviceError, errorDraft, tuning.tuningRequest);
handlerHarness.confirm(false); assert.match(errors.value, /参数超出允许范围/);
errorDraft.value = { 'net.core.somaxconn': '128' };
handlerHarness.confirm(false); await handlerHarness.submit(); assert.match(errors.value, /save rejected/);
await handlerHarness.dismissAdvice({ key: cc, value: 'bbr3' }); assert.match(adviceError.value, /advice rejected/);
console.log('PASS AUDIT A06–A10/A15: vector maximum, writable filter, host algorithms, filtered merges, process fd limits, honest consistency status and visible errors');
