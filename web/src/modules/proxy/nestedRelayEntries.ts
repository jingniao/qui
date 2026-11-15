import type { Inbound } from './api';
import type { NodeRelayRoute, ProxyNodeItem } from './proxyNodes';

// Entries a node's 添加中转 created are shown only in their exit node's 中转
// section of the node list: a relay scheme's newly created entry inbound, or the
// entry of a tunnel rule linked to a node. 节点分组 builds its own list and still
// offers them as separate nodes for granting.
export interface RelayNesting {
  hidden: Set<string>;                      // ProxyNodeItem ids folded into an exit node
  entries: Map<string, ProxyNodeItem[]>;    // scheme id -> its created entries
}

export function nestRelayEntries(
  selfHosted: ProxyNodeItem[], manual: ProxyNodeItem[], inbounds: Inbound[],
  routesOf: (node: ProxyNodeItem) => NodeRelayRoute[], isLinkedRule: (ruleId: string) => boolean,
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
  // Only schemes that really appear under another listed node are folded in, so an
  // entry whose drawing no longer reaches a listed exit keeps its own row.
  const listed = new Set<string>();
  for (const node of [...selfHosted, ...manual]) {
    if (!entryIds.has(node.id)) for (const route of routesOf(node)) listed.add(route.schemeId);
  }
  const hidden = new Set<string>();
  for (const [scheme, nodes] of entries) {
    if (listed.has(scheme)) nodes.forEach(node => hidden.add(node.id));
    else entries.delete(scheme);
  }
  for (const node of selfHosted) {
    const owner = inboundOf(node)?.managedBy;
    if (owner?.kind === 'rule' && isLinkedRule(owner.id)) hidden.add(node.id);
  }
  return { hidden, entries };
}
