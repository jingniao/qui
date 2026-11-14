import { ref, type Ref } from 'vue';
import { useRouter } from 'vue-router';
import type { NodeView } from '../nodes/api';
import { nodeRelayAPI, type NodeRelay } from '../tunnels/api';
import type { Inbound } from './api';
import { quickRelayProtocolChoices } from './quickRelay';
import { isUserEntry } from './userEntries';
import { proxyNodeStore, getNodeRelayRoutes, createDraftSchemeForEntryNode, type ProxyNodeItem, type NodeRelayRoute } from './proxyNodes';

export function useNodeRelayActions(isOwner: Readonly<Ref<boolean>>, servers: Ref<NodeView[]>, serverInbounds: Ref<Inbound[]>) {
const router = useRouter();

// Relay routes linkage helpers
function getNodeRoutes(node: ProxyNodeItem): NodeRelayRoute[] {
  return getNodeRelayRoutes(node, proxyNodeStore.schemes);
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
const nodeRelays = ref<Record<string, NodeRelay[]>>({});
const relayErrors = ref<Record<string, string>>({});
function relayCount(node: ProxyNodeItem) { return getNodeRoutes(node).length + linkedRelays(node).length; }
function linkedRelays(node: ProxyNodeItem) { return node.sourceType === 'self_hosted' ? (nodeRelays.value[node.sourceId || ''] || []).filter(r => !node.inboundId || (r.rule.target.mode === 'linked' && r.rule.target.inboundId === node.inboundId)) : []; }
async function loadNodeRelays() {
  await Promise.all(servers.value.map(async node => {
    try { nodeRelays.value[node.id] = (await nodeRelayAPI.list(node.id)).items; delete relayErrors.value[node.id]; }
    catch (e) { relayErrors.value[node.id] = (e as Error).message; }
  }));
}
function canUseExistingTunnel(node: ProxyNodeItem): boolean {
  if (!isOwner.value || node.sourceType !== 'self_hosted') return false;
  return serverInbounds.value.some(inbound => inbound.id === node.inboundId && inbound.enabled && isUserEntry(inbound) && inbound.protocol !== 'forward');
}
function canAddRelay(node: ProxyNodeItem): boolean {
  if (!isOwner.value) return false;
  if (node.sourceType !== 'self_hosted') return true;
  return canUseExistingTunnel(node) && servers.value.some(server => server.id === node.sourceId && server.enabled && !server.deletedAt) && servers.value.some(server =>
    server.id !== node.sourceId && server.enabled && !server.deletedAt && quickRelayProtocolChoices(server).length > 0);
}
function handleAddRelay(node: ProxyNodeItem) {
  if (!canAddRelay(node)) return;
  if (node.sourceType === 'self_hosted') quickRelayInboundId.value = node.inboundId!;
  else relayNode.value = node;
}
async function createRelayScheme() {
  const node = relayNode.value!;
  const { scheme, entryInstanceId } = await createDraftSchemeForEntryNode(node, { servers: servers.value, inbounds: serverInbounds.value, imported: proxyNodeStore.manualNodes });
  await router.push({ path: '/proxy/relay', query: { schemeId: scheme.id, focusInstId: entryInstanceId } });
}

return {
  relayNode, quickRelayInboundId, relayErrors, getNodeRoutes, goToEditRelayRoute,
  relayCount, linkedRelays, loadNodeRelays, canUseExistingTunnel, canAddRelay,
  handleAddRelay, createRelayScheme,
};
}
