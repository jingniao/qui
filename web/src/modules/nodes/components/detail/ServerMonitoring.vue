<template>
  <div class="space-y-6">
    <!-- 6 Trend Metric Cards: CPU, 内存, 磁盘, 网络, 连接, 进程 -->
    <ServerMetricsGrid :node="node" :now="now" v-model:hours="monitoringHours" />

    <!-- Latency & Network Quality Chart -->
    <LatencyChart :node-id="node.id" v-model:hours="monitoringHours" />
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import type { NodeView } from '../../api';
import ServerMetricsGrid from '../ServerMetricsGrid.vue';
import LatencyChart from '../LatencyChart.vue';

defineProps<{ node: NodeView; now: number }>();
const monitoringHours = ref(1);
</script>
