<template>
  <section class="panel-card p-5 space-y-3">
    <div class="flex items-center justify-between"><h2 class="font-semibold text-ios-label">线路组</h2><button v-if="isOwner && !node.deletedAt" class="ios-action-btn-secondary" aria-label="编辑所属线路组" @click="openGroups">✎</button></div>
    <div class="flex flex-wrap gap-2 text-xs text-ios-secondary">
      <template v-for="group in memberships" :key="group.id"><router-link v-if="isOwner" :to="{ path: '/proxy/groups', query: { group: group.id } }" class="rounded-full bg-ios-blue/10 px-3 py-1 text-ios-blue">{{ group.name }}{{ group.enabled ? '' : '（已停用）' }}</router-link><span v-else>{{ group.name }}</span></template>
      <span v-if="!memberships.length">{{ isOwner ? '未加入线路组' : '当前权限无法读取线路组' }}</span>
    </div>
    <FormDrawer v-model="groupsOpen" title="编辑所属线路组" :loading="saving" @confirm="saveGroups">
      <label v-for="group in editableGroups" :key="group.id" class="flex items-center gap-2 text-sm text-ios-secondary cursor-pointer">
        <input v-model="selected" type="checkbox" :value="group.id" />
        {{ group.name }}
        ·
        {{ groupPurposeNames[group.purpose] }}
      </label>
      <p v-if="groupsLoaded && !editableGroups.length" class="text-sm text-ios-secondary">尚无线路组，请先创建线路组。</p>
      <button v-if="groupError && !groupsLoaded" class="ios-btn-secondary" @click="openGroups">重新读取</button>
      <p v-if="groupError" role="alert" class="text-sm text-ios-red">{{ groupError }}</p>
    </FormDrawer>
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import type { NodeView } from '../../api';
import { groupAPI, type NodeGroup } from '../../groupsApi';
import { groupInput, groupPurposeNames } from '../../../proxy/lineGroups';
import { choices } from '../../../proxy/api';
import { isOwner } from '../../../../app/auth';
import FormDrawer from '../../../../components/FormDrawer.vue';
const props = defineProps<{ node: NodeView; groups: NodeGroup[] }>();
const emit = defineEmits<{ changed: [] }>();
const memberships = computed(() => props.groups.filter(group => group.nodeIds.includes(props.node.id)));
const groupsOpen = ref(false), saving = ref(false), groupsLoaded = ref(false), groupError = ref(''), selected = ref<string[]>([]), editableGroups = ref<NodeGroup[]>([]);
async function openGroups() {
  groupError.value = ''; groupsOpen.value = true; saving.value = true; groupsLoaded.value = false;
  try { editableGroups.value = await choices(groupAPI.list); selected.value = editableGroups.value.filter(g => g.nodeIds.includes(props.node.id)).map(g => g.id); groupsLoaded.value = true; }
  catch (e) { groupError.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function saveGroups() {
  if (saving.value || !groupsLoaded.value) return;
  saving.value = true; groupError.value = '';
  try {
    const latest = await choices(groupAPI.list);
    for (const group of latest) {
      const before = editableGroups.value.find(item => item.id === group.id);
      if (!before || before.nodeIds.includes(props.node.id) === selected.value.includes(group.id)) continue;
      const ids = group.nodeIds.filter(id => id !== props.node.id);
      if (selected.value.includes(group.id)) ids.push(props.node.id);
      await groupAPI.save(group.id, groupInput(group, ids));
    }
    groupsOpen.value = false; emit('changed');
  } catch (e) { groupError.value = (e as Error).message; emit('changed'); }
  finally { saving.value = false; }
}
</script>
