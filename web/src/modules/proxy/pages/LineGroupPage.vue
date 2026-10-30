<template>
  <div class="space-y-6">
    <PageHeader
      title="节点分组"
      parent="节点配置"
      description="按代理节点灵活管理分组，支持在分流策略与规则中引用。"
    >
      <template #actions>
        <button class="ios-btn-secondary" :disabled="loading || busy" @click="load">刷新</button>
        <button v-if="isOwner" class="ios-btn-primary" :disabled="loading || !!error || busy" @click="edit(null)">新建节点组</button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <p v-else-if="loading" role="status" class="text-sm text-ios-tertiary">正在加载节点分组…</p>
    <EmptyState
      v-else-if="!groups.length"
      title="暂无节点分组"
      description="将代理节点加入分组，可在分流控制与策略中统一引用。"
    />
    <div v-else class="grid grid-cols-1 lg:grid-cols-4 gap-6 items-start">
      <!-- Left sidebar list -->
      <aside class="panel-card p-4 space-y-4 lg:col-span-1" aria-label="节点分组列表">
        <input v-model="keyword" class="form-input-base" placeholder="搜索节点分组" aria-label="搜索节点分组" />
        <section class="space-y-2">
          <button
            v-for="group in filteredGroups"
            :key="group.id"
            type="button"
            :aria-current="selected?.id === group.id ? 'true' : undefined"
            class="w-full rounded-xl p-3 text-left transition-all active:scale-95"
            :class="[
              selected?.id === group.id ? 'bg-ios-blue/10 ring-1 ring-ios-blue/20' : 'hover:bg-ios-bg',
              { 'opacity-50': !group.enabled }
            ]"
            @click="selectGroup(group.id)"
          >
            <span class="block text-sm font-semibold text-ios-label break-words">{{ group.name }}</span>
            <span class="text-xs text-ios-secondary font-mono tabular-nums">
              {{ groupNodesFor(group.id).length }} 个节点
            </span>
          </button>
        </section>
        <p v-if="!filteredGroups.length" class="text-sm text-ios-tertiary">没有符合条件的节点分组</p>
      </aside>

      <!-- Right main content -->
      <div v-if="selected" class="lg:col-span-3 space-y-6 min-w-0">
        <!-- 1. Group info card -->
        <section class="panel-card p-6 space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-[17px] sm:text-lg font-semibold text-ios-label break-words">{{ selected.name }}</h2>
            <div v-if="isOwner" class="flex gap-2">
              <button class="ios-btn-secondary" :disabled="busy" @click="edit(selected)">编辑</button>
              <button class="ios-btn-danger" :disabled="busy" @click="askDelete">删除</button>
            </div>
          </div>
          <div class="flex flex-wrap items-center gap-2 text-xs text-ios-secondary">
            <span>{{ groupPurposeNames[selected.purpose] || '节点分组' }}</span>
            <StatusBadge :status="selected.enabled ? 'active' : 'disabled'" />
          </div>
          <p v-if="selected.remark" class="text-sm text-ios-secondary whitespace-pre-wrap break-words">{{ selected.remark }}</p>
        </section>

        <!-- 2. Nodes in group (Group Nodes Table) -->
        <section class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-[17px] font-semibold text-ios-label">
              组内节点 <span class="font-mono tabular-nums">（{{ currentGroupNodes.length }}）</span>
            </h2>
            <button v-if="isOwner" class="ios-btn-secondary" :disabled="busy" @click="openPicker">+ 添加节点</button>
          </div>

          <DataTable v-if="currentGroupNodes.length">
            <template #header>
              <th>节点名称</th>
              <th>来源类型</th>
              <th>协议</th>
              <th>端口</th>
              <th>所属服务器 / 来源</th>
              <th>状态</th>
              <th v-if="isOwner">操作</th>
            </template>
            <template #body>
              <tr v-for="node in currentGroupNodes" :key="node.id">
                <td>
                  <span class="text-[15px] font-semibold text-ios-label">
                    {{ node.name }}
                  </span>
                </td>
                <td>
                  <span
                    class="px-2 py-0.5 rounded text-xs font-medium"
                    :class="sourceBadgeClass(node.sourceType)"
                  >
                    {{ node.sourceTypeName }}
                  </span>
                </td>
                <td>
                  <span class="inline-flex items-center px-2 py-0.5 rounded-md text-xs font-mono font-bold uppercase bg-ios-blue/10 text-ios-blue">
                    {{ node.protocol }}
                  </span>
                </td>
                <td class="font-mono tabular-nums text-xs text-ios-secondary">
                  {{ node.port }}
                </td>
                <td>
                  <router-link
                    v-if="node.serverId && serverMap[node.serverId]"
                    :to="`/nodes/${node.serverId}`"
                    class="text-xs text-ios-blue hover:underline inline-flex items-center gap-1.5"
                  >
                    <span>{{ serverMap[node.serverId].name }}</span>
                    <span
                      class="w-1.5 h-1.5 rounded-full inline-block"
                      :class="serverMap[node.serverId].status === 'online' ? 'bg-emerald-500' : 'bg-neutral-400'"
                    />
                  </router-link>
                  <span v-else class="text-xs text-ios-secondary font-medium">
                    {{ node.sourceName }}
                  </span>
                </td>
                <td>
                  <StatusBadge :status="node.enabled ? 'active' : 'disabled'" />
                </td>
                <td v-if="isOwner">
                  <button class="ios-action-btn-danger" :disabled="busy" @click="removeNode(node.id)">移出</button>
                </td>
              </tr>
            </template>
          </DataTable>
          <EmptyState
            v-else
            title="暂无组内节点"
            description="添加自建代理节点或导入节点后，可在此查看已加入该组的具体节点。"
          />
        </section>

        <!-- 3. Policy References -->
        <section class="panel-card p-6 space-y-3">
          <h2 class="text-[17px] font-semibold text-ios-label">被引用</h2>
          <ul v-if="references.length" class="space-y-2 text-sm">
            <li v-for="reference in references" :key="reference.key">
              <router-link :to="reference.path" class="text-ios-blue hover:underline">{{ reference.label }}</router-link>
            </li>
          </ul>
          <p v-else class="text-sm text-ios-tertiary">暂无引用</p>
        </section>
      </div>
    </div>

    <!-- Modals -->
    <LineGroupEditor v-model="editorOpen" :group="editing" @saved="saved" />
    <LineGroupPicker
      v-if="pickerGroup"
      v-model="pickerOpen"
      :group="pickerGroup"
      :servers="servers"
      :nodes="allAvailableNodes"
      :current-selected-ids="pickerNodeIds"
      :saving="busy"
      :error="pickerError"
      @save="savePicker"
    />
    <ConfirmDialog v-model="confirmOpen" :title="confirmTitle" :message="confirmMessage" @confirm="runConfirmed" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { request, type Page } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import { isOwner } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import LineGroupEditor from '../components/LineGroupEditor.vue';
