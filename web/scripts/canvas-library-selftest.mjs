// Self-test for the relay canvas resource library (walkthrough fix B).
//
// Everything here is source text plus the real helpers transpiled on the fly, so
// a regression in any of these four areas fails without a browser:
//   1. self-hosted node transports come from the inbound (listenNetworks), not
//      from a protocol guess;
//   2. the canvas "我的自建节点" library is built from the panel's inbounds and a
//      dropped item still resolves to an `inbound:` reference;
//   3. the empty state covers the workspace and hides the scheme inspector;
//   4. an import source's derived kind decides 机场订阅 vs 单独导入.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const proxyRoot = new URL('../src/modules/proxy/', import.meta.url);
const read = name => readFileSync(new URL(name, proxyRoot), 'utf8');
const transpile = (source, transform = value => value) =>
  ts.transpileModule(transform(source), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
  }).outputText;
const load = code => import('data:text/javascript;base64,' + Buffer.from(code).toString('base64'));

// ---------------------------------------------------------------- 1. transports
// The server detail page and the node list must agree on one inbound's
// transports; both now ask nodeEditorModel.listenNetworks.
const nodeEditor = await load(transpile(read('components/node-editor/nodeEditorModel.ts'), source =>
  source.replace(
    /import \{[^}]*\} from '\.\.\/\.\.\/\.\.\/nodes\/coreSummary';/,
    'const blockedCores = () => []; const findCore = () => undefined; const multiCoreSupported = () => false; const nodeCores = () => []; const primaryCoreName = () => "";'
  )));
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'vless', settings: {} }), ['tcp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'trojan', settings: {} }), ['tcp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'shadowsocks', settings: {} }), ['tcp', 'udp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'socks5', settings: {} }), ['tcp', 'udp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'hysteria2', settings: {} }), ['udp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'wireguard', settings: {} }), ['udp']);
assert.deepEqual(nodeEditor.listenNetworks({ protocol: 'forward', settings: { forward: { host: 'h', port: 443, network: 'tcp,udp' } } }), ['tcp', 'udp']);

const listPage = read('pages/ProxyNodeListPage.vue');
const selfHostedBlock = listPage.slice(listPage.indexOf('const selfHostedNodes = computed'));
const selfHostedItems = selfHostedBlock.slice(0, selfHostedBlock.indexOf('// All aggregated nodes'));
// Self-hosted items read the inbound; the protocol guess is gone from the page.
assert.match(selfHostedItems, /listenNetworks\(ib\)/);
assert.match(selfHostedItems, /supportTcp: networks\.includes\('tcp'\)/);
assert.match(selfHostedItems, /supportUdp: networks\.includes\('udp'\)/);
assert.doesNotMatch(selfHostedItems, /getTcpUdpSupport/);
assert.doesNotMatch(selfHostedItems, /protocol\s*===\s*'hysteria2'/);
assert.equal(listPage.includes('getTcpUdpSupport'), false);
// Only imported proxies — which have no inbound record — keep a protocol guess.
const importApiSource = read('importApi.ts');
assert.match(importApiSource, /guessProtocolNetworks\(p\.protocol\)/);
const proxyNodesSource = read('proxyNodes.ts');
const guess = proxyNodesSource.slice(proxyNodesSource.indexOf('export function guessProtocolNetworks'));
assert.match(guess.slice(0, guess.indexOf('// Simple protocol parser')), /supportUdp: false/);


// ------------------------------------------------------------- 2. canvas library
// api.ts is transpiled with its two runtime imports stubbed so the real
// canvasInstanceRef / selfHostedResourceId are the ones under test.
const api = await load(transpile(read('api.ts'), source => source
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;')
  .replace("import { request, type Page } from '../../app/apiClient';", 'const request = async () => ({});')));
globalThis.__canvasApi = api;
const library = await load(transpile(
  read('components/relay/canvasLibrary.ts')
    .replace(/import\s*\{\s*selfHostedResourceId\s*\}\s*from\s*'\.\.\/\.\.\/api';/, 'const { selfHostedResourceId } = globalThis.__canvasApi;')
));

const servers = [
  { id: 'srv-a', name: 'HK-01', publicIp: '203.0.113.9', status: 'online', enabled: true },
  { id: 'srv-b', name: 'JP-01', hostname: 'jp.example.com', status: 'degraded', enabled: true },
  { id: 'srv-off', name: 'OLD-01', publicIp: '198.51.100.1', status: 'online', enabled: false },
  { id: 'srv-gone', name: 'GONE-01', publicIp: '198.51.100.2', status: 'online', enabled: true, deletedAt: '2026-10-01T00:00:00Z' }
];
const inbounds = [
  { id: 'ib-1', nodeId: 'srv-a', name: 'HK-01 vless', protocol: 'vless', port: 443, enabled: true, internalRelay: false },
  { id: 'ib-2', nodeId: 'srv-a', name: 'SS', protocol: 'shadowsocks', port: 8388, enabled: false, internalRelay: false },
  { id: 'ib-3', nodeId: 'srv-b', name: 'relay', protocol: 'trojan', port: 8443, enabled: true, internalRelay: true },
  { id: 'ib-4', nodeId: 'srv-off', name: 'disabled server', protocol: 'vless', port: 444, enabled: true, internalRelay: false },
  { id: 'ib-5', nodeId: 'srv-gone', name: 'deleted server', protocol: 'vless', port: 445, enabled: true, internalRelay: false }
];
const items = library.selfHostedLibraryItems(servers, inbounds);
// Disabled and deleted servers contribute nothing; disabled inbounds stay listed.
assert.deepEqual(items.map(item => item.inboundId), ['ib-1', 'ib-2', 'ib-3']);
assert.deepEqual(items.map(item => item.name), ['HK-01 · vless', 'HK-01 · SS (已停用)', 'JP-01 · relay (内部中转)']);
assert.deepEqual(items.map(item => item.server), ['203.0.113.9', '203.0.113.9', 'jp.example.com']);
assert.deepEqual(items.map(item => item.status), ['online', 'offline', 'online']);
assert.deepEqual(items.map(item => item.enabled), [true, false, true]);
assert.deepEqual(items.map(item => item.port), [443, 8388, 8443]);
assert.deepEqual(items.map(item => item.internalRelay), [false, false, true]);
assert.equal(items[0].id, 'self-srv-a-ib-1');

// A dropped item must save as `inbound:<id>` — the reference the plan, the
// publisher and validateRefs resolve — never as an import or a server.
for (const item of items) {
  assert.equal(api.canvasInstanceRef({ resourceType: 'node', resourceId: item.id }, { servers, inbounds, imported: [] }), 'inbound:' + item.inboundId);
}
assert.equal(api.canvasInstanceRef({ resourceType: 'server', resourceId: 'srv-a' }, { servers, inbounds, imported: [] }), 'server:srv-a');
assert.equal(api.canvasInstanceRef({ resourceType: 'node', resourceId: 'manual-1' }, { servers, inbounds, imported: [] }), 'import:manual-1');

// The search box filters the same way the imported groups do.
assert.deepEqual(library.selfHostedLibraryItems(servers, inbounds, 'shadowsocks').map(item => item.inboundId), ['ib-2']);
assert.deepEqual(library.selfHostedLibraryItems(servers, inbounds, '8388').map(item => item.inboundId), ['ib-2']);
assert.deepEqual(library.selfHostedLibraryItems(servers, inbounds, 'JP-01').map(item => item.inboundId), ['ib-3']);
assert.deepEqual(library.selfHostedLibraryItems(servers, inbounds, 'nothing-here'), []);
assert.deepEqual(library.selfHostedLibraryItems([], inbounds), []);

// The page must use that helper and keep the drop bound to the inbound.
const canvas = read('pages/RelayCanvasPage.vue');
assert.ok(canvas.includes('selfHostedLibraryItems(servers.value, inbounds.value, resourceSearch.value)'));
assert.equal(canvas.includes(".filter(n => n.sourceType === 'self_hosted')"), false);
assert.ok(canvas.includes('entryBound: true'));
assert.ok(canvas.includes('const entryBound = draggedPayload.entryBound === true && currentScheme.value.executionLocation === \'server\';'));
assert.ok(canvas.includes(':draggable="node.enabled"'));
assert.ok(canvas.includes("role = entryBound || currentScheme.value.nodes.length === 0 ? 'entry' : 'relay'"));
// An inbound-bound box is not a hop in a server scheme: connecting it would be
// refused on publish. A client chain is a path of inbound boxes and must stay
// connectable, which is why the guard checks the execution location.
assert.ok(canvas.includes('function isInboundBoundInstance('));
assert.ok(canvas.includes("if (currentScheme.value.executionLocation !== 'server') return false;"));
assert.ok(canvas.includes('inboundEntryHint()'));
assert.ok(canvas.includes('selfHostedResourceId(inbound) === node.resourceId'));
// A drop can never be saved as an import reference: the inbound must be loaded.
assert.ok(canvas.includes('该自建入站尚未载入，请刷新后重试'));

// ------------------------------------------------------------------ 3. empty state
// `bg-ios-background` is not a defined Tailwind colour (nothing was painted), and
// the inspector used to show 方案名称/删除此方案 for a scheme that does not exist.
assert.equal(canvas.includes('bg-ios-background'), false);
assert.ok(canvas.includes('v-if="!currentScheme.id"'));
assert.ok(canvas.includes('absolute inset-0 z-40 bg-[#F5F5F7]'));
assert.ok(canvas.includes('v-else-if="!currentScheme.id"'));
assert.match(canvas, /<div v-if="currentScheme\.id" class="pt-2">\s*<button\s+type="button"\s+@click="confirmDeleteScheme = true"/);

// --------------------------------------------------------------- 4. source kinds
// proxyNodes.ts (store + guess) and importApi.ts are transpiled with their
// runtime imports stubbed; loadImports then runs against fake panel responses.
const proxyNodes = await load(transpile(read('proxyNodes.ts'), source => source
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;')
  .replace(/import \{[^}]*\} from '\.\/api';/, 'const canvasAPI = {}; const canvasSchemeToRelayScheme = () => ({}); const choices = async () => []; const relaySchemeToCanvasInput = () => ({});')));
