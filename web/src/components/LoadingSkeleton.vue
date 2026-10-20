<template>
  <div class="space-y-3 p-5 bg-white rounded-2xl border border-black/[0.08]">
    <div class="h-4 bg-black/[0.06] rounded-full w-1/4 animate-pulse"></div>
    <div class="space-y-2.5 pt-2">
      <div v-for="i in rows" :key="i" class="h-9 bg-black/[0.04] rounded-xl animate-pulse"></div>
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
