import assert from 'node:assert/strict';
import { retainedQuery, readTogether, removePageItem } from './retained-query-fixture.mjs';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { fileURLToPath } from 'node:url';
import { createServer } from 'vite';
import { compile } from '@vue/compiler-dom';
import { parse } from '@vue/compiler-sfc';
import * as Vue from 'vue';
import { renderToString } from '@vue/server-renderer';
import ts from 'typescript';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const tick = () => new Promise(resolve => setImmediate(resolve));
const deferred = () => { let resolve, reject; const promise = new Promise((ok, fail) => { resolve = ok; reject = fail; }); return { promise, resolve, reject }; };
function page(path, bindings, names) {
  const sfc = read(path);
  const script = sfc.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
  const source = ts.createSourceFile('page.ts', script, ts.ScriptTarget.Latest, true);
  const setup = source.statements.filter(node => !ts.isImportDeclaration(node)).map(node => node.getText(source)).join('\n');
  const scope = { ref: Vue.ref, computed: Vue.computed, watch: Vue.watch, reactive: Vue.reactive, onMounted() {}, onUnmounted() {}, useRoute: () => ({ path: '', query: {} }), ...bindings };
  const code = ts.transpileModule(setup, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  return new Function(...Object.keys(scope), code + '\nreturn { ' + names + ' };')(...Object.values(scope));
}
function render(path, state) {
  const template = parse(read(path)).descriptor.template.content;
  const draw = new Function('Vue', compile(template, { prefixIdentifiers: true }).code)(Vue);
  const app = Vue.createSSRApp({ setup: () => state, render: draw });
  app.config.warnHandler = () => {};
  app.component('DataTable', { setup: (_, { slots }) => () => Vue.h('table', slots.body?.()) });
  app.component('ErrorState', { props: ['message'], setup: props => () => Vue.h('p', props.message) });
  app.component('EmptyState', { props: ['title'], setup: props => () => Vue.h('p', props.title) });
  return renderToString(app);
}
class APIError extends Error { constructor(status, message) { super(message); this.status = status; } }
const invalid = e => [401, 403, 404].includes(e?.status);
const common = { removePageItem, readTogether, retainedQuery, APIError, invalidatesRead: invalid };

for (const [component, name, api] of [
  ['RetentionJobPanel', 'job', 'retentionJobAPI'], ['StorageObservationPanel', 'sample', 'storageObservationAPI']
]) test(component + ' keeps last observation through a pending read and transport failure', async () => {
  let response;
  const p = page('modules/system/components/' + component + '.vue', { ...common, [api]: () => response.promise }, 'load,error,' + name);
  response = deferred(); const first = p.load(); response.resolve({ observedAt: 'first' }); await first;
  response = deferred(); const next = p.load();
  assert.equal(p[name].value?.observedAt, 'first');
  response.reject(new Error('timeout')); await next;
  assert.equal(p.error.value, 'timeout'); assert.equal(p[name].value?.observedAt, 'first');
  response = deferred(); const last = p.load(); response.reject(new APIError(403, 'denied')); await last;
  assert.equal(p[name].value, null);
});

test('tunnel table and confirmed empty state remain visible during reads', async () => {
  const path = 'modules/tunnels/pages/TunnelsPage.vue';
  let response;
  const bindings = { ...common, isOwner: Vue.ref(true), tunnelAPI: { list: () => response.promise }, tunnelRuleAPI: { list: async () => ({ items: [] }) },
    loadTunnelChoices: async () => ({ nodes: [], inbounds: [], imports: [] }), groupRules: () => [], tunnelError: e => e.message, mockEnabled: () => false, routeLabel: () => '' };
  const state = page(path, bindings, 'load,loading,error,rulesTab,tunnels,rules,nodes,inbounds,imports,applyNote,editor,mutating,groups,confirmVisible,blockedVisible,blockers,confirmMessage,editingRule,editingTunnel,ready: typeof ready === "undefined" ? undefined : ready');
  const html = () => render(path, { ...bindings, ...state });
  response = deferred(); const first = state.load();
  response.resolve({ items: [{ id: 't1', name: 'known-tunnel', hops: [], status: 'published' }] }); await first;
  response = deferred(); const refresh = state.load();
  assert.match(await html(), /known-tunnel/); response.reject(new Error('offline')); await refresh;
  assert.match(await html(), /known-tunnel/); assert.match(await html(), /offline/);
  response = deferred(); const retry = state.load(); response.resolve({ items: [] }); await retry;
  response = deferred(); const again = state.load(); assert.match(await html(), /暂无隧道/);
  response.resolve({ items: [] }); await again;
});

test('preview keeps one account on retry and clears it on selection or permission loss', async () => {
  let response;
  const p = page('modules/users/pages/PortalPreviewPage.vue', { ...common, request: () => response.promise }, 'selected,snapshot,error,load');
  p.selected.value = 'one'; response = deferred(); const first = p.load(); response.resolve({ me: { id: 'one' } }); await first;
  response = deferred(); const again = p.load(); assert.equal(p.snapshot.value?.me.id, 'one');
  response.reject(new Error('timeout')); await again; assert.equal(p.snapshot.value?.me.id, 'one'); assert.equal(p.error.value, 'timeout');
  response = deferred(); const staleResponse = response, stale = p.load();
  p.selected.value = 'two'; response = deferred(); const second = p.load(); assert.equal(p.snapshot.value, undefined);
  response.resolve({ me: { id: 'two' } }); await second;
  staleResponse.resolve({ me: { id: 'one' } }); await stale; assert.equal(p.snapshot.value?.me.id, 'two');
  response = deferred(); const denied = p.load(); response.reject(new APIError(403, 'denied')); await denied; assert.equal(p.snapshot.value, undefined);
});

test('shared imports discard a past login and invalidate only the denied pending reads', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
  const prevFetch = globalThis.fetch;
  const reads = [];
  globalThis.fetch = (url) => { const d = deferred(); reads.push({ url, ...d }); return d.promise; };
  const rows = (r, id) => r.resolve(Response.json({ items: [{ id, name: id, sourceId: id, kind: 'url', protocol: 'vless' }] }));
  try {
    const { authState, clearSession } = await server.ssrLoadModule('/src/app/auth.ts');
    const { loadImports } = await server.ssrLoadModule('/src/modules/proxy/importApi.ts');
    const { request } = await server.ssrLoadModule('/src/app/apiClient.ts');
    const { proxyNodeStore: store } = await server.ssrLoadModule('/src/modules/proxy/proxyNodes.ts');
    const login = () => { authState.currentUser = { id: 'admin', role: 'Owner' }; authState.isAuthenticated = true; };
    login();
    const first = loadImports(); rows(reads[0], 'one'); rows(reads[1], 'one'); await first;
    const old = loadImports();
    assert.equal(store.manualNodes[0]?.id, 'one');
    clearSession(); assert.deepEqual(store.manualNodes, []);
    login(); const second = loadImports();
    assert.equal(reads.length, 6, 'past login must never supply either fresh response');
    rows(reads[4], 'two'); rows(reads[5], 'two'); await second;
    rows(reads[2], 'old'); rows(reads[3], 'old'); await old;
    assert.equal(store.manualNodes[0]?.id, 'two');
    const retry = loadImports();
    reads[6].resolve(Response.json({}, { status: 500 })); rows(reads[7], 'unused'); await assert.rejects(retry);
    assert.equal(store.manualNodes[0]?.id, 'two');
    const unrelated = request('/health-info');
    const denied = loadImports(); reads[9].resolve(Response.json({}, { status: 403 })); await assert.rejects(denied);
    assert.deepEqual(store.manualNodes, []); assert.deepEqual(store.subscriptions, []);
    const again = loadImports(), other = request('/health-info');
    assert.equal(reads.length, 13);
    rows(reads[11], 'fresh'); rows(reads[12], 'fresh'); await again;
    rows(reads[10], 'denied'); reads[8].resolve(Response.json({ ok: true })); await Promise.all([unrelated, other]);
    assert.equal(store.manualNodes[0]?.id, 'fresh');
    authState.currentUser.role = 'Viewer'; await tick(); assert.deepEqual(store.manualNodes, []);
  } finally { globalThis.fetch = prevFetch; await server.close(); }
});

