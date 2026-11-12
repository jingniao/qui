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
console.log('PASS LATENCY gaps: missing intervals, 1.5x boundary, null and fully failed samples break lines; loss and empty series');

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
const pts = [point(0), point(60), point(120)];
assert.equal(api.nearestPoint(pts, at + 25000), pts[0]);
assert.equal(api.nearestPoint(pts, at + 45000), pts[1]);
assert.equal(api.nearestPoint([], at), null);
console.log('PASS LATENCY nearest point: cursor time resolves to closest sample timestamp');

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


// Chart model (bundled with esbuild so it can import latencySeries.ts)
const { build } = await import('esbuild');
const bundled = await build({
  entryPoints: [new URL('../src/modules/nodes/components/latencyChartModel.ts', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')],
  bundle: true, write: false, format: 'esm', platform: 'node'
});
const model = await import(`data:text/javascript;base64,${Buffer.from(bundled.outputFiles[0].text).toString('base64')}`);
const target = { id: 't1', label: '上海电信' };
const series = [point(0, { medianMs: 20, sent: 3, failed: 0 }), point(60, { medianMs: 500, sent: 3, failed: 1 }), point(120, { medianMs: 22 }), point(180, { medianMs: 21, sent: 3, failed: 3 }), point(240, { medianMs: 23 })];
const entry = [{ target, colorIndex: 0, points: series }];
const on = { showLatency: true, showLoss: true, showSmooth: false };

// 12H / 24H / 7D: chart.js owns the x ticks; the model never emits a fixed tick list.
for (const hours of [12, 24, 168]) {
  const span = hours * 3600000;
  const ds = model.buildLatencyDatasets(entry, 60, on);
  assert.ok(ds.every(d => d.data.every(p => Number.isFinite(p.x))));
  assert.ok(span > 0 && ds[0].data.length >= 2, `${hours}H keeps plotted points`);
  assert.equal(model.latencyAxes(on).x, undefined);
}
const chartSource = readFileSync(new URL('../src/modules/nodes/components/MetricLineChart.vue', import.meta.url), 'utf8');
const latencySource = readFileSync(new URL('../src/modules/nodes/components/LatencyChart.vue', import.meta.url), 'utf8');
const seriesSource = readFileSync(new URL('../src/modules/nodes/components/latencySeries.ts', import.meta.url), 'utf8');
assert.match(chartSource, /autoSkip: true, maxTicksLimit: 6/);
assert.match(chartSource, /formatXTick/);
for (const forbidden of ['hourTicks', 'timeTicks', 'viewBox', '<svg']) {
  assert.ok(!latencySource.includes(forbidden) && !seriesSource.includes(forbidden), forbidden);
}
assert.match(latencySource, /defineAsyncComponent\(\(\) => import\('\.\/MetricLineChart\.vue'\)\)/);
assert.ok(!/RANGES|selectRange/.test(latencySource), 'no duplicate range buttons');
console.log('PASS LATENCY chart: 12H/24H/7D use automatic x ticks, no fixed hour-tick config, Chart.js loaded lazily');

// Loss maps to the second axis as percent; gaps and unusable samples break the line.
const both = model.buildLatencyDatasets(entry, 60, on);
assert.deepEqual(both.map(d => [d.label, d.axis]), [['上海电信 延迟', 'y'], ['上海电信 丢包', 'y1']]);
assert.deepEqual(model.latencyAxes(on), { y: { unit: 'ms' }, y1: { unit: 'percent', max: 100 } });
const lossData = both[1].data.map(p => p.y);
assert.equal(lossData[0], 0);
assert.equal(Number(lossData[1].toFixed(2)), 33.33);
assert.equal(lossData[3], 100);
assert.equal(both[1].format(100), '100%');
const latData = both[0].data.map(p => p.y);
assert.equal(latData[3], null); // fully failed sample breaks the latency line
const gapped = model.chartLine([point(0), point(600)], 60, p => 1);
assert.deepEqual(gapped.map(p => p.y), [1, null, 1]);
const lossOnly = model.buildLatencyDatasets(entry, 60, { ...on, showLatency: false });
assert.deepEqual(lossOnly.map(d => [d.label, d.axis]), [['上海电信', 'y']]);
assert.deepEqual(model.latencyAxes({ ...on, showLatency: false }), { y: { unit: 'percent', max: 100 } });
const latencyOnly = model.buildLatencyDatasets(entry, 60, { ...on, showLoss: false });
assert.deepEqual(latencyOnly.map(d => [d.label, d.axis]), [['上海电信', 'y']]);
assert.equal(model.latencyAxes({ ...on, showLoss: false }).y1, undefined);
console.log('PASS LATENCY loss: second axis in percent, line breaks, per-toggle datasets');

// Smoothing changes only the displayed latency line, never loss or the card statistics.
const smooth = model.buildLatencyDatasets(entry, 60, { ...on, showSmooth: true });
assert.equal(both[0].data[1].y, 500);
assert.ok(smooth[0].data[1].y < 100);
assert.deepEqual(smooth[1].data, both[1].data);
const before = JSON.stringify(api.seriesStats(series));
model.buildLatencyDatasets(entry, 60, { ...on, showSmooth: true });
assert.equal(JSON.stringify(api.seriesStats(series)), before);
assert.equal(series[1].medianMs, 500); // raw samples untouched
assert.match(latencySource, /seriesStats\(rawPoints\(t\.id\)\)/);
console.log('PASS LATENCY smoothing: display data only, card stats stay raw');

console.log('All latency chart self-tests passed');
