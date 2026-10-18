<template>
  <div class="relative rounded-xl overflow-hidden border border-slate-800 bg-slate-950 font-mono text-xs text-slate-200">
    <div class="flex items-center justify-between px-4 py-2 bg-slate-900 border-b border-slate-800 text-[11px] text-slate-400">
      <span>{{ title || '配置内容' }}</span>
      <button
        @click="copyCode"
        type="button"
        class="inline-flex items-center gap-1 text-slate-300 hover:text-white transition-colors cursor-pointer"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
        </svg>
        <span>{{ copied ? '已复制！' : '复制' }}</span>
      </button>
    </div>
    <div class="p-4 overflow-x-auto max-h-96 leading-relaxed selection:bg-indigo-500 selection:text-white">
      <pre>{{ code }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';

const props = defineProps<{
  code: string;
  title?: string;
}>();

const copied = ref(false);

async function copyCode() {
  try {
    await navigator.clipboard.writeText(props.code);
    copied.value = true;
    setTimeout(() => {
      copied.value = false;
    }, 2000);
  } catch (e) {
    console.error('Failed to copy', e);
  }
}
</script>
