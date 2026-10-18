<template>
  <div class="space-y-6">
    <PageHeader title="代理用户" parent="用户" description="通过节点组分配接入权限，每台设备使用独立协议凭据。"><template #actions><button @click="openCreate" class="ios-btn-primary">新建用户</button></template></PageHeader>
    <form @submit.prevent="search" class="flex gap-3"><input v-model="keyword" placeholder="搜索用户名或显示名称" class="form-input-base" /><button class="ios-btn-secondary shrink-0">查询</button></form>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="loading && !users.length" /><EmptyState v-else-if="!users.length" title="暂无代理用户" />
    <DataTable v-else>
      <template #header><th>用户</th><th>状态</th><th>节点组</th><th>设备</th><th>连接</th><th>额度 / 已用</th><th>到期时间</th><th>操作</th></template>
      <template #body><tr v-for="user in users" :key="user.id"><td class="p-4"><router-link :to="`/users/${user.id}`" class="font-semibold text-blue-600">{{ user.displayName }}</router-link><p class="text-xs text-gray-500">{{ user.username }}</p></td><td><StatusBadge :status="effectiveStatus(user)" /></td><td class="text-xs">{{ user.nodeGroupIds.map(id => groups.find(group => group.id === id)?.name || id).join('、') || '未授权' }}</td><td>{{ user.devicesCount }} / {{ user.maxDevices }}</td><td class="text-xs"><template v-if="user.onlineConnections === null"><span class="text-gray-400">未知</span></template><template v-else>{{ user.onlineConnections }} / {{ user.maxConnections }}<span v-if="user.overLimit" class="ml-1 font-medium text-rose-600">超限</span></template></td><td><TrafficValue :bytes="user.usedTraffic" /><span class="text-xs ml-2">/ {{ user.trafficLimit ? (user.trafficLimit / 1073741824).toFixed(1) + ' GiB' : '不限额' }}</span><ResourceProgress v-if="user.trafficLimit" :percentage="quotaPercent(user)" :show-label="false" class="mt-2 max-w-[150px]" /><span v-if="user.trafficLimit && user.usedTraffic >= user.trafficLimit" class="text-xs text-rose-600">额度已用尽</span></td><td class="text-xs">{{ user.expiresAt ? formatTime(user.expiresAt) : '不过期' }}</td><td class="space-x-2"><button @click="openEdit(user)" class="ios-action-btn-primary">编辑</button><button @click="removeTarget = user; confirmVisible = true" class="ios-action-btn-danger">删除</button></td></tr></template>
      <template #footer><Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" /></template>
    </DataTable>
    <FormDrawer v-model="drawer" :title="editing ? '编辑用户' : '新建用户'" :loading="saving" @confirm="save" confirm-text="保存用户">
      <div class="space-y-4 text-sm">
        <label class="block">用户名<input v-model="form.username" :disabled="!!editing" maxlength="64" class="form-input-base mt-2" /></label>
        <label class="block">显示名称<input v-model="form.displayName" maxlength="80" class="form-input-base mt-2" /></label>
        <label class="block">状态<select v-model="form.status" class="form-input-base mt-2"><option value="active">启用</option><option value="disabled">禁用</option></select></label>
        <label class="block">到期时间（{{ authState.timezone }}）<input v-model="expiresLocal" type="datetime-local" class="form-input-base mt-2" /></label>
        <label class="block">流量额度（GiB，0 为不限额）<input v-model.number="trafficGiB" type="number" min="0" max="1000000" class="form-input-base mt-2" /></label>
        <div class="grid grid-cols-2 gap-3"><label>最大设备数<input v-model.number="form.maxDevices" type="number" min="1" max="100" class="form-input-base mt-2" /></label><label>最大连接数<input v-model.number="form.maxConnections" type="number" min="1" max="10000" class="form-input-base mt-2" /></label></div>
        <fieldset><legend class="mb-2">允许协议</legend><div class="flex flex-wrap gap-3"><label v-for="protocol in protocols" :key="protocol"><input v-model="form.allowedProtocols" type="checkbox" :value="protocol" /> {{ protocol }}</label></div></fieldset>
        <fieldset><legend class="mb-2">授权节点组</legend><label v-for="group in groups" :key="group.id" class="block mb-2"><input v-model="form.nodeGroupIds" type="checkbox" :value="group.id" /> {{ group.name }} · {{ group.purpose }}</label><p v-if="!groups.length" class="text-gray-500">请先创建节点组。</p></fieldset>
        <label class="block">备注<textarea v-model="form.remark" maxlength="300" class="form-input-base mt-2"></textarea></label>
        <p class="text-xs text-gray-500">保存业务设置后，请发布新的用户接入配置，让节点采用最新授权与凭据。</p>
      </div>
    </FormDrawer>
    <ConfirmDialog v-model="confirmVisible" title="删除代理用户" :message="`删除 ${removeTarget?.username || ''} 及其设备、凭据和订阅授权？节点配置需重新发布以撤销既有接入。`" @confirm="remove" />
  </div>
