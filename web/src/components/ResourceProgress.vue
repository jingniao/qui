<template>
  <div class="flex items-center gap-2">
    <div class="flex-1 h-2 bg-black/[0.06] rounded-full overflow-hidden">
      <div
        class="h-full rounded-full transition-all duration-300"
        :class="barColor"
        :style="{ width: `${clampedPercent}%` }"
      ></div>
    </div>
    <span v-if="showLabel" class="text-xs font-mono tabular-nums font-medium text-ios-secondary min-w-[36px] text-right">
      {{ clampedPercent }}%
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    percentage: number;
    showLabel?: boolean;
  }>(),
  {
    showLabel: true
  }
);

const clampedPercent = computed(() => {
  return Math.min(100, Math.max(0, Math.round(props.percentage)));
});

const barColor = computed(() => {
  const p = clampedPercent.value;
  if (p >= 85) return 'bg-rose-500';
  if (p >= 70) return 'bg-amber-500';
  return 'bg-emerald-500';
});
</script>