globalThis.__proxyNodes = proxyNodes;
const importApi = await load(transpile(importApiSource, source => source
  .replace("import { request } from '../../app/apiClient';", 'const request = async () => ({});')
  .replace(/import \{[^}]*\} from '\.\/proxyNodes';/, 'const { guessProtocolNetworks, proxyNodeStore } = globalThis.__proxyNodes;')));

assert.equal(importApi.proxySourceType('content'), 'manual');
assert.equal(importApi.proxySourceType('url'), 'subscription');
assert.equal(importApi.importSourceKind({ kind: 'content' }), 'content');
assert.equal(importApi.importSourceKind({ kind: 'url' }), 'url');
assert.equal(importApi.importSourceKind({ kind: 'something-new' }), 'url');
assert.equal(importApi.importSourceKind(undefined), 'url');
assert.deepEqual(importApi.airportSources([{ kind: 'url' }, { kind: 'content' }, {}]).length, 2);
assert.deepEqual(importApi.airportSources([{ kind: 'content' }]), []);
// The protocol guess is only for imported proxies, whose transports the panel
// never learns; it is deliberately narrower than the old every-protocol guess.
assert.equal(proxyNodes.guessProtocolNetworks('vless').supportTcp, true);
assert.equal(proxyNodes.guessProtocolNetworks('vless').supportUdp, false);
assert.equal(proxyNodes.guessProtocolNetworks('shadowsocks').supportUdp, true);
assert.equal(proxyNodes.guessProtocolNetworks('socks5').supportUdp, true);
assert.equal(proxyNodes.guessProtocolNetworks('hysteria2').supportTcp, false);
assert.equal(proxyNodes.guessProtocolNetworks('tuic').supportUdp, true);
assert.equal(proxyNodes.guessProtocolNetworks('http').supportUdp, false);

