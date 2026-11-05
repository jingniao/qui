import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
import ts from 'typescript';
import { computed, reactive, ref } from 'vue';

async function load(path) {
  return import(await moduleUrl(new URL(path, import.meta.url)));
}
// The model now imports the shared per-core helper, so the harness resolves
// relative imports to their own transpiled data: URLs instead of dropping them.
async function moduleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  const source = readFileSync(url, 'utf8');
  let code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  const specs = [...new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(match => match[1]).filter(spec => spec.startsWith('.')))];
  for (const spec of specs) {
    const target = new URL(spec, url);
    if (!/\.tsx?$/.test(target.pathname)) target.pathname += '.ts';
    const child = await moduleUrl(target, cache);
    code = code.split(`from '${spec}'`).join(`from '${child}'`);
  }
  const moduleUrlValue = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, moduleUrlValue);
  return moduleUrlValue;
}
const api = await load('../src/modules/proxy/components/node-editor/nodeEditorModel.ts');
const { legacyList } = await load('./fixtures/node-editor-legacy.ts');
const json = value => JSON.parse(JSON.stringify(value));

// Frozen U3 protocolChanged/save logic, independent from the new builder.
function legacyServer(form, addresses, mtu) {
  const value = json(form);
  // K3b: an empty `core` means "follow the primary core" and is not sent, so the
  // frozen pre-K3b payload never contains the field.
  delete value.core;
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
// K3b core selector: only the cores the Agent reports as enabled are offered, and
// the protocol matrix follows the selected core.
const multi = { ...server, capabilities: {
  core_name: 'xray', core_version: '26.3.27', multi_core_supported: true,
  protocols: [{ protocol: 'vless', inbound: true }],
  cores: [
    { name: 'xray', version: '26.3.27', protocols: [{ protocol: 'vless', inbound: true }], forward_inbound_supported: true, device_gate_supported: true },
    { name: 'sing-box', version: '1.14.4', protocols: [{ protocol: 'vless', inbound: true }, { protocol: 'hysteria2', inbound: true }] }
  ] } };
assert.deepEqual(api.coreOptions(multi).map(core => core.name), ['xray', 'sing-box']);
assert.deepEqual(api.coreOptions(multi).map(core => core.primary), [true, false]);
assert.equal(api.defaultCore(multi), 'xray');
assert.equal(api.coreReason(multi, ''), ''); // empty = follow the primary core
assert.equal(api.coreReason(multi, 'sing-box'), '');
assert.equal(api.protocolReason(multi, 'hysteria2', 'sing-box'), '');
assert.match(api.protocolReason(multi, 'hysteria2', 'xray'), /Xray/);
assert.match(api.coreReason({ ...multi, coreSelectionBlocked: ['sing-box'] }, 'sing-box'), /core_pending/);
// An Agent that never negotiated multi-core keeps one core and cannot select the other.
assert.match(api.coreReason({ ...multi, capabilities: { ...multi.capabilities, multi_core_supported: false } }, 'sing-box'), /未协商多核心支持/);
assert.match(api.coreReason({ ...server, capabilities: { core_name: 'xray' } }, 'sing-box'), /未上报启用/);
const stepState = api.createEditor('server');
stepState.form.core = 'sing-box';
assert.match(api.validateStep(2, stepState, { ...multi, capabilities: { ...multi.capabilities, multi_core_supported: false } }, []), /未协商多核心支持/);
stepState.form.core = '';
stepState.form.name = 'multi-core-443';
assert.equal(api.validateStep(2, stepState, multi, []), '');
// The core travels in the payload, and an empty core (= primary) is omitted.
const coreState = api.createEditor('server', null, 'list');
assert.equal('core' in api.buildPayload(coreState), false);
coreState.form.core = 'sing-box';
assert.equal(api.buildPayload(coreState).core, 'sing-box');
const editCore = api.createEditor('server', { id: 'i1', nodeId: 'server', name: 'x', protocol: 'vless', core: 'sing-box', port: 443,
  listen: '0.0.0.0', enabled: true, subscriptionVisible: true, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [],
  settings: {}, hasTlsKey: false, hasWireGuardKey: false });
assert.equal(editCore.form.core, 'sing-box');
assert.equal(api.buildPayload(editCore).core, 'sing-box');
console.log('PASS NODE_EDITOR_CORE: per-core selector uses reported cores, primary = empty core, protocol matrix per core, core_pending refused, payload keeps the core');

// M3: exact reported security/transport/flow combinations, including scalar
// compatibility reports. Neither a version string nor another core authorizes
// the selected listener. Unsupported stored selections stay visible and fail.
const combinations = [
  ['reality', 'tcp', ''], ['reality', 'tcp', 'xtls-rprx-vision'], ['reality', 'grpc', ''],
  ['reality', 'xhttp', ''], ['tls', 'xhttp', '']
].map(([security, transport, flow]) => ({ protocol: 'vless', direction: 'inbound', security, transport, flow }));
const reported = { ...server, capabilities: { ...server.capabilities, server_settings_contract_version: 1, server_settings_combinations: combinations } };
const serverSettings = { security: 'reality', transport: 'tcp', flow: '' };
assert.equal(api.serverSettingsReason(reported, '', 'vless', serverSettings), '');
assert.match(api.serverSettingsReason(server, '', 'vless', serverSettings), /尚未上报/);
assert.match(api.serverSettingsReason({ ...reported, capabilities: { ...reported.capabilities, server_settings_contract_version: 2 } }, '', 'vless', serverSettings), /尚未上报/);
assert.match(api.serverSettingsReason({ ...reported, capabilities: { ...reported.capabilities, server_settings_combinations: combinations.map(c => ({ ...c, direction: 'outbound' })) } }, '', 'vless', serverSettings), /未上报支持/);
assert.match(api.serverSettingsReason(reported, '', 'vless', { ...serverSettings, transport: 'ws' }), /未上报支持/);
assert.match(api.serverSettingsReason(reported, '', 'vless', { ...serverSettings, transport: 'grpc', flow: 'xtls-rprx-vision' }), /Vision/);
assert.match(api.serverSettingsReason(reported, '', 'trojan', serverSettings), /VLESS/);
assert.match(api.serverSettingsReason({ ...reported, coreSelectionBlocked: ['xray'] }, '', 'vless', serverSettings), /core_pending/);
assert.match(api.coreReason({ ...reported, coreSelectionBlocked: ['xray'] }, ''), /core_pending/);
const reportedMulti = { ...multi, capabilities: { ...multi.capabilities,
  server_settings_contract_version: 1, server_settings_combinations: combinations,
  cores: [{ ...multi.capabilities.cores[0], server_settings_contract_version: 1, server_settings_combinations: combinations }, multi.capabilities.cores[1]] } };
assert.equal(api.serverSettingsReason(reportedMulti, 'xray', 'vless', serverSettings), '');
assert.match(api.serverSettingsReason(reportedMulti, 'sing-box', 'vless', serverSettings), /尚未上报/);
assert.match(api.serverSettingsReason({ ...reportedMulti, capabilities: { ...reportedMulti.capabilities,
  cores: [{ ...multi.capabilities.cores[0] }, multi.capabilities.cores[1]] } }, '', 'vless', serverSettings), /尚未上报/); // no scalar fallback when cores[] exists
assert.equal(api.transportChoices(reported, '', 'vless', { security: 'tls', transport: 'tcp' }).find(c => c.value === 'xhttp').reason, '');
assert.match(api.transportChoices(server, '', 'vless', { security: 'tls', transport: 'tcp' }).find(c => c.value === 'xhttp').reason, /尚未上报/);
assert.equal(api.securityChoices(reported, '', 'vless', { transport: 'tcp' }).find(c => c.value === 'reality').reason, '');
assert.equal(api.securityChoices(reported, '', 'shadowsocks', {}).some(c => c.value === 'reality'), false);
assert.match(api.flowChoices(reported, '', 'vless', { security: 'reality', transport: 'grpc' }).find(c => c.value === 'xtls-rprx-vision').reason, /Vision/);
console.log('PASS NODE_EDITOR_SERVER_CAPABILITIES: exact scalar/per-core report, unknown version/absent/foreign/partial report refused, pending primary blocked, choices preserve unsupported selections');

const realityOriginal = { id: 'reality-edit', nodeId: 'server', name: 'Reality', protocol: 'vless', port: 24443,
  listen: '0.0.0.0', enabled: true, subscriptionVisible: true, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [],
  settings: { ...serverSettings, reality: { public_key: 'A'.repeat(43), handshake_target: 'target.invalid:443', server_names: ['sni.invalid'], short_ids: ['0123abcd'] } }, hasRealityKey: true, hasTlsKey: false, hasWireGuardKey: false };
// M4's public combination flag does not remove the selected core's shape limit:
// sing-box represents one server_name, while Xray supports the bounded list.
const singboxReported = { ...reportedMulti, capabilities: { ...reportedMulti.capabilities,
  cores: [reportedMulti.capabilities.cores[0], { ...multi.capabilities.cores[1], server_settings_contract_version: 1,
    server_settings_combinations: combinations.filter(c => c.security === 'reality' && c.transport === 'tcp') }] } };
const freshSingboxEditor = api.createEditor('server');
freshSingboxEditor.form.core = 'sing-box';
assert.equal(api.securityChoices(singboxReported, 'sing-box', 'vless', freshSingboxEditor.form.settings).find(c => c.value === 'reality').reason, '');
api.changeSecurity(freshSingboxEditor, 'reality');
assert.match(api.validateStep(3, freshSingboxEditor, singboxReported, []), /sing-box.*只支持一个/);
freshSingboxEditor.form.settings.reality = { ...json(realityOriginal.settings.reality), server_names: ['one.invalid', 'two.invalid'] };
assert.equal(api.securityChoices(singboxReported, 'sing-box', 'vless', freshSingboxEditor.form.settings).find(c => c.value === 'reality').reason, '');
assert.match(api.validateStep(3, freshSingboxEditor, singboxReported, []), /sing-box.*只支持一个/);
freshSingboxEditor.form.settings.reality.server_names = ['one.invalid'];
assert.equal(api.validateStep(3, freshSingboxEditor, singboxReported, []), '');
const singboxEditor = api.createEditor('server', realityOriginal);
singboxEditor.form.core = 'sing-box';
assert.equal(api.validateStep(3, singboxEditor, singboxReported, []), '');
singboxEditor.form.settings.reality.server_names = ['one.invalid', 'two.invalid'];
const singboxDraft = json(singboxEditor.form);
assert.match(api.validateStep(3, singboxEditor, singboxReported, []), /sing-box.*只支持一个/);
assert.deepEqual(singboxEditor.form, singboxDraft);
assert.deepEqual(api.buildPayload(singboxEditor).settings.reality.server_names, ['one.invalid', 'two.invalid']);
singboxEditor.form.core = 'xray';
assert.equal(api.validateStep(3, singboxEditor, singboxReported, []), '');
const singboxScalar = { ...reported, capabilities: { ...reported.capabilities, core_name: 'sing-box' } };
assert.match(api.serverSettingsReason(singboxScalar, '', 'vless', singboxDraft.settings), /sing-box.*只支持一个/);
assert.match(api.serverSettingsReason(singboxScalar, '', 'vless', { ...singboxDraft.settings, reality: { ...singboxDraft.settings.reality, server_names: [] } }), /sing-box.*只支持一个/);
console.log('PASS NODE_EDITOR_SINGBOX_SHAPE: actual selected core enforces exactly one Reality SNI, scalar/per-core reports agree, rejected multi-name draft remains intact, Xray list remains supported');
for (const origin of ['list', 'server']) {
  const editor = api.createEditor('server', realityOriginal, origin);
  const savedReality = json(editor.form.settings.reality);
  assert.equal(api.validateStep(3, editor, reported, []), ''); // no TLS certificate for Reality
  api.changeTransport(editor, 'xhttp'); editor.form.settings.xhttp.path = '/draft + path';
  const savedXHTTP = json(editor.form.settings.xhttp);
  assert.equal(api.validateStep(3, editor, reported, []), '');
  api.changeSecurity(editor, 'tls');
  assert.match(api.validateStep(3, editor, reported, []), /证书/);
  editor.form.settings.certificate_pem = 'DRAFT-CERT'; editor.form.settings.private_key_pem = 'DRAFT-PRIVATE';
  editor.form.settings.server_name = 'tls.invalid';
  api.changeTransport(editor, 'ws'); editor.form.settings.path = '/ws-draft';
  const tlsPayload = api.buildPayload(editor);
  assert.equal('reality' in tlsPayload.settings, false); assert.equal('xhttp' in tlsPayload.settings, false);
  api.changeTransport(editor, 'grpc'); editor.form.settings.service_name = 'grpc draft';
  api.changeSecurity(editor, 'reality'); api.changeTransport(editor, 'xhttp');
  editor.form.rotateRealityKey = true;
  assert.deepEqual(editor.form.settings.reality, savedReality); assert.deepEqual(editor.form.settings.xhttp, savedXHTTP);
  const beforeBuild = json(editor.form);
  const payload = api.buildPayload(editor);
  assert.equal(payload.rotateRealityKey, true);
  assert.deepEqual(payload.settings.reality, savedReality); assert.deepEqual(payload.settings.xhttp, savedXHTTP);
  for (const key of ['certificate_pem', 'private_key_pem', 'trusted_ca_pem', 'server_name', 'path', 'service_name', 'method', 'quic', 'wireguard']) assert.equal(key in payload.settings, false, key + ' incompatible with active settings');
  assert.deepEqual(editor.form, beforeBuild); // sanitation never destroys the form
  assert.equal(JSON.stringify(api.previewPayload(payload)).includes('DRAFT-PRIVATE'), false);
  api.changeSecurity(editor, 'none'); api.changeTransport(editor, 'tcp');
  assert.equal('rotateRealityKey' in api.buildPayload(editor), false);
  assert.equal('reality' in api.buildPayload(editor).settings, false);
  api.changeSecurity(editor, 'reality'); api.changeTransport(editor, 'xhttp');
  assert.deepEqual(editor.form.settings.reality, savedReality); assert.deepEqual(editor.form.settings.xhttp, savedXHTTP);
  api.changeProtocol(editor, 'trojan'); api.changeProtocol(editor, 'vless');
  assert.deepEqual(editor.form.settings.reality, savedReality); assert.deepEqual(editor.form.settings.xhttp, savedXHTTP);
  assert.match(api.validateStep(3, editor, server, []), /尚未上报/);
  assert.equal(editor.form.settings.security, 'reality'); assert.equal(editor.form.settings.transport, 'xhttp');
}
const freshReality = api.createEditor('server'); freshReality.form.name = 'new Reality';
api.changeSecurity(freshReality, 'reality');
assert.equal(freshReality.form.settings.reality.handshake_target, '');
assert.match(api.validateStep(3, freshReality, reported, []), /握手目标/);
freshReality.form.settings.reality = json(realityOriginal.settings.reality);
freshReality.form.settings.reality.public_key = '';
assert.equal(api.validateStep(3, freshReality, reported, []), '');
assert.equal('rotateRealityKey' in api.buildPayload(freshReality), false);
assert.match(JSON.stringify(api.previewPayload(api.buildPayload(freshReality))), /保存时生成/);
for (const patch of [{ handshake_target: 'https://invalid/' }, { handshake_target: 'target.invalid:65536' }, { server_names: [] }, { server_names: ['bad name'] }, { server_names: ['*.example.com'] }, { server_names: ['127.0.0.1'] }, { server_names: ['::1'] }, { short_ids: ['1'] }, { short_ids: ['gg'] }]) {
  assert.notEqual(api.validateServerFields({ ...serverSettings, reality: { ...realityOriginal.settings.reality, ...patch } }), '');
}
assert.equal(api.validateServerFields({ ...serverSettings, reality: { ...realityOriginal.settings.reality, handshake_target: '[::1]:443' } }), '');
for (const xhttp of [{ path: 'relative', mode: 'auto' }, { path: '/', mode: 'unknown' }, { path: '/', mode: 'auto', host: 'https://invalid' }]) assert.notEqual(api.validateServerFields({ security: 'tls', transport: 'xhttp', xhttp }), '');
const foreignPrivate = { ...api.buildPayload(freshReality), settings: { security: 'reality', reality: { ...realityOriginal.settings.reality, private_key: 'SERVER-PRIVATE-MUST-NOT-APPEAR' } } };
assert.equal(JSON.stringify(api.previewPayload(foreignPrivate)).includes('SERVER-PRIVATE-MUST-NOT-APPEAR'), false);
assert.equal(JSON.stringify(api.buildPayload(api.createEditor('server', { ...realityOriginal, settings: foreignPrivate.settings }))).includes('SERVER-PRIVATE-MUST-NOT-APPEAR'), false);
freshReality.form.internalRelay = true;
const internalRealityDraft = json(freshReality.form);
assert.match(api.validateStep(3, freshReality, reported, []), /内部中转暂不支持/);
assert.match(api.validateStep(5, freshReality, reported, []), /内部中转暂不支持/);
assert.deepEqual(freshReality.form, internalRealityDraft);
assert.equal(api.internalRelaySettingsReason(true, { security: 'tls', transport: 'tcp' }), '');
assert.match(api.internalRelaySettingsReason(true, { security: 'tls', transport: 'xhttp' }), /内部中转暂不支持/);
console.log('PASS NODE_EDITOR_SERVER_DRAFTS: creation/rotation, no private key in payload/preview, Reality never requires TLS upload, inactive fields removed only from payload, transport/security/protocol switches restore drafts, bounded validation');
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
// K3b: the Agent reserves every enabled inbound's numeric port node-wide, so a
// TCP and a UDP listener can NOT share a port number (an earlier change allowed
// it, which the backend never accepted).
const mixed = [{ id: 'vless', nodeId: 'server', port: 443, name: 'VLESS', protocol: 'vless', settings: {} }];
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'hysteria2', settings: {} }, mixed), /VLESS/);
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'tuic', settings: {} }, mixed), /VLESS/);
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'shadowsocks', settings: {} }, mixed), /VLESS/);
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'udp' } } }, mixed), /VLESS/);
assert.match(api.portConflict({ ...state.form, port: 443, protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'tcp,udp' } } }, mixed), /VLESS/);
assert.equal(api.portConflict({ ...state.form, port: 443, protocol: 'hysteria2', settings: {} }, mixed, 'vless'), '');
// The random port picker follows the same rule: a UDP listener skips a TCP port.
assert.equal(api.randomPort({ ...state.form, protocol: 'hysteria2', settings: {} },
  [{ id: 'tcp', nodeId: 'server', port: 10000, name: 'TCP', protocol: 'vless', settings: {} }], '', () => 0), 10001);
