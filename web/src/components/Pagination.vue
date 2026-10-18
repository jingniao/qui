<template>
  <div class="flex items-center justify-between text-xs text-[#8E8E93]">
    <div>
      共 <span class="font-semibold text-[#1C1C1E]">{{ total }}</span> 条记录
      <span v-if="totalPages > 1" class="ml-2">第 {{ currentPage }} / {{ totalPages }} 页</span>
    </div>
    <div class="flex items-center gap-1.5">
      <button
        :disabled="currentPage <= 1"
        @click="$emit('update:page', currentPage - 1)"
        class="px-3 py-1 rounded-full border border-black/[0.08] bg-white text-[#1C1C1E] font-medium hover:bg-black/[0.03] disabled:opacity-30 disabled:cursor-not-allowed transition-all shadow-2xs"
      >
        上一页
      </button>
      <button
        :disabled="currentPage >= totalPages"
        @click="$emit('update:page', currentPage + 1)"
        class="px-3 py-1 rounded-full border border-black/[0.08] bg-white text-[#1C1C1E] font-medium hover:bg-black/[0.03] disabled:opacity-30 disabled:cursor-not-allowed transition-all shadow-2xs"
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
