<template>
  <div :class="compact ? 'space-y-2' : 'space-y-3.5'">
    <!-- Compact Mode Header -->
    <div v-if="compact" class="flex flex-wrap items-center justify-between gap-3 px-1">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-blue-500 animate-pulse"></span>
        <h2 class="font-bold text-sm text-ios-label">网络质量与延迟</h2>
        <span class="text-xs text-ios-tertiary">TCP 建连耗时中位数 · 最近 {{ currentRangeLabel }} 趋势</span>
      </div>
    </div>

    <!-- Standard Mode Header Row: Title & Range Selector & Actions (Outside Card, matching ServerMetricsGrid) -->
    <div v-else class="flex flex-wrap items-center justify-between gap-3 px-1">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-blue-500 animate-pulse"></span>
        <h2 class="font-bold text-sm text-ios-label">网络质量与延迟</h2>
        <span class="text-xs text-ios-tertiary">全国多线 TCP 建连耗时 (非 ICMP ping) · 最近 {{ currentRangeLabel }} 趋势</span>
      </div>

      <div class="flex flex-wrap items-center gap-3">
        <!-- Range Badges matching top grid -->
        <div class="flex items-center gap-1.5">
          <button
            v-for="r in RANGES"
            :key="r.hours"
            type="button"
            class="px-2.5 py-1 rounded-md text-xs font-medium transition-all"
            :class="selectedHours === r.hours
              ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold'
              : 'text-ios-secondary hover:text-ios-label border border-transparent'"
            @click="selectRange(r.hours)"
          >
            {{ r.label }}
          </button>
        </div>

        <!-- Select All / Deselect All / Settings -->
        <div class="flex items-center gap-2 text-xs">
          <button
            type="button"
            class="text-emerald-600 hover:text-emerald-700 font-medium cursor-pointer select-none disabled:opacity-40"
            :disabled="!data.targets.length || hidden.length === 0"
            @click="selectAll"
          >
            全选
          </button>
          <button
            type="button"
            class="text-ios-secondary hover:text-ios-label font-medium cursor-pointer select-none disabled:opacity-40"
            :disabled="!data.targets.length || hidden.length === data.targets.length"
            @click="deselectAll"
          >
            全不选
          </button>
          <button
            v-if="isOwner"
            type="button"
            class="ios-action-btn-secondary text-xs ml-1"
            @click="showDrawer = true"
          >
            ⚙ 检测目标
          </button>
        </div>
      </div>
    </div>

    <!-- Error & Unsupported states -->
    <div v-if="unsupported" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">
      面板版本不支持定时检测
    </div>
    <template v-else>
      <div v-if="error" class="panel-card p-4 bg-white flex items-center justify-between gap-2 shadow-2xs">
        <p role="alert" class="text-sm text-ios-red">{{ error }}</p>
        <button class="ios-btn-secondary text-xs" @click="load">重试</button>
      </div>

      <div v-if="loading && !hasPoints" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">
        正在读取延迟数据…
      </div>
      <div v-else-if="!hasPoints && !error" class="panel-card p-5 bg-white text-sm text-ios-secondary shadow-2xs">
        尚无定时检测数据
      </div>

      <template v-if="hasPoints">
        <div v-if="meansUnavailable" class="panel-card p-4 bg-white text-sm text-ios-secondary shadow-2xs">
          当前样本未提供耗时，暂无法绘制延迟曲线。
        </div>

        <!-- Target Cards: 3 Columns matching ServerMetricsGrid cards -->
        <div v-if="!compact && data.targets.length" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3.5 sm:gap-4">
          <div
            v-for="target in data.targets.slice(0, 5)"
            :key="target.id"
            class="panel-card p-4 bg-white space-y-2 relative overflow-hidden transition-all shadow-2xs cursor-pointer select-none"
            :class="[
              hidden.includes(target.id) ? 'opacity-40 border-dashed' : 'hover:border-neutral-300'
            ]"
            role="button"
            tabindex="0"
            :aria-pressed="!hidden.includes(target.id)"
            @click="toggle(target.id)"
            @keydown.enter.prevent="toggle(target.id)"
            @keydown.space.prevent="toggle(target.id)"
          >
            <div class="flex items-center justify-between gap-1">
              <div class="flex items-center gap-1.5">
                <span class="w-1.5 h-3.5 rounded-full inline-block" :class="colorOf(target.id).bar"></span>
                <span class="font-bold text-sm text-ios-label truncate" :title="target.label">
                  {{ target.label }}
                </span>
              </div>
              <span
                class="text-ios-tertiary text-xs cursor-help"
                :title="`${target.host}:${target.port}`"
                @click.stop
              >
                ⓘ
              </span>
            </div>
            <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
              <span>平均延迟 · 丢包率 · 抖动</span>
            </div>
            <p class="text-xs sm:text-sm font-mono font-semibold text-ios-label tabular-nums pl-2">
              {{ formatCardStats(target.id) }}
            </p>
          </div>
        </div>

        <!-- Main Chart Card matching top panel-card style -->
        <div class="panel-card p-4 bg-white space-y-3 relative overflow-hidden transition-all shadow-2xs">
          <!-- Metric Filter Pills: [ 延迟 ] [ 丢包 ] [ 平滑峰值 ] ⓘ -->
          <div v-if="!compact" class="flex items-center gap-2 text-xs">
            <button
              type="button"
              class="px-3 py-1 rounded-md text-xs font-medium transition-all"
              :class="showLatency
                ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold'
                : 'bg-white text-ios-secondary border border-ios-separator hover:text-ios-label'"
              @click="showLatency = !showLatency"
            >
              延迟
            </button>
            <button
              type="button"
              class="px-3 py-1 rounded-md text-xs font-medium transition-all"
              :class="showLoss
                ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold'
                : 'bg-white text-ios-secondary border border-ios-separator hover:text-ios-label'"
              @click="showLoss = !showLoss"
            >
              丢包
            </button>
            <button
              type="button"
              class="px-3 py-1 rounded-md text-xs font-medium transition-all"
              :class="showSmooth
                ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold'
                : 'bg-white text-ios-secondary border border-ios-separator hover:text-ios-label'"
              @click="showSmooth = !showSmooth"
            >
              平滑峰值
            </button>
            <span
              class="text-ios-tertiary cursor-help ml-1 text-xs"
              title="平滑峰值：使用 5 点滚动中位数过滤单次异常突刺，目标卡片始终展示原始统计数据。"
            >
              ⓘ
            </span>
          </div>

          <!-- SVG Chart Container matching top cards visual style -->
          <div ref="chartContainer" class="relative w-full pt-1">
            <svg
              ref="svgRef"
              viewBox="0 0 800 270"
              class="w-full text-ios-tertiary select-none cursor-crosshair overflow-visible"
              role="img"
              :aria-label="`最近 ${selectedHours} 小时 TCP 建连延迟，${visible.length} 个目标；纵轴毫秒，竖线表示丢包；可用左右方向键逐点查看。`"
              tabindex="0"
              @keydown="onKey"
              @blur="onMouseLeave"
              @mousemove="onMouseMove"
              @mouseleave="onMouseLeave"
            >
              <!-- Gradient Defs for Area Fills -->
              <defs>
                <linearGradient
                  v-for="c in TARGET_COLORS"
                  :id="c.gradId"
                  :key="c.gradId"
                  x1="0"
                  y1="0"
                  x2="0"
                  y2="1"
                >
                  <stop offset="0%" :stop-color="c.stroke" stop-opacity="0.22" />
                  <stop offset="100%" :stop-color="c.stroke" stop-opacity="0.02" />
                </linearGradient>
              </defs>

              <!-- Y Axis dashed grid & ticks (Matching top cards style) -->
              <g v-for="tick in ticks" :key="'grid-y-' + tick">
                <line
                  x1="52"
                  x2="780"
                  :y1="y(tick)"
                  :y2="y(tick)"
                  stroke="currentColor"
                  stroke-dasharray="2 2"
                  class="text-black/[0.06]"
                />
                <text
                  x="46"
                  :y="y(tick) + 3"
                  text-anchor="end"
                  class="text-[9px] font-mono tabular-nums fill-neutral-400"
                >
                  {{ tick }}
                </text>
              </g>

              <!-- X Axis time ticks -->
              <g v-for="tick in times" :key="'tick-x-' + tick.at">
                <text
                  :x="x(tick.at)"
                  y="260"
                  text-anchor="middle"
                  class="text-[9px] font-mono tabular-nums fill-neutral-400"
                >
                  {{ tick.label }}
                </text>
              </g>

              <!-- Target loss lines (vertical markers in soft red wherever failed > 0) -->
              <template v-if="showLoss">
                <g
                  v-for="target in visible"
                  :key="'loss-' + target.id"
                >
                  <line
                    v-for="point in lossPoints(target.id)"
                    :key="'loss-' + target.id + '-' + point.at"
                    :x1="x(Date.parse(point.at))"
                    :x2="x(Date.parse(point.at))"
                    y1="20"
                    y2="240"
                    stroke="#ef4444"
                    stroke-width="1.2"
                    stroke-opacity="0.45"
                  />
                </g>
              </template>

              <!-- Target Area Fills (Subtle gradient fills matching top cards) -->
              <template v-if="showLatency">
                <g
                  v-for="target in visible"
                  :key="'area-' + target.id"
                >
                  <path
                    v-for="(part, pIndex) in targetPaths(target.id)"
                    :key="'area-' + target.id + '-' + pIndex"
                    :d="targetAreaPath(part)"
                    :fill="`url(#${colorOf(target.id).gradId})`"
                  />
                </g>
              </template>

              <!-- Target latency lines -->
              <template v-if="showLatency">
                <g
                  v-for="target in visible"
                  :key="'line-' + target.id"
                >
                  <path
                    v-for="(part, pIndex) in targetPaths(target.id)"
                    :key="pIndex"
                    :d="part.map((point, i) => `${i ? 'L' : 'M'}${x(Date.parse(point.at))},${y(latencyMs(point)!)}`).join(' ')"
                    fill="none"
                    :stroke="colorOf(target.id).stroke"
                    stroke-width="1.8"
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
                >
                  <circle
                    v-if="hoveredPoint(target.id) && usable(hoveredPoint(target.id)!)"
                    :cx="x(hoverTimestamp)"
                    :cy="y(latencyMs(hoveredPoint(target.id)!)!)"
                    r="4"
                    :fill="colorOf(target.id).stroke"
                  />
                </g>
              </template>
            </svg>

            <!-- Floating Tooltip Card -->
            <div
              v-if="hoverTimestamp !== null && hoveredInfo.length"
              class="absolute pointer-events-none z-20 p-2.5 rounded-lg border border-ios-separator bg-white/95 backdrop-blur-ios shadow-ios text-xs space-y-1.5 min-w-44 max-w-xs transition-opacity"
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

          <!-- Legend at bottom: Centered ■ 上海电信 ■ 北京联通 ■ 广州移动 -->
          <div class="flex flex-wrap items-center justify-center gap-5 text-xs text-ios-secondary pt-1">
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
              <span class="inline-block w-2.5 h-2.5 rounded-xs" :class="colorOf(target.id).bg"></span>
              <span class="text-ios-label font-medium">{{ target.label }}</span>
            </div>
          </div>
        </div>
      </template>
    </template>

    <LatencyTargetDrawer
      v-if="isOwner"
      v-model="showDrawer"
      :node-id="props.nodeId"
      @saved="load"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { nodeAPI, type LatencyPoint, type LatencyResponse } from '../api';
