<template>
  <p v-if="node.deletedAt" class="panel-card p-4 text-sm text-ios-secondary">服务器已删除，设置不可修改。</p>
  <div v-else class="space-y-3.5 sm:space-y-4">
    <!-- Balanced 2-Column Responsive Settings Grid (Matching Overview Layout) -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3.5 sm:gap-4 items-start">
      <!-- Left Column: 基本信息, 订阅与访问控制, 程序与运维 -->
      <div class="space-y-3.5 sm:space-y-4 min-w-0">
        <!-- 1. 基本信息 Form Card -->
        <form id="server-basic" class="panel-card p-3.5 sm:p-4 bg-white space-y-3" @submit.prevent="save('basic')">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <h2 class="font-semibold text-ios-label text-sm sm:text-base">基本信息</h2>
              <span v-if="dirty.basic" class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-amber-50 text-amber-700 border border-amber-200/60">未保存更改</span>
              <span v-else class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-black/[0.03] text-neutral-400">已同步</span>
            </div>
            <button
              type="submit"
              class="ios-action-btn-primary text-xs px-3 py-1 flex items-center gap-1.5"
              :disabled="!!saving"
            >
              <span v-if="saving === 'basic'" class="w-3 h-3 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
              <span>保存基本信息</span>
            </button>
          </div>

          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5 text-xs text-ios-secondary">
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">服务器名称 <span class="text-rose-500">*</span></span>
              <input v-model.trim="form.name" required maxlength="40" class="form-input-base text-xs" />
            </label>
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">订阅接入 IP / 域名</span>
              <input v-model.trim="form.publicIp" class="form-input-base text-xs font-mono" placeholder="留空默认使用服务器公网 IP" />
            </label>
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">地区 / 位置</span>
              <input v-model.trim="metaForm.region" class="form-input-base text-xs" placeholder="例如：🇯🇵 东京" />
            </label>
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">备注说明</span>
              <input v-model.trim="metaForm.remark" class="form-input-base text-xs" placeholder="管理员自定备注" />
            </label>

            <!-- 默认端口分配范围 -->
            <div class="sm:col-span-2 pt-2 border-t border-black/[0.04] space-y-1.5">
              <div class="flex items-center justify-between">
                <span class="font-medium text-neutral-700 text-[11px] flex items-center gap-1.5">
                  <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
                  </svg>
                  <span>默认端口分配范围</span>
                </span>
                <span class="text-[10px] text-neutral-400">新建节点、中转与隧道时默认在此范围随机分配</span>
              </div>
              <div class="grid grid-cols-2 gap-2.5">
                <label class="space-y-1">
                  <span class="text-neutral-500 text-[10px]">起始端口 (1 - 65535)</span>
                  <input
                    v-model.number="metaForm.portRangeStart"
                    type="number"
                    min="1"
                    max="65535"
                    class="form-input-base text-xs font-mono"
                    placeholder="留空默认 10000"
                  />
                </label>
                <label class="space-y-1">
                  <span class="text-neutral-500 text-[10px]">结束端口 (1 - 65535)</span>
                  <input
                    v-model.number="metaForm.portRangeEnd"
                    type="number"
                    min="1"
                    max="65535"
                    class="form-input-base text-xs font-mono"
                    placeholder="留空默认 65535"
                  />
                </label>
              </div>
              <p class="text-[10px] text-ios-tertiary">设置后，该服务器创建新节点、中转转发或加密隧道时，端口将自动从此区间内随机选取未被占用的端口。</p>
            </div>
          </div>

          <p v-if="errors.basic" role="alert" class="text-xs text-ios-red font-medium">{{ errors.basic }}</p>
        </form>

        <!-- 3. 订阅与访问控制 Form Card -->
        <form id="server-access" class="panel-card p-3.5 sm:p-4 bg-white space-y-3" @submit.prevent="save('access')">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <h2 class="font-semibold text-ios-label text-sm sm:text-base">订阅与维护</h2>
              <span v-if="dirty.access" class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-amber-50 text-amber-700 border border-amber-200/60">未保存更改</span>
              <span v-else class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-black/[0.03] text-neutral-400">已同步</span>
            </div>
            <button
              type="submit"
              class="ios-action-btn-primary text-xs px-3 py-1 flex items-center gap-1.5"
              :disabled="!!saving"
            >
              <span v-if="saving === 'access'" class="w-3 h-3 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
              <span>保存订阅设置</span>
            </button>
          </div>

          <div class="space-y-2">
            <label class="flex items-start justify-between gap-3 p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04] cursor-pointer hover:bg-black/[0.03] transition-colors">
              <div class="space-y-0.5">
                <div class="text-xs font-semibold text-ios-label">启用服务器控制连接</div>
                <p class="text-[11px] text-ios-secondary">停用后将拒绝该服务器 Agent 的心跳上报与配置下发指令</p>
              </div>
              <input v-model="form.enabled" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-ios-blue focus:ring-ios-blue" />
            </label>
            <label class="flex items-start justify-between gap-3 p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04] cursor-pointer hover:bg-black/[0.03] transition-colors">
              <div class="space-y-0.5">
                <div class="text-xs font-semibold text-ios-label">允许用户订阅</div>
                <p class="text-[11px] text-ios-secondary">开启后该服务器绑定的节点将正常分发至前台用户订阅</p>
              </div>
              <input v-model="form.allowSubscription" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-ios-blue focus:ring-ios-blue" />
            </label>
            <label class="flex items-start justify-between gap-3 p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.04] cursor-pointer hover:bg-black/[0.03] transition-colors">
              <div class="space-y-0.5">
                <div class="text-xs font-semibold text-ios-label">维护模式</div>
                <p class="text-[11px] text-ios-secondary">处于维护状态时将临时从用户订阅中排除，不影响节点已有配置</p>
              </div>
              <input v-model="form.maintenance" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-ios-blue focus:ring-ios-blue" />
            </label>
          </div>

          <p v-if="errors.access" role="alert" class="text-xs text-ios-red font-medium">{{ errors.access }}</p>
        </form>

        <!-- 5. 程序与运维管理 Section -->
        <section id="server-programs" class="panel-card p-3.5 sm:p-4 bg-white space-y-3">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <h2 class="font-semibold text-ios-label text-sm sm:text-base">程序与运维</h2>
            <div class="flex items-center gap-2">
              <router-link to="/nodes/versions" class="ios-action-btn-secondary text-xs px-2.5 py-1">版本管理</router-link>
              <button class="ios-action-btn-secondary text-xs px-2.5 py-1" @click="$emit('install')">安装命令</button>
            </div>
          </div>

          <!-- Status Tile -->
          <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 text-xs text-ios-secondary flex items-center justify-between gap-2 flex-wrap">
            <div class="flex items-center gap-2">
              <span class="font-medium text-neutral-600">Agent:</span>
              <span class="font-mono font-semibold text-neutral-800">{{ node.agentVersion || '—' }}</span>
            </div>
            <div class="flex items-center gap-2">
              <span class="font-medium text-neutral-600">核心:</span>
              <span class="font-semibold text-neutral-800">{{ node.capabilities?.core_name || '—' }} {{ node.capabilities?.core_version || '' }}</span>
            </div>
          </div>

          <!-- 核心更新折叠面板 -->
          <details ref="coreDetails" class="rounded-xl border border-black/[0.05] p-2.5 bg-black/[0.01]">
            <summary class="cursor-pointer text-xs font-medium text-ios-secondary hover:text-ios-blue transition-colors select-none">
              核心更新选项
            </summary>
            <div class="pt-2">
              <CoreUpdatesPanel :nodes="[node]" />
            </div>
          </details>

          <!-- 重启面板 -->
          <NodeRestartPanel :node="node" />
        </section>
      </div>

      <!-- Right Column: 续费设置, 出站网络自定义, 危险区域 -->
      <div class="space-y-3.5 sm:space-y-4 min-w-0">
        <!-- 2. 续费设置 Form Card -->
        <form id="server-renewal" class="panel-card p-3.5 sm:p-4 bg-white space-y-3" @submit.prevent="save('renewal')">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <h2 class="font-semibold text-ios-label text-sm sm:text-base">续费</h2>
              <span v-if="dirty.renewal" class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-amber-50 text-amber-700 border border-amber-200/60">未保存更改</span>
              <span v-else class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-black/[0.03] text-neutral-400">已同步</span>
            </div>
            <button
              type="submit"
              class="ios-action-btn-primary text-xs px-3 py-1 flex items-center gap-1.5"
              :disabled="!!saving"
            >
              <span v-if="saving === 'renewal'" class="w-3 h-3 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
              <span>保存续费设置</span>
            </button>
          </div>

          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5 text-xs text-ios-secondary">
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">续费日期</span>
              <input v-model="metaForm.renewDate" type="date" class="form-input-base text-xs font-mono" />
            </label>
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">提前提醒天数</span>
              <input v-model.number="metaForm.renewNoticeDays" type="number" min="0" max="60" class="form-input-base text-xs font-mono" />
            </label>
            <label class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">币种</span>
              <select v-model="metaForm.renewCurrency" class="form-input-base text-xs">
                <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
              </select>
            </label>
            <div class="grid grid-cols-2 gap-1.5">
              <label class="space-y-1">
                <span class="font-medium text-neutral-600 text-[11px]">续费价格</span>
                <input v-model="metaForm.renewPrice" type="number" min="0" step="any" class="form-input-base text-xs font-mono" placeholder="0.00" />
              </label>
              <label class="space-y-1">
                <span class="font-medium text-neutral-600 text-[11px]">周期</span>
                <select v-model="metaForm.renewCycle" class="form-input-base text-xs">
                  <option v-for="cycle in CYCLE_LIST" :key="cycle">{{ cycle }}</option>
                </select>
              </label>
            </div>
          </div>

          <div v-if="metaForm.renewDate" class="bg-black/[0.02] border border-black/[0.04] rounded-lg p-2 text-xs flex items-center justify-between">
            <span class="text-neutral-500 text-[11px]">到期状态：</span>
            <span class="font-semibold text-neutral-800">{{ calculateRemainingDays(metaForm.renewDate).label }}</span>
          </div>

          <p v-if="errors.renewal" role="alert" class="text-xs text-ios-red font-medium">{{ errors.renewal }}</p>
        </form>

        <!-- 4. 出站网络自定义 Form Card -->
        <form id="server-network" class="panel-card p-3.5 sm:p-4 bg-white space-y-3" @submit.prevent="save('network')">
          <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
            <div class="flex items-center gap-2">
              <h2 class="font-semibold text-ios-label text-sm sm:text-base">出站网络</h2>
              <span v-if="dirty.network" class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-amber-50 text-amber-700 border border-amber-200/60">未保存更改</span>
              <span v-else class="px-1.5 py-0.5 rounded text-[11px] font-medium bg-black/[0.03] text-neutral-400">已同步</span>
            </div>
            <button
              type="submit"
              class="ios-action-btn-primary text-xs px-3 py-1 flex items-center gap-1.5"
              :disabled="!!saving"
            >
              <span v-if="saving === 'network'" class="w-3 h-3 border-2 border-white/30 border-t-white rounded-full animate-spin"></span>
              <span>保存出站网络</span>
            </button>
          </div>

          <p class="text-[11px] text-ios-secondary leading-relaxed">
            核心源 IP / 网卡仅影响代理核心流量；服务器强制出站作用于整机新建连接。保存后需发布配置生效，留空表示清除。
          </p>

          <div class="grid grid-cols-1 sm:grid-cols-2 gap-2.5 text-xs text-ios-secondary">
            <label v-for="field in networkFields" :key="field.key" class="space-y-1">
              <span class="font-medium text-neutral-600 text-[11px]">{{ field.label }}</span>
              <input v-model.trim="form[field.key]" class="form-input-base text-xs font-mono" placeholder="留空清除" />
            </label>
          </div>

          <p v-if="errors.network" role="alert" class="text-xs text-ios-red font-medium">{{ errors.network }}</p>
        </form>

        <!-- 6. 危险区域 Section Card -->
        <section id="server-danger" class="panel-card p-3.5 sm:p-4 bg-rose-50/20 border border-rose-200/80 space-y-3">
          <div class="flex items-center justify-between border-b border-rose-200/50 pb-2.5">
            <h2 class="font-semibold text-rose-600 text-sm sm:text-base flex items-center gap-1.5">
              <svg class="w-4 h-4 text-rose-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
              </svg>
              <span>危险区域</span>
            </h2>
          </div>

          <p class="text-[11px] text-ios-secondary leading-relaxed">
            删除前请检查配置引用与发布状态；删除后吊销节点身份，历史流量和访问记录保留。吊销证书后该服务器不能再使用当前证书建立连接。
          </p>

          <div class="flex flex-wrap gap-2.5 pt-1">
            <button type="button" class="ios-btn-danger text-xs px-3.5 py-1.5" @click="$emit('delete')">
              删除服务器
            </button>
            <button type="button" class="ios-btn-danger text-xs px-3.5 py-1.5" @click="revokeOpen = true">
              吊销证书
            </button>
          </div>

          <p v-if="errors.revoke" role="alert" class="text-xs text-ios-red font-medium">{{ errors.revoke }}</p>
        </section>
      </div>
    </div>

    <!-- Confirm Dialog for Certificate Revocation -->
    <ConfirmDialog
      v-model="revokeOpen"
      title="吊销服务器证书"
      message="吊销后此服务器不能再使用当前证书建立控制连接，已有连接也会在下一次消息校验时关闭。"
      @confirm="revoke"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import { nodeAPI, type NodeView, type NodeSettingsInput } from '../../api';
