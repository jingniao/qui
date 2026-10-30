<template>
  <section class="panel-card p-6 space-y-4">
    <h2 class="font-semibold text-ios-label">配置版本</h2>
    <div class="flex flex-wrap items-center justify-between gap-3">
      <p class="text-sm text-ios-label">运行中 {{ strip.running ? '#' + strip.running : '—' }} · 最新 {{ strip.latest ? '#' + strip.latest : '—' }} {{ strip.state === 'latest' ? '✓' : strip.state === 'unapplied' ? '⚠ 未应用' : '（尚无版本信息）' }}</p>
      <button v-if="isOwner && !node.deletedAt" class="ios-btn-primary" :disabled="busy" @click="publishCurrent">发布配置</button>
    </div>
    <p class="text-xs text-ios-secondary">当前配置 {{ node.heartbeat?.current_revision ? `#${node.heartbeat.current_revision}` : '—' }} · 成功应用版本 {{ node.heartbeat?.successful_revision ? `#${node.heartbeat.successful_revision}` : '—' }}</p>
    <p v-if="!isOwner" class="text-sm text-ios-secondary">当前权限无法读取配置版本及差异，请联系 Owner。</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <div v-if="isOwner" class="space-y-3"><div class="ios-segmented-container inline-flex"><button v-for="option in filters" :key="option.key" class="ios-segmented-item" :class="filter === option.key ? 'active' : ''" @click="filter = option.key; filterChosen = true">{{ option.label }}</button></div><p class="text-xs text-ios-secondary">筛选已加载的服务器页面；翻页后按同一条件筛选。</p></div>
    <LoadingSkeleton v-if="pageLoading" />
    <DataTable v-else-if="isOwner && visible.length">
      <template #header>
        <th>版本</th>
        <th>时间</th>
        <th>来源 / 摘要</th>
        <th>结果</th>
        <th>操作</th>
      </template>
      <template #body>
        <tr v-for="rev in visible" :key="rev.id">
          <td class="font-mono tabular-nums">#{{ rev.id }} <StatusBadge v-if="rev.id === strip.running" status="running" label="运行中" /></td>
          <td class="font-mono tabular-nums text-xs">{{ formatTime(rev.createdAt) }}</td>
          <td>{{ rev.summary.startsWith('auto:') ? '自动' : '手动' }} · {{ rev.summary }}</td>
          <td>
            <StatusBadge :status="rev.status === 'rolled_back' ? 'rollback' : rev.status" :label="status(rev.status)" />
            <p class="text-xs text-ios-secondary"><span :title="rev.resultCode">{{ revisionResult(rev.resultCode) }}</span></p>
          </td>
          <td>
            <div class="flex gap-2">
              <button class="ios-action-btn-secondary" @click="openDiff(rev)">查看差异</button>
              <button v-if="isOwner && !node.deletedAt && rev.status === 'draft'" class="ios-action-btn-primary" :disabled="busy" @click="confirm('deploy', rev.id)">发布</button>
              <button v-if="!node.deletedAt && rev.status === 'success' && rev.id !== node.heartbeat?.successful_revision" class="ios-action-btn-secondary" :disabled="busy" @click="confirm('rollback', rev.id)">回滚到此版本</button>
            </div>
          </td>
        </tr>
      </template>
    </DataTable>
    <p v-else-if="isOwner" class="text-sm text-ios-secondary">当前服务器页中没有符合筛选条件的配置版本。</p>
    <Pagination v-if="isOwner" :total="total" :page-size="20" :current-page="page" @update:page="loadPage" />
    <ConfirmDialog v-model="confirmOpen" :title="action === 'rollback' ? '回滚配置版本' : '发布配置'" :is-danger="action === 'rollback'" :message="action === 'current' ? '按当前已保存的节点、出站、出口组、代理链、分流规则与用户数据生成新版本并发布，服务器确认后生效。' : `确认将服务器 ${node.name} ${action === 'rollback' ? '回滚到' : '发布'}配置 #${target}？`" @confirm="execute" />
    <AppDialog v-model="diffOpen" title="配置版本差异" description="默认与服务器已确认的运行版本比较。" max-width="3xl" @close="clearDiff">
      <LoadingSkeleton v-if="diffLoading" />
      <ErrorState v-else-if="diffError" :message="diffError" @retry="loadDiff" />
      <ConfigDiffViewer v-else-if="diff" :diff="diff" />
    </AppDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import { deploymentAPI, type Revision, type RevisionDiff } from '../../../system/deploymentApi';
