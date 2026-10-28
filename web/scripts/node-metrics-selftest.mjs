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
