<template>
  <div class="space-y-6">
    <PageHeader
      title="任务记录"
      parent="系统管理"
      description="查看服务器 Agent 探针及代理核心的升级执行历史与任务结果。"
    >
      <template #actions>
        <button
          type="button"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
          :disabled="loading"
          @click="loadAll"
        >
          <svg
            v-if="loading"
            class="w-3.5 h-3.5 animate-spin"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
          >
            <circle cx="12" cy="12" r="10" stroke-width="4" class="opacity-25" />
            <path d="M4 12a8 8 0 018-8" stroke-width="4" class="opacity-75" />
          </svg>
          <svg
            v-else
            class="w-3.5 h-3.5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
          >
            <path
              d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15"
              stroke-linecap="round"
              stroke-linejoin="round"
            />
          </svg>
          <span>刷新记录</span>
        </button>
      </template>
    </PageHeader>

    <div class="panel-card p-5 space-y-4">
      <!-- 顶部过滤与搜索栏 -->
      <div class="flex flex-col sm:flex-row items-stretch sm:items-center justify-between gap-3">
        <div class="flex flex-wrap items-center gap-2.5">
          <!-- 类型筛选 -->
          <div class="relative">
            <select
              v-model="filterType"
              class="form-input-base text-xs py-1.5 pl-3 pr-8 rounded-lg appearance-none bg-white cursor-pointer"
            >
              <option value="all">全部类型</option>
              <option value="agent">Agent 探针</option>
              <option value="xray">Xray 核心</option>
              <option value="sing-box">sing-box 核心</option>
            </select>
            <div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-2 text-gray-500">
              <svg class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
              </svg>
            </div>
          </div>

          <!-- 状态筛选 -->
          <div class="relative">
            <select
              v-model="filterStatus"
              class="form-input-base text-xs py-1.5 pl-3 pr-8 rounded-lg appearance-none bg-white cursor-pointer"
            >
              <option value="all">全部状态</option>
              <option value="running">执行中 / 待确认</option>
              <option value="queued">等待节点</option>
              <option value="success">升级成功</option>
              <option value="rolled_back">已回退 / 恢复</option>
              <option value="failed">拒绝 / 失败</option>
            </select>
            <div class="pointer-events-none absolute inset-y-0 right-0 flex items-center px-2 text-gray-500">
              <svg class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z" clip-rule="evenodd" />
              </svg>
            </div>
          </div>

          <!-- 搜索输入框 -->
          <div class="relative min-w-[200px]">
            <input
              v-model.trim="searchKeyword"
              type="text"
              placeholder="搜索服务器名称或版本..."
              class="form-input-base text-xs py-1.5 pl-8 pr-3 w-full rounded-lg"
            />
            <div class="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-2.5 text-gray-400">
              <svg class="h-3.5 w-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clip-rule="evenodd" />
              </svg>
            </div>
          </div>
        </div>

        <div class="text-xs text-gray-500 self-center">
          共 <span class="font-medium text-gray-800">{{ filteredTasks.length }}</span> 条任务记录
        </div>
      </div>

      <!-- 错误状态 -->
      <ErrorState v-if="error" :message="error" @retry="loadAll" />

      <!-- 加载中 -->
      <div v-else-if="loading && !allTasks.length" class="py-12">
        <LoadingSkeleton />
      </div>

      <!-- 空记录 -->
      <EmptyState
        v-else-if="!filteredTasks.length"
        title="暂无任务记录"
        :description="allTasks.length ? '没有匹配当前筛选条件的任务记录。' : '当前尚无 Agent 探针或代理核心的升级执行历史。'"
      />

      <!-- 统一任务列表 -->
      <DataTable v-else min-width="min-w-[860px]">
        <template #header>
          <th class="w-32">任务类型</th>
          <th class="w-48">目标服务器</th>
          <th class="w-36">目标版本</th>
          <th class="w-36">任务状态</th>
          <th>执行结果与诊断</th>
          <th class="w-40">更新时间</th>
          <th class="w-24 text-right">操作</th>
        </template>
        <template #body>
          <tr
            v-for="task in paginatedTasks"
            :key="task.id"
            class="hover:bg-gray-50/80 transition-colors"
          >
            <!-- 任务类型 -->
            <td class="p-3.5 align-middle">
              <span
                v-if="task.source === 'agent'"
                class="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-medium bg-blue-50 text-blue-700 border border-blue-200/60"
              >
                <svg class="w-3 h-3 text-blue-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M12 2v20M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6" />
                </svg>
                Agent 探针
              </span>
              <span
                v-else-if="task.component === 'xray'"
                class="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-medium bg-purple-50 text-purple-700 border border-purple-200/60"
              >
                <svg class="w-3 h-3 text-purple-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <circle cx="12" cy="12" r="3" />
                  <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
                </svg>
                Xray 核心
              </span>
              <span
                v-else
                class="inline-flex items-center gap-1 px-2.5 py-1 rounded-md text-xs font-medium bg-emerald-50 text-emerald-700 border border-emerald-200/60"
              >
                <svg class="w-3 h-3 text-emerald-500" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z" />
                </svg>
                sing-box 核心
              </span>
            </td>

            <!-- 目标服务器 -->
            <td class="p-3.5 align-middle">
              <router-link
                :to="`/nodes/${task.nodeId}`"
                class="font-medium text-blue-600 hover:text-blue-700 hover:underline inline-flex items-center gap-1"
              >
                {{ task.nodeName }}
              </router-link>
              <p class="text-[11px] text-gray-400 mt-0.5">
                配置版本 #{{ task.revision || 0 }}
              </p>
            </td>

            <!-- 目标版本 -->
            <td class="p-3.5 align-middle">
              <span class="font-mono text-xs text-gray-800 bg-gray-100 px-2 py-0.5 rounded">
                {{ task.targetVersion }}
              </span>
            </td>

            <!-- 任务状态 -->
            <td class="p-3.5 align-middle">
              <span
                :class="[
                  'inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-medium border',
                  statusClass(task.statusTone)
                ]"
              >
                <span
                  :class="[
                    'w-1.5 h-1.5 rounded-full shrink-0',
                    statusDotClass(task.statusTone),
                    task.status === 'running' ? 'animate-pulse' : ''
                  ]"
                />
                {{ task.statusLabel }}
              </span>
            </td>

            <!-- 执行结果与诊断 -->
            <td class="p-3.5 align-middle">
              <div class="space-y-0.5">
                <div class="flex items-center gap-2 flex-wrap">
                  <span class="text-xs text-gray-700 font-medium">
                    {{ task.resultLabel }}
                  </span>
                  <span
                    v-if="task.installedVersion"
                    class="text-[11px] font-mono text-gray-500 bg-gray-50 px-1.5 py-0.2 rounded border border-gray-200"
                  >
                    已装 {{ task.installedVersion }}
                  </span>
                </div>
                <p
                  v-if="task.resultGuidance"
                  class="text-[11px] text-amber-700 leading-relaxed"
                >
                  {{ task.resultGuidance }}
                </p>
              </div>
            </td>

            <!-- 更新时间 -->
            <td class="p-3.5 align-middle text-xs text-gray-500">
              {{ formatTime(task.updatedAt) }}
            </td>

            <!-- 操作 -->
            <td class="p-3.5 align-middle text-right">
              <button
                v-if="task.canClear"
                type="button"
                class="text-xs text-gray-500 hover:text-rose-600 transition-colors"
                :disabled="busyId === task.id"
                @click="clearCoreTask(task.id)"
              >
                {{ busyId === task.id ? '清理中…' : '清除' }}
              </button>
              <span v-else class="text-xs text-gray-300">—</span>
            </td>
          </tr>
        </template>
        <template #footer>
          <div class="p-4 border-t border-gray-100 flex items-center justify-between">
            <span class="text-xs text-gray-500">
              第 {{ currentPage }} 页，共 {{ totalPages }} 页 ({{ filteredTasks.length }} 条记录)
            </span>
            <Pagination
              :total="filteredTasks.length"
              :current-page="currentPage"
              :page-size="pageSize"
              @update:page="currentPage = $event"
            />
          </div>
        </template>
      </DataTable>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
