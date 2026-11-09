<template>
  <section class="panel-card p-5 space-y-5">
    <div class="flex justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">核心更新</h2><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新任务</button></div>
    <p class="text-sm text-gray-600">选择版本后自动填写官方发布资产地址和官方公布的压缩包 SHA-256（Xray 取 .dgst 校验文件，sing-box 取 GitHub 发布页摘要，qui 构建取 SHA256SUMS），也可手动填写。节点会校验压缩包摘要、解压唯一的可执行文件、运行版本命令并在健康检查失败时自动回滚到上一核心。</p>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <div class="grid md:grid-cols-2 gap-3 text-sm">
      <label v-if="!fixedNodeId">目标节点<select v-model="nodeId" class="form-input-base mt-2"><option value="">选择节点</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label>
      <label>核心<select v-model="component" class="form-input-base mt-2"><option value="xray">Xray</option><option value="sing-box">sing-box</option></select></label>
      <div v-if="selected" class="md:col-span-2 space-y-2">
        <div v-if="component === 'sing-box' && quiSourceAllowed(selected)" class="flex flex-wrap items-center gap-2 text-xs">
          <span class="text-gray-600">构建来源</span>
          <button type="button" class="px-2.5 py-1 rounded-lg border" :class="source === 'qui' ? 'bg-ios-blue text-white border-ios-blue' : 'bg-white border-black/[0.08]'" @click="setSource('qui')">qui 构建（含用户流量统计）</button>
          <button type="button" class="px-2.5 py-1 rounded-lg border" :class="source === 'official' ? 'bg-ios-blue text-white border-ios-blue' : 'bg-white border-black/[0.08]'" @click="setSource('official')">官方构建</button>
        </div>
        <div class="flex flex-wrap items-center gap-2 text-xs">
          <span class="text-gray-600">选择版本</span>
          <span v-if="releaseState === 'loading'" class="text-gray-500">正在读取官方发布…</span>
          <span v-else-if="releaseState === 'failed'" class="text-ios-orange">无法读取官方发布信息，请手动填写。</span>
          <span v-else-if="releaseState === 'ready' && !releases.length" class="text-ios-orange">没有找到可用的正式版本，请手动填写。</span>
          <button v-for="(item, index) in releases" :key="item.version" type="button" class="px-2.5 py-1 rounded-lg border tabular-nums" :class="picked?.version === item.version ? 'bg-ios-blue text-white border-ios-blue' : 'bg-white border-black/[0.08]'" @click="pick(item)">v{{ item.version }}{{ index === 0 ? '（最新）' : '' }}</button>
          <button v-if="releaseState !== 'loading'" type="button" class="text-ios-blue hover:underline" @click="loadReleases(true)">刷新</button>
        </div>
      </div>
      <label>版本<input v-model.trim="version" class="form-input-base mt-2" placeholder="26.3.27" @input="onVersionInput" /></label>
      <label>SHA-256<input v-model.trim="sha256" class="form-input-base mt-2 font-mono" placeholder="选择版本后自动填写；也可粘贴官方摘要" /></label>
      <label class="md:col-span-2">发布资产地址<input v-model.trim="assetURL" class="form-input-base mt-2 font-mono" placeholder="https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip" /></label>
      <p v-if="digestNote" class="text-xs md:col-span-2" :class="sha256 && assetURL ? 'text-gray-500' : 'text-ios-orange'">{{ digestNote }}</p>
      <p v-if="quiSourceBlocked" class="text-xs text-gray-500 md:col-span-2">该 Agent 尚未上报 qui sing-box 更新能力，请使用官方来源。</p>
      <div class="flex items-end"><button class="ios-btn-primary" :disabled="busy || !selected || !revision || !version || !sha256 || !assetURL || quiSourceBlocked" @click="confirmation = true">创建升级任务</button></div>
    </div>
    <p v-if="selected" class="text-xs text-gray-500">目标 {{ selected.name }}，最近报告的配置 #{{ revision || '尚未确认' }}。中央会再次核对节点核心、平台、版本格式、官方地址和摘要。</p>
    <EmptyState v-if="!tasks.length && !loading" title="暂无更新任务" description="选择节点和版本（地址与摘要会自动填写）后创建任务。" />
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
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { coreUpdatesAPI, type CoreName, type CoreUpdateTask } from '../coreUpdatesApi';
import { coreReleasesAPI, defaultCoreSource, deriveAssetURL, DIGEST_SOURCE_LABELS, nodeArchitecture, nodeUsesMusl, pickAsset, quiSourceAllowed, type CoreRelease, type CoreReleaseAsset, type CoreSource } from '../coreReleases';
import { type NodeView } from '../../nodes/api';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
const props = defineProps<{ nodes: NodeView[]; fixedNodeId?: string }>();
const tasks = ref<CoreUpdateTask[]>([]);
const taskPage = ref(1), taskTotal = ref(0), loading = ref(false), busy = ref(false), error = ref('');
const nodeId = ref(props.fixedNodeId || ''), component = ref('xray'), version = ref(''), assetURL = ref(''), sha256 = ref('');
watch(() => props.fixedNodeId, id => { if (id) nodeId.value = id; });
const confirmation = ref(false);
const selected = computed(() => props.nodes.find(node => node.id === nodeId.value));
const quiSourceBlocked = computed(() => assetURL.value.startsWith('https://github.com/jingniao/qui/releases/download/') && !quiSourceAllowed(selected.value));

