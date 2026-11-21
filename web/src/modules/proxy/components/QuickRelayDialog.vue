<template>
  <FormDrawer :model-value="true" title="添加中转" width="2xl" @update:model-value="close">
    <div v-if="created" class="space-y-3 text-sm" role="status">
      <p class="text-ios-green">已创建 {{ createdName }}，发布中</p>
      <p class="text-ios-secondary">请在画布中查看各服务器的发布结果。</p>
      <p v-for="warning in created.warnings" :key="warning" class="text-ios-orange">{{ warning }}</p>
      <p v-if="created.publication.batchId" class="text-xs text-ios-secondary break-all">发布批次：{{ created.publication.batchId }}</p>
    </div>
    <div v-else-if="createdRule" class="space-y-3 text-sm" role="status" aria-live="polite">
      <p class="text-ios-green">已创建 {{ createdRule.name }}</p>
      <p class="font-mono tabular-nums break-all">{{ ruleAddress }}</p>
      <p v-if="saving" class="text-ios-secondary">{{ progress }}</p>
      <p v-else-if="publishResult?.status === 'success'" class="text-ios-green">已发布到服务器。</p>
      <p v-else-if="publishResult?.status === 'publishing'" class="text-ios-secondary">服务器仍在应用，请稍后在隧道管理中查看。</p>
      <p v-else class="text-ios-red">发布失败，规则已保留，可在「隧道管理」中重试。</p>
      <p v-for="server in publishResult?.servers || []" :key="server.nodeId" class="text-ios-secondary">{{ server.nodeName }}：{{ resultLabels[server.status] }}<span v-if="server.message"> · {{ server.message }}</span></p>
      <p v-for="warning in groupFailures" :key="warning" class="text-ios-orange">{{ warning }}</p>
      <p v-if="error" role="alert" class="text-ios-red">{{ error }}</p>
      <p class="text-xs text-ios-secondary">发布成功、开启订阅显示并加入已授权分组后，用户刷新客户端订阅即可获得这条线路；使用{{ exitImport ? '导入节点' : '出口入站' }}的协议和凭据。</p>
    </div>
    <template v-else>
      <fieldset :disabled="saving" class="space-y-4 text-sm">
        <label class="block space-y-1.5 text-ios-secondary">中转方式
          <select v-model="mode" class="form-input-base">
            <option v-if="!exitImport || convertible(exitImport.id)" value="proxy">代理中转（入口解开协议，经 Trojan/TLS 通道转给出口）</option>
            <option value="forward">纯转发（入口只转发数据，不解开协议）</option>
          </select>
        </label>
        <label class="block space-y-1.5 text-ios-secondary">{{ showImports ? '出口' : '出口用户入站' }}
          <select v-model="exitKey" class="form-input-base">
            <option value="">{{ showImports ? '请选择出口' : '请选择出口入站' }}</option>
            <optgroup v-if="showImports" label="自建入站">
              <option v-for="inbound in exitOptions" :key="inbound.id" :value="'inbound:' + inbound.id">{{ serverName(inbound.nodeId) }} · {{ inbound.name }} · {{ inbound.protocol.toUpperCase() }}</option>
            </optgroup>
            <template v-else>
              <option v-for="inbound in exitOptions" :key="inbound.id" :value="'inbound:' + inbound.id">{{ serverName(inbound.nodeId) }} · {{ inbound.name }} · {{ inbound.protocol.toUpperCase() }}</option>
            </template>
            <optgroup v-if="showImports" label="导入节点">
              <option v-for="item in availableImports" :key="item.id" :value="'import:' + item.id">{{ item.name }} · {{ item.protocol.toUpperCase() }} · {{ item.sourceName }}</option>
            </optgroup>
          </select>
        </label>
        <p v-if="exitImport && quicImport" class="text-xs text-ios-orange">Hysteria2／TUIC 只转发主端口，导入节点的跳跃端口不可用。</p>
        <label class="block space-y-1.5 text-ios-secondary">入口服务器
          <select v-model="form.entryNodeId" class="form-input-base">
            <option value="">请选择入口服务器</option>
            <option v-for="server in entryOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
          </select>
        </label>
        <label v-if="mode === 'proxy' && protocols.length" class="block space-y-1.5 text-ios-secondary">入口协议
          <select v-model="protocolKey" class="form-input-base">
            <option value="">请选择入口协议</option>
            <option v-for="choice in protocols" :key="choice.core + ':' + choice.protocol" :value="choice.core + ':' + choice.protocol">{{ choice.protocol.toUpperCase() }} · {{ choice.core }}</option>
          </select>
        </label>
        <p v-else-if="mode === 'proxy' && form.entryNodeId" class="text-ios-orange">入口服务器尚未报告可用的用户入站协议。</p>
        <EntrySecurityEditor v-if="mode === 'proxy'" v-model="entrySecurity" :protocol="form.entryProtocol" :core="form.entryCore" :server="entryServer" :name="routeName || '新入口'" :exit="exit" />
        <p v-if="mode === 'proxy' && securityError" class="text-xs text-ios-orange">{{ securityError }}</p>
        <label v-if="mode === 'proxy'" class="block space-y-1.5 text-ios-secondary">中间服务器（可选，最多 {{ MAX_QUICK_RELAY_MIDDLE_NODES }} 台）
          <select v-model="middleNodeId" class="form-input-base">
            <option value="">不经过中间服务器</option>
            <option v-for="server in middleOptions" :key="server.id" :value="server.id">{{ server.name }}</option>
          </select>
        </label>
        <template v-if="mode === 'forward'">
          <label class="block space-y-1.5 text-ios-secondary">隧道
            <select v-model="forward.tunnelId" class="form-input-base" :disabled="loadingTunnels || !!tunnelLoadError">
              <option :value="null">直接转发（不经隧道）</option>
              <option v-for="tunnel in availableTunnels" :key="tunnel.id" :value="tunnel.id">{{ tunnel.name }} · {{ routeLabel(tunnel) }}</option>
            </select>
          </label>
          <p v-if="loadingTunnels" class="text-xs text-ios-secondary">正在加载隧道…</p>
          <p v-if="tunnelLoadError" role="alert" class="text-ios-red">{{ tunnelLoadError }} <button type="button" class="underline" @click="loadTunnels">重新加载</button></p>
          <label class="block space-y-1.5 text-ios-secondary">名称
            <input v-model="forward.name" maxlength="80" class="form-input-base" @input="nameEdited = true" />
          </label>
          <label class="block space-y-1.5 text-ios-secondary">监听端口
            <select v-model="forward.portMode" class="form-input-base">
              <option value="auto">自动分配</option><option value="manual">手动</option>
            </select>
          </label>
          <input v-if="forward.portMode === 'manual'" v-model.number="forward.listenPort" type="number" min="1" max="65535" aria-label="手动监听端口" class="form-input-base" />
          <p v-else class="text-xs text-ios-secondary">自动分配需要先在入口服务器设置中配置端口范围。</p>
          <label class="flex items-center gap-2 text-ios-secondary"><input v-model="forward.subscriptionVisible" type="checkbox" />显示在用户订阅中</label>
        </template>
        <details class="rounded-xl border border-ios-border p-3">
          <summary class="cursor-pointer text-ios-secondary">高级选项</summary>
          <div class="mt-3 space-y-3">
            <label class="block space-y-1.5 text-ios-secondary">{{ mode === 'forward' ? '这条线路加入的分组' : '新入口分组' }}
              <select v-model="form.groupMode" class="form-input-base">
                <option value="exit_groups">{{ exitImport ? '加入包含该导入节点的分组' : '加入出口入站所在分组' }}</option>
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
      <p v-if="!ready" class="text-sm text-ios-secondary">{{ mode === 'forward' ? '请选择入口服务器和出口（入口与出口入站须在不同服务器），填写名称、有效端口及指定的分组。' : '请选择不同的入口服务器和出口入站、入口协议，以及指定的分组。' }}</p>
      <p v-if="planning" role="status" class="text-sm text-ios-secondary">正在规划中转…</p>
      <section v-if="mode === 'proxy' && plan" class="space-y-3 text-sm" aria-label="创建与发布预览">
        <h4 class="text-base font-semibold">创建与发布预览</h4>
        <p class="text-ios-label break-words">经过服务器：{{ routeName }}</p>
        <div v-for="entry in plan.entries" :key="entry.instanceId" class="rounded-xl bg-ios-bg p-3 space-y-1">
          <p class="font-semibold break-words">新建入口：{{ entry.name }}</p>
          <p><ProtocolBadge :protocol="entry.protocol" :label="entry.protocol === 'vless' ? entrySecurityLabel(entrySecurity) : entry.protocol.toUpperCase()" compact /> · <span class="font-mono tabular-nums">{{ entry.port }}</span></p>
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
      <button v-else-if="createdRule" type="button" class="ios-dialog-btn-primary" :disabled="saving" @click="viewTunnels">在隧道管理中查看</button>
      <button v-else-if="ready && (mode === 'forward' || (plan?.ok && !planning))" type="button" class="ios-dialog-btn-primary" :disabled="saving" @click="create">{{ saving ? '创建并发布中…' : '创建并发布' }}</button>
    </template>
  </FormDrawer>
