<template>
  <div class="panel-card overflow-hidden">
    <div class="overflow-x-auto">
      <table :class="['w-full text-left text-sm text-[#1C1C1E]', minWidth]">
        <thead class="bg-[#F8F8FA] border-b border-black/[0.05] text-xs font-semibold text-[#8E8E93] tracking-wider uppercase">
          <tr>
            <slot name="header"></slot>
          </tr>
        </thead>
        <tbody class="divide-y divide-black/[0.05]">
          <slot name="body"></slot>
        </tbody>
      </table>
    </div>

    <!-- Empty or loading fallback slot -->
    <div v-if="$slots.empty" class="p-12 text-center">
      <slot name="empty"></slot>
    </div>

    <!-- Table Footer / Pagination Area -->
    <div v-if="$slots.footer" class="px-6 py-4 bg-[#F8F8FA]/60 border-t border-black/[0.05]">
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
