<template>
  <div class="mb-7 flex flex-col md:flex-row md:items-center justify-between gap-4">
    <div>
      <h1 class="text-2xl sm:text-[28px] font-bold text-ios-label tracking-tight flex items-center gap-3">
        <span>{{ title }}</span>
        <slot name="badge"></slot>
      </h1>
      <p v-if="description" class="text-sm text-ios-secondary mt-1.5 max-w-3xl leading-relaxed font-normal">
        {{ description }}
      </p>
    </div>

    <!-- Right Header Actions -->
    <div class="flex items-center gap-3 shrink-0">
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
