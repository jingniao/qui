<template>
  <section ref="section" tabindex="-1" class="space-y-3 border-t border-ios-border pt-4 outline-none" aria-label="隧道列表">
    <div class="flex flex-wrap items-center justify-between gap-2">
      <h3 class="text-[15px] font-semibold text-ios-label">隧道 ({{ scheme.connections.length }})</h3>
      <button type="button" class="ios-btn-secondary" @click="$emit('create')">+ 新建隧道</button>
    </div>
    <p v-if="checking" class="text-xs text-ios-tertiary" role="status">检查中…</p>
    <p v-if="error || plan?.message" class="text-xs text-ios-red" role="status">{{ error || plan?.message }}</p>
    <p v-if="!scheme.connections.length" class="text-xs text-ios-tertiary">暂无隧道，点击“新建隧道”或在画布上连线。</p>
    <button v-for="row in rows" :key="row.connection.id" type="button"
      class="w-full rounded-xl border border-ios-border bg-ios-card p-3 text-left space-y-1 active:scale-[0.98] transition-transform"
      @click="$emit('edit', row.connection)">
      <span class="block text-sm font-medium text-ios-label">{{ row.title }}</span>
      <span class="block text-xs text-ios-secondary font-mono tabular-nums break-all">{{ row.kind }} · {{ row.network }}</span>
      <span v-if="row.connection.invalidReason" class="block text-xs text-ios-red">{{ row.connection.invalidReason }}</span>
      <span v-if="row.status && !checking && !error" class="block text-xs" :class="row.status.error ? 'text-ios-red' : row.status.text === '可发布' ? 'text-ios-green' : 'text-ios-tertiary'">{{ row.status.text }}</span>
    </button>
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import type { CanvasConnection, RelayScheme } from '../../proxyNodes';
import { tunnelStatus, tunnelSummary, type TunnelPlan } from './tunnelEditorModel';
const props = defineProps<{ scheme: RelayScheme; plan: TunnelPlan | null; checking: boolean; error: string }>();
defineEmits<{ create: []; edit: [connection: CanvasConnection] }>();
const section = ref<HTMLElement>();
const rows = computed(() => props.scheme.connections.map(connection => ({ connection, ...tunnelSummary(props.scheme, connection),
  status: tunnelStatus(props.plan, connection.id, props.scheme.executionLocation === 'client') })));
defineExpose({ focus() { section.value?.scrollIntoView({ behavior: 'smooth', block: 'nearest' }); section.value?.focus({ preventScroll: true }); } });
</script>
