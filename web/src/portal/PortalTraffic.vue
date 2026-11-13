<template>
  <section class="panel-card p-6 space-y-4">
    <div class="flex flex-wrap justify-between gap-2">
      <h2 class="text-base font-semibold leading-normal">流量趋势</h2>
      <select v-if="!preview" :value="range" class="form-input-base max-w-40" @change="emit('range', ($event.target as HTMLSelectElement).value as Range)">
        <option value="24h">最近 24 小时</option><option value="7d">最近 7 天</option><option value="30d">最近 30 天</option>
      </select>
    </div>
    <p v-if="preview" class="text-ios-secondary">只读预览仅提供账号、订阅和节点；流量历史请在用户详情查看。</p>
    <p v-else-if="!traffic.items.length" class="text-ios-tertiary">暂无流量记录</p>
    <template v-else>
      <p class="text-sm">合计 <b>{{ formatBytes(up + down) }}</b>（上传 {{ formatBytes(up) }} · 下载 {{ formatBytes(down) }}）</p>
      <div class="relative h-56 w-full"><canvas ref="canvas" role="img" :aria-label="range === '24h' ? '每小时上传和下载流量' : '每日上传和下载流量'"></canvas></div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue';
// Chart.js loads with the first chart, as on the admin side.
import { BarController, BarElement, CategoryScale, Chart, Legend, LinearScale, Tooltip } from 'chart.js';
import type { Traffic } from './api';
import { formatBytes } from './format';
Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip, Legend);
// Same palette as the admin charts (serverMetricsModel.CHART_COLORS); the portal
// must not import admin modules, so the values are mirrored here.
const CHART_COLORS = { blue: '#0071E3', green: '#34C759', tick: '#6E6E73', grid: 'rgba(0, 0, 0, 0.08)', tooltipBg: '#1D1D1F' } as const;
type Range = '24h' | '7d' | '30d';
const props = defineProps<{ traffic: Traffic; range: Range; preview?: boolean }>();
const emit = defineEmits<{ (e: 'range', value: Range): void }>();
const canvas = ref<HTMLCanvasElement>();
let chart: Chart | undefined;
const up = computed(() => props.traffic.items.reduce((s, i) => s + i.uploadBytes, 0));
const down = computed(() => props.traffic.items.reduce((s, i) => s + i.downloadBytes, 0));
const FONT = { size: 12 };
function label(bucket: string) { return props.range === '24h' ? bucket.slice(-8, -3) || bucket : bucket.slice(5, 10) || bucket; }
function render() {
  chart?.destroy(); chart = undefined;
  if (!canvas.value || props.preview || !props.traffic.items.length) return;
  chart = new Chart(canvas.value, {
    type: 'bar',
    data: {
      labels: props.traffic.items.map(i => label(i.bucket)),
      datasets: [
        { label: '上传', data: props.traffic.items.map(i => i.uploadBytes), backgroundColor: CHART_COLORS.blue, stack: 's' },
        { label: '下载', data: props.traffic.items.map(i => i.downloadBytes), backgroundColor: CHART_COLORS.green, stack: 's' }
      ]
    },
    options: {
      responsive: true, maintainAspectRatio: false, animation: false,
      interaction: { mode: 'index', intersect: false },
      scales: {
        x: { stacked: true, grid: { display: false }, border: { display: false }, ticks: { color: CHART_COLORS.tick, font: FONT, maxRotation: 0, autoSkip: true, maxTicksLimit: 8 } },
        y: { stacked: true, beginAtZero: true, grid: { color: CHART_COLORS.grid }, border: { display: false }, ticks: { color: CHART_COLORS.tick, font: FONT, maxTicksLimit: 5, callback: v => formatBytes(Number(v), 1) } }
      },
      plugins: {
        legend: { position: 'top', labels: { color: CHART_COLORS.tick, font: FONT, usePointStyle: true, boxWidth: 8, boxHeight: 8 } },
        tooltip: {
          backgroundColor: CHART_COLORS.tooltipBg, titleFont: FONT, bodyFont: FONT, usePointStyle: true,
          callbacks: {
            title: items => props.traffic.items[items[0].dataIndex]?.bucket ?? '',
            label: item => `${item.dataset.label}: ${formatBytes(item.parsed.y as number)}`
          }
        }
      }
    }
  });
}
watch(() => [props.traffic, props.range], () => void nextTick(render), { immediate: true, flush: 'post' });
onUnmounted(() => { chart?.destroy(); chart = undefined; });
</script>
