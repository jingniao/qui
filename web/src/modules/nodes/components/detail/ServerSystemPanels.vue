<template>
  <!-- 3 System Performance Cards (Full Width 3 Columns, Perfectly Symmetrical) -->
  <div class="grid grid-cols-1 md:grid-cols-3 gap-3.5 sm:gap-4 items-stretch">
    <!-- 1. 硬件信息 -->
    <section class="panel-card p-4 sm:p-4.5 bg-white flex flex-col justify-between space-y-3 shadow-2xs">
      <!-- Card Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <div class="w-7 h-7 rounded-lg bg-blue-50 text-ios-blue flex items-center justify-center shrink-0">
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <rect x="4" y="4" width="16" height="16" rx="2" stroke-width="1.8"></rect>
              <rect x="9" y="9" width="6" height="6" stroke-width="1.8"></rect>
              <path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3" stroke-width="1.8"></path>
            </svg>
          </div>
          <h3 class="font-bold text-ios-label text-sm tracking-tight">硬件信息</h3>
        </div>
        <span
          class="text-[11px] px-2 py-0.5 rounded-full font-semibold tabular-nums"
          :class="cpuPercent >= 85 ? 'bg-rose-50 text-ios-red' : (cpuPercent >= 65 ? 'bg-amber-50 text-amber-700' : 'bg-emerald-50 text-emerald-700')"
        >
          CPU {{ cpuPercentDisplay }}
        </span>
      </div>

      <!-- Upper Hero Metric Box -->
      <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-3 space-y-2">
        <div class="flex items-center justify-between text-xs">
          <span class="text-ios-secondary font-medium truncate max-w-[180px]" :title="cpuModelName">
            {{ cpuModelName }}
          </span>
          <span class="font-bold text-ios-label tabular-nums">{{ cpuPercentDisplay }}</span>
        </div>
        <div class="w-full bg-black/[0.05] h-1.5 rounded-full overflow-hidden">
          <div
            class="h-full rounded-full transition-all duration-500"
            :class="cpuPercentColor"
            :style="{ width: `${cpuPercent}%` }"
          ></div>
        </div>
      </div>

      <!-- Lower 3-Column Sub-grid: 架构 / 内核 / GPU -->
      <div class="grid grid-cols-3 gap-2">
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">架构</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="architecture">
            {{ architecture }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">内核</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="kernel">
            {{ kernel }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">GPU</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="gpu">
            {{ gpu }}
          </p>
        </div>
      </div>
    </section>

    <!-- 2. 存储信息 -->
    <section class="panel-card p-4 sm:p-4.5 bg-white flex flex-col justify-between space-y-3 shadow-2xs">
      <!-- Card Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <div class="w-7 h-7 rounded-lg bg-purple-50 text-purple-600 flex items-center justify-center shrink-0">
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <rect x="3" y="4" width="18" height="16" rx="2" stroke-width="1.8"></rect>
              <line x1="7" y1="2" x2="7" y2="4" stroke-width="1.8"></line>
              <line x1="17" y1="2" x2="17" y2="4" stroke-width="1.8"></line>
              <line x1="7" y1="20" x2="7" y2="22" stroke-width="1.8"></line>
              <line x1="17" y1="20" x2="17" y2="22" stroke-width="1.8"></line>
            </svg>
          </div>
          <h3 class="font-bold text-ios-label text-sm tracking-tight">存储信息</h3>
        </div>
        <span
          class="text-[11px] px-2 py-0.5 rounded-full font-semibold tabular-nums"
          :class="memoryPercent >= 90 ? 'bg-rose-50 text-ios-red' : (memoryPercent >= 75 ? 'bg-amber-50 text-amber-700' : 'bg-purple-50 text-purple-700')"
        >
          内存 {{ memoryPercentDisplay }}
        </span>
      </div>

      <!-- Upper Hero Metric Box -->
      <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-3 space-y-2">
        <div class="flex items-center justify-between text-xs">
          <span class="text-ios-secondary font-medium">内存占用</span>
          <span class="font-bold text-ios-label tabular-nums">{{ memoryPercentDisplay }}</span>
        </div>
        <div class="w-full bg-black/[0.05] h-1.5 rounded-full overflow-hidden">
          <div
            class="h-full rounded-full transition-all duration-500"
            :class="memoryPercentColor"
            :style="{ width: `${memoryPercent}%` }"
          ></div>
        </div>
      </div>

      <!-- Lower 3-Column Sub-grid: 内存 / 内存交换 / 硬盘 -->
      <div class="grid grid-cols-3 gap-2">
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">内存</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="memoryDisplay">
            {{ memoryDisplay }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">内存交换</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="swapDisplay">
            {{ swapDisplay }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">硬盘</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="diskDisplay">
            {{ diskDisplay }}
          </p>
        </div>
      </div>
    </section>

    <!-- 3. 网络信息 -->
    <section class="panel-card p-4 sm:p-4.5 bg-white flex flex-col justify-between space-y-3 shadow-2xs">
      <!-- Card Header -->
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <div class="w-7 h-7 rounded-lg bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
            <svg class="w-3.5 h-3.5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M13 10V3L4 14h7v7l9-11h-7z"></path>
            </svg>
          </div>
          <h3 class="font-bold text-ios-label text-sm tracking-tight">网络信息</h3>
        </div>

      </div>

      <!-- Upper Hero Metric Box -->
      <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-3 space-y-2">
        <div class="flex items-center justify-between text-xs">
          <span class="text-ios-secondary font-medium">实时速率</span>
          <div class="flex items-center gap-2.5 tabular-nums font-semibold text-xs">
            <span class="text-emerald-600 font-medium">▲ {{ nicRate(metrics, 'transmit') }}</span>
            <span class="text-ios-blue font-medium">▼ {{ nicRate(metrics, 'receive') }}</span>
          </div>
        </div>
        <!-- Upload/Download Ratio Bar -->
        <div class="w-full bg-black/[0.05] h-1.5 rounded-full overflow-hidden flex">
          <div
            class="bg-emerald-500 h-full transition-all duration-500"
            :style="{ width: `${uploadRatioPercent}%` }"
            :title="`上行占比: ${uploadRatioPercent}%`"
          ></div>
          <div
            class="bg-ios-blue h-full transition-all duration-500"
            :style="{ width: `${downloadRatioPercent}%` }"
            :title="`下行占比: ${downloadRatioPercent}%`"
          ></div>
        </div>
      </div>

      <!-- Lower 3-Column Sub-grid: 总流量 / 上行 / 下行 -->
      <div class="grid grid-cols-3 gap-2">
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">总流量</div>
          <p class="text-xs font-semibold text-ios-label tabular-nums truncate" :title="totalTrafficDisplay">
            {{ totalTrafficDisplay }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">上行</div>
          <p class="text-xs font-semibold text-emerald-600 tabular-nums truncate" :title="formatCompactBytes(uploadBytes)">
            ↑ {{ formatCompactBytes(uploadBytes) }}
          </p>
        </div>

        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-1">
          <div class="text-[10px] sm:text-[11px] text-ios-tertiary font-medium">下行</div>
          <p class="text-xs font-semibold text-ios-blue tabular-nums truncate" :title="formatCompactBytes(downloadBytes)">
            ↓ {{ formatCompactBytes(downloadBytes) }}
          </p>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../../api';
import type { HostMetrics } from '../../metrics';
import { nicRate } from '../../metrics';

const props = defineProps<{
  node: NodeView;
  metrics: HostMetrics;
  now: number;
  today?: number;
  month?: number;
}>();

const nodeAny = computed(() => props.node as unknown as Record<string, unknown>);
const hbAny = computed(() => (props.node.heartbeat || {}) as unknown as Record<string, unknown>);
const capAny = computed(() => (props.node.capabilities || {}) as unknown as Record<string, unknown>);

// 1. CPU & Hardware
const cpuPercent = computed(() => {
  if (typeof props.node.heartbeat?.cpu_percent === 'number') {
    return Math.max(0, Math.min(100, props.node.heartbeat.cpu_percent));
  }
  return 0;
});

const cpuPercentDisplay = computed(() => {
  if (typeof props.node.heartbeat?.cpu_percent === 'number') {
    return `${props.node.heartbeat.cpu_percent.toFixed(1)}%`;
  }
  return '—';
});

const cpuPercentColor = computed(() => {
  const p = cpuPercent.value;
  if (p >= 85) return 'bg-rose-500';
  if (p >= 65) return 'bg-amber-500';
  return 'bg-emerald-500';
});

const cpuModelName = computed(() => {
  const m = (nodeAny.value.cpuModel as string) || (hbAny.value.cpu_model as string);
  return m || '—';
});

const architecture = computed(() => {
  return (nodeAny.value.architecture as string)
    || (capAny.value.architecture as string)
    || (hbAny.value.architecture as string)
    || '—';
});

const kernel = computed(() => {
  return (nodeAny.value.kernelVersion as string)
    || (hbAny.value.kernel_version as string)
    || '—';
});

const gpu = computed(() => {
  return (nodeAny.value.gpu as string)
    || (hbAny.value.gpu as string)
    || '—';
});

// 2. Memory & Storage
const memoryPercent = computed(() => {
  if (typeof props.node.heartbeat?.memory_percent === 'number') {
    return Math.max(0, Math.min(100, props.node.heartbeat.memory_percent));
  }
  return 0;
});

const memoryPercentDisplay = computed(() => {
  if (typeof props.node.heartbeat?.memory_percent === 'number') {
    return `${props.node.heartbeat.memory_percent.toFixed(1)}%`;
  }
  return '—';
});

const memoryPercentColor = computed(() => {
  const p = memoryPercent.value;
  if (p >= 90) return 'bg-rose-500';
  if (p >= 75) return 'bg-amber-500';
  return 'bg-purple-500';
});

function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null || !Number.isFinite(bytes)) return '—';
  if (bytes === 0) return '0 B';
  const k = 1024;
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  const i = Math.floor(Math.log(bytes) / Math.log(k));
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`;
}

function formatCompactBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null || !Number.isFinite(bytes)) return '—';
  if (bytes === 0) return '0 B';
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} K`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} M`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} G`;
}

const memoryDisplay = computed(() => {
  if (typeof hbAny.value.memory_total_bytes === 'number') {
    return formatBytes(hbAny.value.memory_total_bytes as number);
  }
  return '—';
});

const swapDisplay = computed(() => {
  if (typeof hbAny.value.swap_total_bytes === 'number') {
    return formatBytes(hbAny.value.swap_total_bytes as number);
  }
  return '—';
});

const diskDisplay = computed(() => {
  if (typeof hbAny.value.disk_total_bytes === 'number') {
    return formatBytes(hbAny.value.disk_total_bytes as number);
  }
  return '—';
});

// 3. Network & Traffic
const uploadBytes = computed(() => props.node.heartbeat?.upload_bytes);
const downloadBytes = computed(() => props.node.heartbeat?.download_bytes);
const totalBytes = computed(() => uploadBytes.value === undefined || downloadBytes.value === undefined ? undefined : uploadBytes.value + downloadBytes.value);

const uploadRatioPercent = computed(() => {
  if (totalBytes.value === undefined || totalBytes.value <= 0) return 0;
  return Math.round(((uploadBytes.value ?? 0) / totalBytes.value) * 100);
});

const downloadRatioPercent = computed(() => {
  if (totalBytes.value <= 0) return 50;
  return 100 - uploadRatioPercent.value;
});

const totalTrafficDisplay = computed(() => {
  const total = totalBytes.value;
  if (total !== undefined) {
    return formatBytes(total);
  }
  if (props.month !== undefined && props.month > 0) {
    return `${formatBytes(props.month)} (本月)`;
  }
  return '—';
});
</script>
