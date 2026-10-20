<template>
  <div class="flex flex-col items-center justify-center p-8 text-center bg-white rounded-2xl border border-dashed border-black/10 my-4">
    <div class="w-11 h-11 rounded-full bg-black/[0.04] flex items-center justify-center text-ios-tertiary mb-3 border border-black/[0.04]">
      <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4" />
      </svg>
    </div>
    <h3 class="text-sm font-semibold text-ios-label mb-1">{{ title }}</h3>
    <p class="text-xs text-ios-tertiary max-w-sm mb-4 leading-relaxed">{{ description }}</p>
    <slot name="action"></slot>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string;
    description?: string;
  }>(),
  {
    title: '暂无数据',
    description: '当前列表中暂无相关记录，您可以尝试新增或调整筛选条件。'
  }
);
</script>
