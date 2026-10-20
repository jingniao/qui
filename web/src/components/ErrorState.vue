<template>
  <div class="flex flex-col items-center justify-center p-8 text-center bg-rose-50/50 rounded-2xl border border-rose-200/80 my-4">
    <div class="w-11 h-11 rounded-full bg-rose-100 text-rose-600 flex items-center justify-center mb-3">
      <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
      </svg>
    </div>
    <h3 class="text-sm font-semibold text-rose-900 mb-1">{{ title }}</h3>
    <p class="text-xs text-rose-700/80 max-w-sm mb-4 leading-relaxed">{{ message }}</p>
    <button
      v-if="showRetry"
      @click="$emit('retry')"
      class="px-4 py-1.5 text-xs font-semibold rounded-full bg-rose-600 text-white hover:bg-rose-700 shadow-xs transition-colors"
    >
      重试请求
    </button>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    title?: string;
    message?: string;
    showRetry?: boolean;
  }>(),
  {
    title: '请求数据失败',
    message: '与服务器通信超时或节点响应异常，请检查网络后重试。',
    showRetry: true
  }
);

defineEmits<{
  (e: 'retry'): void;
}>();
</script>
