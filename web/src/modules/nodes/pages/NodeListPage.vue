<template>
  <div class="space-y-6">
    <PageHeader title="服务器列表" parent="服务器" description="查看服务器运行状态、流量与续费信息。">
      <template #actions><button v-if="isOwner" class="ios-btn-primary" @click="enrollment?.open()">新增服务器</button></template>
    </PageHeader>

    <!-- Search, Filter & View Mode Toolbar -->
    <div class="flex flex-col lg:flex-row lg:items-center justify-between gap-3">
      <form class="flex flex-wrap items-center gap-2.5 flex-1" @submit.prevent="applyFilters">
        <!-- Search Input -->
        <div class="relative flex-1 min-w-[200px] max-w-sm">
          <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            v-model="keyword"
            placeholder="搜索名称、IP、主机名、地区或备注…"
            class="ios-search-input"
          />
          <button
            v-if="keyword"
            type="button"
            @click="keyword = ''; applyFilters()"
            class="absolute right-2.5 top-1/2 -translate-y-1/2 text-ios-tertiary hover:text-ios-label p-0.5 rounded-full hover:bg-black/5 transition-colors"
            title="清空"
          >
            <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>

        <!-- Status Filter -->
        <select v-model="status" class="ios-filter-select !w-auto" @change="applyFilters">
          <option v-for="option in statusOptions" :key="option[0]" :value="option[0]">{{ option[1] }}</option>
        </select>

        <!-- Advice Filter -->
        <select v-model="adviceFilter" class="ios-filter-select !w-auto" @change="applyFilters">
          <option value="all">全部调优状态</option>
          <option value="has_advice">有调优建议</option>
        </select>

        <!-- Server group (tag) filter -->
        <select v-model="tag" class="ios-filter-select !w-auto" @change="applyFilters">
          <option value="">全部服务器分组</option>
          <option v-for="item in tagChoices.tags" :key="item.tag" :value="item.tag">{{ item.tag }}（{{ item.count }}）</option>
          <option v-if="tagChoices.tags.length" :value="UNTAGGED">未分组（{{ tagChoices.untagged }}）</option>
        </select>

        <!-- Query Button -->
        <button class="ios-search-btn shrink-0">查询</button>

        <!-- Refresh Button -->
        <button type="button" class="ios-search-btn shrink-0" :disabled="loading" @click="load" title="刷新服务器状态">
          <svg class="w-3.5 h-3.5 text-ios-secondary" :class="{ 'animate-spin': loading }" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>刷新</span>
        </button>
      </form>

      <!-- View Switcher (List vs Grid) -->
      <div class="flex items-center gap-2 shrink-0 self-start lg:self-auto">
        <ServerDisplayMode :display-mode="displayMode" @change="setDisplayMode" />
      </div>
    </div>
    <p v-if="isOwner && !canReorder && filtered.length > 1" class="text-xs text-ios-tertiary -mt-3">清除筛选并回到第一页后可调整服务器顺序。</p>

    <p v-if="error && nodes.length" role="status" class="text-sm text-ios-red">{{ error }}</p>
    <ErrorState v-if="error && !nodes.length" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <template v-else>
      <p v-if="warnings.length" role="status" class="text-sm text-ios-orange">{{ warnings.join('；') }}</p>
      <EmptyState v-if="!filtered.length" title="没有匹配的服务器" description="请调整筛选条件，或新增服务器并完成登记。" />
      <ServerCardGrid v-else-if="displayMode === 'grid'" :nodes="filtered" :clock-now="now" :machine-traffic="machineTraffic" :reorderable="canReorder" :can-add-relay="canAddRelay" @add-relay="openQuickRelay" @install="install" @edit="quickEdit?.open($event)" @delete="deletion?.prompt($event)" @move="move" @reorder="reorder" />
      <ServerInventory v-else :rows="pageRows" :columns="columns" :owner="isOwner" :selected="selected" :reorderable="canReorder" @toggle="toggle" @install="enrollment?.reissue($event)" @delete="deletion?.prompt($event)" @move="move" />
      <p v-if="displayMode === 'list' && filtered.length && hiddenColumns.length" class="text-xs text-ios-tertiary">当前筛选的服务器均无可用数据，已隐藏：{{ hiddenColumns.join('、') }}。</p>
      <Pagination v-if="displayMode === 'list' && filtered.length > 20" :total="filtered.length" :current-page="page" :page-size="20" @update:page="changePage" />
    </template>
    <ServerBulkActions v-if="isOwner && displayMode === 'list'" :selected="selectedNodes" :groups="groups" @clear="selected = []" @changed="load" />
    <ServerQuickEdit v-if="isOwner" ref="quickEdit" :known-tags="tagChoices.tags.map(item => item.tag)" @changed="load" />
    <ServerEnrollment v-if="isOwner" ref="enrollment" @changed="load" />
    <ServerDeleteDialog v-if="isOwner" ref="deletion" @changed="load" />
    <QuickRelayDialog v-if="isOwner && quickRelayNodeId" :servers="nodes" :inbounds="relayInbounds" :groups="relayGroups" :initial-entry-node-id="quickRelayNodeId" @close="quickRelayNodeId = ''" @created="load" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, provide, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ServerCardGrid from '../components/ServerCardGrid.vue';
