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

        <!-- Server group (tag) filter -->
        <select v-model="tag" class="ios-filter-select !w-auto" @change="applyFilters">
          <option value="">全部服务器分组</option>
          <option v-for="item in tagChoices.tags" :key="item.tag" :value="item.tag">{{ item.tag }}（{{ item.count }}）</option>
          <option v-if="tagChoices.tags.length" :value="UNTAGGED">未分组（{{ tagChoices.untagged }}）</option>
        </select>

        <!-- Refresh Button -->
        <button type="button" class="ios-search-btn shrink-0" :disabled="loading" @click="refreshAll" title="刷新服务器状态">
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
      <p v-if="warnings.length || inventoryWarnings.length" role="status" class="text-sm text-ios-orange">{{ [...warnings, ...inventoryWarnings].join('；') }}</p>
      <p v-if="filtered.length && statisticsNotice" role="status" class="text-xs text-ios-secondary">{{ statisticsNotice }}</p>
      <p v-if="filtered.length && displayMode === 'grid' && statistics?.machineError" role="status" class="text-xs text-ios-orange">整机流量读取失败（{{ statistics.machineError }}）</p>
      <p v-if="statisticsHistoryError" role="status" class="text-xs text-ios-orange">本月流量无法精确计算（{{ statisticsHistoryError }}）</p>
      <EmptyState v-if="!filtered.length" title="没有匹配的服务器" description="请调整筛选条件，或新增服务器并完成登记。" />
      <ServerCardGrid v-else-if="displayMode === 'grid'" :nodes="filtered" :clock-now="now" :machine-traffic="machineTraffic" :reorderable="canReorder" @install="install" @edit="quickEdit?.open($event)" @delete="deletion?.prompt($event)" @reorder="reorder" />
      <ServerInventory v-else :rows="pageRows" :columns="columns" :owner="isOwner" :selected="selected" @toggle="toggle" @install="enrollment?.reissue($event)" @delete="deletion?.prompt($event)" />
      <p v-if="displayMode === 'list' && filtered.length && hiddenColumns.length" class="text-xs text-ios-tertiary">当前筛选的服务器均无可用数据，已隐藏：{{ hiddenColumns.join('、') }}。</p>
      <Pagination v-if="displayMode === 'list' && filtered.length > 20" :total="filtered.length" :current-page="page" :page-size="20" @update:page="changePage" />
    </template>
    <ServerBulkActions v-if="isOwner && displayMode === 'list'" :selected="selectedNodes" :groups="groups" @clear="selected = []" @changed="load" />
    <ServerQuickEdit v-if="isOwner" ref="quickEdit" :known-tags="tagChoices.tags.map(item => item.tag)" @changed="load" />
    <ServerEnrollment v-if="isOwner" ref="enrollment" :known-tags="tagChoices.tags.map(item => item.tag)" @changed="load" />
    <ServerDeleteDialog v-if="isOwner" ref="deletion" @changed="load" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';
