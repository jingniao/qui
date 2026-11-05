import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

// Run the page's real save/refresh actions and the real business-message helpers
// with API and reactive-state fixtures, so the toast text, the counts line, the
// reasons dialog and the draft handling are the shipped code.
const page = readFileSync(new URL('../src/modules/proxy/pages/ProxyNodeListPage.vue', import.meta.url), 'utf8');
const script = page.match(/<script setup lang="ts">([\s\S]*?)<\/script>/)[1];
const source = ts.createSourceFile('page.ts', script, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const pageNames = ['startEditSub', 'refreshSubscriptionSource', 'saveEditSub', 'addNewSubscription', 'updateSubscription', 'saveManualNodeModal', 'showImportResult', 'importOutcomeMessage', 'importFailureMessage'];
const pageFunctions = source.statements.filter(node => ts.isFunctionDeclaration(node) && pageNames.includes(node.name?.text));
assert.equal(pageFunctions.length, pageNames.length, 'page actions missing');

const api = readFileSync(new URL('../src/modules/proxy/importApi.ts', import.meta.url), 'utf8');
const apiSource = ts.createSourceFile('api.ts', api, ts.ScriptTarget.Latest, true, ts.ScriptKind.TS);
const helperNames = ['describeImportResult', 'importCountsLine', 'failedImportResult'];
const helpers = apiSource.statements.filter(node => ts.isFunctionDeclaration(node) && helperNames.includes(node.name?.text));
assert.equal(helpers.length, helperNames.length, 'import helpers missing');

const code = ts.transpileModule([...pageFunctions, ...helpers].map(node => node.getText().replace(/^export\s+/, '')).join('\n'), {
  compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.None },
}).outputText;

// One attempt result as the panel serializes it, including its fixed message.
function attempt(overrides = {}) {
  return {
    sourceId: 'saved', outcome: 'ok', stage: 'parse', code: 'import_ok',
    message: '已导入全部可识别节点',
    counts: { total: 1, accepted: 1, rejected: 0, duplicates: 0, cached: 1, usable: 1 },
    attemptedAt: '2026-11-05T00:00:00Z', lastSuccessAt: '2026-11-05T00:00:00Z',
    diagnostics: [], diagnosticsTruncated: false, ...overrides,
  };
}

// A partial attempt: some endpoints were committed, the rest were skipped.
function partial() {
  return attempt({
    outcome: 'partial', code: 'import_partial', message: '部分条目无法导入，已导入其余条目',
    counts: { total: 16, accepted: 5, rejected: 10, duplicates: 1, cached: 5, usable: 5 },
    diagnostics: [{ entryIndex: 3, stage: 'parse', code: 'unsupported_protocol', message: '暂不支持该协议' }],
  });
}

// A source that answered 403: the real status must reach the page, never zero.
function forbidden() {
  return attempt({
    outcome: 'failed', stage: 'http', code: 'source_http_status', upstreamStatus: 403,
    message: '订阅来源返回 HTTP 403；该来源的节点已从订阅中排除',
    counts: { total: null, accepted: 0, rejected: null, duplicates: null, cached: 8, usable: 0 },
    lastSuccessAt: '2026-11-04T00:00:00Z',
  });
}

// A pasted document with no importable entry: entry counts are known, and the panel
// states that no source was created instead of claiming a fetched, excluded source.
function unsupported() {
  return attempt({
    outcome: 'failed', stage: 'parse', code: 'import_paste_rejected', lastSuccessAt: null,
    message: '粘贴内容没有可导入的节点，未保存任何来源',
    counts: { total: 2, accepted: 0, rejected: 2, duplicates: 0, cached: null, usable: null },
    diagnostics: [
      { entryIndex: 0, stage: 'parse', code: 'unsupported_protocol', message: '暂不支持该协议' },
      { entryIndex: 1, stage: 'parse', code: 'invalid_entry', message: '该行不是可识别的节点链接' },
    ],
  });
}

// APIError carries the panel's result in details, exactly as apiClient delivers it.
function apiError(message, result) {
  const error = new Error(message);
  error.details = result ? { result } : {};
  return error;
}

