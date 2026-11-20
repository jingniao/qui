import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { compileScript, parse } from '@vue/compiler-sfc';
import ts from 'typescript';
import * as Vue from 'vue';
import { createServer } from 'vite';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const vite = await createServer({ root: fileURLToPath(new URL('..', import.meta.url)), configFile: false, logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
const client = await vite.ssrLoadModule('/src/app/apiClient.ts');
const model = await vite.ssrLoadModule('/src/modules/nodes/components/detail/serverTuningModel.ts');
await vite.close();
const component = compileScript(parse(read('modules/nodes/components/detail/ServerTuning.vue')).descriptor, { id: 'tuning' });
const code = ts.transpileModule(component.content, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS } }).outputText;
const settle = () => new Promise(resolve => setImmediate(resolve));

test('soft-deleted node unmounts its tuning tab', () => {
  const tag = read('modules/nodes/pages/NodeDetailPage.vue').match(/<ServerTuning\b[^>]*\/>/)[0];
  const guard = tag.match(/v-if="([^"]+)"/)[1];
  const visible = new Function('activeTab', 'node', 'return ' + guard);
  assert.ok(visible('tuning', { id: 'n' }));
  assert.equal(visible('tuning', { id: 'n', deletedAt: '2026-11-19' }), false);
});

test('tuning refuses stale state, draft, confirmation and late advice after deletion', async () => {
  const key = 'net.core.rmem_max';
  const state = { supported: true, desired: { [key]: '8388608' }, status: 'applied', requestId: 'old' };
  const advice = { items: [{ key, value: '8388608' }] };
  const api = { tuning: async () => state, tuningRecommendations: async () => advice };
  const dependencies = { vue: { ...Vue, onUnmounted() {} }, '../../../../app/auth': { isOwner: Vue.ref(true) },
    '../../../../app/api': {}, '../../../../app/apiClient': client, '../../api': { nodeAPI: api },
    './serverTuningModel': model, '../../../../components/ConfirmDialog.vue': {} };
  const exports = {};
  new Function('require', 'exports', 'document', 'setInterval', code)(name => dependencies[name], exports,
    { visibilityState: 'visible', addEventListener() {} }, () => 0);
  const scope = Vue.effectScope();
  try {
    const props = { node: { id: 'n', name: 'node' }, isOwner: true };
    const page = scope.run(() => exports.default.setup(props, { expose() {}, emit() {} }));
    await settle();
    page.confirm(false);
    assert.ok(page.dialog.value && page.confirmed.value);
    api.tuning = async () => { throw new Error('temporary'); };
    api.tuningRecommendations = async () => { throw new Error('temporary'); };
    await Promise.all([page.refresh(), page.loadRecommendations()]);
    assert.equal(page.state.value.requestId, 'old');
    assert.equal(page.recommendations.value.items.length, 1);
    assert.equal(page.draft.value[key], '8388608');
    assert.match(page.readError.value, /temporary/);
    assert.match(page.adviceError.value, /temporary/);
    api.tuningRecommendations = async () => { throw new client.APIError(403, 'forbidden', 'denied'); };
    await page.loadRecommendations();
    assert.equal(page.recommendations.value, null, 'rejected advice must disappear');
    let finishAdvice;
    api.tuningRecommendations = () => new Promise(resolve => { finishAdvice = resolve; });
    const pending = page.loadRecommendations();
    api.tuning = async () => { throw new client.APIError(404, 'not_found', 'deleted'); };
    await page.refresh();
    finishAdvice(advice); await pending;
    assert.equal(page.state.value, null);
    assert.equal(page.draft.value[key], '');
    assert.equal(page.dialog.value, false);
    assert.equal(page.confirmed.value, null);
    assert.equal(page.recommendations.value, null);
    assert.equal(page.canEdit.value, false);
    assert.match(page.readError.value, /deleted/);
  } finally { scope.stop(); }
});
