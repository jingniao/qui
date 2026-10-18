<template>
  <Teleport to="body">
    <Transition
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="opacity-0"
      enter-to-class="opacity-100"
      leave-active-class="transition duration-200 ease-in"
      leave-from-class="opacity-100"
      leave-to-class="opacity-0"
    >
      <div
        v-if="modelValue"
        class="fixed inset-0 z-50 bg-black/30 backdrop-blur-md flex justify-end transition-all"
        @click.self="handleClose"
      >
        <Transition
          enter-active-class="transform transition ease-out duration-300"
          enter-from-class="translate-x-full"
          enter-to-class="translate-x-0"
          leave-active-class="transform transition ease-in duration-200"
          leave-from-class="translate-x-0"
          leave-to-class="translate-x-full"
        >
          <div
            v-if="modelValue"
            :class="[
              'w-full bg-[#F8F8FA]/98 backdrop-blur-2xl h-full shadow-2xl flex flex-col border-l border-black/[0.08]',
              widthClass
            ]"
          >
            <!-- Drawer Header -->
            <div class="px-6 py-4.5 border-b border-black/[0.05] flex items-center justify-between bg-white/70 backdrop-blur-xl">
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

            <!-- Drawer Body -->
            <div class="flex-1 overflow-y-auto p-6 space-y-5 bg-[#F8F8FA]">
              <slot></slot>
            </div>

            <!-- Drawer Footer -->
            <div v-if="showFooter" class="px-6 py-4 border-t border-black/[0.05] bg-white/70 backdrop-blur-xl flex items-center justify-end gap-3">
              <slot name="footer">
                <button
                  type="button"
                  @click="handleClose"
                  class="px-4 py-2 text-xs font-semibold rounded-full bg-black/[0.05] hover:bg-black/[0.08] text-[#1C1C1E] transition-all"
                >
                  取消
                </button>
                <button
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
        </Transition>
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
