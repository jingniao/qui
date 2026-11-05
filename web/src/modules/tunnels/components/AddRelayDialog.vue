<template>
  <FormDrawer :model-value="true" title="添加中转" width="xl" :show-footer="false" @update:model-value="() => { if (!saving) emit('close'); }">
    <p class="text-sm text-ios-secondary">保留直连接入；直连入口的用户可见性在入站编辑器中设置。</p>
    <div v-if="!created" class="space-y-4">
      <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">中转方式</span><select v-model="mode" class="form-input-base" :disabled="saving"><option value="tunnel">使用已有隧道</option><option value="canvas">新建代理中转方案</option></select></label>
      <fieldset v-if="mode === 'tunnel'" :disabled="saving || loading" class="space-y-4">
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">目标入站</span><select v-model="inboundId" class="form-input-base"><option value="">请选择入站</option><option v-for="i in destinations" :key="i.id" :value="i.id">{{ i.name }} · {{ i.protocol.toUpperCase() }} · {{ i.port }}</option></select></label>
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">隧道</span><select v-model="tunnelId" class="form-input-base"><option value="">请选择隧道</option><option v-for="t in available" :key="t.id" :value="t.id">{{ t.name }} · {{ routeLabel(t) }}</option></select></label>
        <PortControl ref="portControl" v-model:mode="portMode" v-model:port="listenPort" :node-id="entryNodeId" />
        <label class="block space-y-1.5"><span class="text-xs font-medium text-ios-secondary">名称</span><input v-model="name" class="form-input-base" /></label>
        <label class="flex gap-2 items-center text-sm text-ios-secondary"><input v-model="subscriptionVisible" type="checkbox" />对用户可见</label>
      </fieldset>
      <p v-else class="text-sm text-ios-secondary">以当前节点为入口创建服务器方案，然后在画布添加中转服务器与代理出口。</p>
    </div>
    <div v-else class="space-y-3">
      <p class="text-sm text-ios-green">规则草稿已保存，发布后生效。</p>
      <div v-if="created.shareLink" class="space-y-2"><p class="text-sm text-ios-label font-mono tabular-nums break-all">{{ created.shareLink }}</p><button class="ios-dialog-btn-secondary" @click="copy(created.shareLink)">复制链接</button></div>
      <p v-else class="text-sm text-ios-secondary">该格式无法表示</p>
    </div>
    <p v-if="loading" class="text-sm text-ios-tertiary">加载隧道中…</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <div class="flex justify-end gap-3">
      <button class="ios-dialog-btn-secondary" :disabled="saving" @click="emit('close')">关闭</button>
      <PublishFlow v-if="created" :tunnels="tunnels" :rules="[created.rule]" :preselect="{ tunnelIds: [], ruleIds: [created.rule.id] }" dialog-button @published="emit('saved')" />
      <button v-else class="ios-dialog-btn-primary" :disabled="saving || (mode === 'tunnel' && loading)" @click="save">{{ saving ? '处理中…' : mode === 'canvas' ? '创建并打开画布' : '保存草稿' }}</button>
    </div>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import PortControl from './PortControl.vue';
import PublishFlow from './PublishFlow.vue';
import { nodeRelayAPI, tunnelAPI, tunnelError, type Tunnel, type NodeRelay } from '../api';
import { routeLabel } from '../model';
import type { Inbound } from '../../proxy/api';
const props = defineProps<{ nodeId: string; nodeName: string; inbounds: Inbound[]; initialInboundId?: string; copy: (text: string) => void; createScheme: () => Promise<void> }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const mode = ref(props.nodeId ? 'tunnel' : 'canvas'), tunnels = ref<Tunnel[]>([]), loading = ref(true), saving = ref(false), error = ref('');
const destinations = computed(() => props.inbounds.filter(i => i.nodeId === props.nodeId && i.protocol !== 'forward' && !i.internalRelay));
const available = computed(() => tunnels.value.filter(t => t.status !== 'pending_delete' && t.hops[0]?.nodeId !== props.nodeId));
const inboundId = ref(props.initialInboundId || ''), tunnelId = ref(''), name = ref(''), subscriptionVisible = ref(true);
const portMode = ref<'auto' | 'manual'>('auto'), listenPort = ref<number>(), portControl = ref<InstanceType<typeof PortControl>>();
const created = ref<NodeRelay | null>(null);
const entryNodeId = computed(() => available.value.find(t => t.id === tunnelId.value)?.hops[0]?.nodeId || '');
watch(tunnelId, () => { name.value = `${props.nodeName} 经 ${available.value.find(t => t.id === tunnelId.value)?.name || '隧道'}`; });
onMounted(async () => { try { tunnels.value = (await tunnelAPI.list()).items; } catch (e) { error.value = tunnelError(e); } finally { loading.value = false; } });
async function save() {
  if (saving.value) return;
  error.value = ''; saving.value = true;
  try {
    if (mode.value === 'canvas') { await props.createScheme(); emit('close'); return; }
    if (!destinations.value.some(i => i.id === inboundId.value) || !entryNodeId.value || !name.value.trim()) { error.value = '请选择入站、隧道并填写名称。'; return; }
    if (!await portControl.value?.validate()) return;
    created.value = await nodeRelayAPI.create(props.nodeId, { tunnelId: tunnelId.value, inboundId: inboundId.value, portMode: portMode.value, ...(listenPort.value === undefined ? {} : { listenPort: listenPort.value }), name: name.value.trim(), subscriptionVisible: subscriptionVisible.value });
    emit('saved');
  } catch (e) { error.value = tunnelError(e); } finally { saving.value = false; }
}
</script>
