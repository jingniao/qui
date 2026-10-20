<template>
  <span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[11px] bg-black/[0.04] text-ios-label border border-black/[0.06] hover:bg-black/[0.07] transition-all cursor-pointer">
    <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="online ? 'bg-ios-green' : 'bg-ios-tertiary'"></span>
    <span class="font-medium font-mono tabular-nums truncate max-w-[200px]">{{ name }}</span>
  </span>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    name: string;
    online?: boolean;
  }>(),
  {
    online: true
  }
);
</script>
