import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const apiSource = readFileSync(new URL('../src/modules/routing/api.ts', import.meta.url), 'utf8');
const apiCode = ts.transpileModule(apiSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText
  .replace(/import .* from ['"]\.\.\/\.\.\/app\/apiClient['"];?/, 'const request = () => { throw new Error("unexpected request"); };');
const api = await import(`data:text/javascript;base64,${Buffer.from(apiCode).toString('base64')}`);
const template = { client: {}, dns: {}, groups: [{ name: '组', type: 'select', members: ['@all'], testUrl: '', intervalSeconds: 0, excludeEndpoints: ['@endpoint:inbound:a'] }], providers: [], rules: [] };
const config = api.toConfig({ template, revision: 1, updatedAt: '' });
assert.deepEqual(config.proxyGroups[0].excludeEndpoints, ['@endpoint:inbound:a']);
assert.deepEqual(api.fromConfig(config).groups[0].excludeEndpoints, ['@endpoint:inbound:a']);
config.proxyGroups[0].excludeEndpoints = [];
assert.equal('excludeEndpoints' in api.fromConfig(config).groups[0], false);
console.log('PASS exclude endpoints: config round trip and empty omission');

const page = readFileSync(new URL('../src/modules/routing/pages/ClientPolicyPage.vue', import.meta.url), 'utf8');
const script = page.slice(page.indexOf('<script setup lang="ts">') + '<script setup lang="ts">'.length, page.indexOf('</script>'));
const ast = ts.createSourceFile('page.ts', script, ts.ScriptTarget.Latest, true);
const names = ['canExcludeEndpoint', 'excludeGroupEndpoint', 'restoreGroupEndpoint'];
const functions = names.map(name => ast.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === name).getText(ast)).join('\n');
const code = ts.transpileModule(functions, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
let dirty = 0;
const run = new Function('getGroupMembers', 'markDirty', code + '\nreturn {' + names.join(',') + '};')(group => api.parseGroupMembers(group.proxies), () => dirty++);
const group = { proxies: '@all', excludeEndpoints: [] };
const member = { kind: 'proxy', name: '节点', ref: 'inbound:a' };
assert.equal(run.canExcludeEndpoint(group, member), true);
assert.equal(run.canExcludeEndpoint({ proxies: '@all,@endpoint:inbound:a' }, member), false);
assert.equal(run.canExcludeEndpoint(group, { kind: 'builtin', name: 'DIRECT' }), false);
assert.equal(run.canExcludeEndpoint(group, { kind: 'proxy', name: '链克隆' }), false);
run.excludeGroupEndpoint(group, member);
assert.deepEqual(group.excludeEndpoints, ['@endpoint:inbound:a']);
assert.equal(run.canExcludeEndpoint(group, member), false);
run.restoreGroupEndpoint(group, '@endpoint:inbound:a');
assert.equal('excludeEndpoints' in group, false);
assert.equal(dirty, 2);
console.log('PASS exclude endpoints: dynamic eligibility, explicit protection, draft exclusion and restoration');
