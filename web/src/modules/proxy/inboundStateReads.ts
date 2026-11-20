import type { InboundStateItem } from '../system/deploymentApi';

type NodeState = { nodeId: string; items: InboundStateItem[]; error?: string };

// A change in visibility can share a read for the current data. A configuration
// change invalidates all coverage and any response already in flight.
export function createInboundStateReads(
  read: (nodeIds: string[]) => Promise<{ items: NodeState[] }>,
  apply: (items: NodeState[]) => void,
  canRead: () => boolean,
) {
  let visibleIds: string[] = [];
  const attempted = new Set<string>();
  let generation = 0, requestedGeneration = -1;
  let reading = false, disposed = false;

  async function drain() {
    if (disposed || reading || requestedGeneration !== generation || !canRead()) return;
    // Match the inbound status endpoint's per-request limit.
    const ids = visibleIds.filter(id => !attempted.has(id)).slice(0, 200);
    if (!ids.length) return;
    reading = true;
    const started = generation;
    try {
      let items: NodeState[];
      try {
        items = (await read(ids)).items;
      } catch (error) {
        items = ids.map(nodeId => ({ nodeId, items: [], error: (error as Error).message }));
      }
      if (!disposed && started === generation) {
        // Failures stay visible until the next explicit retry. A changing set
        // of visible nodes must not turn either kind of failure into a loop.
        for (const id of ids) attempted.add(id);
        apply(items);
      }
    } finally {
      reading = false;
      void drain();
    }
  }

  return {
    load(ids: string[], retryIds: readonly string[] = []) {
      visibleIds = ids;
      requestedGeneration = generation;
      for (const id of retryIds) attempted.delete(id);
      return drain();
    },
    invalidate() {
      generation++;
      attempted.clear();
    },
    dispose() { disposed = true; },
  };
}