import { APIError } from '../../../app/apiClient';
import { authState, isOwner } from '../../../app/auth';
import {
  segments,
  usable,
  loss,
  niceTicks,
  hourTicks as timeTicks,
  scale,
  latencyMs,
  seriesStats,
  formatStatsMean,
  formatStatsLoss,
  formatStatsJitter,
  rollingMedian,
  nearestTimestamp
} from './latencySeries';
import LatencyTargetDrawer from './LatencyTargetDrawer.vue';

const props = withDefaults(
  defineProps<{ nodeId: string; compact?: boolean; hours?: number }>(),
  { compact: false, hours: 1 }
);

const emit = defineEmits<{
  (e: 'monitoring'): void;
  (e: 'update:hours', hours: number): void;
}>();

const RANGES = [
  { label: '1H', hours: 1 },
  { label: '6H', hours: 6 },
  { label: '12H', hours: 12 },
  { label: '24H', hours: 24 },
  { label: '7D', hours: 168 },
];

const TARGET_COLORS = [
  { text: 'text-rose-500', stroke: '#f43f5e', bar: 'bg-rose-500', bg: 'bg-rose-500', gradId: 'latGradRose' },
  { text: 'text-teal-500', stroke: '#06b6d4', bar: 'bg-teal-500', bg: 'bg-teal-500', gradId: 'latGradTeal' },
  { text: 'text-purple-500', stroke: '#8b5cf6', bar: 'bg-purple-500', bg: 'bg-purple-500', gradId: 'latGradPurple' },
  { text: 'text-blue-500', stroke: '#3b82f6', bar: 'bg-blue-500', bg: 'bg-blue-500', gradId: 'latGradBlue' },
  { text: 'text-amber-500', stroke: '#f59e0b', bar: 'bg-amber-500', bg: 'bg-amber-500', gradId: 'latGradAmber' },
];

