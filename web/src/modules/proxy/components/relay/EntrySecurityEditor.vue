<template>
  <section v-if="protocol === 'vless'" class="space-y-3 text-sm" aria-label="安全设置">
    <h4 class="text-base font-semibold">安全设置</h4>
    <template v-if="readonly">
      <p>{{ entrySecurityLabel(modelValue) }}</p>
      <p v-if="modelValue.security === 'reality'" class="break-words">握手目标：{{ modelValue.realityTarget }} · 服务器名称：{{ modelValue.realityServerName }}</p>
      <p class="text-xs text-ios-secondary">已发布入口的安全设置请在「节点列表」编辑该入站后发布</p>
    </template>
    <template v-else>
      <label class="block space-y-1.5 text-ios-secondary">安全方式
        <select :value="modelValue.security" class="form-input-base" @change="changeSecurity(($event.target as HTMLSelectElement).value)">
          <option v-for="choice in securities" :key="choice.value" :value="choice.value">{{ choice.label }}</option>
        </select>
      </label>
      <label class="block space-y-1.5 text-ios-secondary">传输方式
        <select :value="modelValue.flow ? 'vision' : modelValue.transport || 'tcp'" class="form-input-base" @change="changeTransport(($event.target as HTMLSelectElement).value)">
          <option v-for="choice in transports" :key="choice.value" :value="choice.value">{{ choice.label }}</option>
        </select>
      </label>
      <template v-if="modelValue.security === 'reality'">
        <label class="block space-y-1.5 text-ios-secondary">Reality 预设（可选）
          <select :value="''" class="form-input-base" @change="pickPreset"><option value="">选择预设填入参数</option><option v-for="preset in presetChoices" :key="preset.value" :value="preset.value">{{ preset.label }}</option></select>
        </label>
        <label class="block space-y-1.5 text-ios-secondary">握手目标 (host:port)<input :value="modelValue.realityTarget" required class="form-input-base" placeholder="host:443" @input="update({ realityTarget: ($event.target as HTMLInputElement).value })" /></label>
        <label class="block space-y-1.5 text-ios-secondary">服务器名称<input :value="modelValue.realityServerName" required class="form-input-base" @input="update({ realityServerName: ($event.target as HTMLInputElement).value })" /></label>
        <p class="text-xs text-ios-secondary">密钥与 Short ID 在创建时自动生成</p>
      </template>
    </template>
    <p v-if="warning" class="text-xs text-ios-orange">{{ warning }}</p>
  </section>
  <p v-else-if="warning" class="text-xs text-ios-orange">{{ warning }}</p>
</template>

<script setup lang="ts">
import { computed, watch } from 'vue';
import type { CoreName, NodeView } from '../../../nodes/api';
import type { CanvasEntrySecurity, Inbound } from '../../api';
import { applyEntryRealityPreset, resolveEntryReality, defaultEntrySecurity, entryPlaintextWarning, entrySecurityChoices, entrySecurityLabel, entryTransportChoices } from '../../entrySecurity';
import { useRealityPresets } from '../../../system/realityPresets';
import { realityPresetChoices } from '../../../system/realityPresetModel';
const props = defineProps<{ modelValue: CanvasEntrySecurity; protocol: string; core: CoreName | ''; server?: NodeView; name: string; exit?: Inbound; readonly?: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [value: CanvasEntrySecurity] }>();
const { presets, ready: presetsReady } = useRealityPresets();
const presetChoices = computed(() => realityPresetChoices(presets.value));
const securities = computed(() => entrySecurityChoices(props.server, props.core));
const transports = computed(() => entryTransportChoices(props.server, props.core, props.modelValue.security));
const warning = computed(() => entryPlaintextWarning(props.name, props.protocol, props.modelValue.security));
function update(value: CanvasEntrySecurity) { emit('update:modelValue', { ...props.modelValue, ...value }); }
function changeSecurity(security: string) {
  const value = security as CanvasEntrySecurity['security'];
  const transport = entryTransportChoices(props.server, props.core, value)[0]!;
  const next = { security: value, transport: transport.transport, flow: transport.flow };
  emit('update:modelValue', value === 'reality' ? resolveEntryReality({ ...props.modelValue, ...next }, presets.value, props.exit, props.server, props.core) : next);
}
function pickPreset(event: Event) {
  const select = event.target as HTMLSelectElement;
  const preset = presets.value?.items.find(item => item.id === select.value);
  if (preset) emit('update:modelValue', applyEntryRealityPreset(props.modelValue, preset, props.server, props.core));
  else if (select.value) update({ realityTarget: select.value });
  select.value = '';
}
function changeTransport(value: string) {
  const choice = transports.value.find(choice => choice.value === value)!;
  update({ transport: choice.transport, flow: choice.flow });
}
watch(() => [presetsReady.value, props.protocol, props.core, props.server?.id, props.modelValue.security, props.modelValue.transport, props.modelValue.flow, props.readonly, securities.value, transports.value], () => {
  if (!presetsReady.value || props.readonly || props.protocol !== 'vless') return;
  if (props.modelValue.security === undefined || !securities.value.some(choice => choice.value === props.modelValue.security)) {
    changeSecurity(defaultEntrySecurity(props.server, props.core).security!);
  } else if (!transports.value.some(choice => choice.transport === (props.modelValue.transport || 'tcp') && choice.flow === (props.modelValue.flow || ''))) changeSecurity(props.modelValue.security || 'none');
  else if (props.modelValue.security === 'reality') fillReality();
}, { immediate: true });
watch(() => props.exit?.id, () => {
  if (presetsReady.value && !props.readonly && props.modelValue.security === 'reality') fillReality();
});
function fillReality() {
  const next = resolveEntryReality(props.modelValue, presets.value, props.exit, props.server, props.core);
  if (next.realityTarget !== (props.modelValue.realityTarget || '') || next.realityServerName !== (props.modelValue.realityServerName || '')) update(next);
}
</script>
