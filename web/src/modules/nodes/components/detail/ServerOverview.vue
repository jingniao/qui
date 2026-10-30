<template>
  <div class="space-y-3.5 sm:space-y-4">
    <!-- Top 4 Financial KPI Cards (Full Width 4 Columns) -->
    <ServerFinancialCards :node="node" :meta="meta" />

    <!-- 基础信息与系统信息合并卡片 (Full Width 4 Columns) -->
    <section class="panel-card p-3.5 sm:p-4 bg-white space-y-3">
      <!-- Header: Title, Status Badge, Heartbeat & Subscriptions -->
      <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5 flex-wrap gap-2">
        <div class="flex items-center gap-2">
          <h2 class="font-semibold text-ios-label text-sm sm:text-base">基础信息</h2>
          <StatusBadge :status="status.tone" :label="status.label" />
          <span class="text-xs text-ios-secondary font-mono">
            {{ connections === '—' ? '活跃会话 —' : `${connections} 个活跃会话` }}
          </span>
        </div>

        <div class="flex items-center gap-3 text-xs text-ios-secondary">
          <span class="flex items-center gap-1.5" :title="'心跳报告: ' + (node.lastHeartbeat ? formatTime(node.lastHeartbeat) : '从晚上报')">
            <span class="w-1.5 h-1.5 rounded-full" :class="status.tone === 'active' ? 'bg-emerald-500 animate-pulse' : 'bg-neutral-300'"></span>
            <span>{{ heartbeatRelative }}</span>
          </span>
          <span class="text-black/[0.15]">|</span>
          <span :class="node.enabled && node.allowSubscription && !node.maintenance ? 'text-ios-blue font-medium' : 'text-neutral-400'">
            {{ !node.enabled ? '已停用' : (node.maintenance ? '维护中' : (node.allowSubscription ? '允许订阅' : '订阅隐藏')) }}
          </span>
          <span class="text-black/[0.15]">|</span>
          <button
            class="text-[11px] text-ios-blue hover:underline inline-flex items-center gap-0.5 font-medium transition-colors"
            :title="'复制完整服务器 ID: ' + node.id"
            @click="copyId"
          >
            复制 ID
          </button>
        </div>
      </div>

      <!-- Content Grid: 4 Columns Balanced Presentation -->
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2.5">
        <!-- 主机名 -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">主机名</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label font-mono truncate" :title="node.hostname">
            {{ node.hostname || '—' }}
          </p>
        </div>

        <!-- 公网 IP -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="flex items-center justify-between">
            <span class="text-[11px] sm:text-xs text-ios-secondary font-medium">公网 IP</span>
            <button
              v-if="node.publicIp"
              class="text-[11px] text-ios-blue hover:underline inline-flex items-center gap-0.5 font-medium transition-colors"
              title="复制公网 IP"
              @click="copyIp"
            >
              复制
            </button>
          </div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label font-mono truncate" :title="node.publicIp">
            {{ node.publicIp || '—' }}
          </p>
        </div>

        <!-- 地区 -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">地区</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label truncate" :title="regionDisplay">
            {{ regionDisplay }}
          </p>
        </div>

        <!-- 操作系统 -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">操作系统</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label truncate" :title="node.capabilities?.host?.osPrettyName || node.os">
            {{ node.capabilities?.host?.osPrettyName || node.os || '—' }}
          </p>
        </div>

        <!-- 运行时间 (来自系统信息) -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">运行时间</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label font-mono tabular-nums truncate">
            {{ uptimeDisplay }}
          </p>
        </div>

        <!-- 启动时间 (来自系统信息) -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">启动时间</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label font-mono tabular-nums truncate">
            {{ bootTime }}
          </p>
        </div>

        <!-- 服务器端口范围 -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">服务器端口范围</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label font-mono truncate" :title="formatPortRange(meta)">
            {{ formatPortRange(meta) }}
          </p>
        </div>

        <!-- 创建时间 -->
        <div class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 space-y-0.5">
          <div class="text-[11px] sm:text-xs text-ios-secondary font-medium">创建时间</div>
          <p class="text-xs sm:text-sm font-semibold text-ios-label truncate">
            {{ node.createdAt ? formatTime(node.createdAt) : '—' }}
          </p>
        </div>
      </div>

      <!-- 备注说明 -->
      <div v-if="cleanRemark(meta.remark)" class="bg-black/[0.02] border border-black/[0.04] rounded-xl p-2.5 flex items-center justify-between">
        <div class="flex items-center gap-2 min-w-0">
          <span class="text-[11px] sm:text-xs text-ios-secondary font-medium shrink-0">备注说明</span>
          <span class="text-xs sm:text-sm font-medium text-ios-label truncate" :title="cleanRemark(meta.remark)">
            {{ cleanRemark(meta.remark) }}
          </span>
        </div>
      </div>
    </section>

    <!-- 3 System Performance Panels: 硬件信息, 存储信息, 网络信息 (Full Width 3 Columns) -->
    <ServerSystemPanels
      :node="node"
      :metrics="metrics"
      :now="now"
      :today="today"
      :month="month"
    />

    <!-- Bottom Section: Services & Configurations (Balanced 2-Column Grid) -->
    <div class="grid grid-cols-1 lg:grid-cols-2 gap-3.5 sm:gap-4 items-start">
      <!-- Left: 节点概览 & 协议能力 -->
      <div class="space-y-3.5 sm:space-y-4 min-w-0">
        <!-- 节点概览 -->
        <ServerNodeSummary :node="node" @manage="$emit('tab', 'nodes')" @create="$emit('create')" />

        <!-- 协议能力 -->
        <section class="panel-card p-3.5 sm:p-4 space-y-2.5 bg-white">
          <div class="flex items-center justify-between">
            <h2 class="font-semibold text-ios-label text-sm sm:text-base">协议能力</h2>
            <button v-if="customNetwork && isOwner" class="text-xs text-ios-blue hover:underline" @click="$emit('tab', 'settings')">
              出站网络设置 →
            </button>
            <span v-else-if="customNetwork" class="text-xs text-ios-secondary">出站网络已自定义</span>
          </div>
          <div v-if="cores.length" class="space-y-1">
            <div v-for="core in cores" :key="core.name" class="flex items-center justify-between gap-2 rounded-xl bg-ios-bg px-2.5 py-1.5">
              <span class="text-xs font-semibold text-ios-label truncate">
                {{ core.label }}<span class="ml-1.5 font-mono font-normal text-ios-secondary">{{ core.version }}</span>
                <span v-if="core.primary" class="ml-1.5 text-[10px] font-semibold text-ios-blue">主核心</span>
              </span>
              <span
                class="inline-flex items-center gap-1 text-[11px] shrink-0"
                :class="core.healthy === false ? 'text-ios-red' : core.healthy === true ? 'text-ios-green' : 'text-ios-tertiary'"
                :title="core.error || ''"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="core.healthy === false ? 'bg-ios-red' : core.healthy === true ? 'bg-ios-green' : 'bg-ios-tertiary'"></span>
                {{ core.healthy === false ? '异常' : core.healthy === true ? '正常' : '—' }}
              </span>
            </div>
          </div>
          <p v-else class="text-xs text-ios-secondary">尚未上报运行内核</p>
          <div v-if="node.capabilities?.protocols?.length" class="flex flex-wrap gap-1.5">
            <span
              v-for="p in node.capabilities.protocols"
              :key="p.protocol"
              class="px-2 py-0.5 rounded-md text-xs font-mono font-medium"
              :class="p.inbound && p.outbound ? 'bg-blue-50 text-ios-blue border border-blue-100/70' : (p.inbound ? 'bg-emerald-50 text-emerald-700 border border-emerald-100/70' : 'bg-neutral-100 text-neutral-600')"
            >
              {{ p.protocol }}
              <span class="text-[10px] opacity-75 font-sans ml-0.5">{{ p.inbound && p.outbound ? '双向' : (p.inbound ? '入站' : '出站') }}</span>
            </span>
          </div>
          <p v-else class="text-xs text-ios-secondary">尚未上报协议能力</p>
        </section>
      </div>

      <!-- Right: 最近事件（配置版本记录） -->
      <section class="panel-card p-3.5 sm:p-4 space-y-2.5 bg-white">
        <div class="flex items-center justify-between">
          <h2 class="font-semibold text-ios-label text-sm sm:text-base">最近事件</h2>
          <span class="text-xs text-ios-tertiary">配置版本记录</span>
        </div>
        <ol v-if="events.length" class="space-y-2">
          <li v-for="event in events" :key="event.id" class="border-l-2 border-ios-blue/40 pl-2.5 text-xs text-ios-secondary">
            <time class="text-[11px] font-mono text-ios-tertiary">{{ formatTime(event.at) }}</time>
            <p class="text-ios-label font-medium text-xs mt-0.5">{{ event.text }}</p>
          </li>
        </ol>
        <p v-else class="text-xs text-ios-secondary">当前没有可读取的配置事件记录。</p>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import type { Revision } from '../../../system/deploymentApi';
