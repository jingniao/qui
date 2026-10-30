<template>
  <div class="grid lg:grid-cols-3 gap-6 items-start">
    <div class="lg:col-span-2 space-y-6 min-w-0">
      <section class="panel-card p-5 space-y-3">
        <h2 class="font-semibold text-ios-label">状态</h2>
        <div class="flex flex-wrap items-center gap-3 text-sm text-ios-secondary"><StatusBadge :status="status.tone" :label="status.label" /><span>核心 {{ node.capabilities?.core_name || '—' }} {{ node.capabilities?.core_version || '' }} · {{ health }}</span><span>Agent {{ node.agentVersion || '—' }}</span></div>
        <p class="text-sm text-ios-secondary">配置 {{ strip.running ? '#' + strip.running : '—' }} · {{ strip.state === 'latest' ? '最新' : strip.state === 'unapplied' ? '有更新' : '尚无最新版本信息' }} · 心跳 {{ relativeHeartbeat(node.lastHeartbeat, now) }}</p>
        <p class="text-xs text-ios-secondary">接入：{{ node.deletedAt ? '已删除' : !node.enabled ? '已禁用' : node.maintenance ? '维护中（暂不分发）' : !node.allowSubscription ? '不分发订阅' : '允许订阅' }}</p>
      </section>
      <section class="panel-card p-5 space-y-4">
        <h2 class="font-semibold text-ios-label" :title="'流量按面板时区 ' + authState.timezone + ' 汇总保留的账户样本；实时资源超过 45 秒显示未知'">运行状态 ⓘ</h2>
        <div v-if="resources.some(field => field.value !== '—') || today !== undefined || month !== undefined" class="grid grid-cols-2 xl:grid-cols-4 gap-4 text-sm">
          <div v-for="field in resources" :key="field.label"><p class="text-xs text-ios-secondary mb-2">{{ field.label }}</p><ResourceProgress v-if="field.value.endsWith('%')" :percentage="parseFloat(field.value)" /><p v-else class="font-mono text-ios-label tabular-nums">{{ field.value }}</p></div>
          <div><p class="text-xs text-ios-secondary mb-2">今日流量</p><TrafficValue :bytes="today" /></div><div><p class="text-xs text-ios-secondary mb-2">本月流量</p><TrafficValue :bytes="month" /></div>
        </div>
        <p v-else class="text-sm text-ios-secondary">尚无有效的资源或账户流量样本。</p>
      </section>
      <LatencyChart :node-id="node.id" compact @monitoring="$emit('tab', 'monitoring')" />
      <ServerNodeSummary :node="node" @manage="$emit('tab', 'nodes')" @create="$emit('create')" />
      <section class="panel-card p-5 space-y-3"><h2 class="font-semibold text-ios-label">最近事件</h2><ol v-if="events.length" class="space-y-3"><li v-for="event in events" :key="event.id" class="border-l-2 border-ios-separator pl-3 text-sm text-ios-secondary"><time class="text-xs font-mono">{{ formatTime(event.at) }}</time><p>{{ event.text }}</p></li></ol><p v-else class="text-sm text-ios-secondary">当前没有可读取的配置事件记录。</p></section>
    </div>
    <aside class="space-y-6 min-w-0">
      <section class="panel-card p-5 space-y-3"><h2 class="font-semibold text-ios-label">基础信息</h2><dl class="space-y-3 text-sm"><div><dt class="text-xs text-ios-secondary">服务器 ID</dt><dd class="text-ios-label font-mono break-all"><span :title="node.id">{{ node.id.length > 12 ? node.id.slice(0, 8) + '…' + node.id.slice(-4) : node.id }}</span> <button class="ios-action-btn-secondary" @click="copyId">复制</button></dd></div><div v-for="field in basics" :key="field[0]"><dt class="text-xs text-ios-secondary">{{ field[0] }}</dt><dd class="break-all text-ios-label">{{ field[1] }}</dd></div></dl></section>
      <ServerDetailGroups :node="node" :groups="groups" @changed="$emit('changed')" />
      <section class="panel-card p-5 space-y-3"><h2 class="font-semibold text-ios-label">续费</h2><template v-if="meta.renewDate || formatRenewPrice(meta)"><p v-if="meta.renewDate" class="text-sm text-ios-label">{{ meta.renewDate }} · {{ calculateRemainingDays(meta.renewDate).label }}</p><p v-if="formatRenewPrice(meta)" class="text-sm text-ios-secondary">{{ formatRenewPrice(meta) }}</p></template><p v-else class="text-sm text-ios-secondary">尚未填写续费信息。</p></section>
      <section class="panel-card p-5 space-y-3"><h2 class="font-semibold text-ios-label">协议能力</h2><div v-if="node.capabilities?.protocols?.length" class="flex flex-wrap gap-2"><span v-for="cap in node.capabilities.protocols" :key="cap.protocol" :title="'入站：' + cap.inbound + '；出站：' + cap.outbound + '；TCP：' + cap.tcp + '；UDP：' + cap.udp + '（服务器上报）'"><ProtocolBadge :protocol="cap.protocol" /></span></div><p v-else class="text-sm text-ios-secondary">服务器尚未上报协议能力。</p></section>
      <button v-if="customNetwork && isOwner" class="text-sm text-ios-blue" @click="$emit('tab', 'settings')">出站网络已自定义 → 设置</button><p v-else-if="customNetwork" class="text-sm text-ios-secondary">出站网络已自定义</p>
    </aside>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import type { Revision } from '../../../system/deploymentApi';
