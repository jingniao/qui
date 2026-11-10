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
console.log('PASS NOTIFICATIONS SELFTEST');