test('operation list keeps same-query rows beside errors and clears rows when changing page', async () => {
  const path = 'modules/system/pages/OperationLogPage.vue';
  let response;
  const p = page(path, { ...common, request: () => response.promise }, 'load,operations,total,page,error,loading,keyword,display: typeof display === "undefined" ? undefined : display');
  response = deferred(); const first = p.load(); response.resolve({ items: [{ id: 1, action: 'old-action' }], total: 2 }); await first;
  response = deferred(); const again = p.load(); response.reject(new Error('unavailable')); await again;
  const output = await render(path, { ...p, formatTime: String });
  assert.match(output, /old-action/); assert.match(output, /unavailable/);
  p.page.value++; response = deferred(); const next = p.load(); assert.deepEqual(p.operations.value, []);
  response.resolve({ items: [{ id: 2 }], total: 2 }); await next; assert.equal(p.operations.value[0].id, 2);
});
test('portal traffic keeps displayed range and values together until the new read succeeds', async () => {
  let response;
  const bindings = { defineProps: () => ({ section: 'traffic' }), defineAsyncComponent: () => ({}), portalAPI: { traffic: () => response.promise }, watch() {} };
  const p = page('portal/PortalPage.vue', bindings, 'load,range,traffic,ready,error,loadTraffic,displayedRange: typeof displayedRange === "undefined" ? range : displayedRange');
  response = deferred(); const first = p.load(); response.resolve({ items: [{ bucket: 'week' }] }); await first;
  p.range.value = '24h'; response = deferred(); const day = p.loadTraffic();
  assert.equal(p.displayedRange.value, '7d');
  response.reject(new Error('unavailable')); await day; assert.equal(p.traffic.value.items[0].bucket, 'week'); assert.equal(p.error.value, 'unavailable');
  response = deferred(); const retry = p.loadTraffic(); response.resolve({ items: [{ bucket: 'hour' }] }); await retry;
  assert.equal(p.displayedRange.value, '24h'); assert.equal(p.traffic.value.items[0].bucket, 'hour');
});

