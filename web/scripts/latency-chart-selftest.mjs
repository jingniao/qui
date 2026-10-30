import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
const source = readFileSync(new URL('../src/modules/nodes/components/latencySeries.ts', import.meta.url), 'utf8');
const code = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext } }).outputText;
const api = await import(`data:text/javascript;base64,${Buffer.from(code).toString('base64')}`);
const at = Date.parse('2026-10-28T00:00:00Z');
const point = (offset, change = {}) => ({ at: new Date(at + offset * 1000).toISOString(), avgMs: 20, minMs: 10, maxMs: 30, sent: 3, failed: 0, ...change });
assert.deepEqual(api.segments([], 300, at, at + 86400000), []);
assert.equal(api.segments([point(0), point(300), point(900)], 300, at, at + 86400000).length, 2);
assert.equal(api.segments([point(0), point(450)], 300, at, at + 86400000).length, 1);
assert.equal(api.segments([point(0), point(451)], 300, at, at + 86400000).length, 2);
const failed = point(300, { sent: 3, failed: 3, avgMs: 10 });
assert.equal(api.usable(failed), false);
assert.deepEqual(api.segments([point(0), failed, point(600)], 300, at, at + 86400000).map(part => part.length), [1, 1]);
assert.equal(api.segments([point(0), point(300, { avgMs: null }), point(600)], 300, at, at + 86400000).length, 2);
assert.equal(api.segments([point(-1), point(86401)], 300, at, at + 86400000).length, 0);
assert.deepEqual(api.segments([failed], 300, at, at + 86400000), []);
assert.equal(api.loss(failed), 1);
assert.equal(api.loss({ sent: 0, failed: 0 }), null);
assert.equal(api.loss({ sent: 3, failed: 1 }), 1 / 3);
assert.equal(api.loss({ sent: 3, failed: 8 }), 1);
assert.equal(api.usable(point(0, { avgMs: NaN })), false);
assert.equal(api.usable(point(0, { avgMs: -1 })), false);
assert.equal(api.scale(50, 0, 100, 20, 220), 120);
assert.equal(api.scale(0, 0, 0, 20, 220), 20);
console.log('PASS LATENCY gaps: missing intervals, 1.5x boundary, null and fully failed samples break lines; loss and empty series');
for (const max of [0, 0.003, 0.8, 1, 17, 99, 1000, NaN, Infinity]) {
  const ticks = api.niceTicks(max);
  assert.equal(ticks[0], 0);
  assert.ok(ticks.length >= 2 && ticks.length <= 7);
  assert.ok(ticks.every(Number.isFinite));
  assert.ok(ticks.at(-1) >= (Number.isFinite(max) ? max : 1));
  assert.ok(ticks.slice(1).every((value, index) => value > ticks[index]));
}
assert.deepEqual(api.niceTicks(99), [0, 25, 50, 75, 100]);
console.log('PASS LATENCY scaling: nice ticks cover zero, tiny, large and nonfinite maxima');
const utc = api.hourTicks(at, at + 86400000, 'UTC');
const shanghai = api.hourTicks(at, at + 86400000, 'Asia/Shanghai');
assert.equal(utc[0].label, '00:00');
assert.equal(shanghai[0].label, '08:00');
const kathmandu = api.hourTicks(at, at + 86400000, 'Asia/Kathmandu');
assert.equal(kathmandu[0].label, '06:00');
assert.equal(kathmandu[0].at, at + 15 * 60000);
const dst = api.hourTicks(Date.parse('2026-11-01T07:00:00Z'), Date.parse('2026-11-02T07:00:00Z'), 'America/Los_Angeles', 1);
assert.equal(dst.filter(tick => tick.label === '01:00').length, 2);
assert.equal(dst.length, 25);
assert.deepEqual(api.hourTicks(at + 1, at, 'UTC'), []);
console.log('PASS LATENCY timezone: UTC, Asia/Shanghai, fractional offset and DST fallback hour ticks');

// The Agent reports medians only; the chart must plot medianMs when avgMs is null.
const median = { at: new Date(at).toISOString(), medianMs: 42, avgMs: null, minMs: null, maxMs: null, sent: 3, failed: 1 };
assert.equal(api.latencyMs(median), 42);
assert.equal(api.usable(median), true);
assert.equal(api.usable({ ...median, medianMs: null }), false);
console.log('PASS LATENCY median: medianMs is plotted when avg/min/max are null');

