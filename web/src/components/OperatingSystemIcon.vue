<template>
  <svg
    class="w-5 h-5 shrink-0"
    :class="debian ? 'text-ios-red' : alpine ? 'text-ios-blue' : 'text-ios-secondary'"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.5"
    stroke-linecap="round"
    stroke-linejoin="round"
    role="img"
    :aria-label="name"
  >
    <title>{{ name }}</title>
    <path v-if="debian" d="M12.2 10.1c1.5-1.2 3.6-.2 3.5 1.6-.1 2.2-2.7 3.3-4.8 2.1-2.8-1.5-2.6-5.4.1-7.1 3.3-2.1 7.8-.1 8.1 3.8.4 4.9-4.5 8.2-8.9 6.2C5 14.3 4.8 7.7 9.3 4.6c4-2.8 9.2-1.4 11.1 2.3M11.6 17.3c-3.9.1-5.7 2-5 4" />
    <template v-else-if="alpine">
      <path d="m2 19 8-14 4.5 7L17 8l5 11H2Z" />
      <path d="m7.2 9.9 2.8 2.6 2.2-4.1M12.5 19l4.5-7 3 7" />
    </template>
    <template v-else>
      <rect x="3" y="4" width="18" height="13" rx="2" />
      <path d="M8 21h8m-4-4v4M7 9l2 2-2 2m5 0h4" />
    </template>
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{ name: string }>();
const debian = computed(() => /debian/i.test(props.name));
const alpine = computed(() => /alpine/i.test(props.name));
</script>
