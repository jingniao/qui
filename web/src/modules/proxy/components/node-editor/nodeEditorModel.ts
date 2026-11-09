import type { ForwardSettings, ForwardTarget, Inbound, InboundInput, ProtocolSettings, TLSSource } from '../../api';
import type { CoreName, NodeView } from '../../../nodes/api';
import { blockedCores, findCore, multiCoreSupported, nodeCores, primaryCoreName } from '../../../nodes/coreSummary';
import { connectAddress } from '../../../nodes/nodeAddress';
import { generateShortId, internalRelaySettingsReason, nonemptyLines, publicReality, serverSettingsReason, validateServerFields, xhttpSecurityFallback, xhttpSecurityNote } from './serverSettingsModel';
import type { ManagedCertificate } from '../../../system/certificatesApi';
export { transportChoices, securityChoices, flowChoices, visibleChoices, internalRelaySettingsReason, serverSettingsReason, validateServerFields, XHTTP_MODES, generateShortId, appendShortId, sniFromTarget, fillServerNamesFromTarget, randomXhttpPath, REALITY_HANDSHAKE_PRESETS, xhttpSecurityFallback } from './serverSettingsModel';

export type EditorOrigin = 'list' | 'server';
export const forcedTLS = (protocol: string) => ['trojan', 'hysteria2', 'tuic'].includes(protocol);
export const hasTransport = (protocol: string) => !['forward', 'wireguard', 'hysteria2', 'tuic'].includes(protocol);
export const hasSecurity = (protocol: string) => !['forward', 'wireguard'].includes(protocol);
const clone = <T>(value: T): T => JSON.parse(JSON.stringify(value));

// GOST forwards can carry a target pool, a port range and bandwidth/connection
// limits (G2). Those settings belong to the GOST settings contract 2, so the editor
// offers them only for a GOST core that reports it and refuses to move such a
// forward onto a core that cannot render them.
export const GOST_FORWARD_CONTRACT = 2;
export const GOST_FORWARD_LIMITS = { targets: 32, ports: 256, connections: 100000, fails: 100, failTimeout: 3600, bandwidth: 1_000_000_000_000 };
const GOST_FORWARD_KEYS: (keyof ForwardSettings)[] = ['targets', 'strategy', 'max_fails', 'fail_timeout', 'port_end', 'bandwidth_in_bps', 'bandwidth_out_bps', 'max_connections'];
// The GOST-only options that are actually set. Presence-based on purpose: a zero or
// a default the Owner typed is still a setting this core cannot render.
export function gostOptions(settings: ProtocolSettings | undefined): (keyof ForwardSettings)[] {
  const forward = settings?.forward;
  if (!forward) return [];
  return GOST_FORWARD_KEYS.filter(key => forward[key] !== undefined);
}
export function gostForwardReady(server: NodeView | undefined, core: CoreName | ''): boolean {
  const selected = findCore(server, core);
  return !!selected && selected.name === 'gost' && selected.gostContractVersion >= GOST_FORWARD_CONTRACT;
}
// The inclusive listener ports of one inbound. A GOST forward range reserves every
// number it listens on, exactly like the backend expands it.
export function forwardListenPorts(port: number, settings?: ProtocolSettings): number[] {
  const ports = [port];
  const end = settings?.forward?.port_end;
  if (end === undefined || !Number.isInteger(end) || end <= port) return ports;
  for (let value = port + 1; value <= end && value <= 65535 && ports.length < GOST_FORWARD_LIMITS.ports; value++) ports.push(value);
  return ports;
}
// The ordered pool the editor and the preview show: the explicit pool when one is
// configured, otherwise the single legacy target.
export function forwardPool(forward: ForwardSettings | undefined): ForwardTarget[] {
  if (!forward) return [];
  return forward.targets?.length ? forward.targets : [{ host: forward.host, port: forward.port }];
}
// A relay listener is the exit half of a GOST tunnel: the canvas generated it and
// it carries no target, network or port of its own. The editor shows it read-only
// apart from its name and state, and never validates the forward fields it cannot
// have; only the entry half and plain forwards dial a target.
export function relayMode(settings: ProtocolSettings | undefined): boolean {
  return settings?.forward?.mode === 'relay';
}