import LineGroupPicker, { type GroupPickerNode } from '../components/LineGroupPicker.vue';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import type { NodeView } from '../../nodes/api';
import { choices, proxyAPI, type Inbound } from '../api';
import { loadImports } from '../importApi';
import { proxyNodeStore } from '../proxyNodes';
import { policyAPI, type PolicyRecord, type StoredRuleTemplate } from '../../routing/api';
import { groupInput, groupPurposeNames } from '../lineGroups';

const route = useRoute();
const router = useRouter();

const groups = ref<NodeGroup[]>([]);
const servers = ref<NodeView[]>([]);
const inbounds = ref<Inbound[]>([]);
const policies = ref<PolicyRecord[]>([]);
const clientTemplate = ref<StoredRuleTemplate | null>(null);

const loading = ref(true);
const error = ref('');
const busy = ref(false);
const keyword = ref('');
const editorOpen = ref(false);
const editing = ref<NodeGroup | null>(null);

const pickerOpen = ref(false);
const pickerGroup = ref<NodeGroup | null>(null);
const pickerError = ref('');

// Group -> Node IDs mapping
const groupNodeMap = reactive<Record<string, string[]>>({});

const serverMap = computed(() => {
  const map: Record<string, NodeView> = {};
  for (const s of servers.value) {
    map[s.id] = s;
  }
  return map;
});

function sourceBadgeClass(type: string) {
  if (type === 'self_hosted') return 'bg-emerald-100 text-emerald-800';
  if (type === 'subscription') return 'bg-purple-100 text-purple-800';
  return 'bg-blue-100 text-blue-800';
}

