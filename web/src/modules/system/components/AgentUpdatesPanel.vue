<template>
  <section v-if="isOwner" class="panel-card p-5 space-y-5">
    <div class="flex justify-between items-center gap-3"><h2 class="font-semibold">Agent 升级</h2><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新版本与任务</button></div>
    <p class="text-sm text-gray-600">节点需要启用 Agent 托管升级并配置可信发布目录。切换版本可能中断连接，结果以节点确认记录为准。</p>
    <p class="text-xs text-gray-500">旧Worker停止超时且退出未确认时，任务保留未完成记录，暂停后续切换；不会仅凭超时认定代理已关闭或启动替代Worker。请保留原任务与节点日志，按原记录核对停止结果。</p>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <p v-if="loading" class="text-sm text-gray-500">正在读取版本与任务…</p>
    <p v-else-if="!configured && !error" class="text-sm text-amber-700">中央尚未配置发布公钥，配置后才能登记和下发版本。</p>
    <details class="space-y-3"><summary class="text-sm cursor-pointer">登记签名 Agent 版本</summary><p class="text-xs text-gray-500">粘贴离线签名工具生成的完整 Agent 清单。</p><textarea v-model="manifest" maxlength="16384" rows="6" class="form-input-base w-full font-mono text-xs" aria-label="签名 Agent 清单" /><button class="ios-btn-secondary" :disabled="busy || !configured || !manifest.trim()" @click="register">验证并登记</button></details>
    <div class="grid md:grid-cols-3 gap-3 text-sm">
      <label>当前页节点<select v-model="nodeId" class="form-input-base mt-2"><option value="">选择节点</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label>
      <label>当前目录页的 Agent 版本<select v-model="releaseId" class="form-input-base mt-2"><option value="">选择版本</option><option v-for="entry in releases" :key="entry.id" :value="entry.id">{{ entry.signed.manifest.version }} · {{ entry.signed.manifest.os }}/{{ entry.signed.manifest.architecture }} · #{{ entry.signed.manifest.sequence }}</option></select></label>
      <div class="flex items-end"><button class="ios-btn-primary" :disabled="busy || loading || !!error || !configured || !selected || !selectedRelease || !revision" @click="prepare">创建升级任务</button></div>
    </div>
    <p v-if="selected" class="text-xs text-gray-500">{{ selected.name }} · 当前 Agent {{ selected.agentVersion || '未上报' }} · 最近报告配置 #{{ revision || '未确认' }}。提交时会再次检查节点能力、版本与操作冲突。</p>
    <p v-if="!loading && !error && !releases.length" class="text-sm text-gray-500">当前目录页没有 Agent 版本，可翻页或登记新版本。</p>
    <p class="text-xs text-gray-500">以下分页对应共享签名目录；此处仅列出其中的 Agent 版本。</p>
    <Pagination :total="releaseTotal" :current-page="releasePage" :page-size="20" @update:page="changeReleasePage" />
    <EmptyState v-if="!tasks.length && !loading && !error" title="暂无 Agent 升级任务" description="登记可信版本并选择节点后创建任务。" />
    <DataTable v-if="tasks.length" min-width="min-w-[800px]">
      <template #header><th>节点 / 配置</th><th>任务状态</th><th>确认版本</th><th>结果</th><th>更新时间</th></template>
      <template #body><tr v-for="task in tasks" :key="task.id"><td class="p-4"><router-link :to="`/nodes/${task.nodeId}`" class="text-blue-600">{{ nodeName(task.nodeId) }}</router-link><p class="text-xs text-gray-500">配置 #{{ task.revision }}</p></td><td>{{ status(task.status) }}</td><td>{{ task.result?.installed_version || '尚未确认' }}</td><td class="text-xs">{{ updateResultLabel(task.result?.code) }}<p v-if="updateResultGuidance(task.result?.code)" class="mt-2 text-gray-500">{{ updateResultGuidance(task.result?.code) }}</p></td><td class="text-xs">{{ formatTime(task.updatedAt) }}</td></tr></template>
      <template #footer><Pagination :total="taskTotal" :current-page="taskPage" :page-size="20" @update:page="changeTaskPage" /></template>
    </DataTable>

    <ConfirmDialog :model-value="!!confirmation" @update:model-value="value => { if (!value) confirmation = null; }" title="确认 Agent 升级" :message="confirmation ? `将 ${confirmation.name} 的 Agent 升级为 ${confirmation.version}，配置 #${confirmation.revision}。切换可能中断连接，原身份与已用额度保留。` : ''" @confirm="queue" />
  </section>
