// v1.5.4 item 10: the admin login path may not take the first segment of any
// page route, or that page would turn into the admin login. The reserved list
// lives in Go (internal/panel/system/admin_entry.go); this checks it covers
// every route in web/src/app/router.ts, and that the router keeps the admin
// login off fixed paths.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = join(dirname(fileURLToPath(import.meta.url)), '..', '..');
const router = readFileSync(join(root, 'web/src/app/router.ts'), 'utf8');
const goSource = readFileSync(join(root, 'internal/panel/system/admin_entry.go'), 'utf8');

const block = goSource.match(/var ReservedAdminEntrySegments = \[\]string\{([\s\S]*?)\n\}/);
assert.ok(block, 'ReservedAdminEntrySegments not found');
const reserved = new Set([...block[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]));

const segments = new Set();
for (const match of router.matchAll(/path:\s*'([^']*)'/g)) {
  const first = match[1].replace(/^\//, '').split('/')[0];
  if (!first || first.startsWith(':')) continue;
  segments.add(first);
}
assert.ok(segments.has('dashboard') && segments.has('portal'), 'route parsing found no routes');
const missing = [...segments].filter((s) => !reserved.has(s)).sort();
assert.deepEqual(missing, [], `router segments missing from ReservedAdminEntrySegments: ${missing.join(', ')}`);
console.log(`PASS every router first segment (${segments.size}) is reserved`);

assert.doesNotMatch(router, /path:\s*'\/login'/, 'the admin login must not be a fixed /login route');
assert.match(router, /adminEntryPage \? \[\{ path: adminEntryPage/, 'admin login route must come from the marked page load');
assert.match(router, /\{ path: '\/', name: 'PortalLogin'/, 'the site root must be the user login');
assert.ok(router.indexOf("name: 'PortalLogin'") < router.indexOf('component: MainLayout'), 'the user login route must precede MainLayout');
assert.match(router, /\{ path: '\/portal\/login', redirect: '\/' \}/, '/portal/login must redirect to /');
console.log('PASS router serves the user login at / and the admin login only at the marked path');

const guards = readFileSync(join(root, 'web/src/app/guards.ts'), 'utf8');
assert.doesNotMatch(guards, /'\/login'/, 'guards must not send anyone to /login');
console.log('PASS guards send signed-out visitors to /');
console.log('All admin entry self-tests passed');
