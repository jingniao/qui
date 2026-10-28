import { request } from '../../app/apiClient';
import { proxyNodeStore, type SubscriptionItem } from './proxyNodes';

export interface ImportSource { id: string; name: string; intervalSeconds: number; lastFetchAt: string; lastStatus: string; lastError: string; userIds: string[] }
export interface SourceInput { name: string; intervalSeconds: number; userIds: string[]; url?: string; content?: string }
export const importAPI = {
  list: () => request<{ items: ImportSource[] }>('/subscription-sources'),
  create: (input: SourceInput) => request<{id:string}>('/subscription-sources', 'POST', input),
  update: (id: string, input: SourceInput) => request(`/subscription-sources/${encodeURIComponent(id)}`, 'PUT', input),
  remove: (id: string) => request(`/subscription-sources/${encodeURIComponent(id)}`, 'DELETE'),
  refresh: (id: string) => request(`/subscription-sources/${encodeURIComponent(id)}/refresh`, 'POST', undefined, {timeoutMs:20000}),
};

export async function loadImports() {
  const [sources, proxies] = await Promise.all([importAPI.list(), request<{items: {id:string;sourceId:string;name:string;protocol:string;server:string;port:number}[]}>('/imported-proxies')]);
  proxyNodeStore.subscriptions = sources.items.map(s => ({ id:s.id, name:s.name, url:'已加密保存', nodeCount:proxies.items.filter(p => p.sourceId===s.id).length, autoUpdate:true, lastUpdated:s.lastFetchAt || '未拉取', usedTraffic:'—', totalTraffic:'—', userIds:s.userIds, intervalSeconds:s.intervalSeconds, lastStatus:s.lastStatus, lastError:s.lastError } satisfies SubscriptionItem));
  proxyNodeStore.manualNodes = proxies.items.map(p => ({id:p.id, sourceId:p.sourceId, name:p.name, protocol:p.protocol, sourceType:'subscription', sourceName:sources.items.find(s=>s.id===p.sourceId)?.name || '导入来源', server:p.server, port:p.port, status:'untested', supportTcp:false, supportUdp:false, createdAt:''}));
}
