import type { NodeTuningInput, NodeTuning } from '../../api';

export interface TuningField {
  key: string;
  label: string;
  help: string;
  choices?: string[];
  min?: number;
  max?: number;
}

export type KeyStatus = 'panel' | 'external' | 'default' | 'drift' | 'unavailable';

export const tuningFields: TuningField[] = [
  {
    key: 'net.ipv4.tcp_congestion_control',
    label: 'TCP 拥塞控制',
    choices: ['bbr', 'cubic', 'reno', 'htcp', 'hybla', 'bbr2', 'bbr3'],
    help: '选择拥塞控制算法；BBR 在长距离和有丢包的线路上吞吐更稳定。'
  },
  {
    key: 'net.core.default_qdisc',
    label: '排队算法',
    choices: ['fq', 'fq_codel', 'cake', 'pfifo_fast'],
    help: '网络包排队调度算法；fq 能为 BBR 提供精确的发送节奏控制。'
  },
  {
    key: 'net.ipv4.tcp_slow_start_after_idle',
    label: 'tcp_slow_start_after_idle',
    min: 0,
    max: 1,
    help: '0 表示长连接空闲后不重新慢启动；对代理长连接有利，几乎无副作用。'
  },
  {
    key: 'net.ipv4.tcp_notsent_lowat',
    label: 'tcp_notsent_lowat',
    min: 16384,
    max: 16777216,
    help: '控制套接字未发送缓冲区阈值；减少发送端排队堆积，降低交互延迟。'
  },
  {
    key: 'net.ipv4.tcp_rmem',
    label: 'tcp_rmem（仅最大值）',
    min: 4194304,
    max: 268435456,
    help: 'TCP 接收缓冲区上限；长距离大带宽链路需要足够缓冲以充分利用带宽。'
  },
  {
    key: 'net.ipv4.tcp_wmem',
    label: 'tcp_wmem（仅最大值）',
    min: 4194304,
    max: 268435456,
    help: 'TCP 发送缓冲区上限；决定单连接在长延迟高带宽链路上的发送窗口。'
  },
  {
    key: 'net.core.rmem_max',
    label: 'rmem_max',
    min: 212992,
    max: 268435456,
    help: '套接字接收缓冲全局最大值；QUIC（Hysteria2/TUIC）等 UDP 传输需要足够上限。'
  },
  {
    key: 'net.core.wmem_max',
    label: 'wmem_max',
    min: 212992,
    max: 268435456,
    help: '套接字发送缓冲全局最大值；QUIC（Hysteria2/TUIC）等 UDP 传输需要足够上限。'
  },
  {
    key: 'net.ipv4.tcp_mtu_probing',
    label: 'tcp_mtu_probing',
    min: 0,
    max: 2,
    help: '路径 MTU 黑洞探测；设为 1 或 2 可在中转线路上自动避开 PMTU 黑洞。'
  },
  {
    key: 'fs.file-max',
    label: '文件句柄上限',
    min: 65536,
    max: 16777216,
    help: '系统级最大打开文件数；代理高并发连接时需确保足够上限。'
  },
  {
    key: 'net.core.somaxconn',
    label: '连接队列',
    min: 128,
    max: 65535,
    help: '监听队列上限；高并发接入时防止突发新建连接被拒绝。'
  },
  {
    key: 'net.ipv4.tcp_max_syn_backlog',
    label: '半连接队列',
    min: 128,
    max: 262144,
    help: 'SYN 队列上限；防止大量突发握手请求导致握手超时或丢弃。'
  },
  {
    key: 'net.ipv4.tcp_fastopen',
    label: 'TCP Fast Open',
    min: 0,
    max: 3,
    help: '仅在核心对连接启用 TFO 时有效，当前不渲染 TFO，通常保持默认。'
  },
  {
    key: 'net.ipv4.tcp_ecn',
    label: 'tcp_ecn',
    min: 0,
    max: 2,
    help: '显式拥塞通知；建议保持默认 2，改为 1 可能被沿途旧设备误丢包。'
  },
  {
    key: 'net.ipv4.tcp_tw_reuse',
    label: 'tcp_tw_reuse',
    min: 0,
    max: 2,
    help: '允许重用 TIME_WAIT 状态套接字用于新连接；缓解出向端口耗尽。'
  },
  {
    key: 'net.core.rmem_default',
    label: 'rmem_default',
    min: 212992,
    max: 268435456,
    help: '套接字接收缓冲默认值；未显式设置缓冲区的套接字使用此大小。'
  },
  {
    key: 'net.core.wmem_default',
    label: 'wmem_default',
    min: 212992,
    max: 268435456,
    help: '套接字发送缓冲默认值；未显式设置缓冲区的套接字使用此大小。'
  },
  {
    key: 'net.ipv4.udp_rmem_min',
    label: 'udp_rmem_min',
    min: 4096,
    max: 1048576,
    help: 'UDP 接收缓冲保底值；系统内存压力下 UDP 仍能保留的基本缓冲区。'
  },
  {
    key: 'net.ipv4.udp_wmem_min',
    label: 'udp_wmem_min',
    min: 4096,
    max: 1048576,
    help: 'UDP 发送缓冲保底值；系统内存压力下 UDP 仍能保留的基本缓冲区。'
  }
];

