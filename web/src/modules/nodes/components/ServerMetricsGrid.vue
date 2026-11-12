<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <h2 class="text-ios-label text-base font-semibold leading-normal">机器监控</h2>
      <div class="ios-segmented-container flex">
        <button v-for="range in ranges" :key="range" type="button" class="ios-segmented-item" :class="{ active: hours === range }" @click="emit('update:hours', range)">{{ range === 168 ? '7D' : range + 'H' }}</button>
      </div>
    </div>
    <p class="text-xs text-ios-secondary">服务端历史 · 每分钟采样 · 保留 7 天 · 长范围为分桶平均值。网络速率为主机网卡流量，可能包含多接口重复计数；缺测处断线，悬停可读各时刻数值。</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }} <button class="ios-btn-secondary" @click="load(true)">重试</button></p>
    <p v-if="loading" class="text-xs text-ios-secondary">正在读取历史指标…</p>
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
      <section v-for="card in cards" :key="card.id" class="panel-card p-4 space-y-3">
        <div class="flex justify-between text-sm gap-2"><h3 class="text-ios-label text-base font-semibold leading-normal">{{ card.title }}</h3><span class="font-mono text-ios-label">{{ headline(card) }}</span></div>
        <MetricLineChart v-if="card.hasData" :label="card.title + '历史采样'" :datasets="card.datasets" :axes="card.axes" :x-min="start" :x-max="end" />
        <p v-else class="text-xs text-ios-tertiary py-6">— · 暂无历史测量</p>
        <p v-if="current[card.primary] === null && (currentUnavailable || latestMeasured(card.primary) === null)" class="text-xs text-ios-secondary">{{ currentUnavailable ? '服务器离线，当前值不可用' : '暂无读数' }}</p>
        <p class="flex flex-wrap items-center justify-between gap-x-3 gap-y-0.5 text-xs text-ios-tertiary">
          <span class="tabular-nums" :title="`${formatTime(rangeStartIso)} – ${formatTime(rangeEndIso)}`">{{ rangeLabel }}</span>
          <span v-if="card.footerKey" class="tabular-nums">{{ metricRangeLabel(points, card.footerKey) }}</span>
        </p>
      </section>
    </div>
  </div>
</template>
<script setup lang="ts">
import { formatTime } from '../../../app/auth';
import { computed, defineAsyncComponent, ref, watch, onMounted, onUnmounted } from 'vue';
import { nodeAPI, type NodeView } from '../api';
import { beginMetricsLoad, failMetricsLoad } from './metricsRefreshPolicy';
import { metricPoint, extractCurrentValues, formatBytes, formatMetricRange, formatMetricValue, metricCards, metricRangeLabel, seriesData, visibleSeries, CHART_COLORS, type MetricCard, type MetricKey, type SystemMetricPoint } from './serverMetricsModel';
// Chart.js stays out of the entry bundle: it loads with the first chart.
const MetricLineChart = defineAsyncComponent(() => import('./MetricLineChart.vue'));
const props = defineProps<{ node: NodeView; now: number; hours: number }>();
const emit = defineEmits<{ 'update:hours': [hours: number] }>();
const ranges = [1, 6, 12, 24, 168];
const points = ref<SystemMetricPoint[]>([]), intervalSeconds = ref(60), loading = ref(false), error = ref('');
const end = ref(Date.now());
const start = computed(() => end.value - props.hours * 3600000);
const rangeStartIso = computed(() => new Date(start.value).toISOString());
const rangeEndIso = computed(() => new Date(end.value).toISOString());
const rangeLabel = computed(() => formatMetricRange(start.value, end.value));
const currentUnavailable = computed(() => {
  const age = props.now - Date.parse(props.node.lastHeartbeat);
  return !['online', 'degraded'].includes(props.node.status) || !Number.isFinite(age) || age < -5000 || age >= 45000;
});
const current = computed(() => extractCurrentValues(currentUnavailable.value ? { ...props.node, lastHeartbeat: '' } : props.node, props.now));
let generation = 0, disposed = false;
// Foreground loads (first read, node or range switch) clear the charts and show the
// hint; background refreshes keep the old data until the new data arrives.
async function load(foreground = false) {
  const ticket = ++generation, id = props.node.id, hours = props.hours;
  const begun = beginMetricsLoad({ points: points.value, loading: loading.value, error: error.value }, foreground);
  if (foreground) points.value = [];
  loading.value = begun.loading; error.value = begun.error;
  const until = Date.now();
  try {
    const response = await nodeAPI.metrics(id, hours);
    if (disposed || ticket !== generation) return;
    end.value = until; intervalSeconds.value = response.intervalSeconds;
    points.value = response.points.map(metricPoint).filter(p => Number.isFinite(p.time)).sort((a,b) => a.time-b.time);
  } catch (e) { if (!disposed && ticket === generation) { const failed = failMetricsLoad({ points: points.value, loading: loading.value, error: error.value }, '历史指标读取失败：' + (e as Error).message); error.value = failed.error; } }
  finally { if (!disposed && ticket === generation) loading.value = false; }
}
// History is sampled once a minute, so heartbeat events must not trigger reads; only the timer does.
watch([() => props.node.id, () => props.hours], () => { void load(true); }, { immediate: true });
let timer: ReturnType<typeof setInterval>;
onMounted(() => { timer = setInterval(() => { void load(false); }, 60000); });
onUnmounted(() => { disposed = true; generation++; clearInterval(timer); });
function display(key: MetricKey, value: number | null | undefined) {
  if (value === null || value === undefined) {
    const latest = latestMeasured(key);
    if (latest !== null) return formatMetricValue(key, latest);
  }
  return formatMetricValue(key, value ?? null);
}
function latestMeasured(key: MetricKey): number | null {
  const valid = points.value.filter(p => p[key] !== null && p[key] !== undefined);
  return valid.length ? valid[valid.length - 1][key]! : null;
}
const cards = computed(() => metricCards.map(card => {
  const series = visibleSeries(card, points.value);
  const datasets = series.map(sr => ({ label: sr.label, color: CHART_COLORS[sr.color], fill: sr.fill === true, axis: sr.axis ?? 'y' as const,
    data: seriesData(points.value, sr.key, intervalSeconds.value), format: (v: number) => formatMetricValue(sr.key, v) }));
  const axes = { y: card.axes.y, ...(card.axes.y1 && series.some(sr => sr.axis === 'y1') ? { y1: card.axes.y1 } : {}) };
  return { ...card, datasets, axes, primary: card.series[0].key, hasData: series.some(sr => latestMeasured(sr.key) !== null) };
}));
function total(key: 'memoryTotalBytes' | 'diskTotalBytes') {
  const bytes = Number(props.node.capabilities?.host?.[key]);
  return Number.isFinite(bytes) && bytes > 0 ? ' · ' + formatBytes(bytes) : '';
}
function headline(card: MetricCard) {
  const show = (key: MetricKey) => display(key, current.value[key]);
  switch (card.id) {
    case 'memory': return show('memoryBytes') + total('memoryTotalBytes');
    case 'disk': return show('diskBytes') + total('diskTotalBytes');
    case 'network': return `↑ ${show('txRate')} ↓ ${show('rxRate')}`;
    case 'connections': return `TCP ${show('tcp')} · UDP ${show('udp')}`;
    default: return show(card.series[0].key);
  }
}
</script>
