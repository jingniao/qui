import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

const source = readFileSync(new URL('../src/modules/nodes/metrics.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
assert.equal(api.uptime(undefined), '—');
assert.equal(api.nicRate({}, 'receive'), '—');
assert.equal(api.nicRate({ interfaces: [{ name: 'eth0' }] }, 'transmit'), '—');
assert.equal(api.nicRate({ interfaces: [{ name: 'eth0', receive_bytes_per_second: 1048576 }] }, 'receive'), '1.00 MiB/s');
assert.equal(api.uptime(86400), '1天 0小时 0分钟');
const oldAgent = { heartbeat: {}, lastHeartbeat: new Date(100000).toISOString() };
assert.equal(api.uptime(api.hostMetrics(oldAgent, 100000).host_uptime_seconds), '—');
assert.deepEqual(api.hostMetrics({ ...oldAgent, heartbeat: { host_uptime_seconds: 7 } }, 150000), {});
console.log('PASS AU3.1: absent 1.0.9 metrics, first NIC sample and stale heartbeat show —; measured NIC rate and host uptime render');

const metaSource = readFileSync(new URL('../src/modules/nodes/nodeMeta.ts', import.meta.url), 'utf8')
  .replace("import { reactive } from 'vue';", 'const reactive = value => value;')
  .replace("import { request } from '../../app/apiClient';", 'const request = (...args) => globalThis.nodeMetaRequest(...args);');
const metaCode = ts.transpileModule(metaSource, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const metadata = await import(`data:text/javascript;base64,${Buffer.from(metaCode).toString('base64')}`);
const local = new Map();
globalThis.localStorage = { getItem: key => local.get(key) ?? null, removeItem: key => local.delete(key), setItem: (key, value) => local.set(key, value) };
const remote = new Map();
let writes = 0;
globalThis.nodeMetaRequest = async (path, method, body) => {
  if (method === 'PUT') {
    writes++;
    if (!body.importOnly || !remote.has(path)) remote.set(path, body);
  }
  return { meta: remote.get(path) ?? {}, present: remote.has(path) };
};
local.set('qui_node_meta_a', JSON.stringify({ region: 'JP', renewPrice: '$12', renewNoticeDays: 0 }));
await metadata.loadNodeMeta('a', false);
assert.equal(writes, 0);
assert.equal(metadata.getNodeMeta('a').region, undefined);
await metadata.loadNodeMeta('a', true);
assert.equal(writes, 1);
assert.equal(metadata.getNodeMeta('a').region, 'JP');
assert.equal(metadata.getNodeMeta('a').renewPrice, '12');
assert.equal(metadata.getNodeMeta('a').renewNoticeDays, 0);
assert.equal(local.has('qui_node_meta_a'), false);
await metadata.loadNodeMeta('a', true);
assert.equal(writes, 1);
local.set('qui_node_meta_a', JSON.stringify({ region: 'US' }));
await metadata.loadNodeMeta('a', true);
assert.equal(metadata.getNodeMeta('a').region, 'JP');
assert.equal(writes, 1);
local.set('qui_node_meta_b', JSON.stringify({ region: 'GB' }));
const transport = globalThis.nodeMetaRequest;
globalThis.nodeMetaRequest = async (path, method, body) => { if (method === 'PUT') throw new Error('offline'); return transport(path, method, body); };
await assert.rejects(metadata.loadNodeMeta('b', true), /offline/);
assert.equal(local.has('qui_node_meta_b'), true);
globalThis.nodeMetaRequest = transport;
await metadata.saveNodeMeta('a', { region: 'SG', renewNoticeDays: 3 });
await metadata.loadNodeMeta('a', false);
assert.equal(metadata.getNodeMeta('a').region, 'SG');
console.log('PASS B3: Viewer never imports, Owner imports once, server wins, failed import retains local data, saved metadata reloads');

// F1 dedicated range fields must round-trip, clear, and reject invalid pairs before PUT.
await metadata.saveNodeMeta('range', { region: 'JP', portRangeStart: 21000, portRangeEnd: 21002 });
await metadata.loadNodeMeta('range', false);
assert.deepEqual(metadata.getNodePortRange('range'), { start: 21000, end: 21002 });
assert.equal(metadata.getRandomPortForNode('range', port => port === 21000, () => 0), 21001);
assert.equal(metadata.formatPortRange(metadata.getNodeMeta('range')), '21000 - 21002');
const writesBeforeInvalid = writes;
for (const range of [{portRangeStart: 1}, {portRangeStart: 2, portRangeEnd: 1}, {portRangeStart: 1.5, portRangeEnd: 2}, {portRangeStart: 1, portRangeEnd: 65536}]) {
  await assert.rejects(metadata.saveNodeMeta('range', range), /端口/);
}
assert.equal(writes, writesBeforeInvalid);
await metadata.saveNodeMeta('range', { portRangeStart: null, portRangeEnd: null });
assert.equal(metadata.getNodePortRange('range'), null);
assert.equal(metadata.formatPortRange(metadata.getNodeMeta('range')), '—');
const body = remote.get('/nodes/range/meta');
assert.deepEqual(Object.keys(body).sort(), ['region', 'remark', 'renewDate', 'renewCycle', 'renewPrice', 'renewCurrency', 'renewNoticeDays', 'portRangeStart', 'portRangeEnd'].sort());
assert.equal(body.portRangeStart, null); assert.equal(body.portRangeEnd, null);
console.log('PASS F1 PORT_RANGE: dedicated fields round-trip, random candidate uses range, invalid pairs rejected, null clears, API input whitelist');

assert.deepEqual(metadata.calculateMonthlyExpenditure({ renewPrice: '219', renewCycle: '/年', renewCurrency: 'CNY' }), { value: '18.25', currency: 'CNY', symbol: '¥' });
assert.deepEqual(metadata.calculateMonthlyExpenditure({ renewPrice: '15.99', renewCycle: '/月', renewCurrency: 'USD' }), { value: '15.99', currency: 'USD', symbol: '$' });
assert.equal(metadata.calculateMonthlyExpenditure({}), null);

const now = new Date();
const targetDate = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 20);
const pad = n => String(n).padStart(2, '0');
const in20Days = `${targetDate.getFullYear()}-${pad(targetDate.getMonth() + 1)}-${pad(targetDate.getDate())}`;
const expectedDays = metadata.calculateRemainingDays(in20Days).days;
const res = metadata.calculateResidualValue({ renewPrice: '219', renewCycle: '/年', renewCurrency: 'CNY', renewDate: in20Days });
assert.equal(res?.currency, 'CNY');
assert.equal(res?.symbol, '¥');
assert.equal(res?.value, (219 / 365 * expectedDays).toFixed(2));
console.log('PASS FINANCIAL: calculateMonthlyExpenditure and calculateResidualValue compute correct figures');
