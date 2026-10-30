<template>
  <div class="space-y-4">
    <section class="panel-card p-4 space-y-3">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <h2 class="font-semibold text-ios-label">网络与内核调优</h2>
        <button type="button" class="ios-btn-secondary" :disabled="loading" @click="refresh">刷新状态</button>
      </div>
      <p v-if="loading" class="text-sm text-ios-secondary">正在读取调优状态…</p>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <template v-if="state">
        <p class="text-sm text-ios-label">{{ state.supported ? 'Agent 支持调优' : 'Agent 不支持调优' }} · {{ state.requestId ? tuningStatus(state.status) : '暂无调优请求' }}</p>
        <p class="text-xs text-ios-secondary">当前拥塞控制：{{ state.current['net.ipv4.tcp_congestion_control'] || '—' }}。当前值来自最近心跳，可能过期；最后心跳：{{ time(node.lastHeartbeat) }}。</p>
        <p class="text-xs text-ios-secondary">请求时间：{{ time(state.requestedAt) }} · 更新时间：{{ time(state.updatedAt) }}</p>
        <p v-if="state.status === 'unconfirmed' && state.requestId" class="text-sm text-ios-orange">执行结果未确认，操作可能已生效。请检查下一次心跳的当前值；系统不会自动重发。</p>
        <p v-if="pollExpired" class="text-sm text-ios-orange">已等待约 60 秒，仍未收到最终状态。请稍后刷新并核对当前值，勿重复提交。</p>
      </template>
    </section>
    <section v-if="state" class="panel-card p-4 space-y-3">
      <h3 class="font-semibold text-ios-label">白名单参数</h3>
      <p class="text-xs text-ios-secondary">草稿尚未应用。提交会替换整个期望配置，留空的旧参数将恢复初始值。算法是否可用由服务器校验；tcp_rmem / tcp_wmem 只提交最大值，Agent 保留前两项。</p>
      <div v-if="canEdit" class="flex flex-wrap gap-2">
        <button v-for="preset in tuningPresets" :key="preset.name" type="button" class="ios-btn-secondary" @click="draft = { ...preset.settings }">{{ preset.name }}</button>
        <button type="button" class="ios-btn-secondary" @click="draft = { ...state.desired }">载入期望配置为草稿</button>
      </div>
      <div class="overflow-x-auto">
        <table class="w-full text-xs text-left">
          <thead class="text-ios-secondary"><tr><th class="p-2">参数</th><th class="p-2">当前实测</th><th class="p-2">服务端期望</th><th class="p-2">草稿</th></tr></thead>
          <tbody>
            <tr v-for="field in tuningFields" :key="field.key" class="border-t border-ios-separator">
              <td class="p-2 text-ios-label">{{ field.label }}<p class="font-mono text-ios-tertiary">{{ field.key }}</p></td>
              <td class="p-2 font-mono text-ios-label">{{ state.current[field.key] || '—' }}</td>
              <td class="p-2 font-mono text-ios-label">{{ state.desired[field.key] || '—' }}</td>
              <td class="p-2">
                <select v-if="field.choices" v-model="draft[field.key]" class="form-input-base" :disabled="!canEdit"><option value="">不设置</option><option v-for="value in field.choices" :key="value" :value="value">{{ value }}</option></select>
                <input v-else v-model="draft[field.key]" type="text" inputmode="numeric" pattern="[0-9]*" :placeholder="field.min + '–' + field.max" :aria-label="field.label" class="form-input-base" :disabled="!canEdit" />
                <p class="text-ios-tertiary mt-1">{{ field.choices?.join(' / ') || field.min + '–' + field.max }}</p>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="owner" class="flex gap-2">
        <button type="button" class="ios-btn-primary" :disabled="!canEdit" @click="confirm(false)">一键应用</button>
        <button type="button" class="ios-btn-danger" :disabled="!canEdit" @click="confirm(true)">清除调优</button>
      </div>
      <p v-else class="text-xs text-ios-secondary">Viewer 只读</p>
    </section>
    <section v-if="state?.result" class="panel-card p-4 space-y-3">
      <h3 class="font-semibold text-ios-label">最近执行回执 · {{ tuningStatus(state.result.status || state.status) }}</h3>
      <p v-for="(result, index) in state.result.keys || []" :key="index" class="text-xs text-ios-label break-words">
        {{ result.key || '全局错误' }} · {{ result.applied ? '已应用' : '失败' }} · 读回值：{{ result.value || '—' }} <span v-if="result.error" class="text-ios-red">{{ result.error }}</span>
      </p>
    </section>
    <ConfirmDialog v-model="dialog" :title="clearing ? '确认清除调优？' : '确认应用调优？'" :message="confirmMessage" confirm-text="确认发送" :is-danger="clearing" @confirm="submit" />
  </div>
