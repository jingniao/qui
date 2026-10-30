// Canvas resource library: "我的自建节点" is the panel's own inbounds.
//
// The canvas never keeps a browser copy of an inbound (unlike the imported
// proxies, which arrive through `loadImports`), so this group has to be derived
// from the same `servers` + `inbounds` lists the page loads for reference
// resolution. An item's id is the exact value `canvasInstanceRef` recognises
// (`self-<nodeId>-<inboundId>`), which is what turns a dropped box back into an
// `inbound:<inboundId>` reference when the scheme is saved — the same shape the
// node list's "添加中转" draft uses for a self-hosted target.
import type { Inbound } from '../../api';
import type { ProxyNodeItem } from '../../proxyNodes';
import { selfHostedResourceId } from '../../api';

// Only the fields the library reads; NodeView satisfies this.
export interface CanvasLibraryServerRef {
  id: string;
  name: string;
  publicIp?: string;
  hostname?: string;
  status?: string;
  enabled?: boolean;
  deletedAt?: string;
}

export interface SelfHostedLibraryItem {
  // `self-<nodeId>-<inboundId>`; canvasInstanceRef maps it to `inbound:<id>`.
  id: string;
  inboundId: string;
  serverId: string;
  name: string;
  server: string;
  port: number;
  protocol: string;
  status: ProxyNodeItem['status'];
  // A switched-off inbound may still be referenced by a saved scheme, but it
  // carries no traffic: the library shows it and refuses the drop.
  enabled: boolean;
  internalRelay: boolean;
  searchText: string;
}

// Mirrors the naming and address rules of ProxyNodeListPage.selfHostedNodes so
// the same inbound is called the same thing on both pages.
export function selfHostedLibraryItems(
  servers: readonly CanvasLibraryServerRef[],
  inbounds: readonly Inbound[],
  search = ''
): SelfHostedLibraryItem[] {
  const query = search.trim().toLowerCase();
  const items: SelfHostedLibraryItem[] = [];
  for (const server of servers) {
    if (!server.enabled || server.deletedAt) continue;
    for (const inbound of inbounds) {
      if (inbound.nodeId !== server.id) continue;
      let label = (inbound.name || '').trim();
      if (server.name && label.startsWith(server.name)) {
        label = label.slice(server.name.length).replace(/^[·\s\-_]+/, '').trim();
      }
      const base = label
        ? `${server.name} · ${label}`
        : `${server.name} · ${(inbound.protocol || '').toUpperCase()}`;
      const name = inbound.internalRelay
        ? `${base} (内部中转)`
        : inbound.enabled ? base : `${base} (已停用)`;
      const address = server.publicIp || server.hostname || '127.0.0.1';
      const live = (server.status === 'online' || server.status === 'degraded') && inbound.enabled;
      const item: SelfHostedLibraryItem = {
        id: selfHostedResourceId(inbound),
        inboundId: inbound.id,
        serverId: server.id,
        name,
        server: address,
        port: inbound.port,
        protocol: inbound.protocol,
        status: live ? 'online' : 'offline',
        enabled: inbound.enabled,
        internalRelay: inbound.internalRelay,
        searchText: [name, address, String(inbound.port), inbound.protocol]
          .join(' ')
          .toLowerCase()
      };
      if (query && !item.searchText.includes(query)) continue;
      items.push(item);
    }
  }
  return items;
}
