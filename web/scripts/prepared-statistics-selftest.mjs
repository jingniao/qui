import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';

function moduleURL(path, imports = {}) {
  const source = readFileSync(new URL('../src/' + path, import.meta.url), 'utf8');
  const { outputText } = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } });
  const code = Object.entries(imports).reduce((text, [name, url]) => text.replaceAll(`'${name}'`, `'${url}'`), outputText);
  return `data:text/javascript;base64,${Buffer.from(code).toString('base64')}`;
}

const apiURL = moduleURL('app/apiClient.ts');
const { createStatisticsCache, statisticsNotice } = await import(moduleURL('app/statisticsCache.ts', {
  './apiClient': apiURL, vue: import.meta.resolve('vue')
}));
const { dashboardAPI } = await import(moduleURL('modules/dashboard/api.ts', { '../../app/apiClient': apiURL }));
let time = 1_000_000;
const cache = createStatisticsCache(() => time);
const requests = [];
const fetchOriginal = globalThis.fetch;
globalThis.fetch = url => new Promise(resolve => requests.push({ url, resolve }));
const respond = (index, body, status = 200) => requests[index].resolve(new Response(JSON.stringify(body), { status }));
const data = {
  timezone: 'UTC', dayStart: '2026-11-19T00:00:00Z', monthStart: '2026-11-01T00:00:00Z', trafficIndexReady: true,
  todayUpload: 12, todayDownload: 6, trafficObservations: 1, trend: [], nodeTraffic: [], machineTraffic: []
};
const ready = {
  status: 'ready', generation: 'worker-a', version: 3, asOf: '2026-11-19T00:00:00Z',
  generatedAt: '2026-11-19T00:00:01Z', ageSeconds: 8, data
};