function fixture({ savedResult = attempt(), failure = null, reloadFails = false, create = null } = {}) {
  const ref = value => ({ value });
  const calls = [], toasts = [];
  const failureResult = failure ? failure.result : null;
  const saved = { id: 'saved', name: 'Saved source', intervalSeconds: 120, userIds: ['user'], latestResult: savedResult };
  const state = {
    importAPI: {
      create: async () => {
        calls.push('create');
        if (create && create.error) throw create.error;
        return { id: saved.id, ...(create && create.result ? { result: create.result } : {}) };
      },
      update: async () => { calls.push('update'); return { id: saved.id }; },
      refresh: async id => {
        calls.push(`refresh:${id}`);
        if (failure) throw apiError(failure.message, failureResult);
        return savedResult;
      },
    },
    loadImports: async () => {
      calls.push('reload');
      if (reloadFails) throw new Error('reload failed');
      state.proxyNodeStore.subscriptions = [saved];
    },
    showToast: (message, kind = 'success') => toasts.push({ message, kind }),
    isOwner: ref(true), newSubUrl: ref('https://example.com/fixture'), newSubName: ref(saved.name),
    submittingSub: ref(false), refreshSeconds: ref(120), savingSub: ref(false), updatingSubId: ref(null),
    editingSubId: ref(saved.id), editSubForm: { name: 'Changed source', url: 'https://example.com/new', intervalSeconds: 120 },
    importDetails: ref(null), nodeForm: { name: '粘贴节点' },
    quickLinkInput: ref('socks5://fixture:fixture@example.com:1080#one'),
    openImportNodeModal: ref(true), proxyNodeStore: { subscriptions: [] },
  };
  const exported = [...pageNames, ...helperNames];
  const actions = new Function(...Object.keys(state), `${code}\nreturn { ${exported.join(',')} };`)(...Object.values(state));
  return { state, actions, saved, savedResult, calls, toasts };
}


// The three refresh callers show the panel's message and counts, reload after a
// failure and keep the saved source editable. A partial import is a warning.
for (const action of ['addNewSubscription', 'saveEditSub', 'updateSubscription']) {
  const partialRun = fixture({ savedResult: partial() });
  await partialRun.actions[action](partialRun.saved);
  assert.equal(partialRun.calls.at(-1), 'reload', `${action} reloads after a partial import`);
  assert.deepEqual(partialRun.toasts, [{ message: '导入 5 个，跳过 10 个，重复 1 个；可查看原因', kind: 'warning' }], `${action} reports a partial import as a warning`);

  const ok = fixture();
  await ok.actions[action](ok.saved);
  assert.equal(ok.calls.at(-1), 'reload');
  assert.deepEqual(ok.toasts, [{ message: '已更新：导入 1 个节点', kind: 'success' }], `${action} reports the imported count`);

  for (const reloadFails of [false, true]) {
    const failed = fixture({
      reloadFails,
      failure: { message: '订阅来源返回 HTTP 403；该来源的节点已从订阅中排除', result: forbidden() },
    });
    const before = { ...failed.state.editSubForm };
    await failed.actions[action](failed.saved);
    assert.ok(failed.calls.includes('refresh:saved'), `${action} refreshes the saved source`);
    assert.equal(failed.calls.at(-1), 'reload', `${action} reloads on refresh failure`);
    assert.deepEqual(failed.toasts, [{
      message: '订阅来源返回 HTTP 403；该来源的节点已从订阅中排除；本次导入 0 · 缓存 8 · 可用于订阅 0',
      kind: 'error',
    }], `${action} states the real upstream status`);
    assert.ok(!failed.toasts[0].message.includes('HTTP 0'), `${action} never reports HTTP 0`);
    assert.equal(failed.state.editingSubId.value, 'saved', `${action} preserves editing context`);
    assert.equal(failed.state.submittingSub.value, false);
    assert.equal(failed.state.savingSub.value, false);
    assert.equal(failed.state.updatingSubId.value, null);
    if (action === 'addNewSubscription' && !reloadFails) {
      assert.equal(failed.state.editSubForm.name, failed.saved.name, 'a saved source stays editable after a failed first fetch');
      assert.equal(failed.state.editSubForm.url, '', 'the saved URL stays on the server');
      assert.equal(failed.state.newSubUrl.value, '', 'retry does not create a duplicate source');
    }
    if (action === 'saveEditSub') assert.deepEqual(failed.state.editSubForm, before, 'a failed URL refresh keeps the draft');
  }
}

// A failure without a recorded result keeps the panel's own message.
{
  const stale = fixture({ failure: { message: '订阅来源抓取或解析失败，该来源的节点已从订阅中排除', result: null } });
  await stale.actions.updateSubscription(stale.saved);
  assert.deepEqual(stale.toasts, [{ message: '订阅来源抓取或解析失败，该来源的节点已从订阅中排除', kind: 'error' }], 'legacy failure keeps its message');
}

