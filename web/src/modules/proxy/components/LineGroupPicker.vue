<template>
  <FormDrawer
    :model-value="modelValue"
    title="添加代理节点"
    :loading="saving"
    width="xl"
    :confirm-text="selected.length ? `添加节点（已选 ${selected.length} 个）` : '添加'"
    @update:model-value="$emit('update:modelValue', $event)"
    @confirm="$emit('save', selected)"
  >
    <div class="space-y-4">
      <!-- Search & Filters -->
      <div class="space-y-2">
        <input
          v-model="keyword"
          class="form-input-base text-xs"
          placeholder="搜索节点名称、协议、端口或所属服务器/来源"
          aria-label="搜索"
        />

        <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
          <select v-model="selectedSourceType" class="form-input-base text-xs" aria-label="来源类型筛选">
            <option value="">全部来源类型</option>
            <option value="self_hosted">自建节点</option>
            <option value="subscription">机场导入节点</option>
            <option value="manual">单独导入节点</option>
          </select>
          <select v-model="selectedProtocol" class="form-input-base text-xs" aria-label="协议筛选">
            <option value="">全部协议</option>
            <option v-for="proto in protocols" :key="proto" :value="proto">{{ proto.toUpperCase() }}</option>
          </select>
          <select v-model="selectedSourceKey" class="form-input-base text-xs" aria-label="来源名称筛选">
            <option value="">全部服务器/来源</option>
            <option v-for="src in availableSources" :key="src" :value="src">{{ src }}</option>
          </select>
        </div>
      </div>

      <!-- Quick Select Helpers -->
      <div class="flex items-center justify-between text-xs text-ios-secondary px-1">
        <span>可选节点：{{ filteredNodes.length }} 个</span>
        <div class="flex items-center gap-2">
          <button
            type="button"
            @click="selectAllFiltered"
            class="text-ios-blue hover:underline"
            :disabled="!filteredNodes.length"
          >
            全选当前筛选
          </button>
          <span class="text-black/20">|</span>
          <button
            type="button"
            @click="selected = []"
            class="text-ios-tertiary hover:text-ios-label"
            :disabled="!selected.length"
          >
            清空已选
          </button>
        </div>
      </div>

      <!-- Item selection list -->
      <div class="space-y-2 max-h-96 overflow-y-auto pr-1">
        <label
          v-for="node in filteredNodes"
          :key="node.id"
          class="flex items-start gap-3 rounded-xl bg-ios-bg p-3 cursor-pointer hover:bg-black/[0.03] transition-colors border border-black/[0.04]"
        >
          <input
            v-model="selected"
            type="checkbox"
            :value="node.id"
            :disabled="!!disabledReason(node) || saving"
            class="mt-1 accent-ios-blue shrink-0 w-4 h-4 rounded"
          />
          <div class="flex-1 min-w-0 space-y-1">
            <div class="flex items-center gap-2 flex-wrap">
              <span class="text-sm font-semibold text-ios-label truncate">
                {{ node.name }}
              </span>
              <span
                class="px-1.5 py-0.5 rounded text-[10px] font-semibold"
                :class="sourceBadgeClass(node.sourceType)"
              >
                {{ node.sourceTypeName }}
              </span>
              <span class="inline-flex items-center px-1.5 py-0.5 rounded text-[11px] font-mono font-bold uppercase bg-ios-blue/10 text-ios-blue">
                {{ node.protocol }}
              </span>
              <span class="text-xs font-mono text-ios-secondary">
                :{{ node.port }}
              </span>
            </div>
            <div class="flex items-center gap-2 text-xs text-ios-tertiary">
              <span>{{ node.sourceType === 'self_hosted' ? '所属服务器：' : '来源：' }}{{ node.sourceName }}</span>
              <span v-if="node.subscriptionVisible" class="text-ios-green">· 订阅可见</span>
            </div>
          </div>
          <span v-if="disabledReason(node)" class="text-xs text-ios-orange">{{ disabledReason(node) }}</span>
          <StatusBadge v-if="node.sourceType === 'self_hosted'" :status="node.enabled ? 'active' : 'disabled'" />
        </label>
        <p v-if="!filteredNodes.length" class="text-sm text-ios-tertiary text-center py-6">
          没有符合条件的代理节点
        </p>
      </div>

      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    </div>
  </FormDrawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import type { NodeGroup } from '../../nodes/groupsApi';
import type { NodeView } from '../../nodes/api';
import type { Inbound } from '../api';

