import type { RealityPreset, RealityPresets } from './settingsApi';
import type { CoreName, NodeView } from '../nodes/api';
import type { ProtocolSettings } from '../proxy/api';
import { REALITY_HANDSHAKE_PRESETS, serverSettingsReason } from '../proxy/components/node-editor/serverSettingsModel';

export function realityPresetChoices(value: RealityPresets | null) {
  return value?.items.length ? value.items.map(item => ({ value: item.id, label: item.name }))
    : REALITY_HANDSHAKE_PRESETS.map(target => ({ value: target, label: target }));
}
export function defaultRealityPreset(value: RealityPresets | null): RealityPreset | undefined {
  return value?.items.find(item => item.id === value.defaultId);
}
export function applyRealityPreset(settings: ProtocolSettings, preset: RealityPreset, server: NodeView | undefined, core: CoreName | ''): ProtocolSettings {
  const result = { ...settings, reality: { ...settings.reality!, handshake_target: preset.handshakeTarget, server_names: [...preset.serverNames] } };
  if (!preset.transport && !preset.flow) return result;
  const next = { ...result, transport: preset.transport || settings.transport || 'tcp', flow: preset.flow };
  if (serverSettingsReason(server, core, 'vless', next, false)) return result;
  if (next.transport === 'xhttp') next.xhttp ||= { mode: 'auto', path: '/' };
  return next;
}
