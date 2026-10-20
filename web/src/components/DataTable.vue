<template>
  <div class="panel-card overflow-hidden">
    <div class="overflow-x-auto">
      <table :class="['w-full text-left text-sm text-ios-label', minWidth]">
        <thead class="bg-[#F5F5F7] border-b border-black/[0.08] text-xs font-medium text-ios-tertiary tracking-wider uppercase">
          <tr>
            <slot name="header"></slot>
          </tr>
        </thead>
        <tbody class="divide-y divide-black/[0.06]">
          <slot name="body"></slot>
        </tbody>
      </table>
    </div>

    <!-- Empty or loading fallback slot -->
    <div v-if="$slots.empty" class="p-12 text-center">
      <slot name="empty"></slot>
    </div>

    <!-- Table Footer / Pagination Area -->
    <div v-if="$slots.footer" class="px-6 py-3.5 bg-[#F5F5F7]/80 border-t border-black/[0.06]">
      <slot name="footer"></slot>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(
  defineProps<{
    minWidth?: string;
  }>(),
  {
    minWidth: 'min-w-[960px]'
  }
);
</script>

<style scoped>
:deep(th) {
  padding: 0.75rem 1.25rem;
  font-weight: 500;
  white-space: nowrap;
  letter-spacing: 0.03em;
}

:deep(td) {
  padding: 0.85rem 1.25rem;
  vertical-align: middle;
}

:deep(tbody tr) {
  transition: background-color 0.15s ease;
}

:deep(tbody tr:hover) {
  background-color: rgba(0, 0, 0, 0.025);
}
</style>
