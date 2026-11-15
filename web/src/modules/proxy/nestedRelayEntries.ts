import type { Inbound } from './api';
import type { TunnelRule } from '../tunnels/api';
import type { NodeView } from '../nodes/api';
import type { NodeRelayRoute, ProxyNodeItem } from './proxyNodes';

export function ruleEntryAddress(rule: TunnelRule, selfHosted: ProxyNodeItem[], inbounds: Inbound[], servers: NodeView[]) {
  const inbound = inbounds.find(item => item.managedBy?.kind === 'rule' && item.managedBy.id === rule.id);
  const entry = inbound && selfHosted.find(item => item.inboundId === inbound.id);
  if (entry) return { server: entry.server, port: entry.port };
  const server = servers.find(item => item.id === rule.entryNodeId)?.publicIp;
  return server ? { server, port: rule.listenPort } : undefined;
}

// Entries a node's 添加中转 created are shown only in their exit node's 中转
// section of the node list: a relay scheme's newly created entry inbound, or the
// entry of a tunnel rule linked to a node. 节点分组 builds its own list and still
// offers them as separate nodes for granting.
export interface RelayNesting {
  hidden: Set<string>;                      // ProxyNodeItem ids folded into an exit node
  entries: Map<string, ProxyNodeItem[]>;    // scheme id -> its created entries
  routes: Map<string, NodeRelayRoute[]>;    // node id -> routes assigned in display order
}

export function nestRelayEntries(
  selfHosted: ProxyNodeItem[], manual: ProxyNodeItem[], inbounds: Inbound[],
  routesOf: (node: ProxyNodeItem, candidates: ProxyNodeItem[]) => NodeRelayRoute[], isLinkedRule: (ruleId: string) => boolean,
): RelayNesting {
  const byId = new Map(inbounds.map(inbound => [inbound.id, inbound]));
  const inboundOf = (node: ProxyNodeItem) => node.inboundId ? byId.get(node.inboundId) : undefined;
  const entries = new Map<string, ProxyNodeItem[]>();
  for (const node of selfHosted) {
    const summary = inboundOf(node)?.routeSummary;
    if (summary?.type !== 'relay' || summary.bindingMode !== 'create' || !summary.schemeId) continue;
    entries.set(summary.schemeId, [...(entries.get(summary.schemeId) || []), node]);
  }
  const entryIds = new Set([...entries.values()].flat().map(node => node.id));
  const hidden = new Set<string>();
  for (const node of selfHosted) {
    const owner = inboundOf(node)?.managedBy;
    if (owner?.kind === 'tunnel' || (owner?.kind === 'rule' && isLinkedRule(owner.id))) hidden.add(node.id);
  }
  let candidates = selfHosted.filter(node => !hidden.has(node.id) && !entryIds.has(node.id));
  const initiallyListed = new Set([...candidates, ...manual].flatMap(node => routesOf(node, candidates).map(route => route.schemeId)));
  // Orphan entries keep their cards and may host another scheme's server exit.
  const foldedEntries = new Set([...entries].filter(([scheme]) => initiallyListed.has(scheme)).flatMap(([, nodes]) => nodes.map(node => node.id)));
  candidates = selfHosted.filter(node => !hidden.has(node.id) && !foldedEntries.has(node.id));
  const routes = new Map([...selfHosted, ...manual].map(node => [node.id, routesOf(node, candidates)]));
  // Only schemes that really appear under another listed node are folded in, so an
  // entry whose drawing no longer reaches a listed exit keeps its own row.
  const listed = new Set<string>();
  for (const node of [...selfHosted, ...manual]) {
    if (hidden.has(node.id) || foldedEntries.has(node.id)) continue;
    for (const route of routes.get(node.id)!) {
      if (!entries.get(route.schemeId)?.some(entry => entry.id === node.id)) listed.add(route.schemeId);
    }
  }
  for (const [scheme, nodes] of entries) {
    if (listed.has(scheme)) nodes.forEach(node => hidden.add(node.id));
    else entries.delete(scheme);
  }
  return { hidden, entries, routes };
}
