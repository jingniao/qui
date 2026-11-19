import { shallowReactive } from 'vue';
import { APIError, request } from './apiClient';
import type { MachineTrafficSummary } from '../modules/nodes/api';

export interface NodeTraffic {
  nodeId: string;
  todayUpload: number;
  todayDownload: number;
  todayMeasured: boolean;
  monthUpload: number;
  monthDownload: number;
  measured: boolean;
  historyError?: string;
}

interface StatisticsData {
  timezone: string;
  dayStart: string;
  monthStart: string;
  trafficIndexReady: boolean;
  todayUpload: number;
  todayDownload: number;
  trafficObservations: number;
  trend: { time: string; upload: number; download: number }[];
  nodeTraffic: NodeTraffic[];
  machineTraffic: MachineTrafficSummary[];
  machineError?: string;
}

interface StatisticsVersion {
  generation: string;
  version: number;
  error?: string;
}

interface PendingStatistics extends StatisticsVersion {
  status: 'pending' | 'error';
  data?: never;
}

interface ProducedStatistics extends StatisticsVersion {
  status: 'ready' | 'stale';
  asOf: string;
  generatedAt: string;
  ageSeconds: number;
  data: StatisticsData;
}

type StatisticsView = PendingStatistics | ProducedStatistics;

function hasData(view?: StatisticsView): view is ProducedStatistics {
  return view?.status === 'ready' || view?.status === 'stale';
}

interface CachedStatistics {
  view: StatisticsView;
  receivedAt: number;
  nextRead: number;
}

function readInterval(view: StatisticsView) {
  switch (view.status) {
    case 'ready': return Math.max(1000, (30 - view.ageSeconds) * 1000);
    case 'pending': return 2000;
    case 'stale': return 3000;
    case 'error': return 5000;
  }
}

export function createStatisticsCache(now = () => performance.now()) {
  const entries = shallowReactive(new Map<string, CachedStatistics>());
  const reads = new Map<string, Promise<void>>();
  let generation = '';
  let latestVersion = 0;
  let preparing = false;
  let readEpoch = 0;

  function expireEntries() {
    readEpoch++;
    // Keep each in-flight read until it settles. The HTTP client also
    // coalesces GETs, so starting again now would just rejoin the old response.
    entries.clear();
  }

  function clear() {
    generation = '';
    latestVersion = 0;
    preparing = false;
    expireEntries();
  }

  function view(nodeId = '', at = now()): StatisticsView | undefined {
    const entry = entries.get(nodeId);
    if (!entry || !hasData(entry.view)) return entry?.view;
    const ageSeconds = entry.view.ageSeconds + Math.max(0, at - entry.receivedAt) / 1000;
    return { ...entry.view, ageSeconds, status: ageSeconds > 35 ? 'stale' : entry.view.status };
  }

  function nextReadIn(nodeId = '') {
    return Math.max(0, (entries.get(nodeId)?.nextRead ?? 0) - now());
  }

  function refresh(nodeId = '', force = false): Promise<void> {
    const pending = reads.get(nodeId);
    if (pending) return pending;
    if (!force && nextReadIn(nodeId) > 0) return Promise.resolve();

    const path = nodeId ? `/statistics/nodes?nodeId=${encodeURIComponent(nodeId)}` : '/dashboard/statistics';
    const startedIn = readEpoch;
    const save = (next: StatisticsView) => {
      const receivedAt = now();
      entries.set(nodeId, { view: next, receivedAt, nextRead: receivedAt + readInterval(next) });
    };
    const read = request<StatisticsView>(path).then(next => {
      if (startedIn !== readEpoch) return;
      const preparingNext = !hasData(next);
      const generationChanged = generation !== '' && next.generation !== generation;
      if (generationChanged) latestVersion = 0;
      if (generationChanged || (preparingNext && !preparing)) expireEntries();
      if (!preparingNext) {
        if (next.version < latestVersion) return;
        if (next.version > latestVersion) {
          latestVersion = next.version;
          // Different scopes project the same worker snapshot. Older data
          // may describe a different day, interface, or set of nodes.
          for (const [scope, entry] of entries) {
            if (entry.view.version < latestVersion) entries.delete(scope);
          }
        }
      }
      generation = next.generation;
      preparing = preparingNext;
      save(next);
    }, error => {
      if (startedIn !== readEpoch) return;
      const previous = view(nodeId);
      const mayKeepData = !(error instanceof APIError) || error.status === 0 || error.status >= 500;
      if (hasData(previous) && mayKeepData) {
        save({ ...previous, status: 'stale', error: (error as Error).message });
      } else {
        save({ status: 'error', generation, version: 0, error: (error as Error).message });
      }
    }).finally(() => { reads.delete(nodeId); });
    reads.set(nodeId, read);
    return read;
  }

  return { refresh, view, nextReadIn, clear };
}

export function statisticsNotice(view?: StatisticsView): string {
  if (!view || view.status === 'pending') return '流量统计准备中…';
  if (view.status === 'error') return `流量统计读取失败（${view.error}）`;
  if (view.status === 'stale') {
    const age = Math.floor(view.ageSeconds);
    const ageText = age >= 60 ? `${Math.floor(age / 60)} 分 ${age % 60} 秒` : `${age} 秒`;
    return `显示 ${ageText} 前的流量统计${view.error ? `（${view.error}）` : ''}`;
  }
  return '';
}
