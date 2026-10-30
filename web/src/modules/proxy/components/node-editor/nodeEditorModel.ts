import type { Inbound, InboundInput, ProtocolSettings } from '../../api';
import type { NodeView } from '../../../nodes/api';

export type EditorOrigin = 'list' | 'server';
export const forcedTLS = (protocol: string) => ['trojan', 'hysteria2', 'tuic'].includes(protocol);
export const hasTransport = (protocol: string) => !['forward', 'wireguard', 'hysteria2', 'tuic'].includes(protocol);
export const hasSecurity = (protocol: string) => !['forward', 'wireguard'].includes(protocol);
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));

export interface EditorState {
  form: InboundInput;
  origin: EditorOrigin;
  original: Inbound | null;
  addresses: string;
  mtu: number;
  wireguardChanged: boolean;
}
export function createEditor(serverId = '', original: Inbound | null = null, origin: EditorOrigin = 'list'): EditorState {
  const form: InboundInput = original ? {
    name: original.name, nodeId: original.nodeId, protocol: original.protocol, listen: original.listen,
    port: original.port, enabled: original.enabled, subscriptionVisible: original.subscriptionVisible,
    internalRelay: original.internalRelay, defaultOutbound: original.defaultOutbound,
    allowedUserIds: [...original.allowedUserIds], settings: clone(original.settings || {})
  } : {
    name: '', nodeId: serverId, protocol: 'vless', listen: '0.0.0.0', port: 443, enabled: true,
    subscriptionVisible: true, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [],
    settings: { transport: 'tcp', security: 'none' }
  };
  if (origin === 'list') {
    form.settings.transport ||= 'tcp'; form.settings.security ||= 'none';
    form.settings.method ||= 'chacha20-ietf-poly1305';
    form.settings.certificate_pem = ''; form.settings.private_key_pem = ''; form.settings.trusted_ca_pem = '';
  }
  return { form, origin, original, addresses: original?.settings.wireguard?.addresses.join('\n') || '10.77.0.1/24',
    mtu: original?.settings.wireguard?.mtu || 1420, wireguardChanged: false };
}
export function changeProtocol(state: EditorState, protocol: string) {
  const f = state.form;
  f.protocol = protocol;
  if (state.origin === 'server') {
    f.settings = protocol === 'wireguard' ? {} : {
      transport: ['hysteria2', 'tuic'].includes(protocol) ? 'quic' : 'tcp',
      quic: ['hysteria2', 'tuic'].includes(protocol) ? {} : undefined,
      security: forcedTLS(protocol) ? 'tls' : 'none',
      method: protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : undefined
    };
  }
  if (forcedTLS(protocol)) f.settings.security = 'tls';
  if (protocol === 'forward') {
    if (state.origin === 'server') f.settings = { forward: { host: '', port: 443, network: 'tcp' } };
    else f.settings.forward ||= { host: '', port: 443, network: 'tcp' };
    f.subscriptionVisible = false; f.internalRelay = false; f.allowedUserIds = [];
  } else if (state.origin === 'list' && !state.original) f.subscriptionVisible = true;
}

// Match the old list's merge rules, including absent QUIC transport, optional
// TLS replacements and the empty WireGuard transport. Advanced edits are then
// applied explicitly; simply opening a step must never add settings/defaults.
export function buildPayload(state: EditorState): InboundInput {
  const f = state.form, p = f.protocol, input = f.settings;
  let settings: ProtocolSettings;
  if (state.origin === 'server') settings = clone(input);
  else {
    settings = clone(state.original?.settings || {});
    if (p === 'wireguard') settings.transport = '';
    else if (!['hysteria2', 'tuic'].includes(p)) settings.transport = input.transport;
    if (forcedTLS(p)) settings.security = 'tls';
    else if (p !== 'wireguard') settings.security = input.security;
    if (settings.security === 'tls' || (!state.original && input.security === 'tls')) {
      settings.server_name = input.server_name || undefined;
      for (const key of ['certificate_pem', 'private_key_pem', 'trusted_ca_pem'] as const) {
        if (input[key]) settings[key] = input[key];
      }
    }
    if (input.transport === 'ws') settings.path = input.path || undefined;
    if (input.transport === 'grpc') settings.service_name = input.service_name || undefined;
    if (p === 'shadowsocks') settings.method = input.method;
    if (input.quic) settings.quic = clone(input.quic);
  }
  if (p === 'forward') settings = { forward: clone(input.forward || { host: '', port: 443, network: 'tcp' }) };
  if (p === 'wireguard' && (state.origin === 'server' || state.wireguardChanged)) {
    settings = { ...(state.origin === 'list' ? settings : {}), wireguard: {
      secret_key: '', addresses: state.addresses.split(/\r?\n/).map(s => s.trim()).filter(Boolean),
      peers: input.wireguard?.peers || [], mtu: state.mtu
    } };
  }
  return clone({ name: state.origin === 'list' ? f.name.trim() : f.name, nodeId: f.nodeId, protocol: p, listen: f.listen, port: f.port,
    enabled: f.enabled, subscriptionVisible: f.internalRelay ? false : f.subscriptionVisible,
    internalRelay: f.internalRelay, defaultOutbound: f.defaultOutbound, allowedUserIds: f.allowedUserIds, settings });
}

