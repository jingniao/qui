import { request } from '../../app/apiClient';
import { guessProtocolNetworks, proxyNodeStore, type ImportResult, type ImportSourceKind, type NodeSourceType, type SubscriptionItem } from './proxyNodes';

export type { ImportDiagnostic, ImportResult } from './proxyNodes';

// `kind` is read-only and derived by the panel from how the source was stored:
// "url" is a fetched HTTPS subscription (机场订阅), "content" is content pasted
// into the panel once (the node list's 导入节点 action). Nothing else tells the
// two apart, and the same field is what separates the two library groups.
// `latestResult` is the bounded result of the last attempt; `lastSuccessAt` stays
// empty until an attempt commits endpoints.
export interface ImportSource { id: string; name: string; kind?: ImportSourceKind; intervalSeconds: number; lastFetchAt: string; lastSuccessAt: string; lastStatus: string; lastError: string; latestResult?: ImportResult | null; userIds: string[] }
export interface ImportedProxy { id: string; sourceId: string; proxyKey: string; name: string; protocol: string; server: string; port: number }
export interface SourceInput { name: string; intervalSeconds: number; userIds: string[]; url?: string; content?: string }
export const importAPI = {
  proxies: () => request<{ items: ImportedProxy[] }>('/imported-proxies'),
  list: () => request<{ items: ImportSource[] }>('/subscription-sources'),
  // The ID is the primary field. A pasted import is analysed by the same pipeline
  // as a fetch, so its bounded result comes back with the saved source; a source
  // saved without content (a link, or a rename) has no result.
  create: (input: SourceInput) => request<{id:string; result?: ImportResult}>('/subscription-sources', 'POST', input),
  update: (id: string, input: SourceInput) => request<{id:string; result?: ImportResult}>(`/subscription-sources/${encodeURIComponent(id)}`, 'PUT', input),
  remove: (id: string) => request(`/subscription-sources/${encodeURIComponent(id)}`, 'DELETE'),
  // A refresh answers with the attempt's result; a failed attempt answers with an
  // error whose details carry the same result.
  refresh: (id: string) => request<ImportResult>(`/subscription-sources/${encodeURIComponent(id)}/refresh`, 'POST', undefined, {timeoutMs:20000}),
};

// The result of a failed save or refresh, taken from APIError.details so the page
// shows the panel's counts and reasons instead of only its message.
export function failedImportResult(error: unknown): ImportResult | null {
  const result = (error as { details?: { result?: ImportResult } } | undefined)?.details?.result;
  return result && result.attemptedAt ? result : null;
}

// Business-outcome text for one attempt. The failure headline is the panel's own
// fixed message, which names a real upstream status and states whether the source
// state changed, so a storage failure cannot read as "nothing is usable". Counts
// always describe the attempt.
export function describeImportResult(result: ImportResult): string {
  const counts = result.counts;
  const reasons = result.diagnostics.length ? '；可查看原因' : '';
  if (result.outcome === 'ok') return `已更新：导入 ${counts.accepted} 个节点`;
  if (result.outcome === 'partial') {
    return `导入 ${counts.accepted} 个，跳过 ${counts.rejected ?? '—'} 个，重复 ${counts.duplicates ?? '—'} 个${reasons}`;
  }
  return `${result.message}；${importCountsLine(result)}${reasons}`;
}

// A source keeps its rows after a failure, so the card must say what is cached and
// what a subscription can actually use. An undetermined count is never shown as 0.
export function importCountsLine(result: ImportResult): string {
  const parts = [`本次导入 ${result.counts.accepted}`];
  if (result.counts.rejected !== null) parts.push(`跳过 ${result.counts.rejected}`);
  if (result.counts.duplicates !== null) parts.push(`重复 ${result.counts.duplicates}`);
  parts.push(`缓存 ${result.counts.cached ?? '—'}`);
  parts.push(`可用于订阅 ${result.counts.usable ?? '—'}`);
  return parts.join(' · ');
}

// A source an earlier panel version cannot label is treated as a URL source,
// which is what it was before content imports existed.
export function importSourceKind(source: { kind?: string } | undefined): ImportSourceKind {
  return source?.kind === 'content' ? 'content' : 'url';
}

// Content imports are single nodes the Owner pasted in, never an airport
// subscription, so their proxies belong to the 单独导入 library group.
export function proxySourceType(kind: ImportSourceKind): NodeSourceType {
  return kind === 'content' ? 'manual' : 'subscription';
}

// The header count and the subscription manager only talk about airport sources.
export function airportSources<T extends { kind?: string }>(sources: readonly T[]): T[] {
  return sources.filter(source => importSourceKind(source) === 'url');
}

export async function loadImports() {
  const [sources, proxies] = await Promise.all([importAPI.list(), importAPI.proxies()]);
  const kindOf = new Map(sources.items.map(source => [source.id, importSourceKind(source)]));
  proxyNodeStore.subscriptions = sources.items.map(s => ({ id:s.id, name:s.name, kind:importSourceKind(s), url:'已加密保存', nodeCount:proxies.items.filter(p => p.sourceId===s.id).length, autoUpdate:true, lastUpdated:s.lastFetchAt || '未拉取', usedTraffic:'—', totalTraffic:'—', userIds:s.userIds, intervalSeconds:s.intervalSeconds, lastStatus:s.lastStatus, lastError:s.lastError, lastFetchAt:s.lastFetchAt, lastSuccessAt:s.lastSuccessAt, latestResult:s.latestResult ?? null } satisfies SubscriptionItem));
  proxyNodeStore.manualNodes = proxies.items.map(p => ({id:p.id, sourceId:p.sourceId, proxyKey:p.proxyKey, name:p.name, protocol:p.protocol, sourceType:proxySourceType(kindOf.get(p.sourceId) || 'url'), sourceName:sources.items.find(s=>s.id===p.sourceId)?.name || '导入来源', server:p.server, port:p.port, status:'untested', ...guessProtocolNetworks(p.protocol), createdAt:''}));
}