import ErrorState from '../../../components/ErrorState.vue';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { agentUpdatesAPI, type AgentUpdateTask } from '../agentUpdatesApi';
import { coreUpdatesAPI, type CoreUpdateTask, type ReleaseEntry } from '../coreUpdatesApi';
import { updateResultLabel, updateResultGuidance } from '../updateResultText';
import { nodeAPI, type NodeView } from '../../nodes/api';

interface UnifiedTask {
  id: string;
  source: 'agent' | 'core';
  component: string;
  nodeId: string;
  nodeName: string;
  revision: number;
  targetVersion: string;
  status: string;
  statusLabel: string;
  statusTone: 'green' | 'yellow' | 'red' | 'gray' | 'blue';
  resultCode?: string;
  resultLabel: string;
  resultGuidance: string;
  installedVersion?: string;
  updatedAt: string;
  timestamp: number;
  canClear: boolean;
}

const loading = ref(false);
const error = ref('');
const busyId = ref('');
const nodes = ref<NodeView[]>([]);
const agentTasks = ref<AgentUpdateTask[]>([]);
const coreTasks = ref<CoreUpdateTask[]>([]);
const releases = ref<ReleaseEntry[]>([]);

const filterType = ref<'all' | 'agent' | 'xray' | 'sing-box'>('all');
const filterStatus = ref<'all' | 'running' | 'queued' | 'success' | 'rolled_back' | 'failed'>('all');
const searchKeyword = ref('');

