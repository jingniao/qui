import type { Counts } from './api';

export interface LiveSummary { connections: string; onlineUsers: string; blocked: string; partial: boolean }

// The panel fills onlineUsers/activeConnections only when every enabled node
// reported; a partial measurement is never shown as a number.
export function liveSummary(counts: Counts): LiveSummary {
  return {
    connections: counts.activeConnections === null ? '—' : String(counts.activeConnections),
    onlineUsers: counts.onlineUsers === null ? '—' : String(counts.onlineUsers),
    blocked: String(counts.todayBlocked),
    partial: counts.measuredNodes < counts.expectedNodes,
  };
}