importApi.importAPI.list = async () => ({
  items: [
    { id: 'sub-1', name: '机场订阅', kind: 'url', intervalSeconds: 3600, lastFetchAt: '2026-10-29T00:00:00Z', lastStatus: 'ok', lastError: '', userIds: [] },
    { id: 'src-2', name: '手动导入', kind: 'content', intervalSeconds: 3600, lastFetchAt: '2026-10-29T00:00:00Z', lastStatus: 'ok', lastError: '', userIds: [] }
  ]
});
importApi.importAPI.proxies = async () => ({
  items: [
    { id: 'p1', sourceId: 'sub-1', proxyKey: 'A|vless', name: 'A', protocol: 'vless', server: 'a.example.com', port: 443 },
    { id: 'p2', sourceId: 'src-2', proxyKey: 'B|shadowsocks', name: 'B', protocol: 'shadowsocks', server: 'b.example.com', port: 8388 }
  ]
});
await importApi.loadImports();
const store = proxyNodes.proxyNodeStore;
// The import group split: an airport proxy stays a subscription node, a pasted
// content import becomes a 单独导入 (manual) node.
assert.deepEqual(store.manualNodes.map(node => node.sourceType), ['subscription', 'manual']);
assert.deepEqual(store.manualNodes.map(node => node.id), ['p1', 'p2']);
assert.deepEqual(store.manualNodes.map(node => [node.supportTcp, node.supportUdp]), [[true, false], [true, true]]);
assert.deepEqual(store.subscriptions.map(sub => sub.kind), ['url', 'content']);
assert.equal(importApi.airportSources(store.subscriptions).length, 1);
// The page's airport count/list comes from that filtered list, and the manual
// sources stay manageable through the same source list.
assert.ok(listPage.includes('机场订阅管理 ({{ airportSubscriptions.length }})'));
assert.ok(listPage.includes('const airportSubscriptions = computed(() => airportSources(proxyNodeStore.subscriptions));'));
assert.ok(listPage.includes("const manualSources = computed(() => proxyNodeStore.subscriptions.filter(s => importSourceKind(s) === 'content'));"));
assert.ok(listPage.includes('已添加的导入来源（{{ proxyNodeStore.subscriptions.length }}）'));
assert.ok(listPage.includes("v-if=\"sub.kind !== 'content'\""));
assert.ok(listPage.includes('@click="deleteSubscription(sub.id)"'));
console.log('All canvas-library self-tests passed');

assert.ok(canvas.includes('selfHostedResourceId(inbound) === node.resourceId'));