// Official release picker (GET /api/v1/core-releases): picking a release fills
// version, the asset for this server and the digest the vendor published. Any
// field stays editable; an edited form is submitted as typed.
const source = ref<CoreSource>('official');
const releases = ref<CoreRelease[]>([]);
const releaseState = ref<'idle' | 'loading' | 'ready' | 'failed'>('idle');
const picked = ref<CoreRelease | null>(null);
const pickedURL = ref(''), pickedSHA = ref('');
const digestSource = ref<CoreReleaseAsset['digestSource']>('');
let releaseGeneration = 0;
async function loadReleases(refresh = false) {
  const current = ++releaseGeneration;
  releaseState.value = 'loading'; releases.value = []; picked.value = null;
  try {
    const list = await coreReleasesAPI.list(component.value as CoreName, source.value, refresh);
    if (current !== releaseGeneration) return;
    releases.value = list.items; releaseState.value = 'ready';
    if (list.items.length) pick(list.items[0], true);
  } catch { if (current === releaseGeneration) releaseState.value = 'failed'; }
}
function pick(item: CoreRelease, quiet = false) {
  picked.value = item; version.value = item.version;
  const asset = pickAsset(item, selected.value);
  assetURL.value = asset?.url ?? ''; sha256.value = asset?.sha256 ?? '';
  pickedURL.value = assetURL.value; pickedSHA.value = sha256.value;
  digestSource.value = asset?.sha256 ? asset.digestSource : '';
  if (!asset && !quiet) showToast(`v${item.version} 没有适用于这台服务器（${nodeArchitecture(selected.value) || '架构未知'}${nodeUsesMusl(selected.value) ? '，musl' : ''}）的文件`, 'warning');
}
function setSource(value: CoreSource) {
  if (source.value === value) return;
  source.value = value; version.value = ''; assetURL.value = ''; sha256.value = '';
  void loadReleases();
}
// Typing a version drops the picked release, its digest and the URL it filled;
// the URL is rebuilt where it can be derived from the version, so a hand-typed
// version never keeps the URL and digest of a different release.
function onVersionInput() {
  picked.value = null; digestSource.value = '';
  sha256.value = '';
  assetURL.value = deriveAssetURL(component.value as CoreName, source.value, version.value, selected.value);
}
const digestNote = computed(() => {
  if (!picked.value || version.value !== picked.value.version || assetURL.value !== pickedURL.value || sha256.value !== pickedSHA.value) return '';
  if (!assetURL.value) return '该版本没有适用于这台服务器的文件，请换一个版本或手动填写。';
  if (!sha256.value) return '该文件没有可用的官方摘要（缺失或两处公布的值不一致），请从官方发布页核对后手动粘贴。';
  return `摘要已自动填写，来源：${DIGEST_SOURCE_LABELS[digestSource.value]}；节点下载后仍会逐字节核对。`;
});
watch([component, () => selected.value?.id], () => {
  version.value = ''; assetURL.value = ''; sha256.value = ''; picked.value = null; releases.value = []; releaseState.value = 'idle';
  if (!selected.value) return;
  source.value = defaultCoreSource(component.value as CoreName, selected.value);
  if (source.value === 'qui' && !quiSourceAllowed(selected.value)) source.value = 'official';
  void loadReleases();
}, { immediate: true });
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
