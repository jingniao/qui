<template>
  <details class="rounded-2xl border border-ios-border p-4 text-sm text-ios-secondary">
    <summary class="cursor-pointer font-medium">高级选项</summary>
    <div class="space-y-4 mt-4">
      <label class="block">监听地址<input v-model="form.listen" class="form-input-base mt-2 font-mono" /></label>
      <label class="block">默认出站<select v-model="form.defaultOutbound" class="form-input-base mt-2">
        <option value="direct">Direct</option><option value="block">Block</option>
        <option v-for="item in outbounds.filter(o => o.nodeId === form.nodeId && o.enabled)" :key="item.id" :value="item.id">{{ item.name }}</option>
        <option v-for="item in groups.filter(o => o.nodeId === form.nodeId && o.enabled)" :key="item.id" :value="'group:' + item.id">出口组 · {{ item.name }}</option>
        <option v-for="item in chains.filter(o => o.nodeIds[0] === form.nodeId && o.enabled)" :key="item.id" :value="'chain:' + item.id">代理链 · {{ item.name }}</option>
      </select></label>
      <label class="block"><input v-model="form.internalRelay" :disabled="editing || form.protocol === 'forward'" @change="form.internalRelay && (form.subscriptionVisible = false)" type="checkbox" /> 内部中转入口</label>
      <label v-if="!form.internalRelay && form.protocol !== 'forward'" class="block"><input v-model="form.subscriptionVisible" type="checkbox" /> 显示在授权用户订阅中</label>
      <div v-if="!form.internalRelay && form.protocol !== 'forward'" class="space-y-2">
        <p class="text-xs">允许用户（不勾选表示线路组内全部授权用户）</p>
        <label v-for="user in users" :key="user.id" class="flex items-center gap-2"><input v-model="form.allowedUserIds" :value="user.id" type="checkbox" /> {{ user.displayName || user.username }}</label>
        <p v-if="!users.length" class="text-xs">暂无用户</p>
      </div>
      <label class="block"><input v-model="form.enabled" type="checkbox" /> 启用</label>
    </div>
  </details>
</template>
<script setup lang="ts">
import type { InboundInput, Outbound, OutboundGroup, ProxyChain } from '../../api';
import type { ProxyUser } from '../../../users/api';
defineProps<{ form: InboundInput; editing: boolean; outbounds: Outbound[]; groups: OutboundGroup[]; chains: ProxyChain[]; users: ProxyUser[] }>();
</script>
