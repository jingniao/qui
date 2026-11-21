<template>
  <div class="space-y-6">
    <section class="panel-card p-6 space-y-4">
      <div class="flex justify-between gap-3">
        <h2 class="text-ios-label text-base font-semibold leading-normal">节点（入站）</h2>
        <button v-if="isOwner && !node.deletedAt && (nodeHasUserCore(node) || nodeCores(node).some(core => core.name === 'gost'))" class="ios-btn-primary" @click="$emit('edit', null)">+ 新建节点</button>
      </div>
      <p v-if="unpublished && isOwner" class="text-sm text-ios-orange">有未发布的更改 · <button class="text-ios-blue" @click="$emit('publish')">发布配置</button></p>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <p v-if="!isOwner" class="text-sm text-ios-secondary">当前权限无法读取入站与出站配置，请联系 Owner。</p>
      <template v-else>
        <p v-if="loading && !inboundReady" class="text-sm text-ios-secondary">正在读取节点…</p>
        <DataTable v-else-if="inbounds.length">
          <template #header>
            <th>节点 / 协议</th>
            <th>端口</th>
            <th>用户范围</th>
            <th>订阅可见</th>
            <th>状态</th><th v-if="isOwner">操作</th>
          </template>
          <template #body>
            <tr v-for="item in inbounds" :key="item.id">
              <td>
                {{ item.name }}
                <ProtocolBadge :protocol="item.protocol" />
                <ProtocolBadge v-if="inboundCoreName(item)" :protocol="inboundCoreName(item)" />
              </td>
              <td class="font-mono tabular-nums">{{ item.listen }}:{{ item.port }} · {{ listenNetworks(item).join('/').toUpperCase() }}</td>
              <td>{{ item.internalRelay ? '内部中转' : item.allowedUserIds.length ? `${item.allowedUserIds.length} 位指定用户` : '全部授权用户' }}</td>
              <td>{{ item.subscriptionVisible ? '是' : '否' }}</td>
              <td><StatusBadge :status="item.enabled ? 'active' : 'disabled'" /></td><td v-if="isOwner"><div class="flex gap-2"><button class="ios-action-btn-secondary" @click="$emit('edit', item)">编辑</button><button v-if="!node.deletedAt" class="ios-action-btn-secondary" :disabled="busy" @click="toggle(item)">{{ item.enabled ? '停用' : '启用' }}</button></div></td>
            </tr>
          </template>
        </DataTable>
        <EmptyState v-else-if="inboundReady" title="此服务器尚未创建节点" description="创建节点以配置用户接入协议与端口"><template #action><button v-if="isOwner && !node.deletedAt && (nodeHasUserCore(node) || nodeCores(node).some(core => core.name === 'gost'))" class="ios-btn-primary" @click="$emit('edit', null)">+ 新建节点</button></template></EmptyState>
      </template>
    </section>
    <details v-if="isOwner" class="panel-card p-6 space-y-4">
      <summary class="cursor-pointer font-semibold text-ios-label">出站（{{ outboundReady ? outbounds.length : '—' }}，只读）</summary>
      <p v-if="!outboundReady" class="text-sm text-ios-secondary">尚未取得出站配置。</p>
      <p v-else-if="!outbounds.length" class="text-sm text-ios-secondary">此服务器尚无出站配置。</p>
      <DataTable v-else>
        <template #header>
          <th>名称</th>
          <th>协议</th>
          <th>目标</th>
          <th>状态</th>
        </template>
        <template #body>
          <tr v-for="item in outbounds" :key="item.id">
            <td>{{ item.name }}</td>
            <td><ProtocolBadge :protocol="item.protocol" compact /></td>
            <td class="font-mono tabular-nums">{{ item.address || '—' }}{{ item.port ? `:${item.port}` : '' }}</td>
            <td>{{ item.enabled ? '已启用' : '已停用' }}</td>
          </tr>
        </template>
      </DataTable>
      <router-link to="/proxy/nodes" class="text-sm text-ios-blue">前往节点列表</router-link>
    </details>
    <details v-if="isOwner" class="panel-card p-6 space-y-4">
      <summary class="cursor-pointer font-semibold text-ios-label">出口组（{{ exitsReady ? exits.length : '—' }}，只读）</summary>
      <p v-if="!exitsReady" class="text-sm text-ios-secondary">尚未取得出口组。</p>
      <p v-else-if="!exits.length" class="text-sm text-ios-secondary">此服务器尚无出口组。</p>
      <ul v-else class="space-y-3 text-sm text-ios-secondary">
        <li v-for="exit in exits" :key="exit.id">{{ exit.name }} · {{ strategyNames[exit.strategy] || '—' }} · {{ exit.members.length }} 个出站 · {{ exit.enabled ? '已启用' : '已停用' }}</li>
      </ul>
    </details>
  </div>
