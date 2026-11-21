import { onMounted, onUnmounted, ref } from 'vue';
import { request } from '../../app/apiClient';
import { authState } from '../../app/auth';
import type { NodeView } from './api';
import { RuntimeFeed, type RuntimeSnapshot, type RuntimeSnapshotList } from './runtimeSnapshots';

// Only display state uses this clock. Measurements and operation admission must
// keep their own real clock and their existing 45-second freshness requirement.
export function useNodeRuntime(options: { onInventoryChanged?: () => void } = {}) {
  const feed = new RuntimeFeed();
  const revision = ref(0), statusNow = ref(Date.now()), ready = ref(false), error = ref('');
  let active = false, read = 0;
  let pending: Promise<void> | undefined;
  let poll: ReturnType<typeof setInterval>, clock: ReturnType<typeof setInterval>;

  function project(base: NodeView): NodeView | null {
    void revision.value;
    return feed.project(base);
  }

  function freeze() { read++; ready.value = false; }

  async function calibrate(): Promise<void> {
    if (!active || document.hidden) return;
    if (pending) return pending;
    const ticket = read;
    pending = (async () => {
      try {
        const snapshot = await request<RuntimeSnapshotList>('/nodes/runtime');
        if (!active || ticket !== read) return;
        const generation = feed.generation, inventory = feed.inventoryRevision;
        if (!feed.calibrate(snapshot)) return;
        revision.value++;
        statusNow.value = Date.now();
        ready.value = true;
        error.value = authState.sseStatus === 'connected' ? '' : '实时状态连接中断，当前显示最近一次校准状态';
        if (generation && (generation !== feed.generation || inventory !== feed.inventoryRevision)) options.onInventoryChanged?.();
      } catch (cause) {
        if (active && ticket === read) {
          ready.value = false;
          error.value = `实时状态同步失败：${(cause as Error).message}；当前保留最后确认状态`;
        }
      } finally {
        pending = undefined;
        if (active && ticket !== read && !document.hidden) void calibrate();
      }
    })();
    return pending;
  }

  function onRuntime(event: Event) {
    const snapshot = (event as CustomEvent<RuntimeSnapshot>).detail;
    const inventory = feed.inventoryRevision;
    const update = feed.receive(snapshot, ready.value && !document.hidden);
    if (snapshot.generation !== feed.generation) {
      freeze();
      void calibrate();
    }
    if (update) {
      revision.value++;
      statusNow.value = Date.now();
    }
    if (inventory !== feed.inventoryRevision) options.onInventoryChanged?.();
  }
  function onOpen() { feed.resetStream(); freeze(); void calibrate(); }
  function onDisconnected() {
    feed.resetStream();
    freeze();
    error.value = '实时状态连接中断，当前保留最后确认状态';
  }
  function onVisibility() {
    freeze();
    if (!document.hidden) void calibrate();
  }

  onMounted(() => {
    active = true;
    window.addEventListener('proxy-node-runtime', onRuntime);
    window.addEventListener('proxy-node-runtime-open', onOpen);
    window.addEventListener('proxy-node-runtime-disconnected', onDisconnected);
    document.addEventListener('visibilitychange', onVisibility);
    void calibrate();
    poll = setInterval(() => { void calibrate(); }, 30000);
    clock = setInterval(() => {
      if (ready.value && !document.hidden && authState.sseStatus === 'connected') statusNow.value = Date.now();
    }, 5000);
  });
  onUnmounted(() => {
    active = false;
    freeze();
    clearInterval(poll);
    clearInterval(clock);
    window.removeEventListener('proxy-node-runtime', onRuntime);
    window.removeEventListener('proxy-node-runtime-open', onOpen);
    window.removeEventListener('proxy-node-runtime-disconnected', onDisconnected);
    document.removeEventListener('visibilitychange', onVisibility);
  });
  return { statusNow, error, ready, project, calibrate };
}
