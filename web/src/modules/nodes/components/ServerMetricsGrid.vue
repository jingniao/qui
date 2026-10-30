<template>
  <div class="space-y-3.5">
    <!-- Top Bar: Title & Time Range Switcher -->
    <div class="flex flex-wrap items-center justify-between gap-3 px-1">
      <div class="flex items-center gap-2">
        <span class="w-2 h-2 rounded-full bg-emerald-500 animate-pulse"></span>
        <h2 class="font-bold text-sm text-ios-label">机器监控</h2>
        <span class="text-xs text-ios-tertiary">实时采样 · 最近 {{ currentRangeLabel }} 趋势</span>
      </div>

      <!-- Time Switcher Pills -->
      <div class="flex items-center gap-1.5">
        <button
          v-for="r in TIME_RANGES"
          :key="r.hours"
          type="button"
          class="px-2.5 py-1 rounded-md text-xs font-medium transition-all"
          :class="selectedRangeHours === r.hours
            ? 'bg-emerald-50 text-emerald-600 border border-emerald-200/80 shadow-2xs font-semibold'
            : 'text-ios-secondary hover:text-ios-label border border-transparent'"
          @click="selectTimeRange(r.hours)"
        >
          {{ r.label }}
        </button>
      </div>
    </div>

    <!-- 6 Trend Metric Cards -->
    <div class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-3.5 sm:gap-4">
      <!-- 1. CPU Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">CPU</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            {{ current.cpu.toFixed(1) }} %
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>CPU %</span>
          <span>负载</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="cpuGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#f97316" stop-opacity="0.28" />
                <stop offset="100%" stop-color="#f97316" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in [0, 20, 40, 60, 80, 100]" :key="'grid-cpu-' + t"
              x1="40" x2="345" :y1="scale(t, 0, 100, 105, 8)" :y2="scale(t, 0, 100, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks (CPU %) -->
            <text v-for="t in [0, 20, 40, 60, 80, 100]" :key="'ltick-cpu-' + t"
              x="35" :y="scale(t, 0, 100, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t }}%
            </text>

            <!-- Right Ticks (Load) -->
            <text v-for="(t, idx) in [0, 0.02, 0.04, 0.06, 0.08, 0.1]" :key="'rtick-cpu-' + idx"
              x="350" :y="scale(idx * 20, 0, 100, 105, 8) + 3" text-anchor="start"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t === 0 ? '0' : t.toFixed(2) }}
            </text>

            <!-- Area & Line -->
            <path :d="cpuAreaPath" fill="url(#cpuGrad)" />
            <path :d="cpuLinePath" fill="none" stroke="#f97316" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicks" :key="'cpu-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>
      </div>

      <!-- 2. 内存 (Memory) Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">内存</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            {{ formatBytes(current.memUsed) }} · {{ formatBytes(current.memTotal) }}
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>内存</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="memGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#ef4444" stop-opacity="0.25" />
                <stop offset="100%" stop-color="#ef4444" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in memTicks" :key="'grid-mem-' + t.val"
              x1="52" x2="370" :y1="scale(t.val, 0, maxMemCeil, 105, 8)" :y2="scale(t.val, 0, maxMemCeil, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks -->
            <text v-for="t in memTicks" :key="'ltick-mem-' + t.val"
              x="46" :y="scale(t.val, 0, maxMemCeil, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t.label }}
            </text>

            <!-- Area & Line -->
            <path :d="memAreaPath" fill="url(#memGrad)" />
            <path :d="memLinePath" fill="none" stroke="#ef4444" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
            <line x1="52" x2="370" y1="105" y2="105" stroke="#eab308" stroke-width="1.2" stroke-linecap="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicksFull" :key="'mem-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>
      </div>

      <!-- 3. 磁盘 (Disk) Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">磁盘</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            {{ formatBytes(current.diskUsed) }} · {{ formatBytes(current.diskTotal) }}
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>磁盘</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="diskGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#14b8a6" stop-opacity="0.22" />
                <stop offset="100%" stop-color="#14b8a6" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in diskTicks" :key="'grid-disk-' + t.val"
              x1="52" x2="370" :y1="scale(t.val, 0, maxDiskCeil, 105, 8)" :y2="scale(t.val, 0, maxDiskCeil, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks -->
            <text v-for="t in diskTicks" :key="'ltick-disk-' + t.val"
              x="46" :y="scale(t.val, 0, maxDiskCeil, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t.label }}
            </text>

            <!-- Area & Line -->
            <path :d="diskAreaPath" fill="url(#diskGrad)" />
            <path :d="diskLinePath" fill="none" stroke="#14b8a6" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicksFull" :key="'disk-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>
      </div>

      <!-- 4. 网络 (Network) Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">网络</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            ↑ {{ formatRate(current.txRate) }} · ↓ {{ formatRate(current.rxRate) }}
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>速度</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="netRxGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#3b82f6" stop-opacity="0.22" />
                <stop offset="100%" stop-color="#3b82f6" stop-opacity="0.02" />
              </linearGradient>
              <linearGradient id="netTxGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#a855f7" stop-opacity="0.20" />
                <stop offset="100%" stop-color="#a855f7" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in netTicks" :key="'grid-net-' + t.val"
              x1="52" x2="370" :y1="scale(t.val, 0, maxNetCeil, 105, 8)" :y2="scale(t.val, 0, maxNetCeil, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks -->
            <text v-for="t in netTicks" :key="'ltick-net-' + t.val"
              x="46" :y="scale(t.val, 0, maxNetCeil, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t.label }}
            </text>

            <!-- Dual Lines & Areas: Download (Blue), Upload (Purple) -->
            <path :d="netRxAreaPath" fill="url(#netRxGrad)" />
            <path :d="netRxLinePath" fill="none" stroke="#3b82f6" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
            <path :d="netTxAreaPath" fill="url(#netTxGrad)" />
            <path :d="netTxLinePath" fill="none" stroke="#a855f7" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicksFull" :key="'net-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>

        <!-- Legend -->
        <div class="flex items-center justify-center gap-4 text-[11px] text-ios-secondary pt-0.5">
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-xs bg-blue-500 inline-block"></span>
            <span>下载</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-xs bg-purple-500 inline-block"></span>
            <span>上传</span>
          </div>
        </div>
      </div>

      <!-- 5. 连接 (Connection) Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">连接</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            TCP: {{ current.tcp }} · UDP: {{ current.udp }}
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>连接数</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="tcpGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#f43f5e" stop-opacity="0.22" />
                <stop offset="100%" stop-color="#f43f5e" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in connTicks" :key="'grid-conn-' + t"
              x1="40" x2="370" :y1="scale(t, 0, maxConnCeil, 105, 8)" :y2="scale(t, 0, maxConnCeil, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks -->
            <text v-for="t in connTicks" :key="'ltick-conn-' + t"
              x="34" :y="scale(t, 0, maxConnCeil, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t }}
            </text>

            <!-- Dual Lines & Areas: TCP (Red/Rose), UDP (Cyan) -->
            <path :d="tcpAreaPath" fill="url(#tcpGrad)" />
            <path :d="tcpLinePath" fill="none" stroke="#f43f5e" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />
            <path :d="udpLinePath" fill="none" stroke="#06b6d4" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicksFull" :key="'conn-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>

        <!-- Legend -->
        <div class="flex items-center justify-center gap-4 text-[11px] text-ios-secondary pt-0.5">
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-xs bg-rose-500 inline-block"></span>
            <span>TCP</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="w-2.5 h-2.5 rounded-xs bg-cyan-500 inline-block"></span>
            <span>UDP</span>
          </div>
        </div>
      </div>

      <!-- 6. 进程 (Process) Card -->
      <div class="panel-card p-4 bg-white space-y-2.5 relative overflow-hidden transition-all shadow-2xs">
        <div class="flex items-center justify-between">
          <h3 class="font-bold text-sm text-ios-label">进程</h3>
          <span class="font-mono text-xs sm:text-sm font-semibold text-ios-label tabular-nums">
            {{ current.processes }}
          </span>
        </div>

        <div class="flex items-center justify-between text-[10px] text-ios-tertiary px-0.5">
          <span>进程</span>
        </div>

        <div class="w-full select-none">
          <svg viewBox="0 0 380 130" class="w-full overflow-visible text-ios-tertiary">
            <defs>
              <linearGradient id="procGrad" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stop-color="#8b5cf6" stop-opacity="0.22" />
                <stop offset="100%" stop-color="#8b5cf6" stop-opacity="0.02" />
              </linearGradient>
            </defs>

            <!-- Grid Lines -->
            <line v-for="t in procTicks" :key="'grid-proc-' + t"
              x1="40" x2="370" :y1="scale(t, 0, maxProcCeil, 105, 8)" :y2="scale(t, 0, maxProcCeil, 105, 8)"
              stroke="currentColor" stroke-dasharray="2 2" class="text-black/[0.06]" />

            <!-- Left Ticks -->
            <text v-for="t in procTicks" :key="'ltick-proc-' + t"
              x="34" :y="scale(t, 0, maxProcCeil, 105, 8) + 3" text-anchor="end"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ t }}
            </text>

            <!-- Area & Line -->
            <path :d="procAreaPath" fill="url(#procGrad)" />
            <path :d="procLinePath" fill="none" stroke="#8b5cf6" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" />

            <!-- X Ticks -->
            <text v-for="(p, i) in sampleXTicksFull" :key="'proc-xtick-' + i"
              :x="p.x" y="122" text-anchor="middle"
              class="text-[9px] font-mono tabular-nums fill-neutral-400">
              {{ p.label }}
            </text>
          </svg>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import type { NodeView } from '../api';