export const essentialKeys: string[] = [
  'net.ipv4.tcp_congestion_control',
  'net.core.default_qdisc',
  'net.ipv4.tcp_slow_start_after_idle',
  'net.ipv4.tcp_notsent_lowat',
  'net.ipv4.tcp_rmem',
  'net.ipv4.tcp_wmem',
  'net.core.rmem_max',
  'net.core.wmem_max',
  'net.ipv4.tcp_mtu_probing'
];

function tuningValue(key: string, value?: string): string | undefined {
  if (value !== undefined && (key === 'net.ipv4.tcp_rmem' || key === 'net.ipv4.tcp_wmem')) {
    return value.trim().split(/\s+/).at(-1);
  }
  return value;
}

export function keyStatus(key: string, t: any, host: any): KeyStatus {
  const keys = host?.tuningKeys || host?.tuning_keys;
  if (Array.isArray(keys)) {
    const k = keys.find((x: any) => x.key === key);
    if (k && (!k.present || !k.writable)) return 'unavailable';
  }
  const current = tuningValue(key, t?.current?.[key]);
  const desired = tuningValue(key, t?.desired?.[key]);
  if (desired !== undefined && desired !== '') {
    return current === desired ? 'panel' : 'drift';
  }
  const sources = host?.sysctlSources || host?.sysctl_sources;
  if (Array.isArray(sources)) {
    const src = sources.find((s: any) => s.key === key);
    if (src && src.file !== '99-proxy-tuning.conf' && src.value !== undefined && current === tuningValue(key, src.value)) {
      return 'external';
    }
  }
  return 'default';
}

export function algorithmOptions(kind: 'cc' | 'qdisc', host: any, current?: string): { value: string; label: string }[] {
  const avail = kind === 'cc'
    ? (host?.congestionAvailable ?? host?.congestion_available)
    : (host?.qdiscAvailable ?? host?.qdisc_available);
  const load = kind === 'cc'
    ? (host?.congestionLoadable ?? host?.congestion_loadable)
    : (host?.qdiscLoadable ?? host?.qdisc_loadable);
  const fallback = tuningFields.find(f => f.key === (kind === 'cc' ? 'net.ipv4.tcp_congestion_control' : 'net.core.default_qdisc'))!.choices!;
  const result: { value: string; label: string }[] = [];
  const seen = new Set<string>();
  for (const v of avail ?? (load === undefined ? fallback : [])) {
    if (!seen.has(v)) {
      seen.add(v);
      result.push({ value: v, label: v });
    }
  }
  for (const v of load ?? []) {
    if (!seen.has(v)) {
      seen.add(v);
      result.push({ value: v, label: `${v}（可加载）` });
    }
  }
  if (current && !seen.has(current)) {
    result.unshift({ value: current, label: current });
  }
  return result;
}

export function adoptExternal(draft: Record<string, string>, t: any, host: any): Record<string, string> {
  const result = { ...draft };
  const sources = host?.sysctlSources || host?.sysctl_sources || [];
  for (const s of sources) {
    if (keyStatus(s.key, {}, host) !== 'unavailable' && s.file !== '99-proxy-tuning.conf' && s.value !== undefined && tuningValue(s.key, t?.current?.[s.key]) === tuningValue(s.key, s.value)) {
      result[s.key] = tuningValue(s.key, s.value)!;
    }
  }
  return result;
}

export function applyAdvice(draft: Record<string, string>, items: { key: string; value: string }[], available?: Set<string>, host?: any): Record<string, string> {
  return applyPreset(draft, { settings: Object.fromEntries(items.map(item => [item.key, item.value])) }, available, host);
}

