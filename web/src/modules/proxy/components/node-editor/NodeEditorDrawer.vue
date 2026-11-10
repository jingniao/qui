<template>
  <FormDrawer :model-value="true" :title="state.original ? '编辑节点' : '新建节点'" description="填写节点信息与协议设置，保存草稿或发布到服务器。" width="2xl" @update:model-value="close">
    <p v-if="loading" role="status" class="text-sm text-ios-secondary">正在读取服务器与入站…</p>
    <div v-else-if="loadError" class="space-y-2"><p role="alert" class="text-ios-red text-sm">{{ loadError }}</p><button class="ios-btn-secondary" @click="load">重试</button></div>
    <p v-if="error" ref="errorMessage" tabindex="-1" role="alert" class="text-sm text-ios-red outline-none">{{ error }}</p>
    <fieldset v-if="!loading && !loadError" :disabled="busy || !!savedId" class="space-y-6">
      <section class="space-y-4">
        <h4 class="text-ios-label text-base font-semibold leading-normal">基础信息</h4>
        <label class="block text-xs text-ios-secondary">节点名称<input v-model="f.name" placeholder="例如 Tokyo-vless-443" class="form-input-base mt-1.5" @input="autoName = false" /><span class="block text-xs mt-1">显示在入站列表、订阅名称与访问记录中；默认按「服务器-协议-端口」自动生成，手动修改后不再自动更新。</span></label>
        <div class="grid gap-4 sm:grid-cols-2">
          <label class="block text-xs text-ios-secondary">所属服务器
            <select v-model="f.nodeId" :disabled="!!state.original" class="form-input-base mt-1.5">
              <option value="" disabled>请选择服务器</option>
              <option v-for="server in eligibleServers" :key="server.id" :value="server.id">{{ server.name }}</option>
            </select>
            <span class="block text-xs mt-1">运行此入站的已登记服务器；编辑时不能更换。</span>
          </label>
          <label v-if="availableProtocols.length" class="block text-xs text-ios-secondary">协议
            <select :value="f.protocol" :disabled="!!state.original" class="form-input-base mt-1.5" @change="changeProtocol(state, ($event.target as HTMLSelectElement).value)">
              <option v-for="protocol in availableProtocols" :key="protocol" :value="protocol">{{ protocol.toUpperCase() }}</option>
            </select>
            <span class="block text-xs mt-1">用户接入使用的协议；只列出所选服务器当前核心实际报告支持的协议。</span>
          </label>
        </div>
        <div role="group" aria-labelledby="node-editor-groups" class="space-y-2">
          <p id="node-editor-groups" class="text-xs text-ios-secondary">所属节点组（可多选）</p>
          <div v-if="memberGroups.length" class="grid max-h-36 gap-2 overflow-y-auto sm:grid-cols-2">
            <label v-for="group in memberGroups" :key="group.id" class="flex items-start gap-2 rounded-xl border p-3 text-sm text-ios-label cursor-pointer" :class="selectedGroupIds.includes(group.id) ? 'border-ios-blue bg-ios-blue/5' : 'border-ios-border'">
              <input v-model="selectedGroupIds" :value="group.id" type="checkbox" class="mt-0.5 shrink-0 accent-ios-blue" />
              <span class="min-w-0 break-words">{{ group.name }}<span v-if="group.purpose === 'relay' || group.purpose === 'egress'" class="block text-xs text-ios-secondary">不授予用户接入</span></span>
            </label>
          </div>
          <p v-else class="text-xs text-ios-secondary">暂无节点组，请先在节点分组页面创建。</p>
          <p class="text-xs text-ios-secondary">保存时加入所选节点组；不选择则不加入任何组，用户订阅也就看不到此入站。</p>
          <p class="text-xs text-ios-secondary">例如把入口入站加入「日本入口」组，再在节点分组页授权用户；组内新增可用入站后用户刷新订阅即可获得。</p>
        </div>
        <p v-if="state.original" class="text-xs text-ios-secondary">编辑时所属服务器和协议保持不变。</p>
        <p v-if="!eligibleServers.length" class="text-sm text-ios-secondary">没有可用的已登记服务器，请先登记服务器。</p>
        <div v-if="coreSelectable.length > 1" class="space-y-1.5">
          <label class="block text-xs text-ios-secondary">运行内核
            <select v-model="f.core" class="form-input-base mt-1.5">
              <option value="">跟随主核心（{{ primaryCoreLabel }}）</option>
              <option v-for="core in coreSelectable" :key="core.name" :value="core.name">{{ core.label }} {{ core.version }}{{ core.primary ? '（主核心）' : '' }}</option>
            </select>
          </label>
          <p class="text-xs text-ios-secondary">只列出该服务器当前上报为启用、且能承载当前协议的核心；协议选项按所选核心的能力过滤。</p>
        </div>
        <p v-else-if="coreSelectable.length === 1" class="text-xs text-ios-secondary">运行内核：{{ coreSelectable[0].label }} {{ coreSelectable[0].version }}（该服务器没有其他能承载当前协议的核心）</p>
        <p v-else class="text-xs text-ios-orange">该服务器尚未上报能承载当前协议的核心，运行内核未知；保存时由后端判定。</p>
        <p v-if="coreError" role="alert" class="text-xs text-ios-red">{{ coreError }}</p>
        <p v-if="protocolReason(selectedServer, f.protocol, f.core)" role="alert" class="text-xs text-ios-orange">{{ protocolReason(selectedServer, f.protocol, f.core) }}</p>
        <p v-if="udpLimitNote" class="text-xs text-ios-secondary">{{ udpLimitNote }}</p>
      </section>
      <section class="space-y-3 border-t border-ios-border pt-5">
        <h4 class="text-ios-label text-base font-semibold leading-normal">连接设置</h4>
        <p class="text-xs text-ios-secondary">服务器地址：<span class="font-mono" :class="connectAddress(selectedServer) ? '' : 'text-ios-orange'">{{ connectAddress(selectedServer) || '未设置连接地址' }}</span><router-link v-if="isOwner && selectedServer && !connectAddress(selectedServer)" :to="`/nodes/${selectedServer.id}?tab=settings`" class="ml-2 text-ios-blue font-medium hover:underline">去设置连接地址</router-link></p>
        <div class="text-xs text-ios-secondary">
          <label for="node-editor-port" class="block">监听端口</label>
          <div class="flex items-center gap-2 mt-1.5">
            <input id="node-editor-port" v-model.number="f.port" type="number" min="1" max="65535" placeholder="例如 443" class="form-input-base min-w-0 flex-1 font-mono" />
            <button type="button" class="ios-btn-secondary shrink-0" @click="pickPort">随机端口</button>
          </div>
          <p class="mt-1">客户端连接此入站时使用的端口；同一服务器上不能与其他入站（含禁用）重复，不分 TCP/UDP。</p>
        </div>
        <p v-if="serverPortRange" class="text-xs text-ios-blue font-mono">服务器分配范围：{{ serverPortRange.start }}–{{ serverPortRange.end }}</p>
        <p v-else class="text-xs text-ios-secondary">未设置服务器范围；随机端口使用编辑器默认范围 10000–65535。</p>
        <p aria-live="polite" :class="['text-xs', conflict ? 'text-ios-red' : 'text-ios-secondary']">{{ conflict || '入站列表中无端口冲突（含画布方案入站）；不检测主机其他进程。' }}</p>
      </section>
      <section class="space-y-4 border-t border-ios-border pt-5">
        <h4 class="text-ios-label text-base font-semibold leading-normal">协议设置</h4>
        <NodeProtocolFields :state="state" :server="selectedServer" :certificates="certificates" :gost="gostForwardReady(selectedServer, f.core)" />
      </section>
      <section class="space-y-3 border-t border-ios-border pt-5">
        <h4 class="text-ios-label text-base font-semibold leading-normal">授权与订阅</h4>
        <div class="flex flex-wrap gap-x-5 gap-y-2 text-sm text-ios-label">
          <label class="inline-flex items-center gap-2"><input v-model="f.enabled" type="checkbox" />启用节点</label>
          <label v-if="!f.internalRelay && f.protocol !== 'forward'" class="inline-flex items-center gap-2"><input v-model="f.subscriptionVisible" type="checkbox" />显示在授权用户订阅中</label>
        </div>
        <p class="text-xs text-ios-secondary">启用节点：取消勾选后此入站不参与发布，已发布配置里的用户凭据随之移除。显示在授权用户订阅中：取消后仅供节点间中转使用，用户订阅与凭据都不再包含它。</p>
        <p v-if="!f.internalRelay && f.protocol !== 'forward' && !f.subscriptionVisible" class="text-xs text-ios-orange">隐藏的用户入站不会获得用户凭据，发布后为停用。</p>
      </section>
      <NodeAdvancedFields :form="f" :editing="!!state.original" :outbounds="outbounds" :groups="groups" :chains="chains" :users="users" :open="!!error" />
      <details :open="!!savedId" class="rounded-2xl border border-ios-border p-4 text-sm">
        <summary class="cursor-pointer font-medium text-ios-label">配置预览与变更</summary>
        <div class="space-y-4 mt-4">
        <p class="text-ios-label">{{ selectedServer?.name || '—' }} · {{ f.name }} · {{ f.protocol.toUpperCase() }} :{{ f.port }}</p>
        <p class="text-ios-secondary">{{ f.listen }} · 运行内核 {{ f.core ? coreChoices.find(c => c.name === f.core)?.label || f.core : `主核心（${primaryCoreLabel}）` }} · {{ f.enabled ? '启用' : '停用' }} · {{ f.subscriptionVisible && !f.internalRelay ? '订阅可见' : '订阅隐藏' }} · {{ f.internalRelay ? '内部中转' : '用户接入' }}</p>
        <p class="text-xs text-ios-secondary">默认出站：{{ f.defaultOutbound }}；用户范围：{{ f.allowedUserIds.length ? f.allowedUserIds.map(id => users.find(u => u.id === id)?.username || id).join('、') : '已获授权、且分组包含此入站的用户' }}</p>
        <p class="text-xs text-ios-secondary">所属节点组：{{ selectedGroupIds.map(id => memberGroups.find(g => g.id === id)?.name || '—').join('、') || '—' }}。非空 allowedUserIds 只进一步缩小上述分组授权范围，不会单独授予接入权限。</p>
        <p v-if="f.protocol === 'forward'" class="text-xs text-ios-secondary">{{ forwardPreview }}</p>
        <p v-if="f.settings.security === 'reality'" class="text-xs text-ios-secondary">Reality：{{ f.settings.reality?.handshake_target || '尚未填写握手目标' }}；订阅使用第一个 SNI / Short ID。{{ f.rotateRealityKey ? '本次保存将轮换密钥；发布成功后客户端须更新订阅。' : state.original?.hasRealityKey ? '保留现有密钥。' : '首次保存时生成密钥，保存后返回公钥。' }}</p>
        <div class="overflow-x-auto rounded-2xl border border-ios-border">
          <table class="w-full text-xs text-left"><thead class="bg-ios-bg text-ios-secondary"><tr><th class="p-3">字段</th><th class="p-3">保存前</th><th class="p-3">保存后</th></tr></thead>
            <tbody><tr v-for="row in diff" :key="row.field" :class="row.changed ? 'bg-ios-blue/10' : ''"><td class="p-3 text-ios-secondary">{{ row.field }}</td><td class="p-3 font-mono whitespace-pre-wrap break-all">{{ row.before }}</td><td class="p-3 font-mono whitespace-pre-wrap break-all">{{ row.after }}</td></tr></tbody>
          </table>
        </div>
        <p class="text-xs text-ios-secondary">证书与密钥仅显示是否设置。</p>
        </div>
      </details>
      <p class="text-xs text-ios-orange">保存草稿后运行配置不变。发布会包含该服务器的全部待发布修改；内部中转请先发布目标服务器。</p>
    </fieldset>
    <p v-if="result" role="status" :class="['text-sm', resultTone === 'success' ? 'text-emerald-600' : resultTone === 'danger' ? 'text-red-600' : resultTone === 'warning' ? 'text-amber-600' : resultTone === 'info' ? 'text-ios-blue' : 'text-ios-secondary']">{{ result }}</p>
    <div v-if="savedInbound?.settings.reality?.public_key" class="space-y-2 text-xs text-ios-secondary">
      <label class="block">已保存的 Reality 公钥<input :value="savedInbound.settings.reality.public_key" readonly class="form-input-base mt-2 font-mono" /></label>
      <button type="button" class="ios-btn-secondary" @click="copySavedPublicKey">复制已保存公钥</button>
      <p v-if="keyCopyStatus" role="status">{{ keyCopyStatus }}</p>
      <p>公钥来自保存后的服务器读回；运行配置与客户端连接仍以成功发布为准。</p>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-end gap-2">
      <button v-if="savedId" class="ios-dialog-btn-secondary" @click="emit('close')">关闭</button>
      <template v-else>
        <button :disabled="busy" class="ios-dialog-btn-secondary" @click="close">取消</button>
        <button :disabled="busy || loading || !!loadError" class="ios-dialog-btn-secondary" @click="save(false)">保存草稿</button>
        <button :disabled="busy || loading || !!loadError" class="ios-dialog-btn-primary" @click="save(true)">{{ busy ? '处理中…' : '保存并发布' }}</button>
      </template>
      </div>
    </template>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import FormDrawer from '../../../../components/FormDrawer.vue';
