<template>
  <div class="mb-7 flex flex-col md:flex-row md:items-center justify-between gap-4">
    <div>
      <div v-if="parent" class="flex items-center gap-1.5 text-xs font-medium text-ios-tertiary mb-1">
        <span>{{ parent }}</span>
        <span class="text-ios-quaternary">/</span>
        <span class="text-ios-blue font-medium">{{ title }}</span>
      </div>
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