console.log('PASS NODE_EDITOR_PORTS: node-wide numeric port rule (TCP and UDP cannot share a port number), random port skips taken ports');
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

// G2: GOST forward pools, ranges and limits. The editor offers them only for a
// GOST core that reports settings contract 2, and every conflict hint expands the
// range exactly like the backend does.
const gostServer = { ...server, capabilities: {
  core_name: 'xray', multi_core_supported: true,
  cores: [
    { name: 'xray', protocols: [{ protocol: 'vless', inbound: true }], forward_inbound_supported: true },
    { name: 'gost', version: '3.3.2', forward_inbound_supported: true, gost_contract_version: 2, protocols: [{ protocol: 'forward', inbound: true }] }
  ] } };
assert.equal(api.gostForwardReady(gostServer, 'gost'), true);
assert.equal(api.gostForwardReady(gostServer, ''), false); // the primary core is xray
const oldGost = { ...gostServer, capabilities: { ...gostServer.capabilities,
  cores: [gostServer.capabilities.cores[0], { ...gostServer.capabilities.cores[1], gost_contract_version: 1 }] } };
assert.equal(api.gostForwardReady(oldGost, 'gost'), false);
assert.deepEqual(api.forwardListenPorts(20000, { forward: { host: 'a', port: 1, network: 'tcp', port_end: 20002 } }), [20000, 20001, 20002]);
assert.deepEqual(api.forwardListenPorts(20000, { forward: { host: 'a', port: 1, network: 'tcp' } }), [20000]);
assert.equal(api.forwardListenPorts(20000, { forward: { host: 'a', port: 1, network: 'tcp', port_end: 30000 } }).length, 256);
const ranged = [{ id: 'fwd', nodeId: 'server', port: 20000, name: 'GOST range', protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'tcp', port_end: 20002 } } }];
const gostState = api.createEditor('server');
gostState.form.protocol = 'forward'; gostState.form.port = 20001;
assert.match(api.portConflict(gostState.form, ranged), /GOST range/);
assert.equal(api.portConflict(gostState.form, ranged, 'fwd'), '');
gostState.form.port = 20003;
assert.equal(api.portConflict(gostState.form, ranged), '');
const single = [{ id: 'one', nodeId: 'server', port: 21000, name: 'single', protocol: 'forward', settings: { forward: { host: 'a', port: 1, network: 'tcp' } } }];
const wide = api.createEditor('server');
wide.form.protocol = 'forward'; wide.form.port = 20998;
wide.form.settings.forward = { host: 'a', port: 1, network: 'tcp', port_end: 21002 };
assert.match(api.portConflict(wide.form, single), /21000/);
// Options are presence-based: a zero the Owner typed still counts.
const optionState = api.createEditor('server');
optionState.form.protocol = 'forward'; optionState.form.name = 'gost-forward'; optionState.form.port = 20500;
optionState.form.settings.forward = { host: 'a', port: 1, network: 'tcp', max_connections: 0 };
assert.deepEqual(api.gostOptions(optionState.form.settings), ['max_connections']);
// Moving the forward back onto a core without the contract is refused, not
// silently stripped.
assert.match(api.validateStep(2, optionState, gostServer, []), /GOST/);
optionState.form.core = 'gost';
assert.equal(api.validateStep(2, optionState, gostServer, []), '');
optionState.form.core = 'xray';
assert.match(api.validateStep(2, optionState, gostServer, []), /GOST/);
// Step 4 bounds: a range is 1-256 ports and a connection cap needs a TCP service.
optionState.form.core = 'gost';
optionState.form.settings.forward = { host: 'a', port: 1, network: 'tcp', port_end: 20500 + 256 };
assert.match(api.validateStep(4, optionState, gostServer, []), /256/);
optionState.form.settings.forward = { host: 'a', port: 1, network: 'tcp', port_end: 20502 };
assert.equal(api.validateStep(4, optionState, gostServer, []), '');
optionState.form.settings.forward = { host: 'a', port: 1, network: 'udp', max_connections: 5 };
assert.match(api.validateStep(4, optionState, gostServer, []), /TCP/);
optionState.form.settings.forward = { targets: [{ host: 'a', port: 1 }, { host: 'b', port: 2 }], network: 'tcp', max_connections: 5 };
assert.equal(api.validateStep(4, optionState, gostServer, []), '');
assert.deepEqual(api.forwardPool({ targets: [{ host: 'a', port: 1 }], host: '', port: 0, network: 'tcp' }), [{ host: 'a', port: 1 }]);
assert.deepEqual(api.forwardPool({ host: 'legacy', port: 443, network: 'tcp' }), [{ host: 'legacy', port: 443 }]);
console.log('PASS NODE_EDITOR_GOST: pool/range expansion, range-aware port conflicts, presence-based GOST options, core-switch refusal, step-4 bounds');

