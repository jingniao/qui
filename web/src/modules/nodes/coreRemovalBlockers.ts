import { canvasAPI, choices, proxyAPI } from '../proxy/api';
import { request } from '../../app/apiClient';

export interface CoreRemovalBlockerGroup {
  label: string;
  items: { id: string; name: string; to?: string }[];
}

// The grouped ID fields are the only mapping to adjust if the backend contract changes.
const blockerTypes = [
  { field: 'inboundIds', label: '入站', read: (nodeId: string) => choices((p, s) => proxyAPI.inbounds(nodeId, p, s)), to: (nodeId: string, _id: string) => `/nodes/${encodeURIComponent(nodeId)}?tab=nodes` },
  { field: 'outboundIds', label: '出站', read: (nodeId: string) => choices((p, s) => proxyAPI.outbounds(nodeId, p, s)), to: (nodeId: string, _id: string) => `/nodes/${encodeURIComponent(nodeId)}?tab=nodes` },
  { field: 'outboundGroupIds', label: '出口组', read: (nodeId: string) => choices((p, s) => proxyAPI.groups(nodeId, p, s)), to: (nodeId: string, _id: string) => `/nodes/${encodeURIComponent(nodeId)}?tab=nodes` },
  { field: 'proxyChainIds', label: '代理链', read: (nodeId: string) => choices((p, s) => proxyAPI.chains(nodeId, p, s)), to: undefined },
  { field: 'relayIds', label: '中转线路', read: async (_nodeId: string) => (await request<{ items: { id: string; name: string }[] }>('/canvas-tunnels')).items, to: (_nodeId: string, _id: string) => '/proxy/relay' },
  { field: 'schemeIds', label: '画布方案', read: (_nodeId: string) => choices((p, s) => canvasAPI.list(p, s)), to: (_nodeId: string, id: string) => `/proxy/relay?schemeId=${encodeURIComponent(id)}` }
] as const;

export function coreRemovalBlockers(details: Record<string, unknown>, nodeId: string): CoreRemovalBlockerGroup[] {
  return blockerTypes.flatMap(type => {
    const ids = details[type.field];
    if (!Array.isArray(ids) || !ids.length) return [];
    return [{ label: type.label, items: ids.map((id: string) => ({ id, name: id, to: type.to?.(nodeId, id) })) }];
  });
}

export async function resolveCoreRemovalBlockerNames(groups: CoreRemovalBlockerGroup[], nodeId: string): Promise<string[]> {
  const results = await Promise.allSettled(groups.map(async group => {
    const type = blockerTypes.find(type => type.label === group.label)!;
    const objects = await type.read(nodeId);
    for (const item of group.items) item.name = objects.find(object => object.id === item.id)?.name || item.id;
  }));
  return results.flatMap(result => result.status === 'rejected' ? [(result.reason as Error).message] : []);
}
