<template>
  <FormDrawer :model-value="modelValue" :title="kind === 'servers' ? '添加服务器' : '授权用户'" :loading="saving" width="xl" :confirm-text="kind === 'servers' ? '添加' : '授权'"
    @update:model-value="$emit('update:modelValue', $event)" @confirm="$emit('save', selected)">
    <div class="space-y-4">
      <input v-model="keyword" class="form-input-base" :placeholder="kind === 'servers' ? '搜索服务器名称或地址' : '搜索用户名或显示名称'" aria-label="搜索" />
      <select v-if="kind === 'servers'" v-model="status" class="form-input-base" aria-label="服务器状态">
        <option value="">全部状态</option>
        <option v-for="(label, value) in statuses" :key="value" :value="value">{{ label }}</option>
      </select>
      <div class="space-y-2 max-h-96 overflow-y-auto">
        <label v-for="item in filtered" :key="item.id" class="flex items-start gap-3 rounded-xl bg-ios-bg p-3 cursor-pointer">
          <input v-model="selected" type="checkbox" :value="item.id" class="mt-1 accent-ios-blue" />
          <span class="flex-1 min-w-0 space-y-2">
            <span class="block text-sm text-ios-label break-words">{{ item.name }}</span>
            <span v-if="kind === 'servers'" class="flex flex-wrap gap-1">
              <span v-for="group in otherGroups(item.id)" :key="group.id" class="rounded-full bg-ios-blue/10 px-2 py-1 text-xs text-ios-blue">{{ group.name }}</span>
            </span>
          </span>
          <StatusBadge v-if="item.status" :status="item.status" />
        </label>
        <p v-if="!filtered.length" class="text-sm text-ios-tertiary">没有符合条件的{{ kind === 'servers' ? '服务器' : '用户' }}</p>
      </div>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    </div>
  </FormDrawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import type { NodeGroup } from '../../nodes/groupsApi';
import type { NodeView } from '../../nodes/api';
import type { ProxyUser } from '../../users/api';

const props = defineProps<{
  modelValue: boolean; kind: 'servers' | 'users'; group: NodeGroup; groups: NodeGroup[];
  servers: NodeView[]; users: ProxyUser[]; saving: boolean; error: string;
}>();
defineEmits<{ 'update:modelValue': [value: boolean]; save: [ids: string[]] }>();
const keyword = ref('');
const status = ref('');
const selected = ref<string[]>([]);
const statuses = { online: '在线', offline: '离线', degraded: '状态降级', error: '异常', applying: '发布中', rollback: '回滚中', pending: '等待登记', disabled: '已停用' };
watch(() => props.modelValue, open => {
  if (open) { keyword.value = ''; status.value = ''; selected.value = []; }
});
// Keep a retry limited to outstanding grants after a partially successful save.
watch(() => props.group, () => {
  selected.value = selected.value.filter(id => props.kind === 'servers'
    ? !props.group.nodeIds.includes(id)
    : !props.users.find(user => user.id === id)?.nodeGroupIds.includes(props.group.id));
});
const filtered = computed(() => {
  const items = props.kind === 'servers'
    ? props.servers.filter(server => !props.group.nodeIds.includes(server.id)).map(server => ({ id: server.id, name: server.name, search: `${server.name} ${server.publicIp} ${server.hostname}`, status: server.status }))
    : props.users.filter(user => !user.nodeGroupIds.includes(props.group.id)).map(user => ({ id: user.id, name: user.displayName ? `${user.displayName}（${user.username}）` : user.username, search: `${user.username} ${user.displayName}`, status: '' }));
  return items.filter(item => item.search.toLowerCase().includes(keyword.value.trim().toLowerCase()) && (!status.value || item.status === status.value));
});
function otherGroups(serverId: string) {
  return props.groups.filter(group => group.id !== props.group.id && group.nodeIds.includes(serverId));
}
</script>
