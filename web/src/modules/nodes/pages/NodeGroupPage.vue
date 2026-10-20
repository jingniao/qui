<template>
  <div class="space-y-6">
    <PageHeader title="节点组" parent="节点" description="用户通过节点组获得入口，一个节点可以属于多个组。中转与出口组不会用于订阅。"><template #actions><button @click="openCreate" class="ios-btn-primary">创建节点组</button></template></PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" /><EmptyState v-else-if="!groups.length" title="暂无节点组" />
    <DataTable v-else><template #header><th>名称</th><th>用途</th><th>节点</th><th>状态</th><th>订阅可见</th><th>操作</th></template><template #body><tr v-for="group in groups" :key="group.id"><td class="font-semibold text-ios-label text-[15px]">{{ group.name }}</td><td class="text-xs text-ios-secondary">{{ purposeNames[group.purpose] }}</td><td class="text-xs text-ios-secondary">{{ group.nodeIds.map(id => nodes.find(node => node.id === id)?.name || id).join('、') || '暂无成员' }}</td><td><StatusBadge :status="group.enabled ? 'active' : 'disabled'" /></td><td class="text-xs text-ios-secondary">{{ group.subscriptionVisible ? '是' : '否' }}</td><td class="space-x-2"><button @click="openEdit(group)" class="ios-action-btn-primary">编辑</button><button @click="target = group; confirmVisible = true" class="ios-action-btn-danger">删除</button></td></tr></template><template #footer><Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" /></template></DataTable>
    <FormDrawer v-model="drawer" :title="editing ? '编辑节点组' : '创建节点组'" :loading="saving" @confirm="save">
      <div class="space-y-4 text-sm">
        <label class="block text-xs font-medium text-ios-secondary">组名<input v-model="form.name" maxlength="40" class="form-input-base mt-1.5" /></label>
        <label class="block text-xs font-medium text-ios-secondary">用途<select v-model="form.purpose" class="form-input-base mt-1.5"><option v-for="(name, purpose) in purposeNames" :key="purpose" :value="purpose">{{ name }}</option></select></label>
        <div class="space-y-2 pt-1">
          <label class="flex items-center gap-2 cursor-pointer select-none text-xs font-normal text-ios-label">
            <input v-model="form.enabled" type="checkbox" class="w-4 h-4 rounded text-ios-blue accent-ios-blue" />
            <span>启用</span>
          </label>
          <label v-if="form.purpose === 'subscription' || form.purpose === 'mixed'" class="flex items-center gap-2 cursor-pointer select-none text-xs font-normal text-ios-label">
            <input v-model="form.subscriptionVisible" type="checkbox" class="w-4 h-4 rounded text-ios-blue accent-ios-blue" />
            <span>可用于用户订阅</span>
          </label>
        </div>
        <div class="space-y-1.5 pt-1">
          <div class="flex items-center justify-between">
            <label class="text-xs font-medium text-ios-secondary">组内节点</label>
            <span v-if="nodes.length" class="text-[11px] text-ios-tertiary">已选 {{ form.nodeIds.length }} 个节点</span>
          </div>
          <div class="rounded-2xl p-3 bg-[#F5F5F7] border border-black/[0.06]">
            <div v-if="nodes.length" class="space-y-1.5 max-h-48 overflow-y-auto pr-1 scrollbar-thin">
              <label
                v-for="node in nodes"
                :key="node.id"
                :class="[
                  'flex items-center justify-between p-2.5 rounded-xl text-xs cursor-pointer select-none transition-all',
                  form.nodeIds.includes(node.id)
                    ? 'bg-white text-ios-label font-medium shadow-2xs border border-ios-blue/30'
                    : 'bg-white/60 text-ios-secondary hover:bg-white hover:text-ios-label border border-transparent'
                ]"
              >
                <div class="flex items-center gap-2.5 min-w-0 flex-1 truncate">
                  <div
                    :class="[
                      'w-4 h-4 rounded-md flex items-center justify-center transition-all shrink-0',
                      form.nodeIds.includes(node.id)
                        ? 'bg-ios-blue text-white'
                        : 'border border-[#C7C7CC] bg-white'
                    ]"
                  >
                    <svg v-if="form.nodeIds.includes(node.id)" class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                      <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                    </svg>
                  </div>
                  <span class="truncate">{{ node.name }}</span>
                </div>
                <span class="text-[11px] text-ios-tertiary font-normal shrink-0 ml-2">{{ node.status }}</span>
                <input v-model="form.nodeIds" type="checkbox" :value="node.id" class="sr-only" />
              </label>
            </div>
            <p v-else class="text-xs text-ios-tertiary py-1">暂无可用节点</p>
          </div>
        </div>
        <label class="block text-xs font-medium text-ios-secondary">备注<textarea v-model="form.remark" maxlength="300" class="form-input-base mt-1.5"></textarea></label>
      </div>
    </FormDrawer>
    <ConfirmDialog v-model="confirmVisible" title="删除节点组" :message="`删除 ${target?.name || ''} 并移除相关用户授权？节点本身会保留。`" @confirm="remove" />
  </div>
</template>
<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'; import { groupAPI, type NodeGroup, type GroupInput } from '../groupsApi'; import { nodeAPI, type NodeView } from '../api'; import { request, type Page } from '../../../app/apiClient'; import { showToast } from '../../../app/api'; import PageHeader from '../../../components/PageHeader.vue'; import DataTable from '../../../components/DataTable.vue'; import StatusBadge from '../../../components/StatusBadge.vue'; import Pagination from '../../../components/Pagination.vue'; import FormDrawer from '../../../components/FormDrawer.vue'; import ConfirmDialog from '../../../components/ConfirmDialog.vue'; import ErrorState from '../../../components/ErrorState.vue'; import EmptyState from '../../../components/EmptyState.vue';
const groups = ref<NodeGroup[]>([]); const nodes = ref<NodeView[]>([]); const total = ref(0); const page = ref(1); const error = ref(''); const drawer = ref(false); const editing = ref<NodeGroup | null>(null); const saving = ref(false); const target = ref<NodeGroup | null>(null); const confirmVisible = ref(false);
const purposeNames = { subscription: '用户订阅入口', relay: '内部中转', egress: '系统出口', mixed: '混合用途' };
function defaults(): GroupInput { return { name: '', purpose: 'subscription', enabled: true, subscriptionVisible: true, nodeIds: [], remark: '' }; }
const form = reactive<GroupInput>(defaults());
async function load() { try { const [result, nodeResult] = await Promise.all([groupAPI.list(page.value), request<Page<NodeView>>('/nodes?page=1&pageSize=100')]); groups.value = result.items; total.value = result.total; nodes.value = nodeResult.items; error.value = ''; } catch (e) { error.value = (e as Error).message; } }
function changePage(value: number) { page.value = value; load(); } function openCreate() { editing.value = null; Object.assign(form, defaults()); drawer.value = true; }
function openEdit(group: NodeGroup) { editing.value = group; Object.assign(form, { name: group.name, purpose: group.purpose, enabled: group.enabled, subscriptionVisible: group.subscriptionVisible, nodeIds: [...group.nodeIds], remark: group.remark }); drawer.value = true; }
async function save() { if (saving.value) return; saving.value = true; try { await groupAPI.save(editing.value?.id || null, { ...form, subscriptionVisible: ['subscription', 'mixed'].includes(form.purpose) && form.subscriptionVisible }); drawer.value = false; showToast('节点组已保存'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; } }
async function remove() { if (!target.value) return; try { await groupAPI.delete(target.value.id); showToast('节点组已删除'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } } onMounted(load);
</script>