import ServerCardGrid from '../components/ServerCardGrid.vue';
import ServerQuickEdit from '../components/ServerQuickEdit.vue';
import ServerDisplayMode from '../components/ServerDisplayMode.vue';
import { showToast } from '../../../app/api';
import { isOwner } from '../../../app/auth';
import { nodeStatusEventChangesInventory, throttleTrailing } from '../../../app/nodeStatusEvents';
import { APIError, request } from '../../../app/apiClient';
import { useStatistics } from '../../../app/useStatistics';
import { nodeAPI, type NodeView } from '../api';
import { groupAPI, type NodeGroup } from '../groupsApi';
import { choices } from '../../proxy/api';
import { getNodeMeta, loadNodeMetadata } from '../nodeMeta';
import { serverRow } from '../components/serverListModel';
import { applyServerOrder, matchesTag, reorderServer, tagOptions, UNTAGGED } from '../serverOrder';
import { applyRuntimeSnapshot, RuntimeFeed, type RuntimeSnapshot, type RuntimeSnapshotList } from '../runtimeSnapshots';
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
function install(node: NodeView) {
  if (!node.deletedAt) void enrollment.value?.reissue(node);
}
const route = useRoute(), router = useRouter();
const scalar = (value: unknown) => typeof value === 'string' ? value : '';
const keyword = ref(''), tag = ref(''), deleted = ref(false), page = ref(1);
const nodes = ref<NodeView[]>([]), groups = ref<NodeGroup[]>([]);
const loading = ref(false), error = ref(''), warnings = ref<string[]>([]), selected = ref<string[]>([]);
const inventoryWarnings = ref<string[]>([]);
const now = ref(Date.now());
const statusFresh = ref(false);
const enrollment = ref<InstanceType<typeof ServerEnrollment>>(), deletion = ref<InstanceType<typeof ServerDeleteDialog>>();
function readQuery() {
  keyword.value = scalar(route.query.keyword);
  tag.value = scalar(route.query.tag);
  deleted.value = route.query.deleted === '1';
  const value = Number(route.query.page);
  page.value = Number.isInteger(value) && value > 0 ? value : 1;
}
readQuery();
function applyFilters() {
  selected.value = [];
  void router.replace({ query: { ...route.query, keyword: keyword.value.trim() || undefined,
    status: undefined, advice: undefined,
    tag: tag.value || undefined, group: undefined,
    deleted: deleted.value ? '1' : undefined, page: undefined } });
}
// The layout keeps a leaving page mounted during its out-in transition while
// route already describes the next page; ignore routes that are not ours.
const ours = () => route.path === '/nodes';
const { data: statistics, notice: statisticsNotice, refresh: refreshStatistics } = useStatistics(() => ours() && !deleted.value ? '' : undefined);
const trafficByNode = computed(() => new Map(statistics.value?.nodeTraffic.map(item => [item.nodeId, item])));
const machineTraffic = computed(() => Object.fromEntries(statistics.value?.machineTraffic.map(item => [item.nodeId, item]) ?? []));
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
  return matchesTag(node, tag.value);
}));
// Ordering only a filtered subset or one page would be ambiguous, so it needs the
// full, unfiltered first page.
const canReorder = computed(() => ours() && isOwner.value && !deleted.value && !scalar(route.query.keyword).trim() && !tag.value && page.value === 1 && filtered.value.length > 1);
let saveTimer: ReturnType<typeof setTimeout> | undefined;
let orderDirty = false;
let orderSavePromise: Promise<void> | undefined;
function setOrder(ids: string[]) {
  orderIds.value = ids;
  orderDirty = true;
  // Quick successive moves are merged into one save.
  clearTimeout(saveTimer);
  saveTimer = setTimeout(() => { void saveOrder(); }, 500);
}
function saveOrder(): Promise<void> {
  clearTimeout(saveTimer);
  // Route leave must await the whole queue, including edits made during a PUT.
  if (orderSavePromise) return orderSavePromise;
  if (!orderDirty) return Promise.resolve();
  orderSavePromise = persistOrder().finally(() => { orderSavePromise = undefined; });
  return orderSavePromise;
}
async function persistOrder() {
  while (orderDirty) {
    orderDirty = false;
    const ids = ordered.value.filter(node => !node.deletedAt).map(node => node.id);
    try {
      const result = await nodeAPI.saveServerOrder(ids, savedOrder.value.revision);
      savedOrder.value = { ids: result.ids, revision: result.revision };
    } catch (e) {
      const conflict = e instanceof APIError && e.status === 409;
      showToast(conflict ? '服务器顺序已在别处修改，已重新读取' : `保存顺序失败：${(e as Error).message}`, 'error');
      if (conflict) {
        try {
          const latest = await nodeAPI.serverOrder();
          savedOrder.value = { ids: latest.ids, revision: latest.revision };
        } catch (readError) {
          showToast(`服务器顺序读取失败：${(readError as Error).message}`, 'error');
          orderDirty = false;
        }
      }
      if (!orderDirty) orderIds.value = savedOrder.value.ids;
    }
  }
}
function visibleIds() { return ordered.value.map(node => node.id); }
function reorder(fromId: string, toId: string) { setOrder(reorderServer(visibleIds(), fromId, toId)); }
const rows = computed(() => filtered.value.map(node => {
  const traffic = trafficByNode.value.get(node.id);
  const today = traffic?.todayMeasured ? traffic.todayUpload + traffic.todayDownload : undefined;
  const month = traffic?.measured && !traffic.historyError ? traffic.monthUpload + traffic.monthDownload : undefined;
  return serverRow(node, now.value, today, month);
}));
const statisticsHistoryError = computed(() => displayMode.value === 'list'
  ? filtered.value.map(node => trafficByNode.value.get(node.id)?.historyError).find(Boolean)
  : undefined);
const columns = computed(() => (rows.value[0]?.fields || []).filter(field => rows.value.some(row => row.fields.some(item => item.key === field.key && item.value !== '—'))));
const hiddenColumns = computed(() => (rows.value[0]?.fields || []).filter(field => !columns.value.some(item => item.key === field.key)).map(field => field.label));
const pageRows = computed(() => rows.value.slice((page.value - 1) * 20, page.value * 20));
const selectedNodes = computed(() => nodes.value.filter(node => selected.value.includes(node.id) && !node.deletedAt));
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id]; }
const runtime = new RuntimeFeed();
let inventory: NodeView[] = [];
let inventoryScope: boolean | undefined;
let lastMembershipCheck = '';
let generation = 0, inventoryRead = 0, visibilityGeneration = 0, runtimeRead = 0;
let refreshingInventory = false, calibrating = false;
let inventoryInvalidation = 0, inventoryConfirmed = 0, lastInventorySuccess = 0;
const staticInventoryInterval = 5 * 60 * 1000;
let staticInventoryTimer: ReturnType<typeof setTimeout> | undefined;