const selectedHours = ref(props.compact ? 1 : (props.hours || 1));
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

const currentRangeLabel = computed(() => {
  const found = RANGES.find(r => r.hours === selectedHours.value);
  return found?.label || `${selectedHours.value}H`;
});

const svgRef = ref<SVGSVGElement | null>(null);
const chartContainer = ref<HTMLDivElement | null>(null);
const hoverTimestamp = ref<number | null>(null);
const hoverX = ref<number | null>(null);
const mouseClientX = ref(0);

const visible = computed(() =>
  data.value.targets.slice(0, 5).filter(t => !hidden.value.includes(t.id))
);

function colorOf(id: string) {
  const index = data.value.targets.findIndex(t => t.id === id);
  return TARGET_COLORS[(index >= 0 ? index : 0) % TARGET_COLORS.length];
}

function rawPoints(targetId: string): LatencyPoint[] {
  const s = data.value.series.find(item => item.targetId === targetId);
  return s ? s.points : [];
}

const targetStatsMap = computed(() => {
  const map: Record<string, ReturnType<typeof seriesStats>> = {};
  for (const t of data.value.targets) {
    map[t.id] = seriesStats(rawPoints(t.id));
  }
  return map;
});

function formatCardStats(targetId: string): string {
  const stats = targetStatsMap.value[targetId];
  if (!stats) return '—';
  const mean = formatStatsMean(stats.meanMs);
  const lossText = formatStatsLoss(stats.lossPercent);
  const jitter = formatStatsJitter(stats.jitterMs);
  return `${mean} · ${lossText} · ${jitter}`;
}

