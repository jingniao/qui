import type { CanvasRemoveStatus } from './api';

// The parent retains this promise after the dialog closes, so completion still
// refreshes its list. A restart loses outcomes; only idle needs the 404 check.
export async function waitCanvasRemoval(
  id: string,
  api: { removeStatus(id: string): Promise<CanvasRemoveStatus>; get(id: string): Promise<unknown> },
  pause = (ms: number) => new Promise<void>(resolve => setTimeout(resolve, ms)),
  now = () => Date.now()
): Promise<string> {
  const deadline = now() + 3600000;
  while (now() < deadline) {
    await pause(2000);
    const status = await api.removeStatus(id);
    if (status.state === 'succeeded') return id;
    if (status.state === 'failed') throw new Error(status.error?.message || '撤除失败，请查看节点配置历史');
    if (status.state === 'idle') {
      try { await api.get(id); }
      catch (cause) {
        if ((cause as { status?: number }).status === 404) return id;
        throw cause;
      }
    }
  }
  throw new Error('等待撤除结果已超过一小时，请刷新查看方案和节点状态');
}