function refreshAll() {
  void load();
  void refreshStatistics(true);
}

function updateVisibleInventory() {
  nodes.value = inventory.flatMap(base => {
    const node = applyRuntimeSnapshot(base, runtime.get(base.id));
    return node && Boolean(node.deletedAt) === deleted.value ? [node] : [];
  });
  selected.value = selected.value.filter(id => nodes.value.some(node => node.id === id && !node.deletedAt));
  if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
  if (inventoryScope !== deleted.value || !runtime.generation || runtime.matchesInventory(inventory.map(node => node.id), deleted.value)) return;
  const revision = `${runtime.generation}:${runtime.inventoryRevision}:${deleted.value}`;
  if (lastMembershipCheck !== revision) {
    lastMembershipCheck = revision;
    refreshStaticInventory();
  }
}

async function load() {
  void calibrateRuntime();
  const current = ++generation;
  const read = ++inventoryRead;
  const invalidation = inventoryInvalidation;
  const visibleGeneration = visibilityGeneration;
  loading.value = true; error.value = '';
  try {
    const groupsRead = Promise.allSettled([isOwner.value ? choices(groupAPI.list) : Promise.resolve([] as NodeGroup[])]);
    void nodeAPI.serverOrder().then(order => {
      if (current !== generation || orderDirty || orderSavePromise || order.revision < savedOrder.value.revision) return;
      savedOrder.value = { ids: order.ids, revision: order.revision }; orderIds.value = order.ids;
    }).catch(e => {
      if (current === generation) warnings.value = [`服务器顺序读取失败：${(e as Error).message}`];
    });
    const servers = await choices((index, size) => nodeAPI.list(index, '', deleted.value, size));
    if (current !== generation) return;
    // Show the inventory immediately; metadata and statistics load independently.
    if (read === inventoryRead && visibleGeneration === visibilityGeneration) {
      inventory = servers;
      inventoryScope = deleted.value;
      updateVisibleInventory();
    } else if (read === inventoryRead) {
      refreshStaticInventory();
    }
    const notices: string[] = [];
    const [metaResult] = await Promise.allSettled([loadNodeMetadata(isOwner.value, deleted.value)]);
    if (read === inventoryRead && visibleGeneration === visibilityGeneration) {
      inventoryWarnings.value = metaResult.status === 'rejected' ? [`元数据读取失败：${(metaResult.reason as Error).message}`] : [];
      if (metaResult.status === 'fulfilled') confirmInventory(invalidation);
      else inventoryInvalidation++;
    }
    const [groupResult] = await groupsRead;
    if (current !== generation) return;
    if (groupResult.status === 'rejected') notices.push(`节点分组读取失败：${(groupResult.reason as Error).message}`);
    else groups.value = groupResult.value;
    warnings.value = notices;
  } catch (e) {
    if (current === generation) {
      if (read === inventoryRead) inventoryInvalidation++;
      error.value = (e as Error).message;
    }
  }
  finally { if (current === generation) loading.value = false; }
}

function confirmInventory(invalidation: number) {
  inventoryConfirmed = invalidation;
  lastInventorySuccess = Date.now();
  clearTimeout(staticInventoryTimer);
  staticInventoryTimer = setTimeout(recheckStaticInventory, staticInventoryInterval);
}

function recheckStaticInventory() {
  if (ours() && document.visibilityState === 'visible' && Date.now() - lastInventorySuccess >= staticInventoryInterval) {
    refreshStaticInventory();
  }
}

async function silentRefresh() {
  if (!ours() || document.visibilityState !== 'visible' || refreshingInventory || inventoryInvalidation === inventoryConfirmed) return;
  refreshingInventory = true;
  const read = ++inventoryRead;
  const invalidation = inventoryInvalidation;
  const visibleGeneration = visibilityGeneration;
  try {
    const [servers, metadata] = await Promise.allSettled([
      choices((index, size) => nodeAPI.list(index, '', deleted.value, size)),
      loadNodeMetadata(isOwner.value, deleted.value),
    ]);
    if (read !== inventoryRead || visibleGeneration !== visibilityGeneration) return;
    const notices: string[] = [];
    if (servers.status === 'fulfilled') {
      inventory = servers.value;
      inventoryScope = deleted.value;
      updateVisibleInventory();
    } else notices.push(`服务器清单读取失败：${(servers.reason as Error).message}`);
    if (metadata.status === 'rejected') notices.push(`元数据读取失败：${(metadata.reason as Error).message}`);
    inventoryWarnings.value = notices;
    if (!notices.length) confirmInventory(invalidation);
  } finally {
    refreshingInventory = false;
    // New changes can queue behind an in-flight read. The same failed change
    // waits for the next calibration and does not retry in this call stack.
    if (inventoryInvalidation > invalidation) throttledSilentRefresh();
  }
}

