import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { reactive, ref } from 'vue';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/nodes/components/ServerQuickEdit.vue', import.meta.url), 'utf8');
const modelSource = readFileSync(new URL('../src/modules/nodes/components/serverListModel.ts', import.meta.url), 'utf8');
const drawerSource = readFileSync(new URL('../src/components/FormDrawer.vue', import.meta.url), 'utf8');
function functionSource(source, name) {
  const result = source.match(new RegExp(`(?:async )?function ${name}\\([^]*?^}`, 'm'))?.[0];
  assert.ok(result, `${name} exists`);
  return result;
}
const functions = [
  functionSource(modelSource, 'editedNodeMeta'),
  functionSource(source, 'openEditDrawer'),
  functionSource(source, 'saveEdit'),
].join('\n');
const code = ts.transpileModule(`${functions}\nreturn { openEditDrawer, saveEdit };`, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None },
}).outputText;

function savingEditor() {
  const requests = [];
  let resume;
  const pending = new Promise(resolve => { resume = resolve; });
  const nodeA = { id: 'A', name: 'Server A', enabled: true, allowSubscription: true, maintenance: false };
  const nodeB = { ...nodeA, id: 'B', name: 'Server B' };
  const editBaseline = {
    name: nodeA.name, region: 'A', remark: '', renewDate: '',
    renewCurrency: 'CNY', renewPrice: '', renewCycle: '/年',
    enabled: nodeA.enabled, allowSubscription: nodeA.allowSubscription,
    maintenance: nodeA.maintenance, autoRenew: false, tags: [],
  };
  const editForm = reactive({
    name: 'Edited A', region: ' JP ', remark: ' A note ', renewDate: '2027-01-01',
    renewCurrency: 'USD', renewPrice: '20', renewCycle: '/月',
    enabled: true, allowSubscription: false, maintenance: true, autoRenew: true, tags: ['A tag'],
  });
  const traffic = { method: 'sum', allowanceBytes: null, resetDay: 1 };
  const state = {
    editForm, editTarget: ref(nodeA), editDrawer: ref(true), editingSaving: ref(false),
    fetchingLatest: ref(false), fetchError: ref(''),
    machineForm: ref({ ...traffic }), connectionForm: reactive({ connectHost: '', portRange: '' }),
    fields: ref({ connectionSettings: () => ({ portRangeStart: null, portRangeEnd: null }) }),
    nodeAPI: {
      async get(id) { requests.push({ method: 'get', id }); await pending; return id === 'A' ? nodeA : nodeB; },
      async save(input) { requests.push({ method: 'save', id: input.id, input: structuredClone(input) }); },
      async machineTrafficSettings(id) { requests.push({ method: 'machineTraffic', id }); await pending; return { ...traffic, interfaceName: '' }; },
      async saveMachineTrafficSettings(id, input) { requests.push({ method: 'saveMachineTraffic', id, input }); return input; },
    },
    async loadNodeMeta(id) { requests.push({ method: 'loadMeta', id }); await pending; return { region: id, extra: `${id} metadata` }; },
    async saveNodeMeta(id, input) { requests.push({ method: 'saveMeta', id, input }); },
    getNodeMeta: () => ({}), parseRenewPrice: () => ({ currency: 'CNY', price: '', cycle: '/年' }),
    formFromSettings: settings => ({ ...settings }), payloadFromForm: form => ({ method: form.method, allowanceBytes: form.allowanceBytes, resetDay: form.resetDay }),
    addressChanges: (before, after) => before.connectHost === after.connectHost ? {} : { connectHost: after.connectHost },
    mergeLatestEditForm: () => {}, showToast: () => {}, emit: () => {}, APIError: class extends Error {},
  };
  const prelude = `let loadToken = 0;\nlet machineBaseline = ${JSON.stringify(traffic)};\nlet connectionBaseline = { connectHost: '', portRange: '' };\nlet editBaseline = ${JSON.stringify(editBaseline)};`;
  const editor = new Function(...Object.keys(state), `${prelude}\n${code}`)(...Object.values(state));
  return { ...state, ...editor, requests, resume, nodeB };
}

test('saving A ignores opening B and sends every request to A', async () => {
  const editor = savingEditor();
  const saving = editor.saveEdit();
  const opening = editor.openEditDrawer(editor.nodeB);
  editor.resume();
  await Promise.all([saving, opening]);
  assert.deepEqual(editor.requests.map(({ id }) => id), ['A', 'A', 'A', 'A']);
  assert.equal(editor.requests.find(request => request.method === 'save').input.name, 'Edited A');
});

// The dialog has no enable or subscription switches (AGENTS.md §11.1): both are
// taken from the node read at save time, and unchanged address, port range and
// traffic settings are not resubmitted.
test('saving uses the initial target, a deep form snapshot and the latest node switches', async () => {
  const editor = savingEditor();
  const saving = editor.saveEdit();
  editor.editTarget.value = editor.nodeB;
  editor.editForm.name = 'Later edit';
  editor.editForm.region = 'Later region';
  editor.editForm.tags.push('Later tag');
  editor.resume();
  await saving;
  assert.deepEqual(editor.requests.map(({ id }) => id), ['A', 'A', 'A', 'A']);
  assert.deepEqual(editor.requests.find(request => request.method === 'save').input, {
    id: 'A', name: 'Edited A', enabled: true, allowSubscription: true, maintenance: true,
  });
  assert.equal(editor.requests.some(request => request.method === 'saveMachineTraffic'), false);
  assert.deepEqual(editor.requests.find(request => request.method === 'saveMeta').input, {
    extra: 'A metadata', region: 'JP', remark: 'A note', renewDate: '2027-01-01',
    renewCurrency: 'USD', renewPrice: '20', renewCycle: '/月', autoRenew: true, tags: ['A tag'],
  });
});

test('FormDrawer ignores closing while loading and closes normally afterwards', () => {
  const props = { loading: true };
  const events = [];
  const close = new Function('props', 'emit', `${functionSource(drawerSource, 'handleClose')}\nreturn handleClose;`)(props, (...args) => events.push(args));
  close();
  assert.deepEqual(events, []);
  props.loading = false;
  close();
  assert.deepEqual(events, [['update:modelValue', false], ['close']]);
});
