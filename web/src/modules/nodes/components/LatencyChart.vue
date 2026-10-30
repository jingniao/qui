<template>
  <section class="panel-card p-6 space-y-4">
    <h2 class="font-semibold text-ios-label">网络延迟（24 小时）</h2>
    <p class="text-xs text-ios-secondary">TCP 建连耗时中位数（非 ICMP ping），每 5 分钟检测 3 次</p>
    <p v-if="unsupported" class="text-sm text-ios-secondary">面板版本不支持定时检测</p>
    <template v-else>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <button v-if="error" class="ios-btn-secondary" @click="load">重试</button>
      <p v-if="loading" class="text-sm text-ios-secondary">正在读取延迟数据…</p>
      <p v-else-if="!hasPoints && !error" class="text-sm text-ios-secondary">尚无定时检测数据</p>
      <template v-if="hasPoints">
        <p v-if="meansUnavailable" class="text-sm text-ios-secondary">当前样本未提供耗时，暂无法绘制延迟曲线。</p>
        <div class="flex flex-wrap gap-3">
          <button v-for="(target, index) in data.targets.slice(0, 5)" :key="target.id" class="text-xs rounded-lg px-2 py-1 border border-ios-separator" :class="[colours[index], hidden.includes(target.id) ? 'opacity-40' : '']" :aria-pressed="!hidden.includes(target.id)" @click="toggle(target.id)">● {{ target.label }}</button>
        </div>
        <svg viewBox="0 0 800 280" class="w-full text-ios-tertiary" role="img" :aria-label="`最近24小时 TCP 建连延迟，${visible.length} 个目标；纵轴毫秒，红色条表示失败比例。`">
          <g v-for="tick in ticks" :key="tick">
            <line x1="55" x2="780" :y1="y(tick)" :y2="y(tick)" stroke="currentColor" class="text-ios-separator" />
            <text x="48" :y="y(tick) + 4" text-anchor="end" fill="currentColor" class="text-xs">{{ tick }}</text>
          </g>
          <text x="8" y="15" fill="currentColor" class="text-xs">ms</text>
          <g v-for="tick in times" :key="tick.at">
            <text :x="x(tick.at)" y="272" text-anchor="middle" fill="currentColor" class="text-xs">{{ tick.label }}</text>
          </g>
          <g v-for="target in visible" :key="target.id" :class="colours[data.targets.findIndex(item => item.id === target.id)]">
            <path v-for="(part, index) in paths(target.id)" :key="index" :d="part.map((point, i) => `${i ? 'L' : 'M'}${x(Date.parse(point.at))},${y(latencyMs(point)!)}`).join(' ')" fill="none" stroke="currentColor" stroke-width="2" />
            <g v-for="point in points(target.id)" :key="point.at" tabindex="0" role="button" :aria-label="description(target.label, point)" @mouseenter="hover = { label: target.label, point }" @mouseleave="hover = null" @focus="hover = { label: target.label, point }" @blur="hover = null">
              <circle :cx="x(Date.parse(point.at))" :cy="usable(point) ? y(latencyMs(point)!) : 240" r="5" fill="currentColor" :fill-opacity="usable(point) ? 0.65 : 0" />
              <rect v-if="point.failed > 0" :x="x(Date.parse(point.at)) - 1.5" y="235" width="3" height="12" class="text-ios-red" fill="currentColor" :opacity="0.2 + (loss(point) ?? 0) * 0.8" />
              <title>{{ description(target.label, point) }}</title>
            </g>
          </g>
        </svg>
        <p class="text-xs text-ios-secondary min-h-8" role="status">{{ hover ? description(hover.label, hover.point) : `时间按面板时区 ${authState.timezone}；缺测与全部失败处断线，红条表示检测失败比例。` }}</p>
        <details class="text-xs text-ios-secondary">
          <summary class="cursor-pointer">查看数据表</summary>
          <div class="max-h-64 overflow-auto">
            <table class="w-full">
              <caption>最近24小时延迟样本（毫秒）</caption>
              <thead>
                <tr>
                  <th>目标</th>
                  <th>时间</th>
                  <th>中位耗时</th>
                  <th>失败率</th>
                </tr>
              </thead>
              <tbody>
                <template v-for="target in visible" :key="target.id">
                  <tr v-for="point in points(target.id)" :key="point.at">
                    <td>{{ target.label }}</td>
                    <td>{{ formatTime(point.at) }}</td>
                    <td>{{ ms(latencyMs(point)) }}</td>
                    <td>{{ lossText(point) }}</td>
                  </tr>
                </template>
              </tbody>
            </table>
          </div>
        </details>
      </template>
      <details v-if="isOwner && targetsLoaded" class="text-sm text-ios-secondary" :open="!hasPoints">
        <summary class="cursor-pointer">定时检测目标（最多 5 个，全服务器共用）</summary>
        <p v-if="!targets.length" class="mt-3 text-xs">尚无目标，请先在下方线路检测中添加目标后刷新。</p>
        <label v-for="target in targets" :key="target.id" class="flex items-center gap-2 mt-3 cursor-pointer">
          <input type="checkbox" :checked="target.scheduled" :disabled="saving || (!target.scheduled && targets.filter(t => t.scheduled).length >= 5)" @change="schedule(target, ($event.target as HTMLInputElement))" />
          {{ target.label }}
          ·
          {{ target.host }}
          :
          {{ target.port }}
        </label>
      </details>
      <button class="ios-btn-secondary" :disabled="loading || saving" @click="load">刷新延迟与目标</button>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { nodeAPI, type LatencyResponse, type LatencyPoint, type ProbeTarget } from '../api';
