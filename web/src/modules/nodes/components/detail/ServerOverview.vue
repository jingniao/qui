<template>
  <div class="space-y-6">
    <section v-if="resources.some(field => field.value !== '—')" class="grid grid-cols-2 lg:grid-cols-3 gap-4">
      <div v-for="field in resources" :key="field.label" class="panel-card p-5">
        <p class="text-xs text-ios-secondary">{{ field.label }}</p>
        <p class="mt-2 text-lg font-semibold text-ios-label font-mono tabular-nums">{{ field.value }}</p>
        <div v-if="field.value.endsWith('%')" class="h-2 mt-3 rounded-full bg-ios-fill overflow-hidden">
          <div class="h-full rounded-full" :class="parseFloat(field.value) >= 90 ? 'bg-ios-red' : parseFloat(field.value) >= 75 ? 'bg-ios-orange' : 'bg-ios-green'" :style="{ width: `${Math.max(0, Math.min(100, parseFloat(field.value)))}%` }"></div>
        </div>
      </div>
    </section>
    <p v-else class="text-sm text-ios-secondary">尚无有效的实时资源样本。</p>
    <section v-if="today !== undefined || month !== undefined" class="grid sm:grid-cols-2 gap-4">
      <div class="panel-card p-5">
        <p class="text-xs text-ios-secondary">今日流量</p>
        <TrafficValue :bytes="today" />
      </div>
      <div class="panel-card p-5">
        <p class="text-xs text-ios-secondary">本月流量</p>
        <TrafficValue :bytes="month" />
      </div>
    </section>
    <p v-else class="text-sm text-ios-secondary">尚无可读取的今日或本月账户流量样本。</p>
    <p class="text-xs text-ios-secondary">流量按面板时区 {{ authState.timezone }} 汇总保留的账户样本；实时资源超过 45 秒显示“—”。</p>
    <section class="panel-card p-6 space-y-4">
      <h2 class="font-semibold text-ios-label">基础信息</h2>
      <dl class="grid sm:grid-cols-2 lg:grid-cols-3 gap-4 text-sm">
        <div v-for="field in basics" :key="field[0]">
          <dt class="text-xs text-ios-secondary">{{ field[0] }}</dt>
          <dd class="mt-1 break-all text-ios-label font-mono tabular-nums">{{ field[1] || '—' }}</dd>
        </div>
      </dl>
    </section>
    <section class="panel-card p-6 space-y-3">
      <h2 class="font-semibold text-ios-label">核心与协议能力</h2>
      <p v-if="!node.capabilities?.protocols?.length" class="text-sm text-ios-secondary">服务器尚未上报协议能力。</p>
      <div v-else class="flex flex-wrap gap-3">
        <span v-for="cap in node.capabilities.protocols" :key="cap.protocol" class="rounded-xl bg-ios-bg px-3 py-2 text-xs text-ios-secondary" :title="`入站${cap.inbound ? '支持' : '不支持'}；出站${cap.outbound ? '支持' : '不支持'}；TCP${cap.tcp ? '支持' : '不支持'}；UDP${cap.udp ? '支持' : '不支持'}（服务器上报）`">
          <strong class="text-ios-label uppercase">{{ cap.protocol }}</strong>
          · 入站
          {{ cap.inbound ? '✓' : '不支持' }}
          / 出站
          {{ cap.outbound ? '✓' : '不支持' }}
          · TCP
          {{ cap.tcp ? '✓' : '不支持' }}
          / UDP
          {{ cap.udp ? '✓' : '不支持' }}
        </span>
      </div>
    </section>
    <section v-if="events.length" class="panel-card p-6 space-y-3">
      <h2 class="font-semibold text-ios-label">最近事件</h2>
      <ol class="space-y-3">
        <li v-for="event in events" :key="event.id" class="border-l-2 border-ios-border pl-3 text-sm text-ios-secondary">
          <time class="text-xs font-mono tabular-nums">{{ formatTime(event.at) }}</time>
          <p>{{ event.text }}</p>
        </li>
      </ol>
    </section>
    <p v-else class="text-xs text-ios-secondary">当前没有可读取的配置事件记录。</p>
  </div>
</template>
<script setup lang="ts">
import { computed, ref, onMounted, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import type { Revision } from '../../../system/deploymentApi';
import { type NodeMeta, formatRenewPrice, resolveCountryInfo, onGeoResolved } from '../../nodeMeta';
import { hostMetrics, uptime, nicRate } from '../../metrics';
import { authState, formatTime } from '../../../../app/auth';
import TrafficValue from '../../../../components/TrafficValue.vue';
const props = defineProps<{ node: NodeView; meta: NodeMeta; now: number; today?: number; month?: number; revisions: Revision[] }>();
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
const basics = computed(() => [
  ['服务器 ID', props.node.id], ['主机名', props.node.hostname], ['操作系统', props.node.os],
  ['地区', props.meta.region || (country.value.code ? `${country.value.flag} ${country.value.name}` : '—')],
  ['备注', props.meta.remark], ['创建时间', formatTime(props.node.createdAt)], ['续费价格', formatRenewPrice(props.meta)],
  ['核心出站源 IP', props.node.outboundSourceIp], ['核心出站网卡', props.node.outboundInterface], ['服务器强制出站 IPv4', props.node.hostSourceIpv4], ['服务器强制出站 IPv6', props.node.hostSourceIpv6]
]);
const events = computed(() => {
  const result = props.revisions.slice(0, 5).map(rev => ({ id: String(rev.id), at: rev.createdAt, text: `配置 #${rev.id} · ${{ draft: '草稿', pending: '待确认', success: '应用成功', failed: '失败', rolled_back: '已回滚' }[rev.status] || rev.status} · ${rev.summary} ${rev.resultCode || ''}` }));
  if (props.node.heartbeat?.core_healthy === false) result.unshift({ id: 'core', at: props.node.lastHeartbeat, text: `核心异常：${props.node.heartbeat.core_error === 'accounting_interrupted' ? '记账中断' : '健康检查未通过'}` });
  return result.slice(0, 5);
});
</script>
