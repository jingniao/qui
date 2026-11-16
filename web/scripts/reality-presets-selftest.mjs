import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { computed, reactive, ref, watch, effectScope, nextTick, shallowRef } from 'vue';

async function moduleUrl(url, cache = new Map()) {
  if (cache.has(url.href)) return cache.get(url.href);
  let code = ts.transpileModule(readFileSync(url, 'utf8'), { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  code = code.replace(/from 'vue'/g, `from '${import.meta.resolve('vue')}'`);
  for (const spec of new Set([...code.matchAll(/from\s+'([^']+)'/g)].map(m => m[1]).filter(s => s.startsWith('.')))) {
    const child = new URL(spec + '.ts', url);
    code = code.split(`from '${spec}'`).join(`from '${await moduleUrl(child, cache)}'`);
  }
  const result = `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
  cache.set(url.href, result);
  return result;
}
const model = await import(await moduleUrl(new URL('../src/modules/system/realityPresetModel.ts', import.meta.url)));
const entry = await import(await moduleUrl(new URL('../src/modules/proxy/entrySecurity.ts', import.meta.url)));
const preset = { id: 'amd', name: 'AMD', handshakeTarget: 'www.amd.com:443', serverNames: ['www.amd.com', 'amd.com'], transport: 'xhttp', flow: '' };
const saved = { items: [preset], defaultId: 'amd', revision: 1 };
const server = { capabilities: { core_name: 'xray', server_settings_contract_version: 1, server_settings_combinations: [
  { protocol: 'vless', direction: 'inbound', security: 'reality', transport: 'tcp', flow: '' },
  { protocol: 'vless', direction: 'inbound', security: 'reality', transport: 'xhttp', flow: '' }
] } };
const settings = { security: 'reality', transport: 'tcp', flow: '', reality: { public_key: 'keep', short_ids: ['00'], handshake_target: '', server_names: [] } };
const filled = model.applyRealityPreset(settings, preset, server, 'xray');
assert.equal(filled.reality.handshake_target, preset.handshakeTarget);
assert.deepEqual(filled.reality.server_names, preset.serverNames);
assert.equal(filled.reality.public_key, 'keep');
assert.deepEqual(filled.reality.short_ids, ['00']);
assert.equal(filled.transport, 'xhttp');
assert.deepEqual(filled.xhttp, { mode: 'auto', path: '/' });
const unsupported = model.applyRealityPreset({ ...settings, flow: 'xtls-rprx-vision' }, preset, undefined, 'xray');
assert.equal(unsupported.transport, 'tcp');
assert.equal(unsupported.flow, 'xtls-rprx-vision');
assert.equal(unsupported.reality.handshake_target, preset.handshakeTarget);
assert.equal(model.realityPresetChoices(null).length, 5);
assert.deepEqual(model.realityPresetChoices(saved).map(c => c.label), ['AMD']);
assert.equal(model.defaultRealityPreset(saved)?.id, 'amd');
assert.equal(model.defaultRealityPreset(null), undefined);
const exit = { settings: { reality: { handshake_target: 'exit.example:443', server_names: ['exit.example'] } } };
const initial = entry.resolveEntryReality({ security: 'reality', transport: 'tcp', flow: '' }, saved, exit, server, 'xray');
assert.equal(initial.realityTarget, preset.handshakeTarget);
assert.equal(initial.realityServerName, 'www.amd.com');
assert.equal(initial.transport, 'xhttp');
const bound = { security: 'reality', transport: 'tcp', flow: '', realityTarget: 'bound.example:443', realityServerName: 'bound.example' };
assert.deepEqual(entry.resolveEntryReality(bound, saved, exit, server, 'xray'), bound);
const fromExit = entry.resolveEntryReality({ security: 'reality' }, null, exit, server, 'xray');
assert.equal(fromExit.realityTarget, 'exit.example:443');
assert.equal(fromExit.realityServerName, 'exit.example');
const blocked = entry.applyEntryRealityPreset({ security: 'reality', transport: 'tcp', flow: '' }, preset, undefined, 'xray');
assert.equal(blocked.transport, 'tcp');
assert.equal(blocked.flow, '');
console.log('PASS preset fields, keys retained, supported combinations and unsupported transport/flow');
console.log('PASS bound values > system default > exit Reality; five built-in targets fallback');

const cacheURL = await moduleUrl(new URL('../src/modules/system/realityPresets.ts', import.meta.url));
const cache = await import(cacheURL);
const originalFetch = globalThis.fetch;
let reads = 0;
try {
  globalThis.fetch = async url => { assert.equal(url, '/api/v1/system/reality-presets'); reads++; return Response.json(saved); };
  const [a, b, c] = await Promise.all([cache.loadRealityPresets(), cache.loadRealityPresets(), cache.loadRealityPresets()]);
  assert.deepEqual(a, saved); assert.equal(a, b); assert.equal(b, c);
  await cache.loadRealityPresets();
  assert.equal(reads, 1, 'concurrent and subsequent consumers share one read');
  cache.rememberRealityPresets({ ...saved, defaultId: '' });
  assert.equal((await cache.loadRealityPresets()).defaultId, '');
  const failed = await import(cacheURL + '#failed');
  globalThis.fetch = async () => { reads++; throw new Error('offline'); };
  assert.equal(await failed.loadRealityPresets(), null);
  assert.equal(await failed.loadRealityPresets(), null);
  assert.equal(reads, 2, 'a failed shared read silently falls back without repeated requests');
} finally { globalThis.fetch = originalFetch; }
console.log('PASS one shared request, settings cache replacement and silent failure fallback');

// Run actual component setup and watchers. Network/certificate operations are
// outside these scenarios; all preset and capability logic is production code.
const nodeModel = await import(await moduleUrl(new URL('../src/modules/proxy/components/node-editor/nodeEditorModel.ts', import.meta.url)));
const presetRef = shallowRef(saved), readyRef = ref(true);
const useRealityPresets = () => ({ presets: presetRef, ready: readyRef });
function setupComponent(path, props, extra, returned) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  const setup = source.split('<script setup lang="ts">')[1].split('</script>')[0];
  const imported = [...setup.matchAll(/^import \{([^}]+)\} from /gm)].flatMap(m => m[1].split(',').map(name => name.trim().split(/\s+as\s+/).pop()));
  const script = setup.replace(/^import .*;\r?\n/gm, '');
  const code = ts.transpileModule(script, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  const deps = { ...nodeModel, ...model, ...entry, computed, ref, watch, useRealityPresets, onUnmounted: () => {}, defineProps: () => props, defineEmits: () => (_, value) => { props.modelValue = value; }, ...extra };
  const names = [...new Set([...imported.filter(name => name in deps), 'defineProps', 'defineEmits'])];
  return new Function('deps', `const { ${names.join(', ')} } = deps;\n${code}\nreturn { ${returned} };`)(deps);
}
const scope = effectScope();
await scope.run(async () => {
  const state = reactive(nodeModel.createEditor('server'));
  state.form.protocol = 'vless'; state.form.core = 'xray';
  const node = setupComponent('../src/modules/proxy/components/node-editor/NodeProtocolFields.vue', { state, server }, {}, 'pickPreset, setSecurity, presetChoices');
  node.setSecurity({ target: { value: 'reality' } }); await nextTick();
  assert.equal(state.form.settings.reality.handshake_target, preset.handshakeTarget, 'new Reality applies system default');
  assert.equal(state.form.settings.transport, 'xhttp');
  const existing = reactive(nodeModel.createEditor('server', { ...state.form, id: 'existing', settings: { ...state.form.settings, reality: { ...state.form.settings.reality, handshake_target: 'kept.example:443', server_names: ['kept.example'] } } }));
  setupComponent('../src/modules/proxy/components/node-editor/NodeProtocolFields.vue', { state: existing, server }, {}, 'pickPreset');
  await nextTick();
  assert.equal(existing.form.settings.reality.handshake_target, 'kept.example:443', 'editing never automatically applies default');
  presetRef.value = null;
  await nextTick();
  assert.equal(node.presetChoices.value.length, 5);
  state.form.settings.reality.server_names = ['custom.example'];
  const select = { value: 'www.apple.com:443' }; node.pickPreset({ target: select }); await nextTick();
  assert.equal(state.form.settings.reality.handshake_target, 'www.apple.com:443');
  assert.deepEqual([...state.form.settings.reality.server_names], ['custom.example'], 'built-in selection preserves existing SNI');
  presetRef.value = saved;

  const props = reactive({ modelValue: {}, server, protocol: 'vless', core: 'xray', name: 'entry', exit });
  readyRef.value = false;
  const relay = setupComponent('../src/modules/proxy/components/relay/EntrySecurityEditor.vue', props, {}, 'pickPreset, changeSecurity');
  assert.deepEqual(props.modelValue, {}, 'exit fallback must wait for shared loading before applying default');
  readyRef.value = true; await nextTick();
  assert.equal(props.modelValue.realityTarget, preset.handshakeTarget);
  assert.equal(props.modelValue.transport, 'xhttp');
  props.modelValue = { ...bound }; await nextTick();
  assert.deepEqual({ ...props.modelValue }, bound);
  relay.pickPreset({ target: { value: 'amd' } }); await nextTick();
  assert.equal(props.modelValue.realityTarget, preset.handshakeTarget);
  const readOnly = reactive({ ...props, readonly: true, modelValue: { ...bound } });
  setupComponent('../src/modules/proxy/components/relay/EntrySecurityEditor.vue', readOnly, {}, 'pickPreset');
  await nextTick(); assert.deepEqual({ ...readOnly.modelValue }, bound);
  props.modelValue = { security: 'reality', transport: 'tcp', flow: '' };
  presetRef.value = null; props.exit = undefined; await nextTick();
  assert.equal(props.modelValue.realityTarget || '', '', 'empty fallback does not loop on model updates');
});
scope.stop();
console.log('PASS actual node/relay setup: defaults, existing values, fallback, readonly and delayed loading');
console.log('All Reality presets self-tests passed');