</template>
<script setup lang="ts">
import { formatTime } from '../../../../app/auth';
import { computed, ref, watch, onUnmounted } from 'vue';
import { isOwner as sessionOwner } from '../../../../app/auth';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import { nodeAPI, type NodeView, type NodeTuning, type NodeTuningInput } from '../../api';
import { tuningFields, tuningStatus, tuningRequest, tuningPresets } from './serverTuningModel';
const props = defineProps<{node:NodeView;isOwner?:boolean}>();
const owner = computed(() => sessionOwner.value && props.isOwner !== false);
const state = ref<NodeTuning | null>(null), draft = ref<Record<string,string>>({});
const loading = ref(false), sending = ref(false), error = ref(''), dialog = ref(false), clearing = ref(false), pollExpired = ref(false);
const canEdit = computed(() => owner.value && state.value?.supported === true && state.value.status !== 'pending' && !loading.value && !sending.value);
let lifecycle = 0, reads = 0, timer: ReturnType<typeof setTimeout> | undefined, deadline = 0;
let confirmed: NodeTuningInput | null = null;
const confirmMessage = computed(() => '将向服务器【' + props.node.name + '】的 Agent 发送' + (clearing.value ? '清除请求。root helper 将恢复初始快照并移除 /etc/sysctl.d/99-proxy-tuning.conf。' : '所确认的完整配置。root helper 将应用参数并持久化至 /etc/sysctl.d/99-proxy-tuning.conf。') + '代理核心不会重启。请以执行回执和后续心跳为准。');
function time(value?:string|null) { return value ? formatTime(value) : '—'; }
function stopPoll() { clearTimeout(timer); timer = undefined; }
function schedulePoll() {
  stopPoll();
  if (state.value?.status !== 'pending') return;
  if (Date.now() >= deadline) { pollExpired.value = true; return; }
  timer = setTimeout(() => void refresh(), 3000);
}
async function refresh() {
  const cycle = lifecycle, ticket = ++reads, id = props.node.id;
  loading.value = true; error.value = '';
  try {
    const result = await nodeAPI.tuning(id);
    if (cycle !== lifecycle || ticket !== reads) return;
    const initial = state.value === null;
    state.value = result;
    if (initial) draft.value = {...result.desired};
  } catch(e) { if (cycle === lifecycle && ticket === reads) error.value = '调优状态读取失败：' + (e as Error).message; }
  finally { if (cycle === lifecycle && ticket === reads) { loading.value = false; schedulePoll(); } }
}
function confirm(clear:boolean) {
  if (!canEdit.value) return;
  try { confirmed = tuningRequest(props.node.id,draft.value,clear); clearing.value = clear; dialog.value = true; error.value = ''; }
  catch(e) { error.value = (e as Error).message; }
}
async function submit() {
  if (!canEdit.value || !confirmed || confirmed.confirmNodeId !== props.node.id) return;
  const input = confirmed, cycle = lifecycle;
  sending.value = true; error.value = ''; stopPoll();
  try {
    const result = await nodeAPI.saveTuning(props.node.id,input);
    if (cycle !== lifecycle) return;
    if (state.value) state.value = {...state.value,desired:input.settings,requestId:result.requestId,status:result.status};
    deadline = Date.now()+60000; pollExpired.value = false;
    await refresh();
  } catch(e) { if (cycle === lifecycle) { error.value = '请求失败（服务端为最终校验）：' + (e as Error).message; } }
  finally { if (cycle === lifecycle) sending.value = false; }
}
watch(() => props.node.lastHeartbeat, () => {
  if (state.value && state.value.status !== 'pending' && !sending.value && !loading.value) void refresh();
});
watch(() => props.node.id, () => {
  lifecycle++; reads++; stopPoll(); state.value = null; draft.value = {}; sending.value = false; dialog.value = false; confirmed = null; pollExpired.value = false; deadline = Date.now()+60000; void refresh();
}, {immediate:true});
onUnmounted(() => { lifecycle++; stopPoll(); });
</script>
