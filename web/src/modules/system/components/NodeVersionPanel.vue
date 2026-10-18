<template>
  <div class="space-y-6">
    <PageHeader :title="kind === 'agent' ? 'Agent 管理' : '代理核心版本'" parent="系统" description="版本与状态来自节点最近一次上报，离线节点保留最后报告。">
      <template #actions><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新</button></template>
    </PageHeader>
    <section class="panel-card p-5 space-y-2">
      <p class="text-sm font-semibold">已登记 {{ total }} 个节点</p>
      <p class="text-sm text-gray-600">当前版本与健康来自节点报告。可通过下面的签名任务提交更新，执行结果以节点确认为准。</p>
      <p class="text-xs text-gray-500">配置发布与回滚可在配置版本页面执行。更新程序前应核对签名和兼容性，升级可能中断现有连接。</p>
    </section>
    <form class="flex gap-3" @submit.prevent="search"><input v-model="keyword" class="form-input-base" placeholder="搜索节点名称或 IP" /><button class="ios-btn-secondary shrink-0">查询</button></form>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <EmptyState v-else-if="!nodes.length" title="暂无匹配节点" description="注册节点并连接 Agent 后，版本信息将随实际报告显示。" />
    <DataTable v-else min-width="min-w-[1000px]">
      <template #header>
        <th>节点</th><th>节点状态</th><th>系统</th>
        <template v-if="kind === 'agent'"><th>Agent 版本</th><th>核心版本</th></template>
        <template v-else><th>核心 / 版本</th><th>最近核心健康</th><th>支持协议</th></template>
        <th>最后心跳</th><th>操作</th>
      </template>
      <template #body>
        <tr v-for="node in nodes" :key="node.id">
          <td class="p-4"><router-link :to="`/nodes/${node.id}`" class="text-blue-600 font-semibold">{{ node.name }}</router-link><p class="text-xs text-gray-500 mt-1">{{ node.publicIp || node.hostname || '等待 Agent 注册' }}</p></td>
          <td><StatusBadge kind="node" :status="node.status" /></td><td>{{ node.os || '尚未上报' }}</td>
          <template v-if="kind === 'agent'"><td class="font-mono text-sm">{{ node.agentVersion || '尚未上报' }}</td><td>{{ coreVersion(node) }}</td></template>
          <template v-else>
            <td>{{ coreVersion(node) }}</td>
            <td><span :class="node.heartbeat?.core_healthy === false ? 'text-red-600' : 'text-gray-600'">{{ coreHealth(node) }}</span></td>
            <td><div v-if="protocols(node).length" class="flex flex-wrap gap-1.5"><ProtocolBadge v-for="protocol in protocols(node)" :key="protocol" :protocol="protocol" /></div><span v-else class="text-gray-500">尚未上报</span></td>
          </template>
          <td class="text-xs">{{ formatTime(node.lastHeartbeat) }}</td>
          <td><router-link :to="`/nodes/${node.id}`" class="ios-action-btn-primary">节点详情</router-link></td>
        </tr>
      </template>
      <template #footer><Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" /></template>
    </DataTable>
	<CoreUpdatesPanel v-if="kind === 'core'" :nodes="nodes" />
    <AgentUpdatesPanel v-if="kind === 'agent'" :nodes="nodes" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { formatTime } from '../../../app/auth';
import { nodeAPI, type NodeView } from '../../nodes/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ProtocolBadge from '../../../components/ProtocolBadge.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import CoreUpdatesPanel from './CoreUpdatesPanel.vue';
import AgentUpdatesPanel from './AgentUpdatesPanel.vue';

defineProps<{ kind: 'agent' | 'core' }>();
const nodes = ref<NodeView[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const loading = ref(false);
const error = ref('');
let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true; error.value = '';
  try {
    const result = await nodeAPI.list(page.value, keyword.value);
    if (current === generation) { nodes.value = result.items; total.value = result.total; }
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
function search() { page.value = 1; void load(); }
function changePage(value: number) { page.value = value; void load(); }
function coreVersion(node: NodeView) {
  if (!node.capabilities?.core_name) return '尚未检测核心';
  return `${node.capabilities.core_name} ${node.capabilities.core_version || '版本未上报'}`;
}
function coreHealth(node: NodeView) {
  if (node.heartbeat?.core_healthy === true) return '最近上报正常';
  if (node.heartbeat?.core_healthy === false) return '最近上报异常';
  return '尚未上报';
}
function protocols(node: NodeView) {
  return [...new Set((node.capabilities?.protocols || []).filter(item => item.inbound || item.outbound).map(item => item.protocol))].sort();
}
onMounted(() => { void load(); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { generation++; window.removeEventListener('proxy-node-status', load); });
</script>
