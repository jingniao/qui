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
export function serverSettingsReason(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings, checkFields = true): string {
  const security = settings.security || 'none', transport = settings.transport || 'tcp', flow = settings.flow || '';
  if (!['none', 'tls', 'reality'].includes(security)) return '当前安全方式暂不支持，草稿已保留，请明确选择其他方式';
  if (!['tcp', 'ws', 'grpc', 'xhttp', 'quic'].includes(transport)) return '当前传输方式暂不支持，草稿已保留，请明确选择其他方式';
  if (transport === 'quic' && !['hysteria2', 'tuic'].includes(protocol)) return '当前协议不支持 QUIC 传输，草稿已保留，请明确选择其他传输';
  if (flow && (protocol !== 'vless' || flow !== 'xtls-rprx-vision' || transport !== 'tcp' || !['tls', 'reality'].includes(security))) return 'Vision 只支持 VLESS + TCP + TLS / Reality，请明确修改传输、安全方式或 Flow';
  if (transport === 'xhttp' && security === 'none') return 'xhttp 只能运行在 Reality 或 TLS 之上：请把安全方式切换为 Reality / TLS，或把传输改回 TCP';
  if (security !== 'reality' && transport !== 'xhttp') return '';
  if (protocol !== 'vless') return 'Reality / xhttp 只支持 VLESS 入站';
  const selected = findCore(server, core);
  if (!selected) return '尚未上报核心能力，Reality / xhttp 暂不可用';
  if (blockedCores(server).includes(selected.name)) return '该核心正在添加或移除中（core_pending），请等待新的能力上报';
  if (selected.serverSettingsContractVersion !== SERVER_SETTINGS_CONTRACT) return `${selected.label} 尚未上报可用的 Reality / xhttp 设置能力，请升级 Agent 后等待上报`;
  if (!selected.serverSettingsCombinations.some(c => c.protocol === protocol && c.direction === 'inbound' && c.security === security && c.transport === transport && (c.flow || '') === flow)) {
    return `${selected.label} 未上报支持 ${security} + ${transport}${flow ? ' + Vision' : ''} 入站；草稿已保留，请明确选择支持的组合`;
  }
  if (checkFields && selected.name === 'sing-box' && security === 'reality' && nonemptyLines(settings.reality?.server_names).length !== 1) {
    return 'sing-box 的 Reality 入站只支持一个服务器名称 / SNI；草稿已保留，请明确调整名称列表';
  }
  return '';
}

export function transportChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: 'tcp', label: 'TCP' }, { value: 'ws', label: 'WebSocket' }, { value: 'grpc', label: 'gRPC' }];
  if (protocol === 'vless' || settings.transport === 'xhttp') items.push({ value: 'xhttp', label: 'xhttp（TCP / H2）' });
  if (settings.transport && !items.some(c => c.value === settings.transport)) items.push({ value: settings.transport, label: `${settings.transport}（现有值）` });
  return items.map(c => ({ ...c, reason: transportReason(server, core, protocol, settings, c.value) }));
}
// xhttp stays offered whenever the selected core reports it with Reality or TLS.
// The security it needs is switched on selection (applyXhttpSecurity), so the option
// itself is only disabled for a core that cannot run xhttp at all.
function transportReason(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings, transport: string): string {
  const target = { ...settings, transport };
  const reason = serverSettingsReason(server, core, protocol, target, false);
  // The exact combination with the current security decides first; only a reason
  // left without any reported xhttp security disables the option.
  if (!reason || transport !== 'xhttp' || protocol !== 'vless') return reason;
  return xhttpSecurityFallback(server, core, protocol) ? '' : reason;
}
export function securityChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: 'none', label: 'None' }, { value: 'tls', label: 'TLS' }];
  if (protocol === 'vless' || settings.security === 'reality') items.push({ value: 'reality', label: 'Reality' });
  if (settings.security && !items.some(c => c.value === settings.security)) items.push({ value: settings.security, label: `${settings.security}（现有值）` });
  return items.map(c => ({ ...c, reason: serverSettingsReason(server, core, protocol, { ...settings, security: c.value }, false) }));
}
export function flowChoices(server: NodeView | undefined, core: CoreName | '', protocol: string, settings: ProtocolSettings): SettingChoice[] {
  const items = [{ value: '', label: '无' }, { value: 'xtls-rprx-vision', label: 'XTLS Vision' }];
  if (settings.flow && !items.some(c => c.value === settings.flow)) items.push({ value: settings.flow, label: `${settings.flow}（现有值）` });
  return items.map(c => ({ ...c, reason: serverSettingsReason(server, core, protocol, { ...settings, flow: c.value }, false) }));
}
// An option the selected core cannot run is hidden instead of shown greyed out.
// Only the value the draft already holds stays listed, so a select never displays
// a different value than the one that would be saved (Owner rule).
export function visibleChoices(choices: SettingChoice[], current: string): SettingChoice[] {
  return choices.filter(choice => !choice.reason || choice.value === current);
}

