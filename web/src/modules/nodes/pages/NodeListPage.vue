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

        <!-- Group Filter (Owner only) -->
        <select v-if="isOwner" v-model="group" class="ios-filter-select !w-auto" @change="applyFilters">
          <option value="">全部节点分组</option>
          <option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option>
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
    <p v-if="!isOwner" class="text-xs text-ios-tertiary -mt-3">节点分组筛选需要 Owner 权限。</p>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <template v-else>
      <p v-if="warnings.length" role="status" class="text-sm text-ios-orange">{{ warnings.join('；') }}</p>
      <EmptyState v-if="!filtered.length" title="没有匹配的服务器" description="请调整筛选条件，或新增服务器并完成登记。" />
      <ServerCardGrid v-else-if="displayMode === 'grid'" :nodes="filtered" :clock-now="now" :node-month-traffic="monthDetails" @install="install" @edit="quickEdit?.open($event)" @delete="deletion?.prompt($event)" />
      <ServerInventory v-else :rows="pageRows" :columns="columns" :owner="isOwner" :selected="selected" @toggle="toggle" @install="enrollment?.reissue($event)" @delete="deletion?.prompt($event)" />
      <p v-if="displayMode === 'list' && filtered.length && hiddenColumns.length" class="text-xs text-ios-tertiary">当前筛选的服务器均无可用数据，已隐藏：{{ hiddenColumns.join('、') }}。</p>
      <Pagination v-if="displayMode === 'list' && filtered.length > 20" :total="filtered.length" :current-page="page" :page-size="20" @update:page="changePage" />
    </template>
    <ServerBulkActions v-if="isOwner && displayMode === 'list'" :selected="selectedNodes" :groups="groups" @clear="selected = []" @changed="load" />
    <ServerQuickEdit v-if="isOwner" ref="quickEdit" @changed="load" />
    <ServerEnrollment v-if="isOwner" ref="enrollment" @changed="load" />
    <ServerDeleteDialog v-if="isOwner" ref="deletion" @changed="load" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import ServerCardGrid from '../components/ServerCardGrid.vue';
