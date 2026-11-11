import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const load = (path, transform = value => value) => import('data:text/javascript;base64,' + Buffer.from(
  ts.transpileModule(transform(read(path)), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText
).toString('base64'));

// 1. notificationsApi.ts severityTone mapping
const api = await load('modules/system/notificationsApi.ts', source => source.replace(
  /import\s*\{[^}]*\}\s*from\s*['"][^'"]*apiClient['"];?/,
  'const request = () => Promise.resolve({});'
));

assert.equal(api.severityTone('resolved'), 'success');
assert.equal(api.severityTone('critical'), 'danger');
assert.equal(api.severityTone('warning'), 'warning');
assert.equal(api.severityTone('info'), 'info');
assert.equal(api.severityTone('unknown'), 'info');

// 2. AppSidebar.vue imports NotificationBell
const sidebar = read('components/AppSidebar.vue');
assert.match(sidebar, /NotificationBell/, 'AppSidebar must import NotificationBell');
assert.match(sidebar, /<NotificationBell/, 'AppSidebar must render NotificationBell');

// 2b. The expanded-sidebar popover is anchored to the sidebar, not right-aligned to the
// bell near its right edge, which pushed a 24rem panel off the left of the window.
const bell = read('components/NotificationBell.vue');
assert.doesNotMatch(bell, /'right-0 top-full mt-2'/, 'expanded popover must not right-align to the bell');
assert.match(bell, /:class="collapsed \? 'relative' : ''"/, 'expanded popover must position against the sidebar');
assert.match(bell, /max-w-\[calc\(100vw-1\.5rem\)\]/, 'popover width must stay within the viewport');
// read-all is Owner-only on the server; Viewers must not see the button.
assert.match(bell, /v-if="isOwner && unreadCount > 0"[\s\S]{0,200}@click="markAllAsRead"/, 'mark-all-read must be Owner-only');
assert.match(bell, /import \{ isOwner \} from '\.\.\/app\/auth'/, 'NotificationBell must read the role from app/auth');

// 3. events.ts dispatches proxy-notification
const events = read('app/events.ts');
assert.match(events, /connection\.addEventListener\('notification',\s*\(\)\s*=>\s*window\.dispatchEvent\(new Event\('proxy-notification'\)\)\)/, 'events.ts must handle notification event');

// 4. SettingsPage.vue has '通知' tab restricted to isOwner
const settings = read('modules/system/pages/SettingsPage.vue');
assert.match(settings, /通知/, 'SettingsPage must have a notification tab');
assert.match(settings, /isOwner/, 'SettingsPage notification tab/panel must check isOwner');

// 5. NotificationSettings.vue doesn't echo botToken / url plaintext in table/list
const notifSettings = read('modules/system/components/NotificationSettings.vue');
assert.doesNotMatch(notifSettings, /channel\.botToken/, 'must not echo plaintext botToken');
assert.doesNotMatch(notifSettings, /channel\.config\.botToken/, 'must not echo plaintext botToken');
assert.match(notifSettings, /last4/, 'must show last4 instead of full secret');

for (const file of ['components/NotificationBell.vue', 'modules/system/components/NotificationSettings.vue']) {
  for (const match of read(file).matchAll(/text-\[(\d+(?:\.\d+)?)px\]/g)) {
    assert.ok(Number(match[1]) >= 12, file + ' notification text must remain readable at 12px or above');
  }
}
// Exercise the actual Vue script and API payloads with controlled transport and reactive primitives.
const vueScript = notifSettings.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const calls = [];
globalThis.__notificationCalls = calls;
const formModule = await import('data:text/javascript;base64,' + Buffer.from(ts.transpileModule(
  vueScript.replace(/import\s+[\s\S]*?\s+from\s+['"][^'"]+['"];?/g, '') + `
  export { channelForm, openAddChannel, openEditChannel, submitChannel, supportsSigning };
  `, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }
).outputText.replace(/^/, `
const ref = value => ({value});
const reactive = value => value;
const onMounted = () => {};
const showToast = () => {};
const notificationsAPI = {saveChannel: async value => globalThis.__notificationCalls.push(JSON.parse(JSON.stringify(value))), channels: async () => []};
`)).toString('base64'));
for (const format of ['generic', 'wecom', 'dingtalk', 'feishu']) {
  assert.equal(formModule.supportsSigning(format), ['dingtalk', 'feishu'].includes(format));
  formModule.openAddChannel();
  Object.assign(formModule.channelForm, { kind: 'webhook', name: 'Ops', url: 'https://example.com/hook', format, secret: 'test-key' });
  await formModule.submitChannel();
  assert.equal(calls.at(-1).config.format, format);
  assert.equal(calls.at(-1).config.secret, ['dingtalk', 'feishu'].includes(format) ? 'test-key' : undefined);
}
formModule.openEditChannel({ id: 'one', kind: 'webhook', name: 'Ops', enabled: true, format: 'dingtalk' });
await formModule.submitChannel();
assert.deepEqual(calls.at(-1).config, { url: '', format: 'dingtalk', secret: '', clearSecret: false });
formModule.channelForm.clearSecret = true;
await formModule.submitChannel();
assert.equal(calls.at(-1).config.clearSecret, true);
formModule.channelForm.secret = 'ignored-when-clearing';
await formModule.submitChannel();
assert.equal(calls.at(-1).config.secret, '');
formModule.channelForm.format = 'wecom';
await formModule.submitChannel();
assert.deepEqual(calls.at(-1).config, { url: '', format: 'wecom' });
assert.match(notifSettings, /v-if="supportsSigning\(channelForm\.format\)"[\s\S]*?v-model="channelForm\.secret"/, 'signing fields must be gated to supported formats');
assert.match(notifSettings, /留空表示保留原密钥/);
assert.match(notifSettings, /清除密钥/);
const transport = [];
globalThis.__notificationTransport = transport;
const realAPI = await load('modules/system/notificationsApi.ts', source => source.replace(
  /import\s*\{[^}]*\}\s*from\s*['"][^'"]*apiClient['"];?/,
  'const request = async (...args) => { globalThis.__notificationTransport.push(JSON.parse(JSON.stringify(args))); return {}; };'
));
await realAPI.notificationsAPI.saveChannel(calls.at(-1));
assert.deepEqual(transport.at(-1), ['/notification-channels/one', 'PUT', {name: 'Ops', enabled: true, config: { url: '', format: 'wecom' }}]);
await realAPI.notificationsAPI.saveChannel({id: 'one', kind: 'webhook', name: 'Ops', enabled: false});
assert.deepEqual(transport.at(-1), ['/notification-channels/one', 'PUT', {name: 'Ops', enabled: false}]);
delete globalThis.__notificationCalls;
delete globalThis.__notificationTransport;
console.log('PASS NOTIFICATIONS SELFTEST');
