<template>
  <div :class="variant === 'card' ? 'space-y-1.5' : 'space-y-1'">
    <div v-for="{ relay, entries } in rows" :key="relay.rule.id"
      :class="variant === 'card' ? 'bg-[#F8F9FA] rounded-xl p-2.5 border border-black/[0.03] flex items-center justify-between gap-2 hover:bg-[#F0F2F5] transition-colors' : ''">
      <div class="min-w-0 flex-1">
        <div :class="variant === 'table' ? 'flex items-center gap-1.5 text-neutral-800' : ''">
          <div :class="variant === 'card' ? 'font-bold text-xs text-neutral-800 truncate' : 'font-semibold truncate max-w-[150px]'"
            :title="path(relay.rule) + ' · ' + relay.rule.name">{{ path(relay.rule) }}</div>
          <span v-if="variant === 'table'" class="px-1 py-0.2 rounded text-xs font-bold shrink-0" :class="statusTone(relay.rule.status)">{{ publishStatuses[relay.rule.status].label }}</span>
        </div>
        <div v-if="variant === 'card'" class="flex flex-wrap items-center gap-1.5 mt-0.5">
          <span class="px-1.5 py-0.2 rounded text-xs font-semibold bg-teal-50 text-teal-700">隧道转发</span>
          <span class="text-neutral-300">·</span>
          <span class="px-1.5 py-0.2 rounded text-xs font-semibold" :class="statusTone(relay.rule.status)">{{ publishStatuses[relay.rule.status].label }}</span>
          <span class="text-neutral-300">·</span>
          <span class="text-xs text-ios-secondary">{{ relay.rule.subscriptionVisible ? '对用户可见' : '对用户隐藏' }}</span>
        </div>
        <RelayRouteEntries :entries="entries" @copy="emit('copy', $event)" />
      </div>
      <button v-if="variant === 'card' && isOwner" type="button"
        @click.stop="router.push('/proxy/tunnel-rules')"
        class="px-2 py-1 text-xs font-bold text-ios-blue hover:bg-ios-blue/10 rounded-lg transition-colors shrink-0"
        title="编辑隧道规则">编辑</button>
    </div>
    <p v-if="error" class="text-xs text-ios-red">关联规则加载失败：{{ error }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { useRouter } from 'vue-router';
import RelayRouteEntries from '../../proxy/components/RelayRouteEntries.vue';
import { publishStatuses } from '../model';
import type { NodeRelay, PublishStatus, TunnelRule } from '../api';
const props = defineProps<{
  relays: NodeRelay[]; error?: string; variant: 'card' | 'table'; isOwner: boolean;
  entryAddress: (rule: TunnelRule) => { server: string; port: number } | undefined;
}>();
const emit = defineEmits<{ copy: [address: string] }>();
const router = useRouter();
const rows = computed(() => props.relays.map(relay => {
  const address = props.entryAddress(relay.rule);
  return { relay, entries: address ? [{ id: relay.rule.id, name: relay.rule.name, ...address }] : [] };
}));
function path(rule: TunnelRule) {
  return [rule.entryNodeName, ...(rule.tunnelId ? ['经 ' + rule.tunnelName] : []), rule.target.mode === 'manual' ? rule.target.host : rule.target.nodeName].join(' → ');
}
function statusTone(status: PublishStatus) {
  switch (status) {
    case 'published': return props.variant === 'card' ? 'bg-[#E8F8F0] text-[#12B76A]' : 'bg-emerald-100 text-emerald-800';
    case 'modified': case 'needs_publish': return 'bg-amber-100 text-amber-800';
    case 'pending_delete': return 'bg-orange-100 text-orange-800';
    case 'draft': return props.variant === 'card' ? 'bg-neutral-200 text-neutral-700' : 'bg-neutral-100 text-neutral-600';
  }
}
</script>
