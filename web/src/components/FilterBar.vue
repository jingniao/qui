<template>
  <div class="panel-card p-4 sm:p-5 mb-6 flex flex-col md:flex-row md:items-center justify-between gap-4">
    <!-- Left: Filters & Search -->
    <div class="flex flex-wrap items-center gap-3 flex-1">
      <slot name="filters"></slot>
    </div>

    <!-- Right: Actions (Add, Export, Refresh) -->
    <div class="flex items-center gap-3 self-end md:self-auto shrink-0">
      <slot name="actions"></slot>
    </div>
  </div>
</template>

<script setup lang="ts">
// Common filter bar layout with enhanced padding and card background
</script>
