<template>
  <div class="space-y-6">
    <PageHeader :title="node?.name || '节点详情'" parent="节点" description="此页仅展示真实注册信息、心跳与已上报能力。"><template #actions><router-link to="/nodes" class="ios-btn-secondary">返回列表</router-link><button @click="load" class="ios-btn-secondary">刷新</button><button v-if="isOwner && node" @click="openSettings" class="ios-btn-secondary">节点设置</button><button v-if="isOwner && node" @click="revokeVisible = true" class="ios-action-btn-danger">吊销证书</button></template></PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!node" />
    <template v-else>
      <NodeHealthPanel v-if="isOwner" :node="node" />
<NodeRestartPanel v-if="isOwner" :node="node" />
<NodeCertificatePanel v-if="isOwner" :node="node" />
      <section v-if="node.heartbeat?.core_healthy === false" class="panel-card p-5 border border-amber-300 bg-amber-50 text-sm text-amber-900" role="status">
        <p class="font-semibold">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '代理已停止，流量消费需要核对' : '代理核心未通过健康检查' }}</p>
        <p class="mt-2">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '节点保持管理连接，但不会根据旧计数重新放行。已采集流量会继续补传；无法确认的消费仍需处理。' : '节点仍可连接管理端。代理恢复前，请检查核心运行状态和最近配置发布结果。' }}</p>
        <p class="mt-2 text-xs">以上为最后一次心跳上报的状态。</p>
      </section>
      <section class="panel-card p-6"><div class="flex gap-3 items-center mb-4"><StatusBadge kind="node" :status="node.status" /><span class="text-sm">{{ node.enabled ? '已启用' : '已禁用' }}</span></div>
        <dl class="grid grid-cols-1 md:grid-cols-2 gap-5 text-sm"><div><dt class="text-gray-500">节点 ID</dt><dd class="font-mono break-all mt-1">{{ node.id }}</dd></div><div><dt class="text-gray-500">主机</dt><dd class="mt-1">{{ node.hostname || '等待注册' }}</dd></div><div><dt class="text-gray-500">操作系统</dt><dd class="mt-1">{{ node.os || '—' }}</dd></div><div><dt class="text-gray-500">公网 IP</dt><dd class="mt-1">{{ node.publicIp || '尚未上报' }}</dd></div><div><dt class="text-gray-500">Agent 版本</dt><dd class="mt-1">{{ node.agentVersion || '—' }}</dd></div><div><dt class="text-gray-500">最后心跳</dt><dd class="mt-1">{{ formatTime(node.lastHeartbeat) }}</dd></div></dl>
      </section>
      <section class="panel-card p-6"><h2 class="font-semibold mb-4">出站网络</h2><dl class="grid grid-cols-1 md:grid-cols-2 gap-5 text-sm"><div><dt class="text-gray-500">核心出站源 IP</dt><dd class="mt-1 font-mono break-all">{{ node.outboundSourceIp || '—' }}</dd></div><div><dt class="text-gray-500">核心出站网卡</dt><dd class="mt-1 font-mono break-all">{{ node.outboundInterface || '—' }}</dd></div><div><dt class="text-gray-500">服务器强制出站 IPv4</dt><dd class="mt-1 font-mono break-all">{{ node.hostSourceIpv4 || '—' }}</dd></div><div><dt class="text-gray-500">服务器强制出站 IPv6</dt><dd class="mt-1 font-mono break-all">{{ node.hostSourceIpv6 || '—' }}</dd></div></dl><p class="text-xs text-gray-500 mt-4">核心出站源 IP/网卡只影响本节点代理核心转发的流量；服务器强制出站作用于整台服务器经该网卡新建的出站连接。留空表示未配置，改动在发布配置后生效。</p></section>

            <section class="panel-card p-6"><h2 class="font-semibold mb-4">实时资源</h2><dl class="grid grid-cols-3 gap-4 text-sm"><div><dt class="text-gray-500">CPU</dt><dd class="mt-2 text-lg font-semibold">{{ resource(node.heartbeat?.cpu_percent) }}</dd></div><div><dt class="text-gray-500">内存</dt><dd class="mt-2 text-lg font-semibold">{{ resource(node.heartbeat?.memory_percent) }}</dd></div><div><dt class="text-gray-500">磁盘</dt><dd class="mt-2 text-lg font-semibold">{{ resource(node.heartbeat?.disk_percent) }}</dd></div></dl><p class="text-xs text-gray-500 mt-4">CPU 需要一个采样窗口；尚未测量的项目显示为 —。</p></section>
            <section class="panel-card p-6"><h2 class="font-semibold mb-4">节点累计流量（核心上报）</h2><dl class="grid grid-cols-2 gap-4 text-sm"><div><dt class="text-gray-500">上传</dt><dd class="mt-2 text-lg"><TrafficValue v-if="node.heartbeat?.upload_bytes !== undefined" :bytes="node.heartbeat.upload_bytes" /><span v-else>—</span></dd></div><div><dt class="text-gray-500">下载</dt><dd class="mt-2 text-lg"><TrafficValue v-if="node.heartbeat?.download_bytes !== undefined" :bytes="node.heartbeat.download_bytes" /><span v-else>—</span></dd></div></dl><p class="text-xs text-gray-500 mt-4">{{ nodeTrafficLabel }}</p></section>
