<template>
  <FormDrawer :model-value="true" :title="state.original ? '编辑节点' : '新建节点'" width="2xl" @update:model-value="close">
    <nav aria-label="节点编辑步骤" class="grid grid-cols-3 sm:grid-cols-6 gap-2">
      <button v-for="item in steps" :key="item.number" type="button" :disabled="busy || (!state.original && item.number > reached) || (item.number === 3 && !securityStep)" :aria-current="step === item.number ? 'step' : undefined" :class="['rounded-xl p-2 text-xs transition-colors active:scale-95 disabled:opacity-40', step === item.number ? 'bg-ios-blue text-white' : 'bg-ios-bg text-ios-secondary']" @click="go(item.number)">{{ item.symbol }} {{ item.label }}</button>
    </nav>
    <p v-if="loading" role="status" class="text-sm text-ios-secondary">正在读取服务器与入站…</p>
    <div v-else-if="loadError" class="space-y-2"><p role="alert" class="text-ios-red text-sm">{{ loadError }}</p><button class="ios-btn-secondary" @click="load">重试</button></div>
    <fieldset v-else :disabled="busy || !!savedId" class="space-y-4">
      <div v-if="step === 1" class="space-y-3">
        <p v-if="state.original" class="text-xs text-ios-secondary">编辑时服务器已锁定。</p>
        <label v-for="server in eligibleServers" :key="server.id" class="block rounded-2xl border border-ios-border p-4 cursor-pointer">
          <span class="flex items-center gap-2 text-sm text-ios-label"><input v-model="f.nodeId" :value="server.id" type="radio" :disabled="!!state.original" />{{ server.name }}</span>
          <span class="block mt-2 text-xs text-ios-secondary font-mono">{{ server.capabilities?.core_name || '—' }} {{ server.capabilities?.core_version || '—' }}</span>
          <span class="block mt-2 text-xs text-ios-secondary">{{ server.capabilities?.protocols?.filter(p => p.inbound).map(p => p.protocol).join(' · ') || '尚未上报支持的入站协议' }}</span>
        </label>
        <p v-if="!eligibleServers.length" class="text-sm text-ios-secondary">没有可用的已登记服务器，请先登记服务器。</p>
      </div>
      <div v-if="step === 2" class="space-y-4">
        <div class="grid grid-cols-2 sm:grid-cols-3 gap-3">
          <button v-for="protocol in protocols" :key="protocol" type="button" :disabled="!!state.original || !!protocolReason(selectedServer, protocol)" :title="protocolReason(selectedServer, protocol)" :class="['text-left rounded-2xl border p-3 active:scale-95 disabled:opacity-50', f.protocol === protocol ? 'border-ios-blue bg-ios-blue/10' : 'border-ios-border bg-ios-card']" @click="changeProtocol(state, protocol)">
            <span class="block text-sm text-ios-label">{{ protocol }}</span><span v-if="protocolReason(selectedServer, protocol)" class="block mt-1 text-xs text-ios-secondary">{{ protocolReason(selectedServer, protocol) }}</span>
          </button>
        </div>
        <label class="block text-xs text-ios-secondary">节点名称<input v-model="f.name" class="form-input-base mt-2" @input="autoName = false" /></label>
        <div>
          <div class="flex items-center justify-between">
            <label class="block text-xs text-ios-secondary">监听端口</label>
            <span v-if="serverPortRange" class="text-[11px] text-ios-blue font-mono font-medium">
              服务器分配范围: {{ serverPortRange.start }} - {{ serverPortRange.end }}
            </span>
          </div>
          <p v-if="!serverPortRange" class="text-[11px] text-ios-secondary">未设置服务器范围；随机端口使用编辑器默认范围 10000–65535。</p>
          <input v-model.number="f.port" type="number" min="1" max="65535" class="form-input-base mt-1.5 font-mono" />
        </div>
        <button type="button" class="ios-btn-secondary" @click="pickPort">随机端口</button>
        <p aria-live="polite" :class="['text-xs', conflict ? 'text-ios-red' : 'text-ios-secondary']">{{ conflict || '入站列表中无端口冲突（含画布方案入站）；不检测主机其他进程。' }}</p>
      </div>
      <NodeProtocolFields v-if="step === 3 || step === 4" :state="state" :step="step" />
      <NodeAdvancedFields v-if="step === 5" :form="f" :editing="!!state.original" :outbounds="outbounds" :groups="groups" :chains="chains" :users="users" />
      <section v-if="step === 5 && isOwner" class="panel-card p-4 space-y-3">
        <h3 class="text-sm font-semibold text-ios-label">所属分组</h3>
        <label v-for="group in memberGroups" :key="group.id" class="flex items-center gap-2 text-sm text-ios-label">
          <input v-model="selectedGroupIds" type="checkbox" :value="group.id" class="accent-ios-blue" />{{ group.name }}
          <span v-if="group.purpose === 'relay' || group.purpose === 'egress'" class="text-xs text-ios-secondary">（不授予用户接入）</span>
        </label>
        <p class="text-xs text-ios-secondary">新入站不会自动加入分组。分组保存后影响订阅筛选；运行配置需发布后由服务器确认生效。</p>
      </section>
      <div v-if="step === 6" class="space-y-4 text-sm">
        <p class="text-ios-label">{{ selectedServer?.name || '—' }} · {{ f.name }} · {{ f.protocol.toUpperCase() }} :{{ f.port }}</p>
        <p class="text-ios-secondary">{{ f.listen }} · {{ f.enabled ? '启用' : '停用' }} · {{ f.subscriptionVisible && !f.internalRelay ? '订阅可见' : '订阅隐藏' }} · {{ f.internalRelay ? '内部中转' : '用户接入' }}</p>
        <p class="text-xs text-ios-secondary">默认出站：{{ f.defaultOutbound }}；用户范围：{{ f.allowedUserIds.length ? f.allowedUserIds.map(id => users.find(u => u.id === id)?.username || id).join('、') : '已获授权、且分组包含此入站的用户' }}</p>
        <p class="text-xs text-ios-secondary">所属分组：{{ selectedGroupIds.map(id => memberGroups.find(g => g.id === id)?.name || '—').join('、') || '—' }}。非空 allowedUserIds 只进一步缩小上述分组授权范围，不会单独授予接入权限。</p>
        <p class="text-xs text-ios-orange">保存草稿后运行配置不变。发布会包含该服务器的全部待发布修改；内部中转请先发布目标服务器。</p>
        <div class="overflow-x-auto rounded-2xl border border-ios-border">
          <table class="w-full text-xs text-left"><thead class="bg-ios-bg text-ios-secondary"><tr><th class="p-3">字段</th><th class="p-3">保存前</th><th class="p-3">保存后</th></tr></thead>
            <tbody><tr v-for="row in diff" :key="row.field" :class="row.changed ? 'bg-ios-blue/10' : ''"><td class="p-3 text-ios-secondary">{{ row.field }}</td><td class="p-3 font-mono whitespace-pre-wrap break-all">{{ row.before }}</td><td class="p-3 font-mono whitespace-pre-wrap break-all">{{ row.after }}</td></tr></tbody>
          </table>
        </div>
        <p class="text-xs text-ios-secondary">证书与密钥仅显示是否设置。</p>
      </div>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <p v-if="result" role="status" class="text-sm text-ios-secondary">{{ result }}</p>
    <template #footer>
      <button v-if="savedId" class="ios-dialog-btn-secondary" @click="emit('close')">关闭</button>
      <template v-else>
        <button :disabled="busy || step === 1 || loading" class="ios-dialog-btn-secondary" @click="back">上一步</button>
        <button v-if="step < 6" :disabled="busy || loading || !!loadError" class="ios-dialog-btn-primary" @click="next">下一步</button>
        <template v-else>
          <button :disabled="busy || loading || !!loadError" class="ios-dialog-btn-secondary" @click="save(false)">保存草稿</button>
          <button :disabled="busy || loading || !!loadError" class="ios-dialog-btn-primary" @click="save(true)">{{ busy ? '处理中…' : '保存并发布' }}</button>
        </template>
      </template>
    </template>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import FormDrawer from '../../../../components/FormDrawer.vue';
