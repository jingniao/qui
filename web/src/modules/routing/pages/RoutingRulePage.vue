<template>
  <div class="space-y-6">
    <!-- Header -->
    <PageHeader
      :title="title"
      parent="分流控制"
      :description="description"
    >
      <template #actions>
        <button v-if="isOwner && egressGroups.length" type="button" class="ios-btn-secondary" @click="egressGroupsVisible = true">管理出站服务器组</button>
        <button v-if="isOwner" type="button" class="ios-btn-secondary" @click="openEgressGroup(null)">新建出站服务器组</button>
        <router-link to="/routing/rules/new" class="ios-btn-primary inline-flex items-center gap-1.5">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          <span>创建节点策略</span>
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
    <LoadingSkeleton v-if="loading && !display.ready" />
    <EmptyState
      v-else-if="display.ready && !rules.length"
      title="暂无节点策略"
      description="未命中任何自定义策略时，流量将使用各节点入站配置的默认出站路由。"
    />

    <!-- Rules Table -->
    <DataTable v-else-if="display.ready">
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
              <router-link :to="`/routing/rules/${rule.id}/edit`" class="text-ios-blue font-semibold hover:underline text-base truncate">
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
              ↳ {{ targetName(rule.action.type, rule.action.target_id) }}
            </p>
          </td>

          <!-- 5. Publishing Status -->
          <td class="text-xs">
            <div class="flex flex-col gap-1">
              <div class="flex items-center gap-1.5">
                <StatusBadge :status="rule.enabled ? 'active' : 'disabled'" />
              </div>
              <span class="text-xs text-neutral-400 font-sans">
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

    <AppDialog v-if="egressGroupsVisible" v-model="egressGroupsVisible" title="管理出站服务器组">
      <ul class="divide-y divide-black/[0.06]">
        <li v-for="group in egressGroups" :key="group.id" class="flex items-center gap-3 py-2 text-sm">
          <div class="min-w-0 flex-1">
            <p class="break-words font-semibold">{{ group.name }}</p>
            <p class="break-all text-xs text-ios-secondary">
              {{ group.serverIds.length }} 台手选服务器<span v-if="group.namePattern"> · 正则 {{ group.namePattern }}</span>
            </p>
          </div>
          <button type="button" :disabled="egressGroupDeleting" class="ios-action-btn-secondary" @click="openEgressGroup(group)">编辑</button>
          <button type="button" :disabled="egressGroupDeleting" class="ios-action-btn-danger" @click="confirmEgressGroupDeletion(group)">删除</button>
        </li>
      </ul>
      <p v-if="!egressGroups.length" class="text-sm text-ios-secondary">暂无出站服务器组。</p>
    </AppDialog>
    <ServerEgressGroupDialog v-if="egressGroupVisible" :group="editingEgressGroup" @close="closeEgressGroup" @saved="egressGroupSaved" />
    <ConfirmDialog
      v-model="confirmEgressGroupDelete"
      title="删除出站服务器组"
      :message="`删除出站服务器组「${deletingEgressGroup?.name || ''}」？组仍被节点策略引用时无法删除。`"
      @confirm="deleteEgressGroup"
    />

    <!-- Publish to nodes -->
    <AppDialog
      v-model="publishVisible"
      title="发布到节点"
      description="按当前已保存的配置和节点策略生成版本。涉及远端出站时，先向目标服务器发布，入口服务器确认后生效。"
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
          <span class="ml-2" :class="{ pending: 'text-ios-secondary', success: 'text-ios-green', failed: 'text-ios-red', unconfirmed: 'text-ios-orange' }[result.status]">{{ result.message }}</span>
        </p>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { readTogether } from '../../../app/readTogether';
import { APIError } from '../../../app/apiClient';
import { retainedQuery, removePageItem } from '../../../app/retainedQuery';
import { reactive, ref, onMounted, onUnmounted } from 'vue';
import { policyAPI, serverEgressGroupAPI, input, actionNames, type PolicyRecord, type ServerEgressGroup, type ServiceSet } from '../api';
import { proxyAPI, type Outbound, type OutboundGroup, type ProxyChain, choices } from '../../proxy/api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { deploymentAPI, type NodePublication } from '../../system/deploymentApi';
import { publishWatchTimeout } from '../../system/publishProgress';
import { revisionResult } from '../../system/revisionResult';
import { showToast } from '../../../app/api';
import { isOwner } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ServerEgressGroupDialog from '../components/ServerEgressGroupDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

