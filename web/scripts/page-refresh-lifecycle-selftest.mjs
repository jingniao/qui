import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const compile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
const ref = value => ({ value });
const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done; }); return { promise, resolve }; };
const tick = () => new Promise(resolve => setImmediate(resolve));

// Execute the actual page loader with out-of-order transport completion.
const access = read('modules/access/pages/AccessRecordPage.vue');
const loader = access.slice(access.indexOf('let generation = 0;'), access.indexOf('watch(() => route.query.tab'));
const reads = [];
const readAPI = kind => (...args) => { const pending = deferred(); reads.push({ kind, args, ...pending }); return pending.promise; };
const state = { activeTab: ref('all'), page: ref(1), loading: ref(false), error: ref(''), records: ref([]), domains: ref([]), total: ref(0), summary: ref(null), accountSummary: ref(null) };
const load = new Function('accessAPI', 'selectedFilter', ...Object.keys(state), compile(loader + '\nreturn load;'))(
  { list: readAPI('list'), domains: readAPI('domains'), summary: readAPI('summary'), accounts: readAPI('accounts') }, () => ({ domain: 'example.com' }), ...Object.values(state));
const first = load(); assert.equal(reads.length, 3, 'list and both summaries start together');
state.activeTab.value = 'stats'; state.page.value = 2;
const second = load(); assert.equal(reads.length, 6, 'a pending request must not drop a new filter/tab');
reads[3].resolve({ items: [{ domain: 'new' }], total: 1 }); reads[4].resolve({ connections: 2 }); reads[5].resolve({ observations: 3 }); await second;
reads[0].resolve({ items: [{ id: 'old-record' }], total: 9 }); reads[1].resolve({ connections: 99 }); reads[2].resolve({ observations: 99 }); await first;
assert.deepEqual(state.domains.value, [{ domain: 'new' }]); assert.deepEqual(state.records.value, []);
assert.equal(state.summary.value.connections, 2); assert.equal(state.loading.value, false);
console.log('PASS access loader: three parallel reads, latest tab/page wins, stale totals ignored');

// Exercise the actual update poller: pending reads cannot revive an unmounted page.
const panel = read('modules/system/components/PanelUpdatePanel.vue');
const polling = panel.slice(panel.indexOf('async function load('), panel.indexOf('// A pending request'));
function panelHarness() {
  const requests = [], timers = new Map(); let serial = 0;
  const document = { hidden: false };
  const values = { state: ref(null), error: ref(''), updating: ref(false), timeoutHint: ref(''), watchedRequestId: ref('job'), finished: ref(null), reloadAfterUpdate: ref(false) };
  const run = new Function('settingsAPI', 'window', 'document', ...Object.keys(values), compile(`
    let disposed = false, polling = false, pollInFlight = false;
    let pollTimer: number | undefined, pollStartedAt = 0;
    const POLL_LIMIT_MS = 900000, POLL_INTERVAL_MS = 3000;
    ${polling}
    return { startPolling, tick, visibilityChanged, load, stop: () => { disposed = true; stopPolling(); } };`))(
    { panelUpdate: () => { const pending = deferred(); requests.push(pending); return pending.promise; } },
    { setTimeout: (callback, delay) => { assert.equal(delay, 3000); timers.set(++serial, callback); return serial; }, clearTimeout: id => timers.delete(id) },
    document, ...Object.values(values));
  return { ...run, requests, timers, document, values };
}
const h = panelHarness(); h.startPolling(); assert.equal(h.timers.size, 1);
h.document.hidden = true; h.visibilityChanged(); assert.equal(h.timers.size, 0);
await h.tick(); assert.equal(h.requests.length, 0, 'hidden page does not read');
h.document.hidden = false; h.visibilityChanged(); assert.equal(h.requests.length, 1);
h.visibilityChanged(); assert.equal(h.requests.length, 1, 'visibility cannot overlap a pending read');
h.stop(); h.requests[0].resolve({ status: { requestId: 'job', state: 'running' } }); await tick();
assert.equal(h.timers.size, 0); assert.equal(h.values.state.value, null, 'unmounted response is discarded');
const done = panelHarness(); done.startPolling(); done.timers.clear(); const pending = done.tick();
done.requests[0].resolve({ status: { requestId: 'job', state: 'succeeded' } }); await pending;
assert.equal(done.values.updating.value, false); assert.equal(done.timers.size, 0); assert.equal(done.values.reloadAfterUpdate.value, true);
console.log('PASS panel poller: hidden pause, foreground single read, pending unmount, terminal stop');

// Async portal sections must keep Chart.js out of the overview's static graph.
const portal = read('portal/PortalPage.vue');
assert.ok(!/import PortalTraffic from/.test(portal));
assert.ok(!/import PortalSubscription from/.test(portal));
assert.equal((read('modules/routing/pages/RoutingRuleEditorPage.vue').match(/policyAPI\.get\(editingId.value\)/g) || []).length, 1);
console.log('PASS page dependencies: lazy traffic/QR and one parallel policy detail request');

const detail = read('modules/users/pages/UserDetailPage.vue');
const statusLoader = detail.slice(detail.indexOf('async function refreshUserStatus()'), detail.indexOf('const refreshStatus ='));
const user = ref({ id: 'u', loginEnabled: false }), loginEnabled = ref(true);
let statusReads = 0, activityReads = 0;
const refreshStatus = new Function('document', 'user', 'loginEnabled', 'userId', 'userAPI', 'loadActivity', 'showToast', compile(`
  let loadingUser = false, userGeneration = 0;
  ${statusLoader}
  return refreshUserStatus;`))(
  { hidden: false }, user, loginEnabled, ref('u'), { get: async () => { statusReads++; return { id: 'u', loginEnabled: false, usedTraffic: 42 }; } },
  () => { activityReads++; }, error => { throw new Error(error); });
await refreshStatus(); assert.equal(user.value.usedTraffic, 42); assert.equal(loginEnabled.value, true, 'background status must preserve unsaved portal toggle');
assert.equal(statusReads, 1); assert.equal(activityReads, 1, 'recent activity still refreshes');
console.log('PASS user status: new usage and activity, unsaved portal switch preserved');