export interface GroupPickerNode {
  id: string;
  name: string;
  protocol: string;
  port: number | string;
  sourceType: 'self_hosted' | 'subscription' | 'manual';
  sourceTypeName: string;
  sourceName: string;
  serverId?: string;
  sourceId?: string;
  proxyKey?: string;
  status: 'online' | 'offline' | 'untested' | 'active' | 'disabled';
  enabled: boolean;
  subscriptionVisible?: boolean;
  internalRelay?: boolean;
}

const props = withDefaults(defineProps<{
  modelValue: boolean;
  group: NodeGroup;
  servers?: NodeView[];
  inbounds?: Inbound[];
  nodes?: GroupPickerNode[];
  currentInboundIds?: string[];
  currentSelectedIds?: string[];
  saving: boolean;
  error: string;
}>(), {
  servers: () => [],
  inbounds: () => [],
  nodes: () => [],
  currentInboundIds: () => [],
  currentSelectedIds: () => []
});

defineEmits<{
  'update:modelValue': [value: boolean];
  save: [ids: string[]];
}>();

const keyword = ref('');
const selectedSourceType = ref('');
const selectedProtocol = ref('');
const selectedSourceKey = ref('');
const selected = ref<string[]>([]);

const protocols = ['vless', 'vmess', 'trojan', 'shadowsocks', 'hysteria2', 'tuic', 'wireguard', 'socks5', 'http'];

const serverMap = computed(() => {
  const map: Record<string, NodeView> = {};
  for (const s of props.servers) {
    map[s.id] = s;
  }
  return map;
});

function serverName(serverId: string) {
  return serverMap.value[serverId]?.name || serverId;
}

function sourceBadgeClass(type: string) {
  if (type === 'self_hosted') return 'bg-ios-green/10 text-ios-green';
  if (type === 'subscription') return 'bg-ios-purple/10 text-ios-purple';
  return 'bg-ios-blue/10 text-ios-blue';
}

const allCandidateNodes = computed<GroupPickerNode[]>(() => {
  if (props.nodes && props.nodes.length) {
    return props.nodes;
  }
  return (props.inbounds || []).map(ib => ({
    id: ib.id,
    name: ib.name || `${ib.protocol.toUpperCase()}-${ib.port}`,
    protocol: ib.protocol,
    port: ib.port,
    sourceType: 'self_hosted' as const,
    sourceTypeName: '自建',
    sourceName: serverName(ib.nodeId),
    serverId: ib.nodeId,
    status: ib.enabled ? 'active' : 'disabled',
    enabled: ib.enabled,
    subscriptionVisible: ib.subscriptionVisible,
    internalRelay: ib.internalRelay
  }));
});

const currentAssignedIds = computed(() => {
  return props.currentSelectedIds?.length ? props.currentSelectedIds : props.currentInboundIds;
});

const availableSources = computed(() => {
  const set = new Set<string>();
  for (const node of allCandidateNodes.value) {
    if (node.sourceName) set.add(node.sourceName);
  }
  return Array.from(set).sort();
});

watch(() => props.modelValue, open => {
  if (open) {
    keyword.value = '';
    selectedSourceType.value = '';
    selectedProtocol.value = '';
    selectedSourceKey.value = '';
    selected.value = [];
  }
});

const filteredNodes = computed(() => {
  const q = keyword.value.trim().toLowerCase();
  const assigned = currentAssignedIds.value;

  return allCandidateNodes.value.filter(node => {
    if (assigned.includes(node.id)) return false;

    if (selectedSourceType.value && node.sourceType !== selectedSourceType.value) {
      return false;
    }
    if (selectedProtocol.value && node.protocol.toLowerCase() !== selectedProtocol.value.toLowerCase()) {
      return false;
    }
    if (selectedSourceKey.value && node.sourceName !== selectedSourceKey.value) {
      return false;
    }

    if (!q) return true;
    const nameMatch = (node.name || '').toLowerCase().includes(q);
    const srcMatch = (node.sourceName || '').toLowerCase().includes(q);
    const protoMatch = (node.protocol || '').toLowerCase().includes(q);
    const portMatch = String(node.port).includes(q);
    return nameMatch || srcMatch || protoMatch || portMatch;
  });
});

function disabledReason(node: GroupPickerNode): string {
  if (node.sourceType !== 'self_hosted') return '';
  if (node.internalRelay) return '内部中转入站，不提供用户接入';
  if (!node.enabled) return '入站已停用';
  if (!node.subscriptionVisible) return '入站订阅不可见';
  return '';
}
function selectAllFiltered() {
  const toAdd = filteredNodes.value.filter(n => !disabledReason(n)).map(n => n.id);
  selected.value = Array.from(new Set([...selected.value, ...toAdd]));
}
</script>
