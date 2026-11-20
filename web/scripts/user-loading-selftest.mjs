import assert from 'node:assert/strict';
import { retainedQuery } from './retained-query-fixture.mjs';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import ts from 'typescript';
import { compile as compileTemplate, computed, reactive, watch } from 'vue';

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

function scriptSource(path) { return readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8'); }
function compile(source) {
  return ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ES2022 } }).outputText;
}
function moduleURL(source) { return `data:text/javascript;base64,${Buffer.from(compile(source)).toString('base64')}`; }
const apiURL = moduleURL(scriptSource('app/apiClient.ts'));
const client = await import(apiURL);
const { APIError, request, setCSRFToken, forgetPendingGet } = client;
const { userAPI } = await import(moduleURL(scriptSource('modules/users/api.ts').replace("'../../app/apiClient'", JSON.stringify(apiURL))));
const access = await import(moduleURL(scriptSource('modules/access/api.ts').replace("'../../app/apiClient'", JSON.stringify(apiURL))));
const detailSource = scriptSource('modules/users/pages/UserDetailPage.vue');
const detailScript = detailSource.split('<script setup lang="ts">')[1].split('</script>')[0];
const detailJS = compile(detailScript.replace(/^import\s[\s\S]*?;\r?$/gm, ''));
function detailRetry(page, condition) {
  const source = detailSource.match(new RegExp(`<([a-z]+) v-if="${condition}"[^>]*>[\\s\\S]*?<\\/\\1>`))[0];
  const { linkMessage, linkReadFailed, previewFailed } = page.scope;
  const feedback = compileTemplate(source)({
    linkMessage: linkMessage.value, linkReadFailed: linkReadFailed.value, previewFailed: previewFailed.value, load: page.load
  }, []);
  return Array.isArray(feedback.children) ? feedback.children.find(child => child.type === 'button') : undefined;
}
function detailHarness(fetchResponse) {
  const requests = [], errors = [];
  const fetchOriginal = globalThis.fetch;
  const route = reactive({ name: 'UserDetail', params: { id: 'u1' } });
  const authState = reactive({ currentUser: { role: 'Owner' } });
  const stops = [];
  setCSRFToken('detail-selftest');
  globalThis.fetch = (url, options) => {
    const path = url.replace(/^\/api\/v1/, '');
    requests.push({ path, method: options.method });
    return fetchResponse(path, options.method);
  };
  const dependencies = {
    ref: value => reactive({ value }), computed, reactive,
    watch: (...args) => stops.push(watch(...args)), onMounted: () => {}, onUnmounted: () => {},
    useRoute: () => route, useRouter: () => ({}), authState, throttleTrailing: f => f, document: { hidden: false },
    settingsAPI: { runtimePolicy: () => request('/system/runtime-policy'), installEndpoint: () => request('/system/install-endpoint') },
    userAPI, ...access,
    groupAPI: { list: async () => ({ items: [], total: 0 }) }, nodeAPI: { choices: async () => ({ items: [], total: 0 }) }, proxyAPI: { inbounds: async () => ({ items: [], total: 0 }) },
    APIError, request, forgetPendingGet, window: { location: { origin: 'https://panel.example.test' } }, generateQRCodeSVG: link => `二维码 ${link}`,
    localInput: value => value, utcInput: value => value, formatTime: value => value,
    showToast: message => errors.push(message)
  };
  const page = new Function(...Object.keys(dependencies), `${detailJS}
    return { load, refreshUserStatus, loadPortalContext, formatQR, doRotateSubscription, submitResetPassword,
      saveLogin, revokeSessions, submitRenew, submitResetTraffic, openEdit, save, openRenewDialog, openPasswordDialog,
      scope: { user, loginEnabled, loginPassword, error, choicesError, choicesReady,
        previewItems, previewFailed, previewLoaded, trafficSummary, recentAccessList, userOperations,
        subUrl, subFormats, subQrSvg, linkMessage, linkReadFailed,
        subscriptionEmptyHint, defaultFormat, installEndpoint, portalEndpointLoaded,
        drawer, form, formMeta, expiresLocal, trafficGiB, renewTargetTime, renewDialogOpen,
        passwordDialogOpen, revokeVisible, resetTrafficVisible, rotateConfirmVisible, saving } };`)(...Object.values(dependencies));
  const scope = { ...page.scope, groupAPI: dependencies.groupAPI, nodeAPI: dependencies.nodeAPI,
    userId: { get value() { return route.params.id; }, set value(id) { route.params.id = id; } } };
  return { ...page, scope, authState, requests, errors, restore: () => {
    stops.forEach(stop => stop()); globalThis.fetch = fetchOriginal; setCSRFToken('');
  } };
}

