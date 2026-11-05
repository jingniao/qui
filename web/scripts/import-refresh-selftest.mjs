import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// Run the page's real action functions with API and reactive-state fixtures.
const page = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8');
const script = page.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const ast = ts.createSourceFile('page.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const names = ['startEditSub', 'refreshSubscriptionSource', 'saveEditSub', 'addNewSubscription', 'updateSubscription'];
const functions = ast.statements.filter(node => ts.isFunctionDeclaration(node) && names.includes(node.name?.text));
assert.equal(functions.length, names.length);
const code = ts.transpileModule(functions.map(node => node.getText(ast)).join('\n'), {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None },
}).outputText;

function fixture({ refreshFails = true, reloadFails = false } = {}) {
  const ref = value => ({ value });
  const calls = [], toasts = [];
  const failure = new Error('fetch or parse failed; nodes excluded');
  const saved = { id: 'saved', name: 'Saved source', intervalSeconds: 120, userIds: ['user'] };
  const state = {
    importAPI: {
      create: async () => { calls.push('create'); return { id: saved.id }; },
      update: async () => { calls.push('update'); },
      refresh: async id => { calls.push(`refresh:${id}`); if (refreshFails) throw failure; },
    },
    loadImports: async () => {
      calls.push('reload');
      if (reloadFails) throw new Error('reload failed');
      state.proxyNodeStore.subscriptions = [{ ...saved, lastStatus: refreshFails ? 'failed' : 'ok' }];
    },
    showToast: (message, kind = 'success') => toasts.push({ message, kind }),
    isOwner: ref(true), newSubUrl: ref('https://example.com/fixture'), newSubName: ref(saved.name),
    submittingSub: ref(false), refreshSeconds: ref(120), savingSub: ref(false), updatingSubId: ref(null),
    editingSubId: ref(saved.id), editSubForm: { name: 'Changed source', url: 'https://example.com/new', intervalSeconds: 120 },
    proxyNodeStore: { subscriptions: [] },
  };
  const actions = new Function(...Object.keys(state), `${code}\nreturn { ${names.join(',')} };`)(...Object.values(state));
  return { state, actions, saved, calls, toasts, failure };
}

for (const action of ['addNewSubscription', 'saveEditSub', 'updateSubscription']) {
  for (const reloadFails of [false, true]) {
    const f = fixture({ reloadFails });
    const before = { ...f.state.editSubForm };
    await f.actions[action](f.saved);
    assert.ok(f.calls.includes('refresh:saved'), `${action} refreshes saved source`);
    assert.equal(f.calls.at(-1), 'reload', `${action} reloads on refresh failure`);
    assert.deepEqual(f.toasts, [{ message: f.failure.message, kind: 'error' }], `${action} preserves refresh error without success toast`);
    assert.equal(f.state.editingSubId.value, 'saved', `${action} preserves editing context`);
    assert.equal(f.state.submittingSub.value, false);
    assert.equal(f.state.savingSub.value, false);
    assert.equal(f.state.updatingSubId.value, null);
    if (!reloadFails) {
      assert.equal(f.state.proxyNodeStore.subscriptions[0].lastStatus, 'failed');
      if (action === 'addNewSubscription') {
        assert.equal(f.state.editSubForm.name, f.saved.name, 'new saved source can be edited after first-fetch failure');
        assert.equal(f.state.editSubForm.url, '', 'saved URL is kept on the server');
        assert.equal(f.state.newSubUrl.value, '', 'retry does not create a duplicate source');
      }
    }
    if (action === 'saveEditSub') assert.deepEqual(f.state.editSubForm, before, 'failed URL refresh keeps edit draft');
  }
  const f = fixture({ refreshFails: false });
  await f.actions[action](f.saved);
  assert.equal(f.calls.at(-1), 'reload');
  assert.equal(f.toasts.length, 1);
  assert.equal(f.toasts[0].kind, 'success');
}
console.log('PASS import refresh: all three callers reload on failure, preserve errors and saved editing context, and retain success behavior');
