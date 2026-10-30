<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="font-semibold text-ios-label">机器监控</h2>
      <div class="ios-segmented-container flex">
        <button v-for="range in ranges" :key="range" type="button" class="ios-segmented-item" :class="{ active: hours === range }" @click="emit('update:hours', range)">{{ range === 168 ? '7D' : range + 'H' }}</button>
      </div>
    </div>
    <p class="text-xs text-ios-secondary">仅显示打开本页后收到的心跳样本。所选范围超出本次采样窗口；持久化历史即将支持。</p>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <section v-for="metric in metrics" :key="metric.key" class="panel-card p-4 space-y-3">
        <div class="flex justify-between text-sm"><h3 class="font-semibold">{{ metric.label }}</h3><span class="font-mono">{{ display(metric.key, current[metric.key]) }}</span></div>
        <svg v-if="points.some(p => p[metric.key] !== null)" viewBox="0 0 320 100" class="w-full text-ios-blue" role="img" :aria-label="metric.label + '本次会话采样'">
          <path :d="path(metric.key)" fill="none" stroke="currentColor" stroke-width="2" />
          <circle v-for="p in chartPoints(metric.key)" :key="p.time" :cx="p.x" :cy="p.y" r="2" fill="currentColor" />
        </svg>
        <p v-else class="text-xs text-ios-tertiary py-6">— · 等待 Agent 心跳采样</p>
        <p class="text-xs text-ios-tertiary">{{ points.length ? formatTimeHMS(points[0].time) + ' – ' + formatTimeHMS(points[points.length - 1].time) : '尚无本次采样' }}</p>
      </section>
      <section v-for="label in ['负载', '进程数', 'TCP / UDP 连接分布', '内存 / 磁盘总量']" :key="label" class="panel-card p-4 space-y-2">
        <h3 class="text-sm font-semibold">{{ label }}</h3><p>—</p><p class="text-xs text-ios-tertiary">需要 Agent 上报</p>
      </section>
    </div>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { NodeView } from '../api';
import { sampleHeartbeat, extractCurrentValues, formatRate, formatTimeHMS, type SystemMetricPoint } from './serverMetricsModel';
const props = defineProps<{ node: NodeView; now: number; hours: number }>();
const emit = defineEmits<{ 'update:hours': [hours: number] }>();
const ranges = [1, 6, 12, 24, 168];
type MetricKey = Exclude<keyof SystemMetricPoint, 'time'>;
const metrics: { key: MetricKey; label: string }[] = [
  { key: 'cpu', label: 'CPU' }, { key: 'memory', label: '内存占用' }, { key: 'disk', label: '磁盘占用' },
  { key: 'rxRate', label: '接收速率' }, { key: 'txRate', label: '发送速率' }, { key: 'connections', label: '连接数' }
];
const history = ref<SystemMetricPoint[]>([]);
let lastHeartbeat = props.node.lastHeartbeat;
watch(() => props.node.id, () => { history.value = []; lastHeartbeat = props.node.lastHeartbeat; });
watch(() => props.node.lastHeartbeat, heartbeat => {
  if (heartbeat === lastHeartbeat) return;
  lastHeartbeat = heartbeat;
  const point = sampleHeartbeat(props.node, props.now);
  if (point) history.value = [...history.value, point].filter(p => p.time >= props.now - 168 * 3600000).slice(-50000);
});
const points = computed(() => history.value.filter(p => p.time >= props.now - props.hours * 3600000));
const current = computed(() => extractCurrentValues(props.node, props.now));
function display(key: MetricKey, value: number | null) {
  if (value === null) return '—';
  if (key === 'rxRate' || key === 'txRate') return formatRate(value);
  return ['cpu', 'memory', 'disk'].includes(key) ? value.toFixed(1) + '%' : String(value);
}
function chartPoints(key: MetricKey) {
  const list = points.value;
  const max = ['cpu', 'memory', 'disk'].includes(key) ? 100 : Math.max(1, ...list.map(p => p[key] ?? 0));
  const start = list[0]?.time ?? 0, span = Math.max(1, (list[list.length - 1]?.time ?? start) - start);
  return list.filter(p => p[key] !== null).map(p => ({ time: p.time, x: 10 + (p.time - start) / span * 300, y: 90 - (p[key] as number) / max * 80 }));
}
function path(key: MetricKey) {
  const coords = new Map(chartPoints(key).map(p => [p.time, p]));
  let connected = false;
  return points.value.map(p => { const c = coords.get(p.time); if (!c) { connected = false; return ''; } const command = connected ? 'L' : 'M'; connected = true; return command + c.x + ',' + c.y; }).join(' ');
}
</script>
