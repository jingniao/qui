<template>
  <div class="space-y-6">
    <PageHeader title="代理用户" parent="用户" description="通过节点组分配接入权限，每台设备使用独立协议凭据。">
      <template #actions>
        <button @click="openCreate" class="ios-btn-primary">新建用户</button>
      </template>
    </PageHeader>

    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索用户名或显示名称…"
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
    <EmptyState v-else-if="!users.length" title="暂无代理用户" />
    <DataTable v-else>
      <template #header>
        <th>用户</th>
        <th>状态</th>
        <th>节点组</th>
        <th>设备</th>
        <th>连接</th>
        <th>额度 / 已用</th>
        <th>到期时间</th>
        <th>操作</th>
      </template>
      <template #body>
        <tr v-for="user in users" :key="user.id">
          <td>
            <router-link :to="`/users/${user.id}`" class="font-semibold text-ios-blue hover:underline text-[15px]">
              {{ user.displayName }}
            </router-link>
            <p class="text-[13px] text-ios-secondary font-normal mt-0.5">{{ user.username }}</p>
          </td>
          <td><StatusBadge :status="effectiveStatus(user)" /></td>
          <td class="text-xs text-ios-secondary">
            {{ user.nodeGroupIds.map(id => groups.find(group => group.id === id)?.name || id).join('、') || '未授权' }}
          </td>
          <td class="text-xs font-mono tabular-nums text-ios-secondary">{{ user.devicesCount }} / {{ user.maxDevices }}</td>
          <td class="text-xs">
            <template v-if="user.onlineConnections === null">
              <span class="text-ios-tertiary">未知</span>
            </template>
            <template v-else>
              <span class="font-mono tabular-nums text-ios-secondary">{{ user.onlineConnections }} / {{ user.maxConnections }}</span>
              <span v-if="user.overLimit" class="ml-1 font-semibold text-ios-red">超限</span>
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
          <td class="space-x-2">
            <button @click="openEdit(user)" class="ios-action-btn-primary">编辑</button>
            <button @click="removeTarget = user; confirmVisible = true" class="ios-action-btn-danger">删除</button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

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
          <label class="block text-xs font-medium text-ios-secondary">
            最大设备数
            <input v-model.number="form.maxDevices" type="number" min="1" max="100" class="form-input-base mt-1.5" />
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            最大连接数
            <input v-model.number="form.maxConnections" type="number" min="1" max="10000" class="form-input-base mt-1.5" />
          </label>
        </div>

        <!-- 允许协议 (iOS Selectable Chips) -->
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

        <!-- 授权节点组 (iOS Inset Grouped List) -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between">
            <label class="text-xs font-medium text-ios-secondary">授权节点组</label>
            <span v-if="groups.length" class="text-[11px] text-ios-tertiary">已选 {{ form.nodeGroupIds.length }} 个节点组</span>
          </div>
          <div class="rounded-2xl p-3 bg-[#F5F5F7] border border-black/[0.06]">
            <div v-if="groups.length" class="space-y-1.5 max-h-48 overflow-y-auto pr-1 scrollbar-thin">
              <label
                v-for="group in groups"
                :key="group.id"
                :class="[
                  'flex items-center justify-between p-2.5 rounded-xl text-xs cursor-pointer select-none transition-all',
                  form.nodeGroupIds.includes(group.id)
                    ? 'bg-white text-ios-label font-medium shadow-2xs border border-ios-blue/30'
                    : 'bg-white/60 text-ios-secondary hover:bg-white hover:text-ios-label border border-transparent'
                ]"
              >
                <div class="flex items-center gap-2.5 min-w-0 flex-1 truncate">
                  <div
                    :class="[
                      'w-4 h-4 rounded-md flex items-center justify-center transition-all shrink-0',
                      form.nodeGroupIds.includes(group.id)
                        ? 'bg-ios-blue text-white'
                        : 'border border-[#C7C7CC] bg-white'
                    ]"
                  >
                    <svg v-if="form.nodeGroupIds.includes(group.id)" class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                    </svg>
                  </div>
                  <span class="truncate">{{ group.name }}</span>
                </div>
                <span v-if="group.purpose" class="text-[11px] text-ios-tertiary font-normal shrink-0 ml-2">{{ group.purpose }}</span>
                <input v-model="form.nodeGroupIds" type="checkbox" :value="group.id" class="sr-only" />
              </label>
            </div>
            <p v-else class="text-xs text-ios-tertiary py-1">请先创建节点组。</p>
          </div>
        </div>

        <label class="block text-xs font-medium text-ios-secondary">
          备注
          <textarea v-model="form.remark" maxlength="300" rows="2" class="form-input-base mt-1.5" placeholder="可选填写用户相关备忘信息…"></textarea>
        </label>

        <div class="rounded-xl p-3 bg-[#F5F5F7] border border-black/[0.06] flex items-center gap-2 text-xs text-ios-secondary">
          <span class="text-ios-blue shrink-0 text-sm">ℹ️</span>
          <p class="leading-relaxed">保存业务设置后，请在系统管理或配置版本中发布用户接入配置，使节点授权生效。</p>
        </div>
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
