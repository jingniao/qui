import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import { computed, ref } from 'vue';

const compile = source => ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
const source = readFileSync(new URL('../src/modules/nodes/runtimeSnapshots.ts', import.meta.url), 'utf8');
const { RuntimeFeed } = await import(`data:text/javascript;base64,${Buffer.from(compile(source)).toString('base64')}`);
const hook = readFileSync(new URL('../src/modules/nodes/useNodeRuntime.ts', import.meta.url), 'utf8').replace(/^import .*;\r?\n/gm, '');
const mount = [], unmount = [], reads = [], timers = [];
const window = new EventTarget(), document = Object.assign(new EventTarget(), { hidden: false });
const authState = { sseStatus: 'connected' };
let now = Date.parse('2026-11-21T10:00:00Z');
const useNodeRuntime = new Function('onMounted', 'onUnmounted', 'ref', 'request', 'RuntimeFeed', 'window', 'document', 'authState', 'setInterval', 'clearInterval', 'Date', compile(hook).replace('export function', 'function') + '\nreturn useNodeRuntime;')(
  fn => mount.push(fn), fn => unmount.push(fn), ref,
  () => new Promise((resolve, reject) => { resolve.reject = reject; reads.push(resolve); }), RuntimeFeed, window, document, authState,
  fn => { timers.push(fn); return timers.length; }, () => {}, { now: () => now }
);
const runtime = useNodeRuntime();
const base = { id: 'alpha', lastHeartbeat: '' };
const node = revision => ({ nodeId: 'alpha', revision, status: 'online', enabled: true, maintenance: false, deletedAt: '', lastHeartbeat: new Date(now).toISOString(), offlineAfterSeconds: 180, heartbeat: {} });
const settle = async () => { for (let i = 0; i < 10; i++) await Promise.resolve(); };
const send = (generation, revision) => window.dispatchEvent(new CustomEvent('proxy-node-runtime', { detail: { generation, inventoryRevision: 1, node: node(revision) } }));
const answer = (generation, revision) => reads.shift()({ generation, inventoryRevision: 1, revision, items: [node(revision)] });
mount.forEach(fn => fn());
answer('old', 1);
await settle();
assert.equal(runtime.ready.value, true);

// A new panel generation starts calibration. More events from that same
// generation must be staged without invalidating the in-flight HTTP read.
send('new', 2);
assert.equal(runtime.ready.value, false);
send('new', 3);
answer('new', 2);
await settle();
assert.equal(runtime.ready.value, true, 'an event during calibration must not invalidate the response');
assert.equal(runtime.project(base).lastHeartbeat.length > 0, true);
assert.equal(reads.length, 0, 'no redundant calibration after accepting the new generation');

// If SSE has advanced but HTTP belongs to the old generation, immediately
// read again rather than leaving the display frozen until the next poll.
send('third', 1);
send('third', 2);
answer('new', 5);
await settle();
assert.equal(runtime.ready.value, false);
assert.equal(reads.length, 1, 'a rejected old-generation response triggers immediate calibration');
answer('third', 2);
await settle();
assert.equal(runtime.ready.value, true);

// Losing an SSE event is repaired by the normal 30-second HTTP calibration.
timers[0]();
answer('third', 4);
await settle();
assert.equal(runtime.project(base).status, 'online');

