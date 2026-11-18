<template>
  <div class="min-w-0" :title="tooltip">
    <template v-if="display === 'metric'">
      <div class="flex items-center justify-between text-xs mb-1">
        <span class="text-neutral-400 font-medium">流量</span>
        <span class="font-bold text-neutral-800 font-mono tabular-nums">{{ percent === null ? '—' : `${percent}%` }}</span>
      </div>
      <div class="h-1 w-full bg-black/[0.05] rounded-full overflow-hidden" :title="unlimited ? '未设置流量额度' : tooltip">
        <div
          v-if="percent !== null || (unlimited && usedBytes !== null)"
          class="h-full rounded-full transition-all duration-300"
          :class="percent !== null && percent >= 90 ? 'bg-ios-red' : percent !== null && percent >= 75 ? 'bg-ios-orange' : 'bg-ios-green'"
          :style="{ width: `${unlimited ? 100 : percent}%` }"
        ></div>
      </div>
    </template>
    <template v-else>
      <p class="text-xs text-neutral-400 font-medium truncate">本月整机流量</p>
      <div
        class="mt-0.5 flex items-baseline gap-x-1.5 gap-y-0.5 flex-wrap tabular-nums"
        :class="compact ? 'text-xs' : 'text-sm'"
      >
        <span class="font-bold text-neutral-900"><TrafficValue :bytes="usedBytes" /></span>
        <span v-if="allowanceBytes" class="text-xs text-neutral-400 font-normal">/ {{ formatBytes(allowanceBytes) }}</span>
        <span v-if="reset" class="text-xs text-neutral-400 font-normal">· {{ reset }}</span>
      </div>
      <p v-if="stale && !compact" class="text-xs text-amber-600 font-medium">截至上次采样</p>
      <div v-if="percent !== null" class="mt-1 h-1 w-full bg-black/[0.05] rounded-full overflow-hidden">
        <div class="h-full bg-ios-blue rounded-full transition-all duration-500" :style="{ width: `${percent}%` }"></div>
      </div>
    </template>
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
const props = withDefaults(defineProps<{ summary?: MachineTrafficSummary | null; compact?: boolean; display?: 'summary' | 'metric' }>(), { display: 'summary' });

const usedBytes = computed(() => machineBytes(props.summary?.usedBytes));
const allowanceBytes = computed(() => props.summary?.settings.allowanceBytes ?? null);
const unlimited = computed(() => !!props.summary && allowanceBytes.value === null);
const reset = computed(() => props.summary ? resetLabel(props.summary.nextResetAt, props.summary.timezone || undefined) : '');
const stale = computed(() => props.summary?.status === 'stale');
const percent = computed(() => usagePercent(props.summary));
const tooltip = computed(() => usageTooltip(props.summary));
</script>
