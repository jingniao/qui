<template>
  <div class="space-y-6">
    <router-link to="/nodes" class="text-sm text-ios-blue">← 服务器</router-link>
    <PageHeader :title="node?.name || '服务器详情'"><template v-if="node" #badge><StatusBadge :status="status.tone" :label="status.label" /></template></PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!node" />
    <template v-else>
      <p v-if="node.heartbeat?.core_healthy === false" role="status" class="rounded-xl bg-ios-red/10 p-4 text-sm text-ios-red">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '代理已停止，流量消费需要核对；已采集流量继续补传。' : '最后心跳报告核心未通过健康检查，请检查程序与配置发布结果。' }}</p>
      <p v-if="warnings.length" role="status" class="text-sm text-ios-orange">{{ warnings.join('；') }}</p>
      <nav class="ios-segmented-container inline-flex overflow-x-auto max-w-full" aria-label="服务器详情页签">
        <button v-for="tab in tabs" :key="tab.key" class="ios-segmented-item whitespace-nowrap" :class="activeTab === tab.key ? 'active' : ''" :aria-current="activeTab === tab.key ? 'page' : undefined" @click="selectTab(tab.key)">{{ tab.label }}</button>
      </nav>
      <template v-if="pending">
        <ServerSettings v-if="activeTab === 'settings' && isOwner" ref="settingsPanel" :key="node.id" :node="node" :meta="meta" @changed="load" @install="enrollment?.reissue(node)" @delete="deletion?.prompt(node)" />
        <ServerSetupGuide v-else-if="activeTab === 'overview'" :node="node" :command="enrollment?.installCode(node.id)" @install="enrollment?.reissue(node)" />
        <p v-else class="text-sm text-ios-secondary">服务器待登记，完成 Agent 安装并等待首次心跳后可查看此页。</p>
      </template>
      <template v-else>
        <p v-if="offline" class="rounded-xl bg-ios-orange/10 p-4 text-sm text-ios-orange">{{ node.deletedAt ? '服务器已删除' : !node.enabled ? '服务器已停用' : '服务器离线' }} · 最后心跳 {{ formatTime(node.lastHeartbeat) }}</p>
        <ServerOverview v-if="activeTab === 'overview'" :key="node.id + ':' + nodeRefresh" :node="node" :groups="groups" :meta="meta" :now="now" :today="today" :month="month" :revisions="revisions" :latest-revision="latestRevision" @changed="load" @tab="selectTab" @create="openEditor(null)" />
        <ServerNodes v-else-if="activeTab === 'nodes'" :key="node.id + ':' + nodeRefresh" :node="node" :unpublished="unpublished" @edit="openEditor" @saved="nodeSaved" @publish="revisionsPanel?.publishCurrent()" />
        <ServerMonitoring v-else-if="activeTab === 'monitoring'" :key="node.id" :node="node" :now="now" />
        <ServerRevisions v-show="activeTab === 'revisions'" :key="node.id" ref="revisionsPanel" :node="node" :refresh="revisionRefresh" :latest-revision="latestRevision" @changed="load" @published="published" />
        <ServerSettings ref="settingsPanel" v-if="activeTab === 'settings' && isOwner" :key="node.id" :node="node" :meta="meta" @changed="load" @install="enrollment?.reissue(node)" @delete="deletion?.prompt(node)" />
        <NodeEditorDrawer v-if="editorOpen && isOwner" :server-id="node.id" :inbound="editing" origin="server" @close="editorOpen = false" @saved="nodeSaved" />
      </template>
    </template>
    <ConfirmDialog v-model="discardOpen" title="有未保存的更改" message="离开后将丢失设置中尚未保存的修改，确认离开？" confirm-text="放弃修改并离开" @confirm="discard" />
    <ServerEnrollment v-if="isOwner" ref="enrollment" @changed="load" />
    <ServerDeleteDialog v-if="isOwner" ref="deletion" @changed="deleted" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted } from 'vue';
