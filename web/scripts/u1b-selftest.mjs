import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const read = path => readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
const load = source => import('data:text/javascript;base64,' + Buffer.from(ts.transpileModule(source, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext },
}).outputText).toString('base64'));
const reactivity = `const ref = value => ({ value }), reactive = value => value, computed = fn => ({ get value() { return fn(); } });`;
async function component(path, props, dependencies, exports) {
  globalThis.__u1bProps = props;
  const script = read(path).split('<script setup lang="ts">')[1].split('</script>')[0].replace(/^import .*;\r?\n/gm, '');
  return load(`${reactivity}
    const defineProps = () => globalThis.__u1bProps;
    export const events = [], mounts = [];
    const defineEmits = () => (...args) => events.push(args), watch = () => {}, onMounted = fn => mounts.push(fn);
    ${dependencies}
    ${script}
    export { ${exports} };
  `);
}

const relay = await component('modules/tunnels/components/AddRelayDialog.vue', {
  nodeId: 'us', nodeName: '美国', initialInboundId: 'vless', createScheme: async () => {},
  inbounds: [
    { id: 'vless', nodeId: 'us', protocol: 'vless' },
    { id: 'internal', nodeId: 'us', protocol: 'trojan', internalRelay: true },
    { id: 'forward', nodeId: 'us', protocol: 'forward' },
    { id: 'other', nodeId: 'jp', protocol: 'socks5' },
  ],
}, `export const calls = [];
  const tunnelError = e => e.message;
  const nodeRelayAPI = { create: async (nodeId, input) => { calls.push({ nodeId, input: JSON.parse(JSON.stringify(input)) }); return { rule: { id: 'new-rule' }, shareLink: null }; } };
`, 'destinations, available, tunnels, tunnelId, entryNodeId, name, portControl, save, created');
relay.tunnels.value = [
  { id: 'good', name: '香港到日本', status: 'published', hops: [{ nodeId: 'hk' }] },
  { id: 'deleting', status: 'pending_delete', hops: [{ nodeId: 'hk' }] },
  { id: 'same-node', status: 'draft', hops: [{ nodeId: 'us' }] },
];
assert.deepEqual(relay.destinations.value.map(i => i.id), ['vless']);
assert.deepEqual(relay.available.value.map(t => t.id), ['good']);
relay.tunnelId.value = 'good'; relay.name.value = '美国 经 香港到日本';
relay.portControl.value = { validate: async () => true };
assert.equal(relay.entryNodeId.value, 'hk');
await relay.save();
assert.equal(relay.calls[0].nodeId, 'us');
assert.equal(relay.calls[0].input.inboundId, 'vless');
assert.equal(relay.calls[0].input.portMode, 'auto');
assert.ok(!Object.hasOwn(relay.calls[0].input, 'listenPort'));
assert.equal(relay.created.value.shareLink, null);
assert.ok(relay.events.some(e => e[0] === 'saved'));

const publish = await component('modules/tunnels/components/PublishFlow.vue', {
  tunnels: [{ id: 'published', status: 'published' }, { id: 'remove-tunnel', status: 'pending_delete' }],
  rules: [{ id: 'remove-rule', status: 'pending_delete' }, { id: 'draft-rule', status: 'draft' }],
}, '', 'open, selection');
publish.open();
assert.deepEqual(publish.selection.tunnelIds, ['remove-tunnel']);
assert.deepEqual(publish.selection.ruleIds, ['remove-rule', 'draft-rule']);

const legacy = await component('modules/proxy/components/relay/CanvasConnectionDialog.vue', {
  source: { instanceId: 'a' }, target: { instanceId: 'b' }, client: false,
  connection: { id: 'old', type: 'port_forward' }, persist: async value => legacyCalls.push(value),
}, '', 'legacy, submit');
const legacyCalls = [];
assert.equal(legacy.legacy.value, true);
await legacy.submit(false);
assert.equal(legacyCalls.length, 0, 'legacy connections cannot be saved');
await legacy.submit(true);
assert.deepEqual(legacyCalls, [null], 'legacy connections can be deleted');
const newCalls = [];
const connection = await component('modules/proxy/components/relay/CanvasConnectionDialog.vue', {
  source: { instanceId: 'a', resourceType: 'server' }, target: { instanceId: 'b', resourceType: 'server' }, client: false,
  persist: async value => newCalls.push(value),
}, '', 'submit, kind, protocol');
connection.kind.value = 'tunnel'; connection.protocol.value = 'wireguard';
await connection.submit(false);
assert.equal(newCalls[0].type, 'tunnel');
assert.equal(newCalls[0].tunnelProtocol, 'wireguard');

