import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// Load parseRenewPrice and mergeLatestEditForm directly from source
const nodeMetaSrc = readFileSync(new URL('../src/modules/nodes/nodeMeta.ts', import.meta.url), 'utf8');
const modelSrc = readFileSync(new URL('../src/modules/nodes/components/serverListModel.ts', import.meta.url), 'utf8');

const parseRenewPriceCode = nodeMetaSrc.match(/export function parseRenewPrice[\s\S]*?^}/m)?.[0];
assert.ok(parseRenewPriceCode, 'parseRenewPrice found in nodeMeta.ts');

const mergeLatestCode = modelSrc.match(/export function mergeLatestEditForm[\s\S]*?^}/m)?.[0];
assert.ok(mergeLatestCode, 'mergeLatestEditForm found in serverListModel.ts');

const transpiled = ts.transpileModule(`
  ${parseRenewPriceCode.replace('export ', '')}
  ${mergeLatestCode.replace('export ', '')}
  return { parseRenewPrice, mergeLatestEditForm };
`, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None } }).outputText;

const { mergeLatestEditForm } = new Function(transpiled)();

// 1. Test mergeLatestEditForm
{
  const baseline = {
    name: 'Old Server',
    region: 'HK',
    remark: 'Old remark',
    renewDate: '2026-12-01',
    renewCurrency: 'CNY',
    renewPrice: '100',
    renewCycle: '/年',
    enabled: true,
    allowSubscription: true,
    maintenance: false
  };

  // Case A: User has not touched anything. All fields from latest node & meta should apply.
  const currentA = { ...baseline };
  const latestNodeA = {
    name: 'New Server Name',
    enabled: false,
    allowSubscription: false,
    maintenance: true
  };
  const latestMetaA = {
    region: 'JP',
    remark: 'New remark',
    renewDate: '2027-01-01',
    renewPrice: '200',
    renewCurrency: 'USD',
    renewCycle: '/月'
  };
  mergeLatestEditForm(currentA, baseline, latestNodeA, latestMetaA);
  assert.equal(currentA.name, 'New Server Name');
  assert.equal(currentA.enabled, false);
  assert.equal(currentA.allowSubscription, false);
  assert.equal(currentA.maintenance, true);
  assert.equal(currentA.region, 'JP');
  assert.equal(currentA.remark, 'New remark');
  assert.equal(currentA.renewDate, '2027-01-01');
  assert.equal(currentA.renewCurrency, 'USD');
  assert.equal(currentA.renewPrice, '200');
  assert.equal(currentA.renewCycle, '/月');

  // Case B: User has edited 'name' and 'maintenance', untouched other fields.
  const currentB = {
    ...baseline,
    name: 'User Edited Server Name',
    maintenance: true
  };
  mergeLatestEditForm(currentB, baseline, latestNodeA, latestMetaA);
  assert.equal(currentB.name, 'User Edited Server Name', 'user edited name must be preserved');
  assert.equal(currentB.maintenance, true, 'user edited maintenance must be preserved');
  assert.equal(currentB.region, 'JP', 'untouched region must be updated');
  assert.equal(currentB.remark, 'New remark', 'untouched remark must be updated');
  assert.equal(currentB.renewPrice, '200', 'untouched price must be updated');
  assert.equal(currentB.enabled, false, 'untouched enabled must be updated');
}

// 2. Component inspection: NodeListPage.vue
{
  const pageSource = readFileSync(new URL('../src/modules/nodes/pages/NodeListPage.vue', import.meta.url), 'utf8');

  // Must import throttleTrailing
  assert.match(pageSource, /import\s*\{[^}]*throttleTrailing[^}]*\}\s*from/, 'imports throttleTrailing');

  // Must listen to proxy-node-status and visibilitychange
  assert.match(pageSource, /window\.addEventListener\(['"]proxy-node-status['"]/, 'listens to proxy-node-status');
  assert.match(pageSource, /document\.addEventListener\(['"]visibilitychange['"]/, 'listens to visibilitychange');

  // Must clean up listeners on unmount
  assert.match(pageSource, /window\.removeEventListener\(['"]proxy-node-status['"]/, 'removes proxy-node-status listener');
  assert.match(pageSource, /document\.removeEventListener\(['"]visibilitychange['"]/, 'removes visibilitychange listener');
  assert.match(pageSource, /clearInterval\(fallbackTimer\)/, 'clears fallbackTimer on unmount');
  assert.match(pageSource, /throttledSilentRefresh\.cancel\(\)/, 'cancels throttledSilentRefresh on unmount');

  // 30s fallback timer only when visible
  assert.match(pageSource, /30000/, 'has 30s interval for fallback timer');
  assert.match(pageSource, /document\.visibilityState\s*===\s*['"]visible['"]/, 'checks visibilityState for fallback timer');

  // silentRefresh must not trigger full loading or reload meta/traffic
  assert.match(pageSource, /async function silentRefresh\(\)/, 'defines silentRefresh function');
  assert.doesNotMatch(pageSource, /silentRefresh[\s\S]*?loadNodeMeta/, 'silentRefresh must not reload node metadata');
  assert.doesNotMatch(pageSource, /silentRefresh[\s\S]*?accounts/, 'silentRefresh must not reload accounts/traffic');
}

// 3. Component inspection: ServerQuickEdit.vue
{
  const editSource = readFileSync(new URL('../src/modules/nodes/components/ServerQuickEdit.vue', import.meta.url), 'utf8');

  // Must display "正在读取最新信息" and handle fetchError
  assert.match(editSource, /正在读取最新信息/, 'shows "正在读取最新信息" banner during fetch');
  assert.match(editSource, /fetchError/, 'shows fetchError banner if fetch fails');

  // Save button must be disabled during fetchingLatest
  assert.match(editSource, /:loading="editingSaving \|\| fetchingLatest"/, 'save button is disabled during background fetch');

  // openEditDrawer must open drawer immediately before background Promise.all
  const openFn = editSource.match(/async function openEditDrawer[\s\S]*?^}/m)?.[0] || '';
  assert.ok(openFn, 'found openEditDrawer');
  const drawerOpenIdx = openFn.indexOf('editDrawer.value = true');
  const promiseAllIdx = openFn.indexOf('Promise.all');
  assert.ok(drawerOpenIdx > 0 && promiseAllIdx > 0, 'drawer open and Promise.all both present');
  assert.ok(drawerOpenIdx < promiseAllIdx, 'drawer is opened BEFORE Promise.all');

  // 404 / deletedAt handling: closes drawer and toasts
  assert.match(openFn, /showToast\(['"]服务器已被删除['"]/, 'toasts deleted server on 404 or deletedAt');
}

console.log('server-list-refresh-selftest passed');
