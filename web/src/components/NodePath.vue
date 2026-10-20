<template>
  <div class="inline-flex items-center flex-wrap gap-1.5 py-1">
    <template v-for="(hop, idx) in normalizedHops" :key="idx">
      <!-- Node Chip -->
      <div class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full bg-white border border-[#D1D1D6] shadow-2xs text-xs">
        <span
          class="w-1.5 h-1.5 rounded-full shrink-0"
          :class="hop.status === 'unhealthy' ? 'bg-rose-500' : 'bg-emerald-500'"
        ></span>
        <span class="font-medium text-ios-label">{{ hop.nodeName }}</span>
        <span v-if="hop.latency" class="text-[10px] text-ios-tertiary font-mono tabular-nums">
          {{ hop.latency }}ms
        </span>
      </div>

      <!-- Arrow & Protocol Transition -->
      <div v-if="idx < normalizedHops.length - 1" class="inline-flex items-center gap-1 text-ios-tertiary">
        <span class="text-xs">→</span>
        <span
          v-if="hop.protocol"
          class="text-[10px] font-mono tabular-nums font-semibold px-1.5 py-0.5 rounded-full bg-black/[0.04] text-ios-secondary border border-black/[0.08]"
        >
          {{ hop.protocol }}
        </span>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';

interface HopItem {
  nodeName: string;
  protocol?: string;
  latency?: number;
  status?: 'healthy' | 'warning' | 'unhealthy';
}

const props = defineProps<{
  hops?: HopItem[];
  pathString?: string; // Optional simple format "Node A -> Node B"
}>();

const normalizedHops = computed<HopItem[]>(() => {
  if (props.hops && props.hops.length > 0) {
    return props.hops;
  }
  if (props.pathString) {
    const parts = props.pathString.split(/→|->/g).map(s => s.trim());
    return parts.map(p => ({
      nodeName: p,
      status: 'healthy'
    }));
  }
  return [];
});
</script>
