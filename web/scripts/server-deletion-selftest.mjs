import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { parse, compileTemplate } from '@vue/compiler-sfc';

const root = new URL('../src/modules/nodes/', import.meta.url);
const read = path => readFileSync(new URL(path, root), 'utf8');
const dialog = read('components/ServerDeleteDialog.vue');
const script = parse(dialog).descriptor.scriptSetup.content;
const tree = ts.createSourceFile('ServerDeleteDialog.ts', script, ts.ScriptTarget.Latest, true);
const functions = tree.statements.filter(node => ts.isFunctionDeclaration(node)).map(node => node.getText(tree)).join('\n');
const js = ts.transpileModule(functions, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const previewValue = { schemes: [{ id: 's', name: '方案' }], tunnels: [], rules: [], inbounds: [], blockers: [] };
const state = Object.fromEntries(['open', 'loading', 'deleting', 'error', 'target', 'preview'].map(key => [key, { value: null }]));
const calls = [], toasts = [], events = [];
const api = {
  deletionPreview: async id => { calls.push(['preview', id]); return previewValue; },
  deleteNode: async id => { calls.push(['delete', id]); return { pendingNodes: [{ nodeId: 'offline', name: '离线服务器', reason: 'offline' }] }; }
};
const run = new Function('state', 'nodeAPI', 'showToast', 'emit', 'pendingCleanupMessage', `const {open,loading,deleting,error,target,preview}=state;let generation=0;${js};return {prompt,remove};`)(state, api, text => toasts.push(text), name => events.push(name), nodes => nodes?.length ? `；待清理：${nodes.map(node => node.name).join('、')}` : '');
await run.prompt({ id: 'n', name: '服务器' });
assert.deepEqual(calls, [['preview', 'n']], 'opening confirmation only requests deletion preview');
assert.equal(state.open.value, true);
assert.deepEqual(state.preview.value, previewValue);
state.preview.value = { ...previewValue, blockers: [{ kind: 'policy', id: 'p', name: '手工策略' }] };
await run.remove();
assert.equal(calls.length, 1, 'blockers prevent deletion');
state.preview.value = previewValue;
await run.remove();
assert.deepEqual(calls[1], ['delete', 'n']);
assert.match(toasts[0], /服务器已删除.*离线服务器/);
assert.equal(state.open.value, false);
assert.deepEqual(events, ['changed']);
assert.match(dialog, /<DeletionDetails v-if="preview" :preview="preview"/);
assert.match(dialog, /v-if="preview && !preview.blockers.length/);
const details = parse(readFileSync(new URL('../src/components/DeletionDetails.vue', import.meta.url), 'utf8')).descriptor;
const detailTree = ts.createSourceFile('DeletionDetails.ts', details.scriptSetup.content, ts.ScriptTarget.Latest, true);
const labelFunction = detailTree.statements.find(node => ts.isFunctionDeclaration(node) && node.name.text === 'blockerLabel');
const labelJS = ts.transpileModule(labelFunction.getText(detailTree), { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const blockerLabel = new Function(`${labelJS};return blockerLabel;`)();
for (const [kind, label] of Object.entries({ inbound: '入站', outbound: '出站', outbound_group: '出口组', outbound_target: '其他服务器的出站', proxy_chain: '代理链', policy: '分流规则', deployment_batch: '进行中的批量发布', custom: 'custom' })) {
  assert.equal(blockerLabel(kind), label, `${kind} blocker category`);
}
assert.match(details.template.content, /\{\{ blockerLabel\(item.kind\) \}\} · \{\{ item.name \}\}/, 'blockers retain their category prefix');
api.deletionPreview = async () => { throw new Error('预览读取失败'); };
await run.prompt({ id: 'n' });
assert.equal(state.error.value, '预览读取失败');
assert.equal(state.preview.value, null, 'failed preview cannot leave a usable confirmation');

const badge = read('components/RelayCleanupBadge.vue');
assert.match(badge, /v-if="node.relayCleanupPending"/);
assert.match(badge, /中转撤除尚未在该节点生效，将自动重试/);
assert.match(badge, /最近失败：.*node.relayCleanupCode/);
assert.match(badge, /text-xs/);
for (const file of ['components/ServerCardGrid.vue', 'components/ServerInventory.vue', 'pages/NodeDetailPage.vue', 'components/detail/ServerOverview.vue']) {
  const content = read(file);
  assert.match(content, /<RelayCleanupBadge/);
  const { descriptor, errors } = parse(content);
  assert.deepEqual(errors, [], file);
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: file, id: 'server-deletion' }).errors, [], file);
}
console.log('PASS server deletion: preview before confirmation, blockers, pending cleanup toast, preview failure, cleanup badges and valid templates');
