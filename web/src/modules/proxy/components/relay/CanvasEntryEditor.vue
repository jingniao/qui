<template>
  <section class="space-y-3 border-t border-ios-separator pt-3 text-sm" aria-label="用户入口绑定">
    <h3 class="text-base font-semibold">用户入口</h3>
    <p class="text-xs text-ios-secondary">{{ binding ? '已绑定入口；修改后点击应用入口设置。' : '尚未登记入口绑定。选择新建或复用入口，应用后随方案发布。' }}</p>
    <label class="block space-y-1.5 text-ios-secondary">入口服务器
      <select v-model="form.nodeId" class="form-input-base" @change="resetForServer">
        <option v-for="server in serverOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
      </select>
    </label>
    <label class="block space-y-1.5 text-ios-secondary">入口方式
      <select v-model="form.mode" class="form-input-base" @change="resetForMode">
        <option value="create">新建独立入口</option>
        <option v-if="reuseOptions.length || form.mode === 'reuse'" value="reuse">复用现有入口</option>
      </select>
    </label>
    <template v-if="form.mode === 'create'">
      <label v-if="protocols.length" class="block space-y-1.5 text-ios-secondary">入口协议与核心
        <select v-model="protocolKey" class="form-input-base">
          <option value="">请选择入口协议</option>
          <option v-for="choice in protocols" :key="choice.core + ':' + choice.protocol" :value="choice.core + ':' + choice.protocol">{{ choice.protocol.toUpperCase() }} · {{ choice.core }}</option>
        </select>
      </label>
      <p v-else class="text-ios-orange">入口服务器尚未报告可用的用户入站协议。</p>
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
    </template>
    <template v-else>
      <label class="block space-y-1.5 text-ios-secondary">现有用户入站
        <select v-model="form.inboundId" class="form-input-base">
          <option value="">请选择入口入站</option>
          <option v-for="inbound in reuseOptions" :key="inbound.id" :value="inbound.id">{{ inbound.name }} · {{ inbound.protocol.toUpperCase() }} · {{ inbound.port }}</option>
        </select>
      </label>
      <p v-if="reused" class="text-xs text-ios-secondary">当前默认出站：{{ reused.defaultOutbound }}。发布时登记恢复目标，保留现有分组和端口。</p>
    </template>
    <p v-if="error" role="alert" class="text-ios-orange">{{ error }}</p>
    <button v-else type="button" class="ios-btn-primary w-full" @click="apply">应用入口设置</button>
    <p class="text-xs text-ios-secondary">已发布入口的服务器、方式、协议与端口受方案归属保护；不允许的修改会在链路检查中显示原因。</p>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue';
import type { NodeView } from '../../../nodes/api';
import { primaryCoreName } from '../../../nodes/coreSummary';
import type { NodeGroup } from '../../../nodes/groupsApi';
import type { CanvasEntryBinding, Inbound } from '../../api';
import { quickRelayProtocolChoices } from '../../quickRelay';
import { isUserEntry } from '../../userEntries';
import PortControl from '../../../tunnels/components/PortControl.vue';

