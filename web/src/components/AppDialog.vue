<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-200 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-150 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="modelValue"
        class="fixed inset-0 z-50 bg-black/35 backdrop-blur-md flex items-center justify-center p-4 transition-all"
        @click.self="handleClose"
      >
        <div
          :class="[
            'w-full bg-white/95 backdrop-blur-2xl rounded-3xl shadow-ios-lg border border-black/[0.08] overflow-hidden flex flex-col max-h-[90vh]',
            maxWidthClass
          ]"
        >
          <!-- Header -->
          <div class="px-6 py-4.5 border-b border-black/[0.05] flex items-center justify-between bg-[#F8F8FA]/80">
            <div>
              <h3 class="text-base font-bold text-[#1C1C1E] tracking-tight">{{ title }}</h3>
              <p v-if="description" class="text-xs text-[#8E8E93] mt-0.5">{{ description }}</p>
            </div>
            <button
              @click="handleClose"
              class="w-7 h-7 rounded-full flex items-center justify-center bg-black/[0.05] text-[#8E8E93] hover:text-[#1C1C1E] hover:bg-black/[0.1] transition-colors"
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
          <div v-if="showFooter" class="px-6 py-4 border-t border-black/[0.05] bg-[#F8F8FA]/80 flex items-center justify-end gap-2.5">
            <slot name="footer">
              <button
                type="button"
                @click="handleClose"
                class="px-4 py-2 text-xs font-semibold rounded-full bg-black/[0.05] hover:bg-black/[0.08] text-[#1C1C1E] transition-all"
              >
                关闭
              </button>
              <button
                v-if="confirmText"
                type="button"
                :disabled="loading"
                @click="$emit('confirm')"
                class="px-5 py-2 text-xs font-semibold rounded-full text-white bg-[#007AFF] hover:bg-[#0071E3] disabled:opacity-40 transition-all shadow-[0_2px_8px_rgba(0,122,255,0.25)]"
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