</template>

<script setup lang="ts">
import { nodeHasUserCore } from '../../nodes/coreSummary';
import ProtocolBadge from '../../../components/ProtocolBadge.vue';
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';
import EntrySecurityEditor from './relay/EntrySecurityEditor.vue';
import type { CanvasEntrySecurity } from '../api';
import { entrySecurityError, entrySecurityLabel } from '../entrySecurity';
import { useRouter } from 'vue-router';
import FormDrawer from '../../../components/FormDrawer.vue';
import type { NodeView } from '../../nodes/api';
import { freshGroup, groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { APIError } from '../../../app/apiClient';
import { tunnelAPI, tunnelRuleAPI, tunnelError, type Tunnel, type TunnelRule, type PublishResult } from '../../tunnels/api';
import { applyTunnelChanges } from '../../tunnels/applyTunnels';
import { endpoint, routeLabel, resultLabels, validPort } from '../../tunnels/model';
import { groupInput } from '../lineGroups';
import { groupHasImport } from '../importIdentity';
import type { ProxyNodeItem } from '../proxyNodes';
import { canvasAPI, type Inbound } from '../api';
import { isUserEntry } from '../userEntries';
import { buildQuickRelayInput, quickRelayProtocolChoices, MAX_QUICK_RELAY_MIDDLE_NODES, type QuickRelayForm, type QuickRelayPlan, type QuickRelayResult } from '../quickRelay';
import { quickRelayAPI } from '../quickRelayApi';

// Imported nodes are not managed servers. Pure forwarding reaches any of them
// (a tunnel rule with an import target); the proxy mode needs one the panel can
// convert into the last server's outbound (v1.6.0), so it is hidden otherwise.
const props = defineProps<{ servers: NodeView[]; inbounds: Inbound[]; groups: NodeGroup[]; imports?: ProxyNodeItem[]; initialExitInboundId?: string; initialExitImportId?: string; initialEntryNodeId?: string }>();
const emit = defineEmits<{ close: []; created: [] }>();
const router = useRouter();
const mode = ref<'proxy' | 'forward'>(props.initialExitImportId ? 'forward' : 'proxy');
const importOptions = computed(() => props.imports || []);
const importExitReasons = ref<Map<string, string>>(new Map());
const importExitNodes = ref<Map<string, string[]>>(new Map());
if (importOptions.value.length) {
  canvasAPI.importExits().then(result => { importExitNodes.value = new Map(result.items.map(item => [item.id, item.nodeIds])); importExitReasons.value = new Map(result.items.map(item => [item.id, item.reason])); }).catch(e => { error.value = `读取导入节点出口能力失败：${(e as Error).message}`; });
}
const convertible = (id: string) => {
  const lastNode = form.middleNodeIds[form.middleNodeIds.length - 1] || form.entryNodeId;
  return importExitReasons.value.get(id) === '' && (!lastNode || !!importExitNodes.value.get(id)?.includes(lastNode));
};
const availableImports = computed(() => mode.value === 'forward' ? importOptions.value : importOptions.value.filter(item => convertible(item.id)));
const showImports = computed(() => availableImports.value.length > 0);
const forward = reactive({ name: '', tunnelId: null as string | null, portMode: 'auto' as 'auto' | 'manual', listenPort: undefined as number | undefined, subscriptionVisible: true });
const nameEdited = ref(false), loadingTunnels = ref(false), tunnelLoadError = ref('');
const tunnels = ref<Tunnel[]>([]), tunnelsLoaded = ref(false);
const createdRule = ref<TunnelRule | null>(null), publishResult = ref<PublishResult | null>(null);
const progress = ref(''), groupFailures = ref<string[]>([]);
const form = reactive<QuickRelayForm>({ entryNodeId: props.initialEntryNodeId || '', exitInboundId: props.initialExitInboundId || '', exitImportId: props.initialExitImportId || '', middleNodeIds: [], entryProtocol: '', entryCore: '', groupMode: 'exit_groups', groupIds: [] });
const exitImportId = computed({ get: () => form.exitImportId || '', set: value => { form.exitImportId = value; } });
const exitImport = computed(() => importOptions.value.find(item => item.id === exitImportId.value));
const quicImport = computed(() => exitImport.value?.protocol === 'hysteria2' || exitImport.value?.protocol === 'tuic');
// Leaving pure forwarding drops an import the proxy mode cannot use.
watch([mode, importExitReasons], ([value]) => { if (value === 'proxy' && exitImportId.value && !convertible(exitImportId.value)) exitImportId.value = ''; });
const saving = ref(false), planning = ref(false), error = ref('');
const plan = ref<QuickRelayPlan | null>(null), created = ref<QuickRelayResult | null>(null), createdName = ref('');
const liveServers = computed(() => props.servers.filter(server => server.enabled && !server.deletedAt && nodeHasUserCore(server)));
const exitOptions = computed(() => props.inbounds.filter(inbound => isUserEntry(inbound) && inbound.protocol !== 'forward' && inbound.enabled && liveServers.value.some(server => server.id === inbound.nodeId)));
const exit = computed(() => exitOptions.value.find(inbound => inbound.id === form.exitInboundId));
const exitKey = computed({
  get: () => exitImportId.value ? `import:${exitImportId.value}` : form.exitInboundId ? `inbound:${form.exitInboundId}` : '',
  set: value => {
    const [kind, id = ''] = value.split(/:(.*)/s);
    exitImportId.value = kind === 'import' ? id : '';
    form.exitInboundId = kind === 'inbound' ? id : '';
  }
});
const entryOptions = computed(() => liveServers.value.filter(server => server.id !== exit.value?.nodeId && (mode.value === 'forward' || quickRelayProtocolChoices(server).length)));
const middleOptions = computed(() => liveServers.value.filter(server => server.id !== form.entryNodeId && server.id !== exit.value?.nodeId));
const groupOptions = computed(() => props.groups.filter(group => group.enabled && ['subscription', 'mixed'].includes(group.purpose)));
const protocols = computed(() => quickRelayProtocolChoices(entryOptions.value.find(server => server.id === form.entryNodeId)));
const entryServer = computed(() => entryOptions.value.find(server => server.id === form.entryNodeId));
const entrySecurity = computed<CanvasEntrySecurity>({
  get: () => ({ security: form.entrySecurity, transport: form.entryTransport, flow: form.entryFlow, realityTarget: form.entryRealityTarget, realityServerName: form.entryRealityServerName }),
  set: value => { form.entrySecurity = value.security; form.entryTransport = value.transport; form.entryFlow = value.flow; form.entryRealityTarget = value.realityTarget; form.entryRealityServerName = value.realityServerName; }
});
const securityError = computed(() => form.entryProtocol === 'vless' ? entrySecurityError(entryServer.value, form.entryCore, entrySecurity.value) : '');
const protocolKey = computed({ get: () => form.entryProtocol ? `${form.entryCore}:${form.entryProtocol}` : '', set: value => {
  const choice = protocols.value.find(item => `${item.core}:${item.protocol}` === value);
  form.entryProtocol = choice?.protocol || ''; form.entryCore = choice?.core || '';
} });
const middleNodeId = computed({ get: () => form.middleNodeIds[0] || '', set: value => { form.middleNodeIds = value ? [value] : []; } });
function serverName(id: string) { return props.servers.find(server => server.id === id)?.name || id; }
const routeName = computed(() => [...[form.entryNodeId, ...form.middleNodeIds, exit.value?.nodeId].filter((id): id is string => !!id).map(serverName), ...(exitImport.value ? [exitImport.value.name] : [])].join(' → '));
const availableTunnels = computed(() => tunnels.value.filter(tunnel => {
  const first = tunnel.hops[0], last = tunnel.hops.at(-1);
  // An imported target is dialled from the tunnel's exit, so any exit server fits.
  return tunnel.status !== 'pending_delete' && first?.role === 'entry' && first.nodeId === form.entryNodeId && last?.role === 'exit' && (!!exitImport.value || last.nodeId === exit.value?.nodeId);
}));
const selectedTunnel = computed(() => availableTunnels.value.find(tunnel => tunnel.id === forward.tunnelId));
const ruleAddress = computed(() => {
  const rule = createdRule.value;
  const server = props.servers.find(item => item.id === rule?.entryNodeId);
  const host = server?.connectHost || server?.publicIp;
  return rule && host ? endpoint(host, rule.listenPort) : '入口服务器未配置连接地址或公网 IP';
});
async function loadTunnels() {
  loadingTunnels.value = true; tunnelLoadError.value = '';
  try { tunnels.value = (await tunnelAPI.list()).items; tunnelsLoaded.value = true; }
  catch (e) { tunnelLoadError.value = tunnelError(e); }
  finally { loadingTunnels.value = false; }
}
watch(mode, value => { if (value === 'forward' && !tunnelsLoaded.value && !loadingTunnels.value) void loadTunnels(); });
watch(availableTunnels, items => {
  if (forward.tunnelId && !items.some(tunnel => tunnel.id === forward.tunnelId)) forward.tunnelId = null;
}, { flush: 'sync' });
watch([() => form.entryNodeId, () => exit.value?.nodeId, () => exitImport.value?.name, () => selectedTunnel.value?.name], () => {
  const target = exitImport.value?.name || (exit.value && serverName(exit.value.nodeId));
  if (!nameEdited.value) forward.name = target && form.entryNodeId ? `${target} 经 ${selectedTunnel.value?.name || serverName(form.entryNodeId)}`.slice(0, 80) : '';
}, { immediate: true, flush: 'sync' });
watch([() => form.entryNodeId, () => form.exitInboundId, exitImportId, mode], () => {
  if (form.entryNodeId === exit.value?.nodeId) form.entryNodeId = '';
  form.middleNodeIds = form.middleNodeIds.filter(id => middleOptions.value.some(server => server.id === id));
  const choice = protocols.value.find(item => item.protocol === (exit.value?.protocol || exitImport.value?.protocol));
  form.entryProtocol = choice?.protocol || ''; form.entryCore = choice?.core || '';
}, { immediate: true, flush: 'sync' });
const ready = computed(() => (!!exit.value || (!!exitImport.value && (mode.value === 'forward' || convertible(exitImport.value.id)))) && entryOptions.value.some(server => server.id === form.entryNodeId) &&
  (mode.value === 'forward'
    ? !loadingTunnels.value && !tunnelLoadError.value && !!forward.name.trim() && forward.name.length <= 80 &&
      (!forward.tunnelId || !!selectedTunnel.value) && (forward.portMode === 'auto' || validPort(forward.listenPort))
    : !securityError.value && protocols.value.some(item => item.protocol === form.entryProtocol && item.core === form.entryCore) &&
      form.middleNodeIds.every(id => middleOptions.value.some(server => server.id === id))) &&
  (form.groupMode !== 'explicit' || (form.groupIds.length > 0 && form.groupIds.every(id => groupOptions.value.some(group => group.id === id)))));

let requestId = crypto.randomUUID();
let generation = 0;
let timer: ReturnType<typeof setTimeout> | undefined;
// Invalidate synchronously: an old successful plan must never enable submission
// for a newly edited form, even during the debounce or an in-flight response.
watch(() => JSON.stringify([mode.value, buildQuickRelayInput(form, 'preview')]), () => {
  const current = ++generation;
  clearTimeout(timer);
  requestId = crypto.randomUUID();
  plan.value = null; error.value = ''; planning.value = mode.value === 'proxy' && ready.value;
  if (mode.value !== 'proxy' || !ready.value) return;
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
  if (mode.value === 'forward') { await createForward(); return; }
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
async function createForward() {
  if (saving.value || !ready.value || createdRule.value) return;
  saving.value = true; error.value = ''; groupFailures.value = [];
  const groups = form.groupMode === 'none' ? [] : form.groupMode === 'explicit'
    ? groupOptions.value.filter(group => form.groupIds.includes(group.id))
    : props.groups.filter(group => ['subscription', 'mixed'].includes(group.purpose) && (exitImport.value
      ? groupHasImport(group, exitImport.value)
      : group.inboundIds.includes(form.exitInboundId)));
  try {
    const rule = await tunnelRuleAPI.create({
      name: forward.name.trim(), entryNodeId: selectedTunnel.value ? selectedTunnel.value.hops[0]!.nodeId : form.entryNodeId,
      portMode: forward.portMode, ...(forward.portMode === 'manual' ? { listenPort: forward.listenPort } : {}),
      network: 'both', tunnelId: forward.tunnelId,
      target: exitImport.value ? { mode: 'import', importId: exitImport.value.id } : { mode: 'linked', inboundId: form.exitInboundId },
      subscriptionVisible: forward.subscriptionVisible, enabled: true
    });
    createdRule.value = rule;
    for (const { id, name } of groups) {
      progress.value = `正在加入分组 ${name}…`;
      try {
        const fresh = await freshGroup(id);
        const input = groupInput(fresh);
        input.relayIds = [...new Set([...(input.relayIds || []), rule.id])];
        await groupAPI.save(id, input);
      } catch (e) { groupFailures.value.push(`规则已创建，加入分组「${name}」失败：${tunnelError(e)}。请在节点分组中补加这条线路。`); }
    }
    publishResult.value = await applyTunnelChanges({ tunnelIds: [], ruleIds: [rule.id] }, text => { progress.value = text; });
  } catch (e) {
    error.value = e instanceof APIError && e.code === 'no_free_port'
      ? '入口服务器没有可用的自动分配端口；若未设置端口范围，请在服务器设置中配置，或选择「手动」监听端口。'
      : tunnelError(e);
  } finally {
    saving.value = false;
    if (createdRule.value) emit('created');
  }
}
async function viewTunnels() {
  await router.push('/proxy/tunnel-rules');
  emit('close');
}
async function viewCanvas() {
  await router.push({ path: '/proxy/relay', query: { schemeId: created.value!.schemeId } });
  emit('close');
}
</script>