const currentPage = ref(1);
const pageSize = 20;

let generation = 0;

function nodeName(id: string): string {
  const n = nodes.value.find(node => node.id === id);
  return n?.name || id;
}

function mapStatusMeta(status: string, source: 'agent' | 'core'): {
  label: string;
  tone: 'green' | 'yellow' | 'red' | 'gray' | 'blue';
} {
  switch (status) {
    case 'success':
      return { label: '升级成功', tone: 'green' };
    case 'running':
      return { label: '下发中，待确认', tone: 'blue' };
    case 'queued':
      return { label: '等待节点', tone: 'yellow' };
    case 'rolled_back':
      return { label: source === 'agent' ? '已恢复原 Agent' : '已回退上一核心', tone: 'yellow' };
    case 'rejected':
      return { label: '节点拒绝', tone: 'red' };
    case 'failed_closed':
      return { label: '未确认已关闭', tone: 'red' };
    case 'cleared':
      return { label: '已清除', tone: 'gray' };
    default:
      return { label: status, tone: 'gray' };
  }
}

function statusClass(tone: 'green' | 'yellow' | 'red' | 'gray' | 'blue'): string {
  const map: Record<string, string> = {
    green: 'bg-emerald-50 text-emerald-800 border-emerald-200',
    yellow: 'bg-amber-50 text-amber-800 border-amber-200',
    red: 'bg-rose-50 text-rose-800 border-rose-200',
    gray: 'bg-black/[0.04] text-[#48484A] border-black/[0.08]',
    blue: 'bg-blue-50 text-[#0071E3] border-blue-200'
  };
  return map[tone] || map.gray;
}

function statusDotClass(tone: 'green' | 'yellow' | 'red' | 'gray' | 'blue'): string {
  const map: Record<string, string> = {
    green: 'bg-emerald-500',
    yellow: 'bg-amber-500',
    red: 'bg-rose-500',
    gray: 'bg-[#6E6E73]',
    blue: 'bg-[#0071E3]'
  };
  return map[tone] || map.gray;
}

async function loadAll() {
  const cur = ++generation;
  loading.value = true;
  error.value = '';

  try {
    const [nodesRes, agentRes, coreRes, releaseRes] = await Promise.all([
      nodeAPI.list(1, '', false).catch(() => ({ items: [] as NodeView[], total: 0 })),
      agentUpdatesAPI.tasks(1, 100).catch(() => ({ items: [] as AgentUpdateTask[], total: 0 })),
      coreUpdatesAPI.tasks(1, 100).catch(() => ({ items: [] as CoreUpdateTask[], total: 0 })),
      coreUpdatesAPI.releases(1, 100).catch(() => ({ items: [] as ReleaseEntry[], total: 0, configured: false }))
    ]);

    if (cur !== generation) return;

    nodes.value = nodesRes.items || [];
    agentTasks.value = agentRes.items || [];
    coreTasks.value = coreRes.items || [];
    releases.value = releaseRes.items || [];
  } catch (e) {
    if (cur === generation) {
      error.value = (e as Error).message || '读取任务记录失败';
    }
  } finally {
    if (cur === generation) {
      loading.value = false;
    }
  }
}

