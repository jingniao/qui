<template>
  <div class="space-y-6">
    <PageHeader title="节点拓扑" parent="节点" description="分别查看成功发布路径与配置草稿，核对中转依赖和节点版本。"><template #actions><button @click="load" class="ios-btn-secondary">刷新</button><router-link to="/proxy/chains" class="ios-btn-primary">管理代理链</router-link></template></PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="loading && !nodes.length" />
    <template v-else>
      <div class="panel-card p-4 grid md:grid-cols-3 gap-4 text-sm">
        <label>节点组<select v-model="groupId" class="form-input-base mt-2"><option value="">全部节点组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label>
        <label>代理链<select v-model="chainId" class="form-input-base mt-2"><option value="">全部代理链</option><option v-for="chain in chains" :key="chain.id" :value="chain.id">{{ chain.name }}{{ chain.enabled ? '' : '（停用草稿）' }}</option></select></label>
        <label>中转协议<select v-model="protocol" class="form-input-base mt-2"><option value="">全部协议</option><option v-for="item in protocols" :key="item" :value="item">{{ item }}</option></select></label>
      </div>
      <section class="panel-card p-6"><h2 class="font-semibold mb-4">节点 · {{ visibleNodes.length }}</h2><EmptyState v-if="!visibleNodes.length" title="没有匹配的节点" description="调整筛选条件或添加节点。" />
        <div v-else class="grid sm:grid-cols-2 lg:grid-cols-3 gap-4"><router-link v-for="node in visibleNodes" :key="node.id" :to="'/nodes/' + node.id" class="border border-gray-100 rounded-xl p-4 hover:border-blue-300 space-y-2"><div class="flex items-center justify-between gap-2"><span class="font-semibold">{{ node.name }}</span><StatusBadge :status="node.status" /></div><p class="text-xs text-gray-500">{{ node.publicIp || node.hostname || '尚未上报地址' }}</p><p class="text-xs text-gray-500">{{ node.capabilities.core_name || '尚未检测核心' }} {{ node.capabilities.core_version }}</p><p v-if="node.maintenance" class="text-xs text-amber-700">维护中</p><p class="text-xs text-gray-500">最近心跳：{{ node.lastHeartbeat ? formatTime(node.lastHeartbeat) : '尚无心跳' }}</p></router-link></div>
      </section>
      <section class="panel-card p-6 space-y-4"><h2 class="font-semibold">已发布路径</h2><p class="text-xs text-gray-500">来源：中央收到成功回执的配置快照。依赖匹配仅核对配置，不表示链路已经连通。</p>
        <EmptyState v-if="!visiblePublished.length" title="没有匹配的已发布路径" description="调整筛选条件，或发布对应节点配置。" />
        <div v-for="item in visiblePublished" :key="item.nodeId" class="border border-gray-100 rounded-xl p-4 space-y-3 text-sm">
          <p class="font-semibold">{{ nodeName(item.nodeId) }} · {{ item.revision ? `成功版本 #${item.revision}` : '尚未发布' }}</p>
          <p class="text-xs" :class="item.observation==='fresh_control'?'text-gray-500':'text-amber-700'">{{ observationNames[item.observation] }} · 节点上报版本 #{{ item.reportedRevision }} · {{ snapshotNames[item.snapshot] }}</p>
          <p v-for="relay in item.relays.filter(relay=>!protocol||relay.protocol===protocol)" :key="relay.outboundId">{{ nodeName(item.nodeId) }} → {{ nodeName(relay.targetNodeId) }} · {{ relay.protocol }} · 目标版本 #{{ relay.targetRevision }}<span class="block text-xs mt-1" :class="relay.dependency==='matched'?'text-gray-500':'text-amber-700'">{{ dependencyNames[relay.dependency] }}<template v-if="relay.dependency==='mismatch' && relay.dependencyDetail"> · {{ dependencyDetailNames[relay.dependencyDetail] || '配置依赖不一致' }}</template></span></p>
          <div v-for="chain in item.chains.filter(chain=>!chainId||chain.id===chainId)" :key="chain.id" class="text-xs text-gray-500">链 {{ chainName(chain.id) }}：{{ chain.node_ids.map(nodeName).join(' → ') }} · 最终出口 {{ targetName(chain.final_outbound_id||'') }}</div>
          <p v-if="item.snapshot==='available'&&!item.relays.length&&!item.chains.length" class="text-xs text-gray-500">此成功快照没有节点中转或代理链。</p>
        </div>
      </section>
      <section class="panel-card p-6 space-y-4"><h2 class="font-semibold">代理链草稿路径 · {{ visibleChains.length }}</h2><EmptyState v-if="!visibleChains.length" title="没有匹配的代理链" description="可在代理模块创建两到三个节点的链路。" />
        <div v-for="chain in visibleChains" :key="chain.id" class="border border-gray-100 rounded-xl p-4 space-y-3"><div class="flex justify-between gap-3"><router-link :to="'/proxy/chains/' + chain.id + '/edit'" class="text-blue-600 font-semibold">{{ chain.name }}</router-link><StatusBadge :status="chain.enabled ? 'active' : 'disabled'" /></div>
          <div class="flex flex-wrap items-center gap-2 text-sm"><template v-for="(id,index) in chain.nodeIds" :key="id"><span v-if="index" class="text-gray-400">→</span><router-link :to="'/nodes/' + id" class="rounded-lg bg-blue-50 text-blue-800 px-3 py-2">{{ nodeName(id) }}</router-link></template><span class="text-gray-400">→</span><span>{{ targetName(chain.finalOutboundId) }}</span></div>
          <p v-for="hop in chain.hops" :key="hop.position" class="text-xs text-gray-500">{{ nodeName(hop.sourceNodeId) }} → {{ nodeName(hop.targetNodeId) }} · {{ hop.protocol }} · {{ healthLabel(outbounds.find(out => out.id === hop.outboundId), outbounds.find(out => out.id === hop.outboundId)?.probeInterval) }}</p>
          <p class="text-xs text-gray-500">失败策略：{{ failureNames[chain.failurePolicy] }}<span v-if="chain.backupTargetId"> · {{ targetName(chain.backupTargetId) }}</span></p>
        </div>
      </section>
      <section class="panel-card p-6 space-y-4"><h2 class="font-semibold">已配置中转出站 · {{ visibleRelays.length }}</h2><EmptyState v-if="!visibleRelays.length" title="没有匹配的中转出站" description="中转出站引用其他节点的内部入站，不进入用户订阅。" />
        <div v-for="out in visibleRelays" :key="out.id" class="border border-gray-100 rounded-xl p-4 text-sm"><p class="font-semibold">{{ out.name }}</p><p class="mt-2">{{ nodeName(out.nodeId) }} → {{ nodeName(out.targetNodeId) }} · {{ out.protocol }}</p><p class="text-xs text-gray-500 mt-2">{{ out.enabled ? '草稿启用' : '草稿停用' }} · {{ healthLabel(out, out.probeInterval) }} · 失败时{{ failureNames[out.failurePolicy] }}</p></div>
      </section>
      <p class="text-xs text-gray-500">节点在线只表示控制通道状态，不能证明代理路径可用。草稿修改需要发布才能生效；此页面不显示未经测量的延迟、丢包率或流量数据。</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'; import { proxyAPI, choices, healthLabel, failureNames, type ProxyChain, type Outbound, type OutboundGroup } from '../../proxy/api'; import { groupAPI, type NodeGroup } from '../groupsApi'; import { type NodeView, type PublishedTopology } from '../api'; import { request, type Page } from '../../../app/apiClient'; import { formatTime } from '../../../app/auth'; import PageHeader from '../../../components/PageHeader.vue'; import ErrorState from '../../../components/ErrorState.vue'; import LoadingSkeleton from '../../../components/LoadingSkeleton.vue'; import EmptyState from '../../../components/EmptyState.vue'; import StatusBadge from '../../../components/StatusBadge.vue';