// All available nodes combining self-hosted inbounds and imported nodes
const allAvailableNodes = computed<GroupPickerNode[]>(() => {
  const result: GroupPickerNode[] = [];

  // 1. Self-hosted Inbounds
  for (const ib of inbounds.value) {
    const srv = serverMap.value[ib.nodeId];
    result.push({
      id: ib.id,
      name: ib.name || `${ib.protocol.toUpperCase()}-${ib.port}`,
      protocol: ib.protocol,
      port: ib.port,
      sourceType: 'self_hosted',
      sourceTypeName: '自建',
      sourceName: srv ? srv.name : (ib.nodeId || '自建服务器'),
      serverId: ib.nodeId,
      status: ib.enabled ? 'active' : 'disabled',
      enabled: ib.enabled,
      subscriptionVisible: ib.subscriptionVisible,
      internalRelay: ib.internalRelay
    });
  }

  // 2. Imported manual nodes & subscription nodes from proxyNodeStore
  for (const node of proxyNodeStore.manualNodes) {
    const isManual = node.sourceType === 'manual';
    result.push({
      id: node.id,
      name: node.name,
      protocol: node.protocol,
      port: node.port,
      sourceType: isManual ? 'manual' : 'subscription',
      sourceTypeName: isManual ? '单独' : '机场',
      sourceName: node.sourceName || (isManual ? '单独导入' : '机场订阅'),
      status: node.status === 'online' ? 'active' : 'disabled',
      enabled: true,
      subscriptionVisible: true,
      internalRelay: false
    });
  }

  return result;
});

const selected = computed(() => groups.value.find(group => group.id === route.query.group) || groups.value[0]);

function groupNodesFor(groupId: string): GroupPickerNode[] {
  const ids = groupNodeMap[groupId] || [];
  return ids.map(id => allAvailableNodes.value.find(n => n.id === id)).filter(Boolean) as GroupPickerNode[];
}

const currentGroupNodes = computed(() => {
  return selected.value ? groupNodesFor(selected.value.id) : [];
});

const pickerNodeIds = computed(() => {
  return pickerGroup.value ? (groupNodeMap[pickerGroup.value.id] || []) : [];
});

const filteredGroups = computed(() => {
  const q = keyword.value.trim().toLowerCase();
  if (!q) return groups.value;
  return groups.value.filter(group => group.name.toLowerCase().includes(q));
});

const references = computed(() => {
  const id = selected.value?.id;
  if (!id) return [];
  return [
    ...policies.value.filter(policy => policy.scope.group_ids?.includes(id)).map(policy => ({
      key: `policy:${policy.id}`,
      label: `节点策略：${policy.name}${policy.enabled ? '' : '（已停用）'}`,
      path: `/routing/rules/${policy.id}/edit`
    })),
    ...(clientTemplate.value?.template.groups || []).filter(group => group.members.includes(`@nodeGroup:${id}`)).map(group => ({
      key: `client:${group.name}`,
      label: `客户端策略：${group.name}`,
      path: '/routing/client'
    }))
  ];
});

function initGroupNodes() {
  for (const g of groups.value) {
    const storageKeyNew = `qui_group_nodes_${g.id}`;
    const storageKeyOld = `qui_group_inbounds_${g.id}`;
    const saved = localStorage.getItem(storageKeyNew) || localStorage.getItem(storageKeyOld);
    if (saved) {
      try {
        const parsed = JSON.parse(saved) as string[];
        groupNodeMap[g.id] = parsed.filter(id => allAvailableNodes.value.some(n => n.id === id));
      } catch {
        // Fallback
      }
    }
    if (!groupNodeMap[g.id]) {
      groupNodeMap[g.id] = inbounds.value
        .filter(inbound => g.nodeIds.includes(inbound.nodeId) && inbound.subscriptionVisible && !inbound.internalRelay)
        .map(i => i.id);
      localStorage.setItem(storageKeyNew, JSON.stringify(groupNodeMap[g.id]));
    }
  }
}

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const data = await Promise.all([
      choices(groupAPI.list),
      choices((page, size) => request<Page<NodeView>>(`/nodes?page=${page}&pageSize=${size}`)),
      choices((page, size) => proxyAPI.inbounds('', page, size)),
      choices((page, size) => policyAPI.list(page, '', size)),
      request<StoredRuleTemplate>('/subscription-rules'),
      loadImports().catch(() => {})
    ]);
    [groups.value, servers.value, inbounds.value, policies.value, clientTemplate.value] = data;
    initGroupNodes();
    syncSelection();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

