<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="font-semibold text-ios-label">机器监控</h2>
      <div class="ios-segmented-container flex">
        <button v-for="range in ranges" :key="range" type="button" class="ios-segmented-item" :class="{ active: hours === range }" @click="emit('update:hours', range)">{{ range === 168 ? '7D' : range + 'H' }}</button>
      </div>
    </div>
    <p class="text-xs text-ios-secondary">服务端历史 · 每分钟采样 · 保留 7 天 · 长范围为分桶平均值。网络速率为主机网卡流量，可能包含多接口重复计数；缺测处断线。</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }} <button class="ios-btn-secondary" @click="load">重试</button></p>
    <p v-if="loading" class="text-xs text-ios-secondary">正在读取历史指标…</p>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <section v-for="metric in metrics" :key="metric.key" class="panel-card p-4 space-y-3">
        <div class="flex justify-between text-sm gap-2"><h3 class="font-semibold text-ios-label">{{ metric.label }}</h3><span class="font-mono text-ios-label">{{ display(metric.key, current[metric.key]) }}</span></div>
        <svg v-if="points.some(p => p[metric.key] !== null)" viewBox="0 0 320 100" class="w-full text-ios-blue" role="img" :aria-label="metric.label + '历史采样'">
          <path :d="metricPath(points, metric.key, start, end, intervalSeconds)" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
          <circle v-for="p in dots(metric.key)" :key="p.time" :cx="p.x" :cy="p.y" r="1.5" fill="currentColor" />
        </svg>
        <p v-else class="text-xs text-ios-tertiary py-6">— · 暂无历史测量</p>
        <p v-if="current[metric.key] === null" class="text-xs text-ios-secondary">{{ currentUnavailable ? '服务器离线，当前值不可用' : '需要新版 Agent 上报' }}</p>
        <p class="text-xs text-ios-tertiary">{{ formatTime(new Date(start).toISOString()) }} – {{ formatTime(new Date(end).toISOString()) }}</p>
      </section>
    </div>
  </div>
</template>
<script setup lang="ts">
import { formatTime } from '../../../app/auth';
import { computed, ref, watch, onMounted, onUnmounted } from 'vue';
import { nodeAPI, type NodeView } from '../api';
import { metricPoint, metricPath, extractCurrentValues, formatRate, type MetricKey, type SystemMetricPoint } from './serverMetricsModel';
const props = defineProps<{ node: NodeView; now: number; hours: number }>();
const emit = defineEmits<{ 'update:hours': [hours: number] }>();
const ranges = [1, 6, 12, 24, 168];
const metrics: { key: MetricKey; label: string }[] = [
  {key:'cpu',label:'CPU'}, {key:'memory',label:'内存占用'}, {key:'disk',label:'磁盘占用'},
  {key:'rxRate',label:'接收速率'}, {key:'txRate',label:'发送速率'}, {key:'connections',label:'代理活跃会话'},
  {key:'tcp',label:'TCP 连接（全部状态）'}, {key:'udp',label:'UDP 套接字'}, {key:'processes',label:'进程数'}, {key:'load',label:'1 分钟负载'}
];
const points = ref<SystemMetricPoint[]>([]), intervalSeconds = ref(60), loading = ref(false), error = ref('');
const end = ref(Date.now());
const start = computed(() => end.value - props.hours * 3600000);
const currentUnavailable = computed(() => {
  const age = props.now - Date.parse(props.node.lastHeartbeat);
  return !['online', 'degraded'].includes(props.node.status) || !Number.isFinite(age) || age < -5000 || age >= 45000;
});
const current = computed(() => extractCurrentValues(currentUnavailable.value ? { ...props.node, lastHeartbeat: '' } : props.node, props.now));
let generation = 0, disposed = false;
async function load() {
  const ticket = ++generation, id = props.node.id, hours = props.hours;
  loading.value = true; error.value = '';
  const until = Date.now();
  try {
    const response = await nodeAPI.metrics(id, hours);
    if (disposed || ticket !== generation) return;
    end.value = until; intervalSeconds.value = response.intervalSeconds;
    points.value = response.points.map(metricPoint).filter(p => Number.isFinite(p.time)).sort((a,b) => a.time-b.time);
  } catch (e) { if (!disposed && ticket === generation) error.value = '历史指标读取失败：' + (e as Error).message; }
  finally { if (!disposed && ticket === generation) loading.value = false; }
}
watch(() => [props.node.id, props.hours], () => { points.value = []; void load(); }, {immediate:true});
let timer: ReturnType<typeof setInterval>;
onMounted(() => { timer = setInterval(load, 60000); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { disposed = true; generation++; clearInterval(timer); window.removeEventListener('proxy-node-status', load); });
function display(key: MetricKey, value: number | null) {
  if (value === null) return '—';
  if (key === 'rxRate' || key === 'txRate') return formatRate(value);
  return ['cpu','memory','disk'].includes(key) ? value.toFixed(1) + '%' : String(value);
}
function dots(key: MetricKey) {
  const max = ['cpu','memory','disk'].includes(key) ? 100 : Math.max(1,...points.value.map(p => p[key] ?? 0));
  return points.value.filter(p => p[key] !== null).map(p => ({time:p.time,x:10+(p.time-start.value)/Math.max(1,end.value-start.value)*300,y:90-p[key]!/max*80}));
}
</script>
