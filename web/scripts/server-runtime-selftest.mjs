import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(relativePath) {
  const source = readFileSync(new URL(relativePath, import.meta.url), 'utf8');
  const { outputText } = ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 }
  });
  return import(`data:text/javascript;base64,${Buffer.from(outputText).toString('base64')}`);
}

const { RuntimeFeed, applyRuntimeSnapshot } = await load('../src/modules/nodes/runtimeSnapshots.ts');
const { nicRate } = await load('../src/modules/nodes/metrics.ts');
const { parseNodeStatusMessage, nodeStatusEventChangesInventory } = await load('../src/app/nodeStatusEvents.ts');
const first = { nodeId: 'alpha', revision: 1, status: 'online', enabled: true, maintenance: false, deletedAt: '', lastHeartbeat: '2026-11-19T08:00:00Z', heartbeat: { cpu_percent: 40 } };
const second = { ...first, nodeId: 'beta', revision: 2, status: 'offline' };
const feed = new RuntimeFeed();
const online = { ...first, revision: 3, heartbeat: { cpu_percent: 0, receive_bytes_per_second: 0, transmit_bytes_per_second: 1048576 } };
const offline = { ...first, revision: 4, status: 'offline', heartbeat: {} };

// The subscription is active before the first HTTP response. The newer event
// wins on its node and leaves its peer untouched.
assert.equal(feed.receive({ generation: 'first', inventoryRevision: 2, node: online }, false), undefined);
assert.equal(feed.get('alpha'), undefined);
assert.equal(feed.calibrate({ generation: 'first', inventoryRevision: 2, revision: 2, items: [first, second] }), true);
assert.equal(feed.get('alpha'), online);
assert.equal(feed.get('beta'), second);
assert.equal(feed.matchesInventory(['alpha', 'beta'], false), true);
assert.equal(feed.matchesInventory(['alpha'], false), false, 'a base read missing a concurrent addition needs another static read');
const base = { id: 'alpha', name: 'Base name', publicIp: '192.0.2.1', status: 'offline', enabled: false, maintenance: true, deletedAt: '', lastHeartbeat: '', heartbeat: { cpu_percent: 90, memory_percent: 33 } };
const current = applyRuntimeSnapshot(base, online);
assert.equal(current.name, 'Base name');
assert.equal(current.publicIp, '192.0.2.1');
assert.equal(current.status, 'online');
assert.equal(current.heartbeat.cpu_percent, 0);
assert.equal(current.heartbeat.memory_percent, undefined);
assert.equal(nicRate(current.heartbeat, 'receive'), '0.00 MiB/s');
assert.equal(nicRate(current.heartbeat, 'transmit'), '1.00 MiB/s');
assert.equal(nicRate({}, 'receive'), '—');

// Foreground recovery freezes the previous display until the server answers.
feed.receive({ generation: 'first', inventoryRevision: 2, node: offline }, false);
assert.equal(feed.get('alpha'), online);
assert.equal(feed.calibrate({ generation: 'first', inventoryRevision: 2, revision: 3, items: [online, second] }), true);
assert.equal(feed.get('alpha').status, 'offline');
assert.equal(feed.receive({ generation: 'first', inventoryRevision: 2, node: online }, true), undefined);
assert.equal(feed.get('alpha').status, 'offline');

// Missing the last removal event is repaired by calibration. An older answer
// cannot revive the removed record or erase a newer event for another node.
const newSecond = { ...second, revision: 7, status: 'online' };
assert.equal(feed.receive({ generation: 'first', inventoryRevision: 5, node: newSecond }, true), newSecond);
feed.calibrate({ generation: 'first', inventoryRevision: 5, revision: 5, items: [second] });
assert.equal(applyRuntimeSnapshot(base, feed.get('alpha')), null);
assert.equal(feed.get('beta'), newSecond);
assert.equal(feed.matchesInventory(['alpha', 'beta'], false), false, 'a base read still listing a removed node needs another static read');
assert.equal(feed.matchesInventory(['beta'], false), true);
feed.calibrate({ generation: 'first', inventoryRevision: 2, revision: 2, items: [first, second] });
assert.equal(feed.get('alpha').removed, true);
assert.equal(feed.inventoryRevision, 5);

// A panel restart resets sequences. While a new stream awaits calibration,
// a late HTTP response from the old generation leaves the display frozen.
feed.resetStream();
feed.receive({ generation: 'second', inventoryRevision: 1, node: { ...first, revision: 2 } }, false);
assert.equal(feed.calibrate({ generation: 'first', inventoryRevision: 5, revision: 7, items: [newSecond] }), false);
assert.equal(feed.get('beta'), newSecond);
assert.equal(feed.calibrate({ generation: 'second', inventoryRevision: 1, revision: 1, items: [first] }), true);
assert.equal(feed.get('alpha').revision, 2);
assert.equal(feed.generation, 'second');
assert.equal(feed.get('beta'), undefined);

assert.equal(nodeStatusEventChangesInventory({ detail: parseNodeStatusMessage('{"nodeId":"alpha","inventoryChanged":false}') }), false);
assert.equal(nodeStatusEventChangesInventory({ detail: parseNodeStatusMessage('{"nodeId":"alpha"}') }), true);

console.log('server-runtime-selftest passed');
