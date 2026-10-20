<template>
  <div class="space-y-6">
    <PageHeader title="代理链" parent="代理" description="使用专用内部入站连接两个或三个节点。保存后通过批次发布，按出口到入口的顺序应用。"><template #actions><router-link to="/proxy/chains/new" class="ios-btn-primary">创建代理链</router-link><router-link to="/system/revisions" class="ios-btn-secondary">查看发布批次</router-link></template></PageHeader>
    <div class="flex items-center gap-3">
      <span class="text-xs font-medium text-ios-secondary">包含节点</span>
      <select v-model="nodeId" @change="page = 1; load()" class="ios-filter-select">
        <option value="">全部节点</option>
        <option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option>
      </select>
    </div>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="loading && !chains.length" /><EmptyState v-else-if="!chains.length" title="暂无代理链" description="先创建目标内部入站和对应中转出站，再组合代理链。" />
    <DataTable v-else><template #header><th>名称 / 配置路径</th><th>每跳协议与最近探测</th><th>能力 / 失败策略</th><th>状态</th><th>操作</th></template><template #body><tr v-for="chain in chains" :key="chain.id">
      <td class="p-4"><router-link :to="'/proxy/chains/' + chain.id + '/edit'" class="font-semibold text-blue-600">{{ chain.name }}</router-link><p class="text-xs mt-2">{{ chain.nodeIds.map(nodeName).join(' → ') }} → {{ outboundName(chain.finalOutboundId) }}</p></td>
      <td class="text-xs py-3"><p v-for="hop in chain.hops" :key="hop.position" class="mb-2">{{ nodeName(hop.sourceNodeId) }} → {{ nodeName(hop.targetNodeId) }} · {{ hop.protocol }}<span class="block text-gray-500">{{ healthLabel(outbounds.find(item => item.id === hop.outboundId), outbounds.find(item => item.id === hop.outboundId)?.probeInterval) }}</span></p></td>
      <td class="text-xs">{{ chain.supportTcp ? 'TCP' : '' }} {{ chain.supportUdp ? 'UDP' : '' }}<p>{{ chain.failurePolicy }}</p></td><td><StatusBadge :status="chain.enabled ? 'active' : 'disabled'" /></td>
      <td class="space-x-2"><router-link :to="'/proxy/chains/' + chain.id + '/edit'" class="ios-action-btn-primary">编辑</router-link><button :disabled="busy || !chain.enabled" @click="makeBatch(chain)" class="ios-action-btn-primary disabled:opacity-40">创建发布草稿</button><button @click="target = chain; removeConfirm = true" class="ios-action-btn-danger">删除</button></td>
    </tr></template><template #footer><Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" /></template></DataTable>
    <p class="text-xs text-gray-500">路径来自配置草稿，探测来自节点最近上报；缺少或过期结果不会显示为健康。草稿创建后仍需在配置版本页面确认发布。</p>
    <ConfirmDialog v-model="removeConfirm" title="删除代理链草稿" :message="'删除 ' + (target?.name || '') + '？被入站、策略或备用链引用时不能删除。运行配置需重新发布。'" @confirm="remove" />
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'; import { useRouter } from 'vue-router';
import { proxyAPI, choices, healthLabel, type ProxyChain, type Outbound } from '../api'; import { deploymentAPI } from '../../system/deploymentApi'; import { request, type Page } from '../../../app/apiClient'; import { type NodeView } from '../../nodes/api'; import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue'; import DataTable from '../../../components/DataTable.vue'; import StatusBadge from '../../../components/StatusBadge.vue'; import Pagination from '../../../components/Pagination.vue'; import ConfirmDialog from '../../../components/ConfirmDialog.vue'; import ErrorState from '../../../components/ErrorState.vue'; import LoadingSkeleton from '../../../components/LoadingSkeleton.vue'; import EmptyState from '../../../components/EmptyState.vue';
const router = useRouter(); const chains = ref<ProxyChain[]>([]); const nodes = ref<NodeView[]>([]); const outbounds = ref<Outbound[]>([]); const nodeId = ref(''); const page = ref(1); const total = ref(0); const loading = ref(false); const busy = ref(false); const error = ref(''); const target = ref<ProxyChain | null>(null); const removeConfirm = ref(false);
function nodeName(id: string) { return nodes.value.find(node => node.id === id)?.name || id; } function outboundName(id: string) { return id === 'direct' ? 'Direct' : id === 'block' ? 'Block' : outbounds.value.find(item => item.id === id)?.name || id; }
async function load() { if (loading.value) return; loading.value = true; try { const [result, ns, outs] = await Promise.all([proxyAPI.chains(nodeId.value, page.value), choices((p,s) => request<Page<NodeView>>('/nodes?page=' + p + '&pageSize=' + s)), choices((p,s) => proxyAPI.outbounds('',p,s))]); chains.value = result.items; total.value = result.total; nodes.value = ns; outbounds.value = outs; error.value = ''; } catch (e) { error.value = (e as Error).message; } finally { loading.value = false; } }
function changePage(value: number) { page.value = value; load(); }
async function makeBatch(chain: ProxyChain) { if (busy.value) return; busy.value = true; try { await deploymentAPI.createBatch(chain.id, '更新代理链 ' + chain.name); showToast('批次草稿已创建，请确认发布'); await router.push('/system/revisions'); } catch (e) { showToast((e as Error).message, 'error'); } finally { busy.value = false; } }
async function remove() { if (!target.value) return; try { await proxyAPI.deleteChain(target.value.id); showToast('代理链草稿已删除'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } }
onMounted(() => { load(); window.addEventListener('proxy-node-status', load); }); onUnmounted(() => window.removeEventListener('proxy-node-status', load));
</script>
