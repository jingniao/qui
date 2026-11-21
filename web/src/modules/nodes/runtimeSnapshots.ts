import type { NodeView } from './api';

type RuntimeHeartbeat = Pick<NodeView['heartbeat'], 'cpu_percent' | 'memory_percent' | 'disk_percent' | 'current_revision' | 'active_connections' | 'active_connections_measured' | 'core_healthy' | 'cores'> & {
  host_uptime_seconds?: number;
  receive_bytes_per_second?: number;
  transmit_bytes_per_second?: number;
};

type RuntimeNode = { nodeId: string; revision: number } & (
  { removed: true } | (Pick<NodeView, 'status' | 'enabled' | 'maintenance' | 'deletedAt' | 'lastHeartbeat' | 'offlineAfterSeconds'> & {
    removed?: false;
    heartbeat: RuntimeHeartbeat;
  })
);

export interface RuntimeSnapshot { generation: string; inventoryRevision: number; node: RuntimeNode; }
export interface RuntimeSnapshotList { generation: string; inventoryRevision: number; revision: number; items: RuntimeNode[]; }

export class RuntimeFeed {
  generation = '';
  inventoryRevision = 0;
  private streamGeneration = '';
  private calibratedRevision = 0;
  private items = new Map<string, RuntimeNode>();
  private staged = new Map<string, RuntimeSnapshot>();

  get(id: string) { return this.items.get(id); }

  project(base: NodeView): NodeView | null {
    const node = this.items.get(base.id);
    if (this.generation && !node) return null;
    return applyRuntimeSnapshot(base, node);
  }

  matchesInventory(ids: string[], deleted: boolean): boolean {
    const expected = new Set<string>();
    for (const node of this.items.values()) {
      if (node.removed !== true && Boolean(node.deletedAt) === deleted) expected.add(node.nodeId);
    }
    return ids.length === expected.size && ids.every(id => expected.has(id));
  }

  resetStream() {
    this.streamGeneration = '';
    this.staged.clear();
  }

  receive(event: RuntimeSnapshot, live: boolean): RuntimeNode | undefined {
    if (this.streamGeneration !== event.generation) {
      this.staged.clear();
      this.streamGeneration = event.generation;
    }
    if (live && event.generation === this.generation) return this.applyEvent(event);
    const previous = this.staged.get(event.node.nodeId);
    if (!previous || event.node.revision > previous.node.revision) this.staged.set(event.node.nodeId, event);
  }

  calibrate(list: RuntimeSnapshotList): boolean {
    if (this.streamGeneration && this.streamGeneration !== list.generation) return false;
    if (this.generation !== list.generation) {
      this.generation = list.generation;
      this.items.clear();
      this.inventoryRevision = 0;
      this.calibratedRevision = 0;
    }
    if (list.revision >= this.calibratedRevision) {
      const included = new Set(list.items.map(node => node.nodeId));
      for (const node of list.items) this.apply(node);
      for (const [id, previous] of this.items) {
        if (!included.has(id) && !previous.removed && previous.revision <= list.revision) {
          this.items.set(id, { nodeId: id, revision: list.revision, removed: true });
        }
      }
      this.calibratedRevision = list.revision;
      this.inventoryRevision = Math.max(this.inventoryRevision, list.inventoryRevision);
    }
    for (const event of this.staged.values()) this.applyEvent(event);
    this.staged.clear();
    return true;
  }

  private applyEvent(event: RuntimeSnapshot) {
    this.inventoryRevision = Math.max(this.inventoryRevision, event.inventoryRevision);
    return this.apply(event.node);
  }

  private apply(node: RuntimeNode): RuntimeNode | undefined {
    const revision = Math.max(this.calibratedRevision, this.items.get(node.nodeId)?.revision ?? 0);
    if (node.revision <= revision) return;
    this.items.set(node.nodeId, node);
    return node;
  }
}

export function applyRuntimeSnapshot(base: NodeView, node?: RuntimeNode): NodeView | null {
  if (!node) return base;
  if (node.removed === true) return null;
  return { ...base, status: node.status, enabled: node.enabled, maintenance: node.maintenance,
    deletedAt: node.deletedAt, lastHeartbeat: node.lastHeartbeat, heartbeat: node.heartbeat, offlineAfterSeconds: node.offlineAfterSeconds };
}
