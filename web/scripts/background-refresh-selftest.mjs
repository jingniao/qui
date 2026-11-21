import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const read = p => readFileSync(new URL('../src/' + p, import.meta.url), 'utf8');
const compile = s => ts.transpileModule(s, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const ref = value => ({ value });
const tick = () => new Promise(resolve => setImmediate(resolve));
const reject = async () => { throw new Error('offline'); };
const bell = read('components/NotificationBell.vue');
const bellCode = bell.slice(bell.indexOf('let generation ='), bell.indexOf('const refreshNotifications ='));
const items = ref([{ id: 'old' }]), unreadCount = ref(3), isOpen = ref(false), toasts = [];
const unreadFilter = ref(false), notificationFilters = [];
const h = new Function('notificationsAPI', 'items', 'unreadCount', 'isOpen', 'unreadFilter', 'loading', 'showToast', 'document', compile(bellCode + '\nreturn { handleNotificationEvent, loadData, toggleOpen };'))(
  { list: unread => { notificationFilters.push(unread); return reject(); } }, items, unreadCount, isOpen, unreadFilter, ref(false), (...args) => toasts.push(args), { hidden: false });
h.handleNotificationEvent(); await tick();
assert.equal(toasts.length, 0, 'closed bell background errors are silent');
isOpen.value = true; h.handleNotificationEvent(); await tick();
assert.equal(toasts.length, 0, 'open bell background errors are silent');
assert.deepEqual(items.value, [{ id: 'old' }]); assert.equal(unreadCount.value, 3);
await h.loadData(); assert.equal(toasts.length, 1, 'explicit reads still report failure');
h.toggleOpen();
h.toggleOpen();
await tick();
assert.equal(notificationFilters.at(-1), true, 'opening the bell requests unread notifications');
assert.deepEqual(items.value, [], 'previous all-notification rows must not flash while unread rows load');
unreadFilter.value = false;
h.toggleOpen();
h.toggleOpen();
await tick();
assert.equal(notificationFilters.at(-1), true, 'reopening resets the previous all-notifications filter');
const detail = read('modules/users/pages/UserDetailPage.vue');
const primary = detail.slice(detail.indexOf('function observeUserDenial('), detail.indexOf('async function loadPreview('));
const activity = detail.slice(detail.indexOf('function loadActivity('), detail.indexOf('const refreshStatus ='));
const user = ref({ id: 'u', username: 'user', usedTraffic: 4 });
const traffic = ref({ observations: 8 }), accesses = ref([{ id: 'a' }]), operations = ref([{ id: 7 }]);
const activityToasts = [];
const run = new Function('accessAPI', 'request', 'trafficSummary', 'recentAccessList', 'userOperations', 'user', 'userAPI', 'loginEnabled', 'document', 'showToast', 'invalidateDeniedUser', compile('let userGeneration = 0, activityGeneration = 0, loadingUser = false, primaryUserRead;\nconst resetProfileRetryRequired = { value: false }, userOperationReadPaths = new Set();\n' + primary + activity + '\nreturn { loadActivity, refreshUserStatus };'))(
  { accounts: reject, list: reject }, reject, traffic, accesses, operations, user, { get: reject }, ref(false), { hidden: false }, (...args) => activityToasts.push(args), () => false);
run.loadActivity(user.value.id, 0, Promise.resolve(user.value)); await tick(); await run.refreshUserStatus();
assert.deepEqual(activityToasts.map(([message]) => message).sort(), [
  '流量记录读取失败：offline', '访问记录读取失败：offline',
  '操作记录读取失败：offline', '用户状态刷新失败：offline'
].sort(), 'failed user sections report their original errors');
assert.ok(activityToasts.every(([, kind]) => kind === 'error'));
assert.deepEqual(traffic.value, { observations: 8 }); assert.deepEqual(accesses.value, [{ id: 'a' }]); assert.deepEqual(operations.value, [{ id: 7 }]); assert.equal(user.value.usedTraffic, 4);
console.log('PASS background refresh: notification events stay silent; user failures report original errors and preserve data');
