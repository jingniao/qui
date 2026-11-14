import { nodeAPI, type NodeView } from '../nodes/api';
import { deploymentAPI } from './deploymentApi';

export function confirmedRevision(node: NodeView): number {
  return node.heartbeat?.successful_revision || node.heartbeat?.current_revision || 0;
}

export function needsBaseline(node: NodeView): boolean { return confirmedRevision(node) === 0; }

export async function publishBaselineAndWait(node: NodeView) {
  const { revision } = await deploymentAPI.publishBaseline(node.id);
  // The default deployment timeout is a few minutes; give up waiting after three.
  for (const deadline = Date.now() + 180_000; Date.now() < deadline;) {
    await new Promise(resolve => setTimeout(resolve, 3000));
    const latest = await nodeAPI.get(node.id);
    if (confirmedRevision(latest) >= revision) { Object.assign(node, latest); return; }
    const record = (await deploymentAPI.nodeRevisions(node.id, 1, 5)).items.find(item => item.id === revision);
    if (record && (record.status === 'failed' || record.status === 'rollback')) throw new Error(`基础配置发布失败（${record.resultCode || record.status}），节点保持原状`);
  }
  throw new Error('基础配置发布超时，节点仍未确认');
}