const throttledSilentRefresh = throttleTrailing(() => {
  void silentRefresh();
}, 5000);

function refreshStaticInventory() {
  inventoryInvalidation++;
  throttledSilentRefresh();
}

function onNodeStatus(event: Event) {
  if (nodeStatusEventChangesInventory(event)) refreshStaticInventory();
}

async function calibrateRuntime() {
  if (!ours() || calibrating || document.visibilityState !== 'visible') return;
  calibrating = true;
  const read = runtimeRead;
  try {
    const snapshot = await request<RuntimeSnapshotList>('/nodes/runtime');
    if (!ours() || read !== runtimeRead) return;
    const previousGeneration = runtime.generation;
    const previousInventory = runtime.inventoryRevision;
    if (!runtime.calibrate(snapshot)) return;
    updateVisibleInventory();
    now.value = Date.now();
    statusFresh.value = true;
    if (previousGeneration && (previousGeneration !== runtime.generation || previousInventory !== runtime.inventoryRevision)) {
      refreshStaticInventory();
    } else if (inventoryInvalidation > inventoryConfirmed) {
      throttledSilentRefresh();
    }
  } catch (e) {
    if (read === runtimeRead) console.error('服务器状态校准失败', e);
  } finally {
    calibrating = false;
    if (read !== runtimeRead) void calibrateRuntime();
  }
}

function freezeRuntime() {
  runtimeRead++;
  statusFresh.value = false;
}

function onRuntimeOpen() {
  runtime.resetStream();
  freezeRuntime();
  void calibrateRuntime();
}

function onRuntimeDisconnected() {
  runtime.resetStream();
  freezeRuntime();
}

function onNodeRuntime(event: Event) {
  const snapshot = (event as CustomEvent<RuntimeSnapshot>).detail;
  const previousInventory = runtime.inventoryRevision;
  const update = runtime.receive(snapshot, statusFresh.value);
  if (statusFresh.value && snapshot.generation !== runtime.generation) {
    freezeRuntime();
    void calibrateRuntime();
  }
  if (runtime.inventoryRevision !== previousInventory) refreshStaticInventory();
  if (!update) return;
  const index = nodes.value.findIndex(node => node.id === update.nodeId);
  if (index < 0) return;
  now.value = Date.now();
  const node = applyRuntimeSnapshot(nodes.value[index]!, update);
  if (!node || Boolean(node.deletedAt) !== deleted.value) {
    nodes.value.splice(index, 1);
    selected.value = selected.value.filter(id => id !== update.nodeId);
    if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
  } else {
    nodes.value[index] = node;
  }
}

function onVisibilityChange() {
  if (document.visibilityState === 'visible') {
    void calibrateRuntime();
    recheckStaticInventory();
  } else {
    visibilityGeneration++;
    freezeRuntime();
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
  window.addEventListener('proxy-node-runtime', onNodeRuntime);
  window.addEventListener('proxy-node-runtime-open', onRuntimeOpen);
  window.addEventListener('proxy-node-runtime-disconnected', onRuntimeDisconnected);
  window.addEventListener('proxy-node-status', onNodeStatus);
  window.addEventListener('proxy-deployment-status', refreshStaticInventory);
  document.addEventListener('visibilitychange', onVisibilityChange);
  void load();
  // Keep the last snapshot's clock until the foreground refresh succeeds.
  timer = setInterval(() => { if (document.visibilityState === 'visible' && statusFresh.value) now.value = Date.now(); }, 5000);
  fallbackTimer = setInterval(() => {
    if (document.visibilityState === 'visible') void calibrateRuntime();
  }, 30000);
});
onBeforeRouteLeave(async () => {
  // The out-in layout creates a fresh queue when this page is entered again.
  await saveOrder();
});
onUnmounted(() => {
  generation++;
  inventoryRead++;
  visibilityGeneration++;
  runtimeRead++;
  void saveOrder();
  clearInterval(timer);
  clearInterval(fallbackTimer);
  clearTimeout(staticInventoryTimer);
  throttledSilentRefresh.cancel();
  window.removeEventListener('proxy-node-runtime', onNodeRuntime);
  window.removeEventListener('proxy-node-runtime-open', onRuntimeOpen);
  window.removeEventListener('proxy-node-runtime-disconnected', onRuntimeDisconnected);
  window.removeEventListener('proxy-node-status', onNodeStatus);
  window.removeEventListener('proxy-deployment-status', refreshStaticInventory);
  document.removeEventListener('visibilitychange', onVisibilityChange);
});
</script>
