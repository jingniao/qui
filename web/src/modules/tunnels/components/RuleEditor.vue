<template>
  <FormDrawer :model-value="true" :title="rule ? '编辑规则' : '创建规则'" width="xl" confirm-text="保存草稿" :loading="saving" @update:model-value="() => { if (!saving) emit('close'); }" @confirm="save">
    <fieldset :disabled="saving" class="space-y-4">
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">名称</span><input v-model="form.name" class="form-input-base" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">隧道</span><select v-model="form.tunnelId" class="form-input-base" @change="tunnelChanged"><option :value="null">直接转发</option><option v-for="tunnel in tunnels.filter(t => t.status !== 'pending_delete')" :key="tunnel.id" :value="tunnel.id">{{ tunnel.name }} · {{ routeLabel(tunnel) }}</option></select></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">入口服务器</span><select v-model="form.entryNodeId" :disabled="!!form.tunnelId" class="form-input-base"><option value="">请选择服务器</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label>
      <PortControl ref="portControl" v-model:mode="form.portMode" v-model:port="form.listenPort" :node-id="form.entryNodeId" :owner="rule ? { kind: 'rule', id: rule.id } : undefined" :disabled="saving" />
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">网络</span><select v-model="form.network" class="form-input-base"><option value="tcp">TCP</option><option value="udp">UDP</option><option value="both">TCP 与 UDP</option></select></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标方式</span><select v-model="targetMode" class="form-input-base" @change="form.subscriptionVisible = false"><option value="linked">关联入站</option><option value="manual">手动地址</option></select></label>
      <div v-if="targetMode === 'linked'" class="space-y-3">
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标服务器</span><select v-model="targetNodeId" class="form-input-base" @change="inboundId = ''"><option value="">请选择服务器</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label>
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标入站</span><select v-model="inboundId" class="form-input-base"><option value="">请选择入站</option><option v-for="inbound in targetInbounds" :key="inbound.id" :value="inbound.id">{{ inbound.name }} · {{ inbound.protocol.toUpperCase() }}</option></select></label>
        <p class="text-xs text-ios-secondary">派生目标：<span class="font-mono tabular-nums">{{ derivedTarget || '请选择目标入站' }}</span></p>
      </div>
      <div v-else class="grid grid-cols-2 gap-3">
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标主机</span><input v-model="manualHost" class="form-input-base font-mono tabular-nums" /></label>
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标端口</span><input v-model.number="manualPort" type="number" min="1" max="65535" class="form-input-base font-mono tabular-nums" /></label>
      </div>
      <label class="flex items-center gap-2 text-sm text-ios-secondary"><input v-model="form.subscriptionVisible" type="checkbox" :disabled="targetMode !== 'linked'" /> 对用户可见（仅关联入站）</label>
      <label class="flex items-center gap-2 text-sm text-ios-secondary"><input v-model="form.enabled" type="checkbox" /> 启用</label>
    </fieldset>
    <p v-if="error" class="text-sm text-ios-red" role="alert">{{ error }}</p>
    <p class="text-xs text-ios-tertiary">保存只更新草稿。关联目标地址由服务端派生；对用户可见的规则发布后才会加入订阅。</p>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import PortControl from './PortControl.vue';
import { tunnelRuleAPI, tunnelError, type Tunnel, type TunnelRule, type TunnelRuleInput } from '../api';
import { endpoint, routeLabel, validPort } from '../model';
import { showToast } from '../../../app/api';
const props = defineProps<{ rule: TunnelRule | null; tunnels: Tunnel[]; nodes: { id: string; name: string; publicIp: string }[]; inbounds: { id: string; nodeId: string; name: string; port: number; protocol: string }[] }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const form = reactive<TunnelRuleInput>({ name: props.rule?.name || '', entryNodeId: props.rule?.entryNodeId || '', portMode: props.rule?.portMode || 'auto', listenPort: props.rule?.listenPort,
  tunnelId: props.rule?.tunnelId || null, network: props.rule?.network || 'both', target: { mode: 'linked', inboundId: '' }, subscriptionVisible: props.rule?.subscriptionVisible || false, enabled: props.rule?.enabled ?? true, updatedAt: props.rule?.updatedAt });
const targetMode = ref<'linked' | 'manual'>(props.rule?.target.mode || 'linked');
const targetNodeId = ref(props.rule?.target.mode === 'linked' ? props.rule.target.nodeId : '');
const inboundId = ref(props.rule?.target.mode === 'linked' ? props.rule.target.inboundId : '');
const manualHost = ref(props.rule?.target.mode === 'manual' ? props.rule.target.host : ''), manualPort = ref(props.rule?.target.mode === 'manual' ? props.rule.target.port : 443);
const targetInbounds = computed(() => props.inbounds.filter(i => i.nodeId === targetNodeId.value));
const derivedTarget = computed(() => {
  const inbound = targetInbounds.value.find(i => i.id === inboundId.value);
  if (!inbound) return '';
  const lastNode = props.tunnels.find(t => t.id === form.tunnelId)?.hops.at(-1)?.nodeId;
  const host = lastNode === targetNodeId.value ? '127.0.0.1' : props.nodes.find(n => n.id === targetNodeId.value)?.publicIp;
  return host ? endpoint(host, inbound.port) : '目标服务器未配置公网地址';
});
function tunnelChanged() { if (form.tunnelId) form.entryNodeId = props.tunnels.find(t => t.id === form.tunnelId)?.hops[0]?.nodeId || ''; }
const portControl = ref<InstanceType<typeof PortControl>>(), saving = ref(false), error = ref('');
async function save() {
  if (saving.value) return;
  error.value = '';
  if (!form.name.trim() || !form.entryNodeId) { error.value = '请填写名称并选择入口服务器。'; return; }
  if (targetMode.value === 'linked' ? !targetInbounds.value.some(i => i.id === inboundId.value) : !manualHost.value.trim() || !validPort(manualPort.value)) { error.value = '请填写有效目标。'; return; }
  saving.value = true;
  try {
    if (!await portControl.value?.validate()) return;
    const input: TunnelRuleInput = { ...form, name: form.name.trim(), subscriptionVisible: targetMode.value === 'linked' && form.subscriptionVisible,
      target: targetMode.value === 'linked' ? { mode: 'linked', inboundId: inboundId.value } : { mode: 'manual', host: manualHost.value.trim(), port: manualPort.value } };
    if (props.rule) await tunnelRuleAPI.update(props.rule.id, input); else await tunnelRuleAPI.create(input);
    showToast('规则草稿已保存'); emit('saved');
  } catch (e) { error.value = tunnelError(e); } finally { saving.value = false; }
}
</script>
