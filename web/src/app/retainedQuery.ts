import { reactive, type Ref } from 'vue';
import { invalidatesRead } from './apiClient';

// A deletion may finish after navigation. Only adjust the visible query if it
// still contains that row, then read the last remaining page if necessary.
export function removePageItem<T extends { id: string }>(rows: Ref<T[]>, total: Ref<number>, page: Ref<number>, id: string, pageSize: number) {
  const index = rows.value.findIndex(row => row.id === id);
  if (index < 0) return;
  rows.value.splice(index, 1);
  total.value--;
  page.value = Math.min(page.value, Math.max(1, Math.ceil(total.value / pageSize)));
}

// The caller owns its request generation. Only the latest read confirms or
// rejects the result. A different query immediately releases the previous rows.
export function retainedQuery(clearRows: () => void) {
  let key = '';
  const state = reactive({
    ready: false,
    begin(...parts: unknown[]) {
      const next = JSON.stringify(parts);
      if (key === next) return;
      key = next;
      state.ready = false;
      clearRows();
    },
    reject(error: unknown) {
      if (invalidatesRead(error)) { state.ready = false; clearRows(); }
    }
  });
  return state;
}