const pointsMap = computed(() => {
  const map: Record<string, LatencyPoint[]> = {};
  for (const t of data.value.targets) {
    const raw = rawPoints(t.id);
    map[t.id] = showSmooth.value ? rollingMedian(raw, 5) : raw;
  }
  return map;
});

function targetPaths(targetId: string): LatencyPoint[][] {
  const pts = pointsMap.value[targetId] || [];
  return segments(pts, data.value.intervalSeconds, start.value, end.value);
}

function targetAreaPath(part: LatencyPoint[]): string {
  if (part.length < 2) return '';
  const lineD = part.map((point, i) => `${i ? 'L' : 'M'}${x(Date.parse(point.at))},${y(latencyMs(point)!)}`).join(' ');
  const first = part[0];
  const last = part[part.length - 1];
  const baseY = 240;
  return `${lineD} L ${x(Date.parse(last.at))},${baseY} L ${x(Date.parse(first.at))},${baseY} Z`;
}

function lossPoints(targetId: string): LatencyPoint[] {
  const pts = rawPoints(targetId);
  return pts.filter(p => {
    const l = loss(p);
    return l !== null && l > 0;
  });
}

const hasPoints = computed(() =>
  data.value.series.some(s => s.points.length > 0)
);

const meansUnavailable = computed(() =>
  hasPoints.value &&
  data.value.series.some(s => s.points.some(p => p.avgMs === null && p.medianMs === null && usable(p)))
);

const maxLatency = computed(() => {
  let peak = 0;
  for (const target of visible.value) {
    const pts = pointsMap.value[target.id] || [];
    for (const point of pts) {
      const ms = latencyMs(point);
      if (ms !== null && Number.isFinite(ms) && usable(point) && ms > peak) {
        peak = ms;
      }
    }
  }
  return peak;
});

const ticks = computed(() => niceTicks(maxLatency.value));

const timezone = computed(() =>
  authState.timezone ||
  Intl.DateTimeFormat().resolvedOptions().timeZone ||
  'UTC'
);

const times = computed(() =>
  timeTicks(start.value, end.value, timezone.value, selectedHours.value)
);

function x(epochMs: number): number {
  return scale(epochMs, start.value, end.value, 55, 780);
}

function y(val: number): number {
  const top = ticks.value[ticks.value.length - 1] || 1;
  return scale(val, 0, top, 240, 20);
}

function selectRange(hours: number) {
  selectedHours.value = hours;
  emit('update:hours', hours);
  void load();
}

watch(
  () => props.hours,
  newHours => {
    if (newHours && newHours !== selectedHours.value) {
      selectedHours.value = newHours;
      void load();
    }
  }
);

function selectAll() {
  hidden.value = [];
}

function deselectAll() {
  hidden.value = data.value.targets.map(t => t.id);
}

