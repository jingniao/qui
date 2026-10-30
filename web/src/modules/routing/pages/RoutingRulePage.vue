<template>
  <div class="space-y-6">
    <!-- Header -->
    <PageHeader
      :title="title"
      parent="分流控制"
      :description="description"
    >
      <template #actions>
        <router-link to="/routing/rules/new" class="ios-btn-primary inline-flex items-center gap-1.5">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          <span>创建节点策略</span>
        </router-link>
        <router-link to="/routing/simulator?tab=node" class="ios-btn-secondary">
          策略模拟
        </router-link>
        <button
          v-if="isOwner"
          type="button"
          @click="openPublish"
          class="ios-btn-secondary"
        >
          发布到节点
        </button>
      </template>
    </PageHeader>

    <!-- Important Notice Banner -->
    <div class="p-4 rounded-2xl bg-ios-blue/[0.06] border border-ios-blue/15 space-y-2">
      <div class="flex items-center gap-2 text-ios-blue font-semibold text-xs tracking-wide">
        <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>节点端分流与网站控制生效规范</span>
      </div>
      <p class="text-xs text-neutral-600 leading-relaxed">
        • <strong>已合并功能</strong>：原“分流规则”与“网站控制”已整合为统一的“节点策略”，由服务器内核（Xray / Sing-box）集中执行。<br />
        • <strong>服务器直连 (Direct)</strong>：由服务器节点直接发起外部出站访问，与用户设备端的“客户端直连”严格区分。<br />
        • <strong>Agent 发布生效机制</strong>：此处保存的修改为配置草稿，生成配置版本并发布到各节点后生效；Agent 不会自动拉取草稿。
      </p>
    </div>

    <!-- Search Bar -->
    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索策略名称、范围或条件…"
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
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-ruleodd="evenodd" />
          </svg>
        </button>
      </div>
      <button class="ios-search-btn shrink-0">查询</button>
    </form>

    <!-- Table States -->
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !rules.length" />
    <EmptyState
      v-else-if="!rules.length"
      title="暂无节点策略"
      description="未命中任何自定义策略时，流量将使用各节点入站配置的默认出站路由。"
    />

    <!-- Rules Table -->
    <DataTable v-else>
      <template #header>
        <th>优先级 / 策略名称</th>
        <th>生效范围 (Scope)</th>
        <th>匹配条件 / 规则集</th>
        <th>服务器动作 (Action)</th>
        <th>发布与运行状态</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="rule in rules" :key="rule.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. Priority & Name -->
          <td class="p-4">
            <div class="flex items-center gap-2">
              <span class="font-mono tabular-nums text-xs font-bold text-neutral-400">#{{ rule.priority }}</span>
              <router-link :to="`/routing/rules/${rule.id}/edit`" class="text-ios-blue font-semibold hover:underline text-[15px] truncate">
                {{ rule.name }}
              </router-link>
            </div>
            <p v-if="rule.remark" class="text-xs text-neutral-400 mt-0.5 truncate max-w-xs">{{ rule.remark }}</p>
          </td>

          <!-- 2. Scope -->
          <td class="text-xs text-ios-secondary">
            <div class="flex flex-wrap gap-1">
              <span class="px-2 py-0.5 rounded-md bg-neutral-100 text-neutral-700 font-medium">
                {{ scopeSummary(rule) }}
              </span>
            </div>
          </td>

          <!-- 3. Match Conditions -->
          <td class="text-xs max-w-72 text-ios-secondary">
            <p class="font-mono text-neutral-800 line-clamp-2 leading-relaxed">{{ matchSummary(rule) }}</p>
            <p v-if="rule.match.time_range" class="text-ios-tertiary mt-1 flex items-center gap-1 font-sans">
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span>{{ rule.match.time_range }} · {{ rule.timezone }}</span>
            </p>
          </td>

          <!-- 4. Action -->
          <td class="text-xs">
            <span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full font-semibold" :class="getActionBadgeClass(rule.action.type)">
              <span class="w-1.5 h-1.5 rounded-full" :class="getActionDotClass(rule.action.type)"></span>
              <span>{{ actionNames[rule.action.type] || rule.action.type }}</span>
            </span>
            <p v-if="rule.action.target_id" class="text-xs text-ios-secondary mt-1 font-mono font-medium truncate max-w-[180px]">
              ↳ {{ targetName(rule.action.target_id) }}
            </p>
          </td>

          <!-- 5. Publishing Status -->
          <td class="text-xs">
            <div class="flex flex-col gap-1">
              <div class="flex items-center gap-1.5">
                <StatusBadge :status="rule.enabled ? 'active' : 'disabled'" />
              </div>
              <span class="text-[11px] text-neutral-400 font-sans">
                保存后需发布生效
              </span>
            </div>
          </td>

          <!-- 6. Actions -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <router-link :to="`/routing/rules/${rule.id}/edit`" class="ios-action-btn-primary">
              编辑
            </router-link>
            <button type="button" @click="prompt(rule, 'status')" class="ios-action-btn-secondary">
              {{ rule.enabled ? '停用' : '启用' }}
            </button>
            <button type="button" @click="prompt(rule, 'delete')" class="ios-action-btn-danger">
              删除
            </button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- Confirm Dialog -->
    <ConfirmDialog
      v-model="confirmVisible"
      title="确认策略草稿修改"
      :message="`对策略 ${target?.name || ''} 执行${operation === 'delete' ? '删除' : '启用状态切换'}？修改后需等待或执行 Agent 发布配置才会更新节点运行状态。`"
      @confirm="execute"
    />

    <!-- Publish to nodes -->
    <AppDialog
      v-model="publishVisible"
      title="发布到节点"
      description="按各节点当前已保存的入站、出站、出口组、代理链与分流规则生成配置版本并发布，节点确认后生效。"
      confirm-text="开始发布"
      :loading="publishLoading"
      @confirm="runPublish"
    >
      <p v-if="!publishNodes.length" class="text-sm text-ios-tertiary">没有可发布的已启用节点。</p>
      <div v-else class="space-y-2">
        <label
          v-for="item in publishNodes"
          :key="item.id"
          class="flex items-center gap-2.5 text-sm cursor-pointer"
        >
          <input v-model="item.checked" type="checkbox" :disabled="publishLoading" class="w-4 h-4 shrink-0" />
          <span class="text-ios-label">{{ item.name }}</span>
        </label>
      </div>
      <div v-if="publishResults.length" class="space-y-1.5 border-t border-black/[0.06] pt-3">
        <p v-for="result in publishResults" :key="result.nodeId" class="text-xs">
          <span class="font-medium text-ios-label">{{ result.name }}</span>
          <span class="ml-2" :class="result.ok ? 'text-ios-green' : 'text-ios-red'">{{ result.message }}</span>
        </p>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { policyAPI, input, actionNames, type PolicyRecord, type ServiceSet } from '../api';
