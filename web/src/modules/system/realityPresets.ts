import { shallowReadonly, shallowRef } from 'vue';
import { settingsAPI, type RealityPresets } from './settingsApi';

const presets = shallowRef<RealityPresets | null>(null);
const ready = shallowRef(false);
let pending: Promise<RealityPresets | null> | undefined;

// All editors share one read, including the explicitly requested empty fallback
// when the panel cannot supply presets. Settings saves replace the cached value.
export function loadRealityPresets(): Promise<RealityPresets | null> {
  return pending ||= settingsAPI.realityPresets().then(value => {
    presets.value = value;
    return value;
  }).catch(() => null).finally(() => { ready.value = true; });
}
export function rememberRealityPresets(value: RealityPresets) {
  presets.value = value;
  ready.value = true;
  pending = Promise.resolve(value);
}
export function useRealityPresets() {
  void loadRealityPresets();
  return { presets: shallowReadonly(presets), ready: shallowReadonly(ready) };
}
