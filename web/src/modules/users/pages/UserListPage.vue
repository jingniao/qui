<template>
  <div class="space-y-6">
    <PageHeader
      title="用户管理"
      parent="用户管理"
      description="统一管理平台用户体系。包含系统管理员（Owner / Viewer）与代理用户（账号、订阅与限额）。"
    >
      <template #actions>
        <button
          v-if="isOwner"
          type="button"
          @click="openCreateAdmin"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M18 9v3m0 0v3m0-3h3m-3 0h-3m-2-5a4 4 0 11-8 0 4 4 0 018 0zM3 20a6 6 0 0112 0v1H3v-1z" />
          </svg>
          创建管理员
        </button>
        <button
          type="button"
          @click="openCreate"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
          </svg>
          新增用户
        </button>
      </template>
    </PageHeader>

    <!-- Search & Filter Bar -->
    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索用户名、备注或显示名称…"
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
    </form>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !users.length" />
    <EmptyState v-else-if="!users.length && !displayAdmins.length" title="暂无用户" description="点击上方“新增用户”为内部成员开通专属代理账号与订阅。" />

    <!-- Unified Table: Admins + Proxy Users -->
    <DataTable v-else>
      <template #header>
        <th>用户名</th>
        <th>状态</th>
        <th>设备</th>
        <th>连接</th>
        <th>额度 / 已用</th>
        <th>到期时间</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <!-- 1. Platform Admins (Owner / Viewer) -->
        <tr v-for="admin in displayAdmins" :key="'admin-' + admin.id" class="bg-purple-50/20 hover:bg-purple-50/40 transition-colors border-l-2 border-l-purple-500">
          <td class="p-4">
            <div class="flex items-center gap-2.5">
              <div
                class="w-8 h-8 rounded-xl flex items-center justify-center shrink-0 font-bold text-xs"
                :class="admin.role === 'Owner'
                  ? 'bg-purple-100 text-purple-700 border border-purple-200'
                  : 'bg-blue-100 text-ios-blue border border-blue-200'"
              >
                {{ (admin.displayName || admin.username).slice(0, 1).toUpperCase() }}
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <span class="font-semibold text-ios-label text-[14px] truncate">{{ admin.displayName || admin.username }}</span>
                  <span
                    class="px-1.5 py-0.2 rounded text-[10px] font-bold tracking-wide uppercase shrink-0"
                    :class="admin.role === 'Owner' ? 'bg-purple-100 text-purple-700' : 'bg-blue-100 text-ios-blue'"
                  >
                    {{ admin.role === 'Owner' ? '系统管理员' : '审计管理员' }}
                  </span>
                </div>
                <p class="text-xs text-ios-tertiary font-mono">@{{ admin.username }}</p>
              </div>
            </div>
          </td>
          <td><StatusBadge :status="admin.status" /></td>
          <td class="text-xs font-mono text-ios-tertiary tabular-nums">—</td>
          <td class="text-xs font-mono text-ios-tertiary tabular-nums">—</td>
          <td class="text-xs text-ios-tertiary font-mono">— (管理账号)</td>
          <td class="text-xs font-mono text-ios-tertiary tabular-nums">永久有效</td>
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <template v-if="admin.role === 'Viewer' && isOwner">
              <button @click="openResetAdmin(admin)" class="ios-action-btn-secondary">重置密码</button>
              <button @click="confirmAdminAction(admin, 'status')" class="ios-action-btn-secondary">
                {{ admin.status === 'active' ? '禁用' : '启用' }}
              </button>
              <button @click="confirmAdminAction(admin, 'revoke')" class="ios-action-btn-secondary">撤销会话</button>
              <button @click="confirmAdminAction(admin, 'delete')" class="ios-action-btn-danger">删除</button>
            </template>
            <span v-else class="text-xs text-ios-tertiary">Owner 受保护</span>
          </td>
        </tr>

        <!-- 2. Proxy Users -->
        <tr v-for="user in users" :key="'user-' + user.id" class="hover:bg-black/[0.015] transition-colors">
          <td>
            <router-link :to="`/users/${user.id}`" class="font-semibold text-ios-blue hover:underline text-[15px]">
              {{ user.displayName || user.username }}
            </router-link>
            <p class="text-[13px] text-ios-secondary font-normal mt-0.5 font-mono">@{{ user.username }}</p>
          </td>
          <td><StatusBadge :status="effectiveStatus(user)" /></td>
          <td class="text-xs font-mono tabular-nums text-ios-tertiary">
            <button class="text-ios-blue" @click="showDevices(user)">{{ user.onlineDevices ?? '未知' }} / {{ user.deviceLimit || '不限' }}</button>
            <p v-if="!user.deviceLimitEnforced" class="text-amber-700" :title="user.deviceLimitUnsupportedNodes?.join('、')">部分执行 · {{ user.deviceLimitUnsupportedNodes?.join('、') }}</p>
          </td>
          <td class="text-xs">
            <template v-if="user.onlineConnections === null">
              <span class="text-ios-tertiary">未知</span>
            </template>
            <template v-else>
              <span class="font-mono tabular-nums text-ios-secondary">{{ user.onlineConnections }}</span>
            </template>
          </td>
          <td>
            <div class="flex items-center">
              <TrafficValue :bytes="user.usedTraffic" />
              <span class="text-xs text-ios-tertiary ml-1.5 font-mono tabular-nums">/ {{ user.trafficLimit ? (user.trafficLimit / 1073741824).toFixed(1) + ' GiB' : '不限额' }}</span>
            </div>
            <ResourceProgress v-if="user.trafficLimit" :percentage="quotaPercent(user)" :show-label="false" class="mt-1.5 max-w-[140px]" />
            <span v-if="user.trafficLimit && user.usedTraffic >= user.trafficLimit" class="text-xs text-ios-red font-medium block mt-0.5">额度已用尽</span>
          </td>
          <td class="text-xs font-mono tabular-nums text-ios-tertiary">{{ user.expiresAt ? formatTime(user.expiresAt) : '不过期' }}</td>
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <button @click="openEdit(user)" class="ios-action-btn-primary">编辑</button>
            <button @click="removeTarget = user; confirmVisible = true" class="ios-action-btn-danger">删除</button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <FormDrawer v-model="devicesDrawer" title="在线设备估算" confirm-text="关闭" @confirm="devicesDrawer = false">
      <div v-if="deviceView" class="space-y-3 text-sm">
        <p>{{ deviceView.onlineDevices ?? '未知' }} / {{ deviceView.deviceLimit || '不限' }}</p>
        <p v-if="!deviceView.deviceLimitEnforced" class="text-amber-700">部分执行：{{ deviceView.deviceLimitUnsupportedNodes.join('、') }}</p>
        <div v-for="(item, index) in deviceView.items" :key="index" class="border-b py-2 font-mono text-xs">{{ item.address }} · {{ item.allowed ? '允许' : '受限' }} · {{ item.active ? '活跃' : '等待释放' }}</div>
        <button :disabled="devicesBusy" class="ios-btn-secondary" @click="resetDeviceOrder">重置准入顺序</button>
        <details><summary>限制与隐私</summary><p v-for="help in deviceLimitHelp" :key="help" class="text-xs mt-2">{{ help }}</p></details>
      </div>
    </FormDrawer>
    <!-- Drawer 1: 新建 / 编辑代理用户 -->
    <FormDrawer v-model="drawer" width="xl" :title="editing ? '编辑用户' : '新建用户'" :loading="saving" @confirm="save" confirm-text="保存用户">
      <div class="space-y-4 text-sm">
        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            用户名
            <input v-model="form.username" :disabled="!!editing" maxlength="64" class="form-input-base mt-1.5" placeholder="用于身份凭据登录" />
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            显示名称
            <input v-model="form.displayName" maxlength="80" class="form-input-base mt-1.5" placeholder="用户别名 / 备注名" />
          </label>
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            状态
            <select v-model="form.status" class="form-input-base mt-1.5">
              <option value="active">启用</option>
              <option value="disabled">禁用</option>
            </select>
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            到期时间（{{ authState.timezone }}）
            <input v-model="expiresLocal" type="datetime-local" class="form-input-base mt-1.5" />
          </label>
        </div>

        <div class="grid sm:grid-cols-3 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            流量额度（GiB，0 为不限）
            <input v-model.number="trafficGiB" type="number" min="0" max="1000000" class="form-input-base mt-1.5" />
          </label>
        </div>

        <label class="block text-xs font-medium text-ios-secondary">同时在线设备上限（0 为不限）
          <input v-model.number="form.deviceLimit" type="number" min="0" max="100" step="1" class="form-input-base mt-1.5" />
        </label>
        <details class="text-xs text-ios-secondary"><summary>同时在线设备上限的限制与隐私</summary><p v-for="help in deviceLimitHelp" :key="help" class="mt-2">{{ help }}</p></details>
        <!-- 允许协议 -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label class="text-xs font-medium text-ios-secondary">允许协议</label>
            <span class="text-[11px] text-ios-tertiary">已选 {{ form.allowedProtocols.length }} / {{ protocols.length }}</span>
          </div>
          <div class="rounded-2xl p-3 bg-[#F5F5F7] border border-black/[0.06] flex flex-wrap gap-2">
            <label
              v-for="protocol in protocols"
              :key="protocol"
              :class="[
                'inline-flex items-center gap-1.5 px-3 py-1.5 rounded-xl text-xs font-medium transition-all cursor-pointer select-none active:scale-95',
                form.allowedProtocols.includes(protocol)
                  ? 'bg-ios-blue text-white shadow-[0_2px_6px_rgba(0,113,227,0.25)] ring-1 ring-ios-blue'
                  : 'bg-white text-ios-secondary border border-black/[0.08] hover:text-ios-label hover:border-black/[0.15] hover:bg-black/[0.02]'
              ]"
            >
              <input
                type="checkbox"
                v-model="form.allowedProtocols"
                :value="protocol"
                class="sr-only"
              />
              <svg
                v-if="form.allowedProtocols.includes(protocol)"
                class="w-3.5 h-3.5 shrink-0"
                viewBox="0 0 20 20"
                fill="currentColor"
              >
                <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
              </svg>
              <span>{{ protocol }}</span>
            </label>
          </div>
        </div>

        <label class="block text-xs font-medium text-ios-secondary">
          备注
          <textarea v-model="form.remark" maxlength="300" rows="2" class="form-input-base mt-1.5" placeholder="可选填写用户相关备忘信息…"></textarea>
        </label>
      </div>
    </FormDrawer>

    <!-- Drawer 2: 创建 / 重置管理员 (Viewer) -->
    <FormDrawer
      v-model="adminDrawer"
      :title="resetAdminTarget ? '重置管理员密码' : '创建平台管理员 (Viewer)'"
      :description="resetAdminTarget ? '重置密码后，该管理员现有会话将立即失效。' : 'Viewer 仅具备查看系统总览、服务器状态与审计记录权限。'"
      :loading="adminSaving"
      @confirm="saveAdmin"
    >
      <div class="space-y-4 text-sm">
        <label v-if="!resetAdminTarget" class="block">
          <span class="text-xs font-medium text-ios-secondary">管理员用户名</span>
          <input v-model="adminForm.username" maxlength="64" placeholder="例如 ops_viewer" class="form-input-base mt-1.5" />
        </label>
        <label v-if="!resetAdminTarget" class="block">
          <span class="text-xs font-medium text-ios-secondary">显示名称</span>
          <input v-model="adminForm.displayName" maxlength="40" placeholder="例如 运维只读" class="form-input-base mt-1.5" />
        </label>
        <label class="block">
          <span class="text-xs font-medium text-ios-secondary">密码（12～128 个字符）</span>
          <input v-model="adminForm.password" type="password" autocomplete="new-password" maxlength="128" placeholder="填写安全登录密码" class="form-input-base mt-1.5" />
        </label>
      </div>
    </FormDrawer>

    <!-- Confirm Dialog 1: 删除代理用户 -->
    <ConfirmDialog
      v-model="confirmVisible"
      title="删除代理用户"
      :message="`删除 ${removeTarget?.username || ''} 及其设备、凭据和订阅授权？已发布到节点的凭据会在约一分钟内自动撤销，无需重新发布。`"
      @confirm="remove"
    />

    <!-- Confirm Dialog 2: 管理员操作 (禁用/撤销/删除) -->
    <ConfirmDialog
      v-model="adminConfirmVisible"
      title="确认管理员操作"
      :message="adminConfirmationMessage"
      @confirm="executeAdminAction"
    />
  </div>
