<template>
  <div class="min-w-0" :title="tooltip">
    <p class="text-[11px] text-neutral-400 font-medium truncate">本月整机流量</p>
    <div
      class="mt-0.5 flex items-baseline gap-x-1.5 gap-y-0.5 flex-wrap tabular-nums"
      :class="compact ? 'text-xs' : 'text-sm'"
    >
      <span class="font-bold text-neutral-900"><TrafficValue :bytes="usedBytes" /></span>
      <span v-if="allowanceBytes" class="text-[10px] text-neutral-400 font-normal">/ {{ formatBytes(allowanceBytes) }}</span>
      <span v-if="reset" class="text-[10px] text-neutral-400 font-normal">· {{ reset }}</span>
    </div>
    <p v-if="stale" class="text-[10px] text-amber-600 font-medium">截至上次采样</p>
    <div v-if="percent !== null" class="mt-1 h-1 w-full bg-black/[0.05] rounded-full overflow-hidden">
      <div class="h-full bg-ios-blue rounded-full transition-all duration-500" :style="{ width: `${percent}%` }"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import type { MachineTrafficSummary } from '../api';
import { formatBytes } from './serverMetricsModel';
import { machineBytes, resetLabel, usagePercent, usageTooltip } from '../machineTrafficModel';

// Machine (whole-server) traffic of the current billing period. The value is
// unknown ("—") without measurements; a measured zero shows "0 B".
const props = defineProps<{ summary?: MachineTrafficSummary | null; compact?: boolean }>();

const usedBytes = computed(() => machineBytes(props.summary?.usedBytes));
const allowanceBytes = computed(() => props.summary?.settings.allowanceBytes ?? null);
const reset = computed(() => props.summary ? resetLabel(props.summary.nextResetAt, props.summary.timezone || undefined) : '');
const stale = computed(() => props.summary?.status === 'stale');
const percent = computed(() => usagePercent(props.summary));
const tooltip = computed(() => usageTooltip(props.summary));
</script>
