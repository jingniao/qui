import assert from 'node:assert/strict';
import { retainedQuery, readTogether, APIError } from './retained-query-fixture.mjs';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const compile = source => ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
const ref = value => ({ value });
const deferred = () => { let resolve, reject; const promise = new Promise((ok, fail) => { resolve = ok; reject = fail; }); return { promise, resolve, reject }; };
const tick = () => new Promise(resolve => setImmediate(resolve));

// Execute the actual page loader with out-of-order transport completion.
const access = read('modules/access/pages/AccessRecordPage.vue');
const loader = access.slice(access.indexOf('let generation = 0;'), access.indexOf('watch(() => route.query.tab'));
const reads = [];
const readAPI = kind => (...args) => { const pending = deferred(); reads.push({ kind, args, ...pending }); return pending.promise; };
const state = { readTogether, retainedQuery, selected: ref(null), drawer: ref(false), activeTab: ref('all'), page: ref(1), loading: ref(false), error: ref(''), records: ref([]), domains: ref([]), total: ref(0), summary: ref(null), accountSummary: ref(null) };
const load = new Function('accessAPI', 'selectedFilter', 'ref', ...Object.keys(state), compile(loader + '\nreturn load;'))(
  { list: readAPI('list'), domains: readAPI('domains'), summary: readAPI('summary'), accounts: readAPI('accounts') }, () => ({ domain: 'example.com' }), ref, ...Object.values(state));
const first = load(); assert.equal(reads.length, 3, 'list and both summaries start together');
state.activeTab.value = 'stats'; state.page.value = 2;
const second = load(); assert.equal(reads.length, 6, 'a pending request must not drop a new filter/tab');
reads[3].resolve({ items: [{ domain: 'new' }], total: 1 }); reads[4].resolve({ connections: 2 }); reads[5].resolve({ observations: 3 }); await second;
reads[0].resolve({ items: [{ id: 'old-record' }], total: 9 }); reads[1].resolve({ connections: 99 }); reads[2].resolve({ observations: 99 }); await first;
assert.deepEqual(state.domains.value, [{ domain: 'new' }]); assert.deepEqual(state.records.value, []);
assert.equal(state.summary.value.connections, 2); assert.equal(state.loading.value, false);
console.log('PASS access loader: three parallel reads, latest tab/page wins, stale totals ignored');
state.activeTab.value = 'all';
const allowed = load();
reads[6].resolve({ items: [{ id: 'allowed', allowed: true }], total: 1 }); reads[7].resolve({ connections: 9 }); reads[8].resolve({ observations: 7 }); await allowed;
state.selected.value = state.records.value[0]; state.drawer.value = true;
state.activeTab.value = 'blocked';
const blocked = load();
assert.deepEqual(state.records.value, [], 'blocked tab must not temporarily display allowed rows');
assert.equal(state.summary.value, null);
assert.equal(state.selected.value, null, 'old record must leave the detail drawer'); assert.equal(state.drawer.value, false);
reads[9].resolve({ items: [], total: 0 }); reads[10].resolve({ connections: 0 }); reads[11].resolve({ observations: 0 }); await blocked;
console.log('PASS blocked query drops previous rows and totals before its read completes');
const existing = load(); const deniedRecord = { id: 'blocked', allowed: false };
reads[12].resolve({ items: [deniedRecord], total: 1 }); reads[13].resolve({ connections: 1 }); reads[14].resolve({ observations: 1 }); await existing;
state.selected.value = deniedRecord; state.drawer.value = true;
const revoked = load(); reads[16].reject(new Error('summary unavailable')); await tick();
reads[15].reject(new APIError(404, 'not_found', 'record removed')); await tick();
assert.equal(state.selected.value, null); assert.equal(state.drawer.value, false);
assert.deepEqual(state.records.value, []); assert.equal(state.error.value, 'record removed');
reads[17].resolve({ observations: 9 }); await revoked;
console.log('PASS denied same-query refresh closes its detail and drops earlier totals');

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
const primaryLoader = detail.slice(detail.indexOf('async function loadPrimaryUser('), detail.indexOf('async function loadPreview('));
const statusLoader = detail.slice(detail.indexOf('async function refreshUserStatus()'), detail.indexOf('const refreshStatus ='));
const user = ref({ id: 'u', loginEnabled: false }), loginEnabled = ref(true);
let statusReads = 0, activityReads = 0;
const refreshStatus = new Function('document', 'user', 'loginEnabled', 'userAPI', 'loadActivity', 'showToast', 'error', compile(`
  let loadingUser = false, userGeneration = 0, primaryUserRead;
  const resetProfileRetryRequired = { value: false };
  ${primaryLoader}
  ${statusLoader}
  return refreshUserStatus;`))(
  { hidden: false }, user, loginEnabled, { get: async () => { statusReads++; return { id: 'u', loginEnabled: false, usedTraffic: 42 }; } },
  () => { activityReads++; }, error => { throw new Error(error); }, ref(''));
await refreshStatus(); assert.equal(user.value.usedTraffic, 42); assert.equal(loginEnabled.value, true, 'background status must preserve unsaved portal toggle');
assert.equal(statusReads, 1); assert.equal(activityReads, 1, 'recent activity still refreshes');
console.log('PASS user status: new usage and activity, unsaved portal switch preserved');
