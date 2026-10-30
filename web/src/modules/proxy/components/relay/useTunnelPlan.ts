import { onBeforeUnmount, ref, watch, type WatchSource } from 'vue';
import type { TunnelPlan } from './tunnelEditorModel';

// Invalidate immediately on an edit, so an older request can never label a new layout.
export function useTunnelPlan(key: WatchSource<string>, request: () => Promise<TunnelPlan> | null) {
  const plan = ref<TunnelPlan | null>(null), checking = ref(false), error = ref('');
  let generation = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  async function run() {
    clearTimeout(timer);
    const token = ++generation;
    plan.value = null;
    error.value = '';
    checking.value = true;
    try {
      const response = await request();
      if (token === generation) plan.value = response;
      return token === generation ? response : null;
    } catch (e) {
      if (token === generation) error.value = `检查失败：${(e as Error).message}`;
      return null;
    } finally {
      if (token === generation) checking.value = false;
    }
  }
  watch(key, () => {
    ++generation;
    clearTimeout(timer);
    plan.value = null;
    error.value = '';
    checking.value = true;
    timer = setTimeout(run, 600);
  }, { immediate: true, flush: 'sync' });
  onBeforeUnmount(() => { ++generation; clearTimeout(timer); });
  return { plan, checking, error, run };
}
