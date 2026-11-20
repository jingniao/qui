<template>
  <div class="space-y-4">
    <!-- 1. 顶部状态卡 (Status Board) -->
    <section class="panel-card p-4 space-y-4">
      <div class="flex flex-wrap justify-between items-center gap-2">
        <div class="flex items-center gap-2">
          <h2 class="text-ios-label text-base font-semibold leading-normal">调优状态</h2>
          <span v-if="state?.supported" class="px-2 py-0.5 rounded-full text-xs font-semibold bg-emerald-50 text-emerald-700 border border-emerald-200/60">Agent 支持</span>
          <span v-else class="px-2 py-0.5 rounded-full text-xs font-semibold bg-neutral-100 text-neutral-500">Agent 不支持</span>
          <span v-if="currentlyConsistent" class="px-2 py-0.5 rounded-full text-xs font-semibold bg-blue-50 text-blue-700 border border-blue-200/60">当前与期望一致</span>
        </div>
        <button type="button" class="ios-btn-secondary" :disabled="loading" @click="manualRefresh">刷新状态</button>
      </div>

      <!-- Key metrics tiles in status board -->
      <div class="grid grid-cols-2 sm:grid-cols-4 gap-2.5 text-xs">
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">当前拥塞控制</span>
          <p class="font-mono font-semibold text-ios-label truncate">{{ state?.current?.['net.ipv4.tcp_congestion_control'] || '—' }}</p>
          <p>{{ algorithmHint('cc') }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">当前默认排队算法</span>
          <p class="font-mono font-semibold text-ios-label truncate">{{ state?.current?.['net.core.default_qdisc'] || '—' }}</p>
          <p>{{ algorithmHint('qdisc') }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">虚拟化环境</span>
          <p class="font-semibold text-ios-label truncate">{{ virtualization }}</p>
        </div>
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <span class="text-ios-secondary">进程文件句柄 (软/硬)</span>
          <p v-for="limit in fdLimits" :key="limit.name" class="font-mono font-semibold text-ios-label break-all">{{ limit.name }}：{{ limit.soft }} / {{ limit.hard }}</p>
          <p v-if="!fdLimits.length" class="text-ios-secondary">未知</p>
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

      <p v-if="state?.supported && !probeStatus" class="text-sm text-ios-secondary">旧 Agent 估算权限，升级本机 Agent 与 helper 后可准确探测。</p>
      <p v-if="probeStatus && probeStatus !== 'ready'" class="text-sm text-ios-orange">权限尚未就绪，请检查并更新本机调优 helper。{{ probeStatus }}</p>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <p v-if="operationFeedback" role="status" class="text-sm">{{ operationFeedback }}</p>

      <!-- Last application result & sync info -->
      <div class="text-xs text-ios-secondary space-y-1 pt-1 border-t border-black/[0.04]">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span>最后执行结果：<strong class="font-medium text-ios-label">{{ sending ? '正在发送' : state?.requestId ? displayStatus(state) : '暂无调优请求' }}</strong></span>
          <span>心跳上报：{{ time(node.lastHeartbeat) }}</span>
        </div>
        <p v-if="state?.status === 'unconfirmed' && state.requestId" class="text-ios-orange">执行结果未确认，操作可能已生效。请检查下一次心跳的当前值；系统不会自动重发。</p>
        <p v-if="pollExpired" class="text-ios-orange">结果未确认。{{ state?.installMissingModules ? '已超过模块准备和参数应用的 15 分 30 秒等待时间。' : '已超过 60 秒等待时间。' }}请用“刷新状态”查看回执并核对当前值。</p>
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
        <div v-if="adoptableAdviceKeys.size > 0 && canEdit" class="flex items-center gap-2">
          <button v-if="selectedAdviceKeys.length" type="button" class="ios-action-btn-primary text-xs px-2.5 py-1" @click="adoptSelectedAdvice">采用所选 ({{ selectedAdviceKeys.length }})</button>
          <button type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="toggleSelectAllAdvice">{{ allAdviceSelected ? '取消全选' : '全选建议' }}</button>
        </div>
      </div>

      <p v-if="adviceError" class="text-sm text-ios-red">{{ adviceError }}</p>
      <div v-if="activeRecommendations.length > 0" class="space-y-2">
        <div v-for="item in activeRecommendations" :key="item.key" class="rounded-xl border border-black/[0.06] p-3 text-xs bg-black/[0.01] flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <div class="flex items-start gap-2.5 min-w-0">
            <input v-if="canEdit && adoptableAdviceKeys.has(item.key)" type="checkbox" :value="item.key" v-model="selectedAdviceKeys" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-ios-blue focus:ring-ios-blue shrink-0" />
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
            <button v-if="adoptableAdviceKeys.has(item.key)" type="button" class="ios-action-btn-secondary text-xs px-2 py-0.5" @click="applySingleAdvice(item)">填入草稿</button>
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
      <label v-if="canEdit && moduleSupported" class="flex items-center gap-2 text-sm">
        <input type="checkbox" v-model="installModules" />
        本次允许按需补装 Ubuntu 当前内核的 BBR / fq 模块
      </label>
      <p v-else class="text-xs text-ios-secondary">模块补装限 Ubuntu 宿主的官方独立模块包。{{ hostInfo?.tuningModuleInstallError || hostInfo?.tuning_module_install_error || (moduleSupported ? '' : '节点未报告补装能力。') }}</p>
      <div v-if="canEdit" class="flex flex-wrap items-center gap-2 text-xs">
        <span class="text-neutral-400">场景预设：</span>
        <button v-for="preset in availablePresets" :key="preset.name" type="button" class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="draft = applyPreset(draft, preset, availableKeys, hostInfo)">{{ preset.name }}</button>
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
                    <button v-if="canEdit && adoptableAdviceKeys.has(field.key)" type="button" class="text-xs text-ios-blue hover:underline" @click="applySingleAdvice(adviceByKey.get(field.key)!)">采用</button>
                  </div>
                  <span v-else class="text-neutral-400">—</span>
                </td>
                <td class="p-2">
                  <span v-if="!isKeyAvailable(field.key)" class="text-ios-secondary">{{ unavailableReason(field.key) }}</span>
                  <select v-else-if="field.choices || isAlgorithmKey(field.key)" v-model="draft[field.key]" class="form-input-base" :disabled="!canEdit">
                    <option value="">不设置</option>
                    <option v-for="opt in fieldOptions(field)" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                  </select>
                  <input v-else v-model="draft[field.key]" type="text" inputmode="numeric" pattern="[0-9]*" :placeholder="field.min + '–' + field.max" :aria-label="field.label" class="form-input-base" :disabled="!canEdit" />
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
                    <button v-if="canEdit && adoptableAdviceKeys.has(field.key)" type="button" class="text-xs text-ios-blue hover:underline" @click="applySingleAdvice(adviceByKey.get(field.key)!)">采用</button>
                  </div>
                  <span v-else class="text-neutral-400">—</span>
                </td>
                <td class="p-2">
                  <span v-if="!isKeyAvailable(field.key)" class="text-ios-secondary">{{ unavailableReason(field.key) }}</span>
                  <select v-else-if="field.choices || isAlgorithmKey(field.key)" v-model="draft[field.key]" class="form-input-base" :disabled="!canEdit">
                    <option value="">不设置</option>
                    <option v-for="opt in fieldOptions(field)" :key="opt.value" :value="opt.value">{{ opt.label }}</option>
                  </select>
                  <input v-else v-model="draft[field.key]" type="text" inputmode="numeric" pattern="[0-9]*" :placeholder="field.min + '–' + field.max" :aria-label="field.label" class="form-input-base" :disabled="!canEdit" />
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </details>

      <div v-if="owner" class="flex gap-2 pt-2 border-t border-black/[0.04]">
        <button type="button" class="ios-btn-primary" :disabled="!canEdit || (!!probeStatus && probeStatus !== 'ready')" @click="confirm(false)">一键应用</button>
        <button type="button" class="ios-btn-danger" :disabled="!canEdit" @click="confirm(true)">清除调优</button>
      </div>
      <p v-else class="text-xs text-ios-secondary">Viewer 只读</p>
      <p v-if="error" class="text-sm text-ios-red">{{ error }}</p>
      <p v-if="operationFeedback" role="status" class="text-sm">{{ operationFeedback }}</p>
    </section>



    <!-- 4. 最近执行回执 (Receipt) -->
    <section v-if="!sending && state?.result" class="panel-card p-4 space-y-3">
      <h3 class="text-ios-label text-base font-semibold leading-normal">最近执行回执 · {{ displayStatus(state) }}</h3>
      <p v-for="(result, index) in state.result.keys || []" :key="index" class="text-xs text-ios-label break-words">
        {{ result.key || '全局错误' }} · {{ result.applied ? '已应用' : '失败' }} · 读回值：{{ result.value || '—' }} <span v-if="result.error" class="text-ios-red">{{ result.error }}</span>
      </p>
      <p v-if="state.result.installedModulePackages?.length" class="text-sm">已配置的模块包：{{ state.result.installedModulePackages.join('、') }}</p>
    </section>

    <ConfirmDialog v-model="dialog" :title="clearing ? '确认清除调优？' : '确认应用调优？'" :message="confirmMessage" confirm-text="确认发送" :is-danger="clearing" @confirm="submit" />
  </div>
</template>
<script setup lang="ts">
import { formatTime } from '../../../../app/auth';
import { computed, ref, watch, onUnmounted } from 'vue';
import { isOwner as sessionOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import { forgetPendingGet, invalidatesRead } from '../../../../app/apiClient';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import { nodeAPI, type NodeView, type NodeTuning, type NodeTuningInput, type RecommendationView, type RecommendationItem } from '../../api';
import {
  tuningFields, tuningStatus, tuningRequest, tuningPresets, tuningDraft, applyPreset, revertedKeys,
  keyStatus, algorithmOptions, adoptExternal, applyAdvice, essentialKeys, type KeyStatus, type TuningField
} from './serverTuningModel';

const props = defineProps<{node:NodeView;isOwner?:boolean}>();
const emit = defineEmits<{ refreshNode: [] }>();
const owner = computed(() => sessionOwner.value && props.isOwner !== false);
const state = ref<NodeTuning | null>(null), draft = ref<Record<string,string>>({});
const recommendations = ref<RecommendationView | null>(null);
const adviceError = ref(''), readError = ref(''), replacedRequest = ref('');
const selectedAdviceKeys = ref<string[]>([]);
const loading = ref(false), sending = ref(false), error = ref(''), dialog = ref(false), clearing = ref(false), pollExpired = ref(false);
const canEdit = computed(() => owner.value && state.value?.supported === true && state.value.status !== 'pending' && !loading.value && !sending.value);

const installModules = ref(false);
const hostInfo = computed(() => props.node.capabilities?.host);
const moduleSupported = computed(() => !!(hostInfo.value?.tuningModuleInstallSupported || hostInfo.value?.tuning_module_install_supported));
const virtualization = computed(() => hostInfo.value?.virtualization || '未知 / 物理机');
const probeStatus = computed(() => hostInfo.value?.tuningProbeStatus || hostInfo.value?.tuning_probe_status || '');
const fdLimits = computed(() => hostInfo.value?.fdLimits || hostInfo.value?.fd_limits || []);

const availableKeys = computed(() => {
  const keys = hostInfo.value?.tuningKeys || hostInfo.value?.tuning_keys;
  if (probeStatus.value && (probeStatus.value !== 'ready' || !Array.isArray(keys))) return new Set<string>();
  if (!Array.isArray(keys)) return undefined;
  return new Set(keys.filter(k => k.present && k.writable).map(k => k.key));
});

function isKeyAvailable(key: string): boolean {
  if (!availableKeys.value) return true;
  return availableKeys.value.has(key);
}

function unavailableReason(key: string): string {
  const host = hostInfo.value;
  const entry = (host?.tuningKeys || host?.tuning_keys)?.find(k => k.key === key);
  const reason = entry?.probeError || entry?.probe_error;
  if (key === 'net.core.default_qdisc' && ['lxc','docker','openvz','podman','systemd-nspawn','userns'].includes(virtualization.value) && reason?.startsWith('缺少入口')) return '当前容器缺少默认排队算法设置入口，无法通过此面板参数启用';
  return reason || (probeStatus.value && probeStatus.value !== 'ready' ? '权限尚未探测' : '此环境不可用');
}

function algorithmHint(kind: 'cc' | 'qdisc'): string {
  const host = hostInfo.value, cc = kind === 'cc', algorithm = cc ? 'bbr' : 'fq';
  const key = cc ? 'net.ipv4.tcp_congestion_control' : 'net.core.default_qdisc';
  const available = cc ? host?.congestionAvailable || host?.congestion_available : host?.qdiscAvailable || host?.qdisc_available;
  const loadable = cc ? host?.congestionLoadable || host?.congestion_loadable : host?.qdiscLoadable || host?.qdisc_loadable;
  let condition = '未报告该算法';
  if (state.value?.current?.[key] === algorithm) condition = '已启用';
  else if (available?.includes(algorithm)) condition = '已可用';
  else if (loadable?.includes(algorithm)) condition = '可由内核加载';
  else if (moduleSupported.value && isKeyAvailable(key)) condition = '可尝试补装 Ubuntu 当前内核模块';
  const permission = isKeyAvailable(key) ? (probeStatus.value === 'ready' ? '参数可写' : '写入权限由旧 Agent 估算') : unavailableReason(key);
  return `${cc ? 'BBR' : 'fq'} · ${condition} · ${permission}`;
}

function displayStatus(value: NodeTuning): string {
  if (value.status === 'failed' && value.result?.keys?.some(item => item.error?.startsWith('tuning status unconfirmed:'))) return '结果未确认';
  return tuningStatus(value.status);
}

const operationFeedback = computed(() => {
  if (sending.value) return '正在发送调优请求…';
  const value = state.value;
  const messages = readError.value ? [readError.value] : [];
  if (replacedRequest.value) messages.push(replacedRequest.value);
  if (value?.requestId) {
    const failure = value.result?.keys?.find(item => item.error);
    messages.push(`${displayStatus(value)} · ${value.requestId}`);
    if (value.status === 'pending' && value.installMissingModules) messages.push('正在等待模块检查、按需补装和参数应用回执');
    if (failure) messages.push([failure.key, failure.error].filter(Boolean).join(' · '));
    if (value.status === 'pending' && pollExpired.value) messages.push('等待回执超时，请刷新状态');
    if ((value.status === 'applied' || value.status === 'cleared') && (!props.node.lastHeartbeat || !value.updatedAt || Date.parse(props.node.lastHeartbeat) <= Date.parse(value.updatedAt))) messages.push('等待下次心跳更新当前值');
  }
  return messages.join(' · ');
});

const essentialFields = computed(() => tuningFields.filter(f => essentialKeys.includes(f.key)));
const advancedFields = computed(() => tuningFields.filter(f => !essentialKeys.includes(f.key)));
const availablePresets = computed(() => tuningPresets.filter(preset => Object.keys(applyPreset({}, preset, availableKeys.value, hostInfo.value)).length > 0));

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

const currentlyConsistent = computed(() => {
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
    return algorithmOptions('cc', hostInfo.value, state.value?.current?.['net.ipv4.tcp_congestion_control'], installModules.value);
  }
  if (field.key === 'net.core.default_qdisc') {
    return algorithmOptions('qdisc', hostInfo.value, state.value?.current?.['net.core.default_qdisc'], installModules.value);
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
const adoptableAdviceKeys = computed(() => new Set(Object.keys(applyAdvice({}, activeRecommendations.value, availableKeys.value, hostInfo.value))));
const adviceByKey = computed(() => {
  const map = new Map<string, RecommendationItem>();
  for (const item of activeRecommendations.value) {
    map.set(item.key, item);
  }
  return map;
});

const allAdviceSelected = computed(() => {
  return adoptableAdviceKeys.value.size > 0 && [...adoptableAdviceKeys.value].every(key => selectedAdviceKeys.value.includes(key));
});

function toggleSelectAllAdvice() {
  if (allAdviceSelected.value) {
    selectedAdviceKeys.value = [];
  } else {
    selectedAdviceKeys.value = [...adoptableAdviceKeys.value];
  }
}

function adoptSelectedAdvice() {
  const items = activeRecommendations.value.filter(item => selectedAdviceKeys.value.includes(item.key));
  draft.value = applyAdvice(draft.value, items, availableKeys.value, hostInfo.value);
  selectedAdviceKeys.value = [];
  showToast('已将所选建议填入草稿');
}

function applySingleAdvice(item: { key: string; value: string }) {
  draft.value = applyAdvice(draft.value, [item], availableKeys.value, hostInfo.value);
  showToast(`已将 ${fieldLabel(item.key)} 推荐值填入草稿`);
}

async function dismissAdvice(item: RecommendationItem) {
  const current = lifecycle;
  try {
    await nodeAPI.dismissTuningRecommendation(props.node.id, item.key, item.value);
    if (current !== lifecycle) return;
    showToast('已记录不采纳建议');
    await loadRecommendations();
  } catch (e) {
    if (current === lifecycle) adviceError.value = '不采纳操作失败：' + (e as Error).message;
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
let adviceReads = 0;
const confirmed = ref<NodeTuningInput | null>(null);
const confirmMessage = computed(() => {
  let msg = '将向服务器【' + props.node.name + '】的 Agent 发送' + (clearing.value ? '清除请求。root helper 将恢复初始快照并移除 /etc/sysctl.d/99-proxy-tuning.conf。' : '所确认的完整配置。root helper 将应用参数并持久化至 /etc/sysctl.d/99-proxy-tuning.conf。') + '代理核心不会重启。请以执行回执和后续心跳为准。';
  if (!clearing.value && state.value && confirmed.value) {
    const settings = confirmed.value.settings;
    if (confirmed.value.installMissingModules) msg += ' 本次允许安装与运行内核完全匹配的 Ubuntu 官方 BBR / fq 模块包。系统完成包配置后才应用参数，参数回滚会保留已装包。';
    msg += ' 本次参数：' + Object.entries(settings).map(([key,value]) => key + '=' + value).join('、') + '。';
    const reverted = revertedKeys(state.value.desired, settings);
    if (reverted.length > 0) {
      msg += ' 以下参数将恢复初始值：' + reverted.join('、') + '。';
    }
  }
  return msg;
});
function time(value?:string|null) { return value ? formatTime(value) : '—'; }
function stopPoll() { clearTimeout(timer); timer = undefined; }
function pollDeadline(install?: boolean, requestedAt?: string | null) {
  const start = requestedAt ? Math.min(Date.now(), Date.parse(requestedAt)) : Date.now();
  return start + (install ? (15 * 60 + 30) * 1000 : 60000);
}
function pollingExpired() {
  if (state.value?.status !== 'pending' || Date.now() < deadline) return false;
  pollExpired.value = true;
  stopPoll();
  return true;
}
function autoRefresh() {
  if (document.visibilityState === 'visible' && !pollingExpired()) void refresh();
}
function schedulePoll() {
  stopPoll();
  if (document.visibilityState !== 'visible' || state.value?.status !== 'pending') return;
  if (!pollingExpired()) timer = setTimeout(autoRefresh, Math.min(3000, deadline - Date.now()));
}

async function loadRecommendations() {
  const current = lifecycle, ticket = ++adviceReads;
  try {
    const result = await nodeAPI.tuningRecommendations(props.node.id);
    if (current === lifecycle && ticket === adviceReads) { recommendations.value = result; adviceError.value = ''; }
  } catch (e) {
    if (current !== lifecycle || ticket !== adviceReads) return;
    if (invalidatesRead(e)) {
      recommendations.value = null;
      selectedAdviceKeys.value = [];
    }
    adviceError.value = '调优建议读取失败：' + (e as Error).message;
  }
}

let refreshing = false;
async function refresh() {
  if (refreshing || sending.value || document.visibilityState !== 'visible') return;
  refreshing = true;
  const cycle = lifecycle, ticket = ++reads, id = props.node.id;
  loading.value = state.value === null;
  try {
    const result = await nodeAPI.tuning(id);
    if (cycle !== lifecycle || ticket !== reads) return;
    // Reads started before a submission were invalidated by its ticket. A later
    // GET returns the single authoritative row, including another owner's request.
    if (state.value?.status === 'pending' && state.value.requestId !== result.requestId) {
      const previous = state.value.requestId;
      replacedRequest.value = result.requestId ? `请求 ${previous} 已由 ${result.requestId} 接替` : `请求 ${previous} 的回执已不在当前记录中`;
    }
    const initial = state.value === null;
    const finished = state.value?.status === 'pending' && result.status !== 'pending';
    if (result.status === 'pending' && state.value?.requestId !== result.requestId) {
      deadline = pollDeadline(result.installMissingModules, result.requestedAt);
      pollExpired.value = false;
    } else if (result.status !== 'pending') {
      pollExpired.value = false;
    }
    readError.value = '';
    state.value = result;
    if (finished) emit('refreshNode');
    if (initial) draft.value = tuningDraft(result.desired);
  } catch(e) {
    if (cycle !== lifecycle || ticket !== reads) return;
    if (invalidatesRead(e)) {
      forgetPendingGet(`/nodes/${encodeURIComponent(id)}/tuning/recommendations`);
      resetTuning();
    }
    readError.value = '调优状态读取失败：' + (e as Error).message;
  }
  finally { if (cycle === lifecycle && ticket === reads) { refreshing = false; loading.value = false; schedulePoll(); } }
}
function manualRefresh() {
  void refresh();
  emit('refreshNode');
  if (state.value?.status !== 'pending') void loadRecommendations();
}
function confirm(clear:boolean) {
  if (!canEdit.value) return;
  try {
    confirmed.value = tuningRequest(props.node.id, draft.value, clear, availableKeys.value, hostInfo.value, installModules.value);
    clearing.value = clear;
    dialog.value = true;
    error.value = '';
  } catch(e) {
    error.value = (e as Error).message;
  }
}
async function submit() {
  if (!canEdit.value || !confirmed.value || confirmed.value.confirmNodeId !== props.node.id) return;
  const input = confirmed.value, cycle = lifecycle;
  reads++; refreshing = false; readError.value = '';
  sending.value = true; error.value = ''; replacedRequest.value = ''; stopPoll();
  try {
    const result = await nodeAPI.saveTuning(props.node.id, input);
    if (cycle !== lifecycle) return;
    sending.value = false;
    installModules.value = false;
    if (state.value) state.value = {...state.value, desired: input.settings, installMissingModules:input.installMissingModules, requestId: result.requestId, status: result.status, result: null, requestedAt: null, updatedAt: null, initial: {}};
    deadline = pollDeadline(input.installMissingModules); pollExpired.value = false;
    await refresh();
  } catch(e) { if (cycle === lifecycle) { error.value = '请求失败（服务端为最终校验）：' + (e as Error).message; } }
  finally { if (cycle === lifecycle) sending.value = false; }
}
function visibilityChanged() {
  if (document.visibilityState === 'visible') autoRefresh();
  else stopPoll();
}
const refreshTimer = setInterval(autoRefresh, 60000);
document.addEventListener('visibilitychange', visibilityChanged);
function resetTuning() {
  lifecycle++;
  reads++;
  refreshing = false;
  stopPoll();
  loading.value = false;
  state.value = null;
  draft.value = tuningDraft();
  recommendations.value = null;
  selectedAdviceKeys.value = [];
  installModules.value = false;
  sending.value = false;
  dialog.value = false;
  clearing.value = false;
  confirmed.value = null;
  error.value = '';
  readError.value = '';
  replacedRequest.value = '';
  adviceError.value = '';
  pollExpired.value = false;
  deadline = Date.now()+60000;
}
watch(() => props.node.id, () => {
  resetTuning();
  void refresh();
  void loadRecommendations();
}, {immediate:true});
onUnmounted(() => { lifecycle++; stopPoll(); clearInterval(refreshTimer); document.removeEventListener('visibilitychange', visibilityChanged); });
</script>
