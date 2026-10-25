<template>
  <div class="space-y-6">
    <PageHeader :title="node?.name || '节点详情'" parent="节点" description="此页仅展示真实注册信息、心跳与已上报能力。">
      <template #actions>
        <router-link to="/nodes" class="ios-btn-secondary">返回列表</router-link>
        <button @click="load" class="ios-btn-secondary">刷新</button>
        <button v-if="isOwner && node && !node.deletedAt" @click="openSettings" class="ios-btn-secondary">节点设置</button>
        <button v-if="isOwner && node && !node.deletedAt" @click="revokeVisible = true" class="ios-btn-danger">吊销证书</button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!node" />

    <template v-else>
      <!-- 全局异常警报（置顶醒目呈现，不随标签页切换遗漏） -->
      <section v-if="node.heartbeat?.core_healthy === false" class="panel-card p-5 border border-amber-300 bg-amber-50 text-sm text-amber-900" role="status">
        <p class="font-semibold">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '代理已停止，流量消费需要核对' : '代理核心未通过健康检查' }}</p>
        <p class="mt-2">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '节点保持管理连接，但不会根据旧计数重新放行。已采集流量会继续补传；无法确认的消费仍需处理。' : '节点仍可连接管理端。代理恢复前，请检查核心运行状态和最近配置发布结果。' }}</p>
        <p class="mt-2 text-xs">以上为最后一次心跳上报的状态。</p>
      </section>

      <!-- iOS 分段药丸分类导航 -->
      <div class="ios-segmented-container inline-flex overflow-x-auto max-w-full">
        <button
          v-for="tab in tabs"
          :key="tab.key"
          type="button"
          @click="selectTab(tab.key)"
          :class="['ios-segmented-item whitespace-nowrap', activeTab === tab.key ? 'active' : '']"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- Tab 1: 运行概览 -->
      <div v-if="activeTab === 'overview'" class="space-y-6">
        <!-- 节点基本属性卡片 -->
        <section class="panel-card p-6">
          <div class="flex gap-3 items-center mb-4">
            <StatusBadge kind="node" :status="node.status" />
            <span v-if="node.deletedAt" class="rounded-full bg-black/[0.05] px-2 py-0.5 text-[11px] font-semibold text-ios-secondary">已删除</span>
            <span class="text-sm font-medium text-ios-secondary">{{ node.enabled ? '已启用' : '已禁用' }}</span>
          </div>
          <dl class="grid grid-cols-1 md:grid-cols-2 gap-5 text-sm">
            <div><dt class="text-xs font-medium text-ios-tertiary">节点 ID</dt><dd class="font-mono break-all mt-1 text-sm font-medium text-ios-label">{{ node.id }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">主机</dt><dd class="mt-1 text-sm font-medium text-ios-label">{{ node.hostname || '等待注册' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">操作系统</dt><dd class="mt-1 text-sm font-medium text-ios-label">{{ node.os || '—' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">公网 IP</dt><dd class="mt-1 text-sm font-medium text-ios-label font-mono tabular-nums">{{ node.publicIp || '尚未上报' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">Agent 版本</dt><dd class="mt-1 text-sm font-medium text-ios-label font-mono">{{ node.agentVersion || '—' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">最后心跳</dt><dd class="mt-1 text-sm font-medium text-ios-label font-mono tabular-nums">{{ formatTime(node.lastHeartbeat) }}</dd></div>
          </dl>
        </section>

        <!-- 实时资源 -->
        <section class="panel-card p-6">
          <h2 class="font-semibold mb-4 text-[17px] text-ios-label tracking-tight">实时资源</h2>
          <dl class="grid grid-cols-3 gap-4 text-sm">
            <div><dt class="text-xs font-medium text-ios-tertiary">CPU</dt><dd class="mt-2 text-2xl font-bold text-ios-label tabular-nums tracking-tight">{{ resource(node.heartbeat?.cpu_percent) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">内存</dt><dd class="mt-2 text-2xl font-bold text-ios-label tabular-nums tracking-tight">{{ resource(node.heartbeat?.memory_percent) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">磁盘</dt><dd class="mt-2 text-2xl font-bold text-ios-label tabular-nums tracking-tight">{{ resource(node.heartbeat?.disk_percent) }}</dd></div>
          </dl>
          <p class="text-xs text-ios-tertiary mt-4">CPU 需要一个采样窗口；尚未测量的项目显示为 —。</p>
        </section>

        <!-- 节点累计流量与当前连接 -->
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <h2 class="font-semibold mb-4 text-[17px] text-ios-label tracking-tight">节点累计流量（核心上报）</h2>
              <dl class="grid grid-cols-2 gap-4 text-sm">
                <div><dt class="text-xs font-medium text-ios-tertiary">上传</dt><dd class="mt-2 text-xl font-bold text-ios-label tabular-nums"><TrafficValue v-if="node.heartbeat?.upload_bytes !== undefined" :bytes="node.heartbeat.upload_bytes" /><span v-else>—</span></dd></div>
                <div><dt class="text-xs font-medium text-ios-tertiary">下载</dt><dd class="mt-2 text-xl font-bold text-ios-label tabular-nums"><TrafficValue v-if="node.heartbeat?.download_bytes !== undefined" :bytes="node.heartbeat.download_bytes" /><span v-else>—</span></dd></div>
              </dl>
            </div>
            <p class="text-xs text-ios-tertiary mt-4 leading-relaxed">{{ nodeTrafficLabel }}</p>
          </section>

          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <h2 class="font-semibold mb-4 text-[17px] text-ios-label tracking-tight">当前用户连接</h2>
              <p class="text-3xl font-bold text-ios-label tabular-nums tracking-tight mt-1">{{ connectionCount() }}</p>
            </div>
            <p class="text-xs text-ios-tertiary mt-4 leading-relaxed">Xray 节点上报该用户的去重客户端 IP 数（回环地址排除），sing-box 节点不提供每用户在线数、显示未知；样本超过 45 秒、节点离线或核心异常时同样显示未知。</p>
          </section>
        </div>

        <!-- 核心与协议能力 -->
        <section class="panel-card p-6">
          <h2 class="font-semibold mb-2 text-[17px] text-ios-label tracking-tight">核心与协议能力</h2>
          <p class="text-sm text-ios-secondary mb-4">{{ node.capabilities?.core_name ? node.capabilities.core_name + ' ' + (node.capabilities.core_version || '') : '尚未上报代理核心，不能据此判断协议可用性。' }}</p>
          <table v-if="node.capabilities?.protocols?.length" class="w-full text-sm">
            <thead>
              <tr class="border-b border-black/[0.06] text-left text-xs font-medium text-ios-tertiary uppercase tracking-wider">
                <th class="py-2.5">协议</th>
                <th>入站</th>
                <th>出站</th>
                <th>TCP</th>
                <th>UDP</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-black/[0.04]">
              <tr v-for="capability in node.capabilities.protocols" :key="capability.protocol" class="text-ios-label">
                <td class="py-2.5 font-medium">{{ capability.protocol }}</td>
                <td>{{ capability.inbound ? '支持' : '—' }}</td>
                <td>{{ capability.outbound ? '支持' : '—' }}</td>
                <td>{{ capability.tcp ? '支持' : '—' }}</td>
                <td>{{ capability.udp ? '支持' : '—' }}</td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>

      <!-- Tab 2: 网络与出站 -->
      <div v-else-if="activeTab === 'network'" class="space-y-6">
        <!-- 出站网络 -->
        <section class="panel-card p-6">
          <h2 class="font-semibold mb-4 text-[17px] text-ios-label tracking-tight">出站网络</h2>
          <dl class="grid grid-cols-1 md:grid-cols-2 gap-5 text-sm">
            <div><dt class="text-xs font-medium text-ios-tertiary">核心出站源 IP</dt><dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.outboundSourceIp || '—' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">核心出站网卡</dt><dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.outboundInterface || '—' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">服务器强制出站 IPv4</dt><dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.hostSourceIpv4 || '—' }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">服务器强制出站 IPv6</dt><dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.hostSourceIpv6 || '—' }}</dd></div>
          </dl>
          <p class="text-xs text-ios-tertiary mt-4 leading-relaxed">核心出站源 IP/网卡只影响本节点代理核心转发的流量；服务器强制出站作用于整台服务器经该网卡新建的出站连接。留空表示未配置，改动在发布配置后生效。</p>
        </section>

        <!-- 账户字节采样来源 -->
        <NodeTrafficSourcesPanel :node="node" :now="clockNow" />
      </div>

      <!-- Tab 3: 运维与健康 -->
      <div v-else-if="activeTab === 'operations'" class="space-y-6">
        <p v-if="node.deletedAt" class="panel-card p-6 text-sm text-ios-secondary">节点已删除，运维与证书操作不可用；历史记录仍保留。</p>
        <!-- 核心健康检查 (统一 36px 按钮) -->
        <NodeHealthPanel v-if="isOwner && !node.deletedAt" :node="node" />

        <!-- 重启当前核心 (统一 36px 按钮) -->
        <NodeRestartPanel v-if="isOwner && !node.deletedAt" :node="node" />

        <!-- 本地断线缓存 -->
        <section class="panel-card p-6 space-y-4">
          <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">本地断线缓存</h2>
          <p class="text-sm" :class="spoolFresh() && node.heartbeat.spool_status?.status === 'capacity_exceeded' ? 'text-ios-red font-medium' : 'text-ios-secondary'">{{ spoolLabel() }}</p>
          <dl v-if="spoolFresh() && node.heartbeat.spool_status?.measured" class="grid sm:grid-cols-2 gap-4 text-sm">
            <div><dt class="text-xs font-medium text-ios-tertiary">实际缓存文件占用 / 总预算</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.disk_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.disk_limit) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">数据库页占用 / 预留上限</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.database_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.database_limit) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">加密记录载荷</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.payload_bytes) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">待补传批次 / 累计淘汰的允许记录</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ node.heartbeat.spool_status.pending_batches ?? 0 }} / {{ node.heartbeat.spool_status.dropped_allowed_rollups ?? 0 }}</dd></div>
          </dl>
          <p class="text-xs text-ios-tertiary leading-relaxed">数据库预算另留事务日志空间，载荷大小与磁盘文件占用不同。容量不足时可能阻止记录写入；不要通过删除队列或身份来解除保护。累计淘汰只针对通过认证的允许访问记录；阻断访问记录与流量记录不会被淘汰。样本超过 45 秒后显示未知。</p>
        </section>

        <!-- 域名记录采集状态 -->
        <NodeCollectorPanel :node="node" :now="clockNow" />
      </div>

      <!-- Tab 4: 证书与登记 -->
      <div v-else-if="activeTab === 'certificate'" class="space-y-6">
        <NodeCertificatePanel v-if="isOwner" :node="node" />
      </div>
    </template>

    <FormDrawer v-model="settingsVisible" title="节点设置" @confirm="saveSettings" :loading="saving">
      <div class="space-y-4 text-sm">
        <label class="block">名称<input v-model="settings.name" maxlength="40" class="form-input-base mt-2" /></label>
        <label class="block">订阅接入 IP<input v-model="settings.publicIp" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label>
        <label class="block"><input v-model="settings.enabled" type="checkbox" /> 启用节点控制</label>
        <label class="block"><input v-model="settings.allowSubscription" type="checkbox" /> 允许用户订阅</label>
        <label class="block"><input v-model="settings.maintenance" type="checkbox" /> 维护中（不进入订阅）</label>
        <div class="border-t pt-4 space-y-3">
          <p class="font-semibold">出站网络</p>
          <p class="text-xs text-ios-tertiary">发布配置后生效；留空表示清除该值。</p>
          <div class="space-y-3">
            <p class="text-xs font-semibold text-ios-secondary">核心出站</p>
            <label class="block">核心出站源 IP<input v-model="settings.outboundSourceIp" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label>
            <label class="block">核心出站网卡<input v-model="settings.outboundInterface" maxlength="15" placeholder="例如 eth0" class="form-input-base mt-2" /></label>
            <p class="text-xs text-ios-tertiary">只影响本节点代理核心转发的流量</p>
          </div>
          <div class="space-y-3">
            <p class="text-xs font-semibold text-ios-secondary">服务器强制出站</p>
            <label class="block">服务器强制出站 IPv4<input v-model="settings.hostSourceIpv4" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label>
            <label class="block">服务器强制出站 IPv6<input v-model="settings.hostSourceIpv6" placeholder="例如 2001:db8::10" class="form-input-base mt-2" /></label>
            <p class="text-xs text-ios-tertiary">整台服务器经该网卡新建的出站连接都改用此源地址（核心出站源 IP 除外）；已有连接重连后生效；仅 Linux；失败自动回滚</p>
          </div>
          <p v-if="mixedFamilyHint" class="text-xs text-ios-orange">双栈服务器请同时填写 IPv4 和 IPv6，未填写的地址族不会被改写</p>
          <p v-if="networkErrorText" role="alert" class="text-sm text-ios-red">{{ networkErrorText }}</p>
        </div>
      </div>
    </FormDrawer>

    <ConfirmDialog v-model="revokeVisible" title="吊销节点证书" message="吊销后，此节点不能再使用当前证书建立控制连接，已建立的连接也会在下一次消息校验时关闭。" @confirm="revoke" />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { nodeAPI, type NodeView } from '../api';
import { isOwner, formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import NodeHealthPanel from '../components/NodeHealthPanel.vue';
import NodeRestartPanel from '../components/NodeRestartPanel.vue';
import NodeCertificatePanel from '../components/NodeCertificatePanel.vue';
import NodeCollectorPanel from '../components/NodeCollectorPanel.vue';
import NodeTrafficSourcesPanel from '../components/NodeTrafficSourcesPanel.vue';
import TrafficValue from '../../../components/TrafficValue.vue';

const route = useRoute();
const router = useRouter();
// The route param is read from the URL every time instead of being remembered:
// the same component instance can move from /nodes/A to /nodes/B.
const routeNodeId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''));
// Raw response body. Nothing here renders or acts on it directly: `node` below
// is null unless that record is the node the URL currently names, so a late
// response for the node we just left can be neither shown, edited nor revoked.
const loadedNode = ref<NodeView | null>(null);
const node = computed<NodeView | null>(() => (loadedNode.value && loadedNode.value.id === routeNodeId.value ? loadedNode.value : null));
const error = ref('');
const revokeVisible = ref(false);

const tabs = computed(() => [
  { key: 'overview', label: '运行概览' },
  { key: 'network', label: '网络与出站' },
  { key: 'operations', label: '运维与健康' },
  ...(isOwner.value && !node.value?.deletedAt ? [{ key: 'certificate', label: '证书与登记' }] : [])
]);

function normalize(tab: unknown) {
  return typeof tab === 'string' && tabs.value.some(t => t.key === tab) ? tab : 'overview';
}

const activeTab = ref<string>(normalize(route.query.tab));

watch(() => route.query.tab, (val) => {
  activeTab.value = normalize(val);
});

function selectTab(key: string) {
  activeTab.value = key;
  void router.replace({ query: { ...route.query, tab: key } });
}

watch(isOwner, () => {
  activeTab.value = normalize(activeTab.value);
});

// A deleted node has no certificate tab; a deep link to it falls back to overview.
watch(() => node.value?.deletedAt, () => {
  activeTab.value = normalize(activeTab.value);
});

const nodeTrafficLabel = computed(() => {
  const heartbeat = node.value?.heartbeat;
  if (heartbeat?.upload_bytes === undefined && heartbeat?.download_bytes === undefined) {
    return '核心未上报节点累计流量：核心未运行、Clash API 不可用，或该核心只按用户统计（Xray 节点）。';
  }
  return '自本次 Agent 启动以来从核心 Clash API 累计观察到的节点总量；核心重启后从零重新累计，不影响按用户统计。sing-box 无法按用户归属，用户额度只统计 Xray 节点。';
});

function resource(value: number | undefined) { return typeof value === 'number' ? value.toFixed(1) + '%' : '—'; }
const clockNow = ref(Date.now());
let clockTimer: ReturnType<typeof setInterval> | undefined;

function connectionCount() {
  const n = node.value;
  const h = n?.heartbeat;
  const observed = (h?.active_connections_observed_at?.seconds ?? 0) * 1000;
  return n?.enabled && n.status === 'online' && h?.core_healthy === true && h.active_connections_measured === true && h.active_connections_scope === 'proxy_user_sessions_v1' && clockNow.value - observed >= -5000 && clockNow.value - observed < 45000 && clockNow.value - Date.parse(n.lastHeartbeat) < 45000 ? (h.active_connections ?? 0) : '尚未测量或已过期';
}

function spoolFresh() {
  const n = node.value;
  const sample = n?.heartbeat.spool_status;
  const observed = (sample?.observed_at?.seconds ?? 0) * 1000;
  return !!(n?.enabled && ['online', 'degraded'].includes(n.status) && sample && clockNow.value - observed >= -5000 && clockNow.value - observed < 45000 && clockNow.value - Date.parse(n.lastHeartbeat) < 45000);
}

const spoolFailureLabels: Record<string, string> = {
  queue_unavailable: '缓存未能打开；保留原队列、密钥和账本，核对节点事件',
  capacity_unavailable: '缓存容量无法读取；核对数据目录访问和磁盘状态，不按零占用处理',
  capacity_invalid: '容量元数据不一致；保留材料并核对节点状态',
  access_head_unavailable: '访问记录队首无法读取；核对存储状态，保留原队列',
  access_head_invalid: '访问记录队首无法认证或内容不一致；保留队列与原密钥，不跳过批次',
  traffic_head_unavailable: '账户流量队首无法读取；保留队列与消费账本，核对存储状态',
  traffic_head_invalid: '账户流量队首无法认证或内容不一致；保留原密钥、队列和消费账本',
  observation_timeout: '本次缓存观察超时，等待后续心跳；当前容量与完整性未知',
  observation_interrupted: '本次缓存观察已中断，等待后续心跳；当前容量与完整性未知'
};

function spoolLabel() {
  if (!spoolFresh()) return '尚未上报或样本已过期';
  const s = node.value?.heartbeat.spool_status;
  if (s?.status === 'unavailable' && s.failure_code) return spoolFailureLabels[s.failure_code] || '缓存状态无法校验';
  const dbUsed = s?.database_bytes ?? 0, dbLimit = s?.database_limit ?? 1, diskUsed = s?.disk_bytes ?? 0, diskLimit = s?.disk_limit ?? 1;
  if (s?.status === 'capacity_exceeded') {
    const db = dbUsed >= dbLimit, disk = diskUsed >= diskLimit;
    const which = db && disk ? '数据库页与缓存文件均已达到预算' : db ? '数据库页预算已满（索引与累计游标也占用该预算）' : '缓存文件（含回滚日志与密钥）已达到物理预算';
    return `已达到缓存容量上限：${which}。先补传已缓存记录并核对容量文件；不要通过删除队列或身份来腾出空间。`;
  }
  if (s?.status === 'near_capacity') {
    const db = dbUsed / dbLimit >= 0.85, disk = diskUsed / diskLimit >= 0.85;
    const which = db && disk ? '数据库页与缓存文件' : db ? '数据库页' : disk ? '缓存文件' : '相应预算';
    return `接近缓存容量（${which}达到85%）；请尽快核对补传进度与磁盘状态，容量满时会阻止新记录写入。`;
  }
  return { ok: '缓存占用正常', unavailable: '缓存容量或队首记录无法校验，保留原材料并检查节点状态' }[s?.status ?? 'unavailable'];
}

function cacheBytes(value: number | undefined) {
  const size = value ?? 0;
  return size < 1024 * 1024 ? `${(size / 1024).toFixed(1)} KiB` : `${(size / 1024 / 1024).toFixed(1)} MiB`;
}

// Each load takes a ticket and the id it asked for. A response is used only if
// it is still the newest request and the URL still names the same node, so a
// slow answer for the previous node cannot overwrite the current one.
let generation = 0;

async function load() {
  const id = routeNodeId.value;
  const current = ++generation;
  // Leaving to /nodes keeps this instance around for one tick; there is no node
  // to fetch and we must not request /nodes/undefined.
  if (!id) {
    loadedNode.value = null;
    error.value = '';
    return;
  }
  try {
    const result = await nodeAPI.get(id);
    if (current !== generation || id !== routeNodeId.value) return;
    loadedNode.value = result;
    error.value = '';
  } catch (e) {
    if (current !== generation || id !== routeNodeId.value) return;
    loadedNode.value = null;
    error.value = (e as Error).message;
  }
}

async function revoke() {
  const target = node.value?.id ?? '';
  if (!target || staleTarget(target)) return;
  try {
    await nodeAPI.revoke(target);
    showToast('节点证书已吊销');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

const settingsVisible = ref(false);
const saving = ref(false);
const networkErrorText = ref('');
const settings = reactive({
  id: '', name: '', publicIp: '', enabled: true, allowSubscription: true, maintenance: false,
  outboundSourceIp: '', outboundInterface: '', hostSourceIpv4: '', hostSourceIpv6: ''
});

// Dialogs hold the id they were opened for, so leaving the node must close them
// instead of letting a pending confirm apply to the node that is now in view.
function closeDialogs() {
  settingsVisible.value = false;
  revokeVisible.value = false;
  saving.value = false;
  networkErrorText.value = '';
  settings.id = '';
}

// Node-targeted actions re-check their target against the URL: a confirm dialog
// opened for A may still be confirmable after the page moved to B.
function staleTarget(id: string) {
  if (id !== '' && id === routeNodeId.value) return false;
  showToast('当前页面已切换到其他节点，操作已取消并重新加载', 'error');
  closeDialogs();
  void load();
  return true;
}

function openSettings() {
  if (!node.value) return;
  Object.assign(settings, {
    id: node.value.id,
    name: node.value.name,
    publicIp: node.value.publicIp,
    enabled: node.value.enabled,
    allowSubscription: node.value.allowSubscription,
    maintenance: node.value.maintenance,
    outboundSourceIp: node.value.outboundSourceIp,
    outboundInterface: node.value.outboundInterface,
    hostSourceIpv4: node.value.hostSourceIpv4,
    hostSourceIpv6: node.value.hostSourceIpv6
  });
  networkErrorText.value = '';
  settingsVisible.value = true;
}

// The four outbound-network fields are checked here the way the panel checks
// them: the family of each address and the Linux interface-name grammar.
// Whether the address or the interface exists is decided on the node when the
// revision is applied, not here.
const interfacePattern = /^[A-Za-z0-9_.:-]{1,15}$/;
function isIPv4(value: string) {
  const parts = value.split('.');
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}
function isIPv6(value: string) {
  return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value) && !value.includes(':::');
}

function networkError() {
  if (settings.outboundSourceIp && !isIPv4(settings.outboundSourceIp) && !isIPv6(settings.outboundSourceIp)) return '核心出站源 IP 需要 IPv4 或 IPv6 地址';
  if (settings.outboundInterface && !interfacePattern.test(settings.outboundInterface)) return '核心出站网卡名只能是 1～15 个字母、数字或 _ . : -';
  if (settings.hostSourceIpv4 && !isIPv4(settings.hostSourceIpv4)) return '服务器强制出站 IPv4 需要 IPv4 地址';
  if (settings.hostSourceIpv6 && !isIPv6(settings.hostSourceIpv6)) return '服务器强制出站 IPv6 需要 IPv6 地址';
  return '';
}

// The panel node record carries no per-family address list, so this is the
// static hint: an unconfigured family is never translated, and a dual-stack
// host that fills only one of the two keeps sending the other one unchanged.
const mixedFamilyHint = computed(() => (settings.hostSourceIpv4 ? 1 : 0) + (settings.hostSourceIpv6 ? 1 : 0) === 1);

async function saveSettings() {
  if (saving.value) return;
  if (staleTarget(settings.id)) return;
  const invalid = networkError();
  if (invalid) {
    networkErrorText.value = invalid;
    showToast(invalid, 'error');
    return;
  }
  networkErrorText.value = '';
  saving.value = true;
  try {
    await nodeAPI.save(settings);
    settingsVisible.value = false;
    showToast('节点设置已保存');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

// A param change means everything loaded so far belongs to another node: drop
// it (and any open dialog) before asking for the new one. The request already in
// flight is discarded by its generation ticket.
watch(() => route.params.id, () => {
  generation++;
  loadedNode.value = null;
  error.value = '';
  closeDialogs();
  void load();
});

onMounted(() => {
  clockTimer = setInterval(() => { clockNow.value = Date.now(); }, 5000);
  load();
  window.addEventListener('proxy-node-status', load);
});

onUnmounted(() => {
  generation++;
  if (clockTimer) clearInterval(clockTimer);
  window.removeEventListener('proxy-node-status', load);
});
</script>
