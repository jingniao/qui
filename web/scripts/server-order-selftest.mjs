import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/nodes/serverOrder.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const order = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);

const nodes = [{ id: 'a' }, { id: 'b', tags: ['香港', 'CN2'] }, { id: 'c', tags: ['CN2'] }, { id: 'd' }, { id: 'x', tags: ['CN2'], deletedAt: '2026-01-01' }];

// Saved order first, unknown (new) servers after in API order, deleted IDs skipped.
assert.deepEqual(order.applyServerOrder(nodes.slice(0, 4), ['c', 'gone', 'a']).map(n => n.id), ['c', 'a', 'b', 'd']);
assert.deepEqual(order.applyServerOrder(nodes.slice(0, 2), []).map(n => n.id), ['a', 'b']);

assert.deepEqual(order.reorderServer(['a', 'b', 'c', 'd'], 'a', 'c'), ['b', 'c', 'a', 'd']);
assert.deepEqual(order.reorderServer(['a', 'b', 'c', 'd'], 'd', 'b'), ['a', 'd', 'b', 'c']);

const options = order.tagOptions(nodes);
assert.deepEqual(Object.fromEntries(options.tags.map(t => [t.tag, t.count])), { CN2: 2, 香港: 1 });
assert.equal(options.untagged, 2);
assert.equal(order.matchesTag(nodes[0], order.UNTAGGED), true);
assert.equal(order.matchesTag(nodes[1], order.UNTAGGED), false);
assert.equal(order.matchesTag(nodes[2], 'CN2'), true);
assert.equal(order.matchesTag(nodes[2], '香港'), false);
assert.equal(order.matchesTag(nodes[2], ''), true);

assert.deepEqual(order.addTag(['CN2'], ' 香港 ').tags, ['CN2', '香港']);
assert.deepEqual(order.addTag(['CN2'], 'cn2').tags, ['CN2']);
assert.ok(order.addTag([], 'a,b').error);
assert.ok(order.addTag([], '一二三四五六七八九十一二三四五六七八九十一').error);
assert.ok(order.addTag(Array.from({ length: 10 }, (_, i) => String(i)), 'new').error);

console.log('PASS server order and tags: saved order, drag reorder, tag counts and filters, tag input rules');