withDefaults(
  defineProps<{
    title?: string;
    description?: string;
  }>(),
  {
    title: '节点策略',
    description: '管理服务器端的分流规则和访问控制，支持指定服务器或服务器组出站。'
  }
);

const outboundGroups = ref<OutboundGroup[]>([]);
const chains = ref<ProxyChain[]>([]);
const rules = ref<PolicyRecord[]>([]);
const services = ref<ServiceSet[]>([]);
const outbounds = ref<Outbound[]>([]);
const servers = ref<NodeView[]>([]);
const egressGroups = ref<ServerEgressGroup[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const error = ref('');
const loading = ref(false);
const target = ref<PolicyRecord | null>(null);
const operation = ref('');
const confirmVisible = ref(false);

function targetName(type: string, id: string) {
  if (type === 'ServerEgress') return servers.value.find(server => server.id === id)?.name || id;
  if (type === 'ServerEgressGroup') return egressGroups.value.find(group => group.id === id)?.name || id;
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
    case 'ServerEgress':
      return 'bg-blue-50 text-ios-blue';
    case 'OutboundGroup':
    case 'ServerEgressGroup':
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
    case 'ServerEgress':
      return 'bg-ios-blue';
    case 'OutboundGroup':
    case 'ServerEgressGroup':
      return 'bg-purple-500';
    case 'ProxyChain':
      return 'bg-amber-500';
    default:
      return 'bg-neutral-600';
  }
}

