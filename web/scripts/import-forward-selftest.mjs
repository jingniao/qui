import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';
import { computed, ref } from 'vue';

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
assert.match(read('../src/modules/proxy/pages/ProxyNodeListPage.vue'), /:initial-exit-import-id="quickRelayImportId"/);

const compile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const newApiClient = new Function('fetch', compile(read('../src/app/apiClient.ts').replace(/^export /gm, '')) +
  '\nreturn { request, APIError };');
const newActions = new Function('ref', 'computed', 'onUnmounted', 'useRouter', 'nodeRelayAPI', 'APIError',
  compile(actions.replace(/^import .*\r?\n/gm, '').replace(/^export /gm, '')) + '\nreturn useNodeRelayActions;');
const entryA = { id: 'entry-a', name: '入口 A' };
const entryB = { id: 'entry-b', name: '入口 B' };
const selfHosted = { sourceType: 'self_hosted', sourceId: entryA.id, inboundId: 'inbound-a' };
const imported = { id: 'imported', sourceType: 'subscription', sourceId: 'source-id' };

function relay(id, target, entryNodeId = entryA.id) {
  return { id, name: id, entryNodeId, entryNodeName: entryNodeId, target, listenPort: 32000, portMode: 'manual', network: 'both',
    tunnelId: null, tunnelName: null, ownerNodeId: null, subscriptionVisible: true, enabled: true, status: 'draft',
    createdAt: '2026-11-19T00:00:00Z', updatedAt: '2026-11-19T00:00:00Z', publishedAt: null };
}
function linkedRule(id, inboundId = selfHosted.inboundId) {
  return relay(id, { mode: 'linked', inboundId, nodeId: entryA.id, nodeName: entryA.name, inboundName: inboundId, protocol: 'vless', host: '192.0.2.10', port: 443 });
}
function importRule(id, importId = imported.id) {
  return relay(id, { mode: 'import', importId, nodeName: importId, protocol: 'vless', host: 'origin.example', port: 443 });
}
function relayHarness(fetch, entryServers = [entryA]) {
  const { request, APIError } = newApiClient(fetch);
  const listRules = ids => request('/nodes/relay-rules?' + new URLSearchParams({ nodeIds: ids.join(',') }));
  const servers = ref(entryServers);
  const useActions = newActions(ref, computed, () => {}, () => ({}), { listRules }, APIError);
  return { ...useActions(ref(true), servers, ref([])), servers };
}

await test('server cards only include rules linked to their inbound', async () => {
  const wanted = linkedRule('exact-inbound');
  const state = relayHarness(async () => Response.json({ items: [{ nodeId: entryA.id, rules: [
    wanted, linkedRule('other-inbound', 'inbound-b'), importRule('import'), relay('manual', { mode: 'manual', host: 'origin.example', port: 443 }),
  ] }] }));
  await state.loadNodeRelays();
  assert.deepEqual(state.linkedRelays(selfHosted), [wanted]);
  assert.deepEqual(state.linkedRelays(imported).map(rule => rule.id), ['import']);
});

await test('refresh sends a fresh read after a same-URL request settles', async () => {
  const requests = [];
  const state = relayHarness(path => new Promise(resolve => requests.push({ path, resolve })));
  const settle = () => new Promise(setImmediate);
  const first = state.loadNodeRelays();
  await settle();
  // A new administrator change requests the same server selection while the
  // first response (already read against the old draft) is still pending.
  const second = state.loadNodeRelays();
  await settle();
  assert.equal(requests.length, 1);
  requests[0].resolve(Response.json({ items: [{ nodeId: entryA.id, rules: [linkedRule('old-draft')] }] }));
  await first;
  await settle();
  assert.equal(requests.length, 2, 'send a fresh request after the API client releases the shared transport');
  assert.equal(requests[1].path, requests[0].path);
  assert.deepEqual(state.linkedRelays(selfHosted), [], 'the invalidated batch never reaches the cards');
  const fresh = linkedRule('new-draft');
  requests[1].resolve(Response.json({ items: [{ nodeId: entryA.id, rules: [fresh] }] }));
  await second;
  assert.deepEqual(state.linkedRelays(selfHosted), [fresh]);
});

await test('import cards report incomplete aggregation when any entry read fails', async () => {
  let failed = true;
  const state = relayHarness(async () => Response.json({ items: [
    { nodeId: entryA.id, rules: [importRule('other-import', 'other')] },
    failed ? { nodeId: entryB.id, rules: [], error: { code: 'internal_error', message: '读取失败' } } : { nodeId: entryB.id, rules: [] },
  ] }), [entryA, entryB]);
  await state.loadNodeRelays();
  assert.deepEqual(state.linkedRelays(imported), []);
  assert.match(state.relayError?.(imported) ?? '', /不完整.*入口 B.*读取失败/);
  assert.equal(state.relayListReady(imported), false, 'an incomplete import aggregate is not a confirmed empty result');
  assert.equal(state.relayError(selfHosted), undefined, 'a different entry failure does not affect this server card');
  failed = false;
  await state.loadNodeRelays();
  assert.equal(state.relayError(imported), undefined);
  assert.equal(state.relayListReady(imported), true);
});

const editor = read('../src/modules/tunnels/components/RuleEditor.vue');
assert.match(editor, /value="import">导入节点<\/option>/, 'the rule editor offers imported nodes');
assert.match(editor, /\{ mode: 'import', importId: importId\.value \}/);
assert.match(editor, /targetMode !== 'manual'/, 'import rules can be visible to users');

const deletion = read('../src/modules/proxy/components/ImportedProxyDialog.vue');
assert.match(deletion, /import_in_use_by_rule/);
assert.match(deletion, /preview\.rules\?\.length/);

assert.match(read('../src/modules/proxy/pages/LineGroupPage.vue'), /if \(rule\.target\.mode === 'manual'\) continue;/, 'import rules are group members');

console.log('PASS import forward: group membership, deletion guard, target labels, quick dialog, rule editor, node list and groups');