test('metrics retry keeps a measured point and chart window until the replacement arrives', async () => {
  const props = { node: { id: 'A' }, hours: 1 };
  let response;
  const path = 'modules/nodes/components/ServerMetricsGrid.vue';
  const p = page(path, { ...common, watch() {}, defineProps: () => props, defineEmits: () => () => {}, defineAsyncComponent: () => ({}),
    document: { visibilityState: 'visible' }, nodeAPI: { metrics: () => response.promise }, metricPoint: item => item,
    beginMetricsLoad: (state, first) => ({ ...state, loading: first, error: '' }), failMetricsLoad: (state, error) => ({ ...state, error })
  }, 'load,points,start,end,error');
  response = deferred(); const first = p.load(true); response.resolve({ intervalSeconds: 60, points: [{ time: 1000, cpu: 42 }] }); await first;
  response = deferred(); const retry = p.load(true); assert.equal(p.points.value[0]?.cpu, 42);
  response.reject(new Error('timeout')); await retry; assert.match(p.error.value, /timeout/);
  props.hours = 6; response = deferred(); const next = p.load(true);
  assert.equal(p.end.value - p.start.value, 3600000); assert.equal(p.points.value[0]?.cpu, 42);
  response.resolve({ intervalSeconds: 300, points: [{ time: 2000, cpu: 10 }] }); await next;
  assert.equal(p.end.value - p.start.value, 21600000); assert.equal(p.points.value[0]?.cpu, 10);
  props.node.id = 'B'; response = deferred(); const changed = p.load(true); assert.deepEqual(p.points.value, []); response.resolve({ intervalSeconds: 60, points: [] }); await changed;
});

test('metadata used by card and search belongs to the active administrator', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
  const prevFetch = globalThis.fetch; let response;
  globalThis.fetch = () => response.promise;
  try {
    const { authState } = await server.ssrLoadModule('/src/app/auth.ts');
    const { getNodeMeta, hasNodeMeta, loadNodeMeta } = await server.ssrLoadModule('/src/modules/nodes/nodeMeta.ts');
    authState.currentUser = { id: 'A', role: 'Viewer' }; authState.isAuthenticated = true;
    response = deferred(); const first = loadNodeMeta('s', false); response.resolve(Response.json({ present: true, meta: { region: 'A' } })); await first;
    response = deferred(); const pending = loadNodeMeta('s', false);
    authState.currentUser = { id: 'B', role: 'Viewer' }; assert.equal(hasNodeMeta('s'), false);
    response.resolve(Response.json({ present: true, meta: { region: 'late A' } })); await pending.catch(() => {});
    assert.equal(getNodeMeta('s').region, undefined);
  } finally { globalThis.fetch = prevFetch; await server.close(); }
});