let generation = 0;
const display = retainedQuery(() => { rules.value = []; total.value = 0; });
async function load() {
  display.begin(page.value, keyword.value);
  const current = ++generation;
  loading.value = true; error.value = '';
  try {
    const [result, sets, outs, gs, cs, serverChoices, egress] = await readTogether([
      policyAPI.list(page.value, keyword.value),
      policyAPI.services(1, 100),
      proxyAPI.outbounds('', 1, 100),
      choices((p, s) => proxyAPI.groups('', p, s)),
      choices((p, s) => proxyAPI.chains('', p, s)),
      choices((p, size) => nodeAPI.choices(p, '', size)),
      serverEgressGroupAPI.list()
    ]);
    if (current !== generation) return;
    display.ready = true; rules.value = result.items;
    total.value = result.total;
    services.value = sets.items;
    outbounds.value = outs.items;
    outboundGroups.value = gs;
    chains.value = cs;
    servers.value = serverChoices;
    egressGroups.value = egress;
    error.value = '';
  } catch (e) {
    if (current === generation) { display.reject(e); error.value = (e as Error).message; }
  } finally {
    if (current === generation) loading.value = false;
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
      const id = target.value.id;
      await policyAPI.delete(id);
      removePageItem(rules, total, page, id, 20);
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
interface PublishOutcome { status: 'pending' | 'success' | 'failed' | 'unconfirmed'; message: string; }
interface PublishResult extends PublishOutcome { nodeId: string; name: string; }

const egressGroupVisible = ref(false);
const egressGroupsVisible = ref(false);
const editingEgressGroup = ref<ServerEgressGroup | null>(null);
const deletingEgressGroup = ref<ServerEgressGroup | null>(null);
const confirmEgressGroupDelete = ref(false);
const egressGroupDeleting = ref(false);

function openEgressGroup(group: ServerEgressGroup | null) {
  editingEgressGroup.value = group;
  egressGroupsVisible.value = false;
  egressGroupVisible.value = true;
}

function closeEgressGroup() {
  egressGroupVisible.value = false;
  if (editingEgressGroup.value) egressGroupsVisible.value = true;
}

async function egressGroupSaved(group: ServerEgressGroup) {
  showToast(editingEgressGroup.value ? '出站服务器组已更新' : '已创建出站服务器组');
  egressGroups.value = [...egressGroups.value.filter(item => item.id !== group.id), group];
  closeEgressGroup();
  await load();
}

function confirmEgressGroupDeletion(group: ServerEgressGroup) {
  deletingEgressGroup.value = group;
  confirmEgressGroupDelete.value = true;
}

async function deleteEgressGroup() {
  const group = deletingEgressGroup.value;
  if (!group) return;
  egressGroupDeleting.value = true;
  try {
    await serverEgressGroupAPI.delete(group.id);
    egressGroups.value = egressGroups.value.filter(item => item.id !== group.id);
    showToast('已删除出站服务器组');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    deletingEgressGroup.value = null;
    egressGroupDeleting.value = false;
  }
}

const publishVisible = ref(false);
const publishLoading = ref(false);
const publishNodes = ref<PublishChoice[]>([]);
const publishResults = ref<PublishResult[]>([]);
let mounted = true;

async function openPublish() {
  publishVisible.value = true;
  if (publishLoading.value) return;
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

async function waitForPublication(nodeId: string, publication: NodePublication): Promise<PublishOutcome> {
  const { revision, batchId } = publication;
  const deadline = Date.now() + publishWatchTimeout;
  while (mounted && Date.now() < deadline) {
    if (batchId) {
      const { status } = await deploymentAPI.batch(batchId);
      switch (status) {
        case 'success': return { status: 'success', message: `配置 #${revision} 已生效` };
        case 'failed': return { status: 'failed', message: `批次发布失败，配置 #${revision}` };
        case 'rolled_back': return { status: 'failed', message: `批次已回滚，配置 #${revision}` };
        case 'rollback_failed': return { status: 'unconfirmed', message: `批次回滚失败，仍可能占用服务器。批次 ${batchId}` };
        case 'deploying':
        case 'rolling_back': break;
        default: return { status: 'unconfirmed', message: `批次 ${batchId} 状态未确认（${status}）` };
      }
    } else {
      const { items } = await deploymentAPI.nodeRevisions(nodeId);
      const record = items.find(item => item.id === revision);
      switch (record?.status) {
        case 'success': return { status: 'success', message: `配置 #${revision} 已生效` };
        case 'failed':
          return { status: record.resultCode === 'rollback_failed' ? 'unconfirmed' : 'failed', message: `配置 #${revision}：${revisionResult(record.resultCode || 'failed')}` };
        case 'rollback':
        case 'rolled_back': return { status: 'failed', message: `配置 #${revision} 已回滚` };
        case 'pending': break;
        default: return { status: 'unconfirmed', message: `配置 #${revision} 状态未确认` };
      }
    }
    await new Promise(resolve => setTimeout(resolve, 3000));
  }
  return { status: 'unconfirmed', message: `${batchId ? `批次 ${batchId}` : `配置 #${revision}`} 暂未确认，等待超时` };
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
  try {
    for (const item of selected) {
      if (!mounted) return;
      item.checked = false;
      const feedback = reactive<PublishResult>({
        nodeId: item.id, name: item.name, status: 'pending', message: '正在提交发布'
      });
      publishResults.value.push(feedback);
      let publication: NodePublication | undefined;
      try {
        publication = await deploymentAPI.publishNode(item.id, '发布节点策略');
        feedback.message = `配置 #${publication.revision} 已提交，等待服务器确认`;
        // A submitted batch can reserve the next selected server as a target.
        // Continue only after a terminal outcome that releases its reservations.
        if (selected.length > 1) {
          Object.assign(feedback, await waitForPublication(item.id, publication));
        }
      } catch (e) {
        if (!publication && e instanceof APIError && e.code === 'server_egress_preflight_failed') {
          feedback.status = 'failed';
          feedback.message = `发布未提交：${e.message}`;
        } else {
          feedback.status = 'unconfirmed';
          feedback.message = publication ? `无法确认配置 #${publication.revision}：${(e as Error).message}` : `发布请求未确认：${(e as Error).message}`;
        }
      }
      if (feedback.status === 'unconfirmed') break;
    }
    if (!mounted) return;
    const succeeded = publishResults.value.filter(result => result.status === 'success').length;
    if (publishResults.value.some(result => result.status === 'unconfirmed')) {
      const remaining = selected.length - publishResults.value.length;
      showToast(remaining ? `发布已暂停，后续 ${remaining} 个节点未提交。请先确认当前发布结果` : '发布结果未确认，请查看配置记录', 'error');
    } else if (selected.length === 1 && publishResults.value[0]?.status === 'pending') {
      showToast('已提交发布，等待服务器确认');
    } else {
      showToast(`已确认生效 ${succeeded}/${selected.length} 个节点`, succeeded === selected.length ? 'success' : 'error');
    }
  } finally {
    publishLoading.value = false;
  }
}

onMounted(load);
onUnmounted(() => { mounted = false; });
</script>