const external = await component('modules/proxy/components/relay/ExternalProxyDialog.vue', {
  initialNodeId: '', nodes: [{ id: 'hk' }, { id: 'jp' }],
}, `export const calls = [];
  const validPort = p => Number.isInteger(p) && p > 0 && p <= 65535;
  const proxyAPI = { saveOutbound: async (id, value) => { calls.push({ id, value: structuredClone(value) }); return { ...value, id: 'external' }; } };
`, 'form, save, protocolChanged');
external.form.name = '外部出口'; external.form.address = 'proxy.example.com';
await external.save();
assert.equal(external.calls.length, 0, 'ambiguous preceding hop must be selected');
external.form.nodeId = 'hk'; external.form.protocol = 'trojan'; external.protocolChanged(); external.form.secret = 'secret';
await external.save();
assert.equal(external.calls[0].id, null, 'uses existing outbound create API');
assert.equal(external.calls[0].value.nodeId, 'hk');
assert.equal(external.calls[0].value.settings.security, 'tls');
assert.equal(external.calls[0].value.secret, 'secret');
assert.equal(external.form.secret, '');
assert.ok(external.events.some(e => e[0] === 'created' && e[1].id === 'external'));

const api = await load(read('modules/proxy/api.ts')
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;')
  .replace("import { request, type Page } from '../../app/apiClient';", 'const request = () => {};'));
const context = { servers: [], inbounds: [{ id: 'socks', nodeId: 'us', protocol: 'socks5', name: '美国 SOCKS5', port: 1080, enabled: true }], imported: [], outbounds: [{ id: 'external', name: '外部代理', protocol: 'trojan', address: 'proxy.example.com', port: 443, enabled: true }] };
const scheme = api.canvasSchemeToRelayScheme({ id: 'scheme', name: '方案', execution: 'server', updatedAt: 'now', publishedAt: '', layout: { instances: [
  { instanceId: 'a', ref: 'server:hk', role: 'entry', x: 0, y: 0 },
  { instanceId: 'b', ref: 'server:jp', role: 'relay', x: 1, y: 1, relayPortMode: 'auto' },
  { instanceId: 'c', ref: 'inbound:socks', role: 'exit', x: 2, y: 2 },
  { instanceId: 'd', ref: 'outbound:external', role: 'exit', x: 3, y: 3 },
  { instanceId: 'e', ref: 'server:us', role: 'exit', x: 4, y: 4, relayPortMode: 'manual', relayPort: 31000 },
], connections: [] } }, context);
const roundTrip = api.relaySchemeToCanvasInput(scheme, context).layout;
assert.equal(roundTrip.instances[2].ref, 'inbound:socks');
assert.equal(roundTrip.instances[2].role, 'exit');
assert.equal(roundTrip.instances[3].ref, 'outbound:external');
assert.equal(scheme.nodes[3].name, '外部代理');
assert.equal(roundTrip.instances[1].relayPortMode, 'auto');
assert.ok(!Object.hasOwn(roundTrip.instances[1], 'relayPort'));
assert.equal(roundTrip.instances[4].relayPortMode, 'manual');
assert.equal(roundTrip.instances[4].relayPort, 31000);

// Exercise the real entry-draft helper with the canvas API boundary replaced.
const store = await load(`${reactivity}
  export const payloads = [];
  const proxyNodeStore = { schemes: [] };
  const createCanvasScheme = async () => ({ id: 'new', nodes: [] });
  const persistCanvasScheme = async scheme => payloads.push(structuredClone(scheme));
  ${read('modules/proxy/proxyNodes.ts').split('export async function createDraftSchemeForEntryNode')[1].split('// Imported proxies carry')[0].replace(/^/, 'export async function createDraftSchemeForEntryNode')}
`);
const draft = await store.createDraftSchemeForEntryNode({ id: 'self-hk-vless', name: '香港', sourceType: 'self_hosted', protocol: 'vless', port: 443 }, {});
assert.equal(store.payloads[0].nodes[0].role, 'entry');
assert.equal(draft.entryInstanceId, store.payloads[0].nodes[0].instanceId);
console.log('PASS U1b: Add Relay filtering/save, auto-empty payload, legacy read-only/delete, new connections, external outbound create/ownership, exit and relay-port round trips, entry draft');
