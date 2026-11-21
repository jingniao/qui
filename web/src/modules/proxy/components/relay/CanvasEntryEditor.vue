<template>
  <section class="space-y-3 border-t border-ios-separator pt-3 text-sm" aria-label="用户入口绑定">
    <h3 class="text-base font-semibold">用户入口</h3>
    <p class="text-xs text-ios-secondary">{{ binding ? '已绑定入口；修改后点击应用入口设置。' : '尚未登记入口绑定。新建独立入口，应用后随方案发布。' }}</p>
    <label class="block space-y-1.5 text-ios-secondary">入口服务器
      <select v-model="form.nodeId" class="form-input-base" @change="resetForServer">
        <option v-for="server in serverOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
      </select>
    </label>
    <label v-if="protocols.length" class="block space-y-1.5 text-ios-secondary">入口协议与核心
      <select v-model="protocolKey" class="form-input-base">
        <option value="">请选择入口协议</option>
        <option v-for="choice in protocols" :key="choice.core + ':' + choice.protocol" :value="choice.core + ':' + choice.protocol">{{ choice.protocol.toUpperCase() }} · {{ choice.core }}</option>
      </select>
    </label>
    <p v-else class="text-ios-orange">入口服务器尚未报告可用的用户入站协议。</p>
    <EntrySecurityEditor :model-value="displaySecurity" :protocol="form.protocol" :core="selectedCore" :server="server" :name="server?.name || '新入口'" :exit="exitInbound" :readonly="!!materializedInboundId" @update:model-value="value => { security = value; }" />
    <PortControl :key="form.nodeId" :node-id="form.nodeId" :mode="form.portMode" :port="form.port" label="用户入口端口" page :owner="{ kind: 'canvas', id: schemeId }" @update:mode="value => { form.portMode = value; form.port = undefined; }" @update:port="value => { form.port = value; if (value !== undefined) form.portMode = 'manual'; }" />
    <div v-if="materializedInboundId" class="space-y-1 text-xs text-ios-secondary">
      <p>当前实际分组：{{ actualGroups.map(group => group.name).join('、') || '未加入分组' }}</p>
      <p>入口已创建，重新发布保留实际分组。请在<RouterLink to="/proxy/groups" class="text-ios-blue">节点分组</RouterLink>中修改。</p>
    </div>
    <template v-else-if="bindingStateReady">
    <label class="block space-y-1.5 text-ios-secondary">新入口分组
      <select v-model="form.groupMode" class="form-input-base" @change="form.groupIds = []">
        <option value="exit_groups">加入出口入站所在分组</option>
        <option value="explicit">指定分组</option>
        <option value="none">不加入分组</option>
      </select>
    </label>
    <div v-if="form.groupMode === 'explicit'" class="space-y-2">
      <label v-for="group in groupOptions" :key="group.id" class="flex items-center gap-2 text-ios-secondary"><input v-model="form.groupIds" type="checkbox" :value="group.id" />{{ group.name }}</label>
      <p v-if="!groupOptions.length" class="text-ios-orange">暂无可选的订阅或混合用途分组。</p>
    </div>
    </template>
    <p v-if="error" role="alert" class="text-ios-orange">{{ error }}</p>
    <button v-else type="button" class="ios-btn-primary w-full" @click="apply">应用入口设置</button>
    <p class="text-xs text-ios-secondary">已发布入口的服务器、协议与端口受方案归属保护；不允许的修改会在链路检查中显示原因。</p>
  </section>
</template>

<script setup lang="ts">
import { nodeHasUserCore } from '../../../nodes/coreSummary';
import { computed, reactive, ref, watch } from 'vue';
import type { CoreName, NodeView } from '../../../nodes/api';
import { primaryCoreName } from '../../../nodes/coreSummary';
import type { NodeGroup } from '../../../nodes/groupsApi';
import type { CanvasEntryBinding, CanvasEntrySecurity, Inbound } from '../../api';
import { entrySecurityError } from '../../entrySecurity';
import EntrySecurityEditor from './EntrySecurityEditor.vue';
import { quickRelayProtocolChoices } from '../../quickRelay';
import PortControl from '../../../tunnels/components/PortControl.vue';