import { useRoute, useRouter, onBeforeRouteLeave, onBeforeRouteUpdate } from 'vue-router';
import { nodeAPI, type NodeView } from '../api';
import { groupAPI, type NodeGroup } from '../groupsApi';
import { getNodeMeta, loadNodeMeta } from '../nodeMeta';
import { deploymentAPI, type Revision } from '../../system/deploymentApi';
import { choices } from '../../proxy/api';
import { accessAPI } from '../../access/api';
import { request } from '../../../app/apiClient';
import { isOwner } from '../../../app/auth';
import { localInput, utcInput } from '../../../app/time';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import { serverStatus } from '../components/serverListModel';
import { detailState } from '../components/detail/serverDetailModel';
import ServerSetupGuide from '../components/detail/ServerSetupGuide.vue';
import NodeEditorDrawer from '../../proxy/components/node-editor/NodeEditorDrawer.vue';
import type { Inbound } from '../../proxy/api';
import { formatTime } from '../../../app/auth';
import ServerOverview from '../components/detail/ServerOverview.vue';
import ServerNodes from '../components/detail/ServerNodes.vue';
import ServerMonitoring from '../components/detail/ServerMonitoring.vue';
import ServerRevisions from '../components/detail/ServerRevisions.vue';
import ServerSettings from '../components/detail/ServerSettings.vue';
import ServerEnrollment from '../components/ServerEnrollment.vue';
import ServerDeleteDialog from '../components/ServerDeleteDialog.vue';
const route = useRoute(), router = useRouter();
const node = ref<NodeView | null>(null), error = ref(''), loading = ref(false), warnings = ref<string[]>([]);
const groups = ref<NodeGroup[]>([]), revisions = ref<Revision[]>([]), now = ref(Date.now());
const status = computed(() => node.value ? detailState(node.value, now.value) === 'pending' ? { tone: 'disabled', label: '待登记' } : serverStatus(node.value, now.value) : { tone: 'offline', label: '读取中' });
const pending = computed(() => node.value && detailState(node.value, now.value) === 'pending');
const offline = computed(() => node.value && detailState(node.value, now.value) === 'offline');
const editorOpen = ref(false), editing = ref<Inbound | null>(null), unpublished = ref(false), nodeRefresh = ref(0), revisionRefresh = ref(0);
function openEditor(item: Inbound | null) { if (!isOwner.value || node.value?.deletedAt) return; editing.value = item; editorOpen.value = true; }
function nodeSaved() { unpublished.value = true; nodeRefresh.value++; void load(); }
function published() { unpublished.value = false; }
const discardOpen = ref(false);
let resolveDiscard: ((value: boolean) => void) | undefined;
async function canLeave() { if (!settingsPanel.value?.hasUnsaved) return true; if (resolveDiscard) return false; discardOpen.value = true; return new Promise<boolean>(resolve => { resolveDiscard = resolve; }); }
function discard() { resolveDiscard?.(true); resolveDiscard = undefined; }
function cancelDiscard() { resolveDiscard?.(false); resolveDiscard = undefined; }
watch(discardOpen, value => { if (!value) cancelDiscard(); });
onBeforeRouteLeave(canLeave);
onBeforeRouteUpdate((to, from) => { if (route.name !== 'NodeDetail') return true; return to.params.id !== from.params.id || to.query.tab !== from.query.tab ? canLeave() : true; });
const today = ref<number>(), month = ref<number>();
const meta = computed(() => node.value ? getNodeMeta(node.value.id) : {});
const latestRevision = computed(() => revisions.value.length ? Math.max(...revisions.value.map(rev => rev.id)) : undefined);
const tabs = computed(() => [{ key: 'overview', label: '概况' }, { key: 'nodes', label: '节点' }, { key: 'monitoring', label: '监控' }, { key: 'revisions', label: '配置版本' }, ...(isOwner.value ? [{ key: 'settings', label: '设置' }] : [])]);
const aliases: Record<string, string> = { testing: 'monitoring', tuning: 'settings', config: 'revisions' };
const activeTab = computed(() => { const raw = typeof route.query.tab === 'string' ? route.query.tab : ''; const key = aliases[raw] || raw; return tabs.value.some(tab => tab.key === key) ? key : 'overview'; });
function selectTab(tab: string) { if (route.name !== 'NodeDetail' || tab === activeTab.value) return; void router.replace({ query: { ...route.query, tab } }); }
const settingsPanel = ref<InstanceType<typeof ServerSettings>>();
const revisionsPanel = ref<InstanceType<typeof ServerRevisions>>(), enrollment = ref<InstanceType<typeof ServerEnrollment>>(), deletion = ref<InstanceType<typeof ServerDeleteDialog>>();
function deleted() { void router.push('/nodes'); }
let generation = 0, timer: ReturnType<typeof setInterval> | undefined;
async function load() {
  if (route.name !== 'NodeDetail') return;
  const id = String(route.params.id || ''), current = ++generation;
  loading.value = true; error.value = '';
  try {
    const server = await nodeAPI.get(id);
    const notices: string[] = [];
    const optional = async <T,>(label: string, promise: Promise<T>, fallback: T): Promise<T> => {
      try { return await promise; } catch (e) { notices.push(`${label}：${(e as Error).message}`); return fallback; }
    };
    let dayStart: string;
    try { dayStart = utcInput(localInput(new Date().toISOString()).slice(0, 10) + 'T00:00'); }
    catch { const utc = new Date(); utc.setUTCHours(0, 0, 0, 0); dayStart = utc.toISOString(); }
    const [groupResult, revisionResult, daily, monthly] = await Promise.all([
      isOwner.value ? optional('线路组读取失败', choices(groupAPI.list), []) : Promise.resolve([]),
      isOwner.value ? optional('版本读取失败', deploymentAPI.nodeRevisions(id, 1, 20).then(result => result.items), []) : Promise.resolve([]),
      optional('今日流量读取失败', accessAPI.accounts({ nodeId: id, from: dayStart, to: new Date().toISOString() }), null),
      optional('本月流量读取失败', request<{ items: { nodeId: string; uploadBytes: number; downloadBytes: number }[]; timezone: string }>('/traffic/nodes/monthly'), null),
      optional('元数据读取失败', loadNodeMeta(id, isOwner.value), null)
    ]);
    if (current !== generation) return;
    node.value = server; groups.value = groupResult; revisions.value = revisionResult; revisionRefresh.value++;
    today.value = daily && daily.observations > 0 ? daily.uploadBytes + daily.downloadBytes : undefined;
    const item = monthly?.items.find(item => item.nodeId === id);
    month.value = item ? item.uploadBytes + item.downloadBytes : undefined;
    warnings.value = notices;
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
// The layout keeps a leaving page mounted during its out-in transition while
// route already describes the next page; ignore routes that are not ours.
watch(() => route.params.id, () => { if (route.name !== 'NodeDetail') return; generation++; editorOpen.value = false; unpublished.value = false; node.value = null; groups.value = []; revisions.value = []; today.value = undefined; month.value = undefined; warnings.value = []; void load(); });
onMounted(() => { void load(); timer = setInterval(() => { now.value = Date.now(); }, 5000); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { cancelDiscard(); generation++; if (timer) clearInterval(timer); window.removeEventListener('proxy-node-status', load); });
</script>