export function serverEligible(server: NodeView): boolean {
  // Agent version is reported only after registration; pending rows have none.
  return server.enabled && !server.deletedAt && server.status !== 'pending' && !!server.agentVersion;
}
export function protocolReason(server: NodeView | undefined, protocol: string): string {
  if (!server) return '请先选择服务器';
  const caps = server.capabilities;
  if (!caps?.core_name) return '尚未上报核心能力';
  if (caps.protocols?.some(p => p.protocol === protocol && p.inbound)) return '';
  if (caps.core_name === 'xray' && ['hysteria2', 'tuic'].includes(protocol)) return 'Xray 不支持此协议，需要支持 QUIC 的 sing-box';
  if (caps.core_name === 'sing-box' && protocol === 'wireguard') return 'sing-box 不支持 WireGuard 入站';
  return `${caps.core_name} 未报告支持 ${protocol} 入站（请检查核心版本与构建标签）`;
}
// A TCP and a UDP listener may share a port (e.g. VLESS on TCP 443 next to
// Hysteria2 on UDP 443), so only overlapping transports conflict.
export function listenNetworks(value: Pick<InboundInput, 'protocol' | 'settings'>): string[] {
  if (['hysteria2', 'tuic', 'wireguard'].includes(value.protocol)) return ['udp'];
  if (value.protocol === 'forward') return (value.settings?.forward?.network || 'tcp').split(',');
  if (['shadowsocks', 'socks5'].includes(value.protocol)) return ['tcp', 'udp'];
  return ['tcp'];
}
export function portConflict(form: InboundInput, inbounds: Inbound[], excludeId = ''): string {
  const networks = listenNetworks(form);
  const conflict = inbounds.find(i => i.nodeId === form.nodeId && i.id !== excludeId && i.port === form.port
    && listenNetworks(i).some(network => networks.includes(network)));
  return conflict ? `端口 ${form.port} 已被「${conflict.name}」占用` : '';
}
export function randomPort(
  form: InboundInput,
  inbounds: Inbound[],
  excludeId = '',
  random = Math.random,
  portRange?: { start: number; end: number } | null
): number {
  const min = (portRange && portRange.start >= 1 && portRange.start <= 65535) ? portRange.start : 10000;
  const max = (portRange && portRange.end >= min && portRange.end <= 65535) ? portRange.end : 65535;
  const span = Math.max(1, max - min + 1);

  const startOffset = Math.floor(random() * span);
  for (let n = 0; n < span; n++) {
    const port = min + (startOffset + n) % span;
    if (!portConflict({ ...form, port }, inbounds, excludeId)) return port;
  }
  throw new Error(`在指定端口范围 ${min}-${max} 内没有可用的端口`);
}
export function validateStep(step: number, state: EditorState, server: NodeView | undefined, inbounds: Inbound[]): string {
  const f = state.form, s = f.settings;
  if (step === 1 && (!server || !serverEligible(server))) return '请选择可用服务器';
  if (step === 2) {
    const reason = protocolReason(server, f.protocol);
    if (reason) return reason;
    if (!f.name.trim()) return '请输入节点名称';
    if (!Number.isInteger(f.port) || f.port < 1 || f.port > 65535) return '请输入有效的端口 (1-65535)';
    return portConflict(f, inbounds, state.original?.id);
  }
  if (step === 3 && hasSecurity(f.protocol) && (forcedTLS(f.protocol) || s.security === 'tls')) {
    if (!s.certificate_pem?.trim() && !state.original?.settings.certificate_pem && !f.internalRelay) return '请输入 TLS 证书 PEM';
    if (!s.private_key_pem?.trim() && !state.original?.hasTlsKey && !f.internalRelay) return '请输入 TLS 私钥 PEM';
  }
  if (step === 4) {
    if (f.protocol === 'forward') {
      if (!s.forward?.host.trim()) return '请输入目标主机';
      if (!Number.isInteger(s.forward.port) || s.forward.port < 1 || s.forward.port > 65535) return '请输入有效的目标端口 (1-65535)';
      if (!['tcp', 'udp', 'tcp,udp'].includes(s.forward.network)) return '请选择有效的转发网络';
    }
    if (f.protocol === 'wireguard') {
      if (!state.addresses.trim()) return '请输入 WireGuard 隧道地址';
      if (!Number.isInteger(state.mtu) || state.mtu < 576 || state.mtu > 9000) return 'MTU 必须为 576–9000';
    }
    if (f.protocol === 'hysteria2') {
      for (const v of [s.quic?.up_mbps, s.quic?.down_mbps]) if (v !== undefined && (!Number.isInteger(v) || v < 0 || v > 100000)) return '带宽必须为 0–100000 Mbps';
    }
  }
  if (step === 5 && !f.listen.trim()) return '请输入监听地址';
  return '';
}
export function previewPayload(value: InboundInput): Record<string, unknown> {
  const safe = clone(value);
  for (const key of ['private_key_pem', 'certificate_pem', 'trusted_ca_pem'] as const) if (safe.settings[key]) safe.settings[key] = '（已设置）';
  if (safe.settings.wireguard) {
    safe.settings.wireguard.secret_key = '（由系统保管）';
    safe.settings.wireguard.peers = [];
  }
  return safe;
}
