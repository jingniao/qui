<template>
  <div class="mb-4 sm:mb-5 flex flex-col md:flex-row md:items-center justify-between gap-3">
    <div>
      <h1 class="text-lg sm:text-xl font-bold text-ios-label tracking-tight flex items-center gap-2.5">
        <span>{{ title }}</span>
        <slot name="badge"></slot>
      </h1>
      <p v-if="description" class="text-xs text-ios-secondary mt-1 max-w-2xl leading-normal font-normal">
        {{ description }}
      </p>
    </div>

    <!-- Right Header Actions -->
    <div class="flex items-center gap-2.5 shrink-0">
      <slot name="actions"></slot>
    </div>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title: string;
  parent?: string;
  description?: string;
}>();
</script>