import { APIError } from '../../../app/apiClient';
import { authState, isOwner, formatTime } from '../../../app/auth';
import { segments, niceTicks, scale, hourTicks, loss, usable, latencyMs } from './latencySeries';
const props = defineProps<{ nodeId: string }>();
const data = ref<LatencyResponse>({ intervalSeconds: 300, targets: [], series: [] });
const targets = ref<ProbeTarget[]>([]), hidden = ref<string[]>([]);
const targetsLoaded = ref(false);
const error = ref(''), unsupported = ref(false), loading = ref(false), saving = ref(false);
const end = ref(Date.now()), start = computed(() => end.value - 86400000);
const hover = ref<{ label: string; point: LatencyPoint } | null>(null);
const colours = ['text-ios-blue', 'text-ios-green', 'text-ios-orange', 'text-ios-purple', 'text-ios-label'];
const visible = computed(() => data.value.targets.slice(0, 5).filter(target => !hidden.value.includes(target.id)));
const points = (id: string) => (data.value.series.find(series => series.targetId === id)?.points ?? []).filter(point => Date.parse(point.at) >= start.value && Date.parse(point.at) <= end.value);
const hasPoints = computed(() => data.value.targets.some(target => points(target.id).length));
const meansUnavailable = computed(() => hasPoints.value && !data.value.targets.some(target => points(target.id).some(usable)) && data.value.targets.some(target => points(target.id).some(point => point.sent > point.failed)));
const ticks = computed(() => niceTicks(Math.max(0, ...visible.value.flatMap(target => points(target.id).filter(usable).map(point => latencyMs(point)!)))));
const times = computed(() => hourTicks(start.value, end.value, authState.timezone));
const x = (at: number) => scale(at, start.value, end.value, 55, 780);
const y = (ms: number) => scale(ms, 0, ticks.value[ticks.value.length - 1]!, 220, 20);
const paths = (id: string) => segments(points(id), data.value.intervalSeconds, start.value, end.value);
const ms = (value: number | null) => value === null ? '—' : `${value.toFixed(2)} ms`;
const lossText = (point: LatencyPoint) => loss(point) === null ? '—' : `${((loss(point) ?? 0) * 100).toFixed(1)}%`;
const description = (label: string, point: LatencyPoint) => `${label} · ${formatTime(point.at)} · 中位 ${ms(latencyMs(point))} · 失败率 ${lossText(point)}`;
function toggle(id: string) { hidden.value = hidden.value.includes(id) ? hidden.value.filter(value => value !== id) : [...hidden.value, id]; }
let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true; error.value = ''; unsupported.value = false; hover.value = null;
  let receivedLatency = false;
  try {
    const result = await nodeAPI.latency(props.nodeId);
    if (current !== generation) return;
    data.value = result; end.value = Date.now(); receivedLatency = true;
    if (isOwner.value) {
      const result = await nodeAPI.probeTargets();
      if (current === generation) { targets.value = result.items; targetsLoaded.value = true; }
    }
  } catch (e) {
    if (current !== generation) return;
    if (e instanceof APIError && e.status === 404 && !receivedLatency) unsupported.value = true;
    else error.value = (e as Error).message;
  } finally { if (current === generation) loading.value = false; }
}
async function schedule(target: ProbeTarget, input: HTMLInputElement) {
  const scheduled = input.checked;
  input.checked = target.scheduled;
  saving.value = true; error.value = '';
  try { await nodeAPI.saveProbeTarget({ ...target, scheduled }); await load(); }
  catch (e) { error.value = (e as Error).message; }
  finally { saving.value = false; }
}
watch(() => props.nodeId, () => { data.value = { intervalSeconds: 300, targets: [], series: [] }; hidden.value = []; void load(); }, { immediate: true });
onUnmounted(() => { generation++; });
</script>
