import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { authState } from './auth';
import { createStatisticsCache, statisticsNotice } from './statisticsCache';

const statistics = createStatisticsCache();
watch([() => authState.currentUser?.id, () => authState.timezone], statistics.clear, { flush: 'sync' });
export const invalidateStatistics = statistics.clear;

export function useStatistics(nodeId: () => string | undefined) {
  const scope = computed(() => authState.isAuthenticated ? nodeId() : undefined);
  const displayedAt = ref(performance.now());
  const view = computed(() => scope.value === undefined ? undefined : statistics.view(scope.value, displayedAt.value));
  const data = computed(() => view.value?.data);
  const notice = computed(() => scope.value === undefined ? '' : statisticsNotice(view.value));
  let timer: ReturnType<typeof setTimeout> | undefined;
  let clock: ReturnType<typeof setInterval> | undefined;
  let ticket = 0;
  let mounted = false;

  async function refresh(force = false) {
    clearTimeout(timer);
    const current = ++ticket;
    const id = scope.value;
    if (!mounted || id === undefined || document.hidden) return;
    displayedAt.value = performance.now();
    await statistics.refresh(id, force);
    if (current === ticket && scope.value === id && !document.hidden) {
      timer = setTimeout(() => { void refresh(); }, statistics.nextReadIn(id));
    }
  }

  function visibilityChanged() { void refresh(!document.hidden); }
  watch([scope, () => authState.timezone], () => { void refresh(); });
  watch(() => authState.sseStatus, status => { if (status === 'connected') void refresh(true); });
  onMounted(() => {
    mounted = true;
    document.addEventListener('visibilitychange', visibilityChanged);
    clock = setInterval(() => {
      if (scope.value !== undefined && !document.hidden) displayedAt.value = performance.now();
    }, 1000);
    void refresh();
  });
  onUnmounted(() => {
    mounted = false;
    ticket++;
    clearTimeout(timer);
    clearInterval(clock);
    document.removeEventListener('visibilitychange', visibilityChanged);
  });
  return { data, notice, refresh };
}
