<template>
  <div class="space-y-6">
    <PageHeader title="线路组" parent="节点配置" description="线路组是一组服务器；授权用户线路组后，用户可使用这些服务器上的订阅节点。">
      <template #actions>
        <button class="ios-btn-secondary" :disabled="loading || busy" @click="load">刷新</button>
        <button v-if="isOwner" class="ios-btn-primary" :disabled="loading || !!error || busy" @click="edit(null)">新建线路组</button>
      </template>
    </PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <p v-else-if="loading" role="status" class="text-sm text-ios-tertiary">正在加载线路组…</p>
    <EmptyState v-else-if="!groups.length" title="暂无线路组" description="将服务器加入线路组，再将用户授权到该组，即可统一管理订阅入口。" />
    <div v-else class="grid grid-cols-1 lg:grid-cols-4 gap-6 items-start">
      <aside class="panel-card p-4 space-y-4 lg:col-span-1" aria-label="线路组列表">
        <input v-model="keyword" class="form-input-base" placeholder="搜索线路组" aria-label="搜索线路组" />
        <section v-for="section in sections" :key="section.title" class="space-y-2">
          <h2 class="text-xs font-medium text-ios-tertiary">{{ section.title }}</h2>
          <button v-for="group in section.items" :key="group.id" type="button" :aria-current="selected?.id === group.id ? 'true' : undefined"
            class="w-full rounded-xl p-3 text-left transition-all active:scale-95"
            :class="[selected?.id === group.id ? 'bg-ios-blue/10 ring-1 ring-ios-blue/20' : 'hover:bg-ios-bg', { 'opacity-50': !group.enabled }]"
            @click="select(group.id)">
            <span class="block text-sm font-semibold text-ios-label break-words">{{ group.name }}</span>
            <span class="text-xs text-ios-secondary font-mono tabular-nums">{{ group.nodeIds.length }} 台<span v-if="isUserGroup(group)"> · {{ granted(group.id).length }} 人</span></span>
          </button>
        </section>
        <p v-if="!sections.length" class="text-sm text-ios-tertiary">没有符合条件的线路组</p>
      </aside>
      <div v-if="selected" class="lg:col-span-3 space-y-6 min-w-0">
        <section class="panel-card p-6 space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-[17px] sm:text-lg font-semibold text-ios-label break-words">{{ selected.name }}</h2>
            <div v-if="isOwner" class="flex gap-2">
              <button class="ios-btn-secondary" :disabled="busy" @click="edit(selected)">编辑</button>
              <button class="ios-btn-danger" :disabled="busy" @click="askDelete">删除</button>
            </div>
          </div>
          <div class="flex flex-wrap items-center gap-2 text-xs text-ios-secondary">
            <span>{{ isUserGroup(selected) ? '用户线路组' : '内部分组' }} · {{ groupPurposeNames[selected.purpose] }}</span>
            <StatusBadge :status="selected.enabled ? 'active' : 'disabled'" />
            <span v-if="isUserGroup(selected)">{{ selected.subscriptionVisible ? '订阅可见' : '订阅隐藏' }}</span>
          </div>
          <p v-if="selected.remark" class="text-sm text-ios-secondary whitespace-pre-wrap break-words">{{ selected.remark }}</p>
        </section>
        <section class="space-y-3">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <h2 class="text-[17px] font-semibold text-ios-label">服务器 <span class="font-mono tabular-nums">（{{ members.length }}）</span></h2>
            <button v-if="isOwner" class="ios-btn-secondary" :disabled="busy" @click="openPicker('servers')">+ 添加服务器</button>
          </div>
          <DataTable v-if="members.length">
            <template #header><th>名称</th><th>状态</th><th>代理核心</th><th>订阅节点</th><th v-if="isOwner">操作</th></template>
            <template #body>
              <tr v-for="server in members" :key="server.id">
                <td><router-link :to="`/nodes/${server.id}`" class="text-[15px] font-semibold text-ios-blue hover:underline">{{ server.name }}</router-link></td>
                <td><StatusBadge :status="server.status" /></td>
                <td class="text-sm text-ios-label">{{ server.capabilities?.core_name || '未知' }}</td>
                <td class="text-xs font-mono tabular-nums text-ios-secondary">{{ userInbounds(server.id).length }}</td>
                <td v-if="isOwner"><button class="ios-action-btn-danger" :disabled="busy" @click="removeServer(server)">移出</button></td>
              </tr>
            </template>
          </DataTable>
          <EmptyState v-else title="暂无服务器" description="添加服务器后，可在此查看它提供的订阅节点数量。" />
          <p v-if="missingMembers" class="text-xs text-ios-tertiary">有 {{ missingMembers }} 台成员服务器已不可读取，列表与预览未计入。</p>
        </section>
        <template v-if="isUserGroup(selected)">
          <section class="panel-card p-6 space-y-4">
            <div class="flex flex-wrap items-center justify-between gap-3">
              <h2 class="text-[17px] font-semibold text-ios-label">授权用户 <span class="font-mono tabular-nums">（{{ grantedUsers.length }}）</span></h2>
              <button v-if="isOwner" class="ios-btn-secondary" :disabled="busy" @click="openPicker('users')">+ 授权用户</button>
            </div>
            <div v-if="grantedUsers.length" class="flex flex-wrap gap-2">
              <span v-for="user in grantedUsers" :key="user.id" class="inline-flex items-center gap-2 rounded-full bg-ios-blue/10 px-3 py-1 text-sm text-ios-blue">
                <router-link :to="`/users/${user.id}`" class="hover:underline">{{ user.displayName || user.username }}</router-link>
                <button v-if="isOwner" :aria-label="`撤销 ${user.username} 的授权`" :disabled="busy" class="rounded-full px-1 active:scale-95" @click="askRevoke(user)">×</button>
              </span>
            </div>
            <p v-else class="text-sm text-ios-tertiary">暂无授权用户</p>
          </section>
          <section class="panel-card p-6 space-y-3">
            <h2 class="text-[17px] font-semibold text-ios-label">效果预览</h2>
            <p class="text-sm text-ios-label">这 <span class="font-mono tabular-nums">{{ grantedUsers.length }}</span> 位用户的订阅候选范围包含 <span class="font-mono tabular-nums">{{ candidateCount }}</span> 个节点。</p>
            <p v-if="!selected.enabled || !selected.subscriptionVisible" class="text-xs text-ios-orange">当前线路组已停用或订阅隐藏，不通过该组提供订阅节点。</p>
            <p v-for="notice in serverNotices" :key="notice" class="text-xs text-ios-secondary">{{ notice }}</p>
            <p class="text-xs text-ios-tertiary">按当前配置的已启用、订阅可见且非内部中转入站估算；实际内容以已发布配置、用户状态、允许协议和入站用户范围为准。</p>
          </section>
        </template>
        <section v-else class="panel-card p-6 space-y-3">
          <h2 class="text-[17px] font-semibold text-ios-label">被引用</h2>
          <ul v-if="references.length" class="space-y-2 text-sm">
            <li v-for="reference in references" :key="reference.key"><router-link :to="reference.path" class="text-ios-blue hover:underline">{{ reference.label }}</router-link></li>
          </ul>
          <p v-else class="text-sm text-ios-tertiary">暂无引用</p>
        </section>
      </div>
    </div>
    <LineGroupEditor v-model="editorOpen" :group="editing" @saved="saved" />
    <LineGroupPicker v-if="pickerGroup" v-model="pickerOpen" :kind="pickerKind" :group="pickerGroup" :groups="groups" :servers="servers" :users="users" :saving="busy" :error="pickerError" @save="savePicker" />
    <ConfirmDialog v-model="confirmOpen" :title="confirmTitle" :message="confirmMessage" @confirm="runConfirmed" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
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
import LineGroupPicker from '../components/LineGroupPicker.vue';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import type { NodeView } from '../../nodes/api';
import { userAPI, type ProxyUser } from '../../users/api';
import { choices, proxyAPI, type Inbound } from '../api';
import { policyAPI, type PolicyRecord, type StoredRuleTemplate } from '../../routing/api';
import { groupInput, groupPurposeNames, isUserGroup } from '../lineGroups';