function valueChoices(field: TuningField, host: any): string[] | undefined {
  if (field.key === 'net.ipv4.tcp_congestion_control' || field.key === 'net.core.default_qdisc') {
    return algorithmOptions(field.key === 'net.ipv4.tcp_congestion_control' ? 'cc' : 'qdisc', host).map(option => option.value);
  }
  return field.choices;
}

export function tuningStatus(status: string): string {
  return ({ pending: '等待 Agent 执行', applied: '已应用', cleared: '已清除', failed: '执行失败', unconfirmed: '结果未确认' } as Record<string, string>)[status] || '未知状态';
}

/** Complete draft for every whitelisted key: an empty value selects the "不设置" option. */
export function tuningDraft(settings: Record<string, string> = {}): Record<string, string> {
  const draft: Record<string, string> = {};
  for (const field of tuningFields) {
    const value = settings[field.key];
    draft[field.key] = typeof value === 'string' ? value : '';
  }
  return draft;
}

export function tuningRequest(nodeId: string, draft: Record<string, string>, clear = false, available?: Set<string>, host?: any): NodeTuningInput {
  if (clear) return { settings: {}, clear: true, confirmNodeId: nodeId };
  const settings: Record<string, string> = {};
  for (const [key, raw] of Object.entries(draft)) {
    if (available && !available.has(key)) continue;
    const value = raw.trim();
    if (!value) continue;
    const field = tuningFields.find(f => f.key === key);
    if (!field) throw new Error('非白名单参数：' + key);
    const choices = valueChoices(field, host);
    if (choices ? !choices.includes(value) : !/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value) < field.min! || Number(value) > field.max!) throw new Error('参数超出允许范围：' + key);
    settings[key] = value;
  }
  if (!Object.keys(settings).length) throw new Error('请填写至少一个参数；恢复原值请使用清除调优');
  return { settings, clear: false, confirmNodeId: nodeId };
}

export const tuningPresets: { name: string; settings: Record<string, string> }[] = [
  {
    name: '通用代理',
    settings: {
      'net.ipv4.tcp_congestion_control': 'bbr',
      'net.core.default_qdisc': 'fq',
      'net.ipv4.tcp_slow_start_after_idle': '0',
      'net.ipv4.tcp_notsent_lowat': '131072'
    }
  },
  {
    name: '高带宽长距离',
    settings: {
      'net.ipv4.tcp_congestion_control': 'bbr',
      'net.core.default_qdisc': 'fq',
      'net.ipv4.tcp_slow_start_after_idle': '0',
      'net.ipv4.tcp_notsent_lowat': '131072',
      'net.ipv4.tcp_rmem': '33554432',
      'net.ipv4.tcp_wmem': '33554432',
      'net.core.rmem_max': '33554432',
      'net.core.wmem_max': '33554432'
    }
  },
  {
    name: 'QUIC/Hysteria2',
    settings: {
      'net.ipv4.tcp_congestion_control': 'bbr',
      'net.core.default_qdisc': 'fq',
      'net.ipv4.tcp_slow_start_after_idle': '0',
      'net.ipv4.tcp_notsent_lowat': '131072',
      'net.core.rmem_max': '8388608',
      'net.core.wmem_max': '8388608'
    }
  },
  {
    name: '小内存 VPS',
    settings: {
      'net.ipv4.tcp_congestion_control': 'bbr',
      'net.core.default_qdisc': 'fq',
      'net.ipv4.tcp_slow_start_after_idle': '0',
      'net.ipv4.tcp_notsent_lowat': '131072',
      'net.ipv4.tcp_rmem': '8388608',
      'net.ipv4.tcp_wmem': '8388608',
      'net.core.rmem_max': '8388608',
      'net.core.wmem_max': '8388608'
    }
  }
];

/** Merge a preset into the draft; keys the preset does not name keep their draft value. */
export function applyPreset(draft: Record<string, string>, preset: { settings: Record<string, string> }, available?: Set<string>, host?: any): Record<string, string> {
  const result: Record<string, string> = { ...draft };
  for (const [key, value] of Object.entries(preset.settings)) {
    const field = tuningFields.find(f => f.key === key);
    const choices = field && valueChoices(field, host);
    if ((!available || available.has(key)) && (!choices || choices.includes(value))) {
      result[key] = value;
    }
  }
  return result;
}

/** Keys that are desired now but empty in the draft: submitting restores their initial value. */
export function revertedKeys(desired: Record<string, string>, draft: Record<string, string>): string[] {
  const result: string[] = [];
  for (const [key, val] of Object.entries(desired)) {
    if (val && !draft[key]) {
      result.push(key);
    }
  }
  return result;
}
