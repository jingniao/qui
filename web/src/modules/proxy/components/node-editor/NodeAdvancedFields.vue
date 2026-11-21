<template>
  <details :open="open" class="rounded-2xl border border-ios-border p-4 text-sm text-ios-secondary" @toggle="emit('toggle', ($event.target as HTMLDetailsElement).open)">
    <summary class="cursor-pointer font-medium">高级选项</summary>
    <p v-if="error" role="alert" class="mt-4 text-ios-red">{{ error }} <button type="button" class="ios-btn-secondary" @click="emit('retry')">重试</button></p>
    <p v-else-if="!ready" role="status" class="mt-4">正在读取出站与授权用户…</p>
    <div v-else class="space-y-4 mt-4">
      <label class="block">监听地址<input v-model="form.listen" placeholder="例如 0.0.0.0 或 127.0.0.1" class="form-input-base mt-2 font-mono" />
        <span class="block text-xs mt-1">入站绑定的本地地址；一般保留 0.0.0.0（全部网卡），只想让本机或中转进程连接时填 127.0.0.1。</span>
      </label>
      <label v-if="egressSupported" class="block">出口 IP<select v-model="form.egressSourceIp" class="form-input-base mt-2 font-mono">
        <option value="">跟随服务器设置</option><option v-for="ip in egressAddresses" :key="ip" :value="ip">{{ ip }}</option>
      </select><span class="block text-xs mt-1">此节点全部流量从所选 IP 发出；域名只解析该地址族，没有该地址族的目标连接失败；Block 规则仍阻断。发布后生效。</span></label>
      <div v-else-if="form.egressSourceIp" class="space-y-2 text-xs">
        <p>出口 IP：<span class="font-mono">{{ form.egressSourceIp }}</span></p>
        <p v-if="(props.form.core || primaryCoreName(props.server)) === 'gost'">GOST 核心不支持指定出口 IP，请清除后保存。</p>
        <p v-else>当前核心未上报出口 IP 能力，请升级 Agent 后编辑，或清除后保存。</p>
        <button type="button" class="ios-btn-secondary text-xs" @click="form.egressSourceIp = ''">清除出口 IP</button>
      </div>
      <label class="block">默认出站<select v-model="form.defaultOutbound" class="form-input-base mt-2">
        <option value="direct">Direct</option><option value="block">Block</option>
        <option v-for="item in outbounds.filter(o => o.nodeId === form.nodeId && o.enabled)" :key="item.id" :value="item.id">{{ item.name }}</option>
        <option v-for="item in groups.filter(o => o.nodeId === form.nodeId && o.enabled)" :key="item.id" :value="'group:' + item.id">出口组 · {{ item.name }}</option>
        <option v-for="item in chains.filter(o => o.nodeIds[0] === form.nodeId && o.enabled)" :key="item.id" :value="'chain:' + item.id">代理链 · {{ item.name }}</option>
      </select>
        <span class="block text-xs mt-1">没有命中任何分流规则时流量走这里；一般保持 Direct，需要固定出口时选择相应出站、出口组或代理链。</span>
      </label>
      <label v-if="form.protocol !== 'forward'" class="block"><input v-model="form.internalRelay" :disabled="editing" @change="form.internalRelay && (form.subscriptionVisible = false)" type="checkbox" /> 内部中转入口</label>
      <p v-if="form.protocol !== 'forward'" class="text-xs">勾选后此入站只供另一台节点中转进来使用，不进入用户订阅、不发放用户凭据；编辑已有入站时不能修改。</p>
      <p v-if="internalRelaySettingsReason(form.internalRelay, form.settings)" role="alert" class="text-xs text-ios-orange">{{ internalRelaySettingsReason(form.internalRelay, form.settings) }}</p>
      <div v-if="!form.internalRelay && form.protocol !== 'forward'" class="space-y-2">
        <p class="text-xs">允许用户（不勾选表示节点分组内全部授权用户）</p>
        <label v-for="user in users" :key="user.id" class="flex items-center gap-2"><input v-model="form.allowedUserIds" :value="user.id" type="checkbox" /> {{ user.displayName || user.username }}</label>
        <p v-if="!users.length" class="text-xs">暂无用户</p>
        <p class="text-xs">勾选后只缩小该入站的可用范围，不会额外授予接入权限；例如只勾选「alice」表示同组其他用户拿不到此入站的凭据。</p>
      </div>
    </div>
  </details>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../../../nodes/api';
import { primaryCoreName } from '../../../nodes/coreSummary';
import type { InboundInput, Outbound, OutboundGroup, ProxyChain } from '../../api';
import type { UserChoice } from '../../../users/api';
import { internalRelaySettingsReason } from './nodeEditorModel';
const props = defineProps<{ server?: NodeView; form: InboundInput; editing: boolean; outbounds: Outbound[]; groups: OutboundGroup[]; chains: ProxyChain[]; users: UserChoice[]; open?: boolean; ready: boolean; error: string }>();
const emit = defineEmits<{ toggle: [open: boolean]; retry: [] }>();
const egressSupported = computed(() => {
  const caps = props.server?.capabilities;
  const core = props.form.core || primaryCoreName(props.server);
  if (core !== 'xray' && core !== 'sing-box') return false;
  if (caps?.multi_core_supported) return caps.cores?.find(c => c.name === core)?.inbound_egress_source_supported === true;
  return caps?.core_name === core && caps.inbound_egress_source_supported === true;
});
const egressAddresses = computed(() => Array.from(new Set([
  ...(props.server?.publicAddresses || []).map(a => a.address),
  ...(props.form.egressSourceIp ? [props.form.egressSourceIp] : [])
])));
</script>
