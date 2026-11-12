import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

async function load(path) {
  const source = readFileSync(new URL(path, import.meta.url), 'utf8');
  const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
  return import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
}
const policy = await load('../src/modules/nodes/components/metricsRefreshPolicy.ts');
const events = await load('../src/app/nodeStatusEvents.ts');

// Node object replaced, ID and range unchanged: not a key change.
assert.equal(policy.metricsKeyChanged({ nodeId: 'n1', hours: 1 }, { nodeId: 'n1', hours: 1 }), false);
assert.equal(policy.metricsKeyChanged({ nodeId: 'n1', hours: 1 }, { nodeId: 'n1', hours: 6 }), true);
assert.equal(policy.metricsKeyChanged({ nodeId: 'n1', hours: 1 }, { nodeId: 'n2', hours: 1 }), true);
assert.equal(policy.metricsKeyChanged(null, { nodeId: 'n1', hours: 1 }), true);

const old = { points: [1, 2, 3], loading: false, error: '' };
const background = policy.beginMetricsLoad(old, false);
assert.deepEqual(background.points, [1, 2, 3], 'background refresh keeps data');
assert.equal(background.loading, false, 'background refresh shows no hint');
const foreground = policy.beginMetricsLoad(old, true);
assert.deepEqual(foreground.points, [], 'range switch clears data');
assert.equal(foreground.loading, true, 'range switch shows hint');
const failed = policy.failMetricsLoad(background, 'boom');
assert.deepEqual(failed.points, [1, 2, 3], 'failed refresh keeps old data');
assert.equal(failed.error, 'boom');

// Component wiring.
const grid = readFileSync(new URL('../src/modules/nodes/components/ServerMetricsGrid.vue', import.meta.url), 'utf8');
assert.match(grid, /watch\(\[\(\) => props\.node\.id, \(\) => props\.hours\]/, 'watch compares id and hours separately');
assert.doesNotMatch(grid, /proxy-node-status/, 'heartbeat events do not trigger metric reads');

// Event helpers.
assert.deepEqual(events.parseNodeStatusMessage('{"nodeId":"n1"}'), { nodeId: 'n1' });
assert.deepEqual(events.parseNodeStatusMessage('not json'), { nodeId: '' });
assert.deepEqual(events.parseNodeStatusMessage(undefined), { nodeId: '' });
const ev = id => ({ detail: id === null ? undefined : { nodeId: id } });
assert.equal(events.nodeStatusMatches(ev('n1'), 'n1'), true);
assert.equal(events.nodeStatusMatches(ev('n2'), 'n1'), false);
assert.equal(events.nodeStatusMatches(ev(''), 'n1'), true, 'empty detail means all nodes');
assert.equal(events.nodeStatusMatches(ev(null), 'n1'), true);
assert.equal(events.nodeStatusMatches({}, 'n1'), true);

// Throttle: leading call, one trailing call per interval.
let clock = 0, calls = 0;
const throttled = events.throttleTrailing(() => { calls++; }, 5000, () => clock);
throttled(); assert.equal(calls, 1);
clock = 1000; throttled(); throttled(); assert.equal(calls, 1, 'throttled inside interval');
await new Promise(resolve => setTimeout(resolve, 4100));
assert.equal(calls, 2, 'one trailing call runs');
throttled.cancel();
console.log('monitoring refresh selftest passed');
