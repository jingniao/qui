<template>
  <div class="space-y-6">
    <PageHeader title="服务器列表" parent="服务器" description="查看服务器运行状态、流量与续费信息。">
      <template #actions><button v-if="isOwner" class="ios-btn-primary" @click="enrollment?.open()">新增服务器</button></template>
    </PageHeader>
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="applyFilters">
      <label class="flex-1 text-xs text-ios-secondary">搜索<input v-model="keyword" class="form-input-base mt-1.5" placeholder="名称、IP、主机名、地区或备注" /></label>
      <label class="text-xs text-ios-secondary">状态<select v-model="status" class="form-input-base mt-1.5" @change="applyFilters"><option v-for="option in statusOptions" :key="option[0]" :value="option[0]">{{ option[1] }}</option></select></label>
      <label v-if="isOwner" class="text-xs text-ios-secondary">线路组<select v-model="group" class="form-input-base mt-1.5" @change="applyFilters"><option value="">全部线路组</option><option v-for="item in groups" :key="item.id" :value="item.id">{{ item.name }}</option></select></label>
      <label class="text-sm text-ios-secondary flex items-center gap-2 cursor-pointer"><input v-model="deleted" type="checkbox" @change="applyFilters" />显示已删除</label>
      <button class="ios-btn-secondary">查询</button>
      <button type="button" class="ios-btn-secondary" :disabled="loading" @click="load">刷新</button>
    </form>
    <ServerDisplayMode :display-mode="displayMode" @change="setDisplayMode" />
    <p v-if="!isOwner" class="text-xs text-ios-tertiary">线路组筛选需要 Owner 权限。</p>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <template v-else>
      <p v-if="warnings.length" role="status" class="text-sm text-ios-orange">{{ warnings.join('；') }}</p>
      <EmptyState v-if="!filtered.length" title="没有匹配的服务器" description="请调整筛选条件，或新增服务器并完成登记。" />
      <ServerCardGrid v-else-if="displayMode === 'grid'" :nodes="pageRows.map(row => row.node)" :clock-now="now" :node-month-traffic="monthDetails" @install="install" @edit="quickEdit?.open($event)" @delete="deletion?.prompt($event)" />
      <ServerInventory v-else :rows="pageRows" :columns="columns" :owner="isOwner" :selected="selected" @toggle="toggle" @install="enrollment?.reissue($event)" @delete="deletion?.prompt($event)" />
      <p v-if="displayMode === 'list' && filtered.length && hiddenColumns.length" class="text-xs text-ios-tertiary">当前筛选的服务器均无可用数据，已隐藏：{{ hiddenColumns.join('、') }}。</p>
      <p v-if="filtered.length" class="text-xs text-ios-tertiary">今日与本月流量按面板时区 {{ monthZone || authState.timezone }} 统计。流量来自保留的账户样本，不等于网卡总流量；过期心跳的实时指标不显示。</p>
      <Pagination v-if="filtered.length" :total="filtered.length" :current-page="page" :page-size="20" @update:page="changePage" />
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
function changePage(value: number) { void router.replace({ query: { ...route.query, page: value > 1 ? String(value) : undefined } }); }
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
    if (groupResult.status === 'rejected') notices.push(`线路组读取失败：${(groupResult.reason as Error).message}`);
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
  readQuery(); selected.value = [];
  if (page.value > Math.max(1, Math.ceil(filtered.value.length / 20))) changePage(1);
});
watch(() => route.query.deleted, () => { void load(); });
let timer: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  timer = setInterval(() => { now.value = Date.now(); }, 5000);
  void load(); window.addEventListener('proxy-node-status', load);
});
onUnmounted(() => {
  generation++; if (timer) clearInterval(timer);
  window.removeEventListener('proxy-node-status', load);
});
</script>