import { isOwner } from '../../../../app/auth';
import { deviceGateUdpUnsupported } from '../../../../app/deviceLimit';
import { proxyAPI, choices, loadProtocolSupport, protocolOptions, type Inbound, type Outbound, type OutboundGroup, type ProxyChain } from '../../api';
import { nodeAPI, type NodeView } from '../../../nodes/api';
import { getNodePortRange, loadNodeMeta } from '../../../nodes/nodeMeta';
import { connectAddress } from '../../../nodes/nodeAddress';
import { groupAPI, freshGroup, type NodeGroup } from '../../../nodes/groupsApi';
import { groupInput } from '../../lineGroups';
import { userAPI, type ProxyUser } from '../../../users/api';
import { deploymentAPI } from '../../../system/deploymentApi';
import { watchPublish, type PublishTone, type PublishWatch } from '../../../system/publishProgress';
import { certificatesAPI, type ManagedCertificate } from '../../../system/certificatesApi';
import NodeProtocolFields from './NodeProtocolFields.vue';
import NodeAdvancedFields from './NodeAdvancedFields.vue';
import { withInboundMembership, buildPayload, coreOptions, coreReason, createEditor, changeProtocol, forwardListenPorts, forwardPool, gostForwardReady, portConflict, previewPayload, protocolChoices, protocolReason, randomPort, relayMode, serverEligible, validateStep, type EditorOrigin } from './nodeEditorModel';
const props = withDefaults(defineProps<{ serverId?: string; inbound?: Inbound | null; origin?: EditorOrigin }>(), { serverId: '', inbound: null, origin: 'list' });
const emit = defineEmits<{ close: []; saved: [] }>();
const route = useRoute();
const state = reactive(createEditor(props.serverId || String(route.query.server || ''), props.inbound, props.origin));
const f = computed(() => state.form);
const servers = ref<NodeView[]>([]), inbounds = ref<Inbound[]>([]), users = ref<ProxyUser[]>([]);
const certificates = ref<ManagedCertificate[]>([]);
const outbounds = ref<Outbound[]>([]), groups = ref<OutboundGroup[]>([]), chains = ref<ProxyChain[]>([]);
const memberGroups = ref<NodeGroup[]>([]), selectedGroupIds = ref<string[]>([]);
const initialGroupIds = ref<string[]>([]);
const autoName = ref(!props.inbound);
const loading = ref(true), busy = ref(false), error = ref(''), loadError = ref(''), result = ref(''), resultTone = ref<PublishTone>('info'), savedId = ref('');
let publishWatcher: PublishWatch | null = null;
const savedInbound = ref<Inbound | null>(null), keyCopyStatus = ref('');
const errorMessage = ref<HTMLElement | null>(null);
watch(error, async message => { if (message) { await nextTick(); errorMessage.value?.focus(); } });
const protocols = computed(() => [...new Set([...protocolOptions.inbound, 'forward'])]);
const selectedServer = computed(() => servers.value.find(s => s.id === f.value.nodeId));
const eligibleServers = computed(() => servers.value.filter(serverEligible));
// Only the protocols the selected server's core reports are offered; an edited
// inbound keeps its stored protocol listed so the select never shows another value.
const availableProtocols = computed(() => protocolChoices(selectedServer.value, f.value.core, protocols.value, f.value.protocol, !!state.original));
// Cores the selected server reports as enabled (K3b). One core = read-only text;
// several = a selector whose empty value follows the node's primary core.
const coreChoices = computed(() => coreOptions(selectedServer.value));
// Cores offered for the protocol being edited: GOST terminates forwards only, so
// it never appears for a user protocol.
const coreSelectable = computed(() => coreOptions(selectedServer.value, f.value.protocol));
const primaryCore = computed(() => coreChoices.value.find(item => item.primary) || coreChoices.value[0]);
const udpLimitNote = computed(() => {
  const core = f.value.core || primaryCore.value?.name;
  return deviceGateUdpUnsupported(core, f.value.protocol) ? `设备限制不支持此入站的 UDP（${core} ${f.value.protocol}），TCP 仍受限制。` : '';
});
const primaryCoreLabel = computed(() => primaryCore.value ? `${primaryCore.value.label} ${primaryCore.value.version}` : '—');
const coreError = computed(() => selectedServer.value ? coreReason(selectedServer.value, f.value.core) : '');
const conflict = computed(() => portConflict(f.value, inbounds.value, state.original?.id));
// The preview states what publishing this forward will actually create: one
// service per (port, transport), each with the whole pool and its own limits.
const forwardPreview = computed(() => {
  const forward = f.value.settings.forward;
  if (!forward) return '端口转发尚未设置目标';
  // A relay listener is the GOST tunnel's exit half: one service, no pool.
  if (relayMode(f.value.settings)) return `GOST 中继监听：${f.value.port} 端口由隧道资源管理，只计入节点总量`;
  const ports = forwardListenPorts(f.value.port, f.value.settings);
  const networks = forward.network === 'tcp,udp' ? 2 : 1;
  const pool = forwardPool(forward).map(target => `${target.host}:${target.port}`).join('、') || '—';
  const limits = [
    forward.bandwidth_in_bps ? `入 ${forward.bandwidth_in_bps / 1024} KiB/s` : '',
    forward.bandwidth_out_bps ? `出 ${forward.bandwidth_out_bps / 1024} KiB/s` : '',
    forward.max_connections !== undefined ? `连接上限 ${forward.max_connections}` : ''
  ].filter(Boolean).join('、');
  const end = ports[ports.length - 1];
  return `目标池：${pool}；策略 ${forward.strategy || 'round'}；监听 ${ports.length > 1 ? `${ports[0]}–${end} 共 ${ports.length} 个端口` : `${ports[0]} 端口`} × ${networks} 协议 = ${ports.length * networks} 个服务${limits ? `；${limits}（每个服务各自生效）` : ''}；只计入节点总量`;
});
const serverPortRange = computed(() => getNodePortRange(f.value.nodeId));
const diff = computed(() => {
  const after = previewPayload(buildPayload(state, selectedServer.value));
  const before = state.original ? previewPayload(createEditor('', state.original, 'server').form) : {} as Record<string, unknown>;
  return Object.entries(after).map(([field, value]) => ({ field, before: JSON.stringify(before[field], null, 2) ?? '—', after: JSON.stringify(value, null, 2), changed: JSON.stringify(before[field]) !== JSON.stringify(value) }));
});
watch(() => [f.value.nodeId, f.value.protocol, f.value.port, selectedServer.value?.name], () => {
  if (autoName.value && selectedServer.value) f.value.name = `${selectedServer.value.name}-${f.value.protocol}-${f.value.port}`;
});
watch(() => f.value.nodeId, async (newNodeId) => {
  // The selected core belongs to the previous server; reset to its primary.
  f.value.core = '';
  if (newNodeId) {
    try { await loadNodeMeta(newNodeId, isOwner.value); }
    catch (e) { error.value = `端口范围读取失败：${(e as Error).message}`; return; }
    if (disposed || f.value.nodeId !== newNodeId) return;
  }
  if (!state.original && newNodeId) {
    // Creation: the draft protocol falls back to a reported one, so an option that
    // is no longer offered is never kept selected.
    const offered = protocolChoices(selectedServer.value, f.value.core, protocols.value, f.value.protocol);
    if (offered.length && !offered.includes(f.value.protocol)) changeProtocol(state, offered[0]);
    const range = getNodePortRange(newNodeId);
    if (range) {
      try {
        f.value.port = randomPort(f.value, inbounds.value, '', Math.random, range);
      } catch {}
    }
  }
});
let disposed = false;
onUnmounted(() => { disposed = true; f.value.settings.private_key_pem = ''; publishWatcher?.stop(); });
async function load() {
  loading.value = true; loadError.value = '';
  try {
    await loadProtocolSupport();
    if (isOwner.value) certificates.value = await certificatesAPI.list();
    const data = await Promise.all([choices((p, s) => nodeAPI.list(p, '', false, s)), choices((p, s) => proxyAPI.inbounds('', p, s)), choices((p, s) => userAPI.list(p, '', s)), choices((p, s) => proxyAPI.outbounds('', p, s)), choices((p, s) => proxyAPI.groups('', p, s)), choices((p, s) => proxyAPI.chains('', p, s)), choices(groupAPI.list)]);
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
function pickPort() {
  try {
    f.value.port = randomPort(f.value, inbounds.value, state.original?.id, Math.random, serverPortRange.value);
  } catch (e) {
    error.value = (e as Error).message;
  }
}
function close() { if (!busy.value) emit('close'); }
async function copySavedPublicKey() {
  try { await navigator.clipboard.writeText(savedInbound.value?.settings.reality?.public_key || ''); keyCopyStatus.value = '公钥已复制'; }
  catch { keyCopyStatus.value = '未能复制，请在公钥框中选择并手动复制'; }
}
async function save(publish: boolean) {
  if (busy.value || !isOwner.value || savedId.value) return;
  busy.value = true; error.value = ''; result.value = '';
  try {
    // Re-read before save; the live check is advisory and the API remains authoritative.
    inbounds.value = await choices((p, s) => proxyAPI.inbounds('', p, s));
    for (let n = 1; n <= 5; n++) if (!check(n)) return;
    const value = buildPayload(state, selectedServer.value);
    const saved = await proxyAPI.saveInbound(state.original?.id || null, value);
    savedInbound.value = saved;
    savedId.value = saved.id;
    result.value = '节点草稿已保存，运行配置未改变。';
    resultTone.value = 'warning';
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
        result.value = `配置 #${published.revision}：待服务器确认`;
        resultTone.value = 'info';
        publishWatcher?.stop();
        publishWatcher = watchPublish(value.nodeId, published.revision, (phase, timedOut) => {
          let text = `配置 #${published.revision}：${phase.label}`;
          if (timedOut) text += '，仍未收到服务器结果，可在服务器详情的配置版本中查看';
          result.value = text;
          resultTone.value = phase.tone;
        });
      } catch (e) { error.value = `发布失败：${(e as Error).message}。草稿已保存并保留，可在服务器详情的配置版本中重试发布。`; }
    }
    emit('saved');
  } catch (e) { error.value = '保存草稿失败，输入已保留：' + (e as Error).message; }
  finally { busy.value = false; }
}
onMounted(load);
</script>
