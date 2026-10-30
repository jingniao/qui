import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import ts from 'typescript';
import { computed, reactive, ref } from 'vue';

async function load(path) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const api = await load('../src/modules/proxy/components/node-editor/nodeEditorModel.ts');
const { legacyList } = await load('./fixtures/node-editor-legacy.ts');
const json = value => JSON.parse(JSON.stringify(value));

// Frozen U3 protocolChanged/save logic, independent from the new builder.
function legacyServer(form, addresses, mtu) {
  const value = json(form);
  if (value.internalRelay) value.subscriptionVisible = false;
  if (value.protocol === 'wireguard') value.settings = {
    wireguard: { secret_key: '', addresses: addresses.split(/\r?\n/).map(value => value.trim()).filter(Boolean), peers: [], mtu }
  };
  return value;
}
function oldServerProtocol(protocol) {
  if (protocol === 'forward') return { forward: { host: '', port: 443, network: 'tcp' } };
  return protocol === 'wireguard' ? {} : {
    transport: ['hysteria2', 'tuic'].includes(protocol) ? 'quic' : 'tcp',
    quic: ['hysteria2', 'tuic'].includes(protocol) ? {} : undefined,
    security: ['trojan', 'hysteria2', 'tuic'].includes(protocol) ? 'tls' : 'none',
    method: protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : undefined
  };
}
const protocols = ['vless', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'forward', 'socks5', 'http'];
let comparisons = 0;
for (const protocol of protocols) {
  const settings = protocol === 'vless' ? { transport: 'ws', security: 'tls', path: '/proxy', server_name: 'example.com', certificate_pem: 'CERT', private_key_pem: 'KEY', trusted_ca_pem: 'CA' }
    : protocol === 'wireguard' ? { wireguard: { secret_key: '', addresses: ['10.77.0.1/24'], peers: [], mtu: 1420 } }
    : protocol === 'forward' ? { forward: { host: 'example.com', port: 8443, network: 'tcp,udp' } }
    : protocol === 'hysteria2' ? { security: 'tls', quic: { up_mbps: 30, down_mbps: 100 } }
    : protocol === 'tuic' ? { security: 'tls', quic: { congestion_control: 'bbr', udp_relay_mode: 'native' } }
    : { transport: 'tcp', security: protocol === 'trojan' ? 'tls' : 'none', method: 'aes-128-gcm' };
  for (const edit of [false, true]) {
    const original = edit ? { id: 'editing', nodeId: 'server', name: 'old', protocol, port: 443, listen: '::', enabled: false,
      subscriptionVisible: protocol !== 'forward', internalRelay: false, defaultOutbound: 'group:one', allowedUserIds: ['alice'],
      settings: { ...settings, flow: 'preserve-extra-field' }, hasTlsKey: true, hasWireGuardKey: true } : null;
    const state = api.createEditor('server', original);
    if (!edit) api.changeProtocol(state, protocol);
    state.form.name = '  representative  ';
    state.form.port = 1443;
    if (!edit && protocol === 'vless') Object.assign(state.form.settings, settings);
    const f = state.form;
    const oldForm = { ...f.settings, name: f.name, protocol, serverId: f.nodeId, port: f.port, inboundId: original?.id || '', forward: f.settings.forward || { host: '', port: 443, network: 'tcp' } };
    assert.deepEqual(api.buildPayload(state), json(legacyList(oldForm, original)), `list ${protocol} ${edit ? 'edit' : 'create'}`);
    comparisons++;
    if (edit && protocol === 'vless') {
      f.settings.private_key_pem = 'REPLACEMENT';
      assert.deepEqual(api.buildPayload(state), json(legacyList({ ...oldForm, private_key_pem: 'REPLACEMENT' }, original)));
      comparisons++;
    }
  }
  const server = api.createEditor('server', null, 'server');
  api.changeProtocol(server, protocol);
  assert.deepEqual(json(server.form.settings), json(oldServerProtocol(protocol)), `U3 ${protocol} protocol defaults`);
  for (const internal of [false, true]) {
    server.form.name = '  server name  ';
    server.form.internalRelay = internal;
    assert.deepEqual(api.buildPayload(server), legacyServer(server.form, server.addresses, server.mtu), `U3 ${protocol} payload`);
    comparisons++;
  }
}
console.log(`PASS NODE_EDITOR_PAYLOAD: ${comparisons} old/new wire payload comparisons; nine protocols, list create/edit, TLS replacement, U3 defaults/internal relay`);