</template>
<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted } from 'vue';
import { userAPI, protocols, type ProxyUser, type UserInput } from '../api';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { authState, formatTime } from '../../../app/auth';
import { localInput, utcInput } from '../../../app/time';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue'; import DataTable from '../../../components/DataTable.vue'; import Pagination from '../../../components/Pagination.vue'; import StatusBadge from '../../../components/StatusBadge.vue'; import TrafficValue from '../../../components/TrafficValue.vue'; import ResourceProgress from '../../../components/ResourceProgress.vue'; import FormDrawer from '../../../components/FormDrawer.vue'; import ConfirmDialog from '../../../components/ConfirmDialog.vue'; import ErrorState from '../../../components/ErrorState.vue'; import EmptyState from '../../../components/EmptyState.vue'; import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const users = ref<ProxyUser[]>([]); const groups = ref<NodeGroup[]>([]); const total = ref(0); const page = ref(1); const keyword = ref(''); const loading = ref(false); const error = ref('');
const drawer = ref(false); const editing = ref<ProxyUser | null>(null); const saving = ref(false); const expiresLocal = ref(''); const trafficGiB = ref(0); const confirmVisible = ref(false); const removeTarget = ref<ProxyUser | null>(null);
function defaults(): UserInput { return { username: '', displayName: '', status: 'active', expiresAt: '', trafficLimit: 0, maxDevices: 3, maxConnections: 8, allowedProtocols: ['socks5'], nodeGroupIds: [], remark: '' }; }
const form = reactive<UserInput>(defaults());
function effectiveStatus(user: ProxyUser) { return user.status === 'active' && user.expiresAt && new Date(user.expiresAt).getTime() <= Date.now() ? 'expired' : user.status; }
function quotaPercent(user: ProxyUser) { return user.trafficLimit ? Math.min(100, (user.usedTraffic / user.trafficLimit) * 100) : 0; }
async function load() { loading.value = true; try { const [result, groupResult] = await Promise.all([userAPI.list(page.value, keyword.value), groupAPI.list(1, 100)]); users.value = result.items; total.value = result.total; groups.value = groupResult.items; error.value = ''; } catch (e) { error.value = (e as Error).message; } finally { loading.value = false; } }
function search() { page.value = 1; load(); } function changePage(value: number) { page.value = value; load(); }
function openCreate() { editing.value = null; Object.assign(form, defaults()); expiresLocal.value = ''; trafficGiB.value = 0; drawer.value = true; }
function openEdit(user: ProxyUser) { editing.value = user; Object.assign(form, { username: user.username, displayName: user.displayName, status: user.status, expiresAt: user.expiresAt, trafficLimit: user.trafficLimit, maxDevices: user.maxDevices, maxConnections: user.maxConnections, allowedProtocols: [...user.allowedProtocols], nodeGroupIds: [...user.nodeGroupIds], remark: user.remark }); expiresLocal.value = localInput(user.expiresAt); trafficGiB.value = user.trafficLimit / 1073741824; drawer.value = true; }
async function save() { if (saving.value) return; saving.value = true; try { const desiredLimit=Math.round(trafficGiB.value * 1073741824);const id=editing.value?.id; await userAPI.save(id||null,{...form,expiresAt:utcInput(expiresLocal.value),trafficLimit:desiredLimit});drawer.value=false;showToast('用户已保存');await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; } }
async function remove() { if (!removeTarget.value) return; try { await userAPI.delete(removeTarget.value.id); showToast('用户已删除'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } }
onMounted(() => { load(); window.addEventListener('proxy-system-alert', load); });
onUnmounted(() => window.removeEventListener('proxy-system-alert', load));
</script>

