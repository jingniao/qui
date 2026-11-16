import assert from 'node:assert/strict';
import { readFileSync, existsSync } from 'node:fs';
import ts from 'typescript';

async function moduleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  let code = ts.transpileModule(readFileSync(url, 'utf8'), {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
  }).outputText;
  const imports = [...new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(match => match[1]).filter(spec => spec.startsWith('.')))];
  for (const spec of imports) {
    const child = new URL(spec, url);
    if (!child.pathname.endsWith('.ts')) child.pathname += '.ts';
    code = code.split(`from '${spec}'`).join(`from '${await moduleUrl(child, cache)}'`);
  }
  const result = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, result);
  return result;
}

const path = new URL('../src/modules/proxy/quickRelay.ts', import.meta.url);
assert.ok(existsSync(path), 'quick relay request and protocol model must exist');
const { buildQuickRelayInput, quickRelayProtocolChoices, MAX_QUICK_RELAY_MIDDLE_NODES } = await import(await moduleUrl(path));
assert.equal(MAX_QUICK_RELAY_MIDDLE_NODES, 1);
const form = {
  entryNodeId: 'a', middleNodeIds: ['m'], exitInboundId: 'b-inbound',
  entryProtocol: 'vless', entryCore: 'xray', groupMode: 'exit_groups', groupIds: ['stale-group']
};
const input = buildQuickRelayInput(form, 'retry-id');
assert.deepEqual(input, {
  requestId: 'retry-id', entryNodeId: 'a', middleNodeIds: ['m'], exitInboundId: 'b-inbound',
  entryProtocol: 'vless', entryCore: 'xray', entryGroups: { mode: 'exit_groups' }
});
assert.deepEqual(buildQuickRelayInput(form, 'retry-id'), input, 'retry keeps the same request identity and payload');
assert.deepEqual(buildQuickRelayInput({ ...form, groupMode: 'none', entryCore: '' }, 'id').entryGroups, { mode: 'none' });
assert.equal('entryCore' in buildQuickRelayInput({ ...form, entryCore: '' }, 'id'), false);
const explicitForm = { ...form, groupMode: 'explicit', groupIds: ['g1', 'g2'] };
const explicit = buildQuickRelayInput(explicitForm, 'id');
assert.deepEqual(explicit.entryGroups, { mode: 'explicit', groupIds: ['g1', 'g2'] });
input.middleNodeIds.push('changed');
explicit.entryGroups.groupIds.push('changed');
assert.deepEqual(form.middleNodeIds, ['m'], 'request arrays do not mutate the form');
assert.deepEqual(explicitForm.groupIds, ['g1', 'g2']);
assert.deepEqual(form.groupIds, ['stale-group']);

