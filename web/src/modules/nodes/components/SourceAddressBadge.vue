<template>
  <span v-if="alert" class="inline-flex min-h-6 items-center rounded-full px-2 py-0.5 text-xs shrink-0" :class="alert.failed ? 'bg-ios-orange/10 text-ios-orange' : 'bg-ios-red/10 text-ios-red'" :title="title">{{ alert.failed ? '出口 IP 检查失败' : '出口 IP 缺失' }}</span>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../api';
import { describeSourceIssue, sourceAddressAlert } from '../sourceAddress';
const props = defineProps<{ node: NodeView; statusKey: string }>();
const alert = computed(() => sourceAddressAlert(props.node, props.statusKey));
const title = computed(() => !alert.value ? '' : alert.value.failed
  ? 'Agent 无法检查出口 IP 是否仍在服务器上，详见 Agent 日志'
  : `以下地址已不在服务器上，使用它们的流量会失败：${alert.value.issues.map(describeSourceIssue).join('；')}`);
</script>
