<template>
  <span v-if="node.relayCleanupPending" class="inline-flex min-h-6 items-center rounded-full bg-ios-orange/10 px-2 py-0.5 text-xs text-ios-orange shrink-0" :title="`中转撤除尚未在该节点生效，将自动重试${node.relayCleanupCode ? '；最近失败：' + node.relayCleanupCode : ''}`">待清理</span>
</template>
<script setup lang="ts">
import type { NodeView } from '../api';
defineProps<{ node: NodeView }>();
</script>
