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
        <div class="w-full max-w-md bg-white/95 backdrop-blur-2xl rounded-3xl shadow-ios-lg border border-black/[0.08] p-6 space-y-4">
          <div class="flex items-start gap-4">
            <div
              :class="[
                'w-11 h-11 rounded-2xl flex items-center justify-center shrink-0 shadow-2xs',
                isDanger ? 'bg-[#FF3B30]/12 text-[#FF3B30]' : 'bg-[#FF9500]/12 text-[#FF9500]'
              ]"
            >
              <svg v-if="isDanger" class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              <svg v-else class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
            </div>
            <div class="flex-1">
              <h3 class="text-base font-bold text-[#1C1C1E] tracking-tight">{{ title }}</h3>
              <p class="text-xs text-[#8E8E93] mt-1 leading-relaxed">{{ message }}</p>
              <div v-if="warningDetails" class="mt-2.5 p-2.5 rounded-xl bg-[#FF3B30]/8 text-[#D70015] text-xs border border-[#FF3B30]/15">
                {{ warningDetails }}
              </div>
            </div>
          </div>

          <div class="flex items-center justify-end gap-2.5 pt-2">
            <button
              type="button"
              @click="handleClose"
              class="px-4 py-2 text-xs font-semibold rounded-full bg-black/[0.05] hover:bg-black/[0.08] text-[#1C1C1E] transition-all"
            >
              取消
            </button>
            <button
              type="button"
              @click="handleConfirm"
              :class="[
                'px-5 py-2 text-xs font-semibold rounded-full text-white transition-all shadow-xs',
                isDanger ? 'bg-[#FF3B30] hover:bg-[#D70015] shadow-[0_2px_8px_rgba(255,59,48,0.25)]' : 'bg-[#FF9500] hover:bg-[#D97D00] shadow-[0_2px_8px_rgba(255,149,0,0.25)]'
              ]"
            >
              {{ confirmText }}
            </button>
          </div>
        </div>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    modelValue: boolean;
    title: string;
    message: string;
    warningDetails?: string;
    confirmText?: string;
    isDanger?: boolean;
  }>(),
  {
    confirmText: '确定执行',
    isDanger: true
  }
);

const emit = defineEmits<{
  (e: 'update:modelValue', val: boolean): void;
  (e: 'confirm'): void;
}>();

function handleClose() {
  emit('update:modelValue', false);
}

function handleConfirm() {
  emit('confirm');
  emit('update:modelValue', false);
}
</script>
