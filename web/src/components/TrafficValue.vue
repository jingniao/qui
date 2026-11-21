<template>
  <span class="tabular-nums text-ios-label tracking-normal inline-flex items-baseline">
    <span :class="valueClass">{{ formatted.value }}</span>
    <span v-if="formatted.unit" class="text-xs text-ios-tertiary ml-1 font-normal">{{ formatted.unit }}</span>
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { byteParts } from '../app/bytes';

const props = withDefaults(
  defineProps<{
    bytes: number | null | undefined;
    valueClass?: string;
  }>(),
  {
    valueClass: 'font-medium'
  }
);

const formatted = computed(() => byteParts(props.bytes, 2));
</script>
