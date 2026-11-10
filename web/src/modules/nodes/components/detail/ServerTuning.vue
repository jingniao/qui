<template>
  <div class="space-y-4">
    <!-- 1. 顶部状态卡 (Status Board) -->
    <section class="panel-card p-4 space-y-4">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <div class="flex items-center gap-2">
          <h2 class="text-ios-label text-base font-semibold leading-normal">调优状态</h2>
          <span v-if="state?.supported" class="px-2 py-0.5 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200/60">Agent 支持</span>
          <span v-else class="px-2 py-0.5 rounded-full text-xs font-semibold bg-neutral-100 text-neutral-500">Agent 不支持</span>
          <span v-if="persistAcrossReboot" class="px-2 py-0.5 rounded-full text-xs font-semibold bg-blue-50 text-blue-700 border border-blue-200/60">重启后保持</span>
        </div>
        <button type="button" class="ios-btn-secondary" :disabled="loading" @click="refresh">刷新状态</button>
      </div>

      <!-- Key metrics tiles in status board -->
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-2.5 text-xs">
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">当前拥塞控制</span>
          <p class="font-mono font-semibold text-ios-label truncate">{{ state?.current?.['net.ipv4.tcp_congestion_control'] || '—' }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">当前排队算法</span>
          <p class="font-mono font-semibold text-ios-label truncate">{{ state?.current?.['net.core.default_qdisc'] || '—' }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">虚拟化环境</span>
          <p class="font-semibold text-ios-label truncate">{{ virtualization }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">Agent 文件句柄 (软/硬)</span>
          <p class="font-mono font-semibold text-ios-label truncate">{{ agentFdLimits }}</p>
        </div>
      </div>

      <!-- Status counts summary pill bar -->
      <div class="flex flex-wrap items-center gap-2 text-xs">
        <span class="text-neutral-400">参数状态统计：</span>
        <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md font-medium bg-emerald-50 text-emerald-700 border border-emerald-200/60">面板管理: {{ statusCounts.panel }}</span>
        <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md font-medium bg-amber-50 text-amber-700 border border-amber-200/60">偏离/漂移: {{ statusCounts.drift }}</span>
        <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md font-medium bg-blue-50 text-blue-700 border border-blue-200/60">外部配置: {{ statusCounts.external }}</span>
        <span class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md font-medium bg-black/[0.04] text-neutral-600 border border-black/[0.06]">系统默认: {{ statusCounts.default }}</span>
        <span v-if="statusCounts.unavailable > 0" class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md font-medium bg-neutral-100 text-neutral-400 border border-neutral-200">不可用: {{ statusCounts.unavailable }}</span>
      </div>

      <!-- LXC notice -->
      <div v-if="isLXC" class="bg-blue-50/60 border border-blue-200/50 rounded-xl p-2.5 text-xs text-blue-800">
        💡 提示：容器内看不到 net.core.*，由宿主决定。
      </div>

      <!-- Last application result & sync info -->
      <div class="text-xs text-ios-secondary space-y-1 pt-1 border-t border-black/[0.04]">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span>最后执行结果：<strong class="font-medium text-ios-label">{{ state?.requestId ? tuningStatus(state.status) : '暂无调优请求' }}</strong></span>
          <span>心跳上报：{{ time(node.lastHeartbeat) }}</span>
        </div>
        <p v-if="state?.status === 'unconfirmed' && state.requestId" class="text-ios-orange">执行结果未确认，操作可能已生效。请检查下一次心跳的当前值；系统不会自动重发。</p>
        <p v-if="pollExpired" class="text-ios-orange">已等待约 60 秒，仍未收到最终状态。请稍后刷新并核对当前值，勿重复提交。</p>
      </div>
    </section>

    <!-- 2. 推荐卡 (Recommendations Card) -->
    <section v-if="state?.supported" class="panel-card p-4 space-y-3">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <div class="flex items-center gap-2">
          <h3 class="text-ios-label text-base font-semibold leading-normal">调优建议</h3>
          <span v-if="recommendations?.rulesVersion" class="text-xs text-neutral-400 font-mono">规则版本 {{ recommendations.rulesVersion }}</span>
          <span v-if="activeRecommendations.length > 0" class="px-1.5 py-0.5 rounded text-xs font-semibold bg-amber-500/10 text-amber-600 border border-amber-500/20">{{ activeRecommendations.length }} 项待采纳</span>
        </div>
        <div v-if="activeRecommendations.length > 0 && canEdit" class="flex items-center gap-2">
          <button type="button" class="ios-action-btn-primary text-xs px-2.5 py-1" :disabled="!selectedAdviceKeys.length" @click="adoptSelectedAdvice">采用所选 ({{ selectedAdviceKeys.length }})</button>
          <button type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="toggleSelectAllAdvice">{{ allAdviceSelected ? '取消全选' : '全选建议' }}</button>
        </div>
      </div>

      <div v-if="activeRecommendations.length > 0" class="space-y-2">
        <div v-for="item in activeRecommendations" :key="item.key" class="rounded-xl border border-black/[0.06] p-3 text-xs bg-black/[0.01] flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div class="flex items-start gap-2.5 min-w-0">
            <input v-if="canEdit" type="checkbox" :value="item.key" v-model="selectedAdviceKeys" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-ios-blue focus:ring-ios-blue shrink-0" />
            <div class="space-y-0.5 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-semibold text-ios-label">{{ fieldLabel(item.key) }}</span>
                <span class="font-mono text-neutral-400 text-xs">{{ item.key }}</span>
                <span class="font-mono font-semibold text-emerald-600 bg-emerald-50 px-1.5 py-0.2 rounded border border-emerald-200/50">推荐值：{{ item.value }}</span>
                <span :class="adviceLevelClass(item.level)" class="px-1.5 py-0.2 rounded text-xs font-medium">{{ adviceLevelLabel(item.level) }}</span>
              </div>
              <p class="text-neutral-600">{{ item.reason }}</p>
            </div>
          </div>
          <div v-if="canEdit" class="flex items-center gap-2 shrink-0 self-end sm:self-auto">
            <button type="button" class="ios-action-btn-secondary text-xs px-2 py-0.5" @click="applySingleAdvice(item)">填入草稿</button>
            <button type="button" class="text-xs text-neutral-400 hover:text-rose-600 transition-colors" @click="dismissAdvice(item)">不采纳</button>
          </div>
        </div>
      </div>
      <p v-else class="text-xs text-neutral-400 py-2">暂无调优建议（当前参数配置已较优，或未匹配到需要调优的规则）。</p>
    </section>

    <!-- 3. 参数表与操作 (Parameter Table) -->
    <section v-if="state" class="panel-card p-4 space-y-4">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <h3 class="text-ios-label text-base font-semibold leading-normal">白名单参数</h3>
        <div v-if="canEdit" class="flex flex-wrap items-center gap-2">
          <button type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="adoptExternalToDraft" title="将未通过面板设置的外部 sysctl 配置并入草稿">纳入面板管理</button>
          <button type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="draft = tuningDraft(state.desired)">载入期望配置为草稿</button>
        </div>
      </div>

      <!-- Presets -->
      <div v-if="canEdit" class="flex flex-wrap items-center gap-2 text-xs">
        <span class="text-neutral-400">场景预设：</span>
        <button v-for="preset in tuningPresets" :key="preset.name" type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="draft = applyPreset(draft, preset, availableKeys)">{{ preset.name }}</button>
      </div>

      <p class="text-xs text-ios-secondary leading-relaxed">
        草稿尚未应用。提交会替换整个期望配置，留空的旧参数将恢复初始值。算法是否可用由服务器校验；tcp_rmem / tcp_wmem 只提交最大值，Agent 保留前两项。
      </p>

      <!-- 重点参数 (Essential Keys) -->
      <div class="space-y-2">
        <div class="flex items-center justify-between text-xs font-semibold text-neutral-700">
          <span>重点参数（常用性能关键项）</span>
        </div>
        <div class="overflow-x-auto">
          <table class="w-full text-xs text-left">
            <thead class="text-ios-secondary bg-black/[0.02]">
              <tr>
                <th class="p-2">参数</th>
                <th class="p-2">当前</th>
                <th class="p-2">状态</th>
                <th class="p-2">来源</th>
                <th class="p-2">初始值</th>
                <th class="p-2">期望</th>
                <th class="p-2">推荐</th>
                <th class="p-2 min-w-[140px]">草稿</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="field in essentialFields" :key="field.key" class="border-t border-ios-separator">
                <td class="p-2 text-ios-label">
                  <div class="font-medium">{{ field.label }}</div>
                  <div class="font-mono text-ios-tertiary text-xs">{{ field.key }}</div>
                  <div class="text-neutral-400 text-xs mt-0.5 max-w-xs">{{ field.help }}</div>
                </td>
                <td class="p-2 font-mono text-ios-label">{{ state.current?.[field.key] || '—' }}</td>
                <td class="p-2">
                  <span class="px-1.5 py-0.5 rounded text-xs font-medium inline-block" :class="statusClass(getFieldStatus(field.key))">
                    {{ statusText(getFieldStatus(field.key)) }}
                  </span>
                </td>
                <td class="p-2 font-mono text-ios-secondary text-xs">{{ keySource(field.key) }}</td>
                <td class="p-2 font-mono text-ios-secondary">{{ state.initial?.[field.key] || '—' }}</td>
                <td class="p-2 font-mono text-ios-label">{{ state.desired?.[field.key] || '—' }}</td>
                <td class="p-2">
                  <div v-if="adviceByKey.has(field.key)" class="flex items-center gap-1">
                    <span class="font-mono font-semibold text-emerald-600">{{ adviceByKey.get(field.key)!.value }}</span>
                    <button v-if="canEdit" type="button" class="text-xs text-ios-blue hover:underline" @click="applySingleAdvice(adviceByKey.get(field.key)!)">采用</button>
                  </div>
                  <span v-else class="text-neutral-400">—</span>
                </td>
                <td class="p-2">
                  <select v-if="field.choices || isAlgorithmKey(field.key)" v-model="draft[field.key]" class="form-input-base" :disabled="!canEdit || !isKeyAvailable(field.key)">
                    <option value="">不设置</option>
                    <option v-for="opt in fieldOptions(field)" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                  </select>
                  <input v-else v-model="draft[field.key]" type="text" inputmode="numeric" pattern="[0-9]*" :placeholder="field.min + '–' + field.max" :aria-label="field.label" class="form-input-base" :disabled="!canEdit || !isKeyAvailable(field.key)" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <!-- 高级参数 (Advanced Keys) -->
      <details class="rounded-xl border border-black/[0.05] p-3 bg-black/[0.01] space-y-2">
        <summary class="cursor-pointer text-xs font-medium text-ios-secondary hover:text-ios-blue transition-colors select-none">
          高级参数（{{ advancedFields.length }} 项）
        </summary>
        <div class="pt-2 overflow-x-auto">
          <table class="w-full text-xs text-left">
            <thead class="text-ios-secondary bg-black/[0.02]">
              <tr>
                <th class="p-2">参数</th>
                <th class="p-2">当前</th>
                <th class="p-2">状态</th>
                <th class="p-2">来源</th>
                <th class="p-2">初始值</th>
                <th class="p-2">期望</th>
                <th class="p-2">推荐</th>
                <th class="p-2 min-w-[140px]">草稿</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="field in advancedFields" :key="field.key" class="border-t border-ios-separator">
                <td class="p-2 text-ios-label">
                  <div class="font-medium">{{ field.label }}</div>
                  <div class="font-mono text-ios-tertiary text-xs">{{ field.key }}</div>
                  <div class="text-neutral-400 text-xs mt-0.5 max-w-xs">{{ field.help }}</div>
                </td>
                <td class="p-2 font-mono text-ios-label">{{ state.current?.[field.key] || '—' }}</td>
                <td class="p-2">
                  <span class="px-1.5 py-0.5 rounded text-xs font-medium inline-block" :class="statusClass(getFieldStatus(field.key))">
                    {{ statusText(getFieldStatus(field.key)) }}
                  </span>
                </td>
                <td class="p-2 font-mono text-ios-secondary text-xs">{{ keySource(field.key) }}</td>
                <td class="p-2 font-mono text-ios-secondary">{{ state.initial?.[field.key] || '—' }}</td>
                <td class="p-2 font-mono text-ios-label">{{ state.desired?.[field.key] || '—' }}</td>
                <td class="p-2">
                  <div v-if="adviceByKey.has(field.key)" class="flex items-center gap-1">
                    <span class="font-mono font-semibold text-emerald-600">{{ adviceByKey.get(field.key)!.value }}</span>
                    <button v-if="canEdit" type="button" class="text-xs text-ios-blue hover:underline" @click="applySingleAdvice(adviceByKey.get(field.key)!)">采用</button>
                  </div>
                  <span v-else class="text-neutral-400">—</span>
                </td>
                <td class="p-2">
                  <select v-if="field.choices || isAlgorithmKey(field.key)" v-model="draft[field.key]" class="form-input-base" :disabled="!canEdit || !isKeyAvailable(field.key)">
                    <option value="">不设置</option>
                    <option v-for="opt in fieldOptions(field)" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                  </select>
                  <input v-else v-model="draft[field.key]" type="text" inputmode="numeric" pattern="[0-9]*" :placeholder="field.min + '–' + field.max" :aria-label="field.label" class="form-input-base" :disabled="!canEdit || !isKeyAvailable(field.key)" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </details>

      <div v-if="owner" class="flex gap-2 pt-2 border-t border-black/[0.04]">
        <button type="button" class="ios-btn-primary" :disabled="!canEdit" @click="confirm(false)">一键应用</button>
        <button type="button" class="ios-btn-danger" :disabled="!canEdit" @click="confirm(true)">清除调优</button>
      </div>
      <p v-else class="text-xs text-ios-secondary">Viewer 只读</p>
    </section>

    <!-- 4. 最近执行回执 (Receipt) -->
    <section v-if="state?.result" class="panel-card p-4 space-y-3">
      <h3 class="text-ios-label text-base font-semibold leading-normal">最近执行回执 · {{ tuningStatus(state.result.status || state.status) }}</h3>
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
import { showToast } from '../../../../app/api';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import { nodeAPI, type NodeView, type NodeTuning, type NodeTuningInput, type RecommendationView, type RecommendationItem } from '../../api';
import {
  tuningFields, tuningStatus, tuningRequest, tuningPresets, tuningDraft, applyPreset, revertedKeys,
  keyStatus, algorithmOptions, adoptExternal, applyAdvice, essentialKeys, type KeyStatus, type TuningField
} from './serverTuningModel';

const props = defineProps<{node:NodeView;isOwner?:boolean}>();
const owner = computed(() => sessionOwner.value && props.isOwner !== false);
const state = ref<NodeTuning | null>(null), draft = ref<Record<string,string>>({});
const recommendations = ref<RecommendationView | null>(null);
const selectedAdviceKeys = ref<string[]>([]);
const loading = ref(false), sending = ref(false), error = ref(''), dialog = ref(false), clearing = ref(false), pollExpired = ref(false);
const canEdit = computed(() => owner.value && state.value?.supported === true && state.value.status !== 'pending' && !loading.value && !sending.value);

const hostInfo = computed(() => props.node.capabilities?.host);
const virtualization = computed(() => hostInfo.value?.virtualization || '未知 / 物理机');
const isLXC = computed(() => virtualization.value?.toLowerCase().includes('lxc'));
const agentFdLimits = computed(() => {
  const limits = hostInfo.value?.agentLimits || hostInfo.value?.agent_limits;
  if (!limits) return '—';
  const cur = 'fdCur' in limits ? limits.fdCur : limits.fd_cur;
  const max = 'fdMax' in limits ? limits.fdMax : limits.fd_max;
  return `${cur ?? '—'} / ${max ?? '—'}`;
});

const availableKeys = computed(() => {
  const keys = hostInfo.value?.tuningKeys || hostInfo.value?.tuning_keys;
  if (!Array.isArray(keys)) return undefined;
  return new Set(keys.filter(k => k.present).map(k => k.key));
});

function isKeyAvailable(key: string): boolean {
  if (!availableKeys.value) return true;
  return availableKeys.value.has(key);
}

const essentialFields = computed(() => tuningFields.filter(f => essentialKeys.includes(f.key)));
const advancedFields = computed(() => tuningFields.filter(f => !essentialKeys.includes(f.key)));

function getFieldStatus(key: string): KeyStatus {
  if (!state.value) return 'default';
  return keyStatus(key, state.value, hostInfo.value);
}

const statusCounts = computed(() => {
  const counts: Record<KeyStatus, number> = { panel: 0, drift: 0, external: 0, default: 0, unavailable: 0 };
  if (!state.value) return counts;
  for (const field of tuningFields) {
    const st = getFieldStatus(field.key);
    counts[st] = (counts[st] || 0) + 1;
  }
  return counts;
});

const persistAcrossReboot = computed(() => {
  return statusCounts.value.panel > 0 && statusCounts.value.drift === 0;
});

function statusText(st: KeyStatus): string {
  const map: Record<KeyStatus, string> = {
    panel: '面板管理',
    drift: '偏离/漂移',
    external: '外部配置',
    default: '系统默认',
    unavailable: '不可用'
  };
  return map[st] || '未知';
}

function statusClass(st: KeyStatus): string {
  const map: Record<KeyStatus, string> = {
    panel: 'bg-emerald-50 text-emerald-700 border border-emerald-200/60',
    drift: 'bg-amber-50 text-amber-700 border border-amber-200/60',
    external: 'bg-blue-50 text-blue-700 border border-blue-200/60',
    default: 'bg-black/[0.04] text-neutral-600 border border-black/[0.06]',
    unavailable: 'bg-neutral-100 text-neutral-400 border border-neutral-200'
  };
  return map[st] || 'bg-neutral-100 text-neutral-400';
}

function keySource(key: string): string {
  const sources = hostInfo.value?.sysctlSources || hostInfo.value?.sysctl_sources || [];
  const src = sources.find(s => s.key === key);
  if (src && src.file) return src.file;
  if (state.value?.desired?.[key]) return '99-proxy-tuning.conf';
  return '系统默认';
}

function fieldLabel(key: string): string {
  const f = tuningFields.find(field => field.key === key);
  return f ? f.label : key;
}

function isAlgorithmKey(key: string): boolean {
  return key === 'net.ipv4.tcp_congestion_control' || key === 'net.core.default_qdisc';
}

function fieldOptions(field: TuningField): { value: string; label: string }[] {
  if (field.key === 'net.ipv4.tcp_congestion_control') {
    return algorithmOptions('cc', hostInfo.value, state.value?.current?.['net.ipv4.tcp_congestion_control']);
  }
  if (field.key === 'net.core.default_qdisc') {
    return algorithmOptions('qdisc', hostInfo.value, state.value?.current?.['net.core.default_qdisc']);
  }
  const opts: { value: string; label: string }[] = [];
  if (field.choices) {
    for (const c of field.choices) {
      opts.push({ value: c, label: c });
    }
  }
  return opts;
}

// Recommendations
const activeRecommendations = computed(() => (recommendations.value?.items || []).filter(item => !item.dismissed));
const adviceByKey = computed(() => {
  const map = new Map<string, RecommendationItem>();
  for (const item of activeRecommendations.value) {
    map.set(item.key, item);
  }
  return map;
});

const allAdviceSelected = computed(() => {
  return activeRecommendations.value.length > 0 && selectedAdviceKeys.value.length === activeRecommendations.value.length;
});

function toggleSelectAllAdvice() {
  if (allAdviceSelected.value) {
    selectedAdviceKeys.value = [];
  } else {
    selectedAdviceKeys.value = activeRecommendations.value.map(item => item.key);
  }
}

function adoptSelectedAdvice() {
  const items = activeRecommendations.value.filter(item => selectedAdviceKeys.value.includes(item.key));
  draft.value = applyAdvice(draft.value, items);
  selectedAdviceKeys.value = [];
  showToast('已将所选建议填入草稿');
}

function applySingleAdvice(item: { key: string; value: string }) {
  draft.value = applyAdvice(draft.value, [item]);
  showToast(`已将 ${fieldLabel(item.key)} 推荐值填入草稿`);
}

async function dismissAdvice(item: RecommendationItem) {
  try {
    await nodeAPI.dismissTuningRecommendation(props.node.id, item.key, item.value);
    showToast('已记录不采纳建议');
    await loadRecommendations();
  } catch (e) {
    error.value = '不采纳操作失败：' + (e as Error).message;
  }
}

function adviceLevelLabel(level: 'info' | 'suggested' | 'warning'): string {
  if (level === 'warning') return '强烈建议';
  if (level === 'suggested') return '建议';
  return '参考';
}
function adviceLevelClass(level: 'info' | 'suggested' | 'warning'): string {
  if (level === 'warning') return 'bg-rose-50 text-rose-700 border border-rose-200/60';
  if (level === 'suggested') return 'bg-amber-50 text-amber-700 border border-amber-200/60';
  return 'bg-blue-50 text-blue-700 border border-blue-200/60';
}

function adoptExternalToDraft() {
  if (!state.value) return;
  draft.value = adoptExternal(draft.value, state.value, hostInfo.value);
  showToast('已将外部配置并入草稿');
}

let lifecycle = 0, reads = 0, timer: ReturnType<typeof setTimeout> | undefined, deadline = 0;
let confirmed: NodeTuningInput | null = null;
const confirmMessage = computed(() => {
  let msg = '将向服务器【' + props.node.name + '】的 Agent 发送' + (clearing.value ? '清除请求。root helper 将恢复初始快照并移除 /etc/sysctl.d/99-proxy-tuning.conf。' : '所确认的完整配置。root helper 将应用参数并持久化至 /etc/sysctl.d/99-proxy-tuning.conf。') + '代理核心不会重启。请以执行回执和后续心跳为准。';
  if (!clearing.value && state.value?.desired) {
    const reverted = revertedKeys(state.value.desired, draft.value);
    if (reverted.length > 0) {
      msg += ' 以下参数将恢复初始值：' + reverted.join('、') + '。';
    }
  }
  return msg;
});
function time(value?:string|null) { return value ? formatTime(value) : '—'; }
function stopPoll() { clearTimeout(timer); timer = undefined; }
function schedulePoll() {
  stopPoll();
  if (state.value?.status !== 'pending') return;
  if (Date.now() >= deadline) { pollExpired.value = true; return; }
  timer = setTimeout(() => void refresh(), 3000);
}

async function loadRecommendations() {
  try {
    recommendations.value = await nodeAPI.tuningRecommendations(props.node.id);
  } catch {
    // Graceful fallback
  }
}

async function refresh() {
  const cycle = lifecycle, ticket = ++reads, id = props.node.id;
  loading.value = true; error.value = '';
  try {
    const [result] = await Promise.all([
      nodeAPI.tuning(id),
      loadRecommendations()
    ]);
    if (cycle !== lifecycle || ticket !== reads) return;
    const initial = state.value === null;
    state.value = result;
    if (initial) draft.value = tuningDraft(result.desired);
  } catch(e) { if (cycle === lifecycle && ticket === reads) error.value = '调优状态读取失败：' + (e as Error).message; }
  finally { if (cycle === lifecycle && ticket === reads) { loading.value = false; schedulePoll(); } }
}
function confirm(clear:boolean) {
  if (!canEdit.value) return;
  try {
    confirmed = tuningRequest(props.node.id, draft.value, clear, availableKeys.value);
    clearing.value = clear;
    dialog.value = true;
    error.value = '';
  } catch(e) {
    error.value = (e as Error).message;
  }
}
async function submit() {
  if (!canEdit.value || !confirmed || confirmed.confirmNodeId !== props.node.id) return;
  const input = confirmed, cycle = lifecycle;
  sending.value = true; error.value = ''; stopPoll();
  try {
    const result = await nodeAPI.saveTuning(props.node.id, input);
    if (cycle !== lifecycle) return;
    if (state.value) state.value = {...state.value, desired: input.settings, requestId: result.requestId, status: result.status};
    deadline = Date.now()+60000; pollExpired.value = false;
    await refresh();
  } catch(e) { if (cycle === lifecycle) { error.value = '请求失败（服务端为最终校验）：' + (e as Error).message; } }
  finally { if (cycle === lifecycle) sending.value = false; }
}
watch(() => props.node.lastHeartbeat, () => {
  if (state.value && state.value.status !== 'pending' && !sending.value && !loading.value) void refresh();
});
watch(() => props.node.id, () => {
  lifecycle++; reads++; stopPoll(); state.value = null; draft.value = tuningDraft(); recommendations.value = null; selectedAdviceKeys.value = []; sending.value = false; dialog.value = false; confirmed = null; pollExpired.value = false; deadline = Date.now()+60000; void refresh();
}, {immediate:true});
onUnmounted(() => { lifecycle++; stopPoll(); });
</script>
