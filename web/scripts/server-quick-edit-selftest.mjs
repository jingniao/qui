import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import { reactive, ref } from 'vue';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/nodes/components/ServerQuickEdit.vue', import.meta.url), 'utf8');
const drawerSource = readFileSync(new URL('../src/components/FormDrawer.vue', import.meta.url), 'utf8');
function functionSource(source, name) {
  const result = source.match(new RegExp(`(?:async )?function ${name}\\([^]*?^}`, 'm'))?.[0];
  assert.ok(result, `${name} exists`);
  return result;
}
const functions = ['openEditDrawer', 'saveEdit'].map(name => functionSource(source, name)).join('\n');
const code = ts.transpileModule(`${functions}\nreturn { openEditDrawer, saveEdit };`, {
  compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.None },
}).outputText;

function savingEditor() {
  const requests = [];
  let resume;
  const pending = new Promise(resolve => { resume = resolve; });
  const nodeA = { id: 'A', name: 'Server A', enabled: true, allowSubscription: true, maintenance: false };
  const nodeB = { ...nodeA, id: 'B', name: 'Server B' };
  const editForm = reactive({
    name: 'Edited A', region: ' JP ', remark: ' A note ', renewDate: '2027-01-01',
    renewCurrency: 'USD', renewPrice: '20', renewCycle: '/月',
    enabled: true, allowSubscription: false, maintenance: true, tags: ['A tag'],
  });
  const state = {
    editForm, editTarget: ref(nodeA), editDrawer: ref(true), editingSaving: ref(false),
    fetchingLatest: ref(false), fetchError: ref(''), tagInput: ref(''), tagError: ref(''),
    nodeAPI: {
      async get(id) { requests.push({ method: 'get', id }); await pending; return id === 'A' ? nodeA : nodeB; },
      async save(input) { requests.push({ method: 'save', id: input.id, input: structuredClone(input) }); },
    },
    async loadNodeMeta(id) { requests.push({ method: 'loadMeta', id }); await pending; return { region: id, extra: `${id} metadata` }; },
    async saveNodeMeta(id, input) { requests.push({ method: 'saveMeta', id, input }); },
    getNodeMeta: () => ({}), parseRenewPrice: () => ({ currency: 'CNY', price: '', cycle: '/年' }),
    mergeLatestEditForm: () => {}, showToast: () => {}, emit: () => {}, APIError: class extends Error {},
  };
  const editor = new Function(...Object.keys(state), `let loadToken = 0;\n${code}`)(...Object.values(state));
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

test('saving uses the initial target and a deep form snapshot, omitting network settings', async () => {
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
    id: 'A', name: 'Edited A', enabled: true, allowSubscription: false, maintenance: true,
  });
  assert.deepEqual(editor.requests.find(request => request.method === 'saveMeta').input, {
    extra: 'A metadata', region: 'JP', remark: 'A note', renewDate: '2027-01-01',
    renewCurrency: 'USD', renewPrice: '20', renewCycle: '/月', tags: ['A tag'],
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
