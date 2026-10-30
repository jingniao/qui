<template>
  <FormDrawer :model-value="modelValue" :title="group ? '编辑线路组' : '新建线路组'" :loading="saving" confirm-text="保存"
    @update:model-value="$emit('update:modelValue', $event)" @confirm="save">
    <div class="space-y-4 text-sm">
      <label class="block text-xs font-medium text-ios-secondary">名称
        <input v-model="form.name" maxlength="40" class="form-input-base mt-1.5" />
      </label>
      <label class="block text-xs font-medium text-ios-secondary">类型
        <select :value="isUserGroup(form) ? 'user' : 'internal'" class="form-input-base mt-1.5" @change="changeKind">
          <option value="user">用户线路组</option><option value="internal">内部分组</option>
        </select>
      </label>
      <label class="flex items-center gap-2 cursor-pointer text-ios-label"><input v-model="form.enabled" type="checkbox" class="accent-ios-blue" />启用</label>
      <label class="block text-xs font-medium text-ios-secondary">备注
        <textarea v-model="form.remark" maxlength="300" class="form-input-base mt-1.5"></textarea>
      </label>
      <details class="rounded-xl bg-ios-bg p-3">
        <summary class="cursor-pointer text-ios-secondary">高级</summary>
        <div class="mt-3 space-y-3">
          <label class="block text-xs font-medium text-ios-secondary">用途
            <select v-model="form.purpose" class="form-input-base mt-1.5">
              <option value="subscription">用户订阅入口（subscription）</option>
              <option value="mixed">混合用途（mixed）</option>
              <option value="relay">内部中转（relay）</option>
              <option value="egress">系统出口（egress）</option>
            </select>
          </label>
          <label v-if="isUserGroup(form)" class="flex items-center gap-2 cursor-pointer text-ios-label">
            <input v-model="form.subscriptionVisible" type="checkbox" class="accent-ios-blue" />订阅可见
          </label>
        </div>
      </details>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    </div>
  </FormDrawer>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import { groupAPI, type GroupInput, type NodeGroup } from '../../nodes/groupsApi';
import { isUserGroup } from '../lineGroups';

const props = defineProps<{ modelValue: boolean; group: NodeGroup | null }>();
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; saved: [group: NodeGroup] }>();
const saving = ref(false);
const error = ref('');
function defaults(): GroupInput {
  return { name: '', purpose: 'subscription', enabled: true, subscriptionVisible: true, nodeIds: [], remark: '' };
}
const form = reactive(defaults());
watch(() => props.modelValue, open => {
  if (!open) return;
  Object.assign(form, props.group ? { ...props.group, nodeIds: [...props.group.nodeIds] } : defaults());
  error.value = '';
});
function changeKind(event: Event) {
  form.purpose = (event.target as HTMLSelectElement).value === 'user' ? 'subscription' : 'relay';
  form.subscriptionVisible = isUserGroup(form);
}
async function save() {
  if (saving.value) return;
  if (!form.name.trim()) { error.value = '请输入线路组名称'; return; }
  saving.value = true;
  try {
    const result = await groupAPI.save(props.group?.id || null, {
      name: form.name.trim(), purpose: form.purpose, enabled: form.enabled, remark: form.remark,
      subscriptionVisible: isUserGroup(form) && form.subscriptionVisible, nodeIds: [...form.nodeIds]
    });
    emit('saved', result);
    emit('update:modelValue', false);
  } catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
</script>
