<template>
  <div class="space-y-6">
    <section class="panel-card p-6 space-y-4">
      <div class="flex justify-between gap-3">
        <h2 class="font-semibold text-ios-label">节点（入站）</h2>
        <router-link v-if="isOwner && !node.deletedAt" :to="{ path: '/proxy/nodes', query: { create: '1', server: node.id } }" class="ios-btn-primary">+ 新建节点</router-link>
      </div>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <p v-if="!isOwner" class="text-sm text-ios-secondary">当前权限无法读取入站与出站配置，请联系 Owner。</p>
      <template v-else>
        <p v-if="loading" class="text-sm text-ios-secondary">正在读取节点…</p>
        <DataTable v-else-if="inbounds.length">
          <template #header>
            <th>节点 / 协议</th>
            <th>端口</th>
            <th>用户范围</th>
            <th>订阅可见</th>
            <th>启用</th>
          </template>
          <template #body>
            <tr v-for="item in inbounds" :key="item.id">
              <td>
                {{ item.name }}
                <p class="text-xs text-ios-secondary uppercase">{{ item.protocol }}</p>
              </td>
              <td class="font-mono tabular-nums">{{ item.listen }}:{{ item.port }}</td>
              <td>{{ item.internalRelay ? '内部中转' : item.allowedUserIds.length ? `${item.allowedUserIds.length} 位指定用户` : '全部授权用户' }}</td>
              <td>{{ item.subscriptionVisible ? '是' : '否' }}</td>
              <td>{{ item.enabled ? '已启用' : '已停用' }}</td>
            </tr>
          </template>
        </DataTable>
        <p v-else-if="!error" class="text-sm text-ios-secondary">此服务器尚未创建节点。</p>
      </template>
    </section>
    <section v-if="isOwner && outbounds.length" class="panel-card p-6 space-y-4">
      <h2 class="font-semibold text-ios-label">出站与出口（只读）</h2>
      <DataTable>
        <template #header>
          <th>名称</th>
          <th>协议</th>
          <th>目标</th>
          <th>状态</th>
        </template>
        <template #body>
          <tr v-for="item in outbounds" :key="item.id">
            <td>{{ item.name }}</td>
            <td>{{ item.protocol }}</td>
            <td class="font-mono tabular-nums">{{ item.address || '—' }}{{ item.port ? `:${item.port}` : '' }}</td>
            <td>{{ item.enabled ? '已启用' : '已停用' }}</td>
          </tr>
        </template>
      </DataTable>
      <router-link to="/proxy/nodes" class="text-sm text-ios-blue">前往节点列表</router-link>
    </section>
    <p v-else-if="isOwner && !loading && !error" class="text-xs text-ios-secondary">此服务器尚无出站配置。</p>
    <section v-if="isOwner && exits.length" class="panel-card p-6 space-y-4">
      <h2 class="font-semibold text-ios-label">出口组（只读）</h2>
      <ul class="space-y-3 text-sm text-ios-secondary">
        <li v-for="exit in exits" :key="exit.id">{{ exit.name }} · {{ strategyNames[exit.strategy] || '—' }} · {{ exit.members.length }} 个出站 · {{ exit.enabled ? '已启用' : '已停用' }}</li>
      </ul>
    </section>
  </div>
</template>
<script setup lang="ts">
import { ref, watch, onUnmounted } from 'vue';
import type { NodeView } from '../../api';
import { proxyAPI, choices, strategyNames, type Inbound, type Outbound, type OutboundGroup } from '../../../proxy/api';
import { isOwner } from '../../../../app/auth';
import DataTable from '../../../../components/DataTable.vue';
const props = defineProps<{ node: NodeView }>();
const inbounds = ref<Inbound[]>([]), outbounds = ref<Outbound[]>([]), exits = ref<OutboundGroup[]>([]), error = ref(''), loading = ref(false);
let generation = 0;
watch(() => props.node.id, async id => {
  const current = ++generation;
  inbounds.value = []; outbounds.value = []; exits.value = []; error.value = '';
  if (!isOwner.value) return;
  loading.value = true;
  const results = await Promise.allSettled([choices((page, size) => proxyAPI.inbounds(id, page, size)), choices((page, size) => proxyAPI.outbounds(id, page, size)), choices((page, size) => proxyAPI.groups(id, page, size))]);
  if (current !== generation) return;
  if (results[0].status === 'fulfilled') inbounds.value = results[0].value;
  if (results[1].status === 'fulfilled') outbounds.value = results[1].value;
  if (results[2].status === 'fulfilled') exits.value = results[2].value;
  error.value = results.filter(result => result.status === 'rejected').map(result => (result as PromiseRejectedResult).reason.message).join('；');
  loading.value = false;
}, { immediate: true });
onUnmounted(() => { generation++; });
</script>
