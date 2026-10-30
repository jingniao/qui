<template>
  <div class="space-y-6">
    <PageHeader title="服务器详情" parent="服务器">
      <template #actions>
        <router-link to="/nodes" class="ios-btn-secondary">返回列表</router-link>
        <button class="ios-btn-secondary" :disabled="loading" @click="load">刷新</button>
      </template>
    </PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!node" />
    <template v-else>
      <ServerDetailHeader :node="node" :groups="groups" :meta="meta" :now="now" :latest-revision="latestRevision" @publish="revisionsPanel?.publishCurrent()" @programs="programs" @install="enrollment?.reissue(node)" @delete="deletion?.prompt(node)" @changed="load" />
      <p v-if="warnings.length" role="status" class="text-sm text-ios-orange">{{ warnings.join('；') }}</p>
      <nav class="ios-segmented-container inline-flex overflow-x-auto max-w-full" aria-label="服务器详情页签">
        <button v-for="tab in tabs" :key="tab.key" class="ios-segmented-item whitespace-nowrap" :class="activeTab === tab.key ? 'active' : ''" :aria-current="activeTab === tab.key ? 'page' : undefined" @click="selectTab(tab.key)">{{ tab.label }}</button>
      </nav>
      <ServerOverview v-if="activeTab === 'overview'" :node="node" :meta="meta" :now="now" :today="today" :month="month" :revisions="revisions" />
      <ServerNodes v-else-if="activeTab === 'nodes'" :key="node.id" :node="node" />
      <ServerMonitoring v-else-if="activeTab === 'monitoring'" :key="node.id" :node="node" :now="now" />
      <ServerRevisions v-show="activeTab === 'revisions'" :key="node.id" ref="revisionsPanel" :node="node" :revisions="revisions" @changed="load" />
      <ServerSettings ref="settingsPanel" v-if="activeTab === 'settings' && isOwner" :key="node.id" :node="node" :meta="meta" @changed="load" @install="enrollment?.reissue(node)" @delete="deletion?.prompt(node)" />
    </template>
    <ServerEnrollment v-if="isOwner" ref="enrollment" @changed="load" />
    <ServerDeleteDialog v-if="isOwner" ref="deletion" @changed="deleted" />
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch, onMounted, onUnmounted, nextTick } from 'vue';
import { useRoute, useRouter } from 'vue-router';
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
import ServerDetailHeader from '../components/detail/ServerDetailHeader.vue';
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
const today = ref<number>(), month = ref<number>();
const meta = computed(() => node.value ? getNodeMeta(node.value.id) : {});
const latestRevision = computed(() => revisions.value.length ? Math.max(...revisions.value.map(rev => rev.id)) : undefined);
const tabs = computed(() => [{ key: 'overview', label: '概况' }, { key: 'nodes', label: '节点' }, { key: 'monitoring', label: '监控' }, { key: 'revisions', label: '配置版本' }, ...(isOwner.value ? [{ key: 'settings', label: '设置' }] : [])]);
const aliases: Record<string, string> = { testing: 'monitoring', tuning: 'settings', config: 'revisions' };
const activeTab = computed(() => { const raw = typeof route.query.tab === 'string' ? route.query.tab : ''; const key = aliases[raw] || raw; return tabs.value.some(tab => tab.key === key) ? key : 'overview'; });
function selectTab(tab: string) { void router.replace({ query: { ...route.query, tab } }); }
const settingsPanel = ref<InstanceType<typeof ServerSettings>>();
const revisionsPanel = ref<InstanceType<typeof ServerRevisions>>(), enrollment = ref<InstanceType<typeof ServerEnrollment>>(), deletion = ref<InstanceType<typeof ServerDeleteDialog>>();
async function programs() { await router.replace({ query: { ...route.query, tab: 'settings' } }); await nextTick(); settingsPanel.value?.openCoreUpdates(); document.getElementById('server-programs')?.scrollIntoView({ behavior: 'smooth' }); }
function deleted() { void router.push('/nodes'); }
let generation = 0, timer: ReturnType<typeof setInterval> | undefined;
async function load() {
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
      isOwner.value ? optional('版本读取失败', choices((page, size) => deploymentAPI.nodeRevisions(id, page, size)), []) : Promise.resolve([]),
      optional('今日流量读取失败', accessAPI.accounts({ nodeId: id, from: dayStart, to: new Date().toISOString() }), null),
      optional('本月流量读取失败', request<{ items: { nodeId: string; uploadBytes: number; downloadBytes: number }[]; timezone: string }>('/traffic/nodes/monthly'), null),
      optional('元数据读取失败', loadNodeMeta(id, isOwner.value), null)
    ]);
    if (current !== generation) return;
    node.value = server; groups.value = groupResult; revisions.value = revisionResult;
    today.value = daily && daily.observations > 0 ? daily.uploadBytes + daily.downloadBytes : undefined;
    const item = monthly?.items.find(item => item.nodeId === id);
    month.value = item ? item.uploadBytes + item.downloadBytes : undefined;
    warnings.value = notices;
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
watch(() => route.params.id, () => { generation++; node.value = null; groups.value = []; revisions.value = []; today.value = undefined; month.value = undefined; warnings.value = []; void load(); });
onMounted(() => { void load(); timer = setInterval(() => { now.value = Date.now(); }, 5000); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { generation++; if (timer) clearInterval(timer); window.removeEventListener('proxy-node-status', load); });
</script>
