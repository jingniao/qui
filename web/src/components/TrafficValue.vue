<template>
  <span class="tabular-nums text-ios-label tracking-normal inline-flex items-baseline">
    <span :class="valueClass">{{ formatted.value }}</span>
    <span v-if="formatted.unit" class="text-xs text-ios-tertiary ml-1 font-normal">{{ formatted.unit }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    bytes: number | null | undefined;
    valueClass?: string;
  }>(),
  {
    valueClass: 'font-medium'
  }
);

const formatted = computed(() => {
  const b = props.bytes;
  if (b === null || b === undefined) return { value: '—', unit: '' };
  if (b === 0) return { value: '0', unit: 'B' };
  if (!b) return { value: '0', unit: 'B' };
  const k = 1024;
  const sizes = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];
  const i = Math.floor(Math.log(b) / Math.log(k));
  const val = (b / Math.pow(k, i)).toFixed(i === 0 ? 0 : 2);
  return { value: val, unit: sizes[i] };
});
</script>