// Stats: mean, loss %, 波动 (jitter) with gaps and fully failed points
const p1 = point(0, { medianMs: 20, sent: 3, failed: 0 });
const pFail = point(60, { medianMs: 50, sent: 3, failed: 3 }); // fully failed
const p2 = point(120, { medianMs: 40, sent: 3, failed: 1 }); // loss 1/3
const p3 = point(300, { medianMs: 30, sent: 3, failed: 0 }); // gap from 120 to 300
const stats = api.seriesStats([p1, pFail, p2, p3]);
assert.equal(stats.totalSent, 12);
assert.equal(stats.totalFailed, 4);
assert.equal(Number(stats.lossPercent.toFixed(2)), 33.33);
assert.equal(api.formatStatsLoss(stats.lossPercent), '33.33%');
assert.equal(stats.usableCount, 3);
// Usable medians are 20, 40, 30. Mean is (20 + 40 + 30) / 3 = 30
assert.equal(stats.meanMs, 30);
assert.equal(api.formatStatsMean(stats.meanMs), '30ms');
// Fluctuation (波动): consecutive usable differences |40 - 20| = 20, |30 - 40| = 10. Mean is 15.
assert.equal(stats.jitterMs, 15);
assert.equal(api.formatStatsJitter(stats.jitterMs), '15.00');
// Empty points
assert.deepEqual(api.seriesStats([]), { meanMs: null, lossPercent: null, jitterMs: null, totalSent: 0, totalFailed: 0, usableCount: 0 });
assert.equal(api.formatStatsMean(null), '—');
assert.equal(api.formatStatsLoss(null), '—');
assert.equal(api.formatStatsJitter(null), '—');
// Single usable point: jitter is null
assert.equal(api.seriesStats([p1]).jitterMs, null);
// All failed points: mean and jitter are null, loss is 100%
const allFailedStats = api.seriesStats([pFail]);
assert.equal(allFailedStats.meanMs, null);
assert.equal(allFailedStats.jitterMs, null);
assert.equal(allFailedStats.lossPercent, 100);
console.log('PASS LATENCY stats: mean, loss %, 波动 with gaps and fully failed points');

// Rolling median: 5 points window hides single spikes, preserves failures and unusable points
const spikeSeries = [
  point(0, { medianMs: 20 }),
  point(60, { medianMs: 22 }),
  point(120, { medianMs: 500 }), // spike
  point(180, { medianMs: 21 }),
  point(240, { medianMs: 23 }),
];
const smoothed = api.rollingMedian(spikeSeries, 5);
assert.equal(smoothed[2].medianMs, 22); // median of [20, 21, 22, 23, 500] is 22
// An unusable point in the series remains unusable and does not enter neighbors' medians
const withFailedPoint = [
  point(0, { medianMs: 20 }),
  point(60, { medianMs: 22 }),
  point(120, { medianMs: 500, sent: 3, failed: 3 }), // failed point
  point(180, { medianMs: 24 }),
  point(240, { medianMs: 26 }),
];
const smoothedWithFailed = api.rollingMedian(withFailedPoint, 5);
assert.equal(api.usable(smoothedWithFailed[2]), false);
console.log('PASS LATENCY rolling median: 5-point rolling window hides spikes, preserves gaps and failures');

// Nearest point / timestamp lookup
const timestamps = [at, at + 60000, at + 120000];
assert.equal(api.nearestTimestamp(timestamps, at + 10000), at);
assert.equal(api.nearestTimestamp(timestamps, at + 50000), at + 60000);
assert.equal(api.nearestTimestamp(timestamps, at + 200000), at + 120000);
assert.equal(api.nearestTimestamp([], at), null);
const pts = [point(0), point(60), point(120)];
assert.equal(api.nearestPoint(pts, at + 25000), pts[0]);
assert.equal(api.nearestPoint(pts, at + 45000), pts[1]);
assert.equal(api.nearestPoint([], at), null);
console.log('PASS LATENCY nearest point: cursor time resolves to closest sample timestamp');

// Tick generation for 1H and 7D
const ticks1h = api.timeTicks(at, at + 3600000, 'UTC', 1);
assert.equal(ticks1h.length, 13);
assert.equal(ticks1h[0].label, '00:00');
assert.equal(ticks1h[1].label, '00:05');
assert.equal(ticks1h.at(-1).label, '01:00');

const ticks7d = api.timeTicks(at, at + 168 * 3600000, 'UTC', 168);
assert.equal(ticks7d.length, 8);
assert.equal(ticks7d[0].label, '10/28');
assert.equal(ticks7d[1].label, '10/29');
assert.equal(ticks7d.at(-1).label, '11/04');
console.log('PASS LATENCY ticks: 1H minute ticks and 7D day ticks adapt to range and timezone');

// Bucket vs raw wording
const rawSample = point(0, { medianMs: 25 });
assert.equal(api.describePointLatency(rawSample, false), '中位 25 ms');
const rawDecimal = point(0, { medianMs: 25.43 });
assert.equal(api.describePointLatency(rawDecimal, false), '中位 25.43 ms');
const bucketSample = point(0, { medianMs: 25, minMs: 10, maxMs: 30 });
assert.equal(api.describePointLatency(bucketSample, true), '平均中位 25 ms（区间 10–30）');
const bucketDecimal = point(0, { medianMs: 25.5, minMs: 10.2, maxMs: 30.8 });
assert.equal(api.describePointLatency(bucketDecimal, true), '平均中位 25.50 ms（区间 10.20–30.80）');
const nullSample = point(0, { medianMs: null, avgMs: null });
assert.equal(api.describePointLatency(nullSample, false), '—');
assert.equal(api.describePointLatency(nullSample, true), '—');
console.log('PASS LATENCY wording: bucket shows interval range and raw shows median');

console.log('All latency chart self-tests passed');
