<template>
  <Teleport to="body">
    <Transition name="ios-modal">
      <div
        v-if="modelValue"
        class="fixed inset-0 z-50 bg-black/40 backdrop-blur-md flex items-center justify-center p-4 overflow-hidden"
        @click.self="handleClose"
      >
        <div
          :class="[
            'ios-modal-panel w-full bg-white rounded-3xl shadow-ios-lg border border-black/[0.08] overflow-hidden flex flex-col max-h-[90vh]',
            maxWidthClass
          ]"
        >
          <!-- Header -->
          <div class="px-6 py-[1.125rem] border-b border-black/[0.06] flex items-center justify-between bg-[#F5F5F7]">
            <div>
              <h3 class="text-lg font-semibold text-ios-label tracking-tight">{{ title }}</h3>
              <p v-if="description" class="text-xs text-ios-tertiary mt-1">{{ description }}</p>
            </div>
            <button
              @click="handleClose"
              class="w-7 h-7 rounded-full flex items-center justify-center bg-black/[0.05] text-ios-tertiary hover:text-ios-label hover:bg-black/[0.1] transition-colors"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          <!-- Body -->
          <div class="p-6 overflow-y-auto space-y-4">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="showFooter" class="px-6 py-3.5 border-t border-black/[0.06] bg-[#F5F5F7] flex items-center justify-end gap-2.5">
            <slot name="footer">
              <button
                type="button"
                @click="handleClose"
                class="ios-dialog-btn-secondary"
              >
                关闭
              </button>
              <button
                v-if="confirmText"
                type="button"
                :disabled="loading"
                @click="$emit('confirm')"
                class="ios-dialog-btn-primary"
              >
                {{ confirmText }}
              </button>
            </slot>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{
    modelValue: boolean;
    title: string;
    description?: string;
    maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl';
    confirmText?: string;
    loading?: boolean;
    showFooter?: boolean;
  }>(),
  {
    maxWidth: 'md',
    loading: false,
    showFooter: true
  }
);

const emit = defineEmits<{
  (e: 'update:modelValue', val: boolean): void;
  (e: 'confirm'): void;
  (e: 'close'): void;
}>();

const maxWidthClass = computed(() => {
  switch (props.maxWidth) {
    case 'sm': return 'max-w-sm';
    case 'lg': return 'max-w-lg';
    case 'xl': return 'max-w-xl';
    case '2xl': return 'max-w-2xl';
    case '3xl': return 'max-w-3xl';
    default: return 'max-w-md';
  }
});

function handleClose() {
  emit('update:modelValue', false);
  emit('close');
}
</script>
