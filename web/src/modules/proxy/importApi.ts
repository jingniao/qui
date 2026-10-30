import { request } from '../../app/apiClient';
import { guessProtocolNetworks, proxyNodeStore, type ImportSourceKind, type NodeSourceType, type SubscriptionItem } from './proxyNodes';

// `kind` is read-only and derived by the panel from how the source was stored:
// "url" is a fetched HTTPS subscription (机场订阅), "content" is content pasted
// into the panel once (the node list's 导入节点 action). Nothing else tells the
// two apart, and the same field is what separates the two library groups.
export interface ImportSource { id: string; name: string; kind?: ImportSourceKind; intervalSeconds: number; lastFetchAt: string; lastStatus: string; lastError: string; userIds: string[] }
export interface ImportedProxy { id: string; sourceId: string; proxyKey: string; name: string; protocol: string; server: string; port: number }
export interface SourceInput { name: string; intervalSeconds: number; userIds: string[]; url?: string; content?: string }
export const importAPI = {
  proxies: () => request<{ items: ImportedProxy[] }>('/imported-proxies'),
  list: () => request<{ items: ImportSource[] }>('/subscription-sources'),
  create: (input: SourceInput) => request<{id:string}>('/subscription-sources', 'POST', input),
  update: (id: string, input: SourceInput) => request(`/subscription-sources/${encodeURIComponent(id)}`, 'PUT', input),
  remove: (id: string) => request(`/subscription-sources/${encodeURIComponent(id)}`, 'DELETE'),
  refresh: (id: string) => request(`/subscription-sources/${encodeURIComponent(id)}/refresh`, 'POST', undefined, {timeoutMs:20000}),
};

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
  proxyNodeStore.subscriptions = sources.items.map(s => ({ id:s.id, name:s.name, kind:importSourceKind(s), url:'已加密保存', nodeCount:proxies.items.filter(p => p.sourceId===s.id).length, autoUpdate:true, lastUpdated:s.lastFetchAt || '未拉取', usedTraffic:'—', totalTraffic:'—', userIds:s.userIds, intervalSeconds:s.intervalSeconds, lastStatus:s.lastStatus, lastError:s.lastError } satisfies SubscriptionItem));
  proxyNodeStore.manualNodes = proxies.items.map(p => ({id:p.id, sourceId:p.sourceId, proxyKey:p.proxyKey, name:p.name, protocol:p.protocol, sourceType:proxySourceType(kindOf.get(p.sourceId) || 'url'), sourceName:sources.items.find(s=>s.id===p.sourceId)?.name || '导入来源', server:p.server, port:p.port, status:'untested', ...guessProtocolNetworks(p.protocol), createdAt:''}));
}