import { type NodeMeta, resolveCountryInfo, onGeoResolved, cleanRemark, formatPortRange } from '../../nodeMeta';
import { hostMetrics, uptime } from '../../metrics';
import { formatTime } from '../../../../app/auth';
import { serverStatus, relativeHeartbeat } from '../serverListModel';
import { revisionEventText } from './serverDetailModel';
import { nodeCores } from '../../coreSummary';
import type { NodeGroup } from '../../groupsApi';
import StatusBadge from '../../../../components/StatusBadge.vue';
import ServerNodeSummary from './ServerNodeSummary.vue';
import ServerFinancialCards from './ServerFinancialCards.vue';
import ServerSystemPanels from './ServerSystemPanels.vue';
import { isOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';

const props = defineProps<{
  node: NodeView;
  meta: NodeMeta;
  now: number;
  today?: number;
  month?: number;
  revisions: Revision[];
  groups?: NodeGroup[];
  latestRevision?: number;
}>();

const geoTick = ref(0);
let unsubscribe: (() => void) | undefined;
onMounted(() => {
  unsubscribe = onGeoResolved(() => {
    geoTick.value++;
  });
});
onUnmounted(() => {
  if (unsubscribe) unsubscribe();
});

const regionDisplay = computed(() => {
  void geoTick.value;
  const adminRegion = props.meta.region?.trim();
  if (adminRegion) return adminRegion;
  const info = resolveCountryInfo(props.meta?.region, props.node.publicIp, props.node.name, props.node.hostname);
  if (info && info.code !== 'UN') {
    return `${info.flag} ${info.name}`;
  }
  return '—';
});

const heartbeatRelative = computed(() => {
  if (props.node.lastHeartbeat) {
    return relativeHeartbeat(props.node.lastHeartbeat, props.now);
  }
  return '—';
});

const metrics = computed(() => hostMetrics(props.node, props.now));

const uptimeDisplay = computed(() => {
  return uptime(metrics.value.host_uptime_seconds);
});

const bootTime = computed(() => {
  const s = metrics.value.host_uptime_seconds;
  if (s === undefined || !Number.isFinite(s) || s <= 0) return '—';
  const bootDate = new Date(props.now - s * 1000);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${bootDate.getFullYear()}-${pad(bootDate.getMonth() + 1)}-${pad(bootDate.getDate())} ${pad(bootDate.getHours())}:${pad(bootDate.getMinutes())}:${pad(bootDate.getSeconds())}`;
});

const connections = computed(() => {
  const h = Object.keys(metrics.value).length ? props.node.heartbeat : undefined;
  const at = (h?.active_connections_observed_at?.seconds ?? 0) * 1000;
  return h?.active_connections_measured && h.active_connections_scope === 'proxy_user_sessions_v1' && props.node.enabled && h.core_healthy === true && props.now - at >= -5000 && props.now - at < 45000 ? String(h.active_connections ?? 0) : '—';
});

const status = computed(() => serverStatus(props.node, props.now));
// Per-core view (K3b): each reported core with its own version and health. A
// legacy Agent reports one core; a missing health observation stays "—".
const cores = computed(() => nodeCores(props.node));
const customNetwork = computed(() => !!(props.node.outboundSourceIp || props.node.outboundInterface || props.node.hostSourceIpv4 || props.node.hostSourceIpv6));

defineEmits<{ changed: []; tab: [value: string]; create: [] }>();

async function copyId() {
  try {
    await navigator.clipboard.writeText(props.node.id);
    showToast('已复制服务器 ID');
  } catch {
    showToast('复制失败，请手动复制 ID', 'error');
  }
}

async function copyIp() {
  if (!props.node.publicIp) return;
  try {
    await navigator.clipboard.writeText(props.node.publicIp);
    showToast('已复制公网 IP');
  } catch {
    showToast('复制失败', 'error');
  }
}

const events = computed(() => {
  const result = props.revisions.slice(0, 5).map(rev => ({
    id: String(rev.id),
    at: rev.createdAt,
    text: revisionEventText(rev)
  }));
  return result;
});
</script>