import { isOwner, formatTime } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import Pagination from '../../../../components/Pagination.vue';
import StatusBadge from '../../../../components/StatusBadge.vue';
import { defaultRevisionFilter, filterRevisions, revisionStrip, revisionResult, type RevisionFilter } from './serverDetailModel';
import DataTable from '../../../../components/DataTable.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import AppDialog from '../../../../components/AppDialog.vue';
import ConfigDiffViewer from '../../../../components/ConfigDiffViewer.vue';
import LoadingSkeleton from '../../../../components/LoadingSkeleton.vue';
import ErrorState from '../../../../components/ErrorState.vue';
const props = defineProps<{ node: NodeView; latestRevision?: number; refresh: number }>();
const emit = defineEmits<{ changed: []; published: [] }>();
const items = ref<Revision[]>([]), page = ref(1), total = ref(0), pageLoading = ref(false);
const filter = ref<RevisionFilter>('all'), filterChosen = ref(false), initialized = ref(false);
const filters: { key: RevisionFilter; label: string }[] = [{ key: 'all', label: '全部' }, { key: 'manual', label: '手动' }, { key: 'auto', label: '自动' }];
const visible = computed(() => filterRevisions(items.value, filter.value));
const strip = computed(() => revisionStrip(props.node.heartbeat?.current_revision, props.latestRevision));
let pageGeneration = 0;
async function loadPage(next = page.value) {
  if (!isOwner.value) return;
  const current = ++pageGeneration; pageLoading.value = true; error.value = '';
  try { const result = await deploymentAPI.nodeRevisions(props.node.id, next, 20); if (current !== pageGeneration) return; items.value = result.items; page.value = result.page; total.value = result.total; if (!initialized.value && !filterChosen.value) filter.value = defaultRevisionFilter(result.items); initialized.value = true; }
  catch (e) { if (current === pageGeneration) error.value = (e as Error).message; }
  finally { if (current === pageGeneration) pageLoading.value = false; }
}
const confirmOpen = ref(false), busy = ref(false), error = ref(''), action = ref<'current' | 'deploy' | 'rollback'>('current'), target = ref(0);
const status = (value: string) => ({ draft: '草稿', pending: '待服务器确认', success: '已生效', failed: '失败', rolled_back: '已回滚' }[value] || value);
function confirm(kind: typeof action.value, revision = 0) { if (!isOwner.value || busy.value) return; action.value = kind; target.value = revision; confirmOpen.value = true; }
function publishCurrent() { confirm('current'); }
async function execute() {
  if (!isOwner.value || busy.value) return;
  busy.value = true; error.value = '';
  try {
    if (action.value === 'current') await deploymentAPI.publishNode(props.node.id, '手动发布当前配置');
    else if (action.value === 'rollback') await deploymentAPI.rollbackNode(props.node.id, target.value);
    else await deploymentAPI.deployRevision(target.value);
    showToast('请求已提交，等待服务器确认'); emit('published'); emit('changed');
  } catch (e) { error.value = (e as Error).message; showToast(error.value, 'error'); emit('changed'); }
  finally { busy.value = false; }
}
const diffOpen = ref(false), diffLoading = ref(false), diffError = ref(''), diff = ref<RevisionDiff | null>(null), diffTarget = ref(0);
let generation = 0;
function clearDiff() { generation++; diff.value = null; diffLoading.value = false; }
function openDiff(rev: Revision) { clearDiff(); diffTarget.value = rev.id; diffOpen.value = true; void loadDiff(); }
async function loadDiff() {
  const current = ++generation;
  diffLoading.value = true; diffError.value = ''; diff.value = null;
  try { const result = await deploymentAPI.diff(diffTarget.value); if (current === generation && diffOpen.value) diff.value = result; }
  catch (e) { if (current === generation) diffError.value = (e as Error).message; }
  finally { if (current === generation) diffLoading.value = false; }
}
watch(() => props.refresh, () => { void loadPage(); }, { immediate: true });
onUnmounted(() => { pageGeneration++; clearDiff(); });
defineExpose({ publishCurrent });
</script>
