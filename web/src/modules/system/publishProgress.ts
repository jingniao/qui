import { deploymentAPI } from './deploymentApi';

export type PublishTone = 'info' | 'success' | 'danger' | 'warning';
export interface PublishPhase {
  label: string;
  tone: PublishTone;
  final: boolean;
}

export function publishPhase(status: string): PublishPhase {
  switch (status) {
    case 'draft':
      return { label: '草稿未发布', tone: 'warning', final: true };
    case 'pending':
      return { label: '待服务器确认', tone: 'info', final: false };
    case 'success':
      return { label: '已生效', tone: 'success', final: true };
    case 'failed':
      return { label: '失败，已保留原配置', tone: 'danger', final: true };
    case 'rolled_back':
      return { label: '已回滚', tone: 'danger', final: true };
    default:
      return { label: status, tone: 'warning', final: true };
  }
}

export function serverPublishState(
  latest: { id: number; status: string } | undefined,
  runningRevision: number | undefined
): PublishPhase {
  if (!latest) {
    return { label: '尚未发布', tone: 'warning', final: true };
  }
  if (latest.status === 'success') {
    if (runningRevision === latest.id) {
      return { label: '已生效', tone: 'success', final: true };
    }
    return { label: '已生效，等待服务器上报', tone: 'info', final: false };
  }
  return publishPhase(latest.status);
}

export const publishWatchTimeout = 120_000;

export interface PublishWatch {
  stop(): void;
}

export function watchPublish(
  nodeId: string,
  revision: number,
  onUpdate: (phase: PublishPhase, timedOut: boolean) => void,
  deps?: {
    list?: typeof deploymentAPI.nodeRevisions;
    now?: () => number;
    setTimer?: typeof setTimeout;
    clearTimer?: typeof clearTimeout;
    target?: EventTarget;
  }
): PublishWatch {
  const list = deps?.list ?? ((nid, page, pageSize) => deploymentAPI.nodeRevisions(nid, page, pageSize));
  const now = deps?.now ?? (() => Date.now());
  const setTimer = deps?.setTimer ?? setTimeout;
  const clearTimer = deps?.clearTimer ?? clearTimeout;
  const target = deps?.target ?? (typeof window !== 'undefined' ? window : undefined);

  let stopped = false;
  let timerId: any = null;
  let currentPhase = publishPhase('pending');
  const startTime = now();

  async function check() {
    if (stopped) return;
    if (timerId !== null) {
      clearTimer(timerId);
      timerId = null;
    }

    const elapsed = now() - startTime;
    if (elapsed >= publishWatchTimeout) {
      onUpdate(currentPhase, true);
      stop();
      return;
    }

    try {
      const res = await list(nodeId, 1, 20);
      if (stopped) return;
      const found = res.items.find(item => item.id === revision);
      if (found) {
        currentPhase = publishPhase(found.status);
        onUpdate(currentPhase, false);
        if (currentPhase.final) {
          stop();
          return;
        }
      }
    } catch {
      // Ignore network errors while polling
    }

    if (!stopped) {
      timerId = setTimer(check, 3000);
    }
  }

  const handler = () => {
    if (!stopped) {
      check();
    }
  };

  if (target && typeof target.addEventListener === 'function') {
    target.addEventListener('proxy-deployment-status', handler);
  }

  timerId = setTimer(check, 3000);

  function stop() {
    if (stopped) return;
    stopped = true;
    if (timerId !== null) {
      clearTimer(timerId);
      timerId = null;
    }
    if (target && typeof target.removeEventListener === 'function') {
      target.removeEventListener('proxy-deployment-status', handler);
    }
  }

  return { stop };
}