import { isOwner } from '../../../../app/auth';
import { proxyAPI, choices, loadProtocolSupport, protocolOptions, type Inbound, type Outbound, type OutboundGroup, type ProxyChain } from '../../api';
import { nodeAPI, type NodeView } from '../../../nodes/api';
import { getNodePortRange, loadNodeMeta } from '../../../nodes/nodeMeta';
import { groupAPI, freshGroup, type NodeGroup } from '../../../nodes/groupsApi';
import { groupInput } from '../../lineGroups';
import { userAPI, type ProxyUser } from '../../../users/api';
import { deploymentAPI } from '../../../system/deploymentApi';
import NodeProtocolFields from './NodeProtocolFields.vue';
import NodeAdvancedFields from './NodeAdvancedFields.vue';
import { withInboundMembership, buildPayload, createEditor, changeProtocol, hasSecurity, portConflict, previewPayload, protocolReason, randomPort, serverEligible, validateStep, type EditorOrigin } from './nodeEditorModel';
const props = withDefaults(defineProps<{ serverId?: string; inbound?: Inbound | null; origin?: EditorOrigin }>(), { serverId: '', inbound: null, origin: 'list' });
const emit = defineEmits<{ close: []; saved: [] }>();
const route = useRoute();
const state = reactive(createEditor(props.serverId || String(route.query.server || ''), props.inbound, props.origin));
const f = computed(() => state.form);
const servers = ref<NodeView[]>([]), inbounds = ref<Inbound[]>([]), users = ref<ProxyUser[]>([]);
const outbounds = ref<Outbound[]>([]), groups = ref<OutboundGroup[]>([]), chains = ref<ProxyChain[]>([]);
const memberGroups = ref<NodeGroup[]>([]), selectedGroupIds = ref<string[]>([]);
const initialGroupIds = ref<string[]>([]);
const step = ref(props.inbound ? 6 : 1), reached = ref(props.inbound ? 6 : 1), autoName = ref(!props.inbound);
const loading = ref(true), busy = ref(false), error = ref(''), loadError = ref(''), result = ref(''), savedId = ref('');
const steps = ['服务器', '协议与端口', '传输与安全', '协议专属', '高级', '确认'].map((label, i) => ({ label, number: i + 1, symbol: '①②③④⑤⑥'[i] }));
const protocols = computed(() => [...new Set([...protocolOptions.inbound, 'forward'])]);
const selectedServer = computed(() => servers.value.find(s => s.id === f.value.nodeId));
const eligibleServers = computed(() => servers.value.filter(serverEligible));
const securityStep = computed(() => hasSecurity(f.value.protocol));
const conflict = computed(() => portConflict(f.value, inbounds.value, state.original?.id));
const serverPortRange = computed(() => getNodePortRange(f.value.nodeId));
const diff = computed(() => {
  const after = previewPayload(buildPayload(state));
  const before = state.original ? previewPayload(createEditor('', state.original, 'server').form) : {} as Record<string, unknown>;
  return Object.entries(after).map(([field, value]) => ({ field, before: JSON.stringify(before[field], null, 2) ?? '—', after: JSON.stringify(value, null, 2), changed: JSON.stringify(before[field]) !== JSON.stringify(value) }));
});
watch(() => [f.value.nodeId, f.value.protocol, f.value.port, selectedServer.value?.name], () => {
  if (autoName.value && selectedServer.value) f.value.name = `${selectedServer.value.name}-${f.value.protocol}-${f.value.port}`;
});
watch(() => f.value.nodeId, async (newNodeId) => {
  if (newNodeId) {
    try { await loadNodeMeta(newNodeId, isOwner.value); }
    catch (e) { error.value = `端口范围读取失败：${(e as Error).message}`; return; }
    if (disposed || f.value.nodeId !== newNodeId) return;
  }
  if (!state.original && newNodeId) {
    const range = getNodePortRange(newNodeId);
    if (range) {
      try {
        f.value.port = randomPort(f.value, inbounds.value, '', Math.random, range);
      } catch {}
    }
  }
});
let disposed = false;
onUnmounted(() => { disposed = true; f.value.settings.private_key_pem = ''; });
async function load() {
  loading.value = true; loadError.value = '';
  try {
    await loadProtocolSupport();
    const data = await Promise.all([choices(p => nodeAPI.list(p, '')), choices((p, s) => proxyAPI.inbounds('', p, s)), choices((p, s) => userAPI.list(p, '', s)), choices((p, s) => proxyAPI.outbounds('', p, s)), choices((p, s) => proxyAPI.groups('', p, s)), choices((p, s) => proxyAPI.chains('', p, s)), choices(groupAPI.list)]);
    if (disposed) return;
    [servers.value, inbounds.value, users.value, outbounds.value, groups.value, chains.value] = data;
    memberGroups.value = data[6];
    initialGroupIds.value = memberGroups.value.filter(g => g.inboundIds?.includes(props.inbound?.id || '')).map(g => g.id);
    selectedGroupIds.value = [...initialGroupIds.value];
    if (!f.value.nodeId) f.value.nodeId = eligibleServers.value[0]?.id || '';
    else if (!eligibleServers.value.some(s => s.id === f.value.nodeId)) error.value = '预选服务器不可用，请选择其他已登记服务器';
    if (f.value.nodeId) await loadNodeMeta(f.value.nodeId, isOwner.value);
    if (!props.inbound && f.value.nodeId) {
      const range = getNodePortRange(f.value.nodeId);
      if (range) {
        try {
          f.value.port = randomPort(f.value, inbounds.value, '', Math.random, range);
        } catch {}
      }
    }
  } catch (e) { if (!disposed) loadError.value = (e as Error).message; }
  finally { if (!disposed) loading.value = false; }
}
function check(n: number) { error.value = validateStep(n, state, selectedServer.value, inbounds.value); return !error.value; }
function go(n: number) {
  if (busy.value || loading.value || savedId.value) return;
  if (n > step.value && !check(step.value)) return;
  error.value = ''; step.value = n;
}
function next() {
  if (!check(step.value)) return;
  const n = step.value === 2 && !securityStep.value ? 4 : step.value + 1;
  reached.value = Math.max(reached.value, n); step.value = n;
}
function back() { error.value = ''; step.value = step.value === 4 && !securityStep.value ? 2 : step.value - 1; }
function pickPort() {
  try {
    f.value.port = randomPort(f.value, inbounds.value, state.original?.id, Math.random, serverPortRange.value);
  } catch (e) {
    error.value = (e as Error).message;
  }
}
function close() { if (!busy.value) emit('close'); }
async function save(publish: boolean) {
  if (busy.value || !isOwner.value || savedId.value) return;
  busy.value = true; error.value = ''; result.value = '';
  try {
    // Re-read before save; the live check is advisory and the API remains authoritative.
    inbounds.value = await choices((p, s) => proxyAPI.inbounds('', p, s));
    for (let n = 1; n <= 5; n++) if (!check(n)) { step.value = n; return; }
    const value = buildPayload(state);
    const saved = await proxyAPI.saveInbound(state.original?.id || null, value);
    savedId.value = saved.id; step.value = 6;
    result.value = '节点草稿已保存，运行配置未改变。';
    try {
      const changed = [...new Set([...initialGroupIds.value, ...selectedGroupIds.value])]
        .filter(id => initialGroupIds.value.includes(id) !== selectedGroupIds.value.includes(id));
      for (const id of changed) {
        const fresh = await freshGroup(id), input = groupInput(fresh);
        input.inboundIds = withInboundMembership(input.inboundIds, saved.id, selectedGroupIds.value.includes(id));
        await groupAPI.save(id, input);
      }
    } catch (e) {
      error.value = '节点草稿已保存并保留，分组保存未全部完成：' + (e as Error).message + '。本次未发布；请重新打开节点核对分组后重试。';
      emit('saved'); return;
    }
    if (publish) {
      try {
        const published = await deploymentAPI.publishNode(value.nodeId, `${state.original ? '更新' : '新建'}自建节点入站: ${value.name}`);
        result.value = `已保存并发布配置 #${published.revision}（${published.status}），服务器确认后生效。`;
      } catch (e) { error.value = `发布失败：${(e as Error).message}。草稿已保存并保留，可在服务器详情的配置版本中重试发布。`; }
    }
    emit('saved');
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
onMounted(load);
</script>