import {
  type SystemMetricPoint,
  formatBytes,
  formatRate,
  scaleLinear as scale,
  buildSvgPath,
  buildAreaSvgPath,
  extractCurrentValues,
  getOrInitMetricsHistory,
  pushMetricsPoint
} from './serverMetricsModel';

const props = withDefaults(
  defineProps<{ node: NodeView; now: number; hours?: number }>(),
  { hours: 1 }
);

const emit = defineEmits<{
  (e: 'update:hours', hours: number): void;
}>();

const TIME_RANGES = [
  { label: '1H', hours: 1 },
  { label: '6H', hours: 6 },
  { label: '12H', hours: 12 },
  { label: '24H', hours: 24 },
  { label: '7D', hours: 168 }
];

const selectedRangeHours = ref(props.hours || 1);

const currentRangeLabel = computed(() => {
  const found = TIME_RANGES.find(r => r.hours === selectedRangeHours.value);
  return found?.label || `${selectedRangeHours.value}H`;
});

function selectTimeRange(hours: number) {
  selectedRangeHours.value = hours;
  emit('update:hours', hours);
  history.value = getOrInitMetricsHistory(props.node, props.now, 25, hours);
}

watch(
  () => props.hours,
  newHours => {
    if (newHours && newHours !== selectedRangeHours.value) {
      selectedRangeHours.value = newHours;
      history.value = getOrInitMetricsHistory(props.node, props.now, 25, newHours);
    }
  }
);

