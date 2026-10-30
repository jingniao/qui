import { reactive, ref } from 'vue';
import { canvasTunnelAPI, type CanvasTunnel, type CanvasTunnelObject } from '../../api';

// Owns the server-side tunnel library (K4). A shared tunnel outlives the drawings
// that reference it, so the page always reads it back from the panel: the browser
// keeps no copy of it (the earlier build's local pool is gone).
// Returned as a reactive object so the page's template reads plain values.
export function useCanvasTunnels() {
  const tunnels = ref<CanvasTunnel[]>([]);
  const legacyObjects = ref<CanvasTunnelObject[]>([]);
  const loading = ref(false);
  const legacyLoading = ref(false);
  const error = ref('');
  let generation = 0;

  // Older responses are ignored: the library is reloaded after every mutation.
  async function reload(): Promise<CanvasTunnel[]> {
    const token = ++generation;
    loading.value = true;
    error.value = '';
    try {
      const page = await canvasTunnelAPI.list();
      if (token !== generation) return tunnels.value;
      tunnels.value = page.items;
      return page.items;
    } catch (e) {
      if (token === generation) error.value = `载入共享隧道失败：${(e as Error).message}`;
      return tunnels.value;
    } finally {
      if (token === generation) loading.value = false;
    }
  }

  // Legacy objects are the server objects an earlier publication created without a
  // recorded owner. Listing them changes nothing; only an explicit adopt claims one.
  async function loadLegacyObjects(): Promise<void> {
    legacyLoading.value = true;
    try {
      const page = await canvasTunnelAPI.legacyObjects();
      legacyObjects.value = page.items;
    } catch (e) {
      error.value = `载入历史对象失败：${(e as Error).message}`;
    } finally {
      legacyLoading.value = false;
    }
  }

  async function remove(id: string): Promise<void> {
    await canvasTunnelAPI.delete(id);
    await reload();
  }

  async function adopt(tunnelId: string, object: CanvasTunnelObject): Promise<void> {
    await canvasTunnelAPI.adopt(tunnelId, [{ kind: object.kind, id: object.id }]);
    await Promise.all([reload(), loadLegacyObjects()]);
  }

  function byId(id: string): CanvasTunnel | undefined {
    return tunnels.value.find(tunnel => tunnel.id === id);
  }

  function nameOf(id: string): string {
    return byId(id)?.name || '';
  }

  return reactive({ tunnels, legacyObjects, loading, legacyLoading, error, reload, loadLegacyObjects, remove, adopt, byId, nameOf });
}
