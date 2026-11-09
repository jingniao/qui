// Self-test for the node address rules (v1.4.7 B2).
//
// The panel stores one address for clients, not for itself (§11.1): the Owner's
// connect host wins, otherwise the detected or typed public IP is used, and an
// empty result means "no address" — never the hostname or 127.0.0.1. These checks
// cover three things a browser would otherwise be needed for:
//   1. connectAddress is the one definition of that address;
//   2. only an address field the Owner actually changed is submitted, so saving an
//      unrelated setting cannot turn a detected public IP into a manual one;
//   3. the components that used to fall back to the hostname now show the shared
//      helper's result, and the settings forms no longer echo the loaded address.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const load = (path, transform = value => value) => import('data:text/javascript;base64,' + Buffer.from(
  ts.transpileModule(transform(read(path)), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText
).toString('base64'));

const address = await load('modules/nodes/nodeAddress.ts');

// --- connectAddress -----------------------------------------------------------
assert.equal(address.connectAddress({ connectHost: 'node.example.com', publicIp: '203.0.113.9' }), 'node.example.com');
assert.equal(address.connectAddress({ publicIp: '203.0.113.9' }), '203.0.113.9');
assert.equal(address.connectAddress({ connectHost: '  node.example.com  ' }), 'node.example.com');
// A blank connect host falls back instead of becoming the address itself.
assert.equal(address.connectAddress({ connectHost: '   ', publicIp: '203.0.113.9' }), '203.0.113.9');
assert.equal(address.connectAddress({}), '');
assert.equal(address.connectAddress({ connectHost: '', publicIp: '' }), '');
assert.equal(address.connectAddress(null), '');
assert.equal(address.connectAddress(undefined), '');

// --- addressChanges -----------------------------------------------------------
// Nothing changed: nothing is submitted.
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9', connectHost: '' }, { publicIp: '203.0.113.9', connectHost: '' }), {});
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9', connectHost: 'a.example.com' }, { publicIp: '203.0.113.9', connectHost: 'a.example.com' }), {});
// A changed field travels alone.
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9', connectHost: '' }, { publicIp: '198.51.100.7', connectHost: '' }), { publicIp: '198.51.100.7' });
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9', connectHost: '' }, { publicIp: '203.0.113.9', connectHost: 'node.example.com' }), { connectHost: 'node.example.com' });
// An emptied field is a real instruction ("clear it, detect again"), not an omission.
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9' }, { publicIp: '  ' }), { publicIp: '' });
assert.deepEqual(address.addressChanges({ connectHost: 'node.example.com' }, { connectHost: '' }), { connectHost: '' });
// A field the form does not hold is never submitted.
assert.deepEqual(address.addressChanges({ publicIp: '203.0.113.9' }, {}), {});
assert.deepEqual(address.addressChanges({}, { publicIp: undefined, connectHost: undefined }), {});
assert.deepEqual(address.addressChanges({}, { publicIp: '203.0.113.9', connectHost: 'node.example.com' }), { publicIp: '203.0.113.9', connectHost: 'node.example.com' });
// Trimming happens on both sides, so a stray space is not a change.
assert.deepEqual(address.addressChanges({ publicIp: ' 203.0.113.9 ' }, { publicIp: '203.0.113.9' }), {});

// --- validConnectHost ---------------------------------------------------------
for (const value of ['', '   ', 'node.example.com', 'node-1.example.com', '203.0.113.9', '2001:db8::1', '::1']) {
  assert.equal(address.validConnectHost(value), true, 'accepts ' + JSON.stringify(value));
}
for (const value of ['https://node.example.com', 'node.example.com:443', '[2001:db8::1]', 'node_example.com',
  '-node.example.com', 'node-.example.com', 'node..example.com', 'node example.com', 'node.example.com/path',
  'a'.repeat(64) + '.example.com']) {
  assert.equal(address.validConnectHost(value), false, 'rejects ' + JSON.stringify(value));
}


// --- the update body ----------------------------------------------------------
// nodeAPI.save must leave an absent address field out of the body entirely: the
// endpoint reads a missing field as "keep it" and a value as an instruction.
const nodesApi = await load('modules/nodes/api.ts', source => source.replace(
  "import { request, type Page } from '../../app/apiClient';",
  'const request = (...args) => globalThis.__nodeAddressRequest(...args);'
));
let call;
globalThis.__nodeAddressRequest = (...args) => { call = args; return Promise.resolve({}); };
const base = { id: 'node-a', name: 'Tokyo', enabled: true, allowSubscription: true, maintenance: false,
  outboundSourceIp: '', outboundInterface: '', hostSourceIpv4: '', hostSourceIpv6: '' };
