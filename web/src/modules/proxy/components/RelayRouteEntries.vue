<template>
  <div v-for="entry in entries" :key="entry.id" class="flex items-center gap-1 mt-0.5 text-xs font-mono text-neutral-500">
    <span class="truncate max-w-[180px]" :title="entry.name">入口 {{ entry.server }}:{{ entry.port }}</span>
    <button type="button" class="text-neutral-400 hover:text-ios-blue shrink-0" title="复制入口地址" @click.stop="emit('copy', entry.server + ':' + entry.port)">复制</button>
  </div>
</template>
<script setup lang="ts">
import type { ProxyNodeItem } from '../proxyNodes';
defineProps<{ entries: ProxyNodeItem[] }>();
const emit = defineEmits<{ copy: [address: string] }>();
</script>