const json = (data, status = 200) => Response.json(data, { status });
const unavailable = () => json({ error: { message: 'offline' } }, 503);
const profile = (id = 'u1') => ({
  id, username: `name-${id}`, displayName: id, status: 'active', usedTraffic: 42, loginEnabled: true,
  expiresAt: '2028-06-01T00:00:00Z', trafficLimit: 2 ** 30, trafficResetCycle: 'monthly', trafficResetAnchor: '2026-07-17',
  deviceLimit: 2, nodeGroupIds: [`group-${id}`], remark: `remark-${id}`
});
const subscription = token => ({ path: `/sub/${token}`, formats: [{ id: 'clash', url: `/sub/${token}?format=clash` }] });
function detailResponse(path) {
  if (path === '/users/u1' || path === '/users/u2') return json(profile(path.slice(-2)));
  if (path.endsWith('/subscription-link')) return json(subscription(path.split('/')[2]));
  if (path.endsWith('/subscription-preview')) return json({ items: [{ nodeId: path.split('/')[2] }], total: 1, warnings: [] });
  if (path === '/system/runtime-policy') return json({ policy: { subscriptionFormat: 'clash' } });
  if (path === '/system/install-endpoint') return json({ host: 'example.test', httpPort: 443 });
  if (path.startsWith('/traffic/accounts')) return json({ uploadBytes: 17, downloadBytes: 25, observations: 2 });
  if (path.startsWith('/access-records') || path.startsWith('/operations')) return json({ items: [] });
  throw new Error(`unexpected request ${path}`);
}

const settle = () => new Promise(resolve => setImmediate(resolve));

for (const section of ['choices', 'operations']) test(`late denial after an auxiliary failure clears user details (${section})`, async t => {
  const denied = deferred();
  let refreshing = false;
  const page = detailHarness(path => {
    if (refreshing && section === 'operations' && path.startsWith('/operations')) {
      return path.endsWith('name-u1') ? unavailable() : denied.promise;
    }
    return detailResponse(path);
  });
  try {
    if (section === 'operations') {
      const original = Object.getOwnPropertyDescriptor(globalThis, 'window');
      Object.defineProperty(globalThis, 'window', { configurable: true, value: new EventTarget() });
      t.after(() => {
        if (original) Object.defineProperty(globalThis, 'window', original);
        else delete globalThis.window;
      });
    }
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    refreshing = true;
    if (section === 'choices') {
      page.scope.nodeAPI.choices = () => Promise.reject(new APIError(404, 'not_found', 'option missing'));
      page.scope.groupAPI.list = () => denied.promise;
    }
    const refresh = page.load();
    await settle();
    assert.ok(page.scope.user.value && page.scope.subQrSvg.value);
    if (section === 'choices') denied.reject(new APIError(403, 'forbidden', 'forbidden'));
    else denied.resolve(json({ error: { message: 'unauthorized' } }, 401));
    await refresh; await settle();
    assert.equal(page.scope.user.value, null, 'late denial clears the profile');
    assert.equal(page.scope.subUrl.value, '');
    assert.equal(page.scope.subQrSvg.value, '');
  } finally { page.restore(); }
});