const history = ref<SystemMetricPoint[]>(getOrInitMetricsHistory(props.node, props.now, 25, selectedRangeHours.value));
const current = computed(() => extractCurrentValues(props.node));

// Keep history synced as now or node updates
watch(
  () => [props.now, props.node.lastHeartbeat],
  () => {
    history.value = pushMetricsPoint(history.value, props.node, props.now, 25, selectedRangeHours.value);
  }
);

// X scale mapping
const pointsCount = computed(() => history.value.length);
function getX(index: number, xMin = 40, xMax = 345) {
  if (pointsCount.value <= 1) return xMin;
  return xMin + (index / (pointsCount.value - 1)) * (xMax - xMin);
}

// 5 evenly spaced X ticks across the bottom
const sampleXTicks = computed(() => {
  const pts = history.value;
  if (!pts.length) return [];
  const indices = [0, Math.floor(pts.length * 0.25), Math.floor(pts.length * 0.5), Math.floor(pts.length * 0.75), pts.length - 1];
  return indices.map(idx => ({
    x: getX(idx, 40, 345),
    label: pts[idx]?.timeLabel || ''
  }));
});

const sampleXTicksFull = computed(() => {
  const pts = history.value;
  if (!pts.length) return [];
  const indices = [0, Math.floor(pts.length * 0.25), Math.floor(pts.length * 0.5), Math.floor(pts.length * 0.75), pts.length - 1];
  return indices.map(idx => ({
    x: getX(idx, 52, 370),
    label: pts[idx]?.timeLabel || ''
  }));
});