const props = defineProps<{ nodeId: string; binding?: CanvasEntryBinding; servers: NodeView[]; inbounds: Inbound[]; groups: NodeGroup[]; exitProtocol: string; exitInbound?: Inbound; schemeId: string; bindingStateReady: boolean; bindingStateError?: string; materializedInboundId?: string }>();
const emit = defineEmits<{ apply: [nodeId: string, binding: CanvasEntryBinding] }>();
const form = reactive({ nodeId: props.nodeId, protocol: '', core: '', portMode: 'auto' as 'auto' | 'manual', port: undefined as number | undefined, groupMode: 'exit_groups' as CanvasEntryBinding['groupMode'], groupIds: [] as string[] });
const serverOptions = computed(() => props.servers.filter(server => server.enabled && !server.deletedAt && nodeHasUserCore(server)));
const server = computed(() => serverOptions.value.find(item => item.id === form.nodeId));
const security = ref<CanvasEntrySecurity>({});
const selectedCore = computed(() => (form.core || '') as CoreName | '');
const displaySecurity = computed<CanvasEntrySecurity>(() => {
  const inbound = props.inbounds.find(item => item.id === props.materializedInboundId);
  if (!inbound) return security.value;
  const settings = inbound.settings || {};
  return { security: settings.security as CanvasEntrySecurity['security'], transport: settings.transport as CanvasEntrySecurity['transport'], flow: settings.flow as CanvasEntrySecurity['flow'], realityTarget: settings.reality?.handshake_target, realityServerName: settings.reality?.server_names?.[0] };
});
const protocols = computed(() => quickRelayProtocolChoices(server.value));
const groupOptions = computed(() => props.groups.filter(group => group.enabled && ['subscription', 'mixed'].includes(group.purpose)));
const actualGroups = computed(() => props.groups.filter(group => group.inboundIds.includes(props.materializedInboundId!)));
const protocolKey = computed({ get: () => form.protocol ? `${form.core || primaryCoreName(server.value)}:${form.protocol}` : '', set: value => {
  const choice = protocols.value.find(item => `${item.core}:${item.protocol}` === value);
  form.protocol = choice?.protocol || ''; form.core = choice?.core || '';
} });
function resetForMode() {
  security.value = {};
  form.portMode = 'auto'; form.port = undefined; form.groupIds = [];
  form.groupMode = 'exit_groups';
  const choice = protocols.value.find(item => item.protocol === props.exitProtocol);
  form.protocol = choice?.protocol || ''; form.core = choice?.core || '';
}
function resetForServer() { resetForMode(); }
watch(() => props.binding, binding => {
  form.nodeId = props.nodeId;
  if (!binding) { resetForMode(); return; }
  form.protocol = binding.protocol || ''; form.core = binding.core || '';
  security.value = { security: binding.security, transport: binding.transport, flow: binding.flow, realityTarget: binding.realityTarget, realityServerName: binding.realityServerName };
  form.portMode = binding.portMode; form.port = binding.port; form.groupMode = binding.groupMode; form.groupIds = [...(binding.groupIds || [])];
}, { immediate: true });
const error = computed(() => {
  if (props.bindingStateError) return props.bindingStateError;
  if (!props.bindingStateReady) return '正在读取入口绑定与实际分组…';
  if (!server.value) return '请选择启用的入口服务器。';
  if (!protocols.value.some(choice => `${choice.core}:${choice.protocol}` === protocolKey.value)) return '请选择本服务器支持的入口协议。';
  if (form.portMode === 'manual' && (!Number.isInteger(form.port) || form.port! < 1 || form.port! > 65535)) return '手动入口端口须为 1–65535。';
  if (!props.materializedInboundId && form.protocol === 'vless') {
    const reason = entrySecurityError(server.value, selectedCore.value, security.value);
    if (reason) return reason;
  }
  if (!props.materializedInboundId && form.groupMode === 'explicit' && (!form.groupIds.length || form.groupIds.some(id => !groupOptions.value.some(group => group.id === id)))) return '请选择启用的订阅或混合用途分组。';
  return '';
});
function apply() {
  if (error.value) return;
  const binding: CanvasEntryBinding = { mode: 'create', protocol: form.protocol, ...(form.core ? { core: form.core } : {}), portMode: form.portMode, ...(form.portMode === 'manual' ? { port: form.port } : {}), groupMode: form.groupMode, ...(form.groupMode === 'explicit' ? { groupIds: [...form.groupIds] } : {}) };
  if (!props.materializedInboundId && form.protocol === 'vless') Object.assign(binding, Object.fromEntries(Object.entries(security.value).filter(([, value]) => value !== undefined)));
  if (props.materializedInboundId && props.binding) {
    binding.groupMode = props.binding.groupMode;
    delete binding.groupIds;
    if (props.binding.groupIds) binding.groupIds = [...props.binding.groupIds];
  }
  emit('apply', form.nodeId, binding);
}
</script>
