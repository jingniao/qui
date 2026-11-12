// Helpers for the `proxy-node-status` window event. The event detail carries the
// node ID from the SSE message; an empty detail means "any/all nodes".
export function nodeStatusDetail(nodeId: unknown): { nodeId: string } {
  return { nodeId: typeof nodeId === 'string' ? nodeId : '' };
}

export function parseNodeStatusMessage(data: unknown): { nodeId: string } {
  if (typeof data !== 'string' || !data) return nodeStatusDetail('');
  try {
    const parsed = JSON.parse(data) as { nodeId?: unknown } | null;
    return nodeStatusDetail(parsed?.nodeId);
  } catch { return nodeStatusDetail(''); }
}

export function nodeStatusEventNodeId(event: Event | null | undefined): string {
  const detail = (event as CustomEvent<{ nodeId?: unknown } | null> | null | undefined)?.detail;
  return typeof detail?.nodeId === 'string' ? detail.nodeId : '';
}

// True when the event concerns the node (or is not node-specific).
export function nodeStatusMatches(event: Event | null | undefined, nodeId: string): boolean {
  const id = nodeStatusEventNodeId(event);
  return id === '' || id === nodeId;
}

// Leading + one trailing call: runs immediately, then at most once per interval.
export function throttleTrailing(fn: () => void, intervalMs: number, now: () => number = Date.now) {
  let last = -Infinity;
  let timer: ReturnType<typeof setTimeout> | null = null;
  const run = () => { timer = null; last = now(); fn(); };
  const throttled = () => {
    const wait = last + intervalMs - now();
    if (wait <= 0) { if (timer) { clearTimeout(timer); timer = null; } run(); return; }
    if (!timer) timer = setTimeout(run, wait);
  };
  throttled.cancel = () => { if (timer) { clearTimeout(timer); timer = null; } };
  return throttled;
}