test('a delayed session probe cannot restore a previous owner after logout and login', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
  const prevFetch = globalThis.fetch;
  const old = deferred();
  const session = (id, role) => ({ isAuthenticated: true, isInitialized: true, csrfToken: id + '-token', currentUser: { id, role }, timezone: 'UTC', platformName: 'panel' });
  let next = session('A', 'Owner');
  globalThis.fetch = async url => url.endsWith('/session') ? old.promise : Response.json(next);
  try {
    const auth = await server.ssrLoadModule('/src/app/auth.ts');
    await auth.login('A', 'pw');
    const probe = auth.refreshSession();
    await auth.logout(); next = session('B', 'Viewer'); await auth.login('B', 'pw');
    old.resolve(Response.json(session('A', 'Owner'))); await probe;
    assert.equal(auth.authState.currentUser.id, 'B'); assert.equal(auth.isOwner.value, false);
  } finally { globalThis.fetch = prevFetch; await server.close(); }
});

test('a confirmed import deletion cannot be brought back by an older response', async () => {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
  const prevFetch = globalThis.fetch;
  const old = deferred();
  const rows = { items: [{ id: 'source', sourceId: 'source', name: 'last', protocol: 'vless' }] };
  let delayed = false;
  globalThis.fetch = async (_, { method }) => method === 'DELETE' ? new Response(null, { status: 204 }) : delayed ? Response.json(await old.promise) : Response.json(rows);
  try {
    const { authState } = await server.ssrLoadModule('/src/app/auth.ts');
    authState.currentUser = { id: 'A', role: 'Owner' }; authState.isAuthenticated = true;
    const imports = await server.ssrLoadModule('/src/modules/proxy/importApi.ts');
    const { proxyNodeStore } = await server.ssrLoadModule('/src/modules/proxy/proxyNodes.ts');
    await imports.loadImports(); delayed = true;
    const read = imports.loadImports(); await imports.importAPI.removeProxy('source', { expectedRevision: 1, removeReferences: true });
    old.resolve(rows); await read;
    assert.deepEqual(proxyNodeStore.manualNodes, []); assert.deepEqual(proxyNodeStore.subscriptions, []);
  } finally { globalThis.fetch = prevFetch; await server.close(); }
});

async function taskModules(work) {
  const server = await createServer({ configFile: false, root: fileURLToPath(new URL('..', import.meta.url)), logLevel: 'silent', server: { middlewareMode: true }, optimizeDeps: { noDiscovery: true } });
  const original = globalThis.fetch;
  try { await work(path => server.ssrLoadModule(path)); }
  finally { globalThis.fetch = original; await server.close(); }
}

test('review: last arriving denied read revokes tunnel rows despite an earlier network failure', async () => {
  let list = deferred(), rules = deferred(), options = deferred();
  const p = page('modules/tunnels/pages/TunnelsPage.vue', { ...common, isOwner: Vue.ref(true), tunnelAPI: { list: () => list.promise }, tunnelRuleAPI: { list: () => rules.promise },
    loadTunnelChoices: () => options.promise, tunnelError: e => e.message }, 'load,tunnels,ready,error');
  const first = p.load(); list.resolve({ items: [{ id: 'old' }] }); rules.resolve({ items: [] }); options.resolve({}); await first;
  list = deferred(); rules = deferred(); options = deferred(); const retry = p.load();
  rules.reject(new Error('network')); await tick(); list.reject(new APIError(403, 'revoked')); await tick();
  assert.equal(p.ready.value, false); assert.deepEqual(p.tunnels.value, []); assert.equal(p.error.value, 'revoked');
  options.resolve({}); await retry;
});

