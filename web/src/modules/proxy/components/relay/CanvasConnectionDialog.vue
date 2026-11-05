<template>
  <FormDrawer :model-value="true" :title="legacy ? '将迁移到隧道管理' : connection ? '编辑代理中转' : '添加代理中转'" :show-footer="false" @update:model-value="() => { if (!saving) emit('close'); }">
    <p class="text-sm text-ios-secondary">{{ source.name }} → {{ target.name }}</p>
    <p v-if="legacy" class="text-sm text-ios-orange">将迁移到隧道管理；当前连线只读，可从画布删除。</p>
    <fieldset v-else :disabled="saving" class="space-y-4">
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">连接类型</span><select v-model="kind" class="form-input-base"><option value="proxy_chain">代理链</option><option v-if="!client && source.resourceType === 'server' && target.resourceType === 'server'" value="tunnel">加密中转</option></select></label>
      <label v-if="!client" class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">中转协议</span><select v-model="protocol" class="form-input-base"><option value="tls">TLS</option><option value="wireguard">WireGuard</option></select></label>
      <p class="text-xs text-ios-tertiary">内部监听端口在目标服务器的属性面板中设置。</p>
      <label class="flex gap-2 text-sm text-ios-secondary"><input v-model="tcp" type="checkbox" :disabled="!supportsTcp" />TCP<span v-if="!supportsTcp" class="text-xs text-ios-tertiary">此外部出站不支持 TCP</span></label>
      <label class="flex gap-2 text-sm text-ios-secondary"><input v-model="udp" type="checkbox" :disabled="!supportsUdp" />UDP<span v-if="!supportsUdp" class="text-xs text-ios-tertiary">此外部出站不支持 UDP</span></label>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <div class="flex justify-end gap-3">
      <button class="ios-dialog-btn-secondary" :disabled="saving" @click="emit('close')">取消</button>
      <button v-if="connection" class="ios-dialog-btn-danger" :disabled="saving" @click="submit(true)">删除连线</button>
      <button v-if="!legacy" class="ios-dialog-btn-primary" :disabled="saving" @click="submit(false)">保存</button>
    </div>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import FormDrawer from '../../../../components/FormDrawer.vue';
import type { CanvasConnection, CanvasNodeInstance } from '../../proxyNodes';
import type { Outbound } from '../../api';
const props = defineProps<{ connection?: CanvasConnection; source: CanvasNodeInstance; target: CanvasNodeInstance; outbounds?: Outbound[]; client: boolean; persist: (value: CanvasConnection | null) => Promise<void> }>();
const emit = defineEmits<{ close: [] }>();
const legacy = computed(() => props.connection?.type === 'port_forward' || (props.connection?.tunnelProtocol && !['tls', 'wireguard'].includes(props.connection.tunnelProtocol)));
const kind = ref<'proxy_chain' | 'tunnel'>(props.connection?.type === 'tunnel' ? 'tunnel' : 'proxy_chain');
const protocol = ref<'tls' | 'wireguard'>(props.connection?.tunnelProtocol === 'wireguard' ? 'wireguard' : 'tls');
const externalOutbound = computed(() => props.outbounds?.find(o => `outbound:${o.id}` === props.target.resourceId));
const supportsTcp = computed(() => !props.target.resourceId?.startsWith('outbound:') || externalOutbound.value?.supportTcp === true);
const supportsUdp = computed(() => !props.target.resourceId?.startsWith('outbound:') || (externalOutbound.value?.supportUdp === true && externalOutbound.value.protocol !== 'http'));
const tcp = ref(supportsTcp.value && (props.connection?.supportTcp ?? true)), udp = ref(supportsUdp.value && (props.connection?.supportUdp ?? true)), saving = ref(false), error = ref('');
async function submit(remove: boolean) {
  if (saving.value) return;
  if (legacy.value && !remove) return;
  tcp.value = tcp.value && supportsTcp.value;
  udp.value = udp.value && supportsUdp.value;
  if (!remove && !tcp.value && !udp.value) { error.value = '请至少选择 TCP 或 UDP'; return; }
  saving.value = true; error.value = '';
  try {
    await props.persist(remove ? null : { ...props.connection, id: props.connection?.id || `conn-${Date.now()}`, sourceInstanceId: props.source.instanceId, targetInstanceId: props.target.instanceId, type: props.client ? 'proxy_chain' : kind.value, tunnelProtocol: props.client ? undefined : protocol.value, supportTcp: tcp.value, supportUdp: udp.value, status: 'valid' });
    emit('close');
  } catch (e) { error.value = (e as Error).message; } finally { saving.value = false; }
}
</script>
