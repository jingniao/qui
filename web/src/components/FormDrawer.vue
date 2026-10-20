<template>
  <Teleport to="body">
    <Transition name="ios-modal">
      <div
        v-if="modelValue"
        class="fixed inset-0 z-50 bg-black/45 backdrop-blur-md flex items-center justify-center p-6 sm:p-8 lg:p-12 overflow-hidden"
        @click.self="handleClose"
      >
        <div
          :class="[
            'ios-modal-panel w-full bg-white rounded-3xl shadow-ios-lg border border-black/[0.08] overflow-hidden flex flex-col max-h-[85vh] my-auto',
            widthClass
          ]"
        >
          <!-- Modal Header (Symmetrical with Footer) -->
          <div class="px-7 py-[1.125rem] min-h-[4rem] border-b border-black/[0.08] flex items-center justify-between bg-[#F5F5F7] shrink-0">
            <div>
              <h3 class="text-lg font-semibold text-ios-label tracking-tight leading-none">{{ title }}</h3>
              <p v-if="description" class="text-xs text-ios-tertiary mt-1.5 leading-normal">{{ description }}</p>
            </div>
            <button
              type="button"
              @click="handleClose"
              class="w-[1.875rem] h-[1.875rem] rounded-full flex items-center justify-center bg-black/[0.05] text-ios-tertiary hover:text-ios-label hover:bg-black/[0.1] transition-colors"
              title="关闭"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>

          <!-- Modal Body (Generous & Symmetrical Padding) -->
          <div class="flex-1 overflow-y-auto px-7 py-5 space-y-[1.125rem] scrollbar-thin">
            <slot></slot>
          </div>

          <!-- Modal Footer (Symmetrical with Header) -->
          <div v-if="showFooter" class="px-7 py-4 min-h-[3.75rem] border-t border-black/[0.08] bg-[#F5F5F7] flex items-center justify-end gap-3 shrink-0">
            <slot name="footer">
              <button
                type="button"
                @click="handleClose"
                class="ios-dialog-btn-secondary"
              >
                取消
              </button>
              <button
                type="button"
                :disabled="loading"
                @click="$emit('confirm')"
                class="ios-dialog-btn-primary"
              >
                <svg v-if="loading" class="animate-spin -ml-0.5 h-3.5 w-3.5 text-white" fill="none" viewBox="0 0 24 24">
                  <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
                  <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                </svg>
                <span>{{ confirmText }}</span>
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
    width?: 'md' | 'lg' | 'xl' | '2xl';
    confirmText?: string;
    loading?: boolean;
    showFooter?: boolean;
  }>(),
  {
    width: 'md',
    confirmText: '保存并应用',
    loading: false,
    showFooter: true
  }
);

const emit = defineEmits<{
  (e: 'update:modelValue', val: boolean): void;
  (e: 'confirm'): void;
  (e: 'close'): void;
}>();

const widthClass = computed(() => {
  switch (props.width) {
    case 'lg':
      return 'max-w-lg';
    case 'xl':
      return 'max-w-xl';
    case '2xl':
      return 'max-w-2xl';
    default:
      return 'max-w-md';
  }
});

function handleClose() {
  emit('update:modelValue', false);
  emit('close');
}
</script>
