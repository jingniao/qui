import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const path = new URL('../src/modules/proxy/importIdentity.ts', import.meta.url);
let source = '';
try { source = readFileSync(path, 'utf8'); } catch (error) { if (error.code !== 'ENOENT') throw error; }
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 } }).outputText;
const exports = {};
new Function('exports', compiled)(exports);
assert.equal(typeof exports.importProxyKey, 'function', 'content member key helper exists');
const { importProxyKey, resolveImportMember, importDeletionState } = exports;
const sources = [{ id: 'paste', kind: 'content', name: '粘贴' }, { id: 'airport', kind: 'url', name: '机场' }];
const content = { id: 'stable', sourceId: 'paste', kind: 'content', name: '旧名称', protocol: 'vless', proxyKey: 'id:stable' };
const url = { id: 'hash', sourceId: 'airport', kind: 'url', name: 'id:机场', protocol: 'trojan', proxyKey: 'id:机场|trojan' };
assert.equal(importProxyKey(content), 'id:stable');
assert.equal(importProxyKey(url), 'id:机场|trojan');
const member = { sourceId: 'paste', proxyKey: 'id:stable' };
assert.equal(resolveImportMember(member, sources, [content, url]).name, '旧名称');
assert.equal(resolveImportMember(member, sources, [{ ...content, name: '新名称' }, url]).name, '新名称');
assert.equal(resolveImportMember(member, sources, []).invalid, true);
assert.equal(resolveImportMember(member, sources, []).name, '已失效：id:stable');
assert.equal(resolveImportMember({ sourceId: 'paste', proxyKey: '旧名称|vless' }, sources, [content]).invalid, true);
assert.equal(resolveImportMember({ sourceId: 'paste', proxyKey: '' }, sources, [content]).invalid, true);
assert.equal(resolveImportMember({ sourceId: 'airport', proxyKey: '' }, sources, [url]).name, '机场（整个来源）');
assert.equal(resolveImportMember({ sourceId: 'airport', proxyKey: url.proxyKey }, sources, [url]).name, url.name);
assert.equal(resolveImportMember({ sourceId: 'missing', proxyKey: '' }, sources, []).invalid, true);
const empty = { groups: [], clientPolicies: [], canvases: [] };
assert.deepEqual(importDeletionState(empty, false), { needsRemoval: false, canConfirm: true });
for (const field of ['groups', 'clientPolicies']) {
  const referenced = { ...empty, [field]: [{}] };
  assert.deepEqual(importDeletionState(referenced, false), { needsRemoval: true, canConfirm: false });
  assert.deepEqual(importDeletionState(referenced, true), { needsRemoval: true, canConfirm: true });
}
for (const checked of [false, true]) {
  assert.equal(importDeletionState({ ...empty, canvases: [{}] }, checked).canConfirm, false);
  assert.equal(importDeletionState({ ...empty, groups: [{}], canvases: [{}] }, checked).canConfirm, false);
}
const policy = readFileSync(new URL('../src/modules/routing/pages/ClientPolicyPage.vue', import.meta.url), 'utf8');
const policyScript = policy.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const policyAST = ts.createSourceFile('policy.ts', policyScript, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const display = policyAST.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'endpointDisplayName');
assert.ok(display, 'client policy uses one endpoint name mapping');
const displayJS = ts.transpileModule(display.getText(policyAST), { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const candidates = { value: { items: [{ ref: 'import:paste:id:stable', name: '旧名称' }] } };
const endpointDisplayName = new Function('candidates', displayJS + '; return endpointDisplayName;')(candidates);
assert.equal(endpointDisplayName('@endpoint:import:paste:id:stable'), '旧名称');
candidates.value.items[0].name = '新名称';
assert.equal(endpointDisplayName('@endpoint:import:paste:id:stable'), '新名称');
assert.equal(candidates.value.items[0].ref, 'import:paste:id:stable');
console.log('PASS import identity: content stable IDs, URL source/key semantics, renamed and invalid members, deletion confirmation conditions');