</template>
<script setup lang="ts">
import { nodeHasUserCore, nodeCores } from '../../coreSummary';
import { ref, watch, onUnmounted, type Ref } from 'vue';
import type { NodeView } from '../../api';
import { proxyAPI, choices, strategyNames, type Inbound, type Outbound, type OutboundGroup } from '../../../proxy/api';
import { invalidatesRead } from '../../../../app/apiClient';
import { isOwner } from '../../../../app/auth';
import { listenNetworks } from '../../../proxy/components/node-editor/nodeEditorModel';
import { coreLabel, isCoreName } from '../../coreSummary';
import EmptyState from '../../../../components/EmptyState.vue';
import StatusBadge from '../../../../components/StatusBadge.vue';
import ProtocolBadge from '../../../../components/ProtocolBadge.vue';
import DataTable from '../../../../components/DataTable.vue';
const props = defineProps<{ node: NodeView; refresh: number; unpublished?: boolean }>();
const emit = defineEmits<{ edit: [item: Inbound | null]; publish: []; saved: [] }>();
const busy = ref(false);
function inboundCoreName(item: Inbound): string {
  const core = item.core || props.node.capabilities?.core_name || props.node.capabilities?.cores?.[0]?.name;
  // An unreported core stays blank; a stated one uses the shared label.
  return isCoreName(core) ? coreLabel(core) : '';
}
async function toggle(item: Inbound) {
  if (busy.value || !isOwner.value || props.node.deletedAt) return;
  busy.value = true; error.value = '';
  try {
    const latest = (await choices((p, s) => proxyAPI.inbounds(props.node.id, p, s))).find(i => i.id === item.id);
    if (!latest) throw new Error('节点已删除，请重新读取');
    // The API rejects unknown fields, so send only the input fields of the record.
    const { name, nodeId, protocol, core, listen, port, subscriptionVisible, internalRelay, egressSourceIp, defaultOutbound, allowedUserIds, settings } = latest;
    await proxyAPI.saveInbound(latest.id, { name, nodeId, protocol, core, listen, port, enabled: !latest.enabled, subscriptionVisible, internalRelay, egressSourceIp, defaultOutbound, allowedUserIds, settings });
    emit('saved');
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
const inbounds = ref<Inbound[]>([]), outbounds = ref<Outbound[]>([]), exits = ref<OutboundGroup[]>([]), error = ref(''), loading = ref(false);
let generation = 0;
const inboundReady = ref(false), outboundReady = ref(false), exitsReady = ref(false);
async function readSection<T>(pending: Promise<T[]>, rows: Ref<T[]>, ready: Ref<boolean>, current: number) {
  try {
    const items = await pending;
    if (current !== generation) return;
    rows.value = items; ready.value = true;
  } catch (e) {
    if (current === generation && invalidatesRead(e)) { rows.value = []; ready.value = false; }
    throw e;
  }
}
watch([() => props.node.id, isOwner, () => props.refresh], async ([id, owner], [previousId, previousOwner]) => {
  const current = ++generation;
  if (id !== previousId || owner !== previousOwner) {
    inbounds.value = []; outbounds.value = []; exits.value = []; inboundReady.value = false; outboundReady.value = false; exitsReady.value = false;
  }
  error.value = '';
  if (!owner) return;
  loading.value = true;
  const results = await Promise.allSettled([
    readSection(choices((page, size) => proxyAPI.inbounds(id, page, size)), inbounds, inboundReady, current),
    readSection(choices((page, size) => proxyAPI.outbounds(id, page, size)), outbounds, outboundReady, current),
    readSection(choices((page, size) => proxyAPI.groups(id, page, size)), exits, exitsReady, current)
  ]);
  if (current !== generation) return;
  error.value = results.filter(result => result.status === 'rejected').map(result => (result as PromiseRejectedResult).reason.message).join('；');
  loading.value = false;
}, { immediate: true });
onUnmounted(() => { generation++; });
</script>