test('retry after an auxiliary 403 starts new profile, traffic, access and operation requests', async () => {
  const pending = new Map();
  const ordinary = deferred();
  const profilePath = '/users/u1';
  const trafficPath = '/traffic/accounts/summary?userId=u1';
  const accessPath = '/access-records?userId=u1&page=1&pageSize=20';
  const operationPaths = ['name-u1', 'u1'].map(key => `/operations?page=1&pageSize=10&keyword=${key}`);
  const oldResponse = path => {
    const response = deferred(); pending.set(path, response); return response.promise;
  };
  let phase = 'initial';
  const page = detailHarness(path => {
    if (path === '/ordinary') return ordinary.promise;
    if (phase === 'initial' && [trafficPath, accessPath, ...operationPaths].includes(path)) return oldResponse(path);
    if (phase === 'denied' && path === profilePath) return oldResponse(path);
    if (phase === 'denied' && path.endsWith('/subscription-preview')) return json({ error: { code: 'forbidden' } }, 403);
    return detailResponse(path);
  });
  try {
    const ordinary1 = request('/ordinary');
    await page.load(); await settle();
    phase = 'denied'; const rejected = page.load(); await settle();
    assert.equal(page.scope.user.value, null);
    phase = 'retry'; const retrying = page.load();
    const ordinary2 = request('/ordinary');
    pending.get(profilePath).resolve(json({ ...profile(), displayName: 'old profile' }));
    pending.get(trafficPath).resolve(json({ uploadBytes: 2, downloadBytes: 3, observations: 1 }));
    pending.get(accessPath).resolve(json({ items: [{ id: 'old-access' }] }));
    for (const path of operationPaths) pending.get(path).resolve(json({ items: [{ id: 7, timestamp: '2026-11-19T00:00:00Z' }] }));
    ordinary.resolve(json({ ok: true }));
    await Promise.all([rejected, retrying, ordinary1, ordinary2]); await settle();
    assert.equal(page.scope.user.value.displayName, 'u1');
    assert.equal(page.scope.trafficSummary.value.downloadBytes, 25);
    assert.deepEqual(page.scope.recentAccessList.value, []);
    assert.deepEqual(page.scope.userOperations.value, []);
    for (const [path, expected] of [[profilePath, 3], ...[trafficPath, accessPath, ...operationPaths].map(path => [path, 2]), ['/ordinary', 1]]) {
      assert.equal(page.requests.filter(entry => entry.path === path).length, expected, path);
    }
  } finally { page.restore(); }
});

test('losing Owner access closes all user dialogs and erases copied form data', async () => {
  const page = detailHarness(detailResponse);
  try {
    await page.load(); await settle();
    page.openEdit(); page.openRenewDialog(); page.openPasswordDialog();
    const scope = page.scope;
    scope.revokeVisible.value = scope.resetTrafficVisible.value = scope.rotateConfirmVisible.value = true;
    scope.loginPassword.value = 'typed password';
    assert.equal(scope.drawer.value, true);
    assert.equal(scope.form.username, 'name-u1');
    assert.equal(scope.formMeta.rawRemark, 'remark-u1');
    page.authState.currentUser.role = 'Viewer';
    for (const name of ['drawer', 'renewDialogOpen', 'passwordDialogOpen', 'revokeVisible', 'resetTrafficVisible', 'rotateConfirmVisible']) {
      assert.equal(scope[name].value, false, name);
    }
    for (const name of ['username', 'displayName', 'remark', 'expiresAt', 'trafficResetAnchor']) assert.equal(scope.form[name], '', name);
    assert.deepEqual(scope.form.nodeGroupIds, []);
    assert.equal(scope.formMeta.rawRemark, '');
    assert.equal(scope.loginPassword.value, '');
    assert.equal(scope.renewTargetTime.value, '');
    assert.equal(scope.expiresLocal.value, '');
    assert.equal(scope.trafficGiB.value, 0);
  } finally { page.restore(); }
});

for (const [action, status] of [['saveLogin', 401], ['saveLogin', 403], ['revokeSessions', 403], ['submitRenew', 403],
  ['submitResetTraffic', 403], ['save', 403], ['doRotateSubscription', 403], ['submitResetPassword', 403]]) {
  test(`${action} clears the current user and links on HTTP ${status}`, async () => {
    const page = detailHarness((path, method) => method === 'GET' ? detailResponse(path) : json({ error: { code: status === 401 ? 'unauthorized' : 'forbidden' } }, status));
    const originalWindow = globalThis.window;
    globalThis.window = { dispatchEvent: () => {} };
    try {
      await page.load(); await settle();
      page.openEdit(); page.openRenewDialog(); page.formatQR('/sub/u1?format=clash');
      await page[action]();
      assert.equal(page.scope.user.value, null);
      assert.equal(page.scope.subUrl.value, '');
      assert.equal(page.scope.subQrSvg.value, '');
      assert.match(page.scope.error.value, /登录已过期|权限/);
    } finally { page.restore(); globalThis.window = originalWindow; }
  });
}