// Run the dashboard's actual projection and status computations with Vue refs.
// Static inventory can arrive late; it must not replace the runtime heartbeat.
const coreSource = readFileSync(new URL('../src/modules/nodes/coreSummary.ts', import.meta.url), 'utf8');
const { unhealthyCores } = await import(`data:text/javascript;base64,${Buffer.from(compile(coreSource)).toString('base64')}`);
const model = readFileSync(new URL('../src/modules/nodes/components/serverListModel.ts', import.meta.url), 'utf8');
const statusCode = model.match(/export function serverStatus\([\s\S]*?\n}/)[0].replace('export function', 'function');
const serverStatus = new Function('unhealthyCores', compile(statusCode) + '\nreturn serverStatus;')(unhealthyCores);
const page = readFileSync(new URL('../src/modules/dashboard/pages/DashboardPage.vue', import.meta.url), 'utf8');
const projection = page.match(/const allNodes = computed\([\s\S]*?\n}\)\);/)[0];
const offlineCount = page.match(/^const offlineNodesCount = .*$/m)[0];
const inventoryNodes = ref([{ ...node(1), id: 'alpha', lastHeartbeat: '2026-11-21T09:00:00Z' }]);
const display = new Function('computed', 'inventoryNodes', 'runtime', 'serverStatus', compile(`const statusNow = runtime.statusNow;\n${projection}\n${offlineCount}\nreturn { allNodes, offlineNodesCount };`))(
  computed, inventoryNodes, runtime, serverStatus
);
assert.equal(display.offlineNodesCount.value, 0);
const oldHeartbeat = display.allNodes.value[0].lastHeartbeat;

// Repeated foreground calibration repairs missed SSE events for half an hour.
for (let tick = 0; tick < 60; tick++) {
  now += 30000;
  timers[0]();
  answer('third', 5 + tick);
  await settle();
  timers[1]();
  assert.equal(display.offlineNodesCount.value, 0, `lost SSE event at tick ${tick}`);
  assert.equal(display.allNodes.value[0].lastHeartbeat, new Date(now).toISOString());
}
assert.notEqual(display.allNodes.value[0].lastHeartbeat, oldHeartbeat);

// A hidden tab freezes its clock. Returning to it must await a new heartbeat,
// even if an old inventory response arrives before runtime calibration.
document.hidden = true;
document.dispatchEvent(new Event('visibilitychange'));
now += 10 * 60000;
timers[1]();
assert.equal(display.offlineNodesCount.value, 0);
document.hidden = false;
document.dispatchEvent(new Event('visibilitychange'));
inventoryNodes.value = [{ ...inventoryNodes.value[0], status: 'offline' }];
assert.equal(display.offlineNodesCount.value, 0);
answer('third', 65);
await settle();
assert.equal(display.offlineNodesCount.value, 0);

// While disconnected, HTTP remains authoritative. Reopening SSE during a read
// invalidates that response once, then accepts a new calibration.
authState.sseStatus = 'reconnecting';
window.dispatchEvent(new Event('proxy-node-runtime-disconnected'));
now += 5 * 60000;
timers[1]();
assert.equal(display.offlineNodesCount.value, 0);
timers[0]();
answer('third', 66);
await settle();
assert.equal(display.offlineNodesCount.value, 0);
timers[0]();
authState.sseStatus = 'connected';
window.dispatchEvent(new Event('proxy-node-runtime-open'));
answer('third', 66);
await settle();
assert.equal(reads.length, 1);
answer('third', 67);
await settle();
assert.equal(runtime.ready.value, true);
assert.equal(display.offlineNodesCount.value, 0);

// HTTP failure freezes the display clock and preserves staged heartbeats until
// the next calibration succeeds; it must neither invent offline state nor lose
// the error message while synchronization remains broken.
timers[0]();
reads.shift().reject(new Error('runtime request timed out'));
await settle();
assert.equal(runtime.ready.value, false);
assert.match(runtime.error.value, /runtime request timed out/);
now += 10 * 60000;
timers[1]();
assert.equal(display.offlineNodesCount.value, 0);
send('third', 69);
assert.equal(display.offlineNodesCount.value, 0);
timers[0]();
answer('third', 68);
await settle();
assert.equal(runtime.ready.value, true);
assert.equal(runtime.error.value, '');
assert.equal(display.offlineNodesCount.value, 0);

// Unlike a lost event, a truly old server heartbeat must become offline.
now += 4 * 60000;
timers[1]();
assert.equal(display.offlineNodesCount.value, 1);
send('third', 70);
assert.equal(display.offlineNodesCount.value, 0);
unmount.forEach(fn => fn());
console.log('PASS dashboard runtime: generation transition, 30 minutes of lost SSE, late inventory, background recovery, reconnect, HTTP failure and real offline/recovery');
