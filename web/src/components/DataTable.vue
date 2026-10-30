<template>
  <div class="panel-card overflow-hidden">
    <div ref="viewport" class="overflow-x-auto" :class="{ 'dt-sticky-shadow': shadowed }" @scroll.passive="syncShadow">
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
import { onBeforeUnmount, onMounted, ref } from 'vue';
withDefaults(
  defineProps<{
    minWidth?: string;
  }>(),
  {
    minWidth: 'min-w-[960px]'
  }
);

// The last column stays pinned to the right edge of the scroll container so the
// 操作 / 结果 column is reachable without scrolling a wide table. The left shadow
// only appears while content is actually scrolled under the pinned column.
const viewport = ref<HTMLElement | null>(null);
const shadowed = ref(false);
let observer: ResizeObserver | undefined;

function syncShadow() {
  const element = viewport.value;
  shadowed.value = !!element && element.scrollLeft > 1;
}

onMounted(() => {
  syncShadow();
  if (typeof ResizeObserver !== 'undefined' && viewport.value) {
    observer = new ResizeObserver(syncShadow);
    observer.observe(viewport.value);
  }
});

onBeforeUnmount(() => {
  observer?.disconnect();
  observer = undefined;
});
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

/* Pinned last column: opaque background matching the header / row surface. */
:deep(th:last-child),
:deep(td:last-child) {
  position: sticky;
  right: 0;
  z-index: 1;
}

:deep(th:last-child) {
  background-color: #f5f5f7;
}

:deep(td:last-child) {
  background-color: #ffffff;
}

:deep(tbody tr:hover td:last-child) {
  background-color: #f9f9f9;
}

.dt-sticky-shadow :deep(th:last-child),
.dt-sticky-shadow :deep(td:last-child) {
  box-shadow: -6px 0 8px -6px rgba(0, 0, 0, 0.18);
}

:deep(tbody tr) {
  transition: background-color 0.15s ease;
}

:deep(tbody tr:hover) {
  background-color: rgba(0, 0, 0, 0.025);
}
</style>
