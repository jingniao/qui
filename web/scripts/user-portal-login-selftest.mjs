import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// Pure modules (no imports), transpiled and loaded directly.
async function load(rel) {
  const source = readFileSync(new URL(rel, import.meta.url), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const portal = await load('../src/modules/users/portalLogin.ts');
const entry = await load('../src/modules/system/adminEntry.ts');

// Password rule: 12-128 characters, both entries equal.
assert.match(portal.portalPasswordError('short', 'short'), /12～128/);
assert.match(portal.portalPasswordError('x'.repeat(129), 'x'.repeat(129)), /12～128/);
assert.match(portal.portalPasswordError('a'.repeat(12), 'b'.repeat(12)), /不一致/);
assert.equal(portal.portalPasswordError('a'.repeat(12), 'a'.repeat(12)), '');
assert.equal(portal.portalPasswordError('密'.repeat(12), '密'.repeat(12)), '', 'length counts characters, not bytes');

// Two-step save: success path.
{
  const calls = [];
  const result = await portal.createUserWithPortalLogin({
    create: async () => { calls.push('create'); return { id: 'u1' }; },
    setLogin: async (id, password) => { calls.push(`login:${id}:${password}`); }
  }, 'correct-horse-battery');
  assert.deepEqual(calls, ['create', 'login:u1:correct-horse-battery']);
  assert.equal(result.loginSet, true);
  assert.equal(result.loginError, '');
  assert.equal(result.user.id, 'u1');
}
// Not ticked: only the create call.
{
  const calls = [];
  const result = await portal.createUserWithPortalLogin({
    create: async () => { calls.push('create'); return { id: 'u2' }; },
    setLogin: async () => { calls.push('login'); }
  }, null);
  assert.deepEqual(calls, ['create']);
  assert.equal(result.loginSet, false);
  assert.equal(result.loginError, '');
}
// Second step fails: the user exists, the reason is returned, nothing is created twice.
{
  const calls = [];
  const result = await portal.createUserWithPortalLogin({
    create: async () => { calls.push('create'); return { id: 'u3' }; },
    setLogin: async () => { calls.push('login'); throw new Error('密码不符合要求'); }
  }, 'correct-horse-battery');
  assert.deepEqual(calls, ['create', 'login']);
  assert.equal(result.user.id, 'u3', 'caller navigates to this user');
  assert.equal(result.loginSet, false);
  assert.equal(result.loginError, '密码不符合要求');
  assert.equal(portal.portalLoginFailureMessage(result.loginError), '用户已创建，门户登录未设置：密码不符合要求');
}
// First step fails: nothing else runs and the error propagates (no user exists).
{
  const calls = [];
  await assert.rejects(portal.createUserWithPortalLogin({
    create: async () => { calls.push('create'); throw new Error('用户名已存在'); },
    setLogin: async () => { calls.push('login'); }
  }, 'correct-horse-battery'), /用户名已存在/);
  assert.deepEqual(calls, ['create']);
}

// Portal address.
assert.equal(portal.portalAddress({ host: 'panel.example.com', httpPort: 22550 }, 'x:1'), 'https://panel.example.com:22550/');
assert.equal(portal.portalAddress({ host: 'panel.example.com', httpPort: 443 }, 'x:1'), 'https://panel.example.com/');
assert.equal(portal.portalAddress({ host: 'panel.example.com', httpPort: 0 }, 'x:1'), 'https://panel.example.com/');
assert.equal(portal.portalAddress({ host: '', httpPort: 22550 }, 'browser.example:22550'), 'https://browser.example:22550/');
assert.equal(portal.portalAddress(null, 'browser.example'), 'https://browser.example/');
assert.equal(portal.portalShareText('https://p/', 'https://p/sub/t'), '门户地址：https://p/\n订阅链接：https://p/sub/t');

// Admin entry path validation.
for (const ok of ['/login', '/manage-7f3k', '/a_b-1', '/abc','/' + 'a'.repeat(63)]) assert.equal(entry.adminEntryPathError(ok), '', ok);
for (const bad of ['login', '/ab', '/Login', '/a/b', '/a b', '/' + 'a'.repeat(64), '//abc', '/ab.c', '']) assert.notEqual(entry.adminEntryPathError(bad), '', bad || '(empty)');
assert.equal(entry.adminEntryURL('https://p.example', '/manage-7f3k'), 'https://p.example/manage-7f3k');

// Wiring: the drawer, detail page and settings use these helpers and the new texts.
const read = rel => readFileSync(new URL(`../src/${rel}`, import.meta.url), 'utf8');
const list = read('modules/users/pages/UserListPage.vue');
assert.match(list, /代理凭据与用户门户登录使用的名称/);
assert.match(list, /createUserWithPortalLogin/);
assert.match(list, /允许登录用户门户/);
const detail = read('modules/users/pages/UserDetailPage.vue');
assert.doesNotMatch(detail, /可直接复制或导入/);
assert.match(detail, /复制门户地址和订阅链接/);
assert.match(detail, /orderedSubscriptionClients/);
assert.match(detail, /AUTO_SUBSCRIPTION_LABEL/);
assert.match(read('modules/system/components/RuntimeSettingsPanel.vue'), /Clash Verge／Mihomo Party／Clash Meta／FlClash → Clash Meta（mihomo）/);
const settings = read('modules/system/pages/SettingsPage.vue');
assert.match(settings, /<AdminEntryPanel v-if="isOwner && activeTab === 'security'"/);
assert.match(read('modules/system/settingsApi.ts'), /'\/system\/admin-entry', 'PUT', \{ path, expectedRevision \}/);

console.log('PASS USER PORTAL LOGIN: two-step save (success, second-step failure, first-step failure), portal address, admin entry validation, wiring');