try {
  const overview = cache.refresh();
  const list = cache.refresh();
  assert.equal(requests.length, 1, 'overview and list share one pending global request');
  assert.equal(requests[0].url, '/api/v1/dashboard/statistics');
  assert.equal(cache.view()?.data, undefined, 'the first read cannot show a fake zero');
  respond(0, ready);
  await Promise.all([overview, list]);
  await cache.refresh();
  assert.equal(requests.length, 1, 'opening another page immediately reuses the completed snapshot');
  assert.equal(cache.view()?.data.todayUpload, 12);
  assert.equal(cache.nextReadIn(), 22_000, 'refresh is scheduled against snapshot age');

  const detail = cache.refresh('node/a');
  assert.equal(requests[1].url, '/api/v1/statistics/nodes?nodeId=node%2Fa');
  assert.equal(cache.view('node/a')?.data, undefined, 'detail does not show global statistics or zero in place of node data');
  assert.match(statisticsNotice(cache.view('node/a')), /准备中/);
  const nodeData = { ...data, todayUpload: 7 };
  respond(1, { ...ready, data: nodeData });
  await detail;
  assert.equal(cache.view('node/a')?.data.todayUpload, 7);

  time += 22_000;
  const failed = cache.refresh();
  time += 6000;
  assert.equal(cache.view()?.status, 'stale', 'a slow refresh does not silently extend the lifetime of a ready snapshot');
  respond(2, { error: { code: 'statistics_unavailable', message: '数据库读取失败' } }, 500);
  await failed;
  const stale = cache.view();
  assert.equal(stale?.status, 'stale');
  assert.equal(stale?.ageSeconds, 36);
  assert.equal(stale?.data.todayUpload, 12, 'transport failure preserves the last successful numbers');
  assert.match(statisticsNotice(stale), /36 秒.*数据库读取失败/);

  const recovered = cache.refresh(undefined, true);
  respond(3, { status: 'pending', generation: 'worker-b', version: 0 });
  await recovered;
  assert.equal(cache.view()?.data, undefined, 'a restored worker cannot inherit the previous data');
  assert.equal(cache.view('node/a'), undefined, 'restoration clears every cached node');

  const abandoned = cache.refresh(undefined, true);
  cache.clear();
  respond(4, ready);
  await abandoned;
  assert.equal(cache.view(), undefined, 'an in-flight response from before session reset is discarded');

  const dashboardPage = readFileSync(new URL('../src/modules/dashboard/pages/DashboardPage.vue', import.meta.url), 'utf8');
  const loader = dashboardPage.match(/^async function loadDashboard\(\) \{[\s\S]*?^}/m)?.[0];
  assert.ok(loader, 'find the dashboard base loader');
  const base = { value: null }, revisions = { value: [] }, error = { value: '' };
  const loaderCode = ts.transpileModule(loader, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
  const loadBase = new Function('dashboardAPI', 'dashboard', 'allRevisions', 'error', loaderCode + '\nreturn loadDashboard;')(dashboardAPI, base, revisions, error);
  const blockedStatistics = cache.refresh();
  const firstScreen = loadBase();
  assert.equal(requests[6].url, '/api/v1/dashboard/base', 'the dashboard base does not call the combined endpoint');
  const baseResponse = { timezone: 'UTC', generatedAt: ready.generatedAt, dayStart: data.dayStart, counts: { todayBlocked: 7 }, nodes: [], revisions: [{ id: 4 }] };
  respond(6, baseResponse);
  await firstScreen;
  assert.equal(base.value.counts.todayBlocked, 7, 'the base appears while the statistics request is unresolved');
  assert.deepEqual(revisions.value, [{ id: 4 }]);
  assert.equal(cache.view(), undefined);
  respond(5, { status: 'error', generation: 'worker-b', version: 0, error: '流量读取失败' });
  await blockedStatistics;
  assert.equal(base.value.counts.todayBlocked, 7, 'a statistics error does not blank the independently loaded base');

  // Midnight or NIC changes invalidate every scope within the same service instance.
  const globalData = cache.refresh(undefined, true);
  const detailData = cache.refresh('node/a', true);
  respond(7, { ...ready, generation: 'worker-b' });
  respond(8, { ...ready, generation: 'worker-b', data: nodeData });
  await Promise.all([globalData, detailData]);
  const oldDetail = cache.refresh('node/a', true);
  const changedPeriod = cache.refresh(undefined, true);
  respond(10, { status: 'pending', generation: 'worker-b', version: 0 });
  await changedPeriod;
  assert.equal(cache.view('node/a'), undefined, 'global pending expires the cached node from the old configuration');
  const sameTransport = cache.refresh('node/a');
  assert.equal(requests.length, 11, 'an invalidated request still owns its transport until it settles');
  respond(9, { ...ready, generation: 'worker-b', data: nodeData });
  await Promise.all([oldDetail, sameTransport]);
  assert.equal(cache.view('node/a'), undefined, 'an old in-flight node read cannot repopulate the cache');

  const repeatedPending = cache.refresh(undefined, true);
  const newDetail = cache.refresh('node/a');
  assert.equal(requests.length, 13, 'the settled old transport leaves room for one fresh node read');
  respond(11, { status: 'pending', generation: 'worker-b', version: 0 });
  await repeatedPending;
  respond(12, { ...ready, generation: 'worker-b', version: 4, data: { ...nodeData, todayUpload: 2 } });
  await newDetail;
  assert.equal(cache.view('node/a')?.data.todayUpload, 2, 'a repeated pending does not cancel fresh reads for the new configuration');

  // Another page can observe the first post-midnight snapshot without seeing pending.
  const sameVersionGlobal = cache.refresh(undefined, true);
  respond(13, { ...ready, generation: 'worker-b', version: 4 });
  await sameVersionGlobal;
  assert.equal(cache.view('node/a')?.version, 4, 'same-version scopes keep each other cached');

  const lateGlobal = cache.refresh(undefined, true);
  const nextDay = cache.refresh('node/a', true);
  const newDayStart = '2026-11-20T00:00:00Z';
  const nextDayReady = {
    ...ready, generation: 'worker-b', version: 5,
    asOf: '2026-11-20T00:00:00Z', generatedAt: '2026-11-20T00:00:01Z',
    data: { ...data, dayStart: newDayStart, todayUpload: 4 }
  };
  respond(15, nextDayReady);
  await nextDay;
  assert.equal(cache.view(), undefined, 'a newer ready detail expires the pre-midnight global data before its TTL');
  const joinedLate = cache.refresh();
  assert.equal(requests.length, 16, 'switching back shares the single global request already in flight');
  respond(14, { ...ready, generation: 'worker-b', version: 4 });
  await Promise.all([lateGlobal, joinedLate]);
  assert.equal(cache.view(), undefined, 'a late lower version cannot restore the old day');

  const newGlobal = cache.refresh();
  respond(16, { ...nextDayReady, data: { ...nextDayReady.data, todayUpload: 5 } });
  await newGlobal;
  assert.equal(cache.view()?.data.dayStart, newDayStart);
  assert.equal(cache.view()?.data.todayUpload, 5);
  assert.equal(cache.view('node/a')?.data.todayUpload, 4, 'both current scopes remain available with their own data');

  const restarted = cache.refresh(undefined, true);
  respond(17, { ...nextDayReady, generation: 'worker-c', version: 1 });
  await restarted;
  assert.equal(cache.view()?.version, 1, 'version comparison starts over when the service instance changes');
  assert.equal(cache.view('node/a'), undefined, 'the previous service instance cannot contribute node data');
} finally {
  globalThis.fetch = fetchOriginal;
}

console.log('PASS prepared statistics: shared requests, node scope, freshness, failure and recovery');
