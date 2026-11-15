import { APIError } from '../../app/apiClient';
import { nodeAPI } from '../nodes/api';
import { confirmedRevision, needsBaseline, publishBaselineAndWait } from '../system/nodeBaseline';
import { gostInstallRelease } from '../system/coreReleases';
import { coreUpdatesAPI } from '../system/coreUpdatesApi';
import { tunnelAPI, tunnelError, type PublishPlan, type PublishResult, type PublishSelection } from './api';
import { resultLabels } from './model';

// Saving, deleting or rotating a tunnel or rule applies it at once; there is no
// separate publish step. The server still plans the change, orders it exit to
// entry and reverts every server when one fails.

type Progress = (text: string) => void;

// installGost queues the pinned GOST on servers that lack it (publishing a
// baseline first on a server that never had a configuration) and waits until each
// reports the core. A failure stops before the tunnel is applied.
export async function installGost(servers: { nodeId: string; name: string }[], progress: Progress): Promise<void> {
  const failures: string[] = [];
  for (const server of servers) {
    try {
      let node = await nodeAPI.get(server.nodeId);
      if (!node.capabilities?.cores?.some(core => core.name === 'gost')) {
        if (needsBaseline(node)) {
          progress(`正在为 ${server.name} 发布基础配置…`);
          await publishBaselineAndWait(node);
        }
        progress(`正在为 ${server.name} 安装 GOST…`);
        const release = await gostInstallRelease(node);
        await coreUpdatesAPI.queue({ nodeId: server.nodeId, revision: confirmedRevision(node), component: 'gost', ...release });
        const deadline = Date.now() + 300000;
        while (!node.capabilities?.cores?.some(core => core.name === 'gost')) {
          const remaining = deadline - Date.now();
          if (remaining <= 0) throw new Error('等待 GOST 安装超时（5 分钟）');
          await new Promise(resolve => setTimeout(resolve, Math.min(3000, remaining)));
          if (Date.now() >= deadline) throw new Error('等待 GOST 安装超时（5 分钟）');
          node = await nodeAPI.get(server.nodeId);
        }
      }
    } catch (e) { failures.push(`${server.name}：${tunnelError(e)}`); }
  }
  if (failures.length) throw new Error(`GOST 安装未全部成功，未应用到服务器；已安装的核心保留。${failures.join('；')}`);
}

function planProblem(plan: PublishPlan): string {
  const conflicts = plan.conflicts.map(c => `${plan.servers.find(s => s.nodeId === c.nodeId)?.nodeName || c.nodeId}:${c.port} 已被 ${c.usedBy} 占用`);
  return [plan.message || '配置检查未通过', ...conflicts].join('；');
}

export async function applyTunnelChanges(selection: PublishSelection, progress: Progress = () => {}): Promise<PublishResult> {
  for (let attempt = 0; ; attempt++) {
    progress('正在检查配置…');
    const plan = await tunnelAPI.plan(selection);
    if (!plan.ok || plan.conflicts.length) throw new Error(planProblem(plan));
    await installGost(plan.installGost || [], progress);
    progress('正在下发到服务器…');
    try { return await tunnelAPI.publish(selection); }
    catch (e) {
      // A server lost GOST between the check and the start: plan and install once more.
      if (attempt === 0 && e instanceof APIError && e.status === 409 && e.code === 'gost_install_required') continue;
      throw e;
    }
  }
}

// applyProblem is empty when every server applied the change (or had nothing to change).
export function applyProblem(result: PublishResult): string {
  if (result.status === 'publishing') return '已提交，服务器仍在应用，稍后刷新查看状态。';
  const bad = result.servers.filter(s => s.status !== 'applied' && s.status !== 'unchanged');
  return bad.map(s => `${s.nodeName}：${s.message || resultLabels[s.status]}`).join('；');
}