</template>
<script setup lang="ts">
import { updateResultLabel, updateResultGuidance } from '../updateResultText';
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { agentUpdatesAPI, type AgentUpdateTask } from '../agentUpdatesApi';
import { coreUpdatesAPI, type ReleaseEntry } from '../coreUpdatesApi';
import type { NodeView } from '../../nodes/api';
import { formatTime, isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';

const props = defineProps<{ nodes: NodeView[] }>();
const releases = ref<ReleaseEntry[]>([]), tasks = ref<AgentUpdateTask[]>([]);
const releasePage = ref(1), taskPage = ref(1), releaseTotal = ref(0), taskTotal = ref(0);
const loading = ref(false), busy = ref(false), configured = ref(false), error = ref(''), manifest = ref('');
const nodeId = ref(''), releaseId = ref('');
const selected = computed(() => props.nodes.find(node => node.id === nodeId.value));
const selectedRelease = computed(() => releases.value.find(entry => entry.id === releaseId.value));
const revision = computed(() => selected.value?.heartbeat?.current_revision || 0);
const confirmation = ref<{ nodeId: string; releaseId: string; revision: number; name: string; version: string } | null>(null);
let generation = 0;
function nodeName(id: string) { return props.nodes.find(node => node.id === id)?.name || id; }
async function load() {
  if (!isOwner.value) return;
  const current = ++generation; loading.value = true;
  try {
    const [a, b] = await Promise.all([coreUpdatesAPI.releases(releasePage.value), agentUpdatesAPI.tasks(taskPage.value)]);
    if (current !== generation) return;
    releases.value = a.items.filter(entry => entry.signed.manifest.component === 'proxy-agent');
    releaseTotal.value = a.total; configured.value = a.configured;
    tasks.value = b.items; taskTotal.value = b.total; error.value = '';
    if (!selectedRelease.value) releaseId.value = '';
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
async function register() {
  if (busy.value || !isOwner.value) return;
  busy.value = true;
  try {
    const signed = JSON.parse(manifest.value);
    if (signed?.manifest?.component !== 'proxy-agent') throw new Error('请选择 proxy-agent 的签名清单');
    await coreUpdatesAPI.register(signed); manifest.value = ''; releasePage.value = 1;
    showToast('Agent 签名版本已验证并登记'); await load();
  } catch (e) { showToast((e as Error).message, 'error'); }
  finally { busy.value = false; }
}
function prepare() {
  if (!selected.value || !selectedRelease.value || !revision.value || busy.value) return;
  confirmation.value = { nodeId: selected.value.id, name: selected.value.name, releaseId: selectedRelease.value.id, version: selectedRelease.value.signed.manifest.version, revision: revision.value };
}
async function queue() {
  const target = confirmation.value; confirmation.value = null;
  if (!target || busy.value || !isOwner.value) return;
  busy.value = true;
  try {
    await agentUpdatesAPI.queue(target.nodeId, target.releaseId, target.revision);
    taskPage.value = 1; showToast('Agent 升级任务已受理，等待节点确认'); await load();
  } catch (e) { showToast((e as Error).message, 'error'); }
  finally { busy.value = false; }
}
function status(value: string) { return ({ queued: '等待节点', running: '已下发，结果未确认', success: '已确认成功', rolled_back: '已恢复原 Agent', rejected: '节点拒绝', failed_closed: '保持关闭，待核对' } as Record<string, string>)[value] || value; }
function changeReleasePage(page: number) { releasePage.value = page; releaseId.value = ''; void load(); }
function changeTaskPage(page: number) { taskPage.value = page; void load(); }
onMounted(() => { void load(); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { generation++; window.removeEventListener('proxy-node-status', load); });
</script>

