import { onUnmounted, reactive, type ObjectDirective } from 'vue';
import { request } from '../../../app/apiClient';

interface Point { at: string; medianMs?: number | null; avgMs?: number | null; }
interface CardLatency {
  targets: { id: string; label: string }[];
  series: { targetId: string; points: Point[] }[];
}

export function useServerCardLatency() {
  const results = reactive<Record<string, CardLatency>>({});
  const requested = new Set<string>();
  const observers = new Map<HTMLElement, IntersectionObserver>();
  let disposed = false;
  async function load(id: string) {
    if (requested.has(id)) return;
    requested.add(id);
    try {
      const result = await request<CardLatency>(`/nodes/${encodeURIComponent(id)}/latency?hours=1`);
      if (!disposed) results[id] = result;
    } catch { /* Missing endpoint and unavailable samples keep the original placeholders. */ }
  }
  const vVisible: ObjectDirective<HTMLElement, string> = {
    mounted(el, binding) {
      if (typeof IntersectionObserver === 'undefined') { void load(binding.value); return; }
      const observer = new IntersectionObserver(entries => {
        if (!entries.some(entry => entry.isIntersecting)) return;
        observer.disconnect(); observers.delete(el);
        void load(binding.value);
      });
      observers.set(el, observer); observer.observe(el);
    },
    unmounted(el) { observers.get(el)?.disconnect(); observers.delete(el); }
  };
  function latencyTile(id: string, index: number) {
    const data = results[id], target = data?.targets?.[index];
    const fallback = { label: `测试点 ${index + 1}`, value: '—' };
    if (!target) return fallback;
    const points = data?.series?.find(series => series.targetId === target.id)?.points || [];
    const latest = points.reduce<Point | undefined>((last, point) =>
      !last || Date.parse(point.at) > Date.parse(last.at) ? point : last, undefined);
    // A null median is a measured gap; only use avgMs when medianMs is absent.
    const ms = latest?.medianMs !== undefined ? latest.medianMs : latest?.avgMs;
    return { label: target.label || fallback.label,
      value: typeof ms === 'number' && Number.isFinite(ms) && ms >= 0 ? `${Math.round(ms)} ms` : '—' };
  }
  function latencyStatus(id: string) {
    return [0, 1, 2].some(index => latencyTile(id, index).value !== '—') ? '' : '未配置';
  }
  onUnmounted(() => { disposed = true; observers.forEach(observer => observer.disconnect()); observers.clear(); });
  return { vVisible, latencyTile, latencyStatus };
}
