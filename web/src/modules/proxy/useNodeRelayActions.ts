import { computed, onUnmounted, ref, type Ref } from 'vue';
import { useRouter } from 'vue-router';
import { APIError, requestScope } from '../../app/apiClient';
import type { NodeView } from '../nodes/api';
import { nodeRelayAPI, type TunnelRule } from '../tunnels/api';
import type { Inbound } from './api';
import { quickRelayProtocolChoices } from './quickRelay';
import { isUserEntry } from './userEntries';
import { proxyNodeStore, getNodeRelayRoutes, createDraftSchemeForEntryNode, type ProxyNodeItem, type NodeRelayRoute } from './proxyNodes';

export function useNodeRelayActions(isOwner: Readonly<Ref<boolean>>, servers: Ref<NodeView[]>, serverInbounds: Ref<Inbound[]>) {
const router = useRouter();

// Relay routes linkage helpers
function getNodeRoutes(node: ProxyNodeItem, serverCards: ProxyNodeItem[] = []): NodeRelayRoute[] {
  return getNodeRelayRoutes(node, proxyNodeStore.schemes, serverCards);
}

function goToEditRelayRoute(route: NodeRelayRoute) {
  router.push({
    path: '/proxy/relay',
    query: {
      schemeId: route.schemeId,
      focusConnId: route.connectionId,
      focusInstId: route.targetInstanceId
    }
  });
}

const relayNode = ref<ProxyNodeItem | null>(null);
const quickRelayInboundId = ref('');
// An imported node can only be a pure-forwarding exit, so it opens the quick
// dialog in forward mode instead of a server canvas it could never publish.
const quickRelayImportId = ref('');
const nodeRelays = ref<Record<string, TunnelRule[]>>({});
const relayErrors = ref<Record<string, string>>({});
const relayReadComplete = ref(false);
const importedRelayError = computed(() => {
  const failed = servers.value.flatMap(({ id, name }) => {
    const error = relayErrors.value[id];
    return error ? [`${name}：${error}`] : [];
  });
  if (failed.length) return `导入节点的中转线路数据不完整。入口分组读取失败（${failed.join('；')}）`;
});
const importedRelayReady = computed(() => relayReadComplete.value &&
  servers.value.every(({ id }) => nodeRelays.value[id] && !relayErrors.value[id]));
function relayError(node: ProxyNodeItem) {
  return node.sourceType === 'self_hosted' ? relayErrors.value[node.sourceId || ''] : importedRelayError.value;
}
function relayListReady(node: ProxyNodeItem) {
  if (node.sourceType !== 'self_hosted') return importedRelayReady.value;
  const id = node.sourceId || '';
  return Boolean(nodeRelays.value[id] && !relayErrors.value[id]);
}
function relayCount(node: ProxyNodeItem) { return getNodeRoutes(node).length + linkedRelays(node).length; }
// A server's relay list also carries the import-target rules it is the entry of;
// those belong under the imported node, never under the entry server's cards.
function linkedRelays(node: ProxyNodeItem) {
  if (node.sourceType !== 'self_hosted') {
    const seen = new Set<string>();
    return Object.values(nodeRelays.value).flat().filter(rule => rule.target.mode === 'import' && rule.target.importId === node.id && !seen.has(rule.id) && seen.add(rule.id));
  }
  return (nodeRelays.value[node.sourceId || ''] || []).filter(rule => rule.target.mode === 'linked' && (!node.inboundId || rule.target.inboundId === node.inboundId));
}
// A linked or import-target rule is listed under its target node's relays, so its
// entry inbound does not need a row of its own in the node list.
function isLinkedRelayRule(ruleId: string): boolean {
  return Object.values(nodeRelays.value).some(items => items.some(rule => rule.id === ruleId && rule.target.mode !== 'manual'));
}
let relayReadGeneration = 0;
let relayRead: Promise<void> | undefined;
let relayDisposed = false;
onUnmounted(() => { relayDisposed = true; relayReadGeneration++; });
async function loadNodeRelays() {
  if (relayDisposed) return;
  const generation = ++relayReadGeneration;
  // Same-URL GETs share the API client's in-flight transport. Wait for it to
  // finish before reading fresh rules, and coalesce waiting callers.
  await relayRead;
  if (generation !== relayReadGeneration) return;
  const pending = readNodeRelays(generation);
  relayRead = pending;
  try {
    await pending;
  } finally {
    if (relayRead === pending) relayRead = undefined;
  }
}
async function readNodeRelays(generation: number) {
  const ids = servers.value.map(node => node.id);
  const rules: Record<string, TunnelRule[]> = {};
  const errors: Record<string, string> = {};
  try {
    if (ids.length) {
      const result = await nodeRelayAPI.listRules(ids);
      for (const item of result.items) {
        if (!item.error) rules[item.nodeId] = item.rules;
        else {
          errors[item.nodeId] = item.error.message;
          const previous = nodeRelays.value[item.nodeId];
          if (item.error.code !== 'not_found' && previous) rules[item.nodeId] = previous;
        }
      }
    }
  } catch (error) {
    const accessLost = error instanceof APIError && (error.status === 401 || error.status === 403);
    for (const id of ids) {
      errors[id] = (error as Error).message;
      const previous = nodeRelays.value[id];
      if (!accessLost && previous) rules[id] = previous;
    }
  }
  if (generation !== relayReadGeneration) return;
  nodeRelays.value = rules;
  relayErrors.value = errors;
  relayReadComplete.value = true;
}
function canUseExistingTunnel(node: ProxyNodeItem): boolean {
  if (!isOwner.value || node.sourceType !== 'self_hosted') return false;
  return serverInbounds.value.some(inbound => inbound.id === node.inboundId && inbound.enabled && isUserEntry(inbound) && inbound.protocol !== 'forward');
}
function canAddRelay(node: ProxyNodeItem): boolean {
  if (!isOwner.value) return false;
  if (node.sourceType !== 'self_hosted') return servers.value.some(server => server.enabled && !server.deletedAt);
  return canUseExistingTunnel(node) && servers.value.some(server => server.id === node.sourceId && server.enabled && !server.deletedAt) && servers.value.some(server =>
    server.id !== node.sourceId && server.enabled && !server.deletedAt && quickRelayProtocolChoices(server).length > 0);
}
function handleAddRelay(node: ProxyNodeItem) {
  if (!canAddRelay(node)) return;
  if (node.sourceType === 'self_hosted') quickRelayInboundId.value = node.inboundId!;
  else quickRelayImportId.value = node.id;
}
async function createRelayScheme() {
  const scope = requestScope();
  const node = relayNode.value!;
  const { scheme, entryInstanceId } = await createDraftSchemeForEntryNode(node, { servers: servers.value, inbounds: serverInbounds.value, imported: proxyNodeStore.manualNodes });
  if (!relayDisposed && scope === requestScope()) await router.push({ path: '/proxy/relay', query: { schemeId: scheme.id, focusInstId: entryInstanceId } });
}

return {
  relayNode, quickRelayInboundId, quickRelayImportId, relayError, relayListReady, getNodeRoutes, goToEditRelayRoute,
  relayCount, linkedRelays, loadNodeRelays, canUseExistingTunnel, canAddRelay,
  handleAddRelay, createRelayScheme, isLinkedRelayRule,
};
}
