import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { parse, compileTemplate } from '@vue/compiler-sfc';
import { baseParse } from '@vue/compiler-dom';

const source = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const url = code => `data:text/javascript;base64,${Buffer.from(ts.transpileModule(code, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText).toString('base64')}`;
const { deletionGroups, pendingCleanupMessage } = await import(url(source('app/deletion.ts')));
const preview = { schemes: [{ id: 's', name: '方案' }], tunnels: [{ id: 't', name: '隧道' }], rules: [{ id: 'r', name: '规则' }], inbounds: [{ id: 'i', name: '入口', nodeName: '服务器' }], blockers: [] };
assert.deepEqual(deletionGroups(preview).map(group => group.label), ['中转方案', '隧道', '隧道规则', '入站']);
assert.equal(deletionGroups(preview)[3].items[0].name, '服务器 · 入口');
assert.deepEqual(deletionGroups({ ...preview, schemes: [], tunnels: [], rules: [], inbounds: [] }), []);
assert.equal(pendingCleanupMessage(undefined), '');
assert.equal(pendingCleanupMessage([]), '');
assert.match(pendingCleanupMessage([{ name: '甲' }, { name: '乙' }]), /以下节点离线或应用失败，将在恢复后自动清理：甲、乙/);
const dialog = source('modules/proxy/components/InboundDeleteDialog.vue');
assert.match(dialog, /inboundDeletionPreview\(inbound.id\)/);
assert.match(dialog, /v-if="preview && !preview.blockers.length"/);
assert.match(dialog, /pendingCleanupMessage\(result\?\.pendingNodes\)/);
assert.match(source('components/DeletionDetails.vue'), /将同时删除：/);
assert.match(source('components/DeletionDetails.vue'), /以下引用需先处理：/);
assert.doesNotMatch(source('app/apiClient.ts'), /inbound_in_canvas_scheme|inbound_detached_entry/);
const script = parse(dialog).descriptor.scriptSetup.content;
const tree = ts.createSourceFile('InboundDeleteDialog.ts', script, ts.ScriptTarget.Latest, true);
const functions = tree.statements.filter(ts.isFunctionDeclaration).map(node => node.getText(tree)).join('\n');
const js = ts.transpileModule(functions, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const state = Object.fromEntries(['open', 'loading', 'deleting', 'error', 'target', 'preview'].map(key => [key, { value: null }]));
const calls = [], toasts = [], events = [];
const api = {
  inboundDeletionPreview: async id => { calls.push(['preview', id]); return preview; },
  deleteInbound: async id => { calls.push(['delete', id]); return { pendingNodes: [{ nodeId: 'n', name: '待清理节点', reason: 'offline' }] }; },
};
class APIError extends Error {}
const run = new Function('state', 'proxyAPI', 'showToast', 'emit', 'pendingCleanupMessage', 'APIError', `const {open,loading,deleting,error,target,preview}=state;let generation=0;${js};return {prompt,remove};`)(state, api, value => toasts.push(value), event => events.push(event), pendingCleanupMessage, APIError);
await run.prompt({ id: 'i', name: '入站' });
assert.deepEqual(calls, [['preview', 'i']], 'preview must run before any destructive request');
state.preview.value = { ...preview, blockers: [{ kind: 'outbound', id: 'o', name: '手工出站' }] };
await run.remove();
assert.equal(calls.length, 1, 'blocked previews never delete');
state.preview.value = preview;
await run.remove();
assert.deepEqual(calls[1], ['delete', 'i']);
assert.equal(state.open.value, false);
assert.deepEqual(events, ['changed']);
assert.match(toasts[0], /入站已删除.*恢复后自动清理：待清理节点/);
api.inboundDeletionPreview = async () => { throw new Error('预览失败'); };
await run.prompt({ id: 'other', name: '另一个入站' });
assert.equal(state.preview.value, undefined);
assert.equal(state.error.value, '预览失败');

for (const path of [
  'components/DeletionDetails.vue', 'modules/proxy/components/InboundDeleteDialog.vue',
  'modules/proxy/pages/InboundListPage.vue', 'modules/proxy/pages/ProxyNodeListPage.vue',
  'modules/proxy/pages/RelayCanvasPage.vue', 'modules/proxy/components/EntryRouteSummary.vue',
  'modules/proxy/components/relay/CanvasEntryEditor.vue', 'modules/proxy/components/relay/CanvasRemoveDialog.vue',
  'modules/proxy/components/relay/TunnelPlanSummary.vue',
]) {
  const { descriptor, errors } = parse(source(path));
  assert.deepEqual(errors, [], path);
  assert.deepEqual(compileTemplate({ source: descriptor.template.content, filename: path, id: 'deletion-check' }).errors, [], path);
  const visit = node => {
    if (node.type === 1 && node.tag === 'template') assert.ok(node.props.length, `${path}: bare nested template prevents rendering`);
    for (const child of node.children || []) visit(child);
  };
  visit(baseParse(descriptor.template.content));
}
const { ruleTargetLabel } = await import(url(source('modules/tunnels/model.ts')));
assert.equal(ruleTargetLabel({ mode: 'linked', inboundId: 'gone', host: '', port: 0 }), '目标已删除 · —');
assert.equal(ruleTargetLabel({ mode: 'linked', nodeName: '节点', inboundName: '入口', protocol: 'socks5', host: '1.2.3.4', port: 1080 }), '节点 · 入口 · SOCKS5 · 1.2.3.4:1080');
const page = source('modules/tunnels/pages/TunnelsPage.vue');
assert.match(page, /ruleTargetLabel\(r.target\)/);
assert.doesNotMatch(page, /r.target.protocol.toUpperCase\(\)/);
console.log('relay cleanup selftest passed');
