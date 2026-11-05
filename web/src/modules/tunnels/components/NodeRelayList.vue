<template>
  <div class="space-y-2">
    <div v-for="relay in relays" :key="relay.rule.id" class="text-xs text-ios-secondary space-y-1">
      <RouterLink to="/proxy/tunnel-rules" class="text-ios-blue hover:underline"><span class="font-mono tabular-nums">{{ relay.rule.entryNodeName }}:{{ relay.rule.listenPort }}</span> → 经 {{ relay.rule.tunnelName }} · {{ relay.rule.name }} · 编辑</RouterLink>
      <div class="flex items-center gap-2"><span>{{ relay.rule.subscriptionVisible ? '对用户可见' : '对用户隐藏' }}</span><PublishBadge :status="relay.rule.status" /></div>
    </div>
    <p v-if="error" class="text-xs text-ios-red">关联规则加载失败：{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import PublishBadge from './PublishBadge.vue';
import type { NodeRelay } from '../api';
defineProps<{ relays: NodeRelay[]; error?: string }>();
</script>
