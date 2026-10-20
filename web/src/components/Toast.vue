<template>
  <div
    :class="[
      'flex items-start gap-3 p-4 rounded-2xl shadow-ios-lg border text-sm max-w-sm w-full transition-all duration-300 pointer-events-auto bg-white/95 backdrop-blur-xl',
      containerClass
    ]"
  >
    <div class="shrink-0 mt-0.5">
      <svg v-if="toast.type === 'success'" class="w-5 h-5 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
      <svg v-else-if="toast.type === 'error'" class="w-5 h-5 text-rose-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4m0 4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
      <svg v-else-if="toast.type === 'warning'" class="w-5 h-5 text-amber-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
      </svg>
      <svg v-else class="w-5 h-5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
      </svg>
    </div>
    <div class="flex-1">
      <div v-if="toast.title" class="font-semibold text-ios-label text-xs mb-0.5">{{ toast.title }}</div>
      <div class="text-xs text-ios-secondary leading-relaxed">{{ toast.message }}</div>
    </div>
    <button
      @click="$emit('close')"
      class="text-ios-tertiary hover:text-ios-label shrink-0 text-sm ml-1"
    >
      ×
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { ToastMessage } from '../app/api';

const props = defineProps<{
  toast: ToastMessage;
}>();

defineEmits<{
  (e: 'close'): void;
}>();

const containerClass = computed(() => {
  switch (props.toast.type) {
    case 'success':
      return 'border-emerald-200 text-ios-label';
    case 'error':
      return 'border-rose-200 text-ios-label';
    case 'warning':
      return 'border-amber-200 text-ios-label';
    default:
      return 'border-blue-200 text-ios-label';
  }
});
</script>