// A refresh cancelled while it was still fetching answers 503 with the panel's own
// message and no result at all. The page keeps that truthful text instead of an
// empty summary, opens no reason list and invents no counts.
for (const action of ['addNewSubscription', 'saveEditSub', 'updateSubscription']) {
  const cancelled = fixture({ failure: { message: '刷新已取消，来源状态保持不变', result: null } });
  const before = { ...cancelled.state.editSubForm };
  await cancelled.actions[action](cancelled.saved);
  assert.equal(cancelled.calls.at(-1), 'reload', `${action} still reloads after a cancellation`);
  assert.deepEqual(cancelled.toasts, [{ message: '刷新已取消，来源状态保持不变', kind: 'error' }], `${action} keeps the cancellation message`);
  assert.equal(cancelled.state.importDetails.value, null, `${action} opens no reason list for a cancellation`);
  assert.equal(cancelled.state.submittingSub.value, false);
  assert.equal(cancelled.state.savingSub.value, false);
  assert.equal(cancelled.state.updatingSubId.value, null);
  if (action === 'saveEditSub') assert.deepEqual(cancelled.state.editSubForm, before, 'a cancelled refresh keeps the draft');
  if (action === 'addNewSubscription') assert.equal(cancelled.state.editSubForm.name, cancelled.saved.name, 'a saved source stays editable after a cancelled fetch');
}

// A pasted import that imported everything reports a success, closes the dialog and
// clears the draft.
{
  const pasted = fixture({ create: { result: attempt({ sourceId: '' }) } });
  await pasted.actions.saveManualNodeModal();
  assert.deepEqual(pasted.calls, ['create', 'reload'], 'pasted import saves then reloads');
  assert.equal(pasted.state.quickLinkInput.value, '', 'a successful paste clears the editor');
  assert.equal(pasted.state.openImportNodeModal.value, false, 'the import dialog closes');
  assert.equal(pasted.state.importDetails.value, null, 'a clean paste opens no reason list');
  assert.deepEqual(pasted.toasts, [{ message: '已更新：导入 1 个节点', kind: 'success' }], 'pasted import reports the count');
}

// A mixed paste is a warning, and its bounded reasons open directly: a content
// source has no card of its own to open them from.
{
  const mixed = fixture({ create: { result: partial() } });
  await mixed.actions.saveManualNodeModal();
  assert.equal(mixed.state.quickLinkInput.value, '', 'a mixed paste still clears the editor');
  assert.deepEqual(mixed.toasts, [{ message: '导入 5 个，跳过 10 个，重复 1 个；可查看原因', kind: 'warning' }], 'a mixed paste is a warning');
  assert.equal(mixed.state.importDetails.value?.result.outcome, 'partial', 'the reasons open for a mixed paste');
  assert.equal(mixed.state.importDetails.value?.name, '粘贴节点', 'the reasons dialog names the pasted import');
  assert.equal(mixed.state.openImportNodeModal.value, false);
}

// An all-unsupported paste keeps the draft and the dialog, and shows the panel's
// bounded per-entry result instead of a bare message.
{
  const rejected = fixture({ create: { error: apiError('粘贴内容没有可导入的节点，未保存任何来源', unsupported()) } });
  const content = rejected.state.quickLinkInput.value;
  await rejected.actions.saveManualNodeModal();
  assert.deepEqual(rejected.calls, ['create'], 'a refused paste does not reload');
  assert.equal(rejected.state.quickLinkInput.value, content, 'a rejected paste keeps the editor contents');
  assert.equal(rejected.state.openImportNodeModal.value, true, 'the import dialog stays open');
  assert.equal(rejected.state.importDetails.value?.result.diagnostics.length, 2, 'the bounded reasons are reachable');
  assert.deepEqual(rejected.toasts, [{
    message: '粘贴内容没有可导入的节点，未保存任何来源；本次导入 0 · 跳过 2 · 重复 0 · 缓存 — · 可用于订阅 —；可查看原因',
    kind: 'error',
  }], 'a refused paste shows the panel result');
}

// A save that succeeded is never repeated, and never reported as an error, when the
// reload fails.
{
  const reloadFails = fixture({ reloadFails: true, create: { result: attempt({ sourceId: '' }) } });
  await reloadFails.actions.saveManualNodeModal();
  assert.deepEqual(reloadFails.calls, ['create', 'reload'], 'a reload failure does not save twice');
  assert.deepEqual(reloadFails.toasts, [{ message: '已更新：导入 1 个节点', kind: 'success' }], 'a saved import stays a success');
  assert.equal(reloadFails.state.openImportNodeModal.value, false);
}

// The counts line never presents an undetermined count as zero.
{
  const sample = fixture();
  const result = attempt({ counts: { total: null, accepted: 0, rejected: null, duplicates: null, cached: 3, usable: null } });
  assert.equal(sample.actions.importCountsLine(result), '本次导入 0 · 缓存 3 · 可用于订阅 —');
  const unknown = attempt({ counts: { total: null, accepted: 0, rejected: null, duplicates: null, cached: null, usable: null } });
  assert.equal(sample.actions.importCountsLine(unknown), '本次导入 0 · 缓存 — · 可用于订阅 —', 'an unknown cache is not zero');
}

console.log('PASS import refresh: business outcomes, real statuses, pasted imports, drafts and bounded reasons match the shipped messages');
