<template>
  <div class="relative h-44 w-full"><canvas ref="canvas" role="img" :aria-label="label"></canvas></div>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue';
import { Chart, Filler, Legend, LinearScale, LineController, LineElement, PointElement, Tooltip, type ChartConfiguration } from 'chart.js';
import { CHART_COLORS, formatAxisTick, formatTimeFull, formatXTick, type AxisUnit } from './serverMetricsModel';

Chart.register(LineController, LineElement, PointElement, LinearScale, Filler, Tooltip, Legend);

export interface ChartDataset { label: string; color: string; fill: boolean; axis: 'y' | 'y1'; data: { x: number; y: number | null }[]; format: (value: number) => string }
const props = defineProps<{
  label: string; datasets: ChartDataset[]; xMin: number; xMax: number;
  axes: { y: { unit: AxisUnit; max?: number }; y1?: { unit: AxisUnit } };
}>();

const FONT = { size: 12 };
const canvas = ref<HTMLCanvasElement>();
let chart: Chart | undefined;

function yScale(axis: { unit: AxisUnit; max?: number }, right: boolean) {
  return {
    type: 'linear' as const, position: right ? 'right' as const : 'left' as const, beginAtZero: true, min: 0, max: axis.max,
    grid: { color: CHART_COLORS.grid, drawOnChartArea: !right },
    border: { display: false },
    ticks: { color: CHART_COLORS.tick, font: FONT, maxTicksLimit: 5, callback: (v: string | number) => formatAxisTick(axis.unit, Number(v)) }
  };
}

function build(): ChartConfiguration<'line'> {
  const scales: Record<string, object> = {
    x: {
      type: 'linear', min: props.xMin, max: props.xMax,
      grid: { color: CHART_COLORS.grid }, border: { display: false },
      ticks: { color: CHART_COLORS.tick, font: FONT, maxRotation: 0, autoSkip: true, maxTicksLimit: 6, callback: (v: string | number) => formatXTick(Number(v), props.xMax - props.xMin) }
    },
    y: yScale(props.axes.y, false)
  };
  if (props.axes.y1) scales.y1 = yScale(props.axes.y1, true);
  const formats = props.datasets.map(d => d.format);
  return {
    type: 'line',
    data: {
      datasets: props.datasets.map(d => ({
        label: d.label, data: d.data, yAxisID: d.axis, borderColor: d.color, backgroundColor: d.color + '26',
        fill: d.fill ? 'origin' : false, borderWidth: 1.5, pointRadius: 0, pointHoverRadius: 3,
        cubicInterpolationMode: 'monotone', spanGaps: false
      }))
    },
    options: {
      responsive: true, maintainAspectRatio: false, animation: false,
      interaction: { mode: 'index', intersect: false },
      scales,
      plugins: {
        legend: { display: props.datasets.length > 1, position: 'top', labels: { color: CHART_COLORS.tick, font: FONT, usePointStyle: true, boxWidth: 8, boxHeight: 8 } },
        tooltip: {
          backgroundColor: CHART_COLORS.tooltipBg, titleFont: FONT, bodyFont: FONT, usePointStyle: true,
          filter: item => item.parsed.y !== null,
          callbacks: {
            title: items => items.length ? formatTimeFull(items[0].parsed.x as number) : '',
            label: item => `${item.dataset.label}: ${formats[item.datasetIndex](item.parsed.y as number)}`
          }
        }
      }
    }
  };
}

onMounted(() => { chart = new Chart(canvas.value!, build()); });
watch(() => [props.datasets, props.xMin, props.xMax, props.axes], () => {
  if (!chart) return;
  const next = build();
  chart.data = next.data;
  chart.options = next.options!;
  chart.update('none');
});
onUnmounted(() => { chart?.destroy(); chart = undefined; });
</script>
