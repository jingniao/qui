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
const h = new Function('notificationsAPI', 'items', 'unreadCount', 'isOpen', 'unreadFilter', 'loading', 'showToast', 'document', compile(bellCode + '\nreturn { handleNotificationEvent, loadData, toggleOpen };'))(
  { list: reject }, items, unreadCount, isOpen, ref(false), ref(false), (...args) => toasts.push(args), { hidden: false });
h.handleNotificationEvent(); await tick();
assert.equal(toasts.length, 0, 'closed bell background errors are silent');
isOpen.value = true; h.handleNotificationEvent(); await tick();
assert.equal(toasts.length, 0, 'open bell background errors are silent');
assert.deepEqual(items.value, [{ id: 'old' }]); assert.equal(unreadCount.value, 3);
await h.loadData(); assert.equal(toasts.length, 1, 'explicit reads still report failure');
const detail = read('modules/users/pages/UserDetailPage.vue');
const activity = detail.slice(detail.indexOf('function loadActivity('), detail.indexOf('const refreshStatus ='));
const user = ref({ id: 'u', username: 'user', usedTraffic: 4 });
const traffic = ref({ observations: 8 }), accesses = ref([{ id: 'a' }]), operations = ref([{ id: 7 }]);
const run = new Function('accessAPI', 'request', 'trafficSummary', 'recentAccessList', 'userOperations', 'user', 'userId', 'userAPI', 'loginEnabled', 'document', 'showToast', compile('let userGeneration = 0, loadingUser = false;\n' + activity + '\nreturn { loadActivity, refreshUserStatus };'))(
  { accounts: reject, list: reject }, reject, traffic, accesses, operations, user, ref('u'), { get: reject }, ref(false), { hidden: false }, (...args) => toasts.push(args));
run.loadActivity(user.value, 0); await tick(); await run.refreshUserStatus();
assert.equal(toasts.length, 1, 'activity and status refresh errors are silent');
assert.deepEqual(traffic.value, { observations: 8 }); assert.deepEqual(accesses.value, [{ id: 'a' }]); assert.deepEqual(operations.value, [{ id: 7 }]); assert.equal(user.value.usedTraffic, 4);
console.log('PASS background refresh: notification events and user reads preserve data without toasts; explicit reads report errors');
