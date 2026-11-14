import type { CanvasSchemeStatus } from './api';

export interface CanvasStatusItem {
  key: string;
  label: string;
  value: string;
  tone: 'neutral' | 'success' | 'warning' | 'error';
  details: string[];
  note?: string;
}

const deploymentLabels: Record<string, string> = {
  draft: '待部署', deploying: '进行中', success: '成功', failed: '失败',
  rolling_back: '进行中（回滚）', rolled_back: '失败（已回滚）', rollback_failed: '失败（回滚失败）',
  waiting: '等待前置节点', pending: '待确认', rollback_pending: '等待回滚确认'
};
const entryLabels: Record<string, string> = { applied: '已绑定', detached: '已脱离方案', removed: '已移除' };
const probeLabels: Record<string, string> = { untested: '未探测', reachable: '入口 TCP 可达', unreachable: '入口 TCP 不可达', incomplete: '探测未完成' };

export function canvasBatchInProgress(status?: string): boolean {
  return status === 'deploying' || status === 'rolling_back';
}

export function canvasStatusItems(status: CanvasSchemeStatus): CanvasStatusItem[] {
  const batch = status.batch;
  const confirmed = status.nodes.filter(node => node.confirmed).length;
  return [
    {
      key: 'draft', label: '草稿', value: status.draft.dirty ? '有未发布修改' : '已同步',
      tone: status.draft.dirty ? 'warning' : 'success',
      details: [
        `草稿更新：${status.draft.updatedAt}`,
        status.draft.publishedAt ? `最近发布：${status.draft.publishedAt}` : '从未发布'
      ]
    },
    {
      key: 'batch', label: '部署批次', value: batch.status ? deploymentLabels[batch.status] || batch.status : '尚无部署批次',
      tone: batch.status === 'success' ? 'success' : ['failed', 'rolled_back', 'rollback_failed'].includes(batch.status || '') ? 'error' : 'neutral',
      details: [
        ...(batch.id ? [`批次：${batch.id}`] : []),
        ...batch.items.map(item => `${item.nodeName || item.nodeId}：${deploymentLabels[item.status] || item.status}${item.revision === undefined ? '' : ` · 修订 #${item.revision}`}`)
      ]
    },
    {
      key: 'nodes', label: '节点确认', value: status.nodes.length ? `${confirmed} / ${status.nodes.length} 已确认` : '暂无确认记录',
      tone: status.nodes.length && confirmed === status.nodes.length ? 'success' : 'warning',
      details: status.nodes.map(node => `${node.nodeName || node.nodeId}：运行 ${node.confirmedRevision === undefined ? '未知' : `#${node.confirmedRevision}`} / 期望 ${node.expectedRevision === undefined ? '未下发' : `#${node.expectedRevision}`} · ${node.confirmed ? '已确认' : '未确认'}`)
    },
    {
      key: 'entries', label: '入口绑定', value: status.entries.length ? `${status.entries.length} 个入口` : '用户入口未绑定',
      tone: status.entries.length ? 'neutral' : 'warning',
      details: status.entries.map(entry => [
        entry.name || entry.inboundId || entry.instanceId,
        entry.mode === 'create' ? '新建独立入口' : entry.mode === 'reuse' ? '复用入口' : entry.mode,
        entry.state ? entryLabels[entry.state] || entry.state : undefined,
        entry.route ? `路由：${entry.route}` : undefined
      ].filter(Boolean).join(' · '))
    },
    {
      key: 'subscription', label: '订阅条件', value: status.subscription.ready ? '配置条件齐全' : '条件未齐全',
      tone: status.subscription.ready ? 'success' : 'warning', details: [...status.subscription.reasons],
      note: '此处为管理员总体条件，不代表每个用户都有权限；实际授权以用户所属分组为准。配置条件齐全不表示真实连通。'
    },
    {
      key: 'probe', label: '连通探测', value: probeLabels[status.probe.status] || status.probe.status,
      tone: status.probe.status === 'reachable' ? 'success' : status.probe.status === 'unreachable' ? 'error' : 'neutral',
      details: [
        ...(status.probe.at ? [`探测时间：${status.probe.at}`] : []),
        ...(status.probe.results || []).map(result => [
          `入口 ${result.inboundId}`, `探测服务器 ${result.nodeId}`,
          probeLabels[result.status] || result.status,
          `尝试 ${result.attempted} 次，失败 ${result.lost} 次`,
          result.medianMs === undefined ? undefined : `延迟中位数 ${result.medianMs} ms`,
          result.code
        ].filter(Boolean).join(' · '))
      ],
      note: status.probe.status === 'untested' ? undefined : '这是已有的入口 TCP 探测记录，仅反映当时入口的 TCP 可达性，不代表整条中转链路连通。'
    }
  ];
}
