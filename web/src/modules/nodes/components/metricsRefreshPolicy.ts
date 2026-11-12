// Decisions for the machine-monitoring card, kept pure so they can be self-tested.
export interface MetricsKey { nodeId: string; hours: number }

export function metricsKeyChanged(prev: MetricsKey | null, next: MetricsKey): boolean {
  return !prev || prev.nodeId !== next.nodeId || prev.hours !== next.hours;
}

export interface MetricsState { points: unknown[]; loading: boolean; error: string }

// A foreground load (first read, node or range switch) clears data and shows the hint.
// A background refresh (timer) keeps the old data and never shows the hint.
export function beginMetricsLoad(state: MetricsState, foreground: boolean): MetricsState {
  return foreground ? { points: [], loading: true, error: '' } : { ...state, loading: false, error: '' };
}

export function failMetricsLoad(state: MetricsState, message: string): MetricsState {
  return { ...state, loading: false, error: message };
}
