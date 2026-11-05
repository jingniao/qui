<template>
  <FormDrawer :model-value="true" :title="tunnel ? '编辑隧道' : '创建隧道'" width="xl" confirm-text="保存草稿" :loading="saving" @update:model-value="() => { if (!saving) emit('close'); }" @confirm="save">
    <fieldset :disabled="saving" class="space-y-4">
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">名称</span><input v-model="form.name" class="form-input-base" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">传输协议</span><input value="TLS（GOST 双向认证）" readonly class="form-input-base text-ios-secondary" /></label>
      <div v-for="(hop, i) in form.hops" :key="hop.role" class="space-y-3 rounded-xl border border-ios-border p-4">
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">{{ roleNames[hop.role] }}</span><select v-model="hop.nodeId" class="form-input-base"><option value="">请选择服务器</option><option v-for="node in nodes" :key="node.id" :value="node.id" :disabled="form.hops.some((h, index) => index !== i && h.nodeId === node.id)">{{ node.name }}</option></select></label>
        <PortControl v-if="hop.role !== 'entry'" :ref="el => setPortRef(hop.role, el)" v-model:mode="hop.portMode" v-model:port="hop.listenPort" :node-id="hop.nodeId" :owner="tunnel ? { kind: 'tunnel', id: tunnel.id } : undefined" :disabled="saving" />
        <button v-if="hop.role === 'mid'" type="button" class="ios-dialog-btn-secondary" @click="form.hops.splice(i, 1)">移除中间服务器</button>
      </div>
      <button v-if="form.hops.length === 2" type="button" class="ios-dialog-btn-secondary" @click="form.hops.splice(1, 0, { role: 'mid', nodeId: '', portMode: 'auto' })">添加中间服务器</button>
    </fieldset>
    <section v-if="linkedRules.length" class="space-y-2"><h4 class="text-[15px] font-semibold text-ios-label">关联规则</h4><p v-for="rule in linkedRules" :key="rule.id" class="text-sm text-ios-secondary">{{ rule.name }} · <span class="font-mono tabular-nums">{{ rule.entryNodeName }}:{{ rule.listenPort }}</span></p></section>
    <p v-if="error" class="text-sm text-ios-red" role="alert">{{ error }}</p>
    <p class="text-xs text-ios-tertiary">保存只更新草稿。隧道服务器需要 GOST 核心，发布时会校验能力与端口。</p>
  </FormDrawer>
</template>
<script setup lang="ts">
import { reactive, ref } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import PortControl from './PortControl.vue';
import { tunnelAPI, tunnelError, type Tunnel, type TunnelInput, type TunnelRule } from '../api';
import { showToast } from '../../../app/api';
const props = defineProps<{ tunnel: Tunnel | null; nodes: { id: string; name: string }[]; linkedRules: TunnelRule[] }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const roleNames = { entry: '入口服务器', mid: '中间服务器', exit: '出口服务器' };
const form = reactive<TunnelInput>({ name: props.tunnel?.name || '', transport: 'tls', updatedAt: props.tunnel?.updatedAt,
  hops: props.tunnel ? props.tunnel.hops.map(h => ({ role: h.role, nodeId: h.nodeId, portMode: h.portMode, listenPort: h.listenPort ?? undefined, connectHost: h.connectHost })) : [{ role: 'entry', nodeId: '', portMode: 'auto' }, { role: 'exit', nodeId: '', portMode: 'auto' }] });
const ports = new Map<string, InstanceType<typeof PortControl>>();
function setPortRef(role: string, el: unknown) { if (el) ports.set(role, el as InstanceType<typeof PortControl>); else ports.delete(role); }
const saving = ref(false), error = ref('');
async function save() {
  if (saving.value) return;
  error.value = '';
  if (!form.name.trim() || form.hops.some(h => !h.nodeId)) { error.value = '请填写名称并选择每一跳服务器。'; return; }
  if (new Set(form.hops.map(h => h.nodeId)).size !== form.hops.length) { error.value = '每一跳必须使用不同服务器。'; return; }
  saving.value = true;
  try {
    const checks = await Promise.all([...ports.values()].map(p => p.validate()));
    if (checks.some(ok => !ok)) return;
    const input = { ...form, name: form.name.trim(), hops: form.hops.map(h => h.role === 'entry' ? { role: h.role, nodeId: h.nodeId, portMode: h.portMode, connectHost: h.connectHost } : { ...h }) };
    if (props.tunnel) await tunnelAPI.update(props.tunnel.id, input); else await tunnelAPI.create(input);
    showToast('隧道草稿已保存'); emit('saved');
  } catch (e) { error.value = tunnelError(e); } finally { saving.value = false; }
}
</script>
