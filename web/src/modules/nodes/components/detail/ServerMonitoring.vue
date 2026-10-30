<template>
  <div class="space-y-6">
    <LatencyChart :node-id="node.id" />
    <section class="panel-card p-6 space-y-4">
      <h2 class="font-semibold text-ios-label">流量与活跃连接</h2>
      <dl v-if="flowAvailable" class="grid sm:grid-cols-2 lg:grid-cols-4 gap-4 text-sm">
        <div>
          <dt class="text-xs text-ios-secondary">核心累计上传</dt>
          <dd>
            <TrafficValue :bytes="fresh ? node.heartbeat?.upload_bytes : undefined" />
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">核心累计下载</dt>
          <dd>
            <TrafficValue :bytes="fresh ? node.heartbeat?.download_bytes : undefined" />
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">活跃代理用户会话</dt>
          <dd class="font-mono tabular-nums">{{ connections }}</dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">网卡接收 / 发送</dt>
          <dd class="font-mono tabular-nums">↓ {{ nicRate(metrics, 'receive') }} / ↑ {{ nicRate(metrics, 'transmit') }}</dd>
        </div>
      </dl>
      <p v-else class="text-sm text-ios-secondary">尚无有效的流量、连接或网卡速率样本。</p>
      <p class="text-xs text-ios-secondary">
        核心累计流量在核心重启后重新累计。活跃连接仅展示有效窗口内的 proxy_user_sessions_v1 样本。
        <span v-if="node.capabilities?.core_name === 'sing-box' && !node.capabilities?.singbox_user_stats">此核心仅上报服务器总量，无法按用户统计额度。</span>
      </p>
      <p class="text-xs text-ios-secondary">管理通道：{{ fresh ? '最近有心跳' : '离线 / 超时' }} · 心跳 {{ relativeHeartbeat(node.lastHeartbeat, now) }}。心跳新鲜度不代表网络延迟，当前无管理通道历史丢包数据。</p>
    </section>
    <section class="panel-card p-6 space-y-4">
      <h2 class="font-semibold text-ios-label">本地断线缓存与日志队列</h2>
      <p class="text-sm text-ios-secondary">{{ spoolLabel }}</p>
      <dl v-if="spoolFresh && spool?.measured" class="grid sm:grid-cols-2 lg:grid-cols-4 gap-4 text-sm">
        <div>
          <dt class="text-xs text-ios-secondary">缓存文件 / 预算</dt>
          <dd>
            <TrafficValue :bytes="spool.disk_bytes" />
            /
            <TrafficValue :bytes="spool.disk_limit" />
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">数据库页 / 上限</dt>
          <dd>
            <TrafficValue :bytes="spool.database_bytes" />
            /
            <TrafficValue :bytes="spool.database_limit" />
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">加密记录载荷</dt>
          <dd>
            <TrafficValue :bytes="spool.payload_bytes" />
          </dd>
        </div>
        <div>
          <dt class="text-xs text-ios-secondary">待补传批次 / 淘汰记录</dt>
          <dd class="font-mono tabular-nums">{{ spool.pending_batches ?? '—' }} / {{ spool.dropped_allowed_rollups ?? '—' }}</dd>
        </div>
      </dl>
    </section>
    <NodeHealthPanel v-if="isOwner && !node.deletedAt" :node="node" />
    <section class="panel-card p-6">
      <TCPProbesPanel :node-id="node.id" />
    </section>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../../api';
import { hostMetrics, nicRate } from '../../metrics';
import { isOwner } from '../../../../app/auth';
import { relativeHeartbeat } from '../serverListModel';
import TrafficValue from '../../../../components/TrafficValue.vue';
import LatencyChart from '../LatencyChart.vue';
import NodeHealthPanel from '../NodeHealthPanel.vue';
import TCPProbesPanel from '../../TCPProbesPanel.vue';
const props = defineProps<{ node: NodeView; now: number }>();
const metrics = computed(() => hostMetrics(props.node, props.now));
const fresh = computed(() => props.node.enabled && Object.keys(metrics.value).length > 0);
const connections = computed(() => {
  const h = props.node.heartbeat, age = props.now - (h.active_connections_observed_at?.seconds ?? 0) * 1000;
  return fresh.value && h.core_healthy === true && h.active_connections_measured === true && h.active_connections_scope === 'proxy_user_sessions_v1' && age >= -5000 && age < 45000 ? h.active_connections ?? 0 : '—';
});
const flowAvailable = computed(() => fresh.value && (props.node.heartbeat?.upload_bytes !== undefined || props.node.heartbeat?.download_bytes !== undefined || connections.value !== '—' || nicRate(metrics.value, 'receive') !== '—' || nicRate(metrics.value, 'transmit') !== '—'));
const spool = computed(() => props.node.heartbeat?.spool_status);
const spoolFresh = computed(() => { const age = props.now - (spool.value?.observed_at?.seconds ?? 0) * 1000; return fresh.value && ['online', 'degraded'].includes(props.node.status) && age >= -5000 && age < 45000; });
const spoolLabel = computed(() => !spoolFresh.value ? '尚未上报缓存状态或样本已过期。' : ({ ok: '断线缓存队列与存储空间正常。', near_capacity: '接近缓存容量（达到 85%），请核对补传进度与磁盘状态。', capacity_exceeded: '已达到缓存容量上限，先补传已缓存记录并核对容量文件。', unavailable: '缓存容量或队首记录无法校验。' }[spool.value?.status ?? 'unavailable']));
</script>