await nodesApi.nodeAPI.save(base);
assert.equal(call[0], '/nodes/node-a');
assert.equal(call[1], 'PUT');
assert.deepEqual(Object.keys(call[2]).filter(key => key === 'publicIp' || key === 'connectHost'), [], 'an untouched address is not submitted');
// A save of another group therefore keeps a detected address detected.
await nodesApi.nodeAPI.save({ ...base, name: 'Tokyo-2' });
assert.equal(Object.hasOwn(call[2], 'publicIp'), false);
await nodesApi.nodeAPI.save({ ...base, connectHost: 'node.example.com' });
assert.deepEqual(call[2].connectHost, 'node.example.com');
assert.equal(Object.hasOwn(call[2], 'publicIp'), false);
await nodesApi.nodeAPI.save({ ...base, publicIp: '' });
assert.equal(call[2].publicIp, '', 'clearing the address is a value, not an omission');

// --- the components -----------------------------------------------------------
// Every place that shows a node's address reads the shared helper, and none of
// them falls back to the hostname or to 127.0.0.1 any more.
const displaySites = [
  'modules/nodes/components/ServerCardGrid.vue',
  'modules/nodes/components/detail/ServerOverview.vue',
  'modules/dashboard/pages/DashboardPage.vue',
  'modules/proxy/components/node-editor/NodeEditorDrawer.vue',
  'modules/proxy/pages/ProxyNodeListPage.vue',
  'modules/users/pages/UserDetailPage.vue',
  'modules/routing/pages/ClientPolicyPage.vue',
  'modules/routing/pages/PolicySimulatorPage.vue'
];
for (const path of displaySites) {
  const text = read(path);
  assert.match(text, /connectAddress\(/, path + ' reads the shared address helper');
  assert.doesNotMatch(text, /publicIp \|\| [^\n]*hostname/, path + ' must not fall back to the hostname');
  assert.doesNotMatch(text, /hostname \|\| '127\.0\.0\.1'/, path + ' must not invent a loopback address');
}
// The relay canvas dials nodes on their public IP (AGENTS.md §14), so it shows
// that address rather than the client connect address, still without inventing
// a hostname or loopback fallback.
for (const path of ['modules/proxy/api.ts', 'modules/proxy/components/relay/canvasLibrary.ts', 'modules/proxy/pages/RelayCanvasPage.vue']) {
  const text = read(path);
  assert.doesNotMatch(text, /connectAddress\(/, path + ' shows the public IP the canvas dials');
  assert.match(text, /未设置公网 IP/, path + ' names a missing public IP');
  assert.doesNotMatch(text, /publicIp \|\| [^\n]*hostname/, path + ' must not fall back to the hostname');
  assert.doesNotMatch(text, /hostname \|\| '127\.0\.0\.1'/, path + ' must not invent a loopback address');
}
// The version page shows the address too, but keeps one hostname seed for the
// region guess (resolveCountryInfo), which is not an address display.
const versionPanel = read('modules/system/components/NodeVersionPanel.vue');
assert.equal(versionPanel.match(/connectAddress\(/g).length, 5);
assert.doesNotMatch(versionPanel, /publicIp \|\| [^\n]*hostname \|\| '[^']*地址/);
// Both settings forms stopped echoing the loaded public IP back on every save.
const settings = read('modules/nodes/components/detail/ServerSettings.vue');
assert.match(settings, /addressChanges\(loadedAddresses\.value, submitted\)/);
assert.doesNotMatch(settings, /publicIp: submitted\.publicIp/);
assert.doesNotMatch(settings, /\.\.\.latest, \.\.\.change/);
assert.match(settings, /restoreAutoDetection/);
assert.match(settings, /v-if="isOwner && publicIpSource === 'manual'"/);
for (const label of ['手动设置', '自动探测', '未探测到']) assert.ok(settings.includes(label), 'source hint ' + label);
assert.doesNotMatch(read('modules/nodes/components/ServerQuickEdit.vue'), /publicIp: latest\.publicIp/);
assert.doesNotMatch(read('modules/nodes/components/ServerBulkActions.vue'), /\.\.\.node, maintenance/);
console.log('PASS NODE_ADDRESS: connect address precedence and blanks, change-only submission, connect host validation, no echoed address in either settings form');