</template>

<script setup lang="ts">
import { deviceLimitHelp } from "../../../app/deviceLimit";
import type { DevicesOnline } from "../api";
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue';
import { userAPI, protocols, type ProxyUser, type UserInput } from '../api';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import type { AdminUser } from '../../../types';
import { request, type Page } from '../../../app/apiClient';
import { authState, isOwner, formatTime } from '../../../app/auth';
import { localInput, utcInput } from '../../../app/time';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import ResourceProgress from '../../../components/ResourceProgress.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const users = ref<ProxyUser[]>([]);
const admins = ref<AdminUser[]>([]);
const groups = ref<NodeGroup[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const loading = ref(false);
const error = ref('');

// Filtered Admins by keyword
const displayAdmins = computed(() => {
  if (!keyword.value.trim()) return admins.value;
  const q = keyword.value.trim().toLowerCase();
  return admins.value.filter(a =>
    a.username.toLowerCase().includes(q) || (a.displayName || '').toLowerCase().includes(q)
  );
});

// Proxy User Form State
const drawer = ref(false);
const editing = ref<ProxyUser | null>(null);
const saving = ref(false);
const expiresLocal = ref('');
const trafficGiB = ref(0);
const confirmVisible = ref(false);
const removeTarget = ref<ProxyUser | null>(null);

const devicesDrawer = ref(false), devicesBusy = ref(false), deviceUserID = ref('');
const deviceView = ref<DevicesOnline>();
async function showDevices(user: ProxyUser) {
  try { deviceUserID.value = user.id; deviceView.value = await userAPI.devicesOnline(user.id); devicesDrawer.value = true; }
  catch (e) { error.value = (e as Error).message; }
}
async function resetDeviceOrder() {
  devicesBusy.value = true;
  try { deviceView.value = await userAPI.resetDevicesOnline(deviceUserID.value); await load(); }
  catch (e) { error.value = (e as Error).message; }
  finally { devicesBusy.value = false; }
}
function defaults(): UserInput {
  return {
    trafficResetCycle: 'none',
    trafficResetAnchor: '',
    username: '',
    displayName: '',
    status: 'active',
    expiresAt: '',
    trafficLimit: 0,
    deviceLimit: 0,
    allowedProtocols: ['socks5', 'vless', 'trojan'],
    nodeGroupIds: [],
    remark: ''
  };
}

const form = reactive<UserInput>(defaults());

function effectiveStatus(user: ProxyUser) {
  return user.status === 'active' && user.expiresAt && new Date(user.expiresAt).getTime() <= Date.now() ? 'expired' : user.status;
}

function quotaPercent(user: ProxyUser) {
  return user.trafficLimit ? Math.min(100, (user.usedTraffic / user.trafficLimit) * 100) : 0;
}

// Admin Form & Actions State
const adminDrawer = ref(false);
const adminSaving = ref(false);
const resetAdminTarget = ref<AdminUser | null>(null);
const adminForm = reactive({ username: '', displayName: '', password: '' });
const adminConfirmVisible = ref(false);
const adminAction = ref('');
const adminTarget = ref<AdminUser | null>(null);

const adminConfirmationMessage = computed(() => {
  const map: Record<string, string> = {
    status: '切换启用状态',
    revoke: '撤销全部会话',
    delete: '删除管理员账号'
  };
  return `即将对管理员 ${adminTarget.value?.username || ''} 执行 ${map[adminAction.value] || '此操作'}。`;
});

function openCreateAdmin() {
  resetAdminTarget.value = null;
  Object.assign(adminForm, { username: '', displayName: '', password: '' });
  adminDrawer.value = true;
}

function openResetAdmin(admin: AdminUser) {
  resetAdminTarget.value = admin;
  adminForm.password = '';
  adminDrawer.value = true;
}

async function saveAdmin() {
  if (adminSaving.value) return;
  adminSaving.value = true;
  try {
    if (resetAdminTarget.value) {
      await request(`/admins/${resetAdminTarget.value.id}/password`, 'POST', { password: adminForm.password });
    } else {
      await request('/admins', 'POST', { ...adminForm, role: 'Viewer' });
    }
    adminDrawer.value = false;
    adminForm.password = '';
    showToast('管理员信息已保存');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    adminSaving.value = false;
  }
}

function confirmAdminAction(admin: AdminUser, value: string) {
  adminTarget.value = admin;
  adminAction.value = value;
  adminConfirmVisible.value = true;
}

async function executeAdminAction() {
  const admin = adminTarget.value;
  if (!admin) return;
  try {
    if (adminAction.value === 'status') {
      await request(`/admins/${admin.id}`, 'PUT', {
        displayName: admin.displayName || '',
        status: admin.status === 'active' ? 'disabled' : 'active'
      });
    } else if (adminAction.value === 'revoke') {
      await request(`/admins/${admin.id}/revoke-sessions`, 'POST');
    } else {
      await request(`/admins/${admin.id}`, 'DELETE');
    }
    showToast('操作完成');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

async function load() {
  loading.value = true;
  try {
    const [result, groupResult, adminResult] = await Promise.all([
      userAPI.list(page.value, keyword.value),
      groupAPI.list(1, 100).catch(() => ({ items: [], total: 0 })),
      request<Page<AdminUser>>('/admins?page=1&pageSize=50').catch(() => ({ items: [], total: 0 }))
    ]);
    users.value = result.items;
    total.value = result.total;
    groups.value = groupResult.items;
    admins.value = adminResult.items || [];
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

function openCreate() {
  editing.value = null;
  Object.assign(form, defaults());
  expiresLocal.value = '';
  trafficGiB.value = 0;
  drawer.value = true;
}

function openEdit(user: ProxyUser) {
  editing.value = user;
  Object.assign(form, {
    username: user.username,
    trafficResetCycle: user.trafficResetCycle,
    trafficResetAnchor: user.trafficResetAnchor,
    displayName: user.displayName,
    status: user.status,
    expiresAt: user.expiresAt,
    trafficLimit: user.trafficLimit,
    deviceLimit: user.deviceLimit,
    allowedProtocols: [...user.allowedProtocols],
    nodeGroupIds: [...user.nodeGroupIds],
    remark: user.remark
  });
  expiresLocal.value = localInput(user.expiresAt);
  trafficGiB.value = user.trafficLimit / 1073741824;
  drawer.value = true;
}

async function save() {
  if (saving.value) return;
  saving.value = true;
  try {
    const desiredLimit = Math.round(trafficGiB.value * 1073741824);
    const id = editing.value?.id;
    await userAPI.save(id || null, {
      ...form,
      expiresAt: utcInput(expiresLocal.value),
      trafficLimit: desiredLimit
    });
    drawer.value = false;
    showToast('用户已保存');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

async function remove() {
  if (!removeTarget.value) return;
  try {
    await userAPI.delete(removeTarget.value.id);
    showToast('用户已删除');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

onMounted(() => {
  load();
  window.addEventListener('proxy-system-alert', load);
});

onUnmounted(() => window.removeEventListener('proxy-system-alert', load));
</script>
