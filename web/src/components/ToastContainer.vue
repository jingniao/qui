<template>
  <div class="fixed top-5 right-5 z-50 flex flex-col gap-2.5 pointer-events-none">
    <TransitionGroup
      enter-active-class="transition duration-300 ease-out"
      enter-from-class="opacity-0 translate-y-2 scale-95"
      enter-to-class="opacity-100 translate-y-0 scale-100"
      leave-active-class="transition duration-200 ease-in"
      leave-from-class="opacity-100 translate-y-0 scale-100"
      leave-to-class="opacity-0 -translate-y-2 scale-95"
    >
      <Toast
        v-for="item in toastMessages"
        :key="item.id"
        :toast="item"
        @close="removeToast(item.id)"
      />
    </TransitionGroup>
  </div>
</template>

<script setup lang="ts">
import { toastMessages } from '../app/api';
import Toast from './Toast.vue';

function removeToast(id: string) {
  const idx = toastMessages.findIndex(t => t.id === id);
  if (idx !== -1) toastMessages.splice(idx, 1);
}
</script>
