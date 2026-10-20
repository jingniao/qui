<template>
  <span
    :class="[
      'inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-mono tabular-nums font-semibold border transition-colors',
      badgeClass
    ]"
  >
    {{ protocol }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{
  protocol: string;
}>();

const badgeClass = computed(() => {
  const p = props.protocol.toUpperCase();
  switch (p) {
    case 'VLESS':
      return 'bg-ios-blue/10 text-ios-blue border-ios-blue/20';
    case 'TROJAN':
      return 'bg-ios-purple/10 text-ios-purple border-ios-purple/20';
    case 'SHADOWSOCKS':
      return 'bg-ios-indigo/10 text-ios-indigo border-ios-indigo/20';
    case 'WIREGUARD':
      return 'bg-ios-green/10 text-emerald-800 border-ios-green/20';
    case 'SOCKS5':
      return 'bg-ios-teal/10 text-cyan-800 border-ios-teal/20';
    case 'HTTP':
      return 'bg-ios-orange/10 text-amber-800 border-ios-orange/20';
    case 'DIRECT':
      return 'bg-ios-teal/10 text-cyan-800 border-ios-teal/20';
    case 'BLOCK':
      return 'bg-ios-red/10 text-rose-800 border-ios-red/20';
    default:
      return 'bg-black/[0.04] text-ios-tertiary border-black/[0.08]';
  }
});
</script>