export const nonemptyLines = (values: string[] | undefined): string[] => (values || []).map(s => s.trim()).filter(Boolean);

// The generators below are pure: they take the byte source as an argument so the
// self-test can feed a fixed sequence, and the editor uses the Web Crypto source.
export type RandomBytes = (size: number) => Uint8Array;
const webCryptoBytes: RandomBytes = size => {
  const bytes = new Uint8Array(size);
  if (!globalThis.crypto?.getRandomValues) throw new Error('当前浏览器不提供安全随机数，请手动填写');
  return globalThis.crypto.getRandomValues(bytes);
};
// 8 random bytes as 16 lowercase hex characters: exactly one valid Short ID.
export function generateShortId(randomBytes: RandomBytes = webCryptoBytes): string {
  return Array.from(randomBytes(8)).map(byte => byte.toString(16).padStart(2, '0')).join('');
}
// A generated Short ID goes on its own line; a list the Owner typed is kept.
export function appendShortId(ids: string[] | undefined, id: string = generateShortId()): string[] {
  return [...nonemptyLines(ids), id];
}
// Only the host half of `host:port`, and only when it is a DNS name: an IP literal
// can never be a Reality server name, so it yields '' instead of a rejected draft.
export function sniFromTarget(target: string): string {
  const value = (target || '').trim();
  if (!value || value.startsWith('[')) return '';
  const host = value.replace(/:[0-9]+$/, '');
  if (!host || host.includes(':') || /^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$/.test(host) || host.length > 253) return '';
  return host.split('.').every(label => /^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/.test(label)) ? host : '';
}
// Fill the server names from the handshake target while the list is still empty.
// A filled list is an Owner decision (sing-box accepts exactly one name) and is
// never overwritten by a later target change.
export function fillServerNamesFromTarget(reality: RealitySettings): boolean {
  if (nonemptyLines(reality.server_names).length) return false;
  const sni = sniFromTarget(reality.handshake_target);
  if (!sni) return false;
  reality.server_names = [sni];
  return true;
}
// `/` plus 12 lowercase alphanumerics, which passes validateServerFields.
export function randomXhttpPath(randomBytes: RandomBytes = webCryptoBytes): string {
  const alphabet = 'abcdefghijklmnopqrstuvwxyz0123456789';
  return '/' + Array.from(randomBytes(12)).map(byte => alphabet[byte % alphabet.length]).join('');
}
// Suggested handshake targets: well-known foreign HTTPS sites that serve TLS 1.3
// and HTTP/2. A node only sends unauthenticated handshakes there, so they must not
// be the Owner's own domain.
export const REALITY_HANDSHAKE_PRESETS: readonly string[] = [
  'www.microsoft.com:443', 'www.apple.com:443', 'www.amd.com:443',
  'www.nvidia.com:443', 'dl.google.com:443', 'www.cloudflare.com:443'
];
// xhttp only rides on TLS, so the selected core's report decides which security the
// editor switches to when the Owner picks xhttp while security is still "none".
export function xhttpSecurityFallback(server: NodeView | undefined, core: CoreName | '', protocol: string): 'reality' | 'tls' | '' {
  if (protocol !== 'vless') return '';
  const selected = findCore(server, core);
  if (!selected || selected.serverSettingsContractVersion !== SERVER_SETTINGS_CONTRACT) return '';
  const reported = (security: string) => selected.serverSettingsCombinations.some(c => c.protocol === protocol
    && c.direction === 'inbound' && c.security === security && c.transport === 'xhttp' && !(c.flow || ''));
  if (reported('reality')) return 'reality';
  if (reported('tls')) return 'tls';
  return '';
}
// The 12px note the editor shows after switching the security for xhttp.
export function xhttpSecurityNote(security: 'reality' | 'tls'): string {
  return security === 'reality'
    ? '已把安全方式切换为 Reality：xhttp 只能运行在 Reality 或 TLS 之上，该核心上报了 xhttp + Reality（Reality 优先）。'
    : '已把安全方式切换为 TLS：该核心没有上报 xhttp + Reality，xhttp 只能运行在 TLS 之上，请填写证书与私钥。';
}
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
