<template>
  <FormDrawer :model-value="true" title="添加中转" width="2xl" @update:model-value="close">
    <div v-if="created" class="space-y-3 text-sm" role="status">
      <p class="text-ios-green">已创建 {{ createdName }}，发布中</p>
      <p class="text-ios-secondary">请在画布中查看各服务器的发布结果。</p>
      <p v-for="warning in created.warnings" :key="warning" class="text-ios-orange">{{ warning }}</p>
      <p v-if="created.publication.batchId" class="text-xs text-ios-secondary break-all">发布批次：{{ created.publication.batchId }}</p>
    </div>
    <template v-else>
      <fieldset :disabled="saving" class="space-y-4 text-sm">
        <label class="block space-y-1.5 text-ios-secondary">出口用户入站
          <select v-model="form.exitInboundId" class="form-input-base">
            <option value="">请选择出口入站</option>
            <option v-for="inbound in exitOptions" :key="inbound.id" :value="inbound.id">{{ serverName(inbound.nodeId) }} · {{ inbound.name }} · {{ inbound.protocol.toUpperCase() }}</option>
          </select>
        </label>
        <label class="block space-y-1.5 text-ios-secondary">入口服务器
          <select v-model="form.entryNodeId" class="form-input-base">
            <option value="">请选择入口服务器</option>
            <option v-for="server in entryOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
          </select>
        </label>
        <label v-if="protocols.length" class="block space-y-1.5 text-ios-secondary">入口协议
          <select v-model="protocolKey" class="form-input-base">
            <option value="">请选择入口协议</option>
            <option v-for="choice in protocols" :key="choice.core + ':' + choice.protocol" :value="choice.core + ':' + choice.protocol">{{ choice.protocol.toUpperCase() }} · {{ choice.core }}</option>
          </select>
        </label>
        <p v-else-if="form.entryNodeId" class="text-ios-orange">入口服务器尚未报告可用的用户入站协议。</p>
        <label class="block space-y-1.5 text-ios-secondary">中间服务器（可选，最多 {{ MAX_QUICK_RELAY_MIDDLE_NODES }} 台）
          <select v-model="middleNodeId" class="form-input-base">
            <option value="">不经过中间服务器</option>
            <option v-for="server in middleOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
          </select>
        </label>
        <details class="rounded-xl border border-ios-border p-3">
          <summary class="cursor-pointer text-ios-secondary">高级选项</summary>
          <div class="mt-3 space-y-3">
            <label class="block space-y-1.5 text-ios-secondary">新入口分组
              <select v-model="form.groupMode" class="form-input-base">
                <option value="exit_groups">加入出口入站所在分组</option>
                <option value="explicit">指定分组</option>
                <option value="none">不加入分组</option>
              </select>
            </label>
            <div v-if="form.groupMode === 'explicit'" class="space-y-2">
              <label v-for="group in groupOptions" :key="group.id" class="flex items-center gap-2 text-ios-secondary">
                <input v-model="form.groupIds" type="checkbox" :value="group.id" />{{ group.name }}
              </label>
              <p v-if="!groupOptions.length" class="text-ios-orange">暂无可选的订阅或混合用途分组。</p>
            </div>
          </div>
        </details>
      </fieldset>
      <p v-if="!ready" class="text-sm text-ios-secondary">请选择不同的入口服务器和出口入站、入口协议，以及指定的分组。</p>
      <p v-if="planning" role="status" class="text-sm text-ios-secondary">正在规划中转…</p>
      <section v-if="plan" class="space-y-3 text-sm" aria-label="创建与发布预览">
        <h4 class="text-base font-semibold">创建与发布预览</h4>
        <p class="text-ios-label break-words">经过服务器：{{ routeName }}</p>
        <div v-for="entry in plan.entries" :key="entry.instanceId" class="rounded-xl bg-ios-bg p-3 space-y-1">
          <p class="font-semibold break-words">新建入口：{{ entry.name }}</p>
          <p>{{ entry.protocol.toUpperCase() }} · <span class="font-mono tabular-nums">{{ entry.port }}</span></p>
          <p class="text-ios-secondary">受影响分组：{{ entry.groups?.map(group => group.name).join('、') || '不加入分组' }}</p>
        </div>
        <div v-for="server in plan.servers" :key="server.nodeId" class="space-y-1">
          <p class="font-semibold">{{ server.name }}</p>
          <p v-for="(item, index) in server.creates" :key="'create-' + index" class="text-ios-secondary break-words">新建 {{ item.name }} · {{ item.protocol }}<span v-if="item.port"> · {{ item.port }}</span><span v-if="item.target"> → {{ item.target }}</span></p>
          <p v-for="(item, index) in server.updates" :key="'update-' + index" class="text-ios-secondary break-words">更新 {{ item.name }}</p>
          <p v-for="(item, index) in server.removes" :key="'remove-' + index" class="text-ios-orange break-words">移除 {{ item.name }}</p>
        </div>
        <p v-for="warning in plan.warnings" :key="warning" class="text-ios-orange">{{ warning }}</p>
        <p v-for="(conflict, index) in plan.conflicts" :key="index" class="text-ios-red">{{ serverName(conflict.nodeId) }}：{{ conflict.with }}<span v-if="conflict.port">（端口 {{ conflict.port }}）</span><span v-if="conflict.reason"> · {{ conflict.reason }}</span></p>
        <p v-if="!plan.ok" role="alert" class="text-ios-red">{{ plan.message }}</p>
        <p v-if="plan.ok" class="text-xs text-ios-secondary">确认后按出口到入口的顺序发布，实际端口以创建结果为准。</p>
      </section>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    </template>
    <template #footer>
      <button type="button" class="ios-dialog-btn-secondary" :disabled="saving" @click="close">关闭</button>
      <button v-if="created" type="button" class="ios-dialog-btn-primary" @click="viewCanvas">在画布中查看</button>
      <button v-else-if="ready && plan?.ok && !planning" type="button" class="ios-dialog-btn-primary" :disabled="saving" @click="create">{{ saving ? '创建并发布中…' : '创建并发布' }}</button>
    </template>
  </FormDrawer>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import FormDrawer from '../../../components/FormDrawer.vue';
