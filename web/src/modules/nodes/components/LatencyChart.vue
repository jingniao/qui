<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-blue-500 animate-pulse"></span>
        <h2 class="text-ios-label text-base font-semibold leading-normal">网络质量与延迟</h2>
        <span class="text-xs text-ios-tertiary">全国多线 TCP 建连耗时 (非 ICMP ping) · 最近 {{ rangeLabel }} 趋势，时间范围随上方机器监控</span>
      </div>
      <div class="flex items-center gap-2 text-xs">
        <button v-if="data.targets.length && hidden.length" type="button" class="text-emerald-600 hover:text-emerald-700 font-medium cursor-pointer select-none" @click="selectAll">全选</button>
        <button v-if="data.targets.length && hidden.length < data.targets.length" type="button" class="text-ios-secondary hover:text-ios-label font-medium cursor-pointer select-none" @click="deselectAll">全不选</button>
        <button v-if="isOwner" type="button" class="ios-action-btn-secondary text-xs ml-1" @click="showDrawer = true">⚙ 检测目标</button>
      </div>
    </div>

    <div v-if="unsupported" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">面板版本不支持定时检测</div>
    <template v-else>
      <div v-if="error" class="panel-card p-4 bg-white flex items-center justify-between gap-2 shadow-2xs">
        <p role="alert" class="text-sm text-ios-red">{{ error }}</p>
        <button class="ios-btn-secondary" @click="load">重试</button>
      </div>
      <div v-if="loading && !hasPoints" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">正在读取延迟数据…</div>
      <div v-else-if="!hasPoints && !error" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">尚无定时检测数据</div>

      <template v-if="hasPoints">
        <div v-if="meansUnavailable" class="panel-card p-4 bg-white text-sm text-ios-secondary shadow-2xs">当前样本未提供耗时，暂无法绘制延迟曲线。</div>

        <div v-if="data.targets.length" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          <div
            v-for="(target, index) in data.targets.slice(0, 5)"
            :key="target.id"
            class="panel-card p-4 space-y-2 transition-all cursor-pointer select-none"
            :class="hidden.includes(target.id) ? 'opacity-40 border-dashed' : 'hover:border-neutral-300'"
            role="button"
            tabindex="0"
            :aria-pressed="!hidden.includes(target.id)"
            @click="toggle(target.id)"
            @keydown.enter.prevent="toggle(target.id)"
            @keydown.space.prevent="toggle(target.id)"
          >
            <div class="flex justify-between gap-2">
              <h3 class="flex items-center gap-1.5 text-ios-label text-base font-semibold leading-normal truncate" :title="target.label">
                <span class="w-1.5 h-3.5 rounded-full inline-block shrink-0" :class="targetColor(index).bar"></span>
                <span class="truncate">{{ target.label }}</span>
              </h3>
              <span class="text-ios-tertiary text-xs cursor-help" :title="`${target.host}:${target.port}`" @click.stop>ⓘ</span>
            </div>
            <p class="text-xs text-ios-tertiary">平均延迟 · 丢包率 · 抖动</p>
            <p class="text-sm font-mono text-ios-label tabular-nums">{{ formatCardStats(target.id) }}</p>
          </div>
        </div>

        <section class="panel-card p-4 space-y-3">
          <div class="flex items-center gap-2 text-xs">
            <button v-for="opt in toggles" :key="opt.key" type="button" class="px-3 py-1 rounded-md text-xs font-medium transition-all"
              :class="options[opt.key] ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold' : 'bg-white text-ios-secondary border border-ios-separator hover:text-ios-label'"
              @click="options[opt.key] = !options[opt.key]">{{ opt.label }}</button>
            <span class="text-ios-tertiary cursor-help ml-1 text-xs" title="丢包为虚线，对应右侧纵轴（丢包率 %）。平滑峰值：使用 5 点滚动中位数过滤单次异常突刺，目标卡片始终展示原始统计数据。">ⓘ</span>
          </div>
          <MetricLineChart v-if="datasets.length" label="网络质量与延迟历史采样" :datasets="datasets" :axes="axes" :x-min="start" :x-max="end" height-class="h-56 lg:h-72 2xl:h-80" />
          <p v-else class="text-xs text-ios-tertiary py-6">未选择目标或指标</p>
        </section>
      </template>
    </template>

    <LatencyTargetDrawer v-if="isOwner" v-model="showDrawer" :node-id="props.nodeId" @saved="load" />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onUnmounted, reactive, ref, watch } from 'vue';