const route = useRoute();
const router = useRouter();
const groups = ref<NodeGroup[]>([]);
const servers = ref<NodeView[]>([]);
const users = ref<ProxyUser[]>([]);
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
const pickerKind = ref<'servers' | 'users'>('servers');
const pickerGroup = ref<NodeGroup | null>(null);
const pickerError = ref('');
const selected = computed(() => groups.value.find(group => group.id === route.query.group) || groups.value[0]);
const granted = (id: string) => users.value.filter(user => user.nodeGroupIds.includes(id));
const grantedUsers = computed(() => selected.value ? granted(selected.value.id) : []);
const members = computed(() => servers.value.filter(server => selected.value?.nodeIds.includes(server.id)));
const missingMembers = computed(() => (selected.value?.nodeIds.length || 0) - members.value.length);
const userInbounds = (serverId: string) => inbounds.value.filter(inbound => inbound.nodeId === serverId && inbound.enabled && inbound.subscriptionVisible && !inbound.internalRelay);
const sections = computed(() => {
  const visible = groups.value.filter(group => group.name.toLowerCase().includes(keyword.value.trim().toLowerCase()));
  return [{ title: '用户线路组', items: visible.filter(isUserGroup) }, { title: '内部分组', items: visible.filter(group => !isUserGroup(group)) }].filter(section => section.items.length);
});
const candidateCount = computed(() => {
  if (!selected.value?.enabled || !selected.value.subscriptionVisible) return 0;
  return members.value.filter(server => server.enabled && server.allowSubscription && !server.maintenance).reduce((sum, server) => sum + userInbounds(server.id).length, 0);
});
const serverNotices = computed(() => members.value.flatMap(server => {
  if (!server.enabled) return [`${server.name} 已停用，不计入候选范围。`];
  if (server.maintenance || !server.allowSubscription) return [`${server.name} 维护中或未允许订阅，不计入候选范围。`];
  if (server.status === 'offline') return [`${server.name} 离线，连接可能不可用；离线不代表已发布订阅自动移除。`];
  return [];
}));
const references = computed(() => {
  const id = selected.value?.id;
  if (!id) return [];
  return [
    ...policies.value.filter(policy => policy.scope.group_ids?.includes(id)).map(policy => ({ key: `policy:${policy.id}`, label: `节点策略：${policy.name}${policy.enabled ? '' : '（已停用）'}`, path: `/routing/rules/${policy.id}/edit` })),
    ...(clientTemplate.value?.template.groups || []).filter(group => group.members.includes(`@nodeGroup:${id}`)).map(group => ({ key: `client:${group.name}`, label: `客户端策略：${group.name}`, path: '/routing/client' })),
    ...granted(id).map(user => ({ key: `user:${user.id}`, label: `历史用户授权：${user.displayName || user.username}`, path: `/users/${user.id}` }))
  ];
});
async function load() {
  loading.value = true;
  error.value = '';
  try {
    const data = await Promise.all([
      choices(groupAPI.list), choices((page, size) => request<Page<NodeView>>(`/nodes?page=${page}&pageSize=${size}`)),
      choices((page, size) => userAPI.list(page, '', size)), choices((page, size) => proxyAPI.inbounds('', page, size)),
      choices((page, size) => policyAPI.list(page, '', size)), request<StoredRuleTemplate>('/subscription-rules')
    ]);
    [groups.value, servers.value, users.value, inbounds.value, policies.value, clientTemplate.value] = data;
    syncSelection();
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
// The layout keeps a leaving page mounted during its out-in transition while
// route already describes the next page; ignore routes that are not ours.
const ours = () => route.path === '/proxy/groups';
function select(id: string) { if (!ours()) return; router.replace({ query: { ...route.query, group: id } }); }
function syncSelection() {
  if (!ours()) return;
  if (selected.value && selected.value.id !== route.query.group) select(selected.value.id);
  else if (!groups.value.length && route.query.group) {
    const query = { ...route.query }; delete query.group; router.replace({ query });
  }
}
watch(() => route.query.group, syncSelection);
function edit(group: NodeGroup | null) { editing.value = group; editorOpen.value = true; }
async function saved(group: NodeGroup) { showToast('线路组已保存'); await load(); select(group.id); }
function openPicker(kind: 'servers' | 'users') {
  pickerKind.value = kind; pickerGroup.value = selected.value || null; pickerError.value = ''; pickerOpen.value = true;
}
async function mutate(action: () => Promise<unknown>) {
  if (busy.value || !isOwner.value) return;
  busy.value = true;
  try { await action(); showToast('线路组已更新'); await load(); }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally { busy.value = false; }
}
async function savePicker(ids: string[]) {
  const group = pickerGroup.value;
  if (!group || busy.value || !isOwner.value) return;
  busy.value = true;
  pickerError.value = '';
  try {
    if (pickerKind.value === 'servers') await groupAPI.save(group.id, groupInput(group, [...new Set([...group.nodeIds, ...ids])]));
    else for (const id of ids) await userAPI.grantGroup(id, group.id);
    pickerOpen.value = false; showToast('线路组已更新'); await load();
  } catch (e) {
    pickerError.value = `${(e as Error).message}。部分操作可能已完成，请核对后重试。`;
    await load();
    pickerGroup.value = groups.value.find(item => item.id === group.id) || null;
  } finally { busy.value = false; }
}
function removeServer(server: NodeView) {
  const group = selected.value;
  if (group) mutate(() => groupAPI.save(group.id, groupInput(group, group.nodeIds.filter(id => id !== server.id))));
}
const confirmOpen = ref(false);
const confirmTitle = ref('');
const confirmMessage = ref('');
let confirmedAction: () => Promise<unknown> = async () => {};
function askDelete() {
  const group = selected.value;
  if (!group) return;
  confirmTitle.value = '删除线路组';
  confirmMessage.value = `确认删除“${group.name}”？${isUserGroup(group) ? '已授权用户将失去通过该组获得的访问权限；' : '相关引用可能受影响；'}服务器本身会保留。`;
  confirmedAction = () => groupAPI.delete(group.id); confirmOpen.value = true;
}
function askRevoke(user: ProxyUser) {
  const group = selected.value;
  if (!group) return;
  confirmTitle.value = '撤销用户授权';
  confirmMessage.value = `确认撤销 ${user.username} 对“${group.name}”的授权？用户将失去通过该组获得的访问权限。`;
  confirmedAction = () => userAPI.revokeGroup(user.id, group.id); confirmOpen.value = true;
}
function runConfirmed() { mutate(confirmedAction); }
onMounted(load);
</script>