import { type NodeMeta, formatRenewPrice, calculateRemainingDays, resolveCountryInfo, onGeoResolved } from '../../nodeMeta';
import { hostMetrics, uptime, nicRate } from '../../metrics';
import { authState, formatTime } from '../../../../app/auth';
import { basicFacts, revisionStrip } from './serverDetailModel';
import { serverStatus, relativeHeartbeat } from '../serverListModel';
import type { NodeGroup } from '../../groupsApi';
import StatusBadge from '../../../../components/StatusBadge.vue';
import ProtocolBadge from '../../../../components/ProtocolBadge.vue';
import ResourceProgress from '../../../../components/ResourceProgress.vue';
import LatencyChart from '../LatencyChart.vue';
import ServerDetailGroups from './ServerDetailGroups.vue';
import ServerNodeSummary from './ServerNodeSummary.vue';
import { isOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import TrafficValue from '../../../../components/TrafficValue.vue';
const props = defineProps<{ node: NodeView; meta: NodeMeta; now: number; today?: number; month?: number; revisions: Revision[]; groups: NodeGroup[]; latestRevision?: number }>();
const geoTick = ref(0);
let unsubscribe: (() => void) | undefined;
onMounted(() => { unsubscribe = onGeoResolved(() => { geoTick.value++; }); });
onUnmounted(() => unsubscribe?.());
const country = computed(() => { void geoTick.value; return resolveCountryInfo(props.meta.region, props.node.publicIp, props.node.name, props.node.hostname); });
const metrics = computed(() => hostMetrics(props.node, props.now));
const resources = computed(() => {
  const h = Object.keys(metrics.value).length ? props.node.heartbeat : undefined;
  const percent = (v?: number) => typeof v === 'number' && Number.isFinite(v) ? `${v.toFixed(1)}%` : '—';
  const at = (h?.active_connections_observed_at?.seconds ?? 0) * 1000;
  const connections = h?.active_connections_measured && h.active_connections_scope === 'proxy_user_sessions_v1' && props.node.enabled && h.core_healthy === true && props.now - at >= -5000 && props.now - at < 45000 ? String(h.active_connections ?? 0) : '—';
  return [{ label: 'CPU', value: percent(h?.cpu_percent) }, { label: '内存', value: percent(h?.memory_percent) }, { label: '磁盘', value: percent(h?.disk_percent) }, { label: '活跃连接', value: connections }, { label: '网卡接收 / 发送', value: nicRate(metrics.value, 'receive') === '—' && nicRate(metrics.value, 'transmit') === '—' ? '—' : `↓ ${nicRate(metrics.value, 'receive')} / ↑ ${nicRate(metrics.value, 'transmit')}` }, { label: '主机运行时间', value: uptime(metrics.value.host_uptime_seconds) }, { label: '核心运行时间', value: uptime(metrics.value.core_started_at?.seconds === undefined ? undefined : props.now / 1000 - metrics.value.core_started_at.seconds) }];
});
const basics = computed(() => basicFacts([
  ['主机名', props.node.hostname], ['公网 IP', props.node.publicIp], ['操作系统', props.node.os],
  ['地区', props.meta.region || (country.value.code ? country.value.flag + ' ' + country.value.name : '')],
  ['备注', props.meta.remark], ['创建时间', props.node.createdAt ? formatTime(props.node.createdAt) : '']
]));
const status = computed(() => serverStatus(props.node, props.now));
const strip = computed(() => revisionStrip(props.node.heartbeat?.current_revision, props.latestRevision));
const customNetwork = computed(() => !!(props.node.outboundSourceIp || props.node.outboundInterface || props.node.hostSourceIpv4 || props.node.hostSourceIpv6));
const health = computed(() => !Object.keys(metrics.value).length ? '—' : props.node.heartbeat?.core_healthy === true ? '健康' : props.node.heartbeat?.core_healthy === false ? props.node.heartbeat.core_error === 'accounting_interrupted' ? '记账中断' : '核心异常' : '—');
defineEmits<{ changed: []; tab: [value: string]; create: [] }>();
async function copyId() { try { await navigator.clipboard.writeText(props.node.id); showToast('已复制服务器 ID'); } catch { showToast('复制失败，请手动复制 ID', 'error'); } }
const events = computed(() => {
  const result = props.revisions.slice(0, 5).map(rev => ({ id: String(rev.id), at: rev.createdAt, text: `配置 #${rev.id} · ${{ draft: '草稿', pending: '待确认', success: '应用成功', failed: '失败', rolled_back: '已回滚' }[rev.status] || rev.status} · ${rev.summary} ${rev.resultCode || ''}` }));
  if (props.node.heartbeat?.core_healthy === false) result.unshift({ id: 'core', at: props.node.lastHeartbeat, text: `核心异常：${props.node.heartbeat.core_error === 'accounting_interrupted' ? '记账中断' : '健康检查未通过'}` });
  return result.slice(0, 5);
});
</script>
