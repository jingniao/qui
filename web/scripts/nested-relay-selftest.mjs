import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/nestedRelayEntries.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { nestRelayEntries, ruleEntryAddress } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

const item = (id, inboundId) => ({ id, inboundId, sourceType: 'self_hosted' });
const exit = item('exit', 'in-exit');
const created = item('entry', 'in-entry');          // 添加中转 entry of scheme s1
const orphan = item('orphan', 'in-orphan');         // scheme s2 reaches no listed exit
const ruleEntry = item('rule', 'in-rule');          // linked tunnel rule entry
const manualRule = item('manual', 'in-manual');     // manual-target rule entry
const plain = item('plain', 'in-plain');
const imported = { id: 'imp', sourceType: 'manual' };
const inbounds = [
  { id: 'in-exit', routeSummary: { type: 'direct', path: [] } },
  { id: 'in-entry', routeSummary: { type: 'relay', bindingMode: 'create', schemeId: 's1', path: [] } },
  { id: 'in-orphan', routeSummary: { type: 'relay', bindingMode: 'create', schemeId: 's2', path: [] } },
  { id: 'in-rule', managedBy: { kind: 'rule', id: 'r1', name: 'r1' }, routeSummary: { type: 'tunnel', path: [] } },
  { id: 'in-manual', managedBy: { kind: 'rule', id: 'r2', name: 'r2' }, routeSummary: { type: 'tunnel', path: [] } },
  { id: 'in-plain', routeSummary: { type: 'direct', path: [] } },
];
// The exit lists s1; the s2 route is reported only under its own entry, which does not count.
const routes = { exit: [{ schemeId: 's1' }], orphan: [{ schemeId: 's2' }], imp: [] };
const result = nestRelayEntries([exit, created, orphan, ruleEntry, manualRule, plain], [imported], inbounds,
  node => routes[node.id] || [], id => id === 'r1');

assert.deepEqual([...result.hidden].sort(), ['entry', 'rule']);
const rule = { id: 'r1', entryNodeId: 'entry-server', listenPort: 1234 };
const entryServer = { id: 'entry-server', publicIp: '203.0.113.1' };
assert.deepEqual(ruleEntryAddress(rule, [{ ...ruleEntry, server: 'client.example', port: 4321 }], inbounds, [entryServer]), { server: 'client.example', port: 4321 });
assert.deepEqual(ruleEntryAddress(rule, [], inbounds, [entryServer]), { server: '203.0.113.1', port: 1234 });
assert.equal(ruleEntryAddress(rule, [], inbounds, []), undefined);
console.log('PASS rule entry address: hidden managed entry preferred, public address fallback, missing address omitted');
assert.deepEqual(result.entries.get('s1').map(n => n.id), ['entry']);
assert.equal(result.entries.has('s2'), false, 'an entry whose scheme no listed node shows keeps its row');

// An imported exit lists the scheme too.
const viaImport = nestRelayEntries([created], [imported], inbounds, node => node.id === 'imp' ? [{ schemeId: 's1' }] : [], () => false);
assert.deepEqual([...viaImport.hidden], ['entry']);

console.log('PASS nested relay entries: created entries fold under a listed exit, orphan/manual-rule entries keep rows, linked rule entries fold');

const hop = item('hop', 'in-hop');
const hopBounds = [...inbounds, { id: 'in-hop', managedBy: { kind: 'tunnel', id: 't1' } }];
assert.ok(nestRelayEntries([hop, manualRule], [], hopBounds, () => [], () => false).hidden.has('hop'));
console.log('PASS tunnel hop listeners hidden; manual-target rule entries retained');

const proxySource = readFileSync(new URL('../src/modules/proxy/proxyNodes.ts', import.meta.url), 'utf8');
const routeBlock = proxySource.slice(proxySource.indexOf('export function getNodeRelayRoutes'), proxySource.indexOf('/**\n * Creates or retrieves'));
const getNodeRelayRoutes = new Function(ts.transpileModule(routeBlock.replace('export ', '') + '\nreturn getNodeRelayRoutes;', { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText)();
const first = { ...exit, sourceId: 'server-exit' };
const second = { ...plain, sourceId: 'server-exit' };
const scheme = { id: 's1', name: 'server exit', executionLocation: 'server', published: true, nodes: [
  { instanceId: 'a', resourceType: 'server', resourceId: 'entry-server', role: 'entry', name: '入口' },
  { instanceId: 'b', resourceType: 'server', resourceId: 'server-exit', role: 'exit', name: '出口' },
], connections: [{ id: 'ab', sourceInstanceId: 'a', targetInstanceId: 'b' }] };
const routeFor = (node, candidates) => getNodeRelayRoutes(node, [scheme], candidates);
const nested = nestRelayEntries([hop, created, first, second], [], hopBounds, routeFor, () => false);
assert.equal(nested.routes.get(first.id).length, 1);
assert.equal(nested.routes.get(second.id).length, 0);
assert.equal(nested.routes.get(first.id)[0].pathDisplay, '入口 → 出口');
assert.equal(nested.routes.get(first.id)[0].statusDisplay, 'published');
assert.ok(nested.hidden.has(created.id));
const reversed = nestRelayEntries([second, first, created], [], inbounds, routeFor, () => false);
assert.equal(reversed.routes.get(second.id).length, 1);
assert.equal(reversed.routes.get(first.id).length, 0);
const missing = nestRelayEntries([created], [], inbounds, routeFor, () => false);
assert.equal(missing.hidden.has(created.id), false);
console.log('PASS server-exit scheme uses first listed server card exactly once; entry folds only with a listed exit');

const orphanHost = { ...orphan, sourceId: 'server-exit' };
const withOrphan = nestRelayEntries([orphanHost, first, created], [], inbounds, routeFor, () => false);
assert.equal(withOrphan.routes.get(orphanHost.id).length, 1);
assert.equal(withOrphan.routes.get(first.id).length, 0);
assert.equal(withOrphan.hidden.has(orphanHost.id), false);
assert.equal(withOrphan.hidden.has(created.id), true);
console.log('PASS retained orphan entry participates in server-card display order');

// 中转线路 starts collapsed on every card (Owner 2026-11-15); the count stays in the header.
{
  const page = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8');
  const fn = page.slice(page.indexOf('function isRelayCollapsed'), page.indexOf('function toggleRelayCollapse'));
  assert.match(fn, /return true;/);
  assert.doesNotMatch(fn, /routeCount === 0/);
  console.log('PASS relay routes start collapsed');
}

{
  const component = readFileSync(new URL('../src/modules/tunnels/components/NodeRelayList.vue', import.meta.url), 'utf8');
  const page = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8');
  const container = 'bg-[#F8F9FA] rounded-xl p-2.5 border border-black/[0.03] flex items-center justify-between gap-2 hover:bg-[#F0F2F5] transition-colors';
  assert.ok(component.includes(container) && page.includes(container));
  assert.match(component, /隧道转发/);
  assert.match(component, /v-if="variant === 'card' && isOwner"/);
  assert.match(component, /@click\.stop="router.push\('\/proxy\/tunnel-rules'\)"/);
  assert.match(component, /publishStatuses\[rule.status\].label/);
  assert.match(component, /RelayRouteEntries/);
  assert.match(page, /variant="card"/);
  assert.match(page, /variant="table"/);
  assert.match(page, /ruleEntryAddress\(rule, selfHostedNodes.value, serverInbounds.value, servers.value\)/);
  console.log('PASS linked rule mini-card: scheme container, tunnel chip, shared status labels, entry copy, Owner-only edit, card/table wiring');
}
