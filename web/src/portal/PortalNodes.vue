<template>
  <section class="panel-card p-6 space-y-4">
    <h2 class="text-base font-semibold leading-normal">可用节点</h2>
    <p v-if="!nodes.length" class="text-ios-tertiary">暂无授权节点</p>
    <div v-for="node in nodes" :key="node.id" class="flex flex-wrap justify-between gap-2 border-b border-black/[0.05] py-3">
      <div class="space-y-1">
        <div>{{ node.name }} <small class="text-ios-tertiary">{{ node.region || '—' }}</small></div>
        <div v-if="node.protocols && node.protocols.length" class="flex flex-wrap gap-1">
          <ProtocolBadge v-for="p in node.protocols" :key="p" :protocol="p" :label="protocolLabel(p)" />
        </div>
      </div>
      <StatusBadge :status="node.status" kind="node" />
    </div>
  </section>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import StatusBadge from '../components/StatusBadge.vue';
import ProtocolBadge from '../components/ProtocolBadge.vue';
import type { PortalNode } from './api';
import { protocolLabel, sortNodes } from './format';
const props = defineProps<{ items: PortalNode[] }>();
const nodes = computed(() => sortNodes(props.items));
</script>