export interface EditorState {
  form: InboundInput;
  origin: EditorOrigin;
  original: Inbound | null;
  addresses: string;
  mtu: number;
  wireguardChanged: boolean;
  protocolDrafts: Record<string, ProtocolSettings>;
}
export function createEditor(serverId = '', original: Inbound | null = null, origin: EditorOrigin = 'list'): EditorState {
  const form: InboundInput = original ? {
    name: original.name, nodeId: original.nodeId, protocol: original.protocol, listen: original.listen,
    port: original.port, enabled: original.enabled, subscriptionVisible: original.subscriptionVisible,
    internalRelay: original.internalRelay, defaultOutbound: original.defaultOutbound,
    allowedUserIds: [...original.allowedUserIds], settings: clone(original.settings || {}),
    core: original.core || ''
  } : {
    name: '', nodeId: serverId, protocol: 'vless', listen: '0.0.0.0', port: 443, enabled: true,
    subscriptionVisible: true, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [],
    settings: { transport: 'tcp', security: 'none' }, core: ''
  };
  if (original?.certificateId) form.certificateId = original.certificateId;
  if (origin === 'list') {
    form.settings.transport ||= 'tcp'; form.settings.security ||= 'none';
    form.settings.method ||= 'chacha20-ietf-poly1305';
    form.settings.certificate_pem = ''; form.settings.private_key_pem = ''; form.settings.trusted_ca_pem = '';
  }
  if (form.settings.security === 'reality') {
    const r = form.settings.reality;
    form.settings.reality = { public_key: r?.public_key || '', handshake_target: r?.handshake_target || '', server_names: r?.server_names || [], short_ids: r?.short_ids || [] };
  }
  if (form.settings.transport === 'xhttp' && !form.settings.xhttp) form.settings.xhttp = { host: '', path: '', mode: '' };
  return { form, origin, original, addresses: original?.settings.wireguard?.addresses.join('\n') || '10.77.0.1/24',
    mtu: original?.settings.wireguard?.mtu || 1420, wireguardChanged: false, protocolDrafts: {} };
}
export function changeProtocol(state: EditorState, protocol: string) {
  const f = state.form;
  if (f.protocol !== protocol) state.protocolDrafts[f.protocol] = clone(f.settings);
  f.protocol = protocol;
  if (state.protocolDrafts[protocol]) f.settings = clone(state.protocolDrafts[protocol]);
  else if (state.origin === 'server') {
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

// Switching fields preserves their inactive drafts. The payload builder alone
// removes mutually exclusive settings, so switching back restores entered data.
export function changeTransport(state: EditorState, transport: string) {
  state.form.settings.transport = transport;
  if (transport === 'xhttp') state.form.settings.xhttp ||= { host: '', path: '/', mode: 'auto' };
}
export function changeSecurity(state: EditorState, security: string) {
  state.form.settings.security = security;
  if (security !== 'reality') return;
  const settings = state.form.settings;
  settings.reality ||= { public_key: '', handshake_target: '', server_names: [], short_ids: [] };
  // First switch to Reality: one ready-to-use Short ID saves the Owner a guess. A
  // list that already has values is never replaced.
  if (!nonemptyLines(settings.reality.short_ids).length) settings.reality.short_ids = [generateShortId()];
}
// xhttp only runs on Reality or TLS. Picking it while the current security cannot
// carry xhttp on the selected core (None, or a security the core does not report with
// xhttp) switches to the security the core reports and returns the note the editor
// shows, so the selection never ends in an unroutable pair. A security that already
// runs xhttp here is the Owner's decision and is never replaced: a core that reports
// both TLS and Reality must not move a deliberately chosen TLS listener to Reality.
export function applyXhttpSecurity(state: EditorState, server: NodeView | undefined, core: CoreName | '' = state.form.core || ''): string {
  const f = state.form, s = f.settings;
  if (f.protocol !== 'vless' || s.transport !== 'xhttp') return '';
  // Flow is reported on its own and never justifies rewriting the security.
  if (!serverSettingsReason(server, core, f.protocol, { ...s, transport: 'xhttp', flow: '' }, false)) return '';
  const reported = xhttpSecurityFallback(server, core, f.protocol);
  if (!reported) return '';
  changeSecurity(state, reported);
  return xhttpSecurityNote(reported);
}

// The TLS block the editor shows: the protocols that are TLS by definition, and
// any protocol whose security is TLS. Reality draws its own block and presents no
// certificate at all, so it never carries a certificate source.
export function tlsBlockShown(form: InboundInput): boolean {
  return hasSecurity(form.protocol) && (forcedTLS(form.protocol) || (form.settings.security || 'none') === 'tls');
}

// The one permission bit that allows a node-side certificate pair: the core that
// runs this inbound must report that its renderer reads the two files (v1.4.7
// section 3). An Agent that reports nothing grants nothing.
export function certificateFilesSupported(server: NodeView | undefined, core: CoreName | ''): boolean {
  return findCore(server, core)?.tlsCertificateFilesSupported === true;
}

export const TLS_SOURCE_LABELS: Record<string, string> = { self_signed: '自签名', file: '节点上的证书文件', paste: '粘贴证书', acme: '自动申请（Cloudflare）' };
// The sources the editor offers. A node-side pair is hidden (not disabled) unless
// the selected core reports the capability; the source the draft already holds
// stays listed, so the select never shows a value other than the saved one.
export function tlsSourceChoices(filesSupported: boolean, current: TLSSource = '', acmeUsable = false): { value: TLSSource; label: string }[] {
  const values: TLSSource[] = ['self_signed'];
  if (filesSupported) values.push('file');
  if (acmeUsable) values.push('acme');
  values.push('paste');
  if (current && !values.includes(current)) values.push(current);
  return values.map(value => ({ value, label: TLS_SOURCE_LABELS[value] || `${value}（现有值）` }));
}

// The certificate source the draft records, or the default for its state. A new
// inbound issues its own self-signed certificate when the node has a client
// address to put in it, and asks the Owner to paste one otherwise. Editing keeps
// the stored source; an empty source on a row written before migration 0136 means
// it holds pasted material, which is why it reads as such.
export function tlsSourceFor(state: EditorState, server?: NodeView): TLSSource {
  if (state.form.tlsSource) return state.form.tlsSource;
  if (state.original) {
    const stored = state.original.tlsSource;
    return stored === 'self_signed' || stored === 'file' || stored === 'acme' ? stored : 'paste';
  }
  return connectAddress(server) ? 'self_signed' : 'paste';
}

// The certificates an inbound may reference: the panel holds issued material for
// them, which is the case once notAfter is set (a renewing certificate keeps the
// material it already has).
export function usableCertificates(items: ManagedCertificate[]): ManagedCertificate[] {
  return items.filter(item => !!item.notAfter);
}
// Whether a certificate name list covers a host: exact, or one label under *.
export function certificateCoversHost(domains: string[], host: string): boolean {
  const name = host.trim().toLowerCase();
  return !!name && domains.some(domain => domain === name || (domain.startsWith('*.') && name.split('.').length > 1 && name.slice(name.indexOf('.') + 1) === domain.slice(2)));
}
// The hint shown when the address clients dial is not covered by the chosen
// certificate: the connect address has to be the certificate's domain.
export function acmeAddressHint(certificate: ManagedCertificate | undefined, address: string): string {
  if (!certificate) return '';
  const first = certificate.domains.find(domain => !domain.startsWith('*.')) || certificate.domains[0] || '';
  if (!address) return `节点还没有客户端连接地址；请在节点设置里把「客户端连接地址」设为 ${first}。`;
  if (certificateCoversHost(certificate.domains, address)) return '';
  return `当前客户端连接地址 ${address} 不在证书覆盖范围内；请在节点设置里把「客户端连接地址」改为证书域名（如 ${first}）。`;
}

// The message the panel itself returns when a self-signed certificate has no
// address to name; the editor shows the same words before it even sends the save.
export const SELF_SIGNED_REQUIRES_ADDRESS = '请先在节点设置里填写公网 IP 或客户端连接地址，再生成自签名证书';
// Where the installer puts certificate files, and how long one path may be
// (desiredstate.CertificateDirectory / MaxCertificatePathLength).
export const CERTIFICATE_DIRECTORY = '/etc/proxy-agent/certs/';
export const CERTIFICATE_PATH_LIMIT = 256;
// The node-side pair, judged exactly like desiredstate.validateCertificateFiles:
// two different absolute paths, already clean, inside the directory the installer
// creates. Nothing is normalized here, so one file has exactly one spelling.
export function certificatePathError(certificatePath = '', privateKeyPath = ''): string {
  const certificate = certificatePath.trim(), key = privateKeyPath.trim();
  if (!certificate) return '请填写证书文件路径';
  if (!key) return '请填写私钥文件路径';
  if (certificate === key) return '证书文件与私钥文件不能是同一个文件';
  for (const [label, value] of [['证书文件', certificate], ['私钥文件', key]] as const) {
    if (value.length > CERTIFICATE_PATH_LIMIT) return `${label}路径不能超过 ${CERTIFICATE_PATH_LIMIT} 个字符`;
    if (!value.startsWith('/')) return `${label}路径必须是绝对路径（以 / 开头）`;
    if (!value.startsWith(CERTIFICATE_DIRECTORY)) return `${label}路径必须位于 ${CERTIFICATE_DIRECTORY} 之下`;
    if (value.includes('//') || value.endsWith('/') || value.split('/').some(segment => segment === '.' || segment === '..')) {
      return `${label}路径必须已经规范化（不能含 . 或 .. 段或连续的 /）`;
    }
  }
  return '';
}

// Match the old list's merge rules, including absent QUIC transport, optional
// TLS replacements and the empty WireGuard transport. Advanced edits are then
// applied explicitly; simply opening a step must never add settings/defaults.
export function buildPayload(state: EditorState, server?: NodeView): InboundInput {
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
  if (p === 'vless') {
    settings.flow = input.flow || undefined;
    if (input.security === 'reality' && input.reality) settings.reality = publicReality(input.reality);
    else delete settings.reality;
    if (input.transport === 'xhttp' && input.xhttp) settings.xhttp = { host: input.xhttp.host || '', path: input.xhttp.path, mode: input.xhttp.mode };
    else delete settings.xhttp;
    if (settings.security === 'reality' || settings.xhttp) {
      delete settings.method; delete settings.quic; delete settings.wireguard; delete settings.forward;
    }
    if (settings.security !== 'tls') {
      delete settings.certificate_pem; delete settings.private_key_pem; delete settings.trusted_ca_pem; delete settings.server_name;
    }
    if (settings.transport !== 'ws') delete settings.path;
    if (settings.transport !== 'grpc') delete settings.service_name;
  } else {
    delete settings.reality; delete settings.xhttp;
  }
  // One certificate source at a time (v1.4.7 section 3): node-side files and pasted
  // PEM never travel together, and an empty pasted PEM keeps the stored pair, which
  // is the merge rule the panel has always applied. A relay listener presents the
  // panel-managed material it carries, so it names no source and no files.
  const source = tlsBlockShown(f) && !f.internalRelay ? tlsSourceFor(state, server) : '';
  if (source === 'acme') {
    delete settings.certificate_path; delete settings.private_key_path;
    delete settings.certificate_pem; delete settings.private_key_pem; delete settings.trusted_ca_pem;
  } else if (source === 'file') {
    settings.certificate_path = input.certificate_path?.trim();
    settings.private_key_path = input.private_key_path?.trim();
    delete settings.certificate_pem; delete settings.private_key_pem; delete settings.trusted_ca_pem;
  } else {
    delete settings.certificate_path; delete settings.private_key_path;
  }
  const payload: InboundInput = { name: state.origin === 'list' ? f.name.trim() : f.name, nodeId: f.nodeId, protocol: p, listen: f.listen, port: f.port,
    enabled: f.enabled, subscriptionVisible: f.internalRelay ? false : f.subscriptionVisible,
    internalRelay: f.internalRelay, defaultOutbound: f.core === 'gost' ? 'direct' : f.defaultOutbound, allowedUserIds: f.allowedUserIds, settings };
  // An empty core means "the node's current primary core" and is omitted from the
  // request, exactly like the Agent contract; a concrete core is sent explicitly.
  if (f.core) payload.core = f.core;
  if (source) {
    payload.tlsSource = source;
    if (source === 'self_signed' && f.regenerateCertificate) payload.regenerateCertificate = true;
    if (source === 'acme') payload.certificateId = f.certificateId;
  }
  if (p === 'vless' && settings.security === 'reality' && f.rotateRealityKey) payload.rotateRealityKey = true;
  return clone(payload);
}

export function serverEligible(server: NodeView): boolean {
  // Agent version is reported only after registration; pending rows have none.
  return server.enabled && !server.deletedAt && server.status !== 'pending' && !!server.agentVersion;
}
// The cores the server currently reports as enabled (K3b). A server without a
// core list (old Agent) yields exactly its one reported core.
export function coreOptions(server: NodeView | undefined, protocol?: string): { name: CoreName; label: string; version: string; primary: boolean }[] {
  if (!server) return [];
  return nodeCores(server)
    // GOST carries forward inbounds only, so it is offered for a forward and
    // never for a user protocol; the panel refuses the same combination.
    .filter(core => core.name !== 'gost' || protocol === undefined || protocol === 'forward')
    .map(core => ({ name: core.name, label: core.label, version: core.version, primary: core.primary }));
}
export function defaultCore(server: NodeView | undefined): CoreName | '' {
  return primaryCoreName(server);
}
// Why a core cannot be selected for this inbound, or '' when it can. Mirrors the
// panel's ValidateInboundCore so the editor refuses before the API does.
export function coreReason(server: NodeView | undefined, core: CoreName | ''): string {
  if (!server) return '请先选择服务器';
  const cores = coreOptions(server);
  if (!cores.length) return '尚未上报核心能力';
  if (!core) {
    const primary = cores.find(item => item.primary) || cores[0];
    if (primary && blockedCores(server).includes(primary.name)) return '主核心正在添加或移除中（core_pending），请等待新的能力上报';
    return primary ? '' : '主核心不可用';
  }
  if (!cores.some(item => item.name === core)) return `该服务器未上报启用 ${core} 核心`;
  if (!multiCoreSupported(server) && core !== defaultCore(server)) return '该 Agent 未协商多核心支持，只能使用当前主核心';
  if (blockedCores(server).includes(core)) return '该核心正在添加或移除中（core_pending），请等待 Agent 上报新的能力后再保存或发布';
  return '';
}
export function protocolReason(server: NodeView | undefined, protocol: string, core: CoreName | '' = ''): string {
  if (!server) return '请先选择服务器';
  const caps = server.capabilities;
  if (!caps?.core_name && !coreOptions(server).length) return '尚未上报核心能力';
  const reason = coreReason(server, core);
  if (reason) return reason;
  const selected = findCore(server, core);
  if (!selected) return '尚未上报核心能力';
  if (protocol === 'forward') {
    if (selected.forwardInboundSupported === false) return `${selected.label} 未报告支持端口转发入站`;
    return '';
  }
  if (selected.protocols?.some(p => p.protocol === protocol && p.inbound)) return '';
  if (selected.name === 'xray' && ['hysteria2', 'tuic'].includes(protocol)) return 'Xray 不支持此协议，需要支持 QUIC 的 sing-box';
  if (selected.name === 'sing-box' && protocol === 'wireguard') return 'sing-box 不支持 WireGuard 入站';
  return `${selected.label} 未报告支持 ${protocol} 入站（请检查核心版本与构建标签）`;
}
// The protocols the selected core actually reports for this inbound. An unavailable
// protocol is not offered at all; the stored protocol of an edited inbound stays
// listed (keepCurrent) so the select shows the real value instead of another one.
export function protocolChoices(server: NodeView | undefined, core: CoreName | '', protocols: string[], current: string, keepCurrent = false): string[] {
  return protocols.filter(protocol => !protocolReason(server, protocol, core) || (keepCurrent && protocol === current));
}
// The Agent reserves every enabled inbound's numeric port node-wide, regardless
// of TCP/UDP, transport or core, and the panel enforces the same rule. Keep this
// list for display only; it is no longer used to decide port conflicts.
export function listenNetworks(value: Pick<InboundInput, 'protocol' | 'settings'>): string[] {
  if (['hysteria2', 'tuic', 'wireguard'].includes(value.protocol)) return ['udp'];
  if (value.protocol === 'forward') return (value.settings?.forward?.network || 'tcp').split(',');
  if (['shadowsocks', 'socks5'].includes(value.protocol)) return ['tcp', 'udp'];
  return ['tcp'];
}
export function portConflict(form: InboundInput, inbounds: Inbound[], excludeId = ''): string {
  // Node-wide numeric rule (K3b): a second listener on the same port fails at
  // apply time even when one side is TCP and the other UDP, so the local check
  // must not let a TCP/UDP pair through. A GOST forward range reserves every
  // number it listens on, so both sides are expanded the way the backend does.
  const mine = forwardListenPorts(form.port, form.settings);
  const conflict = inbounds.find(i => i.nodeId === form.nodeId && i.id !== excludeId
    && forwardListenPorts(i.port, i.settings).some(port => mine.includes(port)));
  if (!conflict) return '';
  const taken = forwardListenPorts(conflict.port, conflict.settings).find(port => mine.includes(port));
  return `端口 ${taken ?? form.port} 已被「${conflict.name}」占用`;
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
  if (step === 3 || step === 5) {
    const role = internalRelaySettingsReason(f.internalRelay, s);
    if (role) return role;
  }
  if (step === 1 && (!server || !serverEligible(server))) return '请选择可用服务器';
  if (step === 2) {
    const core = coreReason(server, f.core || '');
    if (core) return core;
    const reason = protocolReason(server, f.protocol, f.core || '');
    if (reason) return reason;
    // A GOST-only option is never dropped silently: the selected core must be able
    // to render it, and moving the inbound to another core needs an explicit clear.
    if (f.protocol === 'forward' && gostOptions(s).length && !gostForwardReady(server, f.core || '')) {
      return '目标池、端口范围与限速只支持 GOST 核心（设置契约 2）；请先清除这些设置，或选择 GOST 核心';
    }
    if (!f.name.trim()) return '请输入节点名称';
    if (!Number.isInteger(f.port) || f.port < 1 || f.port > 65535) return '请输入有效的端口 (1-65535)';
    return portConflict(f, inbounds, state.original?.id);
  }
  if (step === 3 && hasSecurity(f.protocol)) {
    const support = serverSettingsReason(server, f.core || '', f.protocol, s);
    if (support) return support;
    const fields = validateServerFields(s);
    if (fields) return fields;
  }
  // A self-signed certificate or a node-side file pair brings its own material;
  // only a pasted certificate asks for PEM, and an internal relay's certificate is
  // the panel's own business. The two errors here are the panel's own words.
  if (step === 3 && tlsBlockShown(f)) {
    const source = tlsSourceFor(state, server);
    if (!f.internalRelay && source === 'file') {
      const paths = certificatePathError(s.certificate_path, s.private_key_path);
      if (paths) return paths;
    }
    if (!f.internalRelay && source === 'acme' && !f.certificateId) return '请选择一张已签发的证书';
    if (!f.internalRelay && source === 'self_signed' && !connectAddress(server)) return SELF_SIGNED_REQUIRES_ADDRESS;
    if (!f.internalRelay && source === 'paste') {
      if (!s.certificate_pem?.trim() && !state.original?.settings.certificate_pem) return '请输入 TLS 证书 PEM';
      if (!s.private_key_pem?.trim() && !state.original?.hasTlsKey) return '请输入 TLS 私钥 PEM';
    }
  }
  if (step === 4) {
    if (f.protocol === 'forward' && s.forward && !relayMode(s)) {
      const forward = s.forward;
      const pool = forward.targets?.length ? forward.targets : [{ host: forward.host, port: forward.port }];
      if (!forward.targets?.length && !forward.host.trim()) return '请输入目标主机';
      for (const target of pool) {
        if (!target.host?.trim()) return '请输入目标主机';
        if (!Number.isInteger(target.port) || target.port < 1 || target.port > 65535) return '请输入有效的目标端口 (1-65535)';
      }
      if (!['tcp', 'udp', 'tcp,udp'].includes(forward.network)) return '请选择有效的转发网络';
      if (forward.port_end !== undefined && (!Number.isInteger(forward.port_end) || forward.port_end < f.port || forward.port_end > 65535)) return '结束端口必须不小于监听端口且不超过 65535';
      if (forward.port_end !== undefined && forward.port_end - f.port + 1 > GOST_FORWARD_LIMITS.ports) return `端口范围最多 ${GOST_FORWARD_LIMITS.ports} 个端口`;
      for (const rate of [forward.bandwidth_in_bps, forward.bandwidth_out_bps]) {
        if (rate !== undefined && rate !== 0 && (!Number.isInteger(rate) || rate < 1024 || rate > GOST_FORWARD_LIMITS.bandwidth)) return '限速必须为 0 或 1024–1000000000000 字节/秒';
      }
      if (forward.max_connections !== undefined) {
        if (!Number.isInteger(forward.max_connections) || forward.max_connections < 0 || forward.max_connections > GOST_FORWARD_LIMITS.connections) return `连接上限必须为 0–${GOST_FORWARD_LIMITS.connections}`;
        if (forward.network === 'udp') return '连接上限只统计 TCP 连接，请改用在 TCP 或 TCP + UDP 转发';
      }
      if (forward.strategy !== undefined && !['round', 'fifo'].includes(forward.strategy)) return '请选择轮询或主备策略';
      if (forward.max_fails !== undefined && (!Number.isInteger(forward.max_fails) || forward.max_fails < 1 || forward.max_fails > GOST_FORWARD_LIMITS.fails)) return `失败次数必须为 1–${GOST_FORWARD_LIMITS.fails}`;
      if (forward.fail_timeout !== undefined && !/^[1-9][0-9]{0,3}s$/.test(forward.fail_timeout)) return '失败等待时间必须是 1s–3600s 的整数秒';
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
  if (safe.settings.reality) {
    const reality = safe.settings.reality as typeof safe.settings.reality & { private_key?: string };
    delete reality.private_key;
    if (!reality.public_key || safe.rotateRealityKey) reality.public_key = safe.rotateRealityKey ? '（保存时轮换，公钥将在保存后返回）' : '（保存时生成）';
  }
  return safe;
}

// Apply only this inbound's membership delta to freshly read complete lists.
export function withInboundMembership(ids: string[], inboundId: string, included: boolean): string[] {
  return included ? [...new Set([...ids, inboundId])] : ids.filter(id => id !== inboundId);
}