for (const status of [204, 403]) test(`write response ${status} from a prior identity lifetime cannot mutate a reopened user`, async () => {
  const response = deferred();
  const page = detailHarness((path, method) => method === 'PUT' ? response.promise : detailResponse(path));
  try {
    await page.load(); await settle();
    const saving = page.saveLogin();
    page.scope.userId.value = 'u2'; await page.load();
    page.scope.userId.value = 'u1'; await page.load(); await settle();
    page.scope.loginPassword.value = 'new edit';
    const requests = page.requests.length, toasts = page.errors.length;
    response.resolve(status === 204 ? new Response(null, { status }) : json({ error: { code: 'forbidden' } }, status));
    await saving; await settle();
    assert.equal(page.scope.user.value.id, 'u1');
    assert.equal(page.scope.loginPassword.value, 'new edit');
    assert.equal(page.requests.length, requests);
    assert.equal(page.errors.length, toasts);
  } finally { page.restore(); }
});

for (const [action, status] of [['doRotateSubscription', 200], ['submitResetPassword', 200], ['submitResetPassword', 403]]) {
  test(`${action} waits for the primary refresh before displaying replacement data (${status})`, async () => {
    const primary = deferred();
    let refreshing = false, replaced = false;
    const page = detailHarness((path, method) => {
      if (method === 'POST') { replaced = true; return new Response(null, { status: 204 }); }
      if (refreshing && path === '/users/u1') return primary.promise;
      if (replaced && path.endsWith('/subscription-link')) return json(subscription('replaced'));
      if (replaced && path.endsWith('/subscription-preview')) return json({ items: [{ nodeId: 'replaced' }] });
      return detailResponse(path);
    });
    try {
      await page.load(); await settle();
      page.formatQR('/sub/u1?format=clash');
      refreshing = true; const refresh = page.load();
      const resetting = page[action](); await settle();
      assert.equal(page.scope.subUrl.value, '');
      assert.equal(page.scope.subQrSvg.value, '');
      if (action === 'submitResetPassword') assert.deepEqual(page.scope.previewItems.value, []);
      primary.resolve(status === 200 ? json(profile()) : json({ error: { code: 'forbidden' } }, status));
      await Promise.all([refresh, resetting]); await settle();
      assert.equal(page.scope.subUrl.value, status === 200 ? 'https://panel.example.test/sub/replaced' : '');
      if (action === 'submitResetPassword') assert.deepEqual(page.scope.previewItems.value, status === 200 ? [{ nodeId: 'replaced' }] : []);
    } finally { page.restore(); }
  });
}

for (const action of ['doRotateSubscription', 'submitResetPassword']) {
  test(`${action} recovers fresh results after a failed background profile refresh`, async () => {
    let phase = 'initial';
    const recoveredProfile = deferred();
    const page = detailHarness((path, method) => {
      if (method === 'POST') { phase = 'replaced'; return new Response(null, { status: 204 }); }
      if (path === '/users/u1' && phase === 'offline') return unavailable();
      if (path === '/users/u1' && phase === 'replaced') return recoveredProfile.promise;
      if (phase === 'replaced' && path.endsWith('/subscription-link')) return json(subscription('recovered'));
      if (phase === 'replaced' && path.endsWith('/subscription-preview')) return json({ items: [{ nodeId: 'recovered' }] });
      return detailResponse(path);
    });
    try {
      await page.load(); await settle();
      page.scope.loginEnabled.value = false;
      page.formatQR('/sub/u1?format=clash');
      phase = 'offline'; await page.refreshUserStatus();
      assert.ok(page.errors.includes('用户状态刷新失败：offline'));
      const resetting = page[action](); await settle();
      assert.equal(page.scope.subUrl.value, '', 'the link stays clear until the profile is reconfirmed');
      assert.equal(page.scope.subQrSvg.value, '');
      assert.equal(page.requests.filter(r => r.path === '/users/u1').length, 3, 'a successful reset after a 503 reconfirms the profile');
      recoveredProfile.resolve(json(profile())); await resetting; await settle();
      assert.equal(page.scope.loginEnabled.value, false, 'reconfirmation preserves an unsaved portal toggle');
      assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/recovered');
      if (action === 'submitResetPassword') assert.deepEqual(page.scope.previewItems.value, [{ nodeId: 'recovered' }]);
      assert.equal(page.requests.filter(r => r.method === 'POST').length, 1);
    } finally { page.restore(); }
  });
}

