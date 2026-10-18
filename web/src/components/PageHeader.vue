<template>
  <div class="mb-7 flex flex-col md:flex-row md:items-center justify-between gap-4">
    <div>
      <div v-if="parent" class="flex items-center gap-2 text-xs font-medium text-[#8E8E93] mb-1">
        <span>{{ parent }}</span>
        <span class="text-[#C7C7CC]">/</span>
        <span class="text-[#007AFF] font-medium">{{ title }}</span>
      </div>
      <h1 class="text-2xl sm:text-[28px] font-bold text-[#1C1C1E] tracking-tight flex items-center gap-3">
        <span>{{ title }}</span>
        <slot name="badge"></slot>
      </h1>
      <p v-if="description" class="text-sm text-[#8E8E93] mt-1 max-w-3xl leading-relaxed">
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
