import type { ProtocolSettings, RealitySettings } from '../../api';
import type { CoreName, NodeView } from '../../../nodes/api';
import { blockedCores, findCore } from '../../../nodes/coreSummary';

export const SERVER_SETTINGS_CONTRACT = 1;
export const XHTTP_MODES = ['auto', 'packet-up', 'stream-up', 'stream-one'] as const;
export interface SettingChoice { value: string; label: string; reason: string; }
export function internalRelaySettingsReason(internalRelay: boolean, settings: ProtocolSettings): string {
  return internalRelay && (settings.security === 'reality' || settings.transport === 'xhttp')
    ? '内部中转暂不支持 Reality / xhttp；请保留 TCP + TLS，或在新建用户接入时使用这些设置。已填草稿保留。' : '';
}

// Only the selected core's report authorizes a server combination. Version text,
// the scalar VLESS flag and a neighbouring core's report grant nothing.
export function serverSettingsReason(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): string {
  const security = settings.security || 'none', transport = settings.transport || 'tcp', flow = settings.flow || '';
  if (!['none', 'tls', 'reality'].includes(security)) return '当前安全方式暂不支持，草稿已保留，请明确选择其他方式';
  if (!['tcp', 'ws', 'grpc', 'xhttp', 'quic'].includes(transport)) return '当前传输方式暂不支持，草稿已保留，请明确选择其他方式';
  if (transport === 'quic' && !['hysteria2', 'tuic'].includes(protocol)) return '当前协议不支持 QUIC 传输，草稿已保留，请明确选择其他传输';
  if (flow && (protocol !== 'vless' || flow !== 'xtls-rprx-vision' || transport !== 'tcp' || !['tls', 'reality'].includes(security))) return 'Vision 只支持 VLESS + TCP + TLS / Reality，请明确修改传输、安全方式或 Flow';
  if (security !== 'reality' && transport !== 'xhttp') return '';
  if (protocol !== 'vless') return 'Reality / xhttp 只支持 VLESS 入站';
  const selected = findCore(server, core);
  if (!selected) return '尚未上报核心能力，Reality / xhttp 暂不可用';
  if (blockedCores(server).includes(selected.name)) return '该核心正在添加或移除中（core_pending），请等待新的能力上报';
  if (selected.serverSettingsContractVersion !== SERVER_SETTINGS_CONTRACT) return `${selected.label} 尚未上报可用的 Reality / xhttp 设置能力，请升级 Agent 后等待上报`;
  if (!selected.serverSettingsCombinations.some(c => c.protocol === protocol && c.direction === 'inbound' && c.security === security && c.transport === transport && (c.flow || '') === flow)) {
    return `${selected.label} 未上报支持 ${security} + ${transport}${flow ? ' + Vision' : ''} 入站；草稿已保留，请明确选择支持的组合`;
  }
  if (selected.name === 'sing-box' && security === 'reality' && nonemptyLines(settings.reality?.server_names).length !== 1) {
    return 'sing-box 的 Reality 入站只支持一个服务器名称 / SNI；草稿已保留，请明确调整名称列表';
  }
  return '';
}

export function transportChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: 'tcp', label: 'TCP' }, { value: 'ws', label: 'WebSocket' }, { value: 'grpc', label: 'gRPC' }];
  if (protocol === 'vless' || settings.transport === 'xhttp') items.push({ value: 'xhttp', label: 'xhttp（TCP / H2）' });
  if (settings.transport && !items.some(c => c.value === settings.transport)) items.push({ value: settings.transport, label: `${settings.transport}（现有值）` });
  return items.map(c => ({ ...c, reason: serverSettingsReason(server, core, protocol, { ...settings, transport: c.value }) }));
}
export function securityChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: 'none', label: 'None' }, { value: 'tls', label: 'TLS' }];
  if (protocol === 'vless' || settings.security === 'reality') items.push({ value: 'reality', label: 'Reality' });
  if (settings.security && !items.some(c => c.value === settings.security)) items.push({ value: settings.security, label: `${settings.security}（现有值）` });
  return items.map(c => ({ ...c, reason: serverSettingsReason(server, core, protocol, { ...settings, security: c.value }) }));
}
export function flowChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: '', label: '无' }, { value: 'xtls-rprx-vision', label: 'XTLS Vision' }];
  if (settings.flow && !items.some(c => c.value === settings.flow)) items.push({ value: settings.flow, label: `${settings.flow}（现有值）` });
  return items.map(c => ({ ...c, reason: serverSettingsReason(server, core, protocol, { ...settings, flow: c.value }) }));
}

export const nonemptyLines = (values: string[] | undefined): string[] => (values || []).map(s => s.trim()).filter(Boolean);
export function publicReality(value: RealitySettings): RealitySettings {
  // Select public fields explicitly, including when reading a foreign response.
  return { public_key: value.public_key || '', handshake_target: value.handshake_target?.trim() || '', server_names: nonemptyLines(value.server_names), short_ids: nonemptyLines(value.short_ids) };
}

export function validateServerFields(settings: ProtocolSettings): string {
  if (settings.security === 'reality') {
    const r = settings.reality;
    if (!r?.handshake_target?.trim()) return '请输入 Reality 握手目标（主机:端口），不会自动选择站点';
    const target = r.handshake_target.trim();
    const match = /^(?:\[[^\]\s]+\]|[^:/\s]+):([0-9]+)$/.exec(target);
    if (!match || Number(match[1]) < 1 || Number(match[1]) > 65535) return 'Reality 握手目标必须为主机:端口，IPv6 请使用 [地址]:端口';
    const names = nonemptyLines(r.server_names), ids = nonemptyLines(r.short_ids);
    if (!names.length || names.length > 16) return '请填写 1–16 个 Reality 服务器名称（每行一个）';
    const ipv4 = (name: string) => name.split('.').length === 4 && name.split('.').every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
    if (names.some(name => name.length > 253 || ipv4(name) || !name.split('.').every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label)))) return 'Reality 服务器名称须为 DNS 主机名，不能使用通配符、IP、端口或 URL，最多 253 个字符';
    if (!ids.length || ids.length > 16 || ids.some(id => !/^(?:[0-9a-fA-F]{2}){1,8}$/.test(id))) return '请填写 1–16 个 Short ID，每个为 2–16 位偶数长度十六进制';
    if (r.public_key && !/^[A-Za-z0-9_-]{43}$/.test(r.public_key)) return 'Reality 公钥格式无效，请重新读取入站；轮换必须使用明确的轮换选项';
  }
  if (settings.transport === 'xhttp') {
    const x = settings.xhttp;
    if (!x || !XHTTP_MODES.includes(x.mode as typeof XHTTP_MODES[number])) return '请选择支持的 xhttp 模式';
    const path = x.path || '';
    if (!path.startsWith('/') || path.length > 256 || /[\u0000-\u001f\u007f]/.test(path)) return 'xhttp 路径须以 / 开始，最多 256 个字符，不能包含控制字符';
    if (x.host && (x.host.length > 253 || /[:/\s\\@]/.test(x.host))) return 'xhttp Host 须为不带端口、路径或 URL 的主机名，最多 253 个字符';
  }
  return '';
}