const props = defineProps<{ nodeId: string; binding?: CanvasEntryBinding; servers: NodeView[]; inbounds: Inbound[]; groups: NodeGroup[]; exitProtocol: string; schemeId: string; bindingStateReady: boolean; bindingStateError?: string; materializedInboundId?: string }>();
const emit = defineEmits<{ apply: [nodeId: string, binding: CanvasEntryBinding] }>();
const form = reactive({ nodeId: props.nodeId, mode: 'create' as 'create' | 'reuse', inboundId: '', protocol: '', core: '', portMode: 'auto' as 'auto' | 'manual', port: undefined as number | undefined, groupMode: 'exit_groups' as CanvasEntryBinding['groupMode'], groupIds: [] as string[] });
const serverOptions = computed(() => props.servers.filter(server => server.enabled && !server.deletedAt));
const server = computed(() => serverOptions.value.find(item => item.id === form.nodeId));
const protocols = computed(() => quickRelayProtocolChoices(server.value));
const groupOptions = computed(() => props.groups.filter(group => group.enabled && ['subscription', 'mixed'].includes(group.purpose)));
const actualGroups = computed(() => props.groups.filter(group => group.inboundIds.includes(props.materializedInboundId!)));
const reuseOptions = computed(() => props.inbounds.filter(inbound => inbound.nodeId === form.nodeId && inbound.enabled && isUserEntry(inbound) && protocols.value.some(choice => choice.protocol === inbound.protocol && choice.core === (inbound.core || primaryCoreName(server.value)))));
const reused = computed(() => reuseOptions.value.find(inbound => inbound.id === form.inboundId));
const protocolKey = computed({ get: () => form.protocol ? `${form.core || primaryCoreName(server.value)}:${form.protocol}` : '', set: value => {
  const choice = protocols.value.find(item => `${item.core}:${item.protocol}` === value);
  form.protocol = choice?.protocol || ''; form.core = choice?.core || '';
} });
function resetForMode() {
  form.inboundId = ''; form.portMode = 'auto'; form.port = undefined; form.groupIds = [];
  form.groupMode = form.mode === 'reuse' ? 'none' : 'exit_groups';
  const choice = form.mode === 'create' ? protocols.value.find(item => item.protocol === props.exitProtocol) : undefined;
  form.protocol = choice?.protocol || ''; form.core = choice?.core || '';
}
function resetForServer() { form.mode = 'create'; resetForMode(); }
watch(() => props.binding, binding => {
  form.nodeId = props.nodeId;
  if (!binding) { form.mode = 'create'; resetForMode(); return; }
  form.mode = binding.mode; form.inboundId = binding.inboundId || ''; form.protocol = binding.protocol || ''; form.core = binding.core || '';
  form.portMode = binding.portMode; form.port = binding.port; form.groupMode = binding.groupMode; form.groupIds = [...(binding.groupIds || [])];
}, { immediate: true });
const error = computed(() => {
  if (props.bindingStateError) return props.bindingStateError;
  if (!props.bindingStateReady) return '正在读取入口绑定与实际分组…';
  if (!server.value) return '请选择启用的入口服务器。';
  if (form.mode === 'reuse') return reused.value ? '' : '请选择本服务器支持的普通用户入站。';
  if (!protocols.value.some(choice => `${choice.core}:${choice.protocol}` === protocolKey.value)) return '请选择本服务器支持的入口协议。';
  if (form.portMode === 'manual' && (!Number.isInteger(form.port) || form.port! < 1 || form.port! > 65535)) return '手动入口端口须为 1–65535。';
  if (!props.materializedInboundId && form.groupMode === 'explicit' && (!form.groupIds.length || form.groupIds.some(id => !groupOptions.value.some(group => group.id === id)))) return '请选择启用的订阅或混合用途分组。';
  return '';
});
function apply() {
  if (error.value) return;
  const binding: CanvasEntryBinding = form.mode === 'reuse'
    ? { mode: 'reuse', inboundId: form.inboundId, portMode: 'auto', groupMode: 'none' }
    : { mode: 'create', protocol: form.protocol, ...(form.core ? { core: form.core } : {}), portMode: form.portMode, ...(form.portMode === 'manual' ? { port: form.port } : {}), groupMode: form.groupMode, ...(form.groupMode === 'explicit' ? { groupIds: [...form.groupIds] } : {}) };
  if (props.materializedInboundId && binding.mode === 'create' && props.binding?.mode === 'create') {
    binding.groupMode = props.binding.groupMode;
    delete binding.groupIds;
    if (props.binding.groupIds) binding.groupIds = [...props.binding.groupIds];
  }
  emit('apply', form.nodeId, binding);
}
</script>