<section class="panel-card p-6"><h2 class="font-semibold mb-4">当前用户连接</h2><p class="text-xl font-semibold">{{ connectionCount() }}</p><p class="text-xs text-gray-500 mt-3">Xray 节点上报该用户的去重客户端 IP 数（回环地址排除），sing-box 节点不提供每用户在线数、显示未知；样本超过 45 秒、节点离线或核心异常时同样显示未知。</p></section>
<section class="panel-card p-6 space-y-4"><h2 class="font-semibold">本地断线缓存</h2><p class="text-sm" :class="spoolFresh() && node.heartbeat.spool_status?.status === 'capacity_exceeded' ? 'text-red-600' : 'text-gray-500'">{{ spoolLabel() }}</p><dl v-if="spoolFresh() && node.heartbeat.spool_status?.measured" class="grid sm:grid-cols-2 gap-4 text-sm"><div><dt class="text-gray-500">实际缓存文件占用 / 总预算</dt><dd class="mt-1">{{ cacheBytes(node.heartbeat.spool_status.disk_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.disk_limit) }}</dd></div><div><dt class="text-gray-500">数据库页占用 / 预留上限</dt><dd class="mt-1">{{ cacheBytes(node.heartbeat.spool_status.database_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.database_limit) }}</dd></div><div><dt class="text-gray-500">加密记录载荷</dt><dd class="mt-1">{{ cacheBytes(node.heartbeat.spool_status.payload_bytes) }}</dd></div><div><dt class="text-gray-500">待补传批次 / 累计淘汰的允许记录</dt><dd class="mt-1">{{ node.heartbeat.spool_status.pending_batches ?? 0 }} / {{ node.heartbeat.spool_status.dropped_allowed_rollups ?? 0 }}</dd></div></dl><p class="text-xs text-gray-500">数据库预算另留事务日志空间，载荷大小与磁盘文件占用不同。容量不足时可能阻止记录写入；不要通过删除队列或身份来解除保护。累计淘汰只针对通过认证的允许访问记录；阻断访问记录与流量记录不会被淘汰。样本超过 45 秒后显示未知。</p></section>
<NodeCollectorPanel :node="node" :now="clockNow" />
<NodeTrafficSourcesPanel :node="node" :now="clockNow" />
<section class="panel-card p-6"><h2 class="font-semibold mb-4">核心与协议能力</h2><p class="text-sm text-gray-500 mb-4">{{ node.capabilities?.core_name ? node.capabilities.core_name + ' ' + (node.capabilities.core_version || '') : '尚未上报代理核心，不能据此判断协议可用性。' }}</p>
        <table v-if="node.capabilities?.protocols?.length" class="w-full text-sm"><thead><tr><th>协议</th><th>入站</th><th>出站</th><th>TCP</th><th>UDP</th></tr></thead><tbody><tr v-for="capability in node.capabilities.protocols" :key="capability.protocol"><td>{{ capability.protocol }}</td><td>{{ capability.inbound ? '支持' : '—' }}</td><td>{{ capability.outbound ? '支持' : '—' }}</td><td>{{ capability.tcp ? '支持' : '—' }}</td><td>{{ capability.udp ? '支持' : '—' }}</td></tr></tbody></table>
      </section>
    </template>
    <FormDrawer v-model="settingsVisible" title="节点设置" @confirm="saveSettings" :loading="saving"><div class="space-y-4 text-sm"><label class="block">名称<input v-model="settings.name" maxlength="40" class="form-input-base mt-2" /></label><label class="block">订阅接入 IP<input v-model="settings.publicIp" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label><label class="block"><input v-model="settings.enabled" type="checkbox" /> 启用节点控制</label><label class="block"><input v-model="settings.allowSubscription" type="checkbox" /> 允许用户订阅</label><label class="block"><input v-model="settings.maintenance" type="checkbox" /> 维护中（不进入订阅）</label><div class="border-t pt-4 space-y-3"><p class="font-semibold">出站网络</p><p class="text-xs text-gray-500">发布配置后生效；留空表示清除该值。</p><div class="space-y-3"><p class="text-xs font-semibold text-gray-700">核心出站</p><label class="block">核心出站源 IP<input v-model="settings.outboundSourceIp" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label><label class="block">核心出站网卡<input v-model="settings.outboundInterface" maxlength="15" placeholder="例如 eth0" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">只影响本节点代理核心转发的流量</p></div><div class="space-y-3"><p class="text-xs font-semibold text-gray-700">服务器强制出站</p><label class="block">服务器强制出站 IPv4<input v-model="settings.hostSourceIpv4" placeholder="例如 203.0.113.10" class="form-input-base mt-2" /></label><label class="block">服务器强制出站 IPv6<input v-model="settings.hostSourceIpv6" placeholder="例如 2001:db8::10" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">整台服务器经该网卡新建的出站连接都改用此源地址（核心出站源 IP 除外）；已有连接重连后生效；仅 Linux；失败自动回滚</p></div><p v-if="mixedFamilyHint" class="text-xs text-amber-700">双栈服务器请同时填写 IPv4 和 IPv6，未填写的地址族不会被改写</p><p v-if="networkErrorText" role="alert" class="text-sm text-red-600">{{ networkErrorText }}</p></div></div></FormDrawer>
    <ConfirmDialog v-model="revokeVisible" title="吊销节点证书" message="吊销后，此节点不能再使用当前证书建立控制连接，已建立的连接也会在下一次消息校验时关闭。" @confirm="revoke" />
  </div>
