// Self-test for the relay canvas resource library (walkthrough fix B).
//
// Everything here is source text plus the real helpers transpiled on the fly, so
// a regression in any of these four areas fails without a browser:
//   1. self-hosted node transports come from the inbound (listenNetworks), not
//      from a protocol guess;
//   2. the canvas "我的自建节点" library is built from the panel's inbounds and a
//      dropped item still resolves to an `inbound:` reference;
//   3. the empty state covers the workspace and hides the scheme inspector;
//   4. an import source's derived kind decides 机场订阅 vs 单独导入;
//   5. below xl the library and the inspector are overlay drawers and the studio
//      toolbar wraps instead of clipping (desktop geometry stays at xl, with a
//      truncated scheme name at exactly 1280 px).
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
assert.ok(canvas.includes('v-if="schemesLoading || schemesLoadError || !currentScheme.id"'));
assert.ok(canvas.includes('absolute inset-0 z-40 bg-[#F5F5F7]'));
assert.ok(canvas.includes('v-else-if="!currentScheme.id"'));
assert.match(canvas, /<div v-if="currentScheme\.id" class="pt-2">\s*<button\s+type="button"\s+@click="confirmDeleteScheme = true"/);

// Null/missing arrays from historical rows must not discard an entire page.
const schemeContext = { servers: [], inbounds: [], imported: [] };
for (const layout of [undefined, null, {}, { instances: null, connections: null }, { instances: [], connections: null }, { instances: null, connections: [] }]) {
  const mapped = api.canvasSchemeToRelayScheme({ id: 'old', name: 'old', execution: 'server', layout, publishedAt: '', updatedAt: '' }, schemeContext);
  assert.deepEqual(mapped.nodes, []);
  assert.deepEqual(mapped.connections, []);
}
const nullableMetadata = api.canvasSchemeToRelayScheme({
  id: 'populated', name: 'populated', execution: 'server', publishedAt: '', updatedAt: '',
  client_published_layout: null, published_layout: null, tunnels: null, ports: null,
  layout: { instances: [{ instanceId: 'a', ref: 'server:missing', x: 0, y: 0, role: 'entry', listenPort: null, targetPort: null }], connections: [] }
}, schemeContext);
assert.equal(nullableMetadata.id, 'populated');
assert.equal(nullableMetadata.nodes.length, 1);
assert.deepEqual(nullableMetadata.connections, []);
assert.match(proxyNodesSource, /canvasAPI\.create\(\{ name, execution: executionLocation, layout: \{ instances: \[\], connections: \[\] \}/);
assert.match(canvas, /v-else-if="schemesLoadError"[\s\S]*?@click="reloadSchemes">重试<\/button>[\s\S]*?<template v-else>[\s\S]*?暂无中转方案/);
assert.match(canvas, /async function createNewScheme\(\) \{\s*if \(schemesLoading\.value \|\| schemesLoadError\.value\) return;/);
// Exercise the real reload function: failure persists until a successful retry,
// including when an older populated store is still present behind the overlay.
const reloadSource = canvas.slice(canvas.indexOf('async function reloadSchemes()'), canvas.indexOf('// An earlier build kept schemes in this browser. Offer them once;'));
const reloadHarness = await load(transpile(`
  export const schemesLoading = { value: true }, schemesLoadError = { value: '' };
  export let fail = true;
  export function recover() { fail = false; }
  const canvasContext = () => ({}), showToast = () => {};
  async function loadCanvasSchemes() { if (fail) throw new Error('network down'); }
  export ${reloadSource}
`));
await reloadHarness.reloadSchemes();
assert.equal(reloadHarness.schemesLoading.value, false);
assert.match(reloadHarness.schemesLoadError.value, /network down/);
reloadHarness.recover();
await reloadHarness.reloadSchemes();
assert.equal(reloadHarness.schemesLoading.value, false);
assert.equal(reloadHarness.schemesLoadError.value, '');

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
// ------------------------------------------------- 5. narrow-screen studio layout
// /proxy/relay is a desktop studio, but the shell sidebar takes 272 px: at a
// 1120 px viewport the studio is only ~850 px, so the toolbar overlapped itself
// (scheme capsule over 校验链路, the execution-mode control pushed onto the library
// header) and both docked panels left the canvas with nothing. Below xl (1280 px)
// both panels are overlay drawers — closed by default, one at a time — and the
// toolbar wraps; from xl up it is one row again with a truncated scheme name.
const header = canvas.slice(canvas.indexOf('<header'), canvas.indexOf('</header>'));
const asideOf = label => {
  const at = canvas.indexOf(`aria-label="${label}"`);
  assert.ok(at > 0, `the ${label} aside is missing`);
  return canvas.slice(canvas.lastIndexOf('<aside', at), canvas.indexOf('</aside>', at));
};

// The one-shot 56 px row that clipped the actions is gone; xl keeps exactly h-14,
// so below xl the bar grows with its rows instead of spilling onto the library.
assert.equal(header.includes('class="h-14 px-4'), false);
assert.ok(header.includes('flex flex-wrap xl:flex-nowrap items-center justify-between'));
assert.ok(header.includes('min-h-[3.5rem] xl:h-14 xl:py-0'));
// Two wrapping rows below xl: the action group goes to its own row (ml-auto keeps
// it right aligned there) instead of sliding under the scheme capsule.
assert.ok(header.includes('class="flex flex-wrap xl:flex-nowrap items-center gap-2.5 min-w-0 xl:flex-1"'));
assert.ok(header.includes('class="flex items-center gap-2 shrink-0 ml-auto"'));
// Icon-only actions keep an accessible name once their labels are hidden, and the
// compact padding/min-width only applies before sm, so xl is untouched.
assert.match(header, /aria-label="校验链路"/);
assert.match(header, /aria-label="保存草稿"/);
assert.equal((header.match(/!min-w-0 !px-2 sm:!min-w-\[4\.5rem\] sm:!px-5/g) || []).length, 2);
// At exactly 1280 px (studio ~1008 px) the single row cannot hold every label, so
// the scheme name is the one thing that truncates: capped max-width per breakpoint,
// a min-w-0 capsule that may shrink, and the full name in the title tooltip.
assert.ok(header.includes('class="relative min-w-0" @click.stop'));
assert.ok(header.includes('max-w-[140px] sm:max-w-[200px] md:max-w-[260px] xl:max-w-[7rem] 2xl:max-w-[16.25rem]'));
assert.ok(header.includes(':title="currentScheme.name ? \'当前方案：\' + currentScheme.name + \'（点击切换）\' : \'切换中转方案\'"'));
// The execution-mode control stays a normal, non-positioned child of the toolbar
// (it used to render as a second line over the library panel header).
const execSwitcher = header.slice(
  header.indexOf('Refined Segmented Execution Location Switcher'),
  header.indexOf('Local migration notice')
);
assert.ok(execSwitcher.length > 0, 'the execution-mode switcher is missing');
assert.equal(/absolute|fixed/.test(execSwitcher), false);
assert.ok(execSwitcher.includes('class="hidden md:flex items-center p-1 rounded-xl bg-black/[0.03] border border-black/[0.06] text-xs shadow-2xs shrink-0"'));
assert.ok(execSwitcher.includes('hidden md:inline xl:hidden 2xl:inline">客户端执行<'));
assert.ok(execSwitcher.includes('hidden xl:inline 2xl:hidden">客户端<'));
assert.ok(execSwitcher.includes('hidden md:inline xl:hidden 2xl:inline">服务器执行<'));
assert.ok(execSwitcher.includes('hidden xl:inline 2xl:hidden">服务器<'));
// 检查 is the same action as 校验链路, so its duplicate only returns from 2xl up:
// that is the room the scheme name gets back at exactly 1280 px. The <span> decides
// it (a bare hidden on the control would lose to the global .ios-btn-secondary
// display rule, which is what made the old md:inline duplicate always visible).
assert.ok(header.includes('<span class="hidden 2xl:inline">'));
assert.ok(header.includes('<button type="button" class="ios-btn-secondary !py-1 text-xs" @click="checkScheme">检查</button>'));
assert.equal(header.includes('ios-btn-secondary !py-1 text-xs hidden'), false);
// Every toggle goes through the helpers that keep only one drawer open below xl.
assert.ok(header.includes('@click="toggleLeftPanel()"'));
assert.ok(header.includes('@click="toggleRightPanel()"'));
assert.equal(header.includes('@click="leftPanelOpen = !leftPanelOpen"'), false);
assert.equal(header.includes('@click="rightPanelOpen = !rightPanelOpen"'), false);
assert.ok(canvas.includes('@click="openLeftPanel()"'));
assert.ok(canvas.includes('@click="openRightPanel()"'));
// Nothing in the studio still answers to the old lg breakpoint (rounded-lg and the
// Modal's max-width="lg" prop are unrelated, so only the class prefix is checked).
assert.equal(canvas.includes('lg:'), false, 'the compact breakpoint must be xl everywhere');

const libraryAside = asideOf('拓扑资源库');
// Drawer below xl, in-flow column at xl and wider (identical desktop geometry).
assert.ok(libraryAside.includes('absolute inset-y-0 left-0 z-30 w-[min(18rem,85vw)] xl:relative xl:inset-auto xl:z-20 xl:shrink-0'));
assert.ok(libraryAside.includes("leftPanelOpen ? 'translate-x-0 xl:w-72'"));
assert.ok(libraryAside.includes("'-translate-x-full pointer-events-none xl:pointer-events-auto xl:translate-x-0 xl:w-0 xl:border-r-0'"));
// The inner columns fill the drawer below xl and keep their desktop width at xl.
assert.equal((libraryAside.match(/w-full xl:w-72/g) || []).length, 2);
assert.equal(/(?<!xl:)w-72/.test(libraryAside), false);

const inspectorAside = asideOf('属性检查器');
assert.ok(inspectorAside.includes('absolute inset-y-0 right-0 z-30 w-[min(20rem,85vw)] xl:relative xl:inset-auto xl:z-20 xl:shrink-0'));
assert.ok(inspectorAside.includes("rightPanelOpen ? 'translate-x-0 xl:w-80'"));
assert.ok(inspectorAside.includes("'translate-x-full pointer-events-none xl:pointer-events-auto xl:translate-x-0 xl:w-0 xl:border-l-0'"));
assert.equal((inspectorAside.match(/w-full xl:w-80/g) || []).length, 2);
assert.equal(/(?<!xl:)w-80/.test(inspectorAside), false);

// One backdrop closes the open drawer. z-[25] sits above the canvas controls
// (z-20) and below the drawers (z-30), and the no-scheme overlay (z-40) still
// owns the workspace.
const backdropAt = canvas.indexOf('@click="closeDrawers"');
assert.ok(backdropAt > 0, 'the drawer backdrop is missing');
const backdrop = canvas.slice(canvas.lastIndexOf('<div', backdropAt), backdropAt + 40);
assert.ok(backdrop.includes('v-if="leftPanelOpen || rightPanelOpen"'));
assert.ok(backdrop.includes('class="xl:hidden absolute inset-0 z-[25] bg-black/25"'));
assert.equal(canvas.includes('absolute inset-0 z-40 bg-[#F5F5F7]'), true);

// Closed by default below xl, both open at xl, and a resize across xl resets the
// mode so a desktop panel is never left covering the canvas as a drawer.
assert.ok(canvas.includes("const COMPACT_QUERY = '(max-width: 1279.98px)';"));
assert.ok(canvas.includes('window.matchMedia(COMPACT_QUERY)'));
assert.ok(canvas.includes('const compactViewport = ref(compactMedia ? compactMedia.matches : false);'));
assert.ok(canvas.includes('const leftPanelOpen = ref(!compactViewport.value);'));
assert.ok(canvas.includes('const rightPanelOpen = ref(!compactViewport.value);'));
assert.ok(canvas.includes("compactMedia?.addEventListener('change', onCompactViewportChange);"));
assert.ok(canvas.includes("compactMedia?.removeEventListener('change', onCompactViewportChange);"));
assert.match(canvas, /function toggleLeftPanel\(\) \{\s*leftPanelOpen\.value = !leftPanelOpen\.value;\s*if \(leftPanelOpen\.value && compactViewport\.value\) rightPanelOpen\.value = false;/);
assert.match(canvas, /function toggleRightPanel\(\) \{\s*rightPanelOpen\.value = !rightPanelOpen\.value;\s*if \(rightPanelOpen\.value && compactViewport\.value\) leftPanelOpen\.value = false;/);
// A canvas tap still opens the inspector at xl, but never stacks a drawer over the
// topology that was just tapped below xl (the floating 节点属性 button is the way in).
assert.match(canvas, /function revealInspector\(\) \{\s*if \(!compactViewport\.value\) rightPanelOpen\.value = true;/);
const selectNodeBody = canvas.slice(canvas.indexOf('function selectNode'), canvas.indexOf('function selectConnection'));
assert.ok(selectNodeBody.includes('revealInspector();'));
assert.equal(selectNodeBody.includes('rightPanelOpen.value = true'), false);
// The canvas itself stays a full-width flex child: the drawers overlay it.
assert.ok(canvas.includes('ref="canvasViewport" class="flex-1 bg-[#F9F9FB]'));
// The bottom hint may only appear where the canvas is wide enough for it and the
// zoom pill: from sm below xl (the panels are drawers, so the canvas has the full
// studio width) and again from 2xl (both panels docked, canvas ~656 px). It is
// suppressed at xl–2xl, where the sandbox is ~400 px and the two overlapped, and its
// max-width keeps the last line clear of the pill.
assert.ok(canvas.includes('hidden sm:flex xl:hidden 2xl:flex absolute bottom-4 left-4 max-w-[calc(100%-12rem)] bg-black/70'));
assert.ok(canvas.includes('absolute bottom-4 right-4 bg-white/90'));

// The drawer state machine is plain code, so pull the five helpers out of the page
// and run them against fake refs: "one drawer at a time, closed below xl" is then a
// behaviour check instead of only a string check.
const drawerSource = canvas.slice(
  canvas.indexOf('function applyCompactViewport'),
  canvas.indexOf('function setExecutionLocation')
);
assert.ok(drawerSource.includes('function revealInspector'), 'drawer helpers are missing');
const drawer = await load(transpile(`
const compactViewport = { value: false };
const leftPanelOpen = { value: true };
const rightPanelOpen = { value: true };
${drawerSource}
export { compactViewport, leftPanelOpen, rightPanelOpen, applyCompactViewport,
  toggleLeftPanel, toggleRightPanel, openLeftPanel, openRightPanel, closeDrawers, revealInspector };
`));
const state = () => [drawer.compactViewport.value, drawer.leftPanelOpen.value, drawer.rightPanelOpen.value];

drawer.applyCompactViewport(true);
assert.deepEqual(state(), [true, false, false], 'below xl both drawers start closed');
// Opening one closes the other (they would otherwise stack at 320–509 px).
drawer.toggleLeftPanel();
assert.deepEqual(state(), [true, true, false]);
drawer.toggleRightPanel();
assert.deepEqual(state(), [true, false, true]);
drawer.openLeftPanel();
assert.deepEqual(state(), [true, true, false]);
drawer.openRightPanel();
assert.deepEqual(state(), [true, false, true]);
// Tapping a node never drops a drawer over the canvas below xl…
drawer.revealInspector();
assert.deepEqual(state(), [true, false, true]);
// …and the backdrop closes whatever is open.
drawer.closeDrawers();
assert.deepEqual(state(), [true, false, false]);
// Desktop keeps the studio: both columns open, tap-to-reveal works again.
drawer.applyCompactViewport(false);
assert.deepEqual(state(), [false, true, true]);
drawer.closeDrawers();
drawer.revealInspector();
assert.deepEqual(state(), [false, false, true], 'desktop still auto-opens the inspector');
// A repeat notification for the current mode must not undo a manual collapse.
drawer.applyCompactViewport(false);
assert.deepEqual(state(), [false, false, true]);

console.log('All canvas-library self-tests passed');

assert.ok(canvas.includes('selfHostedResourceId(inbound) === node.resourceId'));
