<template>
  <span
    :class="[
      'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-mono font-semibold border transition-colors',
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
      return 'bg-[#007AFF]/10 text-[#007AFF] border-[#007AFF]/20';
    case 'TROJAN':
      return 'bg-[#AF52DE]/10 text-[#AF52DE] border-[#AF52DE]/20';
    case 'SHADOWSOCKS':
      return 'bg-[#5856D6]/10 text-[#5856D6] border-[#5856D6]/20';
    case 'WIREGUARD':
      return 'bg-[#34C759]/10 text-[#248A3D] border-[#34C759]/20';
    case 'SOCKS5':
      return 'bg-[#32ADE6]/10 text-[#0077A6] border-[#32ADE6]/20';
    case 'HTTP':
      return 'bg-[#FF9500]/10 text-[#B26B00] border-[#FF9500]/20';
    case 'DIRECT':
      return 'bg-[#30B0C7]/10 text-[#007788] border-[#30B0C7]/20';
    case 'BLOCK':
      return 'bg-[#FF3B30]/10 text-[#D70015] border-[#FF3B30]/20';
    default:
      return 'bg-black/[0.04] text-[#636366] border-black/[0.08]';
  }
});
</script>
