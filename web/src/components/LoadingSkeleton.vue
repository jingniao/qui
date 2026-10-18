<template>
  <div class="space-y-3 p-4 bg-white rounded-xl border border-slate-200">
    <div class="h-4 bg-slate-200 rounded w-1/4 animate-pulse"></div>
    <div class="space-y-2 pt-2">
      <div v-for="i in rows" :key="i" class="h-10 bg-slate-100 rounded animate-pulse"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    rows?: number;
  }>(),
  {
    rows: 4
  }
);
</script>