const published=ref<PublishedTopology[]>([]);
const observationNames={unknown:'尚无观察',disabled:'节点已禁用',fresh_control:'心跳新鲜且版本一致',stale_control:'心跳过期或节点未就绪',revision_mismatch:'节点上报版本与成功快照不一致'};
const snapshotNames={unpublished:'尚无成功快照',available:'成功快照可读取',unavailable:'成功快照无法读取'};
const dependencyNames={unpublished:'目标尚无成功版本',missing_node:'目标节点已不存在',disabled:'目标节点已禁用',unavailable:'目标成功快照无法读取',matched:'目标内部入站与协议依赖匹配',mismatch:'目标内部入站或协议依赖不匹配'};
const dependencyDetailNames:Record<string,string>={target_node_mismatch:'目标快照归属不一致',internal_inbound_unavailable:'协议或端口对应的内部入站未启用',internal_credential_missing:'内部凭据未匹配',wireguard_settings_missing:'WireGuard 中转设置不完整',wireguard_listener_unreachable:'WireGuard 入站仅本地可达或监听无效',wireguard_server_key_mismatch:'目标服务端公钥与源出站不一致',wireguard_source_identity_mismatch:'源中转身份与目标 peer 不一致',wireguard_endpoint_mismatch:'中转端点与出站地址或端口不一致',wireguard_peer_addresses_mismatch:'中转单栈或双栈地址与目标授权不一致',wireguard_address_pool_mismatch:'中转地址不在目标地址池内或占用接口地址'};
const nodes=ref<NodeView[]>([]); const groups=ref<NodeGroup[]>([]); const chains=ref<ProxyChain[]>([]); const outbounds=ref<Outbound[]>([]); const outboundGroups=ref<OutboundGroup[]>([]); const groupId=ref(''); const chainId=ref(''); const protocol=ref(''); const loading=ref(false); const error=ref('');
const protocols=computed(()=>Array.from(new Set([...outbounds.value.filter(out=>out.targetNodeId).map(out=>out.protocol),...published.value.flatMap(item=>item.relays.map(relay=>relay.protocol))])).sort());
const scopedNodes=computed(()=>groupId.value ? new Set(groups.value.find(group=>group.id===groupId.value)?.nodeIds || []) : new Set(nodes.value.map(node=>node.id)));
const visibleChains=computed(()=>chains.value.filter(chain=>(!chainId.value||chain.id===chainId.value)&&chain.nodeIds.some(id=>scopedNodes.value.has(id))&&(!protocol.value||chain.hops.some(hop=>hop.protocol===protocol.value))));
const chainNodes=computed(()=>new Set(visibleChains.value.flatMap(chain=>chain.nodeIds)));
const visibleNodes=computed(()=>nodes.value.filter(node=>scopedNodes.value.has(node.id)&&(!chainId.value||chainNodes.value.has(node.id))));
const visibleRelays=computed(()=>outbounds.value.filter(out=>out.targetNodeId&&(!protocol.value||out.protocol===protocol.value)&&(scopedNodes.value.has(out.nodeId)||scopedNodes.value.has(out.targetNodeId))&&(!chainId.value||visibleChains.value.some(chain=>chain.outboundIds.includes(out.id)))));
const visiblePublished=computed(()=>published.value.filter(item=>scopedNodes.value.has(item.nodeId)&&(!chainId.value||item.chains.some(chain=>chain.id===chainId.value))&&(!protocol.value||item.relays.some(relay=>relay.protocol===protocol.value))));
function chainName(id:string){return chains.value.find(chain=>chain.id===id)?.name||id;}
function nodeName(id:string){return nodes.value.find(node=>node.id===id)?.name||id;}
function targetName(id:string){return id==='direct'?'Direct':id==='block'?'Block':outbounds.value.find(item=>item.id===id)?.name||outboundGroups.value.find(item=>item.id===id)?.name||chains.value.find(item=>item.id===id)?.name||id;}
async function load(){if(loading.value)return;loading.value=true;try{const [ns,gs,cs,outs,ogs,ps]=await Promise.all([choices((p,s)=>request<Page<NodeView>>('/nodes?page='+p+'&pageSize='+s)),choices((p,s)=>groupAPI.list(p,s)),choices((p,s)=>proxyAPI.chains('',p,s)),choices((p,s)=>proxyAPI.outbounds('',p,s)),choices((p,s)=>proxyAPI.groups('',p,s)),choices((p,s)=>request<Page<PublishedTopology>>('/runtime-topology?page='+p+'&pageSize='+s))]);nodes.value=ns;groups.value=gs;chains.value=cs;outbounds.value=outs;outboundGroups.value=ogs;published.value=ps;error.value='';}catch(e){error.value=(e as Error).message;}finally{loading.value=false;}}
onMounted(()=>{load();window.addEventListener('proxy-node-status',load);});onUnmounted(()=>window.removeEventListener('proxy-node-status',load));
</script>