// 1. CPU Paths
const cpuLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 345),
    y: scale(p.cpu, 0, 100, 105, 8)
  }));
  return buildSvgPath(pts);
});
const cpuAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 345),
    y: scale(p.cpu, 0, 100, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});

// 2. Memory Paths & Ticks
const maxMemCeil = computed(() => current.value.memTotal || 500 * 1024 * 1024);
const memTicks = computed(() => {
  const max = maxMemCeil.value;
  const count = 5;
  const list = [];
  for (let i = 0; i <= count; i++) {
    const val = (max / count) * i;
    list.push({ val, label: formatBytes(val, 1) });
  }
  return list;
});
const memLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.memUsed, 0, maxMemCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const memAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.memUsed, 0, maxMemCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});

// 3. Disk Paths & Ticks
const maxDiskCeil = computed(() => current.value.diskTotal || 1024 * 1024 * 1024);
const diskTicks = computed(() => {
  const max = maxDiskCeil.value;
  const count = 5;
  const list = [];
  for (let i = 0; i <= count; i++) {
    const val = (max / count) * i;
    list.push({ val, label: formatBytes(val, 1) });
  }
  return list;
});
const diskLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.diskUsed, 0, maxDiskCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const diskAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.diskUsed, 0, maxDiskCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});

// 4. Network Paths & Ticks
const maxNetCeil = computed(() => {
  let highest = Math.max(current.value.rxRate, current.value.txRate, 64 * 1024);
  for (const p of history.value) {
    highest = Math.max(highest, p.rxRate, p.txRate);
  }
  return highest * 1.25;
});
const netTicks = computed(() => {
  const max = maxNetCeil.value;
  const count = 5;
  const list = [];
  for (let i = 0; i <= count; i++) {
    const val = (max / count) * i;
    list.push({ val, label: formatRate(val, 1) });
  }
  return list;
});
const netRxLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.rxRate, 0, maxNetCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const netRxAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.rxRate, 0, maxNetCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});
const netTxLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.txRate, 0, maxNetCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const netTxAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 52, 370),
    y: scale(p.txRate, 0, maxNetCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});

// 5. Connection Paths & Ticks
const maxConnCeil = computed(() => {
  let highest = Math.max(current.value.tcp, current.value.udp, 20);
  for (const p of history.value) {
    highest = Math.max(highest, p.tcp, p.udp);
  }
  return Math.ceil(highest * 1.25);
});
const connTicks = computed(() => {
  const max = maxConnCeil.value;
  const count = 5;
  const list = [];
  for (let i = 0; i <= count; i++) {
    list.push(Math.round((max / count) * i));
  }
  return list;
});
const tcpLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 370),
    y: scale(p.tcp, 0, maxConnCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const tcpAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 370),
    y: scale(p.tcp, 0, maxConnCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});
const udpLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 370),
    y: scale(p.udp, 0, maxConnCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});

// 6. Process Paths & Ticks
const maxProcCeil = computed(() => {
  let highest = Math.max(current.value.processes, 50);
  for (const p of history.value) {
    highest = Math.max(highest, p.processes);
  }
  return Math.ceil(highest * 1.25);
});
const procTicks = computed(() => {
  const max = maxProcCeil.value;
  const count = 5;
  const list = [];
  for (let i = 0; i <= count; i++) {
    list.push(Math.round((max / count) * i));
  }
  return list;
});
const procLinePath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 370),
    y: scale(p.processes, 0, maxProcCeil.value, 105, 8)
  }));
  return buildSvgPath(pts);
});
const procAreaPath = computed(() => {
  const pts = history.value.map((p, i) => ({
    x: getX(i, 40, 370),
    y: scale(p.processes, 0, maxProcCeil.value, 105, 8)
  }));
  return buildAreaSvgPath(pts, 105);
});
</script>