const server = { id: 'server', name: 'Tokyo', enabled: true, deletedAt: '', status: 'online', agentVersion: '1.1.0',
  capabilities: { core_name: 'xray', core_version: '26.3', protocols: [{ protocol: 'vless', inbound: true }, { protocol: 'forward', inbound: true }] } };
assert.equal(api.serverEligible(server), true);
assert.equal(api.serverEligible({ ...server, status: 'offline' }), true); // Registered, can save a draft.
for (const patch of [{ deletedAt: 'deleted' }, { status: 'pending' }, { enabled: false }, { agentVersion: '' }]) assert.equal(api.serverEligible({ ...server, ...patch }), false);
assert.equal(api.protocolReason(server, 'vless'), '');
assert.match(api.protocolReason(server, 'tuic'), /Xray/);
assert.match(api.protocolReason({ ...server, capabilities: { core_name: 'sing-box' } }, 'wireguard'), /sing-box/);
assert.match(api.protocolReason(undefined, 'vless'), /选择服务器/);
assert.match(api.protocolReason({ ...server, capabilities: {} }, 'vless'), /尚未上报/);
assert.match(api.protocolReason(server, 'socks5'), /未报告/);
const state = api.createEditor('server');
state.form.name = 'Tokyo-vless-443';
const rows = [{ id: 'current', nodeId: 'server', port: 443, name: 'Current' }, { id: 'canvas', nodeId: 'server', port: 10000, name: '[canvas] relay', enabled: false }, { id: 'other', nodeId: 'elsewhere', port: 1443 }];
assert.match(api.portConflict(state.form, rows), /Current/);
assert.equal(api.portConflict(state.form, rows, 'current'), '');
state.form.port = 10000;
assert.match(api.portConflict(state.form, rows), /canvas/);
assert.equal(api.randomPort(state.form, rows, '', () => 0), 10001);
state.form.port = 1443;
assert.equal(api.portConflict(state.form, rows), '');
// TCP and UDP listeners may share a port; only overlapping transports conflict.
const mixed = [{ id: 'vless', nodeId: 'server', port: 443, name: 'VLESS', protocol: 'vless', settings: {} }];
assert.equal(api.portConflict({ ...state.form, port: 443, protocol: 'hysteria2', settings: {} }, mixed), '');
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'shadowsocks', settings: {} }, mixed), /VLESS/);
assert.equal(api.portConflict({ ...state.form, port: 443, protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'udp' } } }, mixed), '');
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'tcp,udp' } } }, mixed), /VLESS/);
console.log('PASS NODE_EDITOR_PORTS: TCP and UDP listeners may share a port; overlapping transports conflict');
assert.equal(api.validateStep(2, state, server, rows), '');
for (const port of [0, 65536, 1.5, NaN]) { state.form.port = port; assert.match(api.validateStep(2, state, server, rows), /端口/); }
state.form.settings.security = 'tls';
assert.match(api.validateStep(3, state, server, rows), /证书/);
state.form.settings.certificate_pem = 'CERT';
assert.match(api.validateStep(3, state, server, rows), /私钥/);
state.form.settings.private_key_pem = 'SECRET';
assert.equal(api.validateStep(3, state, server, rows), '');
assert.equal(JSON.stringify(api.previewPayload(api.buildPayload(state))).includes('SECRET'), false);
api.changeProtocol(state, 'forward');
assert.equal(api.hasSecurity('forward'), false);
assert.match(api.validateStep(4, state, server, rows), /目标主机/);
state.form.settings.forward.host = 'target'; state.form.settings.forward.port = 0;
assert.match(api.validateStep(4, state, server, rows), /目标端口/);
assert.deepEqual(Object.keys(api.buildPayload(state).settings), ['forward']);
api.changeProtocol(state, 'wireguard'); state.mtu = 575;
assert.match(api.validateStep(4, state, server, rows), /MTU/);
for (const protocol of ['forward', 'wireguard', 'tuic', 'hysteria2']) assert.equal(api.hasTransport(protocol), false);
console.log('PASS NODE_EDITOR_MODEL: registered server filtering, capability reasons, canvas/edit port checks, random port, validation, secret masking');

