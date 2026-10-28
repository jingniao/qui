<template>
  <section class="panel-card p-5 space-y-5">
    <div class="flex justify-between items-center gap-3"><h2 class="font-semibold">核心更新</h2><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新任务</button></div>
    <p class="text-sm text-gray-600">填写官方 Xray-core 或 sing-box 发布版本的版本号、GitHub 发布资产地址和压缩包 SHA-256。支持的 Agent 也可使用 qui 发布中的 sing-box 构建。节点会校验压缩包摘要、解压唯一的可执行文件、运行版本命令并在健康检查失败时自动回滚到上一核心。</p>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <div class="grid md:grid-cols-2 gap-3 text-sm">
      <label>目标节点<select v-model="nodeId" class="form-input-base mt-2"><option value="">选择节点</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label>
      <label>核心<select v-model="component" class="form-input-base mt-2"><option value="xray">Xray</option><option value="sing-box">sing-box</option></select></label>
      <label>版本<input v-model.trim="version" class="form-input-base mt-2" placeholder="26.3.27" /></label>
      <label>SHA-256<input v-model.trim="sha256" class="form-input-base mt-2 font-mono text-xs" placeholder="64 位十六进制摘要" /></label>
      <label class="md:col-span-2">发布资产地址<input v-model.trim="assetURL" class="form-input-base mt-2 font-mono text-xs" placeholder="https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip" /></label>
      <p v-if="quiSourceBlocked" class="text-xs text-gray-500 md:col-span-2">该 Agent 尚未上报 qui sing-box 更新能力，请使用官方来源。</p>
      <p v-else-if="supportsQuiCore && component === 'sing-box'" class="text-xs text-gray-500 md:col-span-2">该 Agent 支持 qui sing-box 构建；请填写对应发布资产的地址及 SHA256SUMS 中的摘要。</p>
      <div class="flex items-end"><button class="ios-btn-primary" :disabled="busy || !selected || !revision || !version || !sha256 || !assetURL || quiSourceBlocked" @click="confirmation = true">创建升级任务</button></div>
    </div>
    <p v-if="selected" class="text-xs text-gray-500">目标 {{ selected.name }}，最近报告的配置 #{{ revision || '尚未确认' }}。中央会再次核对节点核心、平台、版本格式、官方地址和摘要。</p>
    <EmptyState v-if="!tasks.length && !loading" title="暂无更新任务" description="选择节点并填写版本、官方资产地址和 SHA-256 后创建任务。" />
    <DataTable v-else-if="tasks.length" min-width="min-w-[900px]">
      <template #header><th>节点 / 配置</th><th>目标</th><th>状态</th><th>结果</th><th>更新时间</th><th>操作</th></template>
      <template #body>
        <tr v-for="task in tasks" :key="task.id">
          <td class="p-4"><router-link :to="`/nodes/${task.nodeId}`" class="text-blue-600">{{ nodeName(task.nodeId) }}</router-link><p class="text-xs text-gray-500">配置 #{{ task.revision }}</p></td>
          <td class="text-xs">{{ task.component }} {{ task.version }}<p class="text-gray-500">#{{ task.sequence }}</p></td>
          <td>{{ status(task.status) }}</td>
          <td class="text-xs">{{ task.result?.installed_version || '等待结果' }}<p class="text-gray-500">{{ updateResultLabel(task.result?.code) }}</p><p v-if="updateResultGuidance(task.result?.code)" class="mt-2 text-gray-500">{{ updateResultGuidance(task.result?.code) }}</p></td>
          <td class="text-xs">{{ formatTime(task.updatedAt) }}</td>
          <td><button v-if="task.status !== 'cleared'" class="ios-action-btn-secondary" :disabled="busy" @click="clear(task)">清除任务</button></td>
        </tr>
      </template>
      <template #footer><Pagination :total="taskTotal" :current-page="taskPage" :page-size="20" @update:page="changeTaskPage" /></template>
    </DataTable>
    <ConfirmDialog v-model="confirmation" title="确认核心升级" :message="confirmMessage" @confirm="queue" />
  </section>
</template>

<script setup lang="ts">
import { updateResultLabel, updateResultGuidance } from '../updateResultText';
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { coreUpdatesAPI, type CoreUpdateTask } from '../coreUpdatesApi';
import { type NodeView } from '../../nodes/api';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
const props = defineProps<{ nodes: NodeView[] }>();
const tasks = ref<CoreUpdateTask[]>([]);
const taskPage = ref(1), taskTotal = ref(0), loading = ref(false), busy = ref(false), error = ref('');
const nodeId = ref(''), component = ref('xray'), version = ref(''), assetURL = ref(''), sha256 = ref('');
const confirmation = ref(false);
const selected = computed(() => props.nodes.find(node => node.id === nodeId.value));
const supportsQuiCore = computed(() => selected.value?.capabilities?.singbox_user_stats === true);
const quiSourceBlocked = computed(() => assetURL.value.startsWith('https://github.com/jingniao/qui/releases/download/') && !supportsQuiCore.value);
const revision = computed(() => selected.value?.heartbeat?.current_revision || 0);
const confirmMessage = computed(() => `确认升级 ${selected.value?.name || ''} 到 ${component.value} ${version.value}？连接可能中断，健康检查失败时节点会自动回滚到上一核心。`);
let generation = 0;
function nodeName(id: string) { return props.nodes.find(node => node.id === id)?.name || id; }
async function load() {
  const current = ++generation; loading.value = true;
  try {
    const page = await coreUpdatesAPI.tasks(taskPage.value);
    if (current !== generation) return;
    tasks.value = page.items; taskTotal.value = page.total; error.value = '';
  } catch(e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
async function queue() {
  confirmation.value = false;
  if (!selected.value || busy.value || quiSourceBlocked.value) return;
  busy.value = true;
  try {
    await coreUpdatesAPI.queue({ nodeId: nodeId.value, revision: revision.value, component: component.value, version: version.value, url: assetURL.value, sha256: sha256.value });
    showToast('升级任务已受理，等待节点确认');
    await load();
  } catch(e) { showToast((e as Error).message, 'error'); }
  finally { busy.value = false; }
}
async function clear(task: CoreUpdateTask) {
  if (busy.value) return;
  busy.value = true;
  try { await coreUpdatesAPI.clear(task.id); showToast('任务已清除'); await load(); }
  catch(e) { showToast((e as Error).message, 'error'); }
  finally { busy.value = false; }
}
function status(value: string) {
  return ({ queued:'等待节点', running:'已下发，结果未确认', success:'已确认成功', rolled_back:'已恢复上一核心', rejected:'节点拒绝', failed_closed:'未确认，已关闭', cleared:'已清除' } as Record<string,string>)[value] || value;
}
function changeTaskPage(value: number) { taskPage.value = value; void load(); }
onMounted(() => { void load(); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { generation++; window.removeEventListener('proxy-node-status', load); });
</script>