// G3: a relay listener the canvas generated has no target, network or port of its
// own, so a rename must save. Step 4 skips the forward fields it cannot have, and
// the payload carries the sealed relay material back unchanged for the panel to
// keep with the stored copy.
const relayOriginal = { id: 'relay-1', nodeId: 'server', name: 'canvas relay', protocol: 'forward', core: 'gost', port: 23000,
  listen: '0.0.0.0', enabled: true, subscriptionVisible: false, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [],
  settings: { forward: { mode: 'relay', relay: { certificate_pem: 'CERT', private_key_pem: 'KEY', client_ca_pem: 'CA' } } },
  hasTlsKey: false, hasWireGuardKey: false };
const relayState = api.createEditor('server', relayOriginal);
assert.equal(api.relayMode(relayState.form.settings), true);
assert.equal(api.relayMode({ forward: { host: 'a', port: 1, network: 'tcp' } }), false);
for (const step of [1, 2, 3, 4, 5]) assert.equal(api.validateStep(step, relayState, gostServer, []), '', `relay step ${step}`);
// The same forward without the relay mode still needs its target.
const plainForward = { ...relayOriginal, id: 'plain', settings: { forward: { host: '', port: 443, network: 'tcp' } } };
assert.match(api.validateStep(4, api.createEditor('server', plainForward), gostServer, []), /目标主机/);
relayState.form.name = 'renamed relay';
const relayPayload = api.buildPayload(relayState);
assert.equal(relayPayload.name, 'renamed relay');
assert.equal(relayPayload.core, 'gost');
assert.deepEqual(json(relayPayload.settings.forward), json(relayOriginal.settings.forward));
console.log('PASS NODE_EDITOR_RELAY: relay listener skips target/network validation and keeps its sealed material on rename');


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
// F1 adds derived source access counts; assert the contract instead of freezing the dialog markup.
assert.match(page, /通过节点分组可用：/);
assert.doesNotMatch(page, /grantUserIds|importUsers|v-model="editSubForm.userIds"/);
assert.match(page, /userIds: \[\.\.\.\(sub.userIds \|\| \[\]\)\]/);
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
    getNodePortRange: () => null,
    groupInput: group => ({ ...group, inboundIds: [...group.inboundIds], imports: [...group.imports] }),
    freshGroup: async id => ({ id, inboundIds: ['other-inbound'], imports: [{ sourceId: 'source', proxyKey: '' }] }),
    groupAPI: { save: async (id, value) => { calls.push(['group', id, value]); if (mode === 'group-error') throw Error('group refused'); } },
    choices: async read => (await read(1, 100)).items,
    proxyAPI: {
      inbounds: async () => ({ items: [], total: 0 }),
      saveInbound: async (id, value) => {
        calls.push(['save', id, value]); if (mode === 'save-error') throw Error('save refused');
        const settings = json(value.settings);
        if (settings.reality) settings.reality.public_key = 'B'.repeat(43);
        return { id: 'saved-id', settings, hasRealityKey: !!settings.reality };
      }
    },
    deploymentAPI: { publishNode: async (id, summary) => {
      calls.push(['publish', id, summary]);
      if (mode === 'publish-error') throw Error('deploy refused');
      return { revision: 42, status: 'queued' };
    } }
  };
  const run = new Function('deps', `const { ${Object.keys(deps).join(', ')} } = deps;\n${setupCode}\nreturn { save, state, servers, savedId, savedInbound, result, error, busy, selectedGroupIds, initialGroupIds };`);
  const view = run(deps);
  view.servers.value = [server]; view.state.form.name = 'Tokyo-vless-1443'; view.state.form.port = 1443;
  return { ...view, calls, events };
}
assert.deepEqual(api.withInboundMembership(['other', 'saved'], 'saved', false), ['other']);
assert.deepEqual(api.withInboundMembership(['other'], 'saved', true), ['other', 'saved']);
const groupSave = saveHarness('groups'); groupSave.selectedGroupIds.value = ['group']; await groupSave.save(true);
assert.deepEqual(groupSave.calls.map(c => c[0]), ['save', 'group', 'publish']);
assert.deepEqual(groupSave.calls[1][2].inboundIds, ['other-inbound', 'saved-id']);
assert.deepEqual(groupSave.calls[1][2].imports, [{ sourceId: 'source', proxyKey: '' }]);
const groupFailure = saveHarness('group-error'); groupFailure.selectedGroupIds.value = ['group']; await groupFailure.save(true);
assert.deepEqual(groupFailure.calls.map(c => c[0]), ['save', 'group']);
assert.match(groupFailure.error.value, /分组保存未全部完成/);
assert.equal(groupFailure.savedId.value, 'saved-id');
await groupFailure.save(true); assert.equal(groupFailure.calls.length, 2);
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
console.log('PASS NODE_EDITOR_SAVE: actual drawer handler covers draft-only, save-then-publish, retained draft on publish failure, no duplicate creation, no publish after save failure; group delta preserves other members, partial failure blocks publish');