import ServerQuickEdit from '../components/ServerQuickEdit.vue';
import ServerDisplayMode from '../components/ServerDisplayMode.vue';
import { showToast } from '../../../app/api';
import { authState, isOwner } from '../../../app/auth';
import { localInput, utcInput } from '../../../app/time';
import { request } from '../../../app/apiClient';
import { nodeAPI, type NodeView } from '../api';
import { groupAPI, type NodeGroup } from '../groupsApi';
import { choices } from '../../proxy/api';
import { accessAPI } from '../../access/api';
import { getNodeMeta, loadNodeMeta } from '../nodeMeta';
import { serverRow, serverStatus, statusOptions } from '../components/serverListModel';
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
const monthDetails = ref<Record<string, { uploadBytes: number; downloadBytes: number }>>({});
function install(node: NodeView) {
  if (node.status === 'pending' && !node.deletedAt) void enrollment.value?.reissue(node);
  else showToast('该服务器已登记；如需重装或升级请使用 qui-update（见部署文档）', 'info');
}
const route = useRoute(), router = useRouter();
const scalar = (value: unknown) => typeof value === 'string' ? value : '';
const keyword = ref(''), status = ref('all'), group = ref(''), deleted = ref(false), page = ref(1);
const nodes = ref<NodeView[]>([]), groups = ref<NodeGroup[]>([]);
const loading = ref(false), error = ref(''), warnings = ref<string[]>([]), selected = ref<string[]>([]);
const today = ref<Record<string, number>>({}), month = ref<Record<string, number>>({}), monthZone = ref('');
const now = ref(Date.now());
const enrollment = ref<InstanceType<typeof ServerEnrollment>>(), deletion = ref<InstanceType<typeof ServerDeleteDialog>>();
function readQuery() {
  keyword.value = scalar(route.query.keyword);
  const raw = scalar(route.query.status);
  status.value = statusOptions.some(option => option[0] === raw) ? raw : 'all';
  group.value = isOwner.value ? scalar(route.query.group) : '';
  deleted.value = route.query.deleted === '1';
  const value = Number(route.query.page);
  page.value = Number.isInteger(value) && value > 0 ? value : 1;
}
readQuery();
function applyFilters() {
  selected.value = [];
  void router.replace({ query: { ...route.query, keyword: keyword.value.trim() || undefined,
    status: status.value === 'all' ? undefined : status.value, group: group.value || undefined,
    deleted: deleted.value ? '1' : undefined, page: undefined } });
}
// The layout keeps a leaving page mounted during its out-in transition while
// route already describes the next page; ignore routes that are not ours.
const ours = () => route.path === '/nodes';
function changePage(value: number) { if (!ours()) return; void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } }); }
const filtered = computed(() => nodes.value.filter(node => {
  const q = scalar(route.query.keyword).trim().toLowerCase();
  const meta = getNodeMeta(node.id);
  if (q && ![node.name, node.publicIp, node.hostname, meta.region, meta.remark].some(value => value?.toLowerCase().includes(q))) return false;
  if (status.value !== 'all' && serverStatus(node, now.value).key !== status.value) return false;
  return !group.value || groups.value.some(item => item.id === group.value && item.nodeIds.includes(node.id));
}));
const rows = computed(() => filtered.value.map(node => serverRow(node, now.value, today.value[node.id], month.value[node.id])));
const columns = computed(() => (rows.value[0]?.fields || []).filter(field => rows.value.some(row => row.fields.some(item => item.key === field.key && item.value !== '—'))));
const hiddenColumns = computed(() => (rows.value[0]?.fields || []).filter(field => !columns.value.some(item => item.key === field.key)).map(field => field.label));
const pageRows = computed(() => rows.value.slice((page.value - 1) * 20, page.value * 20));
const selectedNodes = computed(() => nodes.value.filter(node => selected.value.includes(node.id) && !node.deletedAt));
function toggle(id: string) { selected.value = selected.value.includes(id) ? selected.value.filter(value => value !== id) : [...selected.value, id]; }
let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true; error.value = '';
  try {
    const [servers, groupResult, monthly] = await Promise.allSettled([
      choices((index) => nodeAPI.list(index, '', deleted.value)),
      isOwner.value ? choices(groupAPI.list) : Promise.resolve([] as NodeGroup[]),
      request<{ items: { nodeId: string; uploadBytes: number; downloadBytes: number }[]; timezone: string }>('/traffic/nodes/monthly')
    ]);
    if (servers.status === 'rejected') throw servers.reason;
    const notices: string[] = [];
    if (groupResult.status === 'rejected') notices.push(`节点分组读取失败：${(groupResult.reason as Error).message}`);
    if (monthly.status === 'rejected') notices.push(`本月流量读取失败：${(monthly.reason as Error).message}`);
    // Midnight can fall in a DST gap in a few zones; fall back to UTC midnight then.
    let dayStart: string;
    try { dayStart = utcInput(localInput(new Date().toISOString()).slice(0, 10) + 'T00:00'); }
    catch { const utc = new Date(); utc.setUTCHours(0, 0, 0, 0); dayStart = utc.toISOString(); }
    const dayEnd = new Date().toISOString();
    const details = await Promise.all(servers.value.map(async node => {
      const [meta, traffic] = await Promise.allSettled([
        loadNodeMeta(node.id, isOwner.value), accessAPI.accounts({ nodeId: node.id, from: dayStart, to: dayEnd })
      ]);
      if (meta.status === 'rejected') notices.push(`${node.name} 元数据读取失败`);
      if (traffic.status === 'rejected') notices.push(`${node.name} 今日流量读取失败`);
      return { id: node.id, bytes: traffic.status === 'fulfilled' && traffic.value.observations > 0 ? traffic.value.uploadBytes + traffic.value.downloadBytes : undefined };
    }));
    if (current !== generation) return;
    nodes.value = servers.value;
    groups.value = groupResult.status === 'fulfilled' ? groupResult.value : [];
    today.value = Object.fromEntries(details.filter(item => item.bytes !== undefined).map(item => [item.id, item.bytes! ]));
    month.value = monthly.status === 'fulfilled' ? Object.fromEntries(monthly.value.items.map(item => [item.nodeId, item.uploadBytes + item.downloadBytes])) : {};
    monthDetails.value = monthly.status === 'fulfilled' ? Object.fromEntries(monthly.value.items.map(item => [item.nodeId, item])) : {};
    monthZone.value = monthly.status === 'fulfilled' ? monthly.value.timezone : '';
    warnings.value = notices;
    selected.value = selected.value.filter(id => nodes.value.some(node => node.id === id && !node.deletedAt));
    if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
watch(() => route.query, () => {
  readQuery();
  if (ours()) void load();
});
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  void load();
  timer = setInterval(() => { now.value = Date.now(); }, 5000);
});
onUnmounted(() => { clearInterval(timer); });
</script>
