import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/proxy/canvasStatus.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { canvasStatusItems, canvasBatchInProgress } = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const status = {
  draft: { dirty: true, updatedAt: '2026-11-14T12:00:00Z', publishedAt: '' },
  batch: { items: [] }, nodes: [], entries: [],
  subscription: { ready: false, reasons: ['尚未绑定用户入口', '部署批次尚未成功'] },
  probe: { status: 'untested' }
};
const before = JSON.stringify(status);
const rows = canvasStatusItems(status);
assert.deepEqual(rows.map(row => row.key), ['draft', 'batch', 'nodes', 'entries', 'subscription', 'probe']);
assert.equal(rows[0].value, '有未发布修改');
assert.equal(rows[1].value, '尚无部署批次');
assert.equal(rows[2].value, '暂无确认记录');
assert.equal(rows[3].value, '用户入口未绑定');
assert.deepEqual(rows[4].details, status.subscription.reasons);
assert.equal(rows[5].value, '未探测');
assert.equal(JSON.stringify(status), before);
console.log('PASS canvas status: six independent dimensions, unpublished, missing entry, reasons, untested, immutable input');

const published = {
  ...status,
  draft: { ...status.draft, dirty: false, publishedAt: '2026-11-14T12:00:00Z' },
  batch: { id: 'batch', status: 'success', items: [{ nodeId: 'a', nodeName: '入口服务器', status: 'success', revision: 8 }] },
  nodes: [{ nodeId: 'a', nodeName: '入口服务器', confirmedRevision: 7, expectedRevision: 8, confirmed: false }],
  entries: [{ instanceId: 'entry', inboundId: 'inbound', name: '用户入口', mode: 'create', state: 'applied', route: 'chain:route' }],
  subscription: { ready: true, reasons: [] }
};
const complete = canvasStatusItems(published);
assert.equal(complete[0].value, '已同步');
assert.equal(complete[1].value, '成功');
assert.match(complete[1].details.join(' '), /入口服务器.*8/);
assert.equal(complete[2].value, '0 / 1 已确认');
assert.match(complete[2].details[0], /7.*8.*未确认/);
assert.match(complete[3].details[0], /用户入口.*新建.*chain:route/);
assert.equal(complete[4].value, '配置条件齐全');
assert.match(complete[4].note, /不代表每个用户都有权限/);
assert.equal(complete[5].value, '未探测');
assert.equal(canvasStatusItems({ ...published, nodes: [{ ...published.nodes[0], confirmed: true, confirmedRevision: 8 }] })[2].value, '1 / 1 已确认');
assert.equal(canvasStatusItems({ ...published, batch: { ...published.batch, status: 'failed' } })[1].value, '失败');
console.log('PASS canvas status: batch success is independent of Agent confirmation, subscription and connectivity');

assert.equal(canvasBatchInProgress(undefined), false);
assert.equal(canvasBatchInProgress('deploying'), true);
assert.equal(canvasBatchInProgress('rolling_back'), true);
assert.equal(canvasBatchInProgress('success'), false);
assert.equal(canvasBatchInProgress('failed'), false);
assert.equal(canvasBatchInProgress('rolled_back'), false);
assert.equal(canvasBatchInProgress('rollback_failed'), false);
assert.equal(canvasStatusItems({ ...published, batch: { ...published.batch, status: 'deploying' } })[1].value, '进行中');
const probed = canvasStatusItems({ ...published, probe: { status: 'unreachable', at: '2026-11-14T13:00:00Z', results: [{ inboundId: 'inbound', nodeId: 'a', status: 'unreachable', at: '2026-11-14T13:00:00Z', attempted: 3, lost: 3, code: 'timeout' }] } })[5];
assert.equal(probed.value, '入口 TCP 不可达');
assert.equal(canvasStatusItems({ ...published, probe: { status: 'incomplete' } })[5].value, '探测未完成');
assert.match(probed.details.join(' '), /2026-11-14T13:00:00Z.*timeout/);
assert.match(probed.note, /不代表整条中转链路连通/);
console.log('PASS canvas status: polling states, terminal states, recorded probe result and time');
const pollingSource = readFileSync(new URL('../src/modules/proxy/canvasStatusPolling.ts', import.meta.url), 'utf8');
const pollingCode = ts.transpileModule(pollingSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const { pollCanvasStatus } = await import(`data:text/javascript;base64,${Buffer.from(pollingCode).toString('base64')}`);
const tick = () => new Promise(resolve => setImmediate(resolve));
const timers = new Map();
let timerId = 0;
const schedule = (callback, delay) => { assert.equal(delay, 3000); timers.set(++timerId, callback); return timerId; };
const cancel = id => timers.delete(id);
const received = [];
let reads = 0;
const stop = pollCanvasStatus(async () => (++reads === 1 ? { ...status, batch: { items: [], status: 'deploying' } } : published), value => received.push(value), error => { throw error; }, canvasBatchInProgress, schedule, cancel);
await tick();
assert.equal(reads, 1);
assert.equal(timers.size, 1);
const callback = [...timers.values()][0];
timers.clear();
callback();
await tick();
assert.equal(reads, 2);
assert.equal(received.length, 2);
assert.equal(timers.size, 0);
stop();

let resolveRead;
const late = [];
const stopLate = pollCanvasStatus(() => new Promise(resolve => { resolveRead = resolve; }), value => late.push(value), error => { throw error; }, canvasBatchInProgress, schedule, cancel);
stopLate();
resolveRead({ ...status, batch: { items: [], status: 'deploying' } });
await tick();
assert.deepEqual(late, []);
assert.equal(timers.size, 0);
const stopPending = pollCanvasStatus(async () => ({ ...status, batch: { items: [], status: 'deploying' } }), () => {}, error => { throw error; }, canvasBatchInProgress, schedule, cancel);
await tick();
assert.equal(timers.size, 1);
stopPending();
assert.equal(timers.size, 0);
const failure = new Error('读取失败');
const errors = [];
pollCanvasStatus(async () => { throw failure; }, () => assert.fail('failed request must not return status'), error => errors.push(error), canvasBatchInProgress, schedule, cancel);
await tick();
assert.deepEqual(errors, [failure]);
assert.equal(timers.size, 0);
console.log('PASS canvas status polling: 3000 ms, completion stops, scheme switch/unmount suppresses late results, cancellation and errors');
console.log('canvas-status-selftest: all checks passed');
