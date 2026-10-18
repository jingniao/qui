<template>
  <span class="inline-flex items-center gap-2 px-3 py-1 rounded-full text-xs bg-black/[0.04] text-[#1C1C1E] border border-black/[0.06] hover:bg-black/[0.07] transition-all cursor-pointer">
    <span class="w-2 h-2 rounded-full shrink-0" :class="online ? 'bg-[#34C759]' : 'bg-[#8E8E93]'"></span>
    <span class="font-semibold font-mono truncate max-w-[200px]">{{ name }}</span>
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
