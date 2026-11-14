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
const { entryEditor } = await import(`data:text/javascript;base64,${Buffer.from(editorImports.join('\n') + '\nexport function entryEditor(props, emit) {\n' + editorCode + '\nreturn { form, error, apply, resetForServer, resetForMode, reuseOptions, protocolKey, actualGroups };\n}').toString('base64')}`);
const events = [];
const editorServers = ['a', 'b'].map(id => ({ ...server, id, name: id, enabled: true }));
const editorInbounds = [
  { id: 'a-user', nodeId: 'a', enabled: true, core: 'xray', protocol: 'vless', internalRelay: false },
  { id: 'a-internal', nodeId: 'a', enabled: true, core: 'xray', protocol: 'vless', internalRelay: true },
  { id: 'a-disabled', nodeId: 'a', enabled: false, core: 'xray', protocol: 'vless' },
  { id: 'b-user', nodeId: 'b', enabled: true, core: 'xray', protocol: 'vless' }
];
const editorProps = { bindingStateReady: true, nodeId: 'a', servers: editorServers, inbounds: editorInbounds, groups: [{ id: 'g1', enabled: true, purpose: 'subscription', inboundIds: [] }], exitProtocol: 'vless', schemeId: 'scheme' };
const advanced = entryEditor({ ...editorProps, binding: { mode: 'reuse', inboundId: 'a-user', portMode: 'auto', groupMode: 'none' } }, (...event) => events.push(event));
assert.equal(advanced.error.value, '');
assert.deepEqual(advanced.reuseOptions.value.map(item => item.id), ['a-user']);
advanced.apply();
assert.deepEqual(events.pop(), ['apply', 'a', { mode: 'reuse', inboundId: 'a-user', portMode: 'auto', groupMode: 'none' }]);
advanced.form.nodeId = 'b'; advanced.resetForServer();
assert.equal(advanced.form.inboundId, '', 'changing server clears the old reuse target');
assert.equal(advanced.form.mode, 'create');
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
advanced.form.mode = 'reuse'; advanced.resetForMode();
assert.equal(advanced.form.protocol, '');
assert.equal(advanced.form.port, undefined);
advanced.form.inboundId = 'a-user'; advanced.apply();
assert.equal(events.length, 0, 'cross-server reuse cannot be applied');
advanced.form.inboundId = 'b-user'; advanced.apply();
assert.deepEqual(events.pop()[2], { mode: 'reuse', inboundId: 'b-user', portMode: 'auto', groupMode: 'none' });
assert.match(editorSource, /@change="resetForServer"/);
assert.match(editorSource, /@change="resetForMode"/);
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
assert.equal(noBinding.form.mode, 'create');
assert.equal(noBinding.protocolKey.value, 'xray:vless', 'new entries preselect a supported exit protocol');
assert.equal(noBinding.form.groupMode, 'exit_groups');
console.log('PASS quick relay: request groups, retry identity, supported protocols, blocked cores, hop limit and advanced create/reuse editor');
