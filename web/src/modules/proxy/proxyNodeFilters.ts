import { computed, ref, watch } from 'vue';

const sourceChoices = [
  { value: 'self_hosted', label: '自建节点' },
  { value: 'relay', label: '中转过的' },
  { value: 'imported', label: '导入的节点' }
] as const;

interface NodeFilterEntry {
  sourceType: 'self_hosted' | 'tunnel_relay' | 'subscription' | 'manual';
  protocol: string;
}

interface NodeFilterOption {
  value: string;
  label: string;
}

const protocolLabels: Record<string, string> = {
  vmess: 'VMess',
  shadowsocks: 'Shadowsocks',
  trojan: 'Trojan',
  hysteria2: 'Hysteria2',
  wireguard: 'WireGuard'
};

export function useProxyNodeFilters<T extends NodeFilterEntry>(
  nodes: () => readonly T[],
  options: {
    hasRelay?: (node: T) => boolean;
    protocolsFor?: (node: T) => readonly string[];
    statusFor?: (node: T) => NodeFilterOption;
  } = {}
) {
  const source = ref('all');
  const protocol = ref('all');
  const status = ref('all');

  const entries = computed(() => nodes().map(node => ({
    node,
    selfHosted: node.sourceType === 'self_hosted',
    relayed: node.sourceType === 'tunnel_relay' || Boolean(options.hasRelay?.(node)),
    imported: node.sourceType === 'manual' || node.sourceType === 'subscription',
    protocols: (options.protocolsFor?.(node) ?? [node.protocol])
      .map(value => value.trim().toLowerCase()).filter(value => value && value !== '—')
  })));

  function matchesSource(entry: typeof entries.value[number], selected: string) {
    if (selected === 'self_hosted') return entry.selfHosted;
    if (selected === 'relay') return entry.relayed;
    if (selected === 'imported') return entry.imported;
    return true;
  }

  const sourceOptions = computed<NodeFilterOption[]>(() => [
    { value: 'all', label: '全部来源' },
    ...sourceChoices.filter(choice => entries.value.some(entry => matchesSource(entry, choice.value)))
  ]);
  const sourceMatches = computed(() => entries.value.filter(entry => matchesSource(entry, source.value)));
  const protocolOptions = computed<NodeFilterOption[]>(() => [
    { value: 'all', label: '全部协议' },
    ...[...new Set(sourceMatches.value.flatMap(entry => entry.protocols))].sort()
      .map(value => ({ value, label: protocolLabels[value] ?? value.toUpperCase() }))
  ]);
  const protocolMatches = computed(() => sourceMatches.value.filter(entry =>
    protocol.value === 'all' || entry.protocols.includes(protocol.value)
  ));
  const statusOptions = computed<NodeFilterOption[]>(() => {
    const available = new Map<string, NodeFilterOption>();
    if (options.statusFor) {
      for (const { node } of protocolMatches.value) {
        const option = options.statusFor(node);
        if (!available.has(option.value)) available.set(option.value, option);
      }
    }
    return [{ value: 'all', label: '全部状态' }, ...available.values()];
  });
  const filteredNodes = computed(() => protocolMatches.value
    .map(entry => entry.node)
    .filter(node => status.value === 'all' || options.statusFor?.(node).value === status.value));

  watch(sourceOptions, choices => {
    if (!choices.some(choice => choice.value === source.value)) source.value = 'all';
  });
  watch(protocolOptions, choices => {
    if (!choices.some(choice => choice.value === protocol.value)) protocol.value = 'all';
  });
  watch(statusOptions, choices => {
    if (!choices.some(choice => choice.value === status.value)) status.value = 'all';
  });

  return { source, protocol, status, sourceOptions, protocolOptions, statusOptions, filteredNodes };
}