const realityFailure = saveHarness('save-error');
realityFailure.servers.value = [reported];
realityFailure.state.original = json(realityOriginal);
realityFailure.state.form.settings = json(realityOriginal.settings);
realityFailure.state.form.rotateRealityKey = true;
const retainedRealityDraft = json(realityFailure.state.form);
await realityFailure.save(true);
assert.deepEqual(realityFailure.state.form, retainedRealityDraft);
assert.equal(realityFailure.savedId.value, '');
assert.equal(realityFailure.calls[0][2].rotateRealityKey, true);
assert.match(realityFailure.error.value, /保存草稿失败，输入已保留/);
const realityPublishedFailure = saveHarness('publish-error');
realityPublishedFailure.servers.value = [reported];
realityPublishedFailure.state.original = json(realityOriginal);
realityPublishedFailure.state.form.settings = json(realityOriginal.settings);
realityPublishedFailure.state.form.rotateRealityKey = true;
await realityPublishedFailure.save(true);
assert.deepEqual(realityPublishedFailure.calls.map(c => c[0]), ['save', 'publish']);
assert.equal(realityPublishedFailure.savedId.value, 'saved-id');
assert.equal(realityPublishedFailure.savedInbound.value.settings.reality.public_key, 'B'.repeat(43));
assert.equal(realityPublishedFailure.state.form.settings.reality.public_key, 'A'.repeat(43));
assert.match(realityPublishedFailure.error.value, /草稿已保存并保留/);
assert.equal(JSON.stringify(api.previewPayload(realityPublishedFailure.calls[0][2])).includes('private_key'), false);
assert.match(drawer, /savedInbound\.value = saved/);
assert.match(drawer, /savedInbound\?\.settings\.reality\?\.public_key/);
console.log('PASS NODE_EDITOR_REALITY_SAVE: explicit rotation sent only on save, rejected save retains form, publish failure retains persisted draft without duplicate save, returned public key has a separate readback view');
