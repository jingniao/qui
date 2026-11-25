import type { NodeView, SourceAddressIssue } from './api';

// Display statuses whose last heartbeat is current enough to trust its source check.
const live = new Set(['online', 'degraded', 'core_error', 'error', 'applying', 'rollback']);

export interface SourceAddressAlert { failed: boolean; issues: SourceAddressIssue[]; }

/**
 * The Agent's report of configured outbound sources missing on the host, only
 * while the node is connected (statusKey from serverStatus). Null when there is
 * nothing to show, including Agents that do not report the check.
 */
export function sourceAddressAlert(node: NodeView, statusKey: string): SourceAddressAlert | null {
  const status = node.heartbeat?.source_address_status;
  if (!status || !live.has(statusKey)) return null;
  if (status.error) return { failed: true, issues: [] };
  return status.missing?.length ? { failed: false, issues: status.missing } : null;
}

export function describeSourceIssue(issue: SourceAddressIssue): string {
  const where = [issue.address, issue.interface ? `网卡 ${issue.interface}` : ''].filter(Boolean).join(' / ');
  switch (issue.kind) {
    case 'host_source': return `服务器强制出站 ${issue.address?.includes(':') ? 'IPv6' : 'IPv4'} ${where}`;
    case 'core_source': return `核心出站源 ${where}`;
    case 'inbound_egress': return `代理节点出口 IP ${where}`;
    case 'outbound_source': return `出站源绑定 ${where}`;
  }
}
