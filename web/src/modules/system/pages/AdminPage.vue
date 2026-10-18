<template>
  <div class="space-y-6">
    <PageHeader title="管理员" parent="系统" description="Owner 管理平台；Viewer 仅查看总览、节点基础状态和流量。">
      <template #actions><button @click="openCreate" class="ios-btn-primary">创建 Viewer</button></template>
    </PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />
    <EmptyState v-else-if="!admins.length" title="暂无管理员" />
    <DataTable v-else>
      <template #header><th>用户名</th><th>显示名称</th><th>角色</th><th>状态</th><th>最后登录</th><th>来源 IP</th><th>操作</th></template>
      <template #body>
        <tr v-for="admin in admins" :key="admin.id">
          <td class="p-4">{{ admin.username }}</td><td>{{ admin.displayName || '—' }}</td><td>{{ admin.role }}</td><td><StatusBadge :status="admin.status" /></td><td>{{ formatTime(admin.lastLoginAt) }}</td><td>{{ admin.loginIp || '—' }}</td>
          <td class="space-x-2" v-if="admin.role === 'Viewer'">
            <button @click="openReset(admin)" class="ios-action-btn-secondary">重置密码</button>
            <button @click="confirmAction(admin, 'status')" class="ios-action-btn-secondary">{{ admin.status === 'active' ? '禁用' : '启用' }}</button>
            <button @click="confirmAction(admin, 'revoke')" class="ios-action-btn-secondary">撤销会话</button>
            <button @click="confirmAction(admin, 'delete')" class="ios-action-btn-danger">删除</button>
          </td><td v-else class="text-xs text-gray-500">Owner 受保护</td>
        </tr>
      </template>
      <template #footer><Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" /></template>
    </DataTable>
    <FormDrawer v-model="drawer" :title="resetTarget ? '重置 Viewer 密码' : '创建 Viewer'" :loading="saving" @confirm="save">
      <div class="space-y-4">
        <label v-if="!resetTarget" class="block text-sm">用户名<input v-model="form.username" maxlength="64" class="form-input-base mt-2" /></label>
        <label v-if="!resetTarget" class="block text-sm">显示名称<input v-model="form.displayName" maxlength="40" class="form-input-base mt-2" /></label>
        <label class="block text-sm">密码（12～128 个字符）<input v-model="form.password" type="password" autocomplete="new-password" maxlength="128" class="form-input-base mt-2" /></label>
        <p class="text-sm text-gray-500">重置密码后，该账号的现有会话立即失效。</p>
      </div>
    </FormDrawer>
    <ConfirmDialog v-model="confirmVisible" title="确认管理员操作" :message="confirmationMessage" @confirm="execute" />
  </div>
</template>
<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import type { AdminUser } from '../../../types';
import { request, type Page } from '../../../app/apiClient';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
const admins = ref<AdminUser[]>([]); const page = ref(1); const total = ref(0);
const loading = ref(false); const saving = ref(false); const error = ref('');
const drawer = ref(false); const resetTarget = ref<AdminUser | null>(null);
const form = reactive({ username: '', displayName: '', password: '' });
const confirmVisible = ref(false); const action = ref(''); const target = ref<AdminUser | null>(null);
const confirmationMessage = computed(() => `即将对 ${target.value?.username || ''} 执行 ${({status:'切换启用状态',revoke:'撤销全部会话',delete:'删除账号'} as Record<string,string>)[action.value]}。`);
async function load() {
  loading.value = true; error.value = '';
  try { const result = await request<Page<AdminUser>>(`/admins?page=${page.value}&pageSize=20`); admins.value = result.items; total.value = result.total; }
  catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function changePage(value: number) { page.value = value; load(); }
function openCreate() { resetTarget.value = null; Object.assign(form, { username: '', displayName: '', password: '' }); drawer.value = true; }
function openReset(admin: AdminUser) { resetTarget.value = admin; form.password = ''; drawer.value = true; }
async function save() {
  if (saving.value) return;
  saving.value = true;
  try {
    if (resetTarget.value) await request(`/admins/${resetTarget.value.id}/password`, 'POST', { password: form.password });
    else await request('/admins', 'POST', { ...form, role: 'Viewer' });
    drawer.value = false; form.password = ''; showToast('管理员信息已保存'); await load();
  } catch (e) { showToast((e as Error).message, 'error'); }
  finally { saving.value = false; }
}
function confirmAction(admin: AdminUser, value: string) { target.value = admin; action.value = value; confirmVisible.value = true; }
async function execute() {
  const admin = target.value; if (!admin) return;
  try {
    if (action.value === 'status') await request(`/admins/${admin.id}`, 'PUT', { displayName: admin.displayName || '', status: admin.status === 'active' ? 'disabled' : 'active' });
    else if (action.value === 'revoke') await request(`/admins/${admin.id}/revoke-sessions`, 'POST');
    else await request(`/admins/${admin.id}`, 'DELETE');
    showToast('操作完成'); await load();
  } catch (e) { showToast((e as Error).message, 'error'); }
}
onMounted(load);
</script>

