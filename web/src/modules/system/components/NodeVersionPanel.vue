<template>
  <div class="space-y-6">
    <PageHeader
      v-if="!embedded"
      :title="kind === 'agent' ? 'Agent 探针管理' : '代理核心版本'"
      parent="服务器"
      :description="kind === 'agent' ? '统一管理与监控各服务器上部署的 Agent 探针运行状态、心跳上报与自动更新任务。' : '检测各服务器代理核心运行状态、版本信息与健康状况，核心升级使用官方发布地址与 SHA-256 校验。'"
    >
      <template #actions>
        <button class="ios-btn-secondary inline-flex items-center gap-1.5" :disabled="loading" @click="load">
          <svg :class="['w-3.5 h-3.5 text-ios-secondary', loading ? 'animate-spin' : '']" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>刷新</span>
        </button>
      </template>
    </PageHeader>

    <!-- Search & Filter Bar (100% Aligned with Server Page) -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <form @submit.prevent="search" class="flex flex-wrap items-center gap-3 flex-1">
        <div class="relative flex-1 max-w-md">
          <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            v-model="keyword"
            :placeholder="kind === 'agent' ? '搜索服务器名称、IP、Agent 版本或系统...' : '搜索服务器名称、IP、核心名称或版本...'"
            class="ios-search-input"
          />
          <button
            v-if="keyword"
            type="button"
            @click="keyword = ''; search()"
            class="absolute right-2.5 top-1/2 -translate-y-1/2 text-ios-tertiary hover:text-ios-label p-0.5 rounded-full hover:bg-black/5 transition-colors"
            title="清空"
          >
            <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>
        <button class="ios-search-btn shrink-0">查询</button>

        <!-- Status Filter Dropdown -->
        <select v-model="statusFilter" @change="search" class="form-input-base !w-auto text-xs py-2">
          <option value="all">全部状态</option>
          <option value="online">在线</option>
          <option value="degraded">核心异常</option>
          <option value="applying">发布中</option>
          <option value="rollback">回滚中</option>
          <option value="error">异常</option>
          <option value="offline">离线</option>
          <option value="pending">等待登记</option>
          <option value="disabled">已停用</option>
        </select>
      </form>

      <!-- Right: View Mode Toggle & Refresh (Same as Server Page) -->
      <div class="flex items-center gap-2 shrink-0 self-end sm:self-auto">
        <!-- View Switcher (List vs Grid) -->
        <div class="flex items-center bg-black/[0.04] p-0.5 rounded-xl border border-black/[0.04]">
          <button
            type="button"
            @click="setDisplayMode('list')"
            :class="[
              'px-2.5 py-1 rounded-lg text-xs font-medium transition-all inline-flex items-center gap-1.5',
              displayMode === 'list'
                ? 'bg-white text-ios-label shadow-xs font-semibold'
                : 'text-ios-tertiary hover:text-ios-label'
            ]"
            title="列表视图"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
            </svg>
            <span>列表</span>
          </button>
          <button
            type="button"
            @click="setDisplayMode('grid')"
            :class="[
              'px-2.5 py-1 rounded-lg text-xs font-medium transition-all inline-flex items-center gap-1.5',
              displayMode === 'grid'
                ? 'bg-white text-ios-label shadow-xs font-semibold'
                : 'text-ios-tertiary hover:text-ios-label'
            ]"
            title="卡片视图"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
            </svg>
            <span>卡片</span>
          </button>
        </div>

        <!-- Refresh Button -->
        <button
          type="button"
          @click="load"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
          title="刷新数据"
        >
          <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span class="hidden sm:inline">刷新</span>
        </button>

        <!-- Toggle Batch Task Panel Button -->
        <button
          type="button"
          @click="showTaskPanel = !showTaskPanel"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
          :class="showTaskPanel ? 'bg-ios-blue text-white hover:bg-ios-blue/90 border-transparent' : ''"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <span>{{ showTaskPanel ? '收起任务面板' : (kind === 'agent' ? '批量升级任务' : '核心升级任务') }}</span>
        </button>
      </div>
    </div>

    <!-- State: Error, Loading, Empty -->
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <EmptyState
      v-else-if="!filteredNodes.length"
      title="暂无匹配服务器"
      description="未找到符合条件的服务器节点记录，请调整筛选或稍后刷新。"
    />

    <!-- ================= Mode 1: Table List View ================= -->
    <DataTable v-else-if="displayMode === 'list'">
      <template #header>
        <th>服务器</th>
        <th>运行状态</th>
        <th>操作系统 / 架构</th>
        <template v-if="kind === 'agent'">
          <th>Agent 探针版本</th>
          <th>协同代理核心</th>
          <th>核心健康状况</th>
        </template>
        <template v-else>
          <th>核心 / 版本</th>
          <th>健康运行状态</th>
          <th>支持网络协议</th>
        </template>
        <th>最近心跳报告</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="node in filteredNodes" :key="node.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. 服务器名称与地区/IP -->
          <td class="p-4">
            <div class="flex items-center gap-2.5">
              <div
                class="w-8 h-8 rounded-xl bg-blue-50 border border-blue-100 flex items-center justify-center shrink-0 text-base select-none cursor-default"
                :title="getCountryInfo(node).name"
              >
                {{ getCountryInfo(node).flag }}
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <router-link :to="`/nodes/${node.id}`" class="font-semibold text-ios-blue hover:underline text-[15px] truncate">
                    {{ node.name }}
                  </router-link>
                  <span v-if="node.deletedAt" class="px-1.5 py-0.2 rounded text-[10px] font-bold tracking-wide uppercase shrink-0 bg-black/[0.05] text-neutral-400">
                    已删除
                  </span>
                </div>
                <p class="text-[13px] text-ios-secondary font-mono truncate mt-0.5">
                  {{ node.publicIp || node.hostname || '等待 Agent 注册' }}
                </p>
              </div>
            </div>
          </td>

          <!-- 2. 状态胶囊 StatusBadge -->
          <td>
            <StatusBadge
              :status="getNodeStatusTone(node)"
              :label="statusCapsuleText(node)"
            />
          </td>

          <!-- 3. 系统架构 -->
          <td class="text-xs text-ios-secondary font-mono">
            <span class="px-2 py-0.5 rounded-md bg-neutral-100 text-neutral-700 font-medium">
              {{ node.os || '尚未上报' }}
            </span>
          </td>

          <!-- 4. Agent 模式专有列 -->
          <template v-if="kind === 'agent'">
            <td>
              <span class="font-mono text-xs font-bold px-2 py-0.5 rounded-md bg-blue-50 text-ios-blue">
                {{ node.agentVersion || '等待注册' }}
              </span>
            </td>
            <td class="text-xs font-mono text-ios-secondary">
              {{ coreVersion(node) }}
            </td>
            <td>
              <span
                class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold"
                :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                <span>{{ coreHealth(node) }}</span>
              </span>
            </td>
          </template>

          <!-- 5. 核心模式专有列 -->
          <template v-else>
            <td>
              <span class="font-mono text-xs font-bold px-2 py-0.5 rounded-md bg-purple-50 text-purple-700">
                {{ coreVersion(node) }}
              </span>
            </td>
            <td>
              <span
                class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold"
                :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                <span>{{ coreHealth(node) }}</span>
              </span>
            </td>
            <td>
              <div v-if="protocols(node).length" class="flex flex-wrap gap-1">
                <span
                  v-for="proto in protocols(node)"
                  :key="proto"
                  class="rounded bg-black/[0.04] px-1.5 py-0.2 text-[10px] font-mono font-bold uppercase text-neutral-700"
                >
                  {{ proto }}
                </span>
              </div>
              <span v-else class="text-xs text-ios-tertiary">尚未上报</span>
            </td>
          </template>

          <!-- 6. 心跳时间 -->
          <td class="text-xs font-mono tabular-nums text-ios-tertiary">
            {{ formatTime(node.lastHeartbeat) }}
          </td>

          <!-- 7. 操作 -->
          <td class="text-right pr-6 space-x-1.5">
            <button
              type="button"
              @click="scrollToTaskPanel"
              class="ios-action-btn-secondary"
            >
              升级任务
            </button>
            <router-link :to="`/nodes/${node.id}`" class="ios-action-btn-primary">
              详情
            </router-link>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- ================= Mode 2: Grid Card View (100% Aligned with Server Page Card Layout) ================= -->
    <div
      v-else-if="displayMode === 'grid'"
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in filteredNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <div class="space-y-3.5">
          <!-- Row 1: Flag · Name · OS & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="getCountryInfo(node).name"
              >
                {{ getCountryInfo(node).flag }}
              </span>
              <router-link
                :to="`/nodes/${node.id}`"
                class="font-bold text-[17px] text-neutral-900 truncate tracking-tight hover:text-ios-blue transition-colors"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span class="rounded bg-black/[0.04] px-1.5 py-0.5 text-[10px] font-mono font-bold uppercase text-neutral-700 shrink-0">
                {{ node.os || 'Linux' }}
              </span>
            </div>

            <!-- Status Badge Capsule -->
            <span
              class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold shrink-0 select-none"
              :class="statusCapsuleClass(node)"
            >
              <span class="w-1.5 h-1.5 rounded-full" :class="statusDotClass(node)"></span>
              <span>{{ statusCapsuleText(node) }}</span>
            </span>
          </div>

          <!-- Row 2: Sub-info (IP & Last Heartbeat) -->
          <div class="text-[12px] text-neutral-400 font-mono tracking-tight flex items-center justify-between">
            <span class="truncate">{{ node.publicIp || node.hostname || '未分配 IP' }}</span>
            <span class="shrink-0 text-neutral-400">{{ formatTime(node.lastHeartbeat) }}</span>
          </div>

          <!-- Row 3: Information Box (Grey rounded panel, Aligned with Server Page specs) -->
          <div class="bg-[#F8F9FA] rounded-2xl p-3 border border-black/[0.03] space-y-2">
            <!-- AGENT MODE -->
            <template v-if="kind === 'agent'">
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">Agent 探针版本</span>
                <span class="font-mono font-bold text-ios-blue px-2 py-0.5 rounded-md bg-blue-50/80">
                  {{ node.agentVersion || '等待上报' }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">协同代理核心</span>
                <span class="font-mono text-neutral-800 font-semibold truncate max-w-[150px]" :title="coreVersion(node)">
                  {{ coreVersion(node) }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">核心健康状况</span>
                <span
                  class="font-semibold px-2 py-0.5 rounded-full text-[11px]"
                  :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500')"
                >
                  {{ coreHealth(node) }}
                </span>
              </div>
            </template>

            <!-- CORE MODE -->
            <template v-else>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">核心与版本</span>
                <span class="font-mono font-bold text-purple-700 px-2 py-0.5 rounded-md bg-purple-50/80">
                  {{ coreVersion(node) }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">健康运行状态</span>
                <span
                  class="font-semibold px-2 py-0.5 rounded-full text-[11px]"
                  :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500')"
                >
                  {{ coreHealth(node) }}
                </span>
              </div>
              <div class="space-y-1 pt-0.5">
                <div class="text-[11px] text-neutral-400 font-medium">支持网络协议:</div>
                <div v-if="protocols(node).length" class="flex flex-wrap gap-1">
                  <span
                    v-for="proto in protocols(node)"
                    :key="proto"
                    class="rounded bg-black/[0.04] px-1.5 py-0.2 text-[10px] font-mono font-bold uppercase text-neutral-700"
                  >
                    {{ proto }}
                  </span>
                </div>
                <div v-else class="text-xs text-neutral-400 italic">尚未上报协议</div>
              </div>
            </template>
          </div>
        </div>

        <!-- Row 4: 彩色操作网格按钮 -->
        <div class="pt-2 border-t border-black/[0.05] grid grid-cols-2 gap-2">
          <!-- 1. 升级/任务 -->
          <button
            type="button"
            @click="scrollToTaskPanel"
            class="py-1.5 rounded-xl bg-[#FEF6EE] text-[#B54708] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            升级任务
          </button>

          <!-- 2. 详情 -->
          <router-link
            :to="`/nodes/${node.id}`"
            class="py-1.5 rounded-xl bg-neutral-100 text-neutral-600 hover:bg-neutral-200 font-semibold text-xs text-center transition-colors"
          >
            详情
          </router-link>
        </div>
      </div>
    </div>

    <!-- Pagination for Card View -->
    <div v-if="displayMode === 'grid'" class="pt-2">
      <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
    </div>

    <!-- Batch Update Panel (Collapsible Drawer / Section) -->
    <div ref="taskPanelRef" v-show="showTaskPanel" class="pt-2 animate-in fade-in duration-300">
      <CoreUpdatesPanel v-if="kind === 'core'" :nodes="nodes" />
      <AgentUpdatesPanel v-if="kind === 'agent'" :nodes="nodes" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref, computed } from 'vue';
import { formatTime } from '../../../app/auth';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { resolveCountryInfo, type CountryInfo } from '../../nodes/nodeMeta';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import CoreUpdatesPanel from './CoreUpdatesPanel.vue';
import AgentUpdatesPanel from './AgentUpdatesPanel.vue';

const props = withDefaults(
  defineProps<{
    kind: 'agent' | 'core';
    embedded?: boolean;
  }>(),
  { embedded: false }
);

const nodes = ref<NodeView[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const statusFilter = ref<'all' | 'online' | 'offline' | 'pending' | 'degraded' | 'applying' | 'rollback' | 'error' | 'disabled'>('all');
const loading = ref(false);
const error = ref('');

// Display Mode: 'grid' (Cards) or 'list' (Table)
const displayMode = ref<'grid' | 'list'>(
  (localStorage.getItem(`qui_${props.kind}_display_mode`) as 'grid' | 'list') || 'grid'
);

function setDisplayMode(mode: 'grid' | 'list') {
  displayMode.value = mode;
  localStorage.setItem(`qui_${props.kind}_display_mode`, mode);
}

// Collapsible Batch Task Panel
const showTaskPanel = ref(false);
const taskPanelRef = ref<HTMLElement | null>(null);

function scrollToTaskPanel() {
  showTaskPanel.value = true;
  setTimeout(() => {
    taskPanelRef.value?.scrollIntoView({ behavior: 'smooth' });
  }, 100);
}

let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  try {
    const result = await nodeAPI.list(page.value, keyword.value);
    if (current === generation) {
      nodes.value = result.items;
      total.value = result.total;
    }
  } catch (e) {
    if (current === generation) error.value = (e as Error).message;
  } finally {
    if (current === generation) loading.value = false;
  }
}

function search() {
  page.value = 1;
  void load();
}

function changePage(value: number) {
  page.value = value;
  void load();
}

const filteredNodes = computed(() => {
  return nodes.value.filter(n => {
    if (statusFilter.value !== 'all' && n.status !== statusFilter.value) {
      return false;
    }
    return true;
  });
});

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

function getCountryInfo(node: NodeView): CountryInfo {
  return resolveCountryInfo(node.name, node.publicIp || node.hostname);
}

function getNodeStatusTone(node: NodeView): 'active' | 'error' | 'warning' {
  if (node.status === 'online') return 'active';
  if (node.status === 'offline' || node.status === 'error') return 'error';
  return 'warning';
}

function statusCapsuleClass(node: NodeView): string {
  if (node.status === 'online') return 'bg-[#E8F8F0] text-[#12B76A]';
  if (node.status === 'offline') return 'bg-neutral-100 text-neutral-500';
  if (node.status === 'error') return 'bg-red-50 text-red-600';
  if (node.status === 'degraded') return 'bg-amber-50 text-amber-700';
  if (node.status === 'applying' || node.status === 'rollback') return 'bg-blue-50 text-ios-blue';
  if (node.status === 'pending') return 'bg-neutral-100 text-neutral-500';
  if (node.status === 'disabled') return 'bg-neutral-100 text-neutral-500';
  return 'bg-neutral-100 text-neutral-500';
}

function statusDotClass(node: NodeView): string {
  if (node.status === 'online') return 'bg-[#12B76A]';
  if (node.status === 'offline') return 'bg-neutral-400';
  if (node.status === 'error') return 'bg-red-500';
  if (node.status === 'degraded') return 'bg-amber-500';
  if (node.status === 'applying' || node.status === 'rollback') return 'bg-ios-blue';
  if (node.status === 'pending') return 'bg-neutral-400';
  if (node.status === 'disabled') return 'bg-neutral-400';
  return 'bg-neutral-400';
}

function statusCapsuleText(node: NodeView): string {
  if (node.status === 'online') return '在线';
  if (node.status === 'degraded') return '核心异常';
  if (node.status === 'applying') return '发布中';
  if (node.status === 'rollback') return '回滚中';
  if (node.status === 'error') return '异常';
  if (node.status === 'offline') return '离线';
  if (node.status === 'pending') return '等待登记';
  if (node.status === 'disabled') return '已停用';
  return node.status;
}

onMounted(() => {
  void load();
  window.addEventListener('proxy-node-status', load);
});

onUnmounted(() => {
  generation++;
  window.removeEventListener('proxy-node-status', load);
});
</script>
