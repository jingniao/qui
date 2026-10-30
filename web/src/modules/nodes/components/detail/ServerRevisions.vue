<template>
  <section class="panel-card p-6 space-y-4">
    <div class="flex justify-between gap-3">
      <h2 class="font-semibold text-ios-label">配置版本</h2>
      <button v-if="isOwner && !node.deletedAt" class="ios-btn-primary" @click="publishCurrent">发布配置</button>
    </div>
    <p class="text-xs text-ios-secondary">当前配置 {{ node.heartbeat?.current_revision ? `#${node.heartbeat.current_revision}` : '—' }} · 成功应用版本 {{ node.heartbeat?.successful_revision ? `#${node.heartbeat.successful_revision}` : '—' }}</p>
    <p v-if="!isOwner" class="text-sm text-ios-secondary">当前权限无法读取配置版本及差异，请联系 Owner。</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <DataTable v-if="isOwner && revisions.length">
      <template #header>
        <th>版本</th>
        <th>时间</th>
        <th>来源 / 摘要</th>
        <th>结果</th>
        <th>操作</th>
      </template>
      <template #body>
        <tr v-for="rev in revisions" :key="rev.id">
          <td class="font-mono tabular-nums">#{{ rev.id }}</td>
          <td class="font-mono tabular-nums text-xs">{{ formatTime(rev.createdAt) }}</td>
          <td>{{ rev.summary.startsWith('auto:') ? '自动' : '手动' }} · {{ rev.summary }}</td>
          <td>
            {{ status(rev.status) }}
            <p class="text-xs text-ios-secondary">{{ rev.resultCode }}</p>
          </td>
          <td>
            <div class="flex gap-2">
              <button class="ios-action-btn-secondary" @click="openDiff(rev)">查看差异</button>
              <button v-if="!node.deletedAt && rev.status === 'draft'" class="ios-action-btn-primary" :disabled="busy" @click="confirm('deploy', rev.id)">发布</button>
              <button v-if="!node.deletedAt && rev.status === 'success' && rev.id !== node.heartbeat?.successful_revision" class="ios-action-btn-secondary" :disabled="busy" @click="confirm('rollback', rev.id)">回滚到此版本</button>
            </div>
          </td>
        </tr>
      </template>
    </DataTable>
    <p v-else-if="isOwner" class="text-sm text-ios-secondary">当前没有可读取的配置版本记录。</p>
    <ConfirmDialog v-model="confirmOpen" :title="action === 'rollback' ? '回滚配置版本' : '发布配置'" :is-danger="action === 'rollback'" :message="action === 'current' ? '按当前已保存的节点、出站、出口组、代理链、分流规则与用户数据生成新版本并发布，服务器确认后生效。' : `确认将服务器 ${node.name} ${action === 'rollback' ? '回滚到' : '发布'}配置 #${target}？`" @confirm="execute" />
    <AppDialog v-model="diffOpen" title="配置版本差异" description="默认与服务器已确认的运行版本比较。" max-width="3xl" @close="clearDiff">
      <LoadingSkeleton v-if="diffLoading" />
      <ErrorState v-else-if="diffError" :message="diffError" @retry="loadDiff" />
      <ConfigDiffViewer v-else-if="diff" :diff="diff" />
    </AppDialog>
  </section>
</template>
<script setup lang="ts">
import { ref, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import { deploymentAPI, type Revision, type RevisionDiff } from '../../../system/deploymentApi';
import { isOwner, formatTime } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import DataTable from '../../../../components/DataTable.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import AppDialog from '../../../../components/AppDialog.vue';
import ConfigDiffViewer from '../../../../components/ConfigDiffViewer.vue';
import LoadingSkeleton from '../../../../components/LoadingSkeleton.vue';
import ErrorState from '../../../../components/ErrorState.vue';
const props = defineProps<{ node: NodeView; revisions: Revision[] }>();
const emit = defineEmits<{ changed: [] }>();
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
    showToast('请求已提交，等待服务器确认'); emit('changed');
  } catch (e) { error.value = (e as Error).message; emit('changed'); }
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
onUnmounted(clearDiff);
defineExpose({ publishCurrent });
</script>