const ours = () => route.path === '/proxy/groups';
function selectGroup(id: string) {
  if (!ours()) return;
  router.replace({ query: { ...route.query, group: id } });
}

function syncSelection() {
  if (!ours()) return;
  if (selected.value && selected.value.id !== route.query.group) {
    selectGroup(selected.value.id);
  } else if (!groups.value.length && route.query.group) {
    const query = { ...route.query };
    delete query.group;
    router.replace({ query });
  }
}
watch(() => route.query.group, syncSelection);

function edit(group: NodeGroup | null) {
  editing.value = group;
  editorOpen.value = true;
}

async function saved(group: NodeGroup) {
  showToast('节点分组已保存');
  await load();
  selectGroup(group.id);
}

function openPicker() {
  pickerGroup.value = selected.value || null;
  pickerError.value = '';
  pickerOpen.value = true;
}

async function mutate(action: () => Promise<unknown>) {
  if (busy.value || !isOwner.value) return;
  busy.value = true;
  try {
    await action();
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    busy.value = false;
  }
}

async function savePicker(ids: string[]) {
  const group = pickerGroup.value;
  if (!group || busy.value || !isOwner.value) return;
  busy.value = true;
  pickerError.value = '';

  try {
    const current = groupNodeMap[group.id] || [];
    const updated = Array.from(new Set([...current, ...ids]));
    groupNodeMap[group.id] = updated;
    localStorage.setItem(`qui_group_nodes_${group.id}`, JSON.stringify(updated));
    localStorage.setItem(`qui_group_inbounds_${group.id}`, JSON.stringify(updated));

    const associatedServers = Array.from(
      new Set(
        updated
          .map(id => allAvailableNodes.value.find(n => n.id === id)?.serverId)
          .filter((sId): sId is string => !!sId && servers.value.some(srv => srv.id === sId))
      )
    );
    await groupAPI.save(group.id, groupInput(group, associatedServers));

    pickerOpen.value = false;
    showToast(`已向分组添加 ${ids.length} 个代理节点`);
    await load();
  } catch (e) {
    pickerError.value = `${(e as Error).message}。部分操作可能已完成，请核对后重试。`;
    await load();
    pickerGroup.value = groups.value.find(item => item.id === group.id) || null;
  } finally {
    busy.value = false;
  }
}

function removeNode(nodeId: string) {
  const group = selected.value;
  if (!group) return;

  mutate(async () => {
    const current = groupNodeMap[group.id] || [];
    const updated = current.filter(id => id !== nodeId);
    groupNodeMap[group.id] = updated;
    localStorage.setItem(`qui_group_nodes_${group.id}`, JSON.stringify(updated));
    localStorage.setItem(`qui_group_inbounds_${group.id}`, JSON.stringify(updated));

    const associatedServers = Array.from(
      new Set(
        updated
          .map(id => allAvailableNodes.value.find(n => n.id === id)?.serverId)
          .filter((sId): sId is string => !!sId && servers.value.some(srv => srv.id === sId))
      )
    );
    await groupAPI.save(group.id, groupInput(group, associatedServers));
    showToast('节点已从分组中移出');
  });
}

const confirmOpen = ref(false);
const confirmTitle = ref('');
const confirmMessage = ref('');
let confirmedAction: () => Promise<unknown> = async () => {};

function askDelete() {
  const group = selected.value;
  if (!group) return;
  confirmTitle.value = '删除节点分组';
  confirmMessage.value = `确认删除“${group.name}”？相关策略引用可能受影响；代理节点本身会保留。`;
  confirmedAction = async () => {
    localStorage.removeItem(`qui_group_nodes_${group.id}`);
    localStorage.removeItem(`qui_group_inbounds_${group.id}`);
    await groupAPI.delete(group.id);
    showToast('节点分组已删除');
  };
  confirmOpen.value = true;
}

function runConfirmed() {
  mutate(confirmedAction);
}

onMounted(load);
</script>