</template>
<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute } from 'vue-router';
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
const nodeTrafficLabel = computed(() => {
  const heartbeat = node.value?.heartbeat;
  if (heartbeat?.upload_bytes === undefined && heartbeat?.download_bytes === undefined) {
    return '核心未上报节点累计流量：核心未运行、Clash API 不可用，或该核心只按用户统计（Xray 节点）。';
  }
  return '自本次 Agent 启动以来从核心 Clash API 累计观察到的节点总量；核心重启后从零重新累计，不影响按用户统计。sing-box 无法按用户归属，用户额度只统计 Xray 节点。';
});
function resource(value: number | undefined) { return typeof value === 'number' ? value.toFixed(1) + '%' : '—'; }
const clockNow = ref(Date.now()); let clockTimer:ReturnType<typeof setInterval>|undefined;
function connectionCount(){ const n=node.value; const h=n?.heartbeat; const observed=(h?.active_connections_observed_at?.seconds ?? 0)*1000; return n?.enabled && n.status==="online" && h?.core_healthy===true && h.active_connections_measured===true && h.active_connections_scope==="proxy_user_sessions_v1" && clockNow.value-observed>=-5000 && clockNow.value-observed<45000 && clockNow.value-Date.parse(n.lastHeartbeat)<45000 ? (h.active_connections ?? 0) : "尚未测量或已过期"; }
function spoolFresh(){const n=node.value;const sample=n?.heartbeat.spool_status;const observed=(sample?.observed_at?.seconds ?? 0)*1000;return !!(n?.enabled && ['online','degraded'].includes(n.status) && sample && clockNow.value-observed>=-5000 && clockNow.value-observed<45000 && clockNow.value-Date.parse(n.lastHeartbeat)<45000);}
const spoolFailureLabels:Record<string,string>={queue_unavailable:'缓存未能打开；保留原队列、密钥和账本，核对节点事件',capacity_unavailable:'缓存容量无法读取；核对数据目录访问和磁盘状态，不按零占用处理',capacity_invalid:'容量元数据不一致；保留材料并核对节点状态',access_head_unavailable:'访问记录队首无法读取；核对存储状态，保留原队列',access_head_invalid:'访问记录队首无法认证或内容不一致；保留队列与原密钥，不跳过批次',traffic_head_unavailable:'账户流量队首无法读取；保留队列与消费账本，核对存储状态',traffic_head_invalid:'账户流量队首无法认证或内容不一致；保留原密钥、队列和消费账本',observation_timeout:'本次缓存观察超时，等待后续心跳；当前容量与完整性未知',observation_interrupted:'本次缓存观察已中断，等待后续心跳；当前容量与完整性未知'};
function spoolLabel(){if(!spoolFresh())return '尚未上报或样本已过期';const s=node.value?.heartbeat.spool_status;if(s?.status==='unavailable' && s.failure_code)return spoolFailureLabels[s.failure_code] || '缓存状态无法核验';const dbUsed=s?.database_bytes??0,dbLimit=s?.database_limit??1,diskUsed=s?.disk_bytes??0,diskLimit=s?.disk_limit??1;if(s?.status==='capacity_exceeded'){const db=dbUsed>=dbLimit,disk=diskUsed>=diskLimit;const which=db&&disk?'数据库页与缓存文件均已达到预算':db?'数据库页预算已满（索引与累计游标也占用该预算）':'缓存文件（含回滚日志与密钥）已达到物理预算';return `已达到缓存容量上限：${which}。先补传已缓存记录并核对容量文件；不要删除队列或身份来腾出空间。`;}if(s?.status==='near_capacity'){const db=dbUsed/dbLimit>=0.85,disk=diskUsed/diskLimit>=0.85;const which=db&&disk?'数据库页与缓存文件':db?'数据库页':disk?'缓存文件':'相应预算';return `接近缓存容量（${which}达到85%）；请尽快核对补传进度与磁盘状态，容量满时会阻止新记录写入。`;}return {ok:'缓存占用正常',unavailable:'缓存容量或队首记录无法核验，保留原材料并检查节点状态'}[s?.status ?? 'unavailable'];}
function cacheBytes(value:number|undefined){const size=value ?? 0;return size<1024*1024?`${(size/1024).toFixed(1)} KiB`:`${(size/1024/1024).toFixed(1)} MiB`;}
const route = useRoute(); const node = ref<NodeView | null>(null); const error = ref(''); const revokeVisible = ref(false);
async function load() { try { node.value = await nodeAPI.get(String(route.params.id)); error.value = ''; } catch (e) { error.value = (e as Error).message; } }
async function revoke() { if (!node.value) return; try { await nodeAPI.revoke(node.value.id); showToast('节点证书已吊销'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } }
const settingsVisible = ref(false); const saving = ref(false); const networkErrorText = ref('');
const settings = reactive({ id: '', name: '', publicIp: '', enabled: true, allowSubscription: true, maintenance: false, outboundSourceIp: '', outboundInterface: '', hostSourceIpv4: '', hostSourceIpv6: '' });
function openSettings() { if (!node.value) return; Object.assign(settings, { id: node.value.id, name: node.value.name, publicIp: node.value.publicIp, enabled: node.value.enabled, allowSubscription: node.value.allowSubscription, maintenance: node.value.maintenance, outboundSourceIp: node.value.outboundSourceIp, outboundInterface: node.value.outboundInterface, hostSourceIpv4: node.value.hostSourceIpv4, hostSourceIpv6: node.value.hostSourceIpv6 }); networkErrorText.value = ''; settingsVisible.value = true; }
// The four outbound-network fields are checked here the way the panel checks
// them: the family of each address and the Linux interface-name grammar.
// Whether the address or the interface exists is decided on the node when the
// revision is applied, not here.
const interfacePattern = /^[A-Za-z0-9_.:-]{1,15}$/;
function isIPv4(value: string) { const parts = value.split('.'); return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255); }
function isIPv6(value: string) { return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value) && !value.includes(':::'); }
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
  const invalid = networkError();
  if (invalid) { networkErrorText.value = invalid; showToast(invalid, 'error'); return; }
  networkErrorText.value = '';
  saving.value = true;
  try { await nodeAPI.save(settings); settingsVisible.value = false; showToast('节点设置已保存'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; }
}
watch(() => route.params.id, load);
onMounted(() => { clockTimer=setInterval(()=>{clockNow.value=Date.now();},5000); load(); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => {if(clockTimer)clearInterval(clockTimer);window.removeEventListener('proxy-node-status', load);});
</script>