// Execute the page's real save functions with controlled requests/timers.
const page = readFileSync(new URL('../src/modules/nodes/pages/NodeListPage.vue', import.meta.url), 'utf8');
const block = page.slice(page.indexOf('let saveTimer:'), page.indexOf('function visibleIds()'));
const compiled = ts.transpileModule(block, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
const calls = [], timers = new Map(), errors = [];
let timerId = 0;
const orderIds = { value: [] }, savedOrder = { value: { ids: [], revision: 4 } };
const ordered = { get value() { return orderIds.value.map(id => ({ id })); } };
const api = { saveServerOrder(ids, revision) { return new Promise((resolve, reject) => calls.push({ ids, revision, resolve, reject })); } };
class APIError extends Error {}
const state = new Function('orderIds', 'savedOrder', 'ordered', 'nodeAPI', 'showToast', 'APIError', 'setTimeout', 'clearTimeout', compiled + '\nreturn { setOrder, saveOrder };')(
  orderIds, savedOrder, ordered, api, message => errors.push(message), APIError,
  callback => { timers.set(++timerId, callback); return timerId; }, id => timers.delete(id));
const tick = async () => { for (let i = 0; i < 8; i++) await Promise.resolve(); };
state.setOrder(['a', 'b']);
const first = state.saveOrder();
state.setOrder(['b', 'a']);
void state.saveOrder();
assert.equal(calls.length, 1, 'only one save may be in flight');
state.setOrder(['c', 'b', 'a']);
calls[0].resolve({ ids: ['a', 'b'], revision: 5 });
await tick();
assert.equal(calls.length, 2, 'save newest order immediately after completion');
assert.deepEqual(calls[1].ids, ['c', 'b', 'a']);
assert.equal(calls[1].revision, 5);
calls[1].resolve({ ids: ['c', 'b', 'a'], revision: 6 });
await first;
await tick();
const unmount = page.match(/onUnmounted\(\(\) => \{([\s\S]*?)\n\}\);/)[1];
assert.match(unmount, /saveOrder\(/, 'leaving flushes unsaved order');
state.setOrder(['a', 'c', 'b']);
void state.saveOrder();
assert.equal(calls.length, 3, 'flush dispatches without waiting for debounce');
calls[2].reject(new Error('save failed'));
await tick();
assert.ok(errors.some(message => message.includes('save failed')));
assert.deepEqual(orderIds.value, ['c', 'b', 'a']);
console.log('PASS serialized saves, latest order, leave flush and visible failure');

// Review P2: a GET captured before the edit must not roll back a completed PUT.
const loadBlock = page.slice(page.indexOf('let generation ='), page.indexOf('async function silentRefresh'));
function orderPage(api) {
  const ids = { value: ['a', 'b'] }, saved = { value: { ids: ['a', 'b'], revision: 4 } };
  let leave = () => {};
  const guard = page.match(/onBeforeRouteLeave\([\s\S]*?\n\}\);/)?.[0] || '';
  const names = ['orderIds', 'savedOrder', 'ordered', 'nodeAPI', 'showToast', 'APIError', 'setTimeout', 'clearTimeout', 'onBeforeRouteLeave'];
  const values = [ids, saved, { get value() { return ids.value.map(id => ({ id })); } }, api, () => {}, APIError, () => 1, () => {}, fn => { leave = fn; }];
  const refs = { loading: false, error: '', isOwner: false, deleted: false, nodes: [], selected: [], warnings: [], groups: [], today: {}, month: {}, machineTraffic: {}, monthZone: '', page: 1, filtered: [] };
  for (const [name, value] of Object.entries(refs)) { names.push(name); values.push({ value }); }
  names.push('choices', 'groupAPI', 'request', 'utcInput', 'localInput', 'changePage');
  values.push(async fetch => (await fetch()).items, {}, async () => ({ items: [], timezone: 'UTC' }), x => x, x => x, () => {});
  const code = ts.transpileModule(block + '\n' + loadBlock + '\n' + guard, { compilerOptions: { target: ts.ScriptTarget.ES2020 } }).outputText;
  const result = new Function(...names, code + '\nreturn { setOrder, saveOrder, load };')(...values);
  return { ...result, ids, saved, leave: () => leave() };
}
const deferred = () => { let resolve; const promise = new Promise(r => { resolve = r; }); return { promise, resolve }; };
const oldRead = deferred();
const late = orderPage({ serverOrder: () => oldRead.promise, list: async () => ({ items: [] }), machineTrafficBatch: async () => ({ items: [] }), saveServerOrder: async ids => ({ ids, revision: 5 }) });
await late.load();
late.setOrder(['b', 'a']);
await late.saveOrder();
oldRead.resolve({ ids: ['a', 'b'], revision: 4 });
await tick();
let reviewFailures = 0;
try {
  assert.deepEqual(late.ids.value, ['b', 'a'], 'late old GET must not undo saved order');
  assert.equal(late.saved.value.revision, 5);
  console.log('PASS late sorting GET after successful PUT');
} catch (error) { reviewFailures++; console.error(error); }

// Review P2: exercise the registered route guard and a fresh page instance.
const pending = [];
let persisted = { ids: ['a', 'b'], revision: 4 };
const routeAPI = { serverOrder: async () => persisted, list: async () => ({ items: [] }), machineTrafficBatch: async () => ({ items: [] }), saveServerOrder: (ids, revision) => new Promise(resolve => pending.push({ ids, revision, finish() { persisted = { ids, revision: revision + 1 }; resolve(persisted); } })) };
const leaving = orderPage(routeAPI);
leaving.setOrder(['b', 'a']);
void leaving.saveOrder();
leaving.setOrder(['c', 'b', 'a']);
let departed = false;
const departure = Promise.resolve(leaving.leave()).then(() => { departed = true; });
await tick();
try {
  assert.equal(departed, false, 'route must wait for in-flight save and latest queued order');
  assert.equal(pending.length, 1);
  pending[0].finish();
  await tick();
  assert.equal(departed, false, 'route must also wait for queued save');
  assert.equal(pending[1].revision, 5);
  pending[1].finish();
  await departure;
  const returned = orderPage(routeAPI);
  await returned.load();
  await tick();
  assert.equal(returned.saved.value.revision, 6);
  returned.setOrder(['a', 'b', 'c']);
  const last = returned.saveOrder();
  assert.equal(pending[2].revision, 6, 'fresh instance saves against drained queue revision');
  pending[2].finish();
  await last;
  assert.deepEqual(persisted.ids, ['a', 'b', 'c']);
  console.log('PASS route departure drains queue before fresh instance return');
} catch (error) { reviewFailures++; console.error(error); }
assert.equal(reviewFailures, 0, 'C6 review sorting regressions');
