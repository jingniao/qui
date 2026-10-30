<template>
  <div class="space-y-6">
    <h2 class="font-semibold text-ios-label">线路检测</h2>
    <p class="text-xs text-ios-secondary">手动检测接口未提供平均耗时与检测时间时显示“—” ，中位数见历史。由服务器连接指定目标。TCP 失败比例不代表 ICMP 或 UDP 丢包；每目标保留最近 100 次手动检测记录。</p>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <p v-if="!isOwner" class="text-sm text-ios-secondary">当前权限无法读取或管理探测目标，请联系 Owner。</p>
    <template v-else>
      <div class="flex flex-wrap gap-3">
        <label class="text-xs text-ios-secondary">
          次数
          <input v-model.number="attempts" type="number" min="1" max="10" class="form-input-base" />
        </label>
        <label class="text-xs text-ios-secondary">
          每次超时（ms）
          <input v-model.number="timeoutMs" type="number" min="1" max="3000" class="form-input-base" />
        </label>
        <button class="ios-btn-secondary self-end" :disabled="busy" @click="refresh">刷新结果</button>
      </div>
      <DataTable v-if="targets.length">
        <template #header>
          <th>目标</th>
          <th>最近结果</th>
          <th>失败比例</th>
          <th>平均耗时</th>
          <th>时间</th>
          <th>操作</th>
        </template>
        <template #body>
          <tr v-for="target in targets" :key="target.id">
            <td>
              {{ target.label }}
              <p class="text-xs font-mono text-ios-secondary">{{ target.host }}:{{ target.port }}</p>
            </td>
            <td>{{ rowLabel(latest(target.id)) }}</td>
            <td class="font-mono tabular-nums">{{ lossLabel(latest(target.id)) }}</td>
            <td class="font-mono tabular-nums">{{ avg(latest(target.id)) }}</td>
            <td class="text-xs font-mono tabular-nums">{{ latest(target.id)?.createdAt ? formatTime(latest(target.id)!.createdAt!) : '—' }}</td>
            <td>
              <div class="flex gap-2">
                <button class="ios-action-btn-primary" :disabled="busy" @click="run(target.id)">立即检测</button>
                <button class="ios-action-btn-secondary" @click="historyTarget = target.id">历史</button>
                <button class="ios-action-btn-danger" :disabled="busy" @click="deleting = target">删除</button>
              </div>
            </td>
          </tr>
        </template>
      </DataTable>
      <p v-else-if="!busy && !error" class="text-sm text-ios-secondary">尚无探测目标。</p>
      <details class="text-sm text-ios-secondary">
        <summary class="cursor-pointer">添加探测目标</summary>
        <div class="mt-3 max-w-xl">
          <ProbeTargetForm :busy="busy" inline @submit="add" />
        </div>
      </details>
      <details v-if="historyTarget" open class="text-sm text-ios-secondary">
        <summary>检测历史 · {{ targets.find(t => t.id === historyTarget)?.label }}</summary>
        <p v-for="row in history[historyTarget]" :key="row.id" class="mt-2 text-xs font-mono tabular-nums">{{ row.createdAt ? formatTime(row.createdAt) : '—' }} · {{ rowLabel(row) }} · 失败 {{ lossLabel(row) }} · 平均 {{ avg(row) }} · 中位数 {{ row.result?.median_ms == null ? '—' : `${row.result.median_ms.toFixed(2)} ms` }}</p>
      </details>
      <ConfirmDialog :model-value="!!deleting" title="删除探测目标" :message="`删除 ${deleting?.label || ''}？目标为全部服务器共用。`" @update:model-value="value => { if (!value) deleting = null; }" @confirm="remove" />
    </template>
  </div>
</template>
<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue';
import { request } from '../../app/apiClient';
import { isOwner, formatTime } from '../../app/auth';
import { nodeAPI, type ProbeTarget } from './api';
import DataTable from '../../components/DataTable.vue';
import ConfirmDialog from '../../components/ConfirmDialog.vue';
import ProbeTargetForm from './components/ProbeTargetForm.vue';

const props = defineProps<{ nodeId: string }>();
interface Row { id: string; status: string; createdAt?: string; result?: { attempted?: number; lost?: number; median_ms?: number; avg_ms?: number; attempts?: { latency_ms?: number; success?: boolean }[] }; }
const targets = ref<ProbeTarget[]>([]), history = ref<Record<string, Row[]>>({}), historyTarget = ref(''), deleting = ref<ProbeTarget | null>(null);
const error = ref(''), busy = ref(false), attempts = ref(3), timeoutMs = ref(1000);
const latest = (id: string) => history.value[id]?.[0];
const rowLabel = (row?: Row) => row ? ({ queued: '等待检测', complete: '已完成', expired: '已过期', failed: '失败' }[row.status] || row.status) : '—';
const lossLabel = (row?: Row) => row?.result?.attempted ? `${((row.result.lost ?? 0) / row.result.attempted * 100).toFixed(1)}%` : '—';
// Older probe results contain only a median; do not relabel it as an average.
const avg = (row?: Row) => row?.result?.avg_ms == null ? '—' : `${row.result.avg_ms.toFixed(2)} ms`;
let generation = 0;
async function readHistory(id: string, nodeId: string) { return (await request<{ items: Row[] }>(`/nodes/${encodeURIComponent(nodeId)}/tcp-probes?targetId=${encodeURIComponent(id)}`)).items; }
async function refresh() {
  const current = ++generation, nodeId = props.nodeId;
  if (!isOwner.value) return;
  busy.value = true; error.value = '';
  try {
    const result = await nodeAPI.probeTargets();
    const records = await Promise.all(result.items.map(async t => [t.id, await readHistory(t.id, nodeId)] as const));
    if (current !== generation) return;
    targets.value = result.items; history.value = Object.fromEntries(records);
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) busy.value = false; }
}
async function guard(action: () => Promise<void>) {
  if (busy.value) return;
  busy.value = true; error.value = '';
  try { await action(); await refresh(); }
  catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
function add(targetData: { label: string; host: string; port: number; allowNonPublic: boolean }) {
  void guard(async () => { await request('/probe-targets', 'POST', targetData); });
}
function remove() { const id = deleting.value?.id; if (id) void guard(async () => { await request(`/probe-targets/${encodeURIComponent(id)}`, 'DELETE'); deleting.value = null; historyTarget.value = ''; }); }
function run(targetId: string) {
  if (!Number.isInteger(attempts.value) || attempts.value < 1 || attempts.value > 10 || !Number.isInteger(timeoutMs.value) || timeoutMs.value < 1 || timeoutMs.value > 3000) { error.value = '次数需为 1～10，超时需为 1～3000 ms 的整数'; return; }
  void guard(async () => { await request(`/nodes/${encodeURIComponent(props.nodeId)}/tcp-probes`, 'POST', { targetId, attempts: attempts.value, timeoutMs: timeoutMs.value }); historyTarget.value = targetId; });
}
watch(() => props.nodeId, () => { history.value = {}; targets.value = []; historyTarget.value = ''; void refresh(); }, { immediate: true });
onUnmounted(() => { generation++; });
</script>
