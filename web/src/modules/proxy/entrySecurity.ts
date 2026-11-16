import type { CoreName, NodeView } from '../nodes/api';
import type { CanvasEntrySecurity, ProtocolSettings } from './api';
import { serverSettingsReason, validateServerFields } from './components/node-editor/serverSettingsModel';
import type { RealityPreset, RealityPresets } from '../system/settingsApi';
import type { Inbound } from './api';
import { defaultRealityPreset } from '../system/realityPresetModel';

export function applyEntryRealityPreset(value: CanvasEntrySecurity, preset: RealityPreset, server: NodeView | undefined, core: CoreName | ''): CanvasEntrySecurity {
  const result = { ...value, realityTarget: preset.handshakeTarget, realityServerName: preset.serverNames[0] };
  if (!preset.transport && !preset.flow) return result;
  const transport = preset.transport || value.transport || 'tcp';
  const choice = entryTransportChoices(server, core, 'reality').find(item => item.transport === transport && item.flow === preset.flow);
  return choice ? { ...result, transport: choice.transport, flow: choice.flow } : result;
}

export function resolveEntryReality(value: CanvasEntrySecurity, presets: RealityPresets | null, exit: Inbound | undefined, server: NodeView | undefined, core: CoreName | ''): CanvasEntrySecurity {
  const preset = defaultRealityPreset(presets);
  // Bound inputs keep their transport and Flow as well as their public fields.
  const initial = preset && !value.realityTarget && !value.realityServerName ? applyEntryRealityPreset(value, preset, server, core) : value;
  return { ...initial,
    realityTarget: initial.realityTarget || preset?.handshakeTarget || exit?.settings.reality?.handshake_target || '',
    realityServerName: initial.realityServerName || preset?.serverNames[0] || exit?.settings.reality?.server_names?.[0] || '' };
}

export function entrySettings(value: CanvasEntrySecurity): ProtocolSettings {
  return { security: value.security || 'none', transport: value.transport || 'tcp', flow: value.flow || '',
    ...(value.security === 'reality' ? { reality: { public_key: '', handshake_target: value.realityTarget || '', server_names: [value.realityServerName || ''], short_ids: [] } } : {}),
    ...(value.transport === 'xhttp' ? { xhttp: { mode: 'auto', path: '/' } } : {}) };
}
export const entryTransports = [
  { value: 'vision', label: 'TCP（Vision）', transport: 'tcp', flow: 'xtls-rprx-vision' },
  { value: 'tcp', label: 'TCP', transport: 'tcp', flow: '' },
  { value: 'xhttp', label: 'xhttp', transport: 'xhttp', flow: '' }
] as const;
export function entryTransportChoices(server: NodeView | undefined, core: CoreName | '', security: CanvasEntrySecurity['security']) {
  return entryTransports.filter(choice => !serverSettingsReason(server, core, 'vless', entrySettings({ security, transport: choice.transport, flow: choice.flow }), false));
}
export function entrySecurityChoices(server: NodeView | undefined, core: CoreName | '') {
  return [
    { value: 'reality' as const, label: 'Reality（推荐）' },
    { value: 'tls' as const, label: 'TLS（自签名证书）' },
    { value: 'none' as const, label: '不加密（不推荐）' }
  ].filter(choice => entryTransportChoices(server, core, choice.value).length);
}
export function defaultEntrySecurity(server: NodeView | undefined, core: CoreName | ''): CanvasEntrySecurity {
  const security = entrySecurityChoices(server, core)[0]!.value;
  const choice = entryTransportChoices(server, core, security)[0]!;
  return { security, transport: choice.transport, flow: choice.flow };
}
export function entryPlaintextWarning(name: string, protocol: string, security?: string): string {
  return ['http', 'socks5'].includes(protocol) || protocol === 'vless' && (!security || security === 'none')
    ? `入口「${name}」使用不加密的 ${protocol.toUpperCase()}，容易被识别和干扰，不建议在国内直连使用` : '';
}
export function entrySecurityError(server: NodeView | undefined, core: CoreName | '', value: CanvasEntrySecurity): string {
  const settings = entrySettings(value);
  const reason = serverSettingsReason(server, core, 'vless', settings);
  if (reason) return reason;
  // Only public inputs belong to this form. The panel generates keys and Short ID.
  if (settings.reality) settings.reality.short_ids = ['00'];
  return validateServerFields(settings);
}
export function entrySecurityLabel(value: CanvasEntrySecurity): string {
  const security = value.security === 'reality' ? 'Reality' : value.security === 'tls' ? 'TLS' : '不加密';
  const transport = value.transport === 'xhttp' ? 'xhttp' : value.flow ? 'TCP（Vision）' : 'TCP';
  return `VLESS · ${security} · ${transport}`;
}