test('if reset reconfirmation also fails, a later status success leaves a way to reload the new link', async () => {
  let offline = false, replaced = false;
  const page = detailHarness((path, method) => {
    if (method === 'POST') { replaced = true; return new Response(null, { status: 204 }); }
    if (path === '/users/u1' && offline) return unavailable();
    if (replaced && path.endsWith('/subscription-link')) return json(subscription('recovered'));
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    offline = true; await page.refreshUserStatus();
    await page.doRotateSubscription();
    assert.equal(page.scope.subUrl.value, '');
    assert.match(page.scope.error.value, /offline/);
    assert.equal(page.scope.subscriptionEmptyHint.value, '重试后可查看');
    await page.load(); await settle();
    assert.match(page.scope.error.value, /offline/, 'retry remains available after a failed manual reload');
    offline = false; await page.refreshUserStatus();
    assert.match(page.scope.error.value, /offline/, 'status alone has not reloaded the new subscription');
    await page.load(); await settle();
    assert.equal(page.scope.error.value, '');
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/recovered');
    assert.equal(page.requests.filter(r => r.method === 'POST').length, 1);
  } finally { page.restore(); }
});

test('the token replacement can be read again when a confirmed reload still fails to read the link', async () => {
  let phase = 'initial';
  const replacement = deferred();
  const page = detailHarness((path, method) => {
    if (method === 'POST') { phase = 'reset'; return new Response(null, { status: 204 }); }
    if (path === '/users/u1' && ['offline', 'reset'].includes(phase)) return unavailable();
    if (path.endsWith('/subscription-link') && phase === 'link-failure') return unavailable();
    if (path.endsWith('/subscription-link') && phase === 'healthy') return replacement.promise;
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    phase = 'offline'; await page.refreshUserStatus();
    await page.doRotateSubscription();
    assert.match(page.scope.error.value, /offline/);
    phase = 'link-failure'; await page.load(); await settle();
    assert.equal(page.scope.user.value.id, 'u1');
    assert.equal(page.scope.subUrl.value, '');
    assert.match(page.scope.linkMessage.value, /offline/);
    const retry = detailRetry(page, 'linkMessage');
    assert.equal(retry?.children.trim(), '重新读取', 'the link error offers a read-only retry');
    assert.equal(page.scope.subscriptionEmptyHint.value, '重试后可查看');
    phase = 'healthy'; await retry.props.onClick(); await settle();
    assert.ok(detailRetry(page, 'linkMessage'), 'the retry stays available until the link returns');
    replacement.resolve(json(subscription('replacement'))); await settle();
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/replacement');
    assert.equal(detailRetry(page, 'linkMessage'), undefined);
    assert.equal(page.requests.filter(entry => entry.method === 'POST').length, 1);
  } finally { page.restore(); }
});

test('after credential reset, failed preview has its own read-only retry even with a valid replacement link', async () => {
  let phase = 'initial';
  const replacement = deferred();
  const page = detailHarness((path, method) => {
    if (method === 'POST') { phase = 'preview-failure'; return new Response(null, { status: 204 }); }
    if (path.endsWith('/subscription-link') && phase !== 'initial') return json(subscription('replacement'));
    if (path.endsWith('/subscription-preview') && phase === 'preview-failure') return unavailable();
    if (path.endsWith('/subscription-preview') && phase === 'healthy') return replacement.promise;
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    await page.submitResetPassword(); await settle();
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/replacement');
    assert.deepEqual(page.scope.previewItems.value, []);
    assert.equal(page.scope.previewFailed.value, true);
    const retry = detailRetry(page, 'previewFailed');
    assert.equal(retry?.children.trim(), '重新读取', 'the preview error offers a read-only retry');
    phase = 'healthy'; await retry.props.onClick(); await settle();
    assert.ok(detailRetry(page, 'previewFailed'), 'the retry stays available until preview returns');
    replacement.resolve(json({ items: [{ nodeId: 'replacement' }] })); await settle();
    assert.deepEqual(page.scope.previewItems.value, [{ nodeId: 'replacement' }]);
    assert.equal(detailRetry(page, 'previewFailed'), undefined);
    assert.equal(page.requests.filter(entry => entry.method === 'POST').length, 1);
  } finally { page.restore(); }
});

test('ID-only reads launch before a blocked profile while operation records wait', async () => {
  const primary = deferred();
  const page = detailHarness(path => path === '/users/u1' ? primary.promise : detailResponse(path));
  try {
    const loading = page.load(); await tick();
    const paths = page.requests.map(({ path }) => path);
    for (const path of ['/users/u1/subscription-link', '/users/u1/subscription-preview', '/traffic/accounts/summary?userId=u1', '/access-records?userId=u1&page=1&pageSize=20']) {
      assert.ok(paths.includes(path), `${path} must start while the profile is pending`);
    }
    assert.ok(!paths.some(path => path.startsWith('/operations')), 'operation reads wait for the username');
    primary.resolve(json(profile())); await loading; await settle();
    assert.ok(page.requests.some(({ path }) => path.includes('keyword=name-u1')));
    assert.equal(page.scope.user.value.usedTraffic, 42);
    assert.equal(page.scope.trafficSummary.value.downloadBytes, 25);
  } finally { page.restore(); }
});

test('pending and failed same-user refreshes retain successful details', async () => {
  const refresh = deferred(), pendingPreview = deferred();
  let refreshing = false;
  const page = detailHarness(path => {
    if (!refreshing) return detailResponse(path);
    if (path === '/users/u1') return refresh.promise;
    if (path.endsWith('/subscription-preview')) return pendingPreview.promise;
    return unavailable();
  });
  try {
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    refreshing = true;
    const loading = page.load(); await tick();
    assert.equal(page.scope.user.value.usedTraffic, 42);
    assert.equal(page.scope.portalEndpointLoaded.value, true, 'the successful portal URL stays visible during refresh');
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/u1');
    assert.equal(page.scope.subQrSvg.value, '二维码 https://panel.example.test/sub/u1?format=clash');
    refresh.resolve(unavailable()); pendingPreview.resolve(unavailable()); await loading; await settle();
    assert.match(page.scope.error.value, /offline/);
    assert.equal(page.scope.user.value.usedTraffic, 42);
    assert.equal(page.scope.installEndpoint.value.host, 'example.test');
    assert.equal(page.scope.defaultFormat.value, 'clash');
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/u1');
    assert.deepEqual(page.scope.subFormats.value, subscription('u1').formats);
    assert.equal(page.scope.subQrSvg.value, '二维码 https://panel.example.test/sub/u1?format=clash');
    assert.match(page.scope.linkMessage.value, /offline/);
    assert.equal(page.scope.previewFailed.value, true);
    assert.ok(page.errors.some(message => message === '流量记录读取失败：offline'));
    assert.ok(page.errors.some(message => message === '访问记录读取失败：offline'));
  } finally { page.restore(); }
});

test('changing route IDs clears the old identity and discards its late reads', async () => {
  const primary = deferred(), lateLink = deferred();
  let refreshing = false;
  const page = detailHarness(path => {
    if (path === '/users/u2') return primary.promise;
    if (refreshing && path === '/users/u1/subscription-link') return lateLink.promise;
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    refreshing = true; await page.load();
    page.scope.userId.value = 'u2';
    const switching = page.load(); await settle();
    assert.equal(page.scope.user.value, null);
    assert.equal(page.scope.subUrl.value, '');
    assert.equal(page.scope.subQrSvg.value, '');
    assert.deepEqual(page.scope.previewItems.value, []);
    primary.resolve(json(profile('u2'))); await switching;
    lateLink.resolve(json(subscription('old'))); await settle();
    assert.equal(page.scope.user.value.id, 'u2');
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/u2');
  } finally { page.restore(); }
});

for (const status of [401, 403, 404]) test(`profile ${status} clears prior data and discards late reads`, async () => {
  const primary = deferred(), lateLink = deferred(), latePreview = deferred();
  let refreshing = false;
  const page = detailHarness(path => {
    if (refreshing && path === '/users/u1') return primary.promise;
    if (refreshing && path.endsWith('/subscription-link')) return lateLink.promise;
    if (refreshing && path.endsWith('/subscription-preview')) return latePreview.promise;
    return detailResponse(path);
  });
  const windowOriginal = globalThis.window;
  globalThis.window = { dispatchEvent: () => {} };
  try {
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    refreshing = true;
    const loading = page.load(); await tick();
    primary.resolve(json({ error: { message: `profile error ${status}` } }, status)); await loading;
    assert.equal(page.scope.user.value, null);
    assert.match(page.scope.error.value, status === 401 ? /登录已过期|profile error/ : /profile error/);
    assert.equal(page.scope.subUrl.value, '');
    assert.equal(page.scope.subQrSvg.value, '');
    lateLink.resolve(json(subscription('late'))); latePreview.resolve(json({ items: [{ nodeId: 'old' }] })); await settle();
    assert.equal(page.scope.subUrl.value, '', 'a late link cannot restore a rejected profile');
    assert.deepEqual(page.scope.previewItems.value, []);
  } finally { page.restore(); globalThis.window = windowOriginal; }
});

test('after denial new sensitive reads avoid old in-flight GETs while ordinary GETs still share', async () => {
  const lateLink = deferred(), latePreview = deferred(), ordinary = deferred(), replacement = deferred();
  let phase = 'initial';
  const page = detailHarness(path => {
    if (path === '/ordinary') return ordinary.promise;
    if (phase === 'denied' && path === '/users/u1') return json({ error: { code: 'forbidden' } }, 403);
    if (path.endsWith('/subscription-link')) {
      if (phase === 'denied') return lateLink.promise;
      if (phase === 'retry') return replacement.promise;
    }
    if (path.endsWith('/subscription-preview') && phase === 'denied') return latePreview.promise;
    return detailResponse(path);
  });
  try {
    const firstOrdinary = request('/ordinary'), secondOrdinary = request('/ordinary');
    await page.load(); await settle();
    phase = 'denied'; await page.load(); await settle();
    phase = 'retry'; await page.load();
    lateLink.resolve(json(subscription('revoked')));
    latePreview.resolve(json({ items: [{ nodeId: 'revoked' }] })); await settle();
    assert.equal(page.requests.filter(({ path }) => path === '/ordinary').length, 1);
    ordinary.resolve(json({ ok: true })); await Promise.all([firstOrdinary, secondOrdinary]);
    assert.equal(page.requests.filter(({ path }) => path === '/users/u1/subscription-link').length, 3);
    assert.equal(page.scope.subUrl.value, '', 'retry cannot expose the old GET result');
    assert.deepEqual(page.scope.previewItems.value, [{ nodeId: 'u1' }], 'retry uses the new preview response');
    replacement.resolve(json(subscription('replacement'))); await settle();
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/replacement');
  } finally { page.restore(); }
});

for (const code of ['not_found', 'link_not_recoverable']) test(`subscription ${code} clears the link even with a pending profile`, async () => {
  const primary = deferred();
  let refreshing = false;
  const page = detailHarness(path => {
    if (refreshing && path === '/users/u1') return primary.promise;
    if (refreshing && path.endsWith('/subscription-link')) return json({ error: { code } }, 404);
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    refreshing = true;
    const loading = page.load(); await settle();
    assert.equal(page.scope.subUrl.value, '');
    assert.deepEqual(page.scope.subFormats.value, []);
    assert.equal(page.scope.subQrSvg.value, '');
    assert.match(page.scope.linkMessage.value, /重置/);
    assert.equal(detailRetry(page, 'linkMessage'), undefined, 'a missing link uses its explicit reset action');
    primary.resolve(json(profile())); await loading;
  } finally { page.restore(); }
});

for (const reset of ['doRotateSubscription', 'submitResetPassword']) test(`${reset} discards an older link read while the replacement is pending`, async () => {
  const lateLink = deferred(), newLink = deferred();
  let linkReads = 0;
  const page = detailHarness((path, method) => {
    if (method === 'POST') return new Response(null, { status: 204 });
    if (path.endsWith('/subscription-link')) {
      linkReads++;
      if (linkReads === 2) return lateLink.promise;
      if (linkReads === 3) return newLink.promise;
    }
    return detailResponse(path);
  });
  try {
    await page.load(); await settle();
    page.formatQR('/sub/u1?format=clash');
    await page.load(); await tick();
    const resetting = page[reset](); await settle();
    assert.equal(page.scope.subUrl.value, '', `${reset} clears the invalid link while awaiting its replacement`);
    assert.equal(page.scope.subQrSvg.value, '');
    assert.equal(linkReads, 3, 'reset initiates a fresh GET');
    lateLink.resolve(json(subscription('stale'))); await settle();
    assert.equal(page.scope.subUrl.value, '', 'the invalid request cannot repopulate the link');
    newLink.resolve(json(subscription('new'))); await resetting; await settle();
    assert.equal(page.scope.subUrl.value, 'https://panel.example.test/sub/new');
  } finally { page.restore(); }
});

test('slow node choices leave exact usage and independent detail reads visible', async () => {
  const slow = deferred(), nodeCalls = [];
  const page = detailHarness(path => path === '/users/u1' ? json({ ...profile(), usedTraffic: 900719925 }) : detailResponse(path));
  const scope = page.scope;
  scope.groupAPI.list = () => slow.promise;
  scope.nodeAPI.choices = async () => { nodeCalls.push('choices'); return { items: [], total: 0 }; };
  scope.nodeAPI.list = async () => { nodeCalls.push('full-nodes'); return { items: [], total: 0 }; };
  try {
    const pending = page.load(); await settle();
    const calls = page.requests.map(({ path }) => path);
    assert.equal(scope.user.value.usedTraffic, 900719925, 'usage is preserved exactly');
    assert.ok(calls.includes('/users/u1/subscription-link'), 'subscription must start before slow choices finish');
    assert.ok(calls.includes('/traffic/accounts/summary?userId=u1'), 'traffic/activity must start before slow choices finish');
    assert.ok(calls.includes('/users/u1/subscription-preview'), 'preview must start before slow choices finish');
    assert.ok(nodeCalls.includes('choices'), 'detail uses lightweight node choices');
    assert.ok(!nodeCalls.includes('full-nodes'), 'detail does not fetch full tuning/node records');
    slow.resolve({ items: [], total: 0 }); await pending;
  } finally { page.restore(); }
});

test('user rows load independently of slow administrator options', async () => {
  const slow = deferred();
  const scope = { retainedQuery, users: ref([]), total: ref(0), admins: ref([]), page: ref(1), keyword: ref(''), loading: ref(false), optionsLoading: ref(false), adminError: ref(''), error: ref(''),
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
});

test('portal address loads despite an unrelated format setting failure', async () => {
  const endpoint = { host: 'panel.example.test', httpPort: 443 };
  const page = detailHarness(path => path === '/system/runtime-policy' ? json({ error: { message: 'policy unavailable' } }, 503) : json(endpoint));
  try {
    await page.loadPortalContext();
    assert.deepEqual(page.scope.installEndpoint.value, endpoint, 'unrelated settings failure must not discard the real portal domain');
    assert.match(page.errors[0], /policy unavailable/);
  } finally { page.restore(); }
});

test('status refresh keeps an in-flight preview but a route change discards it', async () => {
  const preview = deferred(), late = deferred();
  let first = true;
  const page = detailHarness(path => {
    if (path === '/users/u1/subscription-preview') return first ? preview.promise : late.promise;
    return detailResponse(path);
  });
  const scope = page.scope;
  try {
    await page.load(); await page.refreshUserStatus();
    preview.resolve(json({ items: [{ nodeId: 'n1' }] })); await settle();
    assert.deepEqual(scope.previewItems.value, [{ nodeId: 'n1' }], 'status refresh retains independent preview response');
    first = false; await page.load();
    scope.userId.value = 'u2';
    // A failing auxiliary read must leave the successfully loaded quota visible.
    scope.groupAPI.list = async () => { throw new Error('groups unavailable'); };
    await page.load(); late.resolve(json({ items: [{ nodeId: 'n2' }] })); await settle();
    assert.equal(scope.user.value.id, 'u2');
    assert.equal(scope.user.value.usedTraffic, 42);
    assert.equal(scope.error.value, '', 'auxiliary failure must not hide the primary account');
    assert.match(scope.choicesError.value, /groups unavailable/);
    assert.deepEqual(scope.previewItems.value, [{ nodeId: 'u2' }]);
  } finally { page.restore(); }
});
