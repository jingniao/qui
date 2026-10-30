<template>
  <section class="panel-card p-6 space-y-4">
    <!-- Header -->
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h2 class="font-semibold text-ios-label">网络延迟{{ compact ? '（1 小时）' : '' }}</h2>
        <p class="text-xs text-ios-secondary">TCP 建连耗时中位数（非 ICMP ping），每分钟检测 3 次</p>
      </div>

      <!-- Controls in standard mode -->
      <div v-if="!compact" class="flex flex-wrap items-center gap-2">
        <div class="ios-segmented-container inline-flex">
          <button
            v-for="r in RANGES"
            :key="r.hours"
            type="button"
            :class="['ios-segmented-item text-xs', selectedHours === r.hours ? 'active' : '']"
            @click="selectRange(r.hours)"
          >
            {{ r.label }}
          </button>
        </div>
        <button
          v-if="isOwner"
          type="button"
          class="ios-action-btn-secondary text-xs"
          @click="showDrawer = true"
        >
          ⚙ 检测目标
        </button>
        <div class="flex items-center gap-1">
          <button
            type="button"
            class="ios-action-btn-secondary text-xs"
            :disabled="!data.targets.length || hidden.length === 0"
            @click="selectAll"
          >
            全选
          </button>
          <button
            type="button"
            class="ios-action-btn-secondary text-xs"
            :disabled="!data.targets.length || hidden.length === data.targets.length"
            @click="deselectAll"
          >
            全不选
          </button>
        </div>
      </div>
    </div>

    <!-- Error & Unsupported states -->
    <p v-if="unsupported" class="text-sm text-ios-secondary">面板版本不支持定时检测</p>
    <template v-else>
      <div v-if="error" class="flex items-center gap-2">
        <p role="alert" class="text-sm text-ios-red">{{ error }}</p>
        <button class="ios-btn-secondary text-xs" @click="load">重试</button>
      </div>

      <p v-if="loading && !hasPoints" class="text-sm text-ios-secondary">正在读取延迟数据…</p>
      <p v-else-if="!hasPoints && !error" class="text-sm text-ios-secondary">尚无定时检测数据</p>

      <template v-if="hasPoints">
        <p v-if="meansUnavailable" class="text-sm text-ios-secondary">
          当前样本未提供耗时，暂无法绘制延迟曲线。
        </p>

        <!-- Target Cards (Standard mode only) -->
        <div v-if="!compact && data.targets.length" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-3">
          <div
            v-for="(target, index) in data.targets.slice(0, 5)"
            :key="target.id"
            class="p-3 rounded-lg border border-ios-separator bg-ios-bg/30 border-l-4 transition-opacity cursor-pointer select-none"
            :class="[
              TARGET_COLORS[index % TARGET_COLORS.length].border,
              hidden.includes(target.id) ? 'opacity-40' : 'hover:bg-ios-bg/60'
            ]"
            role="button"
            tabindex="0"
            :aria-pressed="!hidden.includes(target.id)"
            @click="toggle(target.id)"
            @keydown.enter.prevent="toggle(target.id)"
            @keydown.space.prevent="toggle(target.id)"
          >
            <div class="flex items-center justify-between gap-1 mb-1">
              <span class="font-medium text-xs text-ios-label truncate" :title="target.label">
                {{ target.label }}
              </span>
              <span
                class="text-ios-tertiary text-xs cursor-help"
                :title="`${target.host}:${target.port}`"
                @click.stop
              >
                ⓘ
              </span>
            </div>
            <p class="text-xs font-mono text-ios-secondary tabular-nums">
              {{ formatCardStats(target.id) }}
            </p>
          </div>
        </div>

        <!-- Toggles (Standard mode only) -->
        <div v-if="!compact" class="flex items-center gap-3 text-xs">
          <div class="ios-segmented-container inline-flex">
            <button
              type="button"
              :class="['ios-segmented-item', showLatency ? 'active' : '']"
              @click="showLatency = !showLatency"
            >
              延迟
            </button>
            <button
              type="button"
              :class="['ios-segmented-item', showLoss ? 'active' : '']"
              @click="showLoss = !showLoss"
            >
              丢包
            </button>
            <button
              type="button"
              :class="['ios-segmented-item', showSmooth ? 'active' : '']"
              @click="showSmooth = !showSmooth"
            >
              平滑峰值
            </button>
          </div>
          <span
            class="text-ios-tertiary cursor-help"
            title="平滑峰值：使用 5 点滚动中位数过滤单次异常突刺，目标卡片始终展示原始统计数据。"
          >
            ⓘ
          </span>
        </div>

        <!-- SVG Chart Container with relative overlay for tooltip -->
        <div ref="chartContainer" class="relative w-full">
          <svg
            ref="svgRef"
            viewBox="0 0 800 280"
            class="w-full text-ios-tertiary select-none cursor-crosshair"
            role="img"
            :aria-label="`最近 ${selectedHours} 小时 TCP 建连延迟，${visible.length} 个目标；纵轴毫秒，竖线表示丢包；可用左右方向键逐点查看。`"
            tabindex="0"
            @keydown="onKey"
            @blur="onMouseLeave"
            @mousemove="onMouseMove"
            @mouseleave="onMouseLeave"
          >
            <!-- Y Axis dashed grid & ticks -->
            <g v-for="tick in ticks" :key="tick">
              <line
                x1="55"
                x2="780"
                :y1="y(tick)"
                :y2="y(tick)"
                stroke="currentColor"
                stroke-dasharray="3 3"
                class="text-ios-separator"
              />
              <text
                x="48"
                :y="y(tick) + 4"
                text-anchor="end"
                fill="currentColor"
                class="text-xs font-mono tabular-nums"
              >
                {{ tick }}
              </text>
            </g>
            <text x="10" y="15" fill="currentColor" class="text-xs font-medium">延迟 (ms)</text>

            <!-- X Axis time ticks -->
            <g v-for="tick in times" :key="tick.at">
              <text
                :x="x(tick.at)"
                y="272"
                text-anchor="middle"
                fill="currentColor"
                class="text-xs font-mono tabular-nums"
              >
                {{ tick.label }}
              </text>
            </g>

            <!-- Target loss lines (vertical full-height lines in target color wherever failed > 0) -->
            <template v-if="showLoss">
              <g
                v-for="target in visible"
                :key="'loss-' + target.id"
                :class="colorOf(target.id).text"
              >
                <line
                  v-for="point in lossPoints(target.id)"
                  :key="'loss-' + target.id + '-' + point.at"
                  :x1="x(Date.parse(point.at))"
                  :x2="x(Date.parse(point.at))"
                  y1="20"
                  y2="240"
                  stroke="currentColor"
                  stroke-width="1.5"
                  :opacity="0.25 + (loss(point) ?? 0) * 0.5"
                />
              </g>
            </template>

            <!-- Target latency lines -->
            <template v-if="showLatency">
              <g
                v-for="target in visible"
                :key="'line-' + target.id"
                :class="colorOf(target.id).text"
              >
                <path
                  v-for="(part, pIndex) in targetPaths(target.id)"
                  :key="pIndex"
                  :d="part.map((point, i) => `${i ? 'L' : 'M'}${x(Date.parse(point.at))},${y(latencyMs(point)!)}`).join(' ')"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                />
              </g>
            </template>

            <!-- Crosshair line at hover timestamp -->
            <line
              v-if="hoverX !== null"
              :x1="hoverX"
              :x2="hoverX"
              y1="20"
              y2="240"
              stroke="currentColor"
              stroke-dasharray="2 2"
              class="text-ios-tertiary opacity-80"
            />

            <!-- Dots on visible points at hover timestamp -->
            <template v-if="hoverTimestamp !== null">
              <g
                v-for="target in visible"
                :key="'hover-dot-' + target.id"
                :class="colorOf(target.id).text"
              >
                <circle
                  v-if="hoveredPoint(target.id) && usable(hoveredPoint(target.id)!)"
                  :cx="x(hoverTimestamp)"
                  :cy="y(latencyMs(hoveredPoint(target.id)!)!)"
                  r="4"
                  fill="currentColor"
                />
              </g>
            </template>
          </svg>

          <!-- Floating Tooltip Card -->
          <div
            v-if="hoverTimestamp !== null && hoveredInfo.length"
            class="absolute pointer-events-none z-20 p-2.5 rounded-lg border border-ios-separator bg-ios-card/95 backdrop-blur-ios shadow-ios text-xs space-y-1.5 min-w-44 max-w-xs transition-opacity"
            :style="tooltipStyle"
          >
            <div class="font-mono text-ios-secondary font-medium pb-1 border-b border-ios-separator">
              {{ formatHoverTime(hoverTimestamp) }}
            </div>
            <div
              v-for="info in hoveredInfo"
              :key="info.id"
              class="flex flex-col gap-0.5"
            >
              <div class="flex items-center gap-1.5">
                <span class="inline-block w-2 h-2 rounded-full" :class="info.color.bg"></span>
                <span class="font-medium text-ios-label truncate">{{ info.label }}</span>
              </div>
              <div class="pl-3.5 text-ios-secondary font-mono space-x-1 tabular-nums">
                <span>{{ info.latencyText }}</span>
                <span v-if="info.lossText !== null" class="text-ios-red font-medium">· {{ info.lossText }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- Legend at bottom -->
        <div class="flex flex-wrap items-center justify-center gap-4 text-xs text-ios-secondary pt-1">
          <div
            v-for="target in visible"
            :key="'legend-' + target.id"
            class="flex items-center gap-1.5 cursor-pointer select-none"
            role="button"
            tabindex="0"
            @click="toggle(target.id)"
            @keydown.enter.prevent="toggle(target.id)"
            @keydown.space.prevent="toggle(target.id)"
          >
            <span class="inline-block w-2.5 h-2.5 rounded-full" :class="colorOf(target.id).bg"></span>
            <span class="text-ios-label">{{ target.label }}</span>
          </div>
        </div>

        <!-- Accessible Status Line -->
        <p class="text-xs text-ios-secondary min-h-6" role="status">
          {{ statusDescription }}
        </p>

        <!-- Accessible Data Table (Standard mode only) -->
        <details v-if="!compact" class="text-xs text-ios-secondary pt-1">
          <summary class="cursor-pointer font-medium hover:text-ios-label">查看数据表</summary>
          <div class="max-h-64 overflow-auto mt-2 border border-ios-separator rounded-lg">
            <table class="w-full text-left">
              <caption class="sr-only">最近 {{ selectedHours }} 小时延迟样本（毫秒）</caption>
              <thead class="bg-ios-bg/60 border-b border-ios-separator sticky top-0">
                <tr>
                  <th class="p-2">目标</th>
                  <th class="p-2">时间</th>
                  <th class="p-2">中位耗时</th>
                  <th class="p-2">失败率</th>
                </tr>
              </thead>
              <tbody>
                <template v-for="target in visible" :key="'table-' + target.id">
                  <tr
                    v-for="point in rawPoints(target.id)"
                    :key="point.at"
                    class="border-b border-ios-separator/50"
                  >
                    <td class="p-2 font-medium">{{ target.label }}</td>
                    <td class="p-2 font-mono tabular-nums">{{ formatTime(point.at) }}</td>
                    <td class="p-2 font-mono tabular-nums">{{ formatTableLatency(point) }}</td>
                    <td class="p-2 font-mono tabular-nums">{{ formatTableLoss(point) }}</td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </details>
      </template>

      <!-- Refresh button (Standard mode) -->
      <div v-if="!compact" class="pt-2">
        <button
          type="button"
          class="ios-btn-secondary"
          :disabled="loading"
          @click="load"
        >
          刷新延迟数据
        </button>
      </div>
    </template>

    <!-- Compact mode footer link -->
    <button
      v-if="compact"
      type="button"
      class="text-sm text-ios-blue"
      @click="$emit('monitoring')"
    >
      查看监控 →
    </button>

    <!-- Target Settings Drawer (Owner) -->
    <LatencyTargetDrawer
      v-if="isOwner"
      v-model="showDrawer"
      :node-id="props.nodeId"
      @saved="load"
    />
  </section>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { nodeAPI, type LatencyResponse, type LatencyPoint } from '../api';
import { APIError } from '../../../app/apiClient';
import { authState, isOwner, formatTime } from '../../../app/auth';
import {
  segments,
  niceTicks,
  scale,
  timeTicks,
  loss,
  usable,
  latencyMs,
  seriesStats,
  formatStatsMean,
  formatStatsLoss,
  formatStatsJitter,
  rollingMedian,
  nearestTimestamp,
  describePointLatency
} from './latencySeries';
import LatencyTargetDrawer from './LatencyTargetDrawer.vue';

const props = defineProps<{ nodeId: string; compact?: boolean }>();
defineEmits<{ monitoring: [] }>();

const RANGES = [
  { label: '1H', hours: 1 },
  { label: '6H', hours: 6 },
  { label: '12H', hours: 12 },
  { label: '24H', hours: 24 },
  { label: '7D', hours: 168 },
];

const TARGET_COLORS = [
  { text: 'text-ios-red', stroke: '#ff3b30', border: 'border-ios-red', bg: 'bg-ios-red' },
  { text: 'text-ios-teal', stroke: '#5ac8fa', border: 'border-ios-teal', bg: 'bg-ios-teal' },
  { text: 'text-ios-purple', stroke: '#af52de', border: 'border-ios-purple', bg: 'bg-ios-purple' },
  { text: 'text-ios-blue', stroke: '#0071e3', border: 'border-ios-blue', bg: 'bg-ios-blue' },
  { text: 'text-ios-orange', stroke: '#ff9500', border: 'border-ios-orange', bg: 'bg-ios-orange' },
];

const selectedHours = ref(props.compact ? 1 : 1);
const showLatency = ref(true);
const showLoss = ref(true);
const showSmooth = ref(false);
const showDrawer = ref(false);

const data = ref<LatencyResponse>({ intervalSeconds: 60, sampleSeconds: 60, targets: [], series: [] });
const hidden = ref<string[]>([]);
const error = ref('');
const unsupported = ref(false);
const loading = ref(false);

const end = ref(Date.now());
const start = computed(() => end.value - selectedHours.value * 3600000);

const svgRef = ref<SVGSVGElement | null>(null);
const chartContainer = ref<HTMLDivElement | null>(null);
const hoverTimestamp = ref<number | null>(null);
const hoverX = ref<number | null>(null);
const mouseClientX = ref(0);

const isBucket = computed(() => data.value.intervalSeconds > (data.value.sampleSeconds ?? 60));

const visible = computed(() =>
  data.value.targets.slice(0, 5).filter(t => !hidden.value.includes(t.id))
);

function colorOf(id: string) {
  const index = data.value.targets.findIndex(t => t.id === id);
  return TARGET_COLORS[(index >= 0 ? index : 0) % TARGET_COLORS.length];
}

function rawPoints(targetId: string): LatencyPoint[] {
  const s = data.value.series.find(item => item.targetId === targetId);
  if (!s) return [];
  return s.points.filter(p => {
    const t = Date.parse(p.at);
    return Number.isFinite(t) && t >= start.value && t <= end.value;
  });
}

function plottedPoints(targetId: string): LatencyPoint[] {
  const pts = rawPoints(targetId);
  return showSmooth.value ? rollingMedian(pts) : pts;
}

const hasPoints = computed(() =>
  data.value.targets.some(target => rawPoints(target.id).length > 0)
);

const meansUnavailable = computed(() =>
  hasPoints.value &&
  !data.value.targets.some(target => rawPoints(target.id).some(usable)) &&
  data.value.targets.some(target => rawPoints(target.id).some(p => p.sent > p.failed))
);

const ticks = computed(() => {
  const maxVal = Math.max(
    0,
    ...visible.value.flatMap(t =>
      plottedPoints(t.id).filter(usable).map(p => latencyMs(p)!)
    )
  );
  return niceTicks(maxVal);
});

const times = computed(() =>
  timeTicks(start.value, end.value, authState.timezone, selectedHours.value)
);

const x = (atMs: number) => scale(atMs, start.value, end.value, 55, 780);
const y = (ms: number) => scale(ms, 0, ticks.value[ticks.value.length - 1] ?? 1, 240, 20);

function targetPaths(targetId: string): LatencyPoint[][] {
  return segments(plottedPoints(targetId), data.value.intervalSeconds, start.value, end.value);
}

function lossPoints(targetId: string): LatencyPoint[] {
  return rawPoints(targetId).filter(p => p.failed > 0);
}

function formatCardStats(targetId: string): string {
  const pts = rawPoints(targetId);
  const s = seriesStats(pts);
  const mean = formatStatsMean(s.meanMs);
  const lossP = formatStatsLoss(s.lossPercent);
  const jitter = formatStatsJitter(s.jitterMs);
  return `${mean} · ${lossP} · ${jitter}`;
}

const allTimestamps = computed(() => {
  const set = new Set<number>();
  for (const t of visible.value) {
    for (const p of rawPoints(t.id)) {
      const at = Date.parse(p.at);
      if (Number.isFinite(at)) set.add(at);
    }
  }
  return Array.from(set).sort((a, b) => a - b);
});

function onMouseMove(e: MouseEvent) {
  if (!svgRef.value) return;
  const rect = svgRef.value.getBoundingClientRect();
  const svgX = ((e.clientX - rect.left) / rect.width) * 800;
  mouseClientX.value = e.clientX - rect.left;

  if (svgX < 50 || svgX > 785 || !allTimestamps.value.length) {
    hoverTimestamp.value = null;
    hoverX.value = null;
    return;
  }
  const cursorTime = scale(svgX, 55, 780, start.value, end.value);
  const nearest = nearestTimestamp(allTimestamps.value, cursorTime);
  if (nearest !== null) {
    hoverTimestamp.value = nearest;
    hoverX.value = x(nearest);
  } else {
    hoverTimestamp.value = null;
    hoverX.value = null;
  }
}

// Keyboard users step the crosshair through sample times.
function onKey(e: KeyboardEvent) {
  const times = allTimestamps.value;
  if (!times.length) return;
  if (e.key === 'Escape') { onMouseLeave(); return; }
  if (!['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(e.key)) return;
  e.preventDefault();
  const index = hoverTimestamp.value === null ? -1 : times.indexOf(hoverTimestamp.value);
  const next = e.key === 'Home' ? 0 : e.key === 'End' ? times.length - 1
    : index < 0 ? (e.key === 'ArrowLeft' ? times.length - 1 : 0)
    : Math.max(0, Math.min(times.length - 1, index + (e.key === 'ArrowLeft' ? -1 : 1)));
  hoverTimestamp.value = times[next]!;
  hoverX.value = x(times[next]!);
  const width = svgRef.value?.getBoundingClientRect().width ?? 800;
  mouseClientX.value = hoverX.value / 800 * width;
}

function onMouseLeave() {
  hoverTimestamp.value = null;
  hoverX.value = null;
}

function hoveredPoint(targetId: string): LatencyPoint | undefined {
  if (hoverTimestamp.value === null) return undefined;
  const pts = plottedPoints(targetId);
  return pts.find(p => Date.parse(p.at) === hoverTimestamp.value);
}

const hoveredInfo = computed(() => {
  if (hoverTimestamp.value === null) return [];
  const results = [];
  for (const t of visible.value) {
    const pt = hoveredPoint(t.id);
    if (!pt) continue;
    const latencyText = describePointLatency(pt, isBucket.value);
    const lossVal = loss(pt);
    const lossText = lossVal !== null && lossVal > 0 ? `丢包 ${(lossVal * 100).toFixed(1)}%` : null;
    results.push({
      id: t.id,
      label: t.label,
      color: colorOf(t.id),
      latencyText,
      lossText,
    });
  }
  return results;
});

const tooltipStyle = computed(() => {
  if (!chartContainer.value || hoverX.value === null) return {};
  const containerWidth = chartContainer.value.clientWidth;
  const targetX = (hoverX.value / 800) * containerWidth;
  // Flip tooltip to left side if past midpoint to prevent clipping
  const isRightHalf = targetX > containerWidth / 2;
  const offset = 12;
  return isRightHalf
    ? { right: `${containerWidth - targetX + offset}px`, top: '24px' }
    : { left: `${targetX + offset}px`, top: '24px' };
});

function formatHoverTime(atMs: number): string {
  return new Intl.DateTimeFormat('zh-CN', {
    timeZone: authState.timezone,
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hourCycle: 'h23',
  }).format(atMs);
}

const statusDescription = computed(() => {
  if (hoverTimestamp.value !== null && hoveredInfo.value.length) {
    const summary = hoveredInfo.value
      .map(i => `${i.label}: ${i.latencyText}${i.lossText ? ' (' + i.lossText + ')' : ''}`)
      .join('；');
    return `${formatHoverTime(hoverTimestamp.value)} · ${summary}`;
  }
  return `时间按面板时区 ${authState.timezone}；缺测与全部失败处断线，红色竖线表示检测失败。`;
});

function formatTableLatency(point: LatencyPoint): string {
  const ms = latencyMs(point);
  return ms === null ? '—' : `${ms.toFixed(2)} ms`;
}

function formatTableLoss(point: LatencyPoint): string {
  const l = loss(point);
  return l === null ? '—' : `${(l * 100).toFixed(1)}%`;
}

function toggle(id: string) {
  hidden.value = hidden.value.includes(id)
    ? hidden.value.filter(v => v !== id)
    : [...hidden.value, id];
}

function selectAll() {
  hidden.value = [];
}

function deselectAll() {
  hidden.value = data.value.targets.map(t => t.id);
}

function selectRange(hours: number) {
  selectedHours.value = hours;
  void load();
}

let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  unsupported.value = false;
  hoverTimestamp.value = null;
  hoverX.value = null;

  try {
    const hours = props.compact ? 1 : selectedHours.value;
    const result = await nodeAPI.latency(props.nodeId, hours);
    if (current !== generation) return;
    data.value = result;
    end.value = Date.now();
  } catch (e) {
    if (current !== generation) return;
    if (e instanceof APIError && e.status === 404) {
      unsupported.value = true;
    } else {
      error.value = (e as Error).message || '读取延迟数据失败';
    }
  } finally {
    if (current === generation) loading.value = false;
  }
}

watch(
  () => props.nodeId,
  () => {
    data.value = { intervalSeconds: 60, sampleSeconds: 60, targets: [], series: [] };
    hidden.value = [];
    selectedHours.value = props.compact ? 1 : 1;
    void load();
  },
  { immediate: true }
);

onUnmounted(() => {
  generation++;
});
</script>
