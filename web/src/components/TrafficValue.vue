<template>
  <span class="font-mono text-slate-800 tracking-tight">
    <span :class="valueClass">{{ formatted.value }}</span>
    <span class="text-xs text-slate-500 ml-1 font-sans">{{ formatted.unit }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    bytes: number;
    valueClass?: string;
  }>(),
  {
    valueClass: 'font-medium'
  }
);

const formatted = computed(() => {
  const b = props.bytes;
  if (!b || b === 0) return { value: '0', unit: 'B' };
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(b) / Math.log(k));
  const val = (b / Math.pow(k, i)).toFixed(i === 0 ? 0 : 2);
  return { value: val, unit: sizes[i] };
});
</script>