const server = {
  capabilities: {
    multi_core_supported: true, core_name: 'xray',
    cores: [
      { name: 'xray', protocols: [{ protocol: 'vless', inbound: true }, { protocol: 'http', outbound: true }, { protocol: 'forward', inbound: true }] },
      { name: 'sing-box', protocols: [{ protocol: 'vless', inbound: true }, { protocol: 'hysteria2', inbound: true }] },
      { name: 'gost', protocols: [{ protocol: 'forward', inbound: true }] }
    ]
  }
};
assert.deepEqual(quickRelayProtocolChoices(server), [
  { protocol: 'vless', core: 'xray' }, { protocol: 'vless', core: 'sing-box' }, { protocol: 'hysteria2', core: 'sing-box' }
]);
assert.deepEqual(quickRelayProtocolChoices({ ...server, coreSelectionBlocked: ['sing-box'] }), [{ protocol: 'vless', core: 'xray' }]);
assert.deepEqual(quickRelayProtocolChoices({ capabilities: { ...server.capabilities, multi_core_supported: false } }), [{ protocol: 'vless', core: 'xray' }]);
assert.deepEqual(quickRelayProtocolChoices(undefined), []);
assert.deepEqual(quickRelayProtocolChoices({ capabilities: {} }), []);
assert.deepEqual(quickRelayProtocolChoices({ capabilities: { core_name: 'xray', protocols: [{ protocol: 'socks5', inbound: true }] } }), [{ protocol: 'socks5', core: 'xray' }]);
// Execute the advanced editor's real setup with Vue reactivity and its actual
// protocol helpers. Only the visual PortControl child is outside this harness.
const editorURL = new URL('../src/modules/proxy/components/relay/CanvasEntryEditor.vue', import.meta.url);
const editorSource = readFileSync(editorURL, 'utf8');
let editorCode = ts.transpileModule(editorSource.split('<script setup lang="ts">')[1].split('</script>')[0], {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext }
}).outputText;
const editorImports = [];
for (const match of [...editorCode.matchAll(/^import .* from '([^']+)';$/gm)]) {
  if (match[1].endsWith('.vue')) { editorCode = editorCode.replace(match[0], ''); continue; }
  const target = match[1] === 'vue' ? import.meta.resolve('vue') : await moduleUrl(new URL(match[1] + '.ts', editorURL));
  editorImports.push(match[0].replace(match[1], target));
  editorCode = editorCode.replace(match[0], '');
}
editorCode = editorCode.replace('const props = defineProps();', '').replace('const emit = defineEmits();', '');
const { entryEditor } = await import(`data:text/javascript;base64,${Buffer.from(editorImports.join('\n') + '\nexport function entryEditor(props, emit) {\n' + editorCode + '\nreturn { form, error, apply, resetForServer, resetForMode, protocolKey, actualGroups };\n}').toString('base64')}`);
const events = [];
const editorServers = ['a', 'b'].map(id => ({ ...server, id, name: id, enabled: true }));
const editorInbounds = [
  { id: 'a-user', nodeId: 'a', enabled: true, core: 'xray', protocol: 'vless', internalRelay: false },
  { id: 'a-internal', nodeId: 'a', enabled: true, core: 'xray', protocol: 'vless', internalRelay: true },
  { id: 'a-disabled', nodeId: 'a', enabled: false, core: 'xray', protocol: 'vless' },
  { id: 'b-user', nodeId: 'b', enabled: true, core: 'xray', protocol: 'vless' }
];
const editorProps = { bindingStateReady: true, nodeId: 'a', servers: editorServers, inbounds: editorInbounds, groups: [{ id: 'g1', enabled: true, purpose: 'subscription', inboundIds: [] }], exitProtocol: 'vless', schemeId: 'scheme' };
const advanced = entryEditor(editorProps, (...event) => events.push(event));
assert.equal(advanced.error.value, '');
advanced.apply();
assert.equal(events.pop()[2].mode, 'create');
assert.equal(editorSource.includes('reuse'), false, 'entry editor only creates independent entries');
advanced.form.nodeId = 'b'; advanced.resetForServer();
assert.equal(advanced.protocolKey.value, 'xray:vless');
advanced.form.groupMode = 'explicit'; advanced.form.groupIds = ['g1']; advanced.form.portMode = 'manual'; advanced.form.port = 35005;
advanced.apply();
assert.deepEqual(events.pop(), ['apply', 'b', { mode: 'create', protocol: 'vless', core: 'xray', portMode: 'manual', port: 35005, groupMode: 'explicit', groupIds: ['g1'] }]);
advanced.form.port = 0; advanced.apply();
assert.match(advanced.error.value, /1–65535/);
assert.equal(events.length, 0);
advanced.form.portMode = 'auto'; advanced.form.groupIds = ['missing'];
assert.match(advanced.error.value, /分组/);
advanced.form.groupMode = 'none'; advanced.apply();
assert.deepEqual(events.pop()[2], { mode: 'create', protocol: 'vless', core: 'xray', portMode: 'auto', groupMode: 'none' }, 'auto and none omit stale port and group fields');
assert.match(editorSource, /@change="resetForServer"/);
const primaryBinding = { mode: 'create', protocol: 'vless', portMode: 'auto', groupMode: 'none' };
const { reactive, nextTick } = await import('vue');
const primaryProps = reactive({ ...editorProps, binding: primaryBinding, materializedInboundId: '', bindingStateError: '' });
const primary = entryEditor(primaryProps, (...event) => events.push(event));
assert.equal(primary.protocolKey.value, 'xray:vless');
primary.apply();
assert.deepEqual(events.pop()[2], primaryBinding, 'an unchanged implicit primary core remains implicit');
primaryProps.materializedInboundId = 'a-user';
primaryProps.groups[0].inboundIds = ['a-user'];
primaryProps.groups[0].enabled = false;
await nextTick();
assert.deepEqual(primary.actualGroups.value.map(group => group.id), ['g1'], 'actual membership includes disabled groups');
primaryProps.groups[0].inboundIds = [];
assert.deepEqual(primary.actualGroups.value, [], 'actual no-group state ignores draft groups');
primary.form.groupMode = 'explicit'; primary.form.groupIds = ['changed'];
primary.apply();
assert.deepEqual(events.pop()?.[2], primaryBinding, 'materialized create cannot save edited group settings');
primaryProps.bindingStateReady = false;
primary.apply();
assert.equal(events.length, 0, 'unknown binding state blocks applying');
primaryProps.bindingStateError = '读取绑定失败';
assert.equal(primary.error.value, '读取绑定失败');
primaryProps.bindingStateReady = true; primaryProps.bindingStateError = '';
primaryProps.materializedInboundId = '';
primaryProps.groups[0].enabled = true;
primary.form.groupIds = ['g1'];
primary.apply();
assert.deepEqual(events.pop()[2].groupIds, ['g1'], 'unmaterialized instance remains editable in the same scheme');
const noBinding = entryEditor(editorProps, (...event) => events.push(event));
assert.equal('mode' in noBinding.form, false);
assert.equal(noBinding.protocolKey.value, 'xray:vless', 'new entries preselect a supported exit protocol');
assert.equal(noBinding.form.groupMode, 'exit_groups');
const dialog = readFileSync(new URL('../src/modules/proxy/components/QuickRelayDialog.vue', import.meta.url), 'utf8');
assert.match(dialog, /<select v-model="mode"[^>]*>[\s\S]*?代理中转[\s\S]*?纯转发[\s\S]*?<\/select>/, 'both relay modes are selectable');
assert.match(dialog, /v-if="mode === 'proxy' && protocols.length"/, 'forwarding hides entry protocol');
assert.match(dialog, /<label v-if="mode === 'proxy'"[^>]*>中间服务器/, 'forwarding hides middle server');
assert.match(dialog, /v-if="mode === 'proxy' && plan"/, 'scheme preview is proxy-only');
assert.match(dialog, /tunnelRuleAPI\.create\(\{[\s\S]*?network: 'both'[\s\S]*?target: \{ mode: 'linked', inboundId: form.exitInboundId \}/);
assert.match(dialog, /const fresh = await freshGroup\(id\)/);
assert.match(dialog, /const input = groupInput\(fresh\)/);
assert.match(dialog, /input.relayIds = \[\.\.\.new Set\(\[\.\.\.\(input.relayIds \|\| \[\]\), rule.id\]\)\]/);
assert.match(dialog, /await groupAPI.save\(id, input\)/);
assert.match(dialog, /applyTunnelChanges\(\{ tunnelIds: \[\], ruleIds: \[rule.id\] \}/);
assert.match(dialog, /no_free_port/);
assert.match(dialog, /在隧道管理中查看/);
const securityModel = await import(await moduleUrl(new URL('../src/modules/proxy/entrySecurity.ts', import.meta.url)));
const { entrySecurityChoices, entryTransportChoices, defaultEntrySecurity, entryPlaintextWarning, entrySecurityError } = securityModel;
const combinations = [
  { protocol: 'vless', direction: 'inbound', security: 'reality', transport: 'tcp' },
  { protocol: 'vless', direction: 'inbound', security: 'reality', transport: 'tcp', flow: 'xtls-rprx-vision' },
  { protocol: 'vless', direction: 'inbound', security: 'reality', transport: 'xhttp' },
  { protocol: 'vless', direction: 'inbound', security: 'tls', transport: 'xhttp' }
];
const secureServer = { ...editorServers[0], capabilities: { ...server.capabilities, cores: server.capabilities.cores.map(core => ({ ...core,
  server_settings_contract_version: 1, server_settings_combinations: core.name === 'xray' ? combinations : combinations.filter(c => c.transport === 'tcp') })) } };
assert.equal(defaultEntrySecurity(secureServer, 'xray').security, 'reality');
assert.equal(defaultEntrySecurity(server, 'xray').security, 'tls', 'missing contract defaults to TLS');
assert.deepEqual(entrySecurityChoices(server, 'xray').map(c => c.value), ['tls', 'none'], 'unreported Reality is hidden');
assert.deepEqual(entryTransportChoices(secureServer, 'sing-box', 'reality').map(c => c.value), ['vision', 'tcp'], 'sing-box xhttp is hidden');
assert.deepEqual(entryTransportChoices(secureServer, 'xray', 'reality').map(c => c.value), ['vision', 'tcp', 'xhttp']);
assert.deepEqual(entryTransportChoices(secureServer, 'xray', 'none').map(c => c.value), ['tcp']);
const secureForm = { ...form, entrySecurity: 'reality', entryTransport: 'tcp', entryFlow: 'xtls-rprx-vision', entryRealityTarget: 'www.example.com:443', entryRealityServerName: 'www.example.com' };
const secureInput = buildQuickRelayInput(secureForm, 'secure-retry');
for (const field of ['entrySecurity', 'entryTransport', 'entryFlow', 'entryRealityTarget', 'entryRealityServerName']) assert.equal(secureInput[field], secureForm[field]);
assert.deepEqual(buildQuickRelayInput(secureForm, 'secure-retry'), secureInput);
assert.equal('entrySecurity' in buildQuickRelayInput({ ...secureForm, entryProtocol: 'trojan' }, 'id'), false);
assert.equal('entryRealityTarget' in buildQuickRelayInput({ ...secureForm, entrySecurity: 'tls' }, 'id'), false);
for (const protocol of ['vless', 'http', 'socks5']) assert.match(entryPlaintextWarning('入口', protocol, 'none'), /使用不加密的.*容易被识别和干扰/);
assert.equal(entryPlaintextWarning('入口', 'vless', 'reality'), '');
assert.match(entrySecurityError(secureServer, 'xray', { security: 'reality' }), /握手目标/);
assert.equal(entrySecurityError(secureServer, 'xray', { security: 'reality', realityTarget: 'www.example.com:443', realityServerName: 'www.example.com' }), '');
assert.match(editorSource, /<EntrySecurityEditor/);
assert.match(dialog, /<EntrySecurityEditor v-if="mode === 'proxy'"/);
assert.match(dialog, /entrySecurityLabel\(entrySecurity\)/);
const securityURL = new URL('../src/modules/proxy/components/relay/EntrySecurityEditor.vue', import.meta.url);
const securitySource = readFileSync(securityURL, 'utf8');
assert.match(securitySource, /v-if="protocol === 'vless'"/);
assert.match(securitySource, /已发布入口的安全设置请在「节点列表」编辑该入站后发布/);
let securityCode = ts.transpileModule(securitySource.split('<script setup lang="ts">')[1].split('</script>')[0], { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const securityImports = [];
for (const match of [...securityCode.matchAll(/^import .* from '([^']+)';$/gm)]) {
  const target = match[1] === 'vue' ? import.meta.resolve('vue') : await moduleUrl(new URL(match[1] + '.ts', securityURL));
  securityImports.push(match[0].replace(match[1], target)); securityCode = securityCode.replace(match[0], '');
}
securityCode = securityCode.replace('const props = defineProps();', '').replace('const emit = defineEmits();', '');
const { securityEditor } = await import(`data:text/javascript;base64,${Buffer.from(securityImports.join('\n') + '\nexport function securityEditor(props, emit) {\n' + securityCode + '\nreturn { securities, transports, changeSecurity, changeTransport };\n}').toString('base64')}`);
const secureProps = reactive({ modelValue: {}, protocol: 'vless', core: 'xray', server: secureServer, name: '入口', exit: { id: 'exit', protocol: 'vless', settings: { security: 'reality', reality: { handshake_target: 'www.example.com:443', server_names: ['www.example.com'] } } } });
const secureEditor = securityEditor(secureProps, (_, value) => { secureProps.modelValue = value; });
assert.equal(secureProps.modelValue.security, 'reality', 'selector defaults to Reality');
assert.equal(secureProps.modelValue.realityTarget, 'www.example.com:443', 'prefills exit public target');
assert.equal(secureProps.modelValue.realityServerName, 'www.example.com');
secureEditor.changeTransport('xhttp');
assert.equal(secureProps.modelValue.transport, 'xhttp');
assert.equal(secureProps.modelValue.flow, '');
secureEditor.changeSecurity('none');
assert.deepEqual(secureProps.modelValue, { security: 'none', transport: 'tcp', flow: '' });
secureProps.modelValue = {};
await nextTick();
assert.equal(secureProps.modelValue.security, 'reality', 'switching to another empty draft on the same server restores the recommended default');
const readonlyEvents = [];
securityEditor({ ...secureProps, readonly: true, modelValue: { security: 'none' } }, (...event) => readonlyEvents.push(event));
assert.deepEqual(readonlyEvents, [], 'published entry emits no security changes');
console.log('PASS quick relay: request groups, retry identity, protocols, advanced editor, forwarding, entry security defaults and capability filtering');
