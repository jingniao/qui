// Per-core view of a node (K3b). The backend stores the whole `Capabilities`
// message, including `cores[]`, and the heartbeat's per-core health. An Agent
// that never negotiated multi-core support reports no core list at all, so it is
// modelled as exactly one core built from the scalar fields. Nothing here invents
// a value: an absent version is "—", an absent health observation stays unknown
// (null) instead of "healthy".
import type { CoreCapability, CoreName, NodeView } from './api';

export type NodeCoreProtocol = { protocol: string; inbound?: boolean; outbound?: boolean; tcp?: boolean; udp?: boolean };

export interface NodeCore {
  name: CoreName;
  label: string;
  version: string;
  // null means "the Agent reported no health for this core"; it is never false.
  healthy: boolean | null;
  error: string;
  protocols: NodeCoreProtocol[];
  singboxUserStats: boolean | null;
  forwardInboundSupported: boolean | null;
  deviceGateSupported: boolean | null;
  primary: boolean;
}

export const CORE_NAMES: CoreName[] = ['xray', 'sing-box'];

export function isCoreName(value: unknown): value is CoreName {
  return value === 'xray' || value === 'sing-box';
}

export function coreLabel(name: CoreName | string | undefined): string {
  if (name === 'xray') return 'Xray';
  if (name === 'sing-box') return 'sing-box';
  return name || '—';
}

function protocolsOf(core?: CoreCapability | null, fallback?: NodeCoreProtocol[]): NodeCoreProtocol[] {
  const list = core?.protocols?.length ? core.protocols : fallback;
  return (list || []).filter(item => item && typeof item.protocol === 'string');
}

// capabilities.cores is authoritative. The scalar core_name/core_version pair is
// only used for an Agent that reports no core list (legacy one-core node).
export function nodeCores(node?: NodeView | null): NodeCore[] {
  if (!node) return [];
  const caps = node.capabilities || {};
  const statuses = Array.isArray(node.heartbeat?.cores) ? node.heartbeat!.cores! : [];
  const statusOf = (name: string) => statuses.find(item => item && item.name === name);
  const listed = (Array.isArray(caps.cores) ? caps.cores : []).filter(core => core && isCoreName(core.name)) as CoreCapability[];
  const primaryName = isCoreName(caps.core_name) ? caps.core_name : listed[0]?.name;
  if (!listed.length) {
    if (!primaryName) return [];
    const status = statusOf(primaryName);
    const healthy = status ? (typeof status.healthy === 'boolean' ? status.healthy : null)
      : typeof node.heartbeat?.core_healthy === 'boolean' ? node.heartbeat.core_healthy : null;
    return [{
      name: primaryName, label: coreLabel(primaryName), version: caps.core_version || '—', healthy,
      error: status?.error || '', protocols: protocolsOf(null, caps.protocols),
      singboxUserStats: typeof caps.singbox_user_stats === 'boolean' ? caps.singbox_user_stats : null,
      forwardInboundSupported: null, deviceGateSupported: null, primary: true
    }];
  }
  return listed.map(core => {
    const status = statusOf(core.name);
    return {
      name: core.name, label: coreLabel(core.name), version: core.version || '—',
      healthy: status ? (typeof status.healthy === 'boolean' ? status.healthy : null)
        : (typeof core.healthy === 'boolean' ? core.healthy : null),
      error: status?.error || '', protocols: protocolsOf(core),
      singboxUserStats: typeof core.singbox_user_stats === 'boolean' ? core.singbox_user_stats : null,
      forwardInboundSupported: typeof core.forward_inbound_supported === 'boolean' ? core.forward_inbound_supported : null,
      deviceGateSupported: typeof core.device_gate_supported === 'boolean' ? core.device_gate_supported : null,
      primary: core.name === primaryName
    };
  });
}

export function reportedCoreNames(node?: NodeView | null): CoreName[] {
  return nodeCores(node).map(core => core.name);
}

export function primaryCoreName(node?: NodeView | null): CoreName | '' {
  return nodeCores(node).find(core => core.primary)?.name || '';
}

export function findCore(node: NodeView | undefined | null, name: CoreName | ''): NodeCore | undefined {
  const cores = nodeCores(node);
  if (!name) return cores.find(core => core.primary) || cores[0];
  return cores.find(core => core.name === name);
}

export function multiCoreSupported(node?: NodeView | null): boolean {
  return node?.capabilities?.multi_core_supported === true;
}

// Cores whose add/remove task has not been confirmed by a capability report.
export function blockedCores(node?: NodeView | null): CoreName[] {
  return (node?.coreSelectionBlocked || []).filter(isCoreName);
}

// "Xray 26.3.27 · sing-box 1.14.4"; "—" when the Agent reported nothing.
export function coreSummaryLabel(node?: NodeView | null): string {
  const cores = nodeCores(node);
  if (!cores.length) return '—';
  return cores.map(core => `${core.label} ${core.version}`).join(' · ');
}

export function unhealthyCores(node?: NodeView | null): NodeCore[] {
  return nodeCores(node).filter(core => core.healthy === false);
}

export function unhealthyCoreLabel(node?: NodeView | null): string {
  return unhealthyCores(node).map(core => core.label).join('、');
}

// Union of the inbound protocols the node's reported cores advertise.
export function coreProtocols(node?: NodeView | null): string[] {
  const set = new Set<string>();
  for (const core of nodeCores(node)) for (const item of core.protocols) if (item.inbound || item.outbound) set.add(item.protocol);
  return [...set].sort();
}

// Core names shown in an inbound editor: the reported cores, or nothing when the
// node has not reported capabilities yet.
export function selectableCores(node?: NodeView | null): NodeCore[] {
  return nodeCores(node);
}