import ServerQuickEdit from '../components/ServerQuickEdit.vue';
import ServerDisplayMode from '../components/ServerDisplayMode.vue';
import { showToast } from '../../../app/api';
import { authState, isOwner } from '../../../app/auth';
import { throttleTrailing } from '../../../app/nodeStatusEvents';
import { localInput, utcInput } from '../../../app/time';
import { request } from '../../../app/apiClient';
import { nodeAPI, type MachineTrafficSummary, type NodeView } from '../api';
import { groupAPI, type NodeGroup } from '../groupsApi';
import { choices, proxyAPI, type Inbound } from '../../proxy/api';
import QuickRelayDialog from '../../proxy/components/QuickRelayDialog.vue';
import { accessAPI } from '../../access/api';
import { getNodeMeta, loadNodeMeta } from '../nodeMeta';
import { serverRow, serverStatus, statusOptions } from '../components/serverListModel';
import { applyServerOrder, matchesTag, moveServer, reorderServer, tagOptions, UNTAGGED } from '../serverOrder';
import { APIError } from '../../../app/apiClient';
import PageHeader from '../../../components/PageHeader.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ServerInventory from '../components/ServerInventory.vue';
import ServerEnrollment from '../components/ServerEnrollment.vue';
import ServerDeleteDialog from '../components/ServerDeleteDialog.vue';
import ServerBulkActions from '../components/ServerBulkActions.vue';
const displayMode = ref<'grid' | 'list'>(localStorage.getItem('qui_server_display_mode') === 'list' ? 'list' : 'grid');
function setDisplayMode(mode: 'grid' | 'list') {
  displayMode.value = mode;
  localStorage.setItem('qui_server_display_mode', mode);
}
const quickEdit = ref<InstanceType<typeof ServerQuickEdit>>();
const machineTraffic = ref<Record<string, MachineTrafficSummary>>({});
function install(node: NodeView) {
  if (node.status === 'pending' && !node.deletedAt) void enrollment.value?.reissue(node);
  else showToast('该服务器已登记；如需重装或升级请使用 qui-update（见部署文档）', 'info');
}
const route = useRoute(), router = useRouter();
const scalar = (value: unknown) => typeof value === 'string' ? value : '';
const keyword = ref(''), status = ref('all'), adviceFilter = ref('all'), tag = ref(''), deleted = ref(false), page = ref(1);
const nodes = ref<NodeView[]>([]), groups = ref<NodeGroup[]>([]);
const loading = ref(false), error = ref(''), warnings = ref<string[]>([]), selected = ref<string[]>([]);
const today = ref<Record<string, number>>({}), month = ref<Record<string, number>>({}), monthZone = ref('');
const now = ref(Date.now());
const quickRelayNodeId = ref('');
const relayInbounds = ref<Inbound[]>([]), relayGroups = ref<NodeGroup[]>([]);
const relayLoading = ref(false);
function canAddRelay(node: NodeView) {
  return isOwner.value && node.enabled && !node.deletedAt && serverStatus(node, now.value).key === 'online';
}
async function openQuickRelay(node: NodeView) {
  if (!canAddRelay(node) || relayLoading.value) return;
  relayLoading.value = true;
  try {
    const [inbounds, availableGroups] = await Promise.all([
      choices((page, size) => proxyAPI.inbounds('', page, size)), choices(groupAPI.list)
    ]);
    relayInbounds.value = inbounds;
    relayGroups.value = availableGroups;
    quickRelayNodeId.value = node.id;
  } catch (e) {
    showToast(`读取中转选项失败：${(e as Error).message}`, 'error');
  } finally { relayLoading.value = false; }
}
// Row menus are nested inside ServerInventory; keep the dialog owned by this page.
provide('serverQuickRelay', { canOpen: canAddRelay, open: openQuickRelay });
const enrollment = ref<InstanceType<typeof ServerEnrollment>>(), deletion = ref<InstanceType<typeof ServerDeleteDialog>>();
function readQuery() {
  keyword.value = scalar(route.query.keyword);
  const raw = scalar(route.query.status);
  status.value = statusOptions.some(option => option[0] === raw) ? raw : 'all';
  adviceFilter.value = route.query.advice === 'has_advice' ? 'has_advice' : 'all';
  tag.value = scalar(route.query.tag);
  deleted.value = route.query.deleted === '1';
  const value = Number(route.query.page);
  page.value = Number.isInteger(value) && value > 0 ? value : 1;
}
readQuery();
function applyFilters() {
  selected.value = [];
  void router.replace({ query: { ...route.query, keyword: keyword.value.trim() || undefined,
    status: status.value === 'all' ? undefined : status.value,
    advice: adviceFilter.value === 'has_advice' ? 'has_advice' : undefined,
    tag: tag.value || undefined, group: undefined,
    deleted: deleted.value ? '1' : undefined, page: undefined } });
}
// The layout keeps a leaving page mounted during its out-in transition while
// route already describes the next page; ignore routes that are not ours.
const ours = () => route.path === '/nodes';
function changePage(value: number) { if (!ours()) return; void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } }); }
// v1.5.5 item 3: the Owner's saved order. `orderIds` is what the page shows;
// `savedOrder` is the last order the panel accepted, restored when a save fails.
const savedOrder = ref<{ ids: string[]; revision: number }>({ ids: [], revision: 0 });
const orderIds = ref<string[]>([]);
const ordered = computed(() => applyServerOrder(nodes.value, orderIds.value));
const tagChoices = computed(() => tagOptions(nodes.value));
const filtered = computed(() => ordered.value.filter(node => {
  const q = scalar(route.query.keyword).trim().toLowerCase();
  const meta = getNodeMeta(node.id);
  if (q && ![node.name, node.publicIp, node.connectHost, node.hostname, meta.region, meta.remark, ...(node.tags || [])].some(value => value?.toLowerCase().includes(q))) return false;
  if (status.value !== 'all' && serverStatus(node, now.value).key !== status.value) return false;
  if (adviceFilter.value === 'has_advice' && (!node.tuningAdviceCount || node.tuningAdviceCount <= 0)) return false;
  return matchesTag(node, tag.value);
}));
// Ordering only a filtered subset or one page would be ambiguous, so it needs the
// full, unfiltered first page.
const canReorder = computed(() => isOwner.value && !deleted.value && !scalar(route.query.keyword).trim() && status.value === 'all' && adviceFilter.value === 'all' && !tag.value && page.value === 1 && filtered.value.length > 1);
let saveTimer: ReturnType<typeof setTimeout> | undefined;
function setOrder(ids: string[]) {
  orderIds.value = ids;
  // Quick successive moves are merged into one save.
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => { void saveOrder(); }, 500);
}
async function saveOrder() {
  const ids = ordered.value.filter(node => !node.deletedAt).map(node => node.id);
  try {
    const result = await nodeAPI.saveServerOrder(ids, savedOrder.value.revision);
    savedOrder.value = { ids: result.ids, revision: result.revision };
  } catch (e) {
    const conflict = e instanceof APIError && e.status === 409;
    showToast(conflict ? '服务器顺序已在别处修改，已重新读取' : `保存顺序失败：${(e as Error).message}`, 'error');
    if (conflict) {
      const latest = await nodeAPI.serverOrder().catch(() => null);
      if (latest) savedOrder.value = { ids: latest.ids, revision: latest.revision };
    }
    orderIds.value = savedOrder.value.ids;
  }
}
function visibleIds() { return ordered.value.map(node => node.id); }
function move(id: string, delta: -1 | 1) { setOrder(moveServer(visibleIds(), id, delta)); }
function reorder(fromId: string, toId: string) { setOrder(reorderServer(visibleIds(), fromId, toId)); }
const rows = computed(() => filtered.value.map(node => serverRow(node, now.value, today.value[node.id], month.value[node.id])));
const columns = computed(() => (rows.value[0]?.fields || []).filter(field => rows.value.some(row => row.fields.some(item => item.key === field.key && item.value !== '—'))));
const hiddenColumns = computed(() => (rows.value[0]?.fields || []).filter(field => !columns.value.some(item => item.key === field.key)).map(field => field.label));
const pageRows = computed(() => rows.value.slice((page.value - 1) * 20, page.value * 20));
const selectedNodes = computed(() => nodes.value.filter(node => selected.value.includes(node.id) && !node.deletedAt));
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id]; }
let generation = 0, refreshing = false;
async function load() {
  const current = ++generation;
  loading.value = true; error.value = '';
  try {
    const supplements = Promise.allSettled([
      isOwner.value ? choices(groupAPI.list) : Promise.resolve([] as NodeGroup[]),
      request<{ items: { nodeId: string; uploadBytes: number; downloadBytes: number }[]; timezone: string }>('/traffic/nodes/monthly'),
      nodeAPI.machineTrafficBatch()
    ]);
    void nodeAPI.serverOrder().then(order => {
      if (current !== generation) return;
      savedOrder.value = { ids: order.ids, revision: order.revision }; orderIds.value = order.ids;
    }).catch(e => {
      if (current === generation) warnings.value = [`服务器顺序读取失败：${(e as Error).message}`];
    });
    const servers = await choices((index, size) => nodeAPI.list(index, '', deleted.value, size));
    if (current !== generation) return;
    // Show the inventory immediately; optional traffic and metadata fill in later.
    nodes.value = servers;
    selected.value = selected.value.filter(id => servers.some(node => node.id === id && !node.deletedAt));
    const notices: string[] = [];
    // Midnight can fall in a DST gap in a few zones; fall back to UTC midnight then.
    let dayStart: string;
    try { dayStart = utcInput(localInput(new Date().toISOString()).slice(0, 10) + 'T00:00'); }
    catch { const utc = new Date(); utc.setUTCHours(0, 0, 0, 0); dayStart = utc.toISOString(); }
    const dayEnd = new Date().toISOString();
    const details = await Promise.all(servers.map(async node => {
      const [meta, traffic] = await Promise.allSettled([
        loadNodeMeta(node.id, isOwner.value), accessAPI.accounts({ nodeId: node.id, from: dayStart, to: dayEnd })
      ]);
      if (meta.status === 'rejected') notices.push(`${node.name} 元数据读取失败`);
      if (traffic.status === 'rejected') notices.push(`${node.name} 今日流量读取失败`);
      return { id: node.id, failed: traffic.status === 'rejected', bytes: traffic.status === 'fulfilled' && traffic.value.observations > 0 ? traffic.value.uploadBytes + traffic.value.downloadBytes : undefined };
    }));
    const [groupResult, monthly, machine] = await supplements;
    if (current !== generation) return;
    if (groupResult.status === 'rejected') notices.push(`节点分组读取失败：${(groupResult.reason as Error).message}`);
    if (monthly.status === 'rejected') notices.push(`本月流量读取失败：${(monthly.reason as Error).message}`);
    if (machine.status === 'rejected') notices.push(`本月整机流量读取失败：${(machine.reason as Error).message}`);
    groups.value = groupResult.status === 'fulfilled' ? groupResult.value : groups.value;
    for (const item of details) { if (!item.failed) { if (item.bytes === undefined) delete today.value[item.id]; else today.value[item.id] = item.bytes; } }
    month.value = monthly.status === 'fulfilled' ? Object.fromEntries(monthly.value.items.map(item => [item.nodeId, item.uploadBytes + item.downloadBytes])) : month.value;
    machineTraffic.value = machine.status === 'fulfilled' ? Object.fromEntries(machine.value.items.map(item => [item.nodeId, item])) : machineTraffic.value;
    monthZone.value = monthly.status === 'fulfilled' ? monthly.value.timezone : monthZone.value;
    warnings.value = notices;
    if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}

async function silentRefresh() {
  if (loading.value || refreshing || document.visibilityState !== 'visible') return;
  refreshing = true;
  const current = generation;
  try {
    const servers = await choices((index, size) => nodeAPI.list(index, '', deleted.value, size));
    if (current !== generation) return;
    nodes.value = servers;
    selected.value = selected.value.filter(id => servers.some(node => node.id === id && !node.deletedAt));
    if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
  } catch (e) {
    if (current === generation) warnings.value = [(e as Error).message];
  } finally { refreshing = false; }
}

const throttledSilentRefresh = throttleTrailing(() => {
  void silentRefresh();
}, 5000);

function onNodeStatus() {
  throttledSilentRefresh();
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void silentRefresh();
  }
}

watch(() => route.query, () => {
  const wasDeleted = deleted.value;
  readQuery();
  if (ours() && wasDeleted !== deleted.value) void load();
});
let timer: ReturnType<typeof setInterval> | undefined;
let fallbackTimer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void load();
  timer = setInterval(() => { now.value = Date.now(); }, 5000);
  fallbackTimer = setInterval(() => {
    if (document.visibilityState === 'visible') void silentRefresh();
  }, 30000);
  window.addEventListener('proxy-node-status', onNodeStatus);
  document.addEventListener('visibilitychange', onVisibilityChange);
});
onUnmounted(() => {
  generation++;
  clearTimeout(saveTimer);
  clearInterval(timer);
  clearInterval(fallbackTimer);
  throttledSilentRefresh.cancel();
  window.removeEventListener('proxy-node-status', onNodeStatus);
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>
