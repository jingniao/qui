<template>
  <div class="space-y-2">
    <span class="text-xs font-medium text-ios-secondary">{{ label || '监听端口' }}</span>
    <div class="flex flex-wrap gap-2">
      <select :value="mode" class="form-input-base w-28 shrink-0" :disabled="disabled" @change="changeMode(($event.target as HTMLSelectElement).value as 'auto' | 'manual')"><option value="auto">自动</option><option value="manual">手动</option></select>
      <input :value="port" type="number" min="1" max="65535" :readonly="mode === 'auto'" :disabled="disabled" class="form-input-base min-w-0 flex-1 font-mono tabular-nums" aria-label="监听端口" @input="emit('update:port', ($event.target as HTMLInputElement).value === '' ? undefined : Number(($event.target as HTMLInputElement).value))" @blur="validate" />
      <button v-if="nodeId" type="button" class="shrink-0" :class="page ? 'ios-btn-secondary w-full' : 'ios-dialog-btn-secondary'" :disabled="disabled || busy" @click="mode === 'auto' ? allocate() : validate()">{{ mode === 'auto' ? '获取建议' : '检查端口' }}</button>
    </div>
    <p v-if="message" class="text-xs text-ios-red" role="alert">{{ message }}</p>
    <p v-else-if="checked" class="text-xs text-ios-green">端口可用，保存时再次校验</p>
    <p class="text-xs text-ios-tertiary">留空由服务端在保存时从端口范围分配，之后固定；获取建议不占用端口，保存时再次校验。</p>
  </div>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue';
import { portAPI, tunnelError } from '../api';
import { portValidation } from '../model';
const props = defineProps<{ nodeId: string; mode: 'auto' | 'manual'; port?: number; label?: string; owner?: { id: string; kind: string }; disabled?: boolean; page?: boolean }>();
const emit = defineEmits<{ 'update:mode': [value: 'auto' | 'manual']; 'update:port': [value: number | undefined] }>();
const busy = ref(false), message = ref(''), checked = ref(false);
let epoch = 0;
watch(() => [props.nodeId, props.port, props.mode], () => { epoch++; message.value = ''; checked.value = false; });
watch(() => props.nodeId, () => emit('update:port', undefined));
function changeMode(mode: 'auto' | 'manual') { emit('update:mode', mode); }
async function allocate() {
  busy.value = true;
  message.value = '';
  const node = props.nodeId, version = ++epoch;
  try { const result = await portAPI.allocate(node); if (version === epoch) emit('update:port', result.ports[0]); }
  catch (e) { if (version === epoch) message.value = tunnelError(e); }
  finally { busy.value = false; }
}
async function validate(): Promise<boolean> {
  const version = ++epoch, node = props.nodeId, port = props.port;
  if (node && props.mode === 'auto' && port === undefined) { message.value = ''; return true; }
  message.value = portValidation(node, port);
  if (message.value) return false;
  busy.value = true;
  try {
    const result = await portAPI.check(node, port!);
    if (version !== epoch) return false;
    message.value = portValidation(node, port, result, props.owner); checked.value = !message.value;
    return checked.value;
  } catch (e) { if (version === epoch) message.value = tunnelError(e); return false; }
  finally { busy.value = false; }
}
defineExpose({ validate });
</script>