import { proxyAPI, type Outbound, type OutboundGroup, type ProxyChain, choices } from '../../proxy/api';
import { nodeAPI } from '../../nodes/api';
import { deploymentAPI } from '../../system/deploymentApi';
import { showToast } from '../../../app/api';
import { isOwner } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

withDefaults(
  defineProps<{
    title?: string;
    description?: string;
    website?: boolean;
  }>(),
  {
    title: '节点策略',
    description: '合并管理服务器端（Xray / Sing-box）的分流规则与访问控制。通过 Agent 发布到服务器，支持指定用户、节点、直连、阻断、出站与代理链。',
    website: false
  }
);

const outboundGroups = ref<OutboundGroup[]>([]);
const chains = ref<ProxyChain[]>([]);
const rules = ref<PolicyRecord[]>([]);
const services = ref<ServiceSet[]>([]);
const outbounds = ref<Outbound[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const error = ref('');
const loading = ref(false);
const target = ref<PolicyRecord | null>(null);
const operation = ref('');
const confirmVisible = ref(false);

function targetName(id: string) {
  return (
    outbounds.value.find(item => item.id === id)?.name ||
    outboundGroups.value.find(item => item.id === id)?.name ||
    chains.value.find(item => item.id === id)?.name ||
    id
  );
}

function scopeSummary(rule: PolicyRecord) {
  // v1.1 has no device scope: a user has one access identity, so the device count
  // that used to appear here no longer exists.
  const parts = [
    [rule.scope.user_ids?.length, '用户'],
    [rule.scope.node_ids?.length, '节点'],
    [rule.scope.group_ids?.length, '节点分组']
  ]
    .filter(item => item[0])
    .map(item => item[0] + ' ' + item[1]);
  return parts.join(' · ') || '全部用户与节点';
}

function matchSummary(rule: PolicyRecord) {
  const addresses = [
    ...(rule.match.domains || []),
    ...(rule.match.suffixes || []).map(value => '.' + value),
    ...(rule.match.cidrs || []),
    ...(rule.match.service_ids || []).map(id => services.value.find(service => service.id === id)?.name || id)
  ];
  return (
    (addresses.slice(0, 3).join('、') || '全部目标地址') +
    (addresses.length > 3 ? ` 等共 ${addresses.length} 项` : '') +
    ' · ' +
    (rule.match.network === 'all' ? 'TCP+UDP' : rule.match.network?.toUpperCase() || 'TCP') +
    (rule.match.ports?.length ? ' · 端口 ' + rule.match.ports.join(',') : '')
  );
}

function getActionBadgeClass(type: string): string {
  switch (type) {
    case 'Direct':
      return 'bg-emerald-50 text-emerald-700';
    case 'Block':
      return 'bg-red-50 text-red-600';
    case 'Outbound':
      return 'bg-blue-50 text-ios-blue';
    case 'OutboundGroup':
      return 'bg-purple-50 text-purple-700';
    case 'ProxyChain':
      return 'bg-amber-50 text-amber-700';
    default:
      return 'bg-neutral-100 text-neutral-800';
  }
}

function getActionDotClass(type: string): string {
  switch (type) {
    case 'Direct':
      return 'bg-emerald-500';
    case 'Block':
      return 'bg-red-500';
    case 'Outbound':
      return 'bg-ios-blue';
    case 'OutboundGroup':
      return 'bg-purple-500';
    case 'ProxyChain':
      return 'bg-amber-500';
    default:
      return 'bg-neutral-600';
  }
}

async function load() {
  loading.value = true;
  try {
    const [result, sets, outs, gs, cs] = await Promise.all([
      policyAPI.list(page.value, keyword.value),
      policyAPI.services(1, 100),
      proxyAPI.outbounds('', 1, 100),
      choices((p, s) => proxyAPI.groups('', p, s)),
      choices((p, s) => proxyAPI.chains('', p, s))
    ]);
    rules.value = result.items;
    total.value = result.total;
    services.value = sets.items;
    outbounds.value = outs.items;
    outboundGroups.value = gs;
    chains.value = cs;
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function search() {
  page.value = 1;
  load();
}

function changePage(value: number) {
  page.value = value;
  load();
}

function prompt(rule: PolicyRecord, action: string) {
  target.value = rule;
  operation.value = action;
  confirmVisible.value = true;
}

async function execute() {
  if (!target.value) return;
  try {
    if (operation.value === 'delete') {
      await policyAPI.delete(target.value.id);
    } else {
      await policyAPI.save(target.value.id, { ...input(target.value), enabled: !target.value.enabled });
    }
    showToast('策略草稿已更新');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// ----------------- Publish to nodes -----------------
interface PublishChoice { id: string; name: string; checked: boolean; }
interface PublishResult { nodeId: string; name: string; ok: boolean; message: string; }

const publishVisible = ref(false);
const publishLoading = ref(false);
const publishNodes = ref<PublishChoice[]>([]);
const publishResults = ref<PublishResult[]>([]);

// Saving a strategy only writes a draft. Publishing renders each node's saved
// configuration and hands it to that node's Agent in one step, one node at a
// time, so a failure on one node still lets the rest continue.
async function openPublish() {
  publishVisible.value = true;
  publishResults.value = [];
  publishNodes.value = [];
  try {
    const choices: PublishChoice[] = [];
    for (let current = 1; ; current++) {
      const result = await nodeAPI.list(current, '');
      choices.push(...result.items
        .filter(item => item.enabled && !item.deletedAt)
        .map(item => ({ id: item.id, name: item.name, checked: true })));
      if (current * 20 >= result.total) break;
    }
    publishNodes.value = choices;
    if (!choices.length) showToast('没有可发布的已启用节点', 'error');
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

async function runPublish() {
  if (publishLoading.value) return;
  const selected = publishNodes.value.filter(item => item.checked);
  if (!selected.length) {
    showToast('请至少选择一个节点', 'error');
    return;
  }
  publishLoading.value = true;
  publishResults.value = [];
  for (const item of selected) {
    try {
      const result = await deploymentAPI.publishNode(item.id, '发布节点策略');
      publishResults.value.push({ nodeId: item.id, name: item.name, ok: true, message: `成功 #${result.revision}` });
    } catch (e) {
      publishResults.value.push({ nodeId: item.id, name: item.name, ok: false, message: `失败: ${(e as Error).message}` });
    }
  }
  publishLoading.value = false;
  const succeeded = publishResults.value.filter(result => result.ok).length;
  showToast(`已发布 ${succeeded}/${selected.length} 个节点，节点确认后生效`, succeeded === selected.length ? 'success' : 'error');
}

onMounted(load);
</script>
