<template>
  <div>
    <div class="hidden lg:block">
      <DataTable>
        <template #header>
          <th v-if="owner"><input type="checkbox" aria-label="选择本页全部服务器" :checked="allSelected" :disabled="!selectable.length" @change="toggleAll" /></th>
          <th>服务器</th><th>状态</th><th v-for="field in columns" :key="field.key">{{ field.label }}</th><th v-if="owner">操作</th>
        </template>
        <template #body>
          <tr v-for="row in rows" :key="row.node.id">
            <td v-if="owner"><input v-if="!row.node.deletedAt" type="checkbox" :aria-label="`选择 ${row.node.name}`" :checked="selected.includes(row.node.id)" @change="$emit('toggle', row.node.id)" /></td>
            <td><RouterLink :to="`/nodes/${row.node.id}`" class="text-ios-blue font-semibold hover:underline">{{ row.node.name }}</RouterLink><p v-if="row.region" class="text-xs text-ios-secondary">{{ row.region }}</p></td>
            <td><StatusBadge :status="row.status.tone" :label="row.status.label" /></td>
            <td v-for="field in visibleFields(row)" :key="field.key" class="text-xs font-mono tabular-nums whitespace-nowrap" :class="field.warning ? 'text-ios-orange' : 'text-ios-secondary'">
              <TrafficValue v-if="field.key === 'today' || field.key === 'month'" :bytes="field.bytes" /><span v-else>{{ field.value }}</span>
            </td>
            <td v-if="owner"><ServerRowMenu :node="row.node" compact @install="$emit('install', row.node)" @delete="$emit('delete', row.node)" /></td>
          </tr>
        </template>
      </DataTable>
    </div>
    <div class="space-y-4 lg:hidden">
      <article v-for="row in rows" :key="row.node.id" class="panel-card p-4 space-y-3">
        <div class="flex items-center gap-3">
          <input v-if="owner && !row.node.deletedAt" type="checkbox" :aria-label="`选择 ${row.node.name}`" :checked="selected.includes(row.node.id)" @change="$emit('toggle', row.node.id)" />
          <RouterLink :to="`/nodes/${row.node.id}`" class="text-ios-blue font-semibold flex-1">{{ row.node.name }}</RouterLink>
          <StatusBadge :status="row.status.tone" :label="row.status.label" />
        </div>
        <p v-if="row.region" class="text-xs text-ios-secondary">{{ row.region }}</p>
        <dl class="grid grid-cols-2 gap-3 text-sm">
          <div v-for="field in visibleFields(row)" :key="field.key" class="min-w-0">
            <dt class="text-xs text-ios-tertiary">{{ field.label }}</dt>
            <dd class="font-mono tabular-nums break-words" :class="field.warning ? 'text-ios-orange' : 'text-ios-label'">
              <TrafficValue v-if="field.key === 'today' || field.key === 'month'" :bytes="field.bytes" /><span v-else>{{ field.value }}</span>
            </dd>
          </div>
        </dl>
        <ServerRowMenu v-if="owner" :node="row.node" @install="$emit('install', row.node)" @delete="$emit('delete', row.node)" />
      </article>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../api';
import type { ServerField, ServerRow } from './serverListModel';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import ServerRowMenu from './ServerRowMenu.vue';
const props = defineProps<{ rows: ServerRow[]; columns: ServerField[]; owner: boolean; selected: string[] }>();
const emit = defineEmits<{ toggle: [id: string]; install: [node: NodeView]; delete: [node: NodeView] }>();
const selectable = computed(() => props.rows.filter(row => !row.node.deletedAt));
const allSelected = computed(() => selectable.value.length > 0 && selectable.value.every(row => props.selected.includes(row.node.id)));
function visibleFields(row: ServerRow) { return row.fields.filter(field => props.columns.some(column => column.key === field.key)); }
function toggleAll() {
  const remove = allSelected.value;
  for (const row of selectable.value) if (props.selected.includes(row.node.id) === remove) emit('toggle', row.node.id);
}
</script>
