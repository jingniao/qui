<template>
  <Teleport to="body">
    <Transition name="ios-modal">
      <div
        v-if="modelValue"
        class="fixed inset-0 z-50 bg-black/40 backdrop-blur-md flex items-center justify-center p-3 sm:p-5 lg:p-6 overflow-hidden"
        @click.self="handleClose"
      >
        <div
          :class="[
            'ios-modal-panel w-full bg-white rounded-3xl shadow-ios-lg border border-black/[0.08] overflow-hidden flex flex-col',
            heightClass ? heightClass : 'max-h-[92vh]',
            resolvedMaxWidthClass,
            panelClass
          ]"
        >
          <!-- Header -->
          <div class="px-6 py-[1.125rem] border-b border-black/[0.06] flex items-center justify-between gap-4 bg-[#F5F5F7] shrink-0">
            <div class="min-w-0 flex-1">
              <h3 class="text-lg font-semibold text-ios-label tracking-tight truncate">{{ title }}</h3>
              <p v-if="description" class="text-xs text-ios-tertiary mt-1 truncate">{{ description }}</p>
            </div>
            <div class="flex items-center gap-2.5 shrink-0">
              <slot name="header-actions"></slot>
              <button
                v-if="!$slots['header-actions']"
                @click="handleClose"
                class="w-7 h-7 rounded-full flex items-center justify-center bg-black/[0.05] text-ios-tertiary hover:text-ios-label hover:bg-black/[0.1] transition-colors"
              >
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          </div>

          <!-- Body -->
          <div :class="['flex-1 min-h-0', bodyClass ? bodyClass : 'p-6 overflow-y-auto space-y-4']">
            <slot></slot>
          </div>

          <!-- Footer -->
          <div v-if="showFooter" class="px-6 py-3.5 border-t border-black/[0.06] bg-[#F5F5F7] flex items-center justify-end gap-2.5 shrink-0">
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
    maxWidth?: 'sm' | 'md' | 'lg' | 'xl' | '2xl' | '3xl' | '4xl' | '5xl' | '6xl' | '7xl' | 'full';
    maxWidthClass?: string;
    heightClass?: string;
    bodyClass?: string;
    panelClass?: string;
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

const resolvedMaxWidthClass = computed(() => {
  if (props.maxWidthClass) {
    return props.maxWidthClass;
  }
  switch (props.maxWidth) {
    case 'sm': return 'max-w-sm';
    case 'lg': return 'max-w-lg';
    case 'xl': return 'max-w-xl';
    case '2xl': return 'max-w-2xl';
    case '3xl': return 'max-w-3xl';
    case '4xl': return 'max-w-4xl';
    case '5xl': return 'max-w-5xl';
    case '6xl': return 'max-w-6xl';
    case '7xl': return 'max-w-7xl';
    case 'full': return 'max-w-[96vw]';
    default: return 'max-w-md';
  }
});

function handleClose() {
  emit('update:modelValue', false);
  emit('close');
}
</script>