// F0 explicitly changes action permissions; assert those boundaries rather than
// freezing a whole page whose authorised button visibility must change.
const page = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8').replace(/\r\n/g, '\n');
for (const action of ['openCreateNodeModal', 'openEditNode', 'deleteNode', 'handleAddRelay']) {
  const buttons = [...page.matchAll(/<button\b[^>]*>/g)].map(m => m[0]).filter(tag => tag.includes('@click=') && tag.includes(action));
  assert.ok(buttons.length, action + ' has a visible entry');
  assert.ok(buttons.every(tag => /v-if="isOwner/.test(tag)), action + ' must be Owner-only');
}
assert.match(page, /<PageHeader[\s\S]*title="节点配置"/);
assert.match(page, /<NodeEditorDrawer/);
const hash = value => createHash('sha256').update(value).digest('hex');
const imports = s => s.slice(s.indexOf('    <!-- Modal 2:'), s.indexOf('</template>\n\n<script'));
assert.equal(hash(imports(page)), '416e21529a7538e85c31aafc81be05e636ce43751c0381d1d11a24a8cc44ed3b');
assert.ok(page.split('\n').length < 1850);
const drawer = readFileSync(new URL('../src/modules/proxy/components/node-editor/NodeEditorDrawer.vue', import.meta.url), 'utf8');
assert.ok(drawer.indexOf('await proxyAPI.saveInbound') < drawer.indexOf('await deploymentAPI.publishNode'));
assert.match(drawer, /savedId\.value = saved\.id/);
assert.match(drawer, /草稿已保存并保留/);
console.log('PASS NODE_EDITOR_INTEGRATION: Owner action permissions and import layout preserved, page smaller, save precedes publish with retained draft');

// Execute the drawer's actual save handler with stubbed network dependencies.
// This checks failures without a browser, real backend, or a duplicate save flow.
const setup = drawer.split('<script setup lang="ts">')[1].split('</script>')[0].replace(/^import .*;\n/gm, '');
const setupCode = ts.transpileModule(setup, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
function saveHarness(mode) {
  const calls = [], events = [];
  const deps = {
    computed, reactive, ref, watch: () => {}, onMounted: () => {}, onUnmounted: () => {},
    defineProps: () => ({ serverId: 'server', inbound: null, origin: 'list' }), withDefaults: props => props,
    defineEmits: () => event => events.push(event), useRoute: () => ({ query: {} }), isOwner: { value: true },
    ...api,
    choices: async read => (await read(1, 100)).items,
    proxyAPI: {
      inbounds: async () => ({ items: [], total: 0 }),
      saveInbound: async (id, value) => { calls.push(['save', id, value]); if (mode === 'save-error') throw Error('save refused'); return { id: 'saved-id' }; }
    },
    deploymentAPI: { publishNode: async (id, summary) => {
      calls.push(['publish', id, summary]);
      if (mode === 'publish-error') throw Error('deploy refused');
      return { revision: 42, status: 'queued' };
    } }
  };
  const run = new Function('deps', `const { ${Object.keys(deps).join(', ')} } = deps;\n${setupCode}\nreturn { save, state, servers, savedId, result, error, busy };`);
  const view = run(deps);
  view.servers.value = [server]; view.state.form.name = 'Tokyo-vless-1443'; view.state.form.port = 1443;
  return { ...view, calls, events };
}
const draft = saveHarness('draft'); await draft.save(false);
assert.deepEqual(draft.calls.map(c => c[0]), ['save']);
assert.equal(draft.savedId.value, 'saved-id');
const published = saveHarness('publish'); await published.save(true);
assert.deepEqual(published.calls.map(c => c[0]), ['save', 'publish']);
assert.equal(published.calls[1][1], 'server'); assert.match(published.result.value, /#42/);
const refused = saveHarness('publish-error'); await refused.save(true);
assert.equal(refused.savedId.value, 'saved-id'); assert.match(refused.error.value, /草稿已保存并保留/);
assert.deepEqual(refused.events, ['saved']);
await refused.save(true); assert.equal(refused.calls.length, 2); // No duplicate creation.
const unsaved = saveHarness('save-error'); await unsaved.save(true);
assert.deepEqual(unsaved.calls.map(c => c[0]), ['save']);
assert.equal(unsaved.savedId.value, ''); assert.equal(unsaved.busy.value, false);
assert.match(unsaved.error.value, /save refused/);
console.log('PASS NODE_EDITOR_SAVE: actual drawer handler covers draft-only, save-then-publish, retained draft on publish failure, no duplicate creation, no publish after save failure');