import type { NodeView } from '../../nodes/api';
import type { NodeGroup } from '../../nodes/groupsApi';
import type { Inbound } from '../api';
import { isUserEntry } from '../userEntries';
import { buildQuickRelayInput, quickRelayProtocolChoices, MAX_QUICK_RELAY_MIDDLE_NODES, type QuickRelayForm, type QuickRelayPlan, type QuickRelayResult } from '../quickRelay';
import { quickRelayAPI } from '../quickRelayApi';

const props = defineProps<{ servers: NodeView[]; inbounds: Inbound[]; groups: NodeGroup[]; initialExitInboundId?: string; initialEntryNodeId?: string }>();
const emit = defineEmits<{ close: []; created: [] }>();
const router = useRouter();
const form = reactive<QuickRelayForm>({ entryNodeId: props.initialEntryNodeId || '', exitInboundId: props.initialExitInboundId || '', middleNodeIds: [], entryProtocol: '', entryCore: '', groupMode: 'exit_groups', groupIds: [] });
const saving = ref(false), planning = ref(false), error = ref('');
const plan = ref<QuickRelayPlan | null>(null), created = ref<QuickRelayResult | null>(null), createdName = ref('');
const liveServers = computed(() => props.servers.filter(server => server.enabled && !server.deletedAt));
const exitOptions = computed(() => props.inbounds.filter(inbound => isUserEntry(inbound) && inbound.protocol !== 'forward' && inbound.enabled && liveServers.value.some(server => server.id === inbound.nodeId)));
const exit = computed(() => exitOptions.value.find(inbound => inbound.id === form.exitInboundId));
const entryOptions = computed(() => liveServers.value.filter(server => server.id !== exit.value?.nodeId && quickRelayProtocolChoices(server).length));
const middleOptions = computed(() => liveServers.value.filter(server => server.id !== form.entryNodeId && server.id !== exit.value?.nodeId));
const groupOptions = computed(() => props.groups.filter(group => group.enabled && ['subscription', 'mixed'].includes(group.purpose)));
const protocols = computed(() => quickRelayProtocolChoices(entryOptions.value.find(server => server.id === form.entryNodeId)));
const protocolKey = computed({ get: () => form.entryProtocol ? `${form.entryCore}:${form.entryProtocol}` : '', set: value => {
  const choice = protocols.value.find(item => `${item.core}:${item.protocol}` === value);
  form.entryProtocol = choice?.protocol || ''; form.entryCore = choice?.core || '';
} });
const middleNodeId = computed({ get: () => form.middleNodeIds[0] || '', set: value => { form.middleNodeIds = value ? [value] : []; } });
function serverName(id: string) { return props.servers.find(server => server.id === id)?.name || id; }
const routeName = computed(() => [form.entryNodeId, ...form.middleNodeIds, exit.value?.nodeId].filter((id): id is string => !!id).map(serverName).join(' → '));
watch([() => form.entryNodeId, () => form.exitInboundId], () => {
  if (form.entryNodeId === exit.value?.nodeId) form.entryNodeId = '';
  form.middleNodeIds = form.middleNodeIds.filter(id => middleOptions.value.some(server => server.id === id));
  const choice = protocols.value.find(item => item.protocol === exit.value?.protocol);
  form.entryProtocol = choice?.protocol || ''; form.entryCore = choice?.core || '';
}, { immediate: true, flush: 'sync' });
const ready = computed(() => !!exit.value && entryOptions.value.some(server => server.id === form.entryNodeId) &&
  protocols.value.some(item => item.protocol === form.entryProtocol && item.core === form.entryCore) &&
  form.middleNodeIds.every(id => middleOptions.value.some(server => server.id === id)) &&
  (form.groupMode !== 'explicit' || (form.groupIds.length > 0 && form.groupIds.every(id => groupOptions.value.some(group => group.id === id)))));

let requestId = crypto.randomUUID();
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
// Invalidate synchronously: an old successful plan must never enable submission
// for a newly edited form, even during the debounce or an in-flight response.
watch(() => JSON.stringify(buildQuickRelayInput(form, 'preview')), () => {
  const current = ++generation;
  clearTimeout(timer);
  requestId = crypto.randomUUID();
  plan.value = null; error.value = ''; planning.value = ready.value;
  if (!ready.value) return;
  const input = buildQuickRelayInput(form, requestId);
  timer = setTimeout(async () => {
    try {
      const result = await quickRelayAPI.plan(input);
      if (current === generation) plan.value = result;
    } catch (e) {
      if (current === generation) error.value = (e as Error).message;
    } finally {
      if (current === generation) planning.value = false;
    }
  }, 500);
}, { immediate: true, flush: 'sync' });
onBeforeUnmount(() => { ++generation; clearTimeout(timer); });
function close() { if (!saving.value) emit('close'); }
async function create() {
  if (saving.value || planning.value || !ready.value || !plan.value?.ok) return;
  saving.value = true; error.value = '';
  try {
    created.value = await quickRelayAPI.create(buildQuickRelayInput(form, requestId));
    createdName.value = routeName.value;
    emit('created');
  } catch (e) {
    error.value = (e as Error).message;
  } finally { saving.value = false; }
}
async function viewCanvas() {
  await router.push({ path: '/proxy/relay', query: { schemeId: created.value!.schemeId } });
  emit('close');
}
</script>
