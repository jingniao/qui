import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const compile = code => ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
const panel = read('modules/system/components/NodeVersionPanel.vue');
const sharedPath = new URL('../src/modules/system/nodeBaseline.ts', import.meta.url);
const shared = readFileSync(sharedPath, 'utf8').replace(/^import .*;\r?\n/gm, '').replaceAll('export ', '');
assert.match(panel, /import \{ needsBaseline, publishBaselineAndWait \} from '\.\.\/nodeBaseline'/);
assert.doesNotMatch(panel, /async function publishBaselineAndWait/);
const flow = read('modules/tunnels/components/PublishFlow.vue');
const publish = flow.slice(flow.indexOf('async function publish()'), flow.indexOf('</script>'));
async function scenario(heartbeat, mode = 'success') {
  const calls = [], progress = { value: '' }, result = { value: null }, error = { value: '' }, installResults = { value: [] };
  let time = 0, reads = 0;
  const initial = { id: 'n', name: '服务器', heartbeat, capabilities: { cores: [] } };
  const nodeAPI = { get: async () => {
    calls.push('get'); reads++;
    if (reads === 1) return structuredClone(initial);
    if (mode === 'failed' || mode === 'timeout' || (mode === 'pending' && reads === 2)) return structuredClone(initial);
    return { ...initial, heartbeat: mode === 'current' ? { current_revision: 7 } : { successful_revision: 7 }, capabilities: { cores: calls.includes('queue') ? [{ name: 'gost' }] : [] } };
  } };
  const deploymentAPI = {
    publishBaseline: async id => { assert.equal(id, 'n'); calls.push('baseline'); assert.match(progress.value, /正在为 服务器 发布基础配置/); return { revision: 7 }; },
    nodeRevisions: async () => ({ items: mode === 'failed' ? [{ id: 7, status: 'rollback', resultCode: 'apply_failed' }] : [] })
  };
  const run = new Function('nodeAPI', 'deploymentAPI', 'coreUpdatesAPI', 'gostInstallRelease', 'tunnelAPI', 'plan', 'busy', 'error', 'progress', 'installResults', 'result', 'selection', 'emit', 'tunnelError', 'APIError', 'Date', 'setTimeout', compile(shared + '\n' + publish + '\nreturn publish;'))(
    nodeAPI, deploymentAPI, { queue: async input => { calls.push('queue'); assert.equal(input.revision, heartbeat?.successful_revision || heartbeat?.current_revision || 7); } }, async () => ({}),
    { publish: async () => { calls.push('publish'); return {}; } }, { value: { ok: true, conflicts: [], installGost: [{ nodeId: 'n', name: '服务器' }] } }, { value: false }, error, progress, installResults, result,
    { tunnelIds: [], ruleIds: [] }, () => {}, e => e.message, class extends Error {}, { now: () => time }, callback => { time += 3000; callback(); });
  await run();
  return { calls, error, installResults, time };
}
for (const heartbeat of [{ current_revision: 0 }, {}, undefined]) {
  const { calls } = await scenario(heartbeat);
  assert.ok(calls.includes('baseline'), 'unpublished nodes must publish baseline');
  assert.ok(calls.indexOf('baseline') < calls.indexOf('queue'));
  assert.ok(calls.indexOf('queue') < calls.indexOf('publish'));
}
for (const heartbeat of [{ current_revision: 4 }, { successful_revision: 5, current_revision: 0 }]) {
  const { calls } = await scenario(heartbeat);
  assert.ok(!calls.includes('baseline'));
  assert.ok(calls.includes('publish'));
}
for (const mode of ['current', 'pending']) {
  const { calls } = await scenario({ current_revision: 0 }, mode);
  assert.ok(calls.includes('publish'));
  if (mode === 'pending') assert.deepEqual(calls.slice(0, 5), ['get', 'baseline', 'get', 'get', 'queue']);
}
for (const mode of ['failed', 'timeout']) {
  const outcome = await scenario({ current_revision: 0 }, mode);
  assert.ok(!outcome.calls.includes('queue'));
  assert.ok(!outcome.calls.includes('publish'));
  assert.match(outcome.error.value, /未发布隧道/);
  assert.match(outcome.installResults.value[0], mode === 'failed' ? /apply_failed/ : /超时/);
  assert.ok(outcome.time <= 180000);
}
console.log('PASS tunnel baseline: zero/missing revision, confirmed revision, baseline failure and three-minute timeout');