import { type NodeMeta, loadNodeMeta, saveNodeMeta, parseRenewPrice, calculateRemainingDays, parseNodeMetaRemark, CURRENCY_LIST, CYCLE_LIST } from '../../nodeMeta';
import { isOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import NodeRestartPanel from '../NodeRestartPanel.vue';
import CoreUpdatesPanel from '../../../system/components/CoreUpdatesPanel.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import { isDirty } from './serverDetailModel';

const coreDetails = ref<HTMLDetailsElement>();

const props = defineProps<{ node: NodeView; meta: NodeMeta }>();
const emit = defineEmits<{ changed: []; install: []; delete: [] }>();

const form = reactive<NodeSettingsInput>({ ...props.node });
const metaForm = reactive({
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  renewNoticeDays: 7,
  portRangeStart: undefined as number | undefined,
  portRangeEnd: undefined as number | undefined
});

const saving = ref('');
const revokeOpen = ref(false);
const errors = reactive<Record<string, string>>({});

const networkFields = [
  { key: 'outboundSourceIp', label: '核心出站源 IP' },
  { key: 'outboundInterface', label: '核心出站网卡' },
  { key: 'hostSourceIpv4', label: '服务器强制出站 IPv4' },
  { key: 'hostSourceIpv6', label: '服务器强制出站 IPv6' }
] as const;

watch(() => props.node.id, () => {
  Object.assign(form, props.node);
  const meta = props.meta;
  const price = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  const parsedRemark = parseNodeMetaRemark(meta.remark);
  Object.assign(metaForm, {
    region: meta.region || '',
    remark: parsedRemark.remark || '',
    renewDate: meta.renewDate || '',
    renewCurrency: price.currency,
    renewPrice: price.price,
    renewCycle: price.cycle,
    renewNoticeDays: meta.renewNoticeDays ?? 7,
    portRangeStart: meta.portRangeStart ?? parsedRemark.portRangeStart,
    portRangeEnd: meta.portRangeEnd ?? parsedRemark.portRangeEnd
  });
}, { immediate: true });

const snapshots = computed(() => ({
  basic: {
    name: form.name,
    publicIp: form.publicIp,
    region: metaForm.region,
    remark: metaForm.remark,
    portRangeStart: metaForm.portRangeStart,
    portRangeEnd: metaForm.portRangeEnd
  },
  renewal: {
    renewDate: metaForm.renewDate,
    renewCurrency: metaForm.renewCurrency,
    renewPrice: metaForm.renewPrice,
    renewCycle: metaForm.renewCycle,
    renewNoticeDays: metaForm.renewNoticeDays
  },
  access: { enabled: form.enabled, allowSubscription: form.allowSubscription, maintenance: form.maintenance },
  network: Object.fromEntries(networkFields.map(field => [field.key, form[field.key]]))
}));

const baseline = reactive<Record<string, unknown>>(JSON.parse(JSON.stringify(snapshots.value)));
const dirty = computed(() => Object.fromEntries(Object.entries(snapshots.value).map(([key, value]) => [key, isDirty(value, baseline[key])])));
const hasUnsaved = computed(() => Object.values(dirty.value).some(Boolean));

defineExpose({ hasUnsaved });

function ipv4(value: string) {
  return value.split('.').length === 4 && value.split('.').every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}

function ipv6(value: string) {
  try {
    return value.includes(':') && new URL(`http://[${value}]/`).hostname.startsWith('[');
  } catch {
    return false;
  }
}

function validateNetwork() {
  if (form.outboundSourceIp && !ipv4(form.outboundSourceIp) && !ipv6(form.outboundSourceIp)) {
    throw new Error('核心出站源 IP 需要有效 IPv4 或 IPv6 地址');
  }
  if (form.outboundInterface && !/^[A-Za-z0-9_.:-]{1,15}$/.test(form.outboundInterface)) {
    throw new Error('网卡名需为 1～15 个字母、数字或 _ . : -');
  }
  if (form.hostSourceIpv4 && !ipv4(form.hostSourceIpv4)) {
    throw new Error('服务器强制出站 IPv4 格式无效');
  }
  if (form.hostSourceIpv6 && !ipv6(form.hostSourceIpv6)) {
    throw new Error('服务器强制出站 IPv6 格式无效');
  }
}

async function save(group: 'basic' | 'renewal' | 'access' | 'network') {
  if (saving.value || !isOwner.value) return;
  saving.value = group;
  errors[group] = '';
  try {
    if (group === 'network') validateNetwork();
    if (group === 'basic') {
      const hasStart = metaForm.portRangeStart !== undefined && metaForm.portRangeStart !== null && (metaForm.portRangeStart as any) !== '';
      const hasEnd = metaForm.portRangeEnd !== undefined && metaForm.portRangeEnd !== null && (metaForm.portRangeEnd as any) !== '';
      if (hasStart && hasEnd) {
        const s = Number(metaForm.portRangeStart);
        const e = Number(metaForm.portRangeEnd);
        if (s < 1 || s > 65535 || e < 1 || e > 65535) {
          throw new Error('端口范围必须在 1 到 65535 之间');
        }
        if (s > e) {
          throw new Error('起始端口不能大于结束端口');
        }
      } else if (hasStart || hasEnd) {
        throw new Error('请同时指定起始端口和结束端口，或全部留空使用默认范围');
      }
    }

    const submitted = JSON.parse(JSON.stringify(snapshots.value[group]));
    const latest = await nodeAPI.get(props.node.id);
    const change = group === 'basic' ? { name: submitted.name, publicIp: submitted.publicIp } : submitted;
    if (group !== 'renewal') await nodeAPI.save({ ...latest, ...change });
    if (group === 'basic' || group === 'renewal') {
      const latestMeta = await loadNodeMeta(props.node.id, true);
      const metaChange = group === 'basic' ? {
        region: submitted.region,
        remark: submitted.remark,
        portRangeStart: submitted.portRangeStart ? Number(submitted.portRangeStart) : undefined,
        portRangeEnd: submitted.portRangeEnd ? Number(submitted.portRangeEnd) : undefined
      } : submitted;
      await saveNodeMeta(props.node.id, { ...latestMeta, ...metaChange });
    }
    baseline[group] = submitted;
    showToast(group === 'network' ? '出站网络已保存，发布配置后生效' : '设置已保存');
    emit('changed');
  } catch (e) {
    errors[group] = (e as Error).message;
    emit('changed');
  } finally {
    saving.value = '';
  }
}

async function revoke() {
  if (saving.value) return;
  saving.value = 'revoke';
  errors.revoke = '';
  try {
    await nodeAPI.revoke(props.node.id);
    showToast('服务器证书已吊销');
    emit('changed');
  } catch (e) {
    errors.revoke = (e as Error).message;
  } finally {
    saving.value = '';
  }
}
</script>

<style scoped>
#server-programs :deep(.panel-card) {
  padding: 0.875rem 1rem !important;
  border-color: rgba(0, 0, 0, 0.05);
}
</style>