// 统一合并 Agent 与 Core 的升级任务
const allTasks = computed<UnifiedTask[]>(() => {
  const releaseMap: Record<string, string> = {};
  for (const rel of releases.value) {
    if (rel.signed?.manifest?.version) {
      releaseMap[rel.id] = rel.signed.manifest.version;
    }
  }

  const list: UnifiedTask[] = [];

  // Agent updates
  for (const t of agentTasks.value) {
    const meta = mapStatusMeta(t.status, 'agent');
    const version = releaseMap[t.releaseId] || t.result?.installed_version || '最新版本';
    list.push({
      id: t.id,
      source: 'agent',
      component: 'agent',
      nodeId: t.nodeId,
      nodeName: nodeName(t.nodeId),
      revision: t.revision,
      targetVersion: version.startsWith('v') ? version : `v${version}`,
      status: t.status,
      statusLabel: meta.label,
      statusTone: meta.tone,
      resultCode: t.result?.code,
      resultLabel: updateResultLabel(t.result?.code),
      resultGuidance: updateResultGuidance(t.result?.code),
      installedVersion: t.result?.installed_version,
      updatedAt: t.updatedAt,
      timestamp: new Date(t.updatedAt).getTime() || 0,
      canClear: false
    });
  }

  // Core updates
  for (const t of coreTasks.value) {
    const meta = mapStatusMeta(t.status, 'core');
    list.push({
      id: t.id,
      source: 'core',
      component: t.component || 'xray',
      nodeId: t.nodeId,
      nodeName: nodeName(t.nodeId),
      revision: t.revision,
      targetVersion: `${t.component} ${t.version}`,
      status: t.status,
      statusLabel: meta.label,
      statusTone: meta.tone,
      resultCode: t.result?.code,
      resultLabel: updateResultLabel(t.result?.code),
      resultGuidance: updateResultGuidance(t.result?.code),
      installedVersion: t.result?.installed_version,
      updatedAt: t.updatedAt,
      timestamp: new Date(t.updatedAt).getTime() || 0,
      canClear: t.status !== 'cleared'
    });
  }

  // 按更新时间倒序排序 (最新任务排在最前)
  return list.sort((a, b) => b.timestamp - a.timestamp);
});

// 过滤后的任务列表
const filteredTasks = computed(() => {
  let result = allTasks.value;

  // 类型过滤
  if (filterType.value !== 'all') {
    if (filterType.value === 'agent') {
      result = result.filter(t => t.source === 'agent');
    } else {
      result = result.filter(t => t.component.toLowerCase() === filterType.value);
    }
  }

  // 状态过滤
  if (filterStatus.value !== 'all') {
    if (filterStatus.value === 'running') {
      result = result.filter(t => t.status === 'running');
    } else if (filterStatus.value === 'queued') {
      result = result.filter(t => t.status === 'queued');
    } else if (filterStatus.value === 'success') {
      result = result.filter(t => t.status === 'success');
    } else if (filterStatus.value === 'rolled_back') {
      result = result.filter(t => t.status === 'rolled_back');
    } else if (filterStatus.value === 'failed') {
      result = result.filter(t => ['rejected', 'failed_closed'].includes(t.status));
    }
  }

  // 关键字过滤
  if (searchKeyword.value) {
    const kw = searchKeyword.value.toLowerCase();
    result = result.filter(
      t =>
        t.nodeName.toLowerCase().includes(kw) ||
        t.targetVersion.toLowerCase().includes(kw) ||
        t.statusLabel.toLowerCase().includes(kw) ||
        (t.installedVersion && t.installedVersion.toLowerCase().includes(kw))
    );
  }

  return result;
});

const totalPages = computed(() => Math.ceil(filteredTasks.value.length / pageSize) || 1);

const paginatedTasks = computed(() => {
  const start = (currentPage.value - 1) * pageSize;
  return filteredTasks.value.slice(start, start + pageSize);
});

async function clearCoreTask(id: string) {
  if (busyId.value) return;
  busyId.value = id;
  try {
    await coreUpdatesAPI.clear(id);
    showToast('任务记录已清除');
    await loadAll();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    busyId.value = '';
  }
}

onMounted(() => {
  void loadAll();
  window.addEventListener('proxy-node-status', loadAll);
});

onUnmounted(() => {
  generation++;
  window.removeEventListener('proxy-node-status', loadAll);
});
</script>
