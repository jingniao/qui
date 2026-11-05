<template>
  <FormDrawer :model-value="true" title="添加外部代理出口" confirm-text="创建并放入画布" :loading="saving" @update:model-value="() => { if (!saving) emit('close'); }" @confirm="save">
    <fieldset :disabled="saving" class="space-y-4">
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">所属节点（出口前一跳）</span><select v-model="form.nodeId" class="form-input-base"><option value="">请选择服务器</option><option v-for="n in nodes" :key="n.id" :value="n.id">{{ n.name }}</option></select></label>
      <p class="text-xs text-ios-tertiary">请选择连接此外部出口的前一跳服务器；出站创建在该服务器上。</p>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">名称</span><input v-model="form.name" class="form-input-base" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">协议</span><select v-model="form.protocol" class="form-input-base" @change="protocolChanged"><option v-for="p in ['socks5','http','shadowsocks','trojan','vless']" :key="p" :value="p">{{ p.toUpperCase() }}</option></select></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">地址</span><input v-model="form.address" class="form-input-base font-mono tabular-nums" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">端口</span><input v-model.number="form.port" type="number" min="1" max="65535" class="form-input-base font-mono tabular-nums" /></label>
      <label v-if="['socks5','http'].includes(form.protocol)" class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">认证用户名</span><input v-model="form.username" autocomplete="off" class="form-input-base" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">{{ form.protocol === 'vless' ? 'UUID' : '密码 / 密钥' }}</span><input v-model="form.secret" type="password" autocomplete="new-password" class="form-input-base" /></label>
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">安全方式</span><select v-model="form.settings.security" class="form-input-base"><option v-if="form.protocol !== 'trojan'" value="none">None</option><option value="tls">TLS</option></select></label>
      <label v-if="form.settings.security === 'tls'" class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">TLS 服务器名称</span><input v-model="form.settings.server_name" class="form-input-base" /></label>
      <label v-if="form.protocol === 'shadowsocks'" class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">加密方式</span><select v-model="form.settings.method" class="form-input-base"><option>chacha20-ietf-poly1305</option><option>aes-256-gcm</option><option>aes-128-gcm</option></select></label>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
  </FormDrawer>
</template>
<script setup lang="ts">
import { reactive, ref } from 'vue';
import FormDrawer from '../../../../components/FormDrawer.vue';
import { proxyAPI, type Outbound, type OutboundInput } from '../../api';
import { validPort } from '../../../tunnels/model';
const props = defineProps<{ nodes: { id: string; name: string }[]; initialNodeId: string }>();
const emit = defineEmits<{ close: []; created: [outbound: Outbound] }>();
const form = reactive<OutboundInput>({ name: '', nodeId: props.initialNodeId, protocol: 'socks5', address: '', port: 1080, enabled: true, targetNodeId: '', targetInboundId: '', username: '', secret: '', settings: { transport: 'tcp', security: 'none' }, supportTcp: true, supportUdp: true, failurePolicy: 'Block', backupOutboundId: '', probeUrl: 'https://www.gstatic.com/generate_204', probeInterval: 30 });
const saving = ref(false), error = ref('');
function protocolChanged() { form.settings = { transport: 'tcp', security: form.protocol === 'trojan' ? 'tls' : 'none', ...(form.protocol === 'shadowsocks' ? { method: 'chacha20-ietf-poly1305' } : {}) }; form.supportUdp = form.protocol !== 'http'; form.username = ''; form.secret = ''; }
async function save() {
  if (saving.value) return;
  error.value = '';
  if (!form.nodeId || !form.name.trim() || !form.address.trim() || !validPort(form.port) || (!['socks5','http'].includes(form.protocol) && !form.secret.trim())) { error.value = '请选择所属节点，填写名称、有效地址端口及协议凭据。'; return; }
  saving.value = true;
  try { const outbound = await proxyAPI.saveOutbound(null, { ...form, name: form.name.trim(), address: form.address.trim() }); form.secret = ''; emit('created', outbound); }
  catch (e) { error.value = (e as Error).message; } finally { saving.value = false; }
}
</script>