function toggle(id: string) {
  if (hidden.value.includes(id)) {
    hidden.value = hidden.value.filter(item => item !== id);
  } else {
    hidden.value = [...hidden.value, id];
  }
}

let generation = 0;

async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  end.value = Date.now();

  try {
    const res = await nodeAPI.latency(props.nodeId, selectedHours.value);
    if (current !== generation) return;
    data.value = res;
    unsupported.value = false;
  } catch (err) {
    if (current !== generation) return;
    if (err instanceof APIError && (err.status === 404 || err.code === 'not_found')) {
      unsupported.value = true;
    } else {
      error.value = (err as Error).message || '读取延迟数据失败';
    }
  } finally {
    if (current === generation) {
      loading.value = false;
    }
  }
}

// Hover interaction
const allTimestamps = computed(() => {
  const set = new Set<number>();
  for (const t of visible.value) {
    const pts = rawPoints(t.id);
    for (const p of pts) {
      set.add(Date.parse(p.at));
    }
  }
  return Array.from(set).sort((a, b) => a - b);
});

function onMouseMove(event: MouseEvent) {
  const svg = svgRef.value;
  if (!svg || !allTimestamps.value.length) return;
  const rect = svg.getBoundingClientRect();
  mouseClientX.value = event.clientX;
  const ratio = (event.clientX - rect.left) / rect.width;
  const hoverEpoch = start.value + ratio * (end.value - start.value);
  const nearest = nearestTimestamp(allTimestamps.value, hoverEpoch);
  if (nearest !== null) {
    hoverTimestamp.value = nearest;
    hoverX.value = x(nearest);
  }
}

function onMouseLeave() {
  hoverTimestamp.value = null;
  hoverX.value = null;
}

function onKey(event: KeyboardEvent) {
  const ts = allTimestamps.value;
  if (!ts.length) return;
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') {
    event.preventDefault();
    if (hoverTimestamp.value === null) {
      hoverTimestamp.value = ts[0];
    } else {
      const idx = ts.indexOf(hoverTimestamp.value);
      hoverTimestamp.value = idx >= 0 && idx < ts.length - 1 ? ts[idx + 1] : ts[0];
    }
    hoverX.value = x(hoverTimestamp.value);
  } else if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') {
    event.preventDefault();
    if (hoverTimestamp.value === null) {
      hoverTimestamp.value = ts[ts.length - 1];
    } else {
      const idx = ts.indexOf(hoverTimestamp.value);
      hoverTimestamp.value = idx > 0 ? ts[idx - 1] : ts[ts.length - 1];
    }
    hoverX.value = x(hoverTimestamp.value);
  } else if (event.key === 'Escape') {
    onMouseLeave();
  }
}

function hoveredPoint(targetId: string): LatencyPoint | null {
  if (hoverTimestamp.value === null) return null;
  const pts = rawPoints(targetId);
  return pts.find(p => Date.parse(p.at) === hoverTimestamp.value) || null;
}

const hoveredInfo = computed(() => {
  if (hoverTimestamp.value === null) return [];
  const list = [];
  for (const t of visible.value) {
    const pt = hoveredPoint(t.id);
    if (!pt) continue;
    const ms = latencyMs(pt);
    const l = loss(pt);
    const latencyText = ms !== null && usable(pt) ? `${Math.round(ms)} ms` : '无响应';
    const lossText = l !== null && l > 0 ? `丢包 ${Math.round(l * 100)}%` : null;
    list.push({
      id: t.id,
      label: t.label,
      color: colorOf(t.id),
      latencyText,
      lossText
    });
  }
  return list;
});

const tooltipStyle = computed(() => {
  if (!chartContainer.value || hoverX.value === null) return { display: 'none' };
  const cWidth = chartContainer.value.clientWidth || 800;
  const leftPx = (hoverX.value / 800) * cWidth;
  const isRightSide = leftPx > cWidth * 0.65;
  return {
    left: isRightSide ? `${leftPx - 180}px` : `${leftPx + 15}px`,
    top: '30px'
  };
});

function formatHoverTime(epochMs: number): string {
  const d = new Date(epochMs);
  const pad = (n: number) => n.toString().padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// Auto reload interval (every 60s)
let timer: ReturnType<typeof setInterval> | null = null;

watch(
  () => props.nodeId,
  () => {
    void load();
  },
  { immediate: true }
);

timer = setInterval(() => {
  if (!loading.value) {
    void load();
  }
}, 60000);

onUnmounted(() => {
  generation++;
  if (timer) clearInterval(timer);
});
</script>
