import type { CanvasPlanEntry } from '../../api';

export interface PlanObject {
  kind: string;
  name: string;
  protocol?: string;
  port?: number;
  target?: string;
}

export interface PlanServer {
  nodeId: string;
  name: string;
  creates: PlanObject[] | null;
  updates: PlanObject[] | null;
  removes: PlanObject[] | null;
}

export interface PlanConflict {
  nodeId: string;
  port: number;
  with: string;
  connectionId: string;
  // reason/objectId identify conflicts that are not listener collisions, e.g. an
  // unadopted legacy object (K4). objectId is the server object the Owner must adopt.
  reason?: string;
  objectId?: string;
}

export interface PlanTunnel {
  tunnelId: string;
  affectedSchemes: { id: string; name: string }[];
}

export interface TunnelPlan {
  ok: boolean;
  entries?: CanvasPlanEntry[] | null;
  servers: PlanServer[] | null;
  connections: { id: string; status: string; message: string }[] | null;
  conflicts: PlanConflict[] | null;
  warnings: string[] | null;
  // Shared tunnels the publication would create or update, with every scheme that
  // references them. Absent on plans produced before K4.
  tunnels?: PlanTunnel[] | null;
  message?: string;
}

export function planLines(plan: TunnelPlan | null | undefined, nodeIds?: string[]): string[] {
  if (!plan || !plan.servers) return [];
  const lines: string[] = [];
  const filtered = nodeIds ? plan.servers.filter(s => nodeIds.includes(s.nodeId)) : plan.servers;

  for (const s of filtered) {
    if (s.creates) {
      for (const obj of s.creates) {
        lines.push(`${s.name}：新增${obj.kind === 'inbound' ? '入站' : obj.kind} ${obj.name}${obj.protocol ? ' ' + obj.protocol : ''}${obj.port ? ' :' + obj.port : ''}`);
      }
    }
    if (s.updates) {
      for (const obj of s.updates) {
        lines.push(`${s.name}：更新${obj.kind === 'outbound' ? '出站' : obj.kind} ${obj.name}${obj.target ? ' → ' + obj.target : ''}`);
      }
    }
    if (s.removes) {
      for (const obj of s.removes) {
        lines.push(`${s.name}：移除${obj.kind === 'chain' ? '代理链' : obj.kind} ${obj.name}`);
      }
    }
  }

  return lines;
}

// A conflict is not always a listener collision: an unadopted legacy object is
// reported with the object id the Owner has to adopt, so the page can name it.
export function tunnelConflictText(conflict: PlanConflict): string {
  if (conflict.reason === 'legacy_object') {
    const object = conflict.objectId ? `（对象 ${conflict.objectId}）` : '';
    return `历史对象未认领：${conflict.with}${object}`;
  }
  if (conflict.reason === 'tunnel_object') return `共享隧道对象冲突：${conflict.with}`;
  if (conflict.reason === 'object_name') return `对象名称冲突：${conflict.with}`;
  if (conflict.reason && conflict.reason !== 'listener') return `${conflict.reason}：${conflict.with}`;
  return `端口冲突 :${conflict.port}（${conflict.with}）`;
}

// The plan reports every shared tunnel a publication touches together with the
// schemes that reference it, so the dialog can say what else changes.
export function tunnelPlanLines(plan: TunnelPlan | null | undefined, nameOfTunnel: (id: string) => string): string[] {
  return (plan?.tunnels || []).map(item => {
    const name = nameOfTunnel(item.tunnelId) || item.tunnelId;
    const schemes = item.affectedSchemes.map(scheme => scheme.name).join('、') || '无';
    return `共享隧道 ${name}：影响 ${item.affectedSchemes.length} 个方案（${schemes}）`;
  });
}
