<template>
  <span
    :class="[
      'inline-flex items-center py-0.5 text-xs font-mono tabular-nums font-semibold border transition-colors',
      compact ? 'px-1.5 rounded' : 'px-2 rounded-full',
      badgeClass
    ]"
  >
    <slot>{{ label || protocol }}</slot>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { PROTOCOL_BADGE_CLASSES, PROTOCOL_COLOR_ALIASES } from './protocolColors';

const props = defineProps<{
  protocol: string;
  label?: string;
  compact?: boolean;
}>();

const badgeClass = computed(() => {
  const protocol = props.protocol.toLowerCase();
  return PROTOCOL_BADGE_CLASSES[PROTOCOL_COLOR_ALIASES[protocol] || protocol]
    || 'bg-black/[0.04] text-ios-tertiary border-black/[0.08]';
});
</script>
