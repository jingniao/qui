import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const ref = value => ({ value });
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
const tick = async () => { for (let i = 0; i < 12; i++) await Promise.resolve(); };
function extract(page, from, to, scope, result = 'load') {
  const source = readFileSync(new URL(`../src/modules/users/pages/${page}.vue`, import.meta.url), 'utf8');
  const start = source.indexOf(from), end = source.indexOf(to, start);
  assert.ok(start >= 0 && end > start, 'production loader exists');
  const code = ts.transpileModule(source.slice(start, end), { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None } }).outputText;
  return new Function(...Object.keys(scope), `${code}; return ${result};`)(...Object.values(scope));
}

// A slow choice list must not hold subscription/traffic reads behind it.
{
  const slow = deferred(), calls = [];
  const account = { id: 'u1', usedTraffic: 900719925, loginEnabled: true };
  const scope = {
    userId: ref('u1'), route: { name: 'UserDetail' }, user: ref(null), loginEnabled: ref(false), error: ref(''), choicesError: ref(''), choicesReady: ref(false),
    groups: ref([]), allNodes: ref([]), memberInbounds: ref([]), previewItems: ref([]), previewFailed: ref(false), previewLoaded: ref(false),
    userAPI: { get: async () => account, preview: async () => { calls.push('preview'); return { items: [] }; } },
    groupAPI: { list: () => slow.promise },
    nodeAPI: { choices: async () => { calls.push('choices'); return { items: [], total: 0 }; }, list: async () => { calls.push('full-nodes'); return { items: [], total: 0 }; } },
    proxyAPI: { inbounds: async () => ({ items: [], total: 0 }) },
    loadSubscription: async () => { calls.push('subscription'); }, loadPortalContext: async () => { calls.push('portal'); },
    loadActivity: () => { calls.push('activity'); }, showToast: () => {},
  };
  const load = extract('UserDetailPage', 'async function readPages', '\nfunction loadActivity', scope);
  const pending = load(); await tick();
  assert.equal(scope.user.value.usedTraffic, account.usedTraffic, 'usage is preserved exactly');
  assert.ok(calls.includes('subscription'), 'subscription must start before slow choices finish');
  assert.ok(calls.includes('activity'), 'traffic/activity must start before slow choices finish');
  assert.ok(calls.includes('preview'), 'preview must start before slow choices finish');
  assert.ok(calls.includes('choices'), 'detail uses lightweight node choices');
  assert.ok(!calls.includes('full-nodes'), 'detail does not fetch full tuning/node records');
  slow.resolve({ items: [], total: 0 }); await pending;
}

// User rows become visible independently from slow administrator/group options.
{
  const slow = deferred();
  const scope = { users: ref([]), total: ref(0), admins: ref([]), page: ref(1), keyword: ref(''), loading: ref(false), optionsLoading: ref(false), adminError: ref(''), error: ref(''),
    userAPI: { list: async () => ({ items: [{ id: 'u1', usedTraffic: 123456 }], total: 1 }) },
    request: () => slow.promise, showToast: () => {} };
  const load = extract('UserListPage', 'let generation = 0;', '\nfunction search()', scope);
  const pending = load(); await tick();
  assert.equal(scope.users.value[0]?.usedTraffic, 123456, 'user rows do not wait for unrelated choices');
  assert.equal(scope.loading.value, false, 'primary loading ends with user response');
  await load(true);
  assert.equal(scope.optionsLoading.value, true, 'pending administrators remain marked as loading');
  slow.resolve({ items: [{ id: 'a1' }], total: 1 }); await pending;
  assert.deepEqual(scope.admins.value, [{ id: 'a1' }], 'status refresh must not discard pending administrators');
  assert.equal(scope.optionsLoading.value, false);
}

{
  const errors = [], endpoint = { host: 'panel.example.test', httpPort: 443 };
  const scope = { userGeneration: 1, defaultFormat: ref(''), installEndpoint: ref(null), portalEndpointLoaded: ref(false), showToast: e => errors.push(e),
    settingsAPI: { runtimePolicy: async () => { throw new Error('policy unavailable'); }, installEndpoint: async () => endpoint } };
  const read = extract('UserDetailPage', 'async function loadPortalContext', '\nfunction absoluteLink', scope, 'loadPortalContext');
  await read();
  assert.deepEqual(scope.installEndpoint.value, endpoint, 'unrelated settings failure must not discard the real portal domain');
  assert.match(errors[0], /policy unavailable/);
}
// Status refresh must not invalidate a still-loading preview; old page results
// must nevertheless be ignored after a newer complete load starts.
{
  const preview = deferred(), late = deferred(), errors = [];
  let account = { id: 'u1', username: 'first', usedTraffic: 42, loginEnabled: true };
  let previewCall = 0;
  const scope = {
    userId: ref('u1'), route: { name: 'UserDetail' }, user: ref(null), loginEnabled: ref(false), error: ref(''), choicesError: ref(''), choicesReady: ref(false),
    groups: ref([]), allNodes: ref([]), memberInbounds: ref([]), previewItems: ref([]), previewFailed: ref(false), previewLoaded: ref(false),
    trafficSummary: ref(null), recentAccessList: ref([]), userOperations: ref([]), document: { hidden: false },
    userAPI: { get: async () => account, preview: () => ++previewCall === 1 ? preview.promise : late.promise },
    groupAPI: { list: async () => ({ items: [], total: 0 }) },
    nodeAPI: { choices: async () => ({ items: [], total: 0 }) }, proxyAPI: { inbounds: async () => ({ items: [], total: 0 }) },
    accessAPI: { accounts: async () => ({ uploadBytes: 17, downloadBytes: 25, observations: 2 }), list: async () => ({ items: [] }) },
    request: async () => ({ items: [] }), loadSubscription: async () => {}, loadPortalContext: async () => {}, showToast: e => errors.push(e),
  };
  const { load, refreshUserStatus } = extract('UserDetailPage', 'async function readPages', '\nconst refreshStatus', scope, '{ load, refreshUserStatus }');
  await load(); await refreshUserStatus();
  preview.resolve({ items: [{ nodeId: 'n1' }] }); await tick();
  assert.deepEqual(scope.previewItems.value, [{ nodeId: 'n1' }], 'status refresh retains independent preview response');
  await load();
  scope.userId.value = 'u2'; account = { ...account, id: 'u2', username: 'second' };
  // A failing auxiliary read must leave the successfully loaded quota visible.
  scope.groupAPI.list = async () => { throw new Error('groups unavailable'); };
  await load(); late.resolve({ items: [{ nodeId: 'n2' }] }); await tick();
  assert.equal(scope.user.value.id, 'u2');
  assert.equal(scope.user.value.usedTraffic, 42);
  assert.equal(scope.error.value, '', 'auxiliary failure must not hide the primary account');
  assert.match(scope.choicesError.value, /groups unavailable/);
}
console.log('PASS user loading: independent reads, choices, exact usage, refresh races and visible auxiliary errors');
