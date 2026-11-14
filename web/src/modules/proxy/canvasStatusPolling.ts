import type { CanvasSchemeStatus } from './api';

// A stopped subscription never delivers a late response or schedules another read.
export function pollCanvasStatus(
  read: () => Promise<CanvasSchemeStatus>,
  receive: (status: CanvasSchemeStatus) => void,
  fail: (error: unknown) => void,
  inProgress: (status?: string) => boolean,
  schedule: (callback: () => void, delay: number) => ReturnType<typeof setTimeout> = setTimeout,
  cancel: (timer: ReturnType<typeof setTimeout>) => void = clearTimeout
): () => void {
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  async function refresh() {
    try {
      const status = await read();
      if (stopped) return;
      receive(status);
      if (inProgress(status.batch.status)) timer = schedule(() => { void refresh(); }, 3000);
    } catch (error) {
      if (!stopped) fail(error);
    }
  }
  void refresh();
  return () => {
    stopped = true;
    if (timer !== undefined) cancel(timer);
  };
}
