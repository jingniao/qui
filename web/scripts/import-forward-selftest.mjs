import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// v1.6.0 plan 2.1: pure forwarding to imported nodes (tunnel rules with an
// import target) in the quick dialog, the rule editor, groups and deletion.
async function load(relative) {
  const url = new URL(relative, import.meta.url);
  const code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
  }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const read = relative => readFileSync(new URL(relative, import.meta.url), 'utf8');

const { groupHasImport, importDeletionState } = await load('../src/modules/proxy/importIdentity.ts');
const content = { sourceId: 'paste', proxyKey: 'id:node-1' };
const fetched = { sourceId: 'airport', proxyKey: 'HK|vless' };
assert.ok(groupHasImport({ imports: [{ sourceId: 'paste', proxyKey: 'id:node-1' }] }, content), 'exact content member');
assert.ok(groupHasImport({ imports: [{ sourceId: 'airport', proxyKey: '' }] }, fetched), 'a whole URL source contains its nodes');
assert.ok(groupHasImport({ imports: [{ sourceId: 'airport', proxyKey: 'HK|vless' }] }, fetched), 'exact URL member');
assert.ok(!groupHasImport({ imports: [{ sourceId: 'airport', proxyKey: 'JP|vless' }] }, fetched), 'another node of the source');
assert.ok(!groupHasImport({ imports: [{ sourceId: 'other', proxyKey: '' }] }, fetched), 'another source');

const empty = { groups: [], clientPolicies: [], canvases: [] };
assert.deepEqual(importDeletionState({ ...empty, rules: [] }, false), { needsRemoval: false, canConfirm: true });
assert.equal(importDeletionState({ ...empty, rules: [{ id: 'r', name: 'rule' }] }, true).canConfirm, false, 'a referencing rule blocks deletion');
assert.equal(importDeletionState(empty, false).canConfirm, true, 'previews without rules stay valid');

const { ruleTargetLabel } = await load('../src/modules/tunnels/model.ts');
assert.equal(ruleTargetLabel({ mode: 'import', importId: 'node-1', nodeName: '美国 家宽 v6', protocol: 'vless', host: '23.246.167.253', port: 36514 }), '导入节点 · 美国 家宽 v6 · VLESS · 23.246.167.253:36514');
assert.equal(ruleTargetLabel({ mode: 'import', nodeName: '目标已删除', host: '', port: 0 }), '导入节点 · 目标已删除');

const dialog = read('../src/modules/proxy/components/QuickRelayDialog.vue');
assert.match(dialog, /<option v-if="!exitImport \|\| convertible\(exitImport.id\)" value="proxy">/, 'the proxy mode needs a convertible import');
assert.match(dialog, /mode\.value === 'forward' \? importOptions\.value : importOptions\.value\.filter\(item => convertible\(item\.id\)\)/, 'the proxy mode offers only convertible imports');
assert.match(read('../src/modules/proxy/pages/RelayCanvasPage.vue'), /if \(server && importExitReasons\.value\?\.get\(n\.id\) !== ''\) return false;/, 'server canvases hide imports the panel cannot convert');
assert.match(dialog, /mode: 'import', importId: exitImport\.value\.id/, 'the rule targets the imported node');
assert.match(dialog, /groupHasImport\(group, exitImport\.value\)/, 'default groups are the ones containing the import');
assert.match(dialog, /!!exitImport\.value \|\| last\.nodeId === exit\.value\?\.nodeId/, 'any tunnel exit can dial an imported target');
assert.match(dialog, /跳跃端口不可用/, 'QUIC imports warn about hop ports');

const actions = read('../src/modules/proxy/useNodeRelayActions.ts');
assert.match(actions, /else quickRelayImportId\.value = node\.id;/, 'imported nodes open the quick dialog');
assert.doesNotMatch(actions, /else relayNode\.value = node;/, 'imported nodes no longer create an unpublishable server draft');
assert.match(actions, /r\.rule\.target\.mode === 'linked' && \(!node\.inboundId/, 'server cards list only linked rules');
assert.match(read('../src/modules/proxy/pages/ProxyNodeListPage.vue'), /:initial-exit-import-id="quickRelayImportId"/);

const editor = read('../src/modules/tunnels/components/RuleEditor.vue');
assert.match(editor, /value="import">导入节点<\/option>/, 'the rule editor offers imported nodes');
assert.match(editor, /\{ mode: 'import', importId: importId\.value \}/);
assert.match(editor, /targetMode !== 'manual'/, 'import rules can be visible to users');

const deletion = read('../src/modules/proxy/components/ImportedProxyDialog.vue');
assert.match(deletion, /import_in_use_by_rule/);
assert.match(deletion, /preview\.rules\?\.length/);

assert.match(read('../src/modules/proxy/pages/LineGroupPage.vue'), /if \(rule\.target\.mode === 'manual'\) continue;/, 'import rules are group members');

console.log('PASS import forward: group membership, deletion guard, target labels, quick dialog, rule editor, node list and groups');
