<template>
  <div class="flex items-center justify-between text-xs text-ios-tertiary">
    <div>
      共 <span class="font-semibold text-ios-label tabular-nums">{{ total }}</span> 条记录
      <span v-if="totalPages > 1" class="ml-2 font-normal">第 <span class="tabular-nums font-medium text-ios-label">{{ currentPage }}</span> / <span class="tabular-nums">{{ totalPages }}</span> 页</span>
    </div>
    <div class="flex items-center gap-2">
      <button
        :disabled="currentPage <= 1"
        @click="$emit('update:page', currentPage - 1)"
        class="px-3.5 py-1 rounded-full border border-[#D1D1D6] bg-white text-ios-label text-xs font-medium hover:bg-[#F5F5F7] disabled:opacity-35 disabled:cursor-not-allowed transition-all shadow-xs"
      >
        上一页
      </button>
      <button
        :disabled="currentPage >= totalPages"
        @click="$emit('update:page', currentPage + 1)"
        class="px-3.5 py-1 rounded-full border border-[#D1D1D6] bg-white text-ios-label text-xs font-medium hover:bg-[#F5F5F7] disabled:opacity-35 disabled:cursor-not-allowed transition-all shadow-xs"
      >
        下一页
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    total: number;
    pageSize?: number;
    currentPage?: number;
  }>(),
  {
    pageSize: 10,
    currentPage: 1
  }
);

defineEmits<{
  (e: 'update:page', page: number): void;
}>();

const totalPages = computed(() => {
  return Math.max(1, Math.ceil(props.total / props.pageSize));
});
</script>