import { nodeAPI, type LatencyPoint, type LatencyResponse } from '../api';
import { APIError } from '../../../app/apiClient';
import { isOwner } from '../../../app/auth';
import { formatStatsJitter, formatStatsLoss, formatStatsMean, seriesStats, usable } from './latencySeries';
import { buildLatencyDatasets, latencyAxes, targetColor } from './latencyChartModel';
import LatencyTargetDrawer from './LatencyTargetDrawer.vue';

// Chart.js stays out of the entry bundle: it loads with the first chart.
const MetricLineChart = defineAsyncComponent(() => import('./MetricLineChart.vue'));

// The time range is owned by the machine-monitoring segmented control above.
const props = defineProps<{ nodeId: string; hours: number }>();

const toggles = [
  { key: 'showLatency', label: '延迟' },
  { key: 'showLoss', label: '丢包' },
  { key: 'showSmooth', label: '平滑峰值' }
] as const;
const options = reactive({ showLatency: true, showLoss: true, showSmooth: false });
const showDrawer = ref(false);

const data = ref<LatencyResponse>({ intervalSeconds: 60, sampleSeconds: 60, targets: [], series: [] });
const hidden = ref<string[]>([]);
const error = ref('');
const unsupported = ref(false);
const loading = ref(false);

const end = ref(Date.now());
const start = computed(() => end.value - props.hours * 3600000);
const rangeLabel = computed(() => (props.hours === 168 ? '7D' : `${props.hours}H`));

function rawPoints(targetId: string): LatencyPoint[] {
  return data.value.series.find(item => item.targetId === targetId)?.points ?? [];
}

// Card statistics always use the raw samples, never the smoothed display line.
const targetStatsMap = computed(() => {
  const map: Record<string, ReturnType<typeof seriesStats>> = {};
  for (const t of data.value.targets) map[t.id] = seriesStats(rawPoints(t.id));
  return map;
});

function formatCardStats(targetId: string): string {
  const stats = targetStatsMap.value[targetId];
  if (!stats) return '—';
  return `${formatStatsMean(stats.meanMs)} · ${formatStatsLoss(stats.lossPercent)} · ${formatStatsJitter(stats.jitterMs)}`;
}

const datasets = computed(() => {
  const shown = data.value.targets.slice(0, 5)
    .map((target, colorIndex) => ({ target, colorIndex, points: rawPoints(target.id) }))
    // A target without samples in the range adds only empty legend entries.
    .filter(item => item.points.length > 0 && !hidden.value.includes(item.target.id));
  return buildLatencyDatasets(shown, data.value.intervalSeconds, options);
});
const axes = computed(() => latencyAxes(options));

const hasPoints = computed(() => data.value.series.some(s => s.points.length > 0));
const meansUnavailable = computed(() =>
  hasPoints.value && data.value.series.some(s => s.points.some(p => p.avgMs === null && p.medianMs === null && usable(p)))
);

function selectAll() { hidden.value = []; }
function deselectAll() { hidden.value = data.value.targets.map(t => t.id); }
function toggle(id: string) {
  hidden.value = hidden.value.includes(id) ? hidden.value.filter(item => item !== id) : [...hidden.value, id];
}

let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  const until = Date.now();
  try {
    const res = await nodeAPI.latency(props.nodeId, props.hours);
    if (current !== generation) return;
    data.value = res;
    end.value = until;
    unsupported.value = false;
  } catch (err) {
    if (current !== generation) return;
    if (err instanceof APIError && (err.status === 404 || err.code === 'not_found')) {
      unsupported.value = true;
    } else {
      error.value = (err as Error).message || '读取延迟数据失败';
    }
  } finally {
    if (current === generation) loading.value = false;
  }
}

watch(() => [props.nodeId, props.hours], () => { void load(); }, { immediate: true });

const timer = setInterval(() => { if (!loading.value) void load(); }, 60000);
onUnmounted(() => { generation++; clearInterval(timer); });
</script>