test('review: denied group dependencies prevent late first group rows from appearing', async () => {
  const component = read('modules/proxy/pages/LineGroupPage.vue');
  const block = component.slice(component.indexOf('const groupsReady ='), component.indexOf('const ours ='));
  const group = deferred(), auxiliary = deferred();
  const dependencies = { ...common, ref: Vue.ref, groupAPI: { list: () => group.promise }, choices: fn => fn(1, 20), request: () => auxiliary.promise,
    policyAPI: { list: async () => [] }, proxyAPI: { inbounds: async () => [] }, loadImports: async () => {}, userAPI: { choices: async () => [] }, tunnelRuleAPI: { list: async () => ({ items: [] }) }, syncSelection() {} };
  for (const n of ['groups', 'servers', 'inbounds', 'policies', 'users', 'tunnelRules', 'clientTemplate', 'loading', 'error']) dependencies[n] = Vue.ref(null);
  const code = ts.transpileModule(block, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  const p = new Function(...Object.keys(dependencies), code + '\nreturn { load, groupsReady };')(...Object.values(dependencies));
  const loading = p.load(); auxiliary.reject(new APIError(403, 'revoked')); await tick();
  group.resolve([{ id: 'old' }]); await loading; await tick();
  assert.equal(p.groupsReady.value, false); assert.deepEqual(dependencies.groups.value, []);
});

test('review: a delayed canvas creation cannot enter another administrator’s scheme list', () => taskModules(async load => {
  const { authState } = await load('/src/app/auth.ts');
  const { createCanvasScheme, proxyNodeStore } = await load('/src/modules/proxy/proxyNodes.ts');
  const response = deferred(); globalThis.fetch = () => response.promise;
  authState.currentUser = { id: 'A', role: 'Owner' }; authState.isAuthenticated = true;
  const pending = createCanvasScheme('old', 'server', { servers: [], inbounds: [], imported: [] });
  authState.currentUser = { id: 'B', role: 'Owner' }; proxyNodeStore.schemes = [{ id: 'new' }];
  response.resolve(Response.json({ id: 'old', name: 'old', execution: 'server', layout: { instances: [], connections: [] }, updatedAt: 'v' }));
  await pending.catch(() => {}); assert.deepEqual(proxyNodeStore.schemes.map(row => row.id), ['new']);
}));

test('review: stale single and batch metadata responses cannot roll back an accepted edit or erase it', () => taskModules(async load => {
  const meta = await load('/src/modules/nodes/nodeMeta.ts');
  let delayed, pendingSave, failSave = false;
  globalThis.fetch = (_, { method, body }) => method === 'PUT'
    ? failSave ? Response.json({}, { status: 503 }) : pendingSave ? pendingSave.promise : Response.json({ meta: JSON.parse(body) })
    : delayed.promise;
  for (const batch of [false, true]) for (const denied of [false, true]) {
    delayed = deferred(); const old = batch ? meta.loadNodeMetadata(false) : meta.loadNodeMeta('s', false);
    await meta.saveNodeMeta('s', { region: 'saved' });
    const item = { nodeId: 's', present: true, meta: { region: 'old' } };
    delayed.resolve(denied ? Response.json({}, { status: 403 }) : Response.json(batch ? { items: [item] } : item));
    await old.catch(() => {}); assert.equal(meta.getNodeMeta('s').region, 'saved', JSON.stringify({ batch, denied }));
  }
  assert.equal(meta.nodeFinanceRevisions?.s ?? 0, 0, 'nonfinancial edits do not refresh financial data');
  await meta.saveNodeMeta('s', { ...meta.getNodeMeta('s'), renewPrice: '12', renewCurrency: 'CNY', renewCycle: '/年', renewDate: '2027-01-01' });
  assert.equal(meta.nodeFinanceRevisions?.s, 1, 'adding pricing refreshes once');
  for (const [key, value] of [['renewPrice', '24'], ['renewCurrency', 'USD'], ['renewCycle', '/月'], ['renewDate', '2027-02-01']]) {
    const before = meta.nodeFinanceRevisions.s;
    await meta.saveNodeMeta('s', { ...meta.getNodeMeta('s'), [key]: value });
    assert.equal(meta.nodeFinanceRevisions.s, before + 1, key + ' refreshes once');
  }
  const before = meta.nodeFinanceRevisions.s;
  await meta.saveNodeMeta('s', { ...meta.getNodeMeta('s'), region: 'changed' });
  assert.equal(meta.nodeFinanceRevisions.s, before, 'unrelated edits keep the financial cache');
  await meta.saveNodeMeta('s', { ...meta.getNodeMeta('s'), renewPrice: '', renewCurrency: '', renewCycle: '', renewDate: '' });
  assert.equal(meta.nodeFinanceRevisions.s, before + 1, 'clearing pricing refreshes once');
  const raced = { ...meta.getNodeMeta('s'), renewPrice: '36', renewCurrency: 'USD' };
  pendingSave = deferred(); delayed = deferred();
  const saving = meta.saveNodeMeta('s', raced);
  const reading = meta.loadNodeMeta('s', false);
  delayed.resolve(Response.json({ meta: raced, present: true })); await reading;
  pendingSave.resolve(Response.json({ meta: raced })); await saving;
  pendingSave = undefined;
  assert.equal(meta.nodeFinanceRevisions.s, before + 2, 'a new GET before the save response cannot suppress its financial refresh');
  failSave = true;
  await assert.rejects(meta.saveNodeMeta('s', { ...meta.getNodeMeta('s'), renewPrice: '30' }));
  assert.equal(meta.nodeFinanceRevisions.s, before + 2, 'failed saves do not refresh financial data');
}));

test('review: deleting the last row on page two reloads page one and preserves a correct total on failure', async () => {
  for (const [visible, expectedPage] of [[1, 1], [2, 2]]) {
    const reads = [];
    const items = Array.from({ length: visible }, (_, i) => ({ id: String(i) }));
    const p = page('modules/routing/pages/ServiceRulePage.vue', { ...common, showToast() {}, policyAPI: {
      deleteService: async () => {}, services: async n => {
        reads.push(n);
        if (reads.length === 1) return { items, total: 20 + visible };
        throw new Error('offline');
      }
    } }, 'page,total,services,target,remove,load,error,display');
    p.page.value = 2; await p.load(); p.target.value = { id: '0' };
    await p.remove();
    assert.deepEqual(reads, [2, expectedPage]); assert.equal(p.page.value, expectedPage);
    assert.equal(p.error.value, 'offline');
    assert.deepEqual(p.services.value.map(row => row.id), visible === 2 ? ['1'] : []);
    if (visible === 2) assert.equal(p.total.value, 21, 'same-page retained total excludes the deleted row');
    else assert.equal(p.display.ready, false, 'unread destination page has no confirmed empty result');
  }
});

test('review: server detail distinguishes unmeasured outbound sections from confirmed empty results', async () => {
  const path = 'modules/nodes/components/detail/ServerNodes.vue';
  const props = Vue.reactive({ node: { id: 'one' }, refresh: 0 });
  let outbound = deferred(), exits = deferred();
  const bindings = { ...common, isOwner: Vue.ref(true), defineProps: () => props, defineEmits: () => () => {}, choices: f => f(1, 100),
    proxyAPI: { inbounds: async () => [], outbounds: () => outbound.promise, groups: () => exits.promise } };
  const names = 'inbounds,outbounds,exits,error,loading,inboundReady,outboundReady: typeof outboundReady === "undefined" ? undefined : outboundReady, exitsReady: typeof exitsReady === "undefined" ? undefined : exitsReady';
  const p = page(path, bindings, names); const html = () => render(path, { ...bindings, ...p, node: props.node });
  assert.doesNotMatch(await html(), /出站（0|出口组（0|此服务器尚无出站配置|此服务器尚无出口组/);
  outbound.resolve([]); exits.reject(new Error('network')); await tick();
  assert.match(await html(), /此服务器尚无出站配置/); assert.doesNotMatch(await html(), /此服务器尚无出口组/);
  outbound = deferred(); exits = deferred(); props.refresh++; await Vue.nextTick();
  outbound.reject(new APIError(403, 'denied')); await tick();
  assert.doesNotMatch(await html(), /此服务器尚无出站配置/, 'denied result disappears even while another section is pending');
  exits.resolve([]); await tick();
  assert.doesNotMatch(await html(), /此服务器尚无出站配置/); assert.match(await html(), /此服务器尚无出口组/);
});
