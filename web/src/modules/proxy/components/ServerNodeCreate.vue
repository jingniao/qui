<template>
  <FormDrawer v-model="drawer" :title="'新建节点'" :loading="saving" @confirm="save" confirm-text="保存草稿">
    <div class="space-y-4 text-sm">
      <label class="block">
        名称
        <input v-model="form.name" maxlength="40" class="form-input-base mt-2" />
      </label>
      <label class="block">
        服务器
        <select v-model="form.nodeId" :disabled="!!editing" class="form-input-base mt-2">
          <option value="">请选择</option>
          <option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }}</option>
        </select>
      </label>
      <label class="block">
        协议
        <select v-model="form.protocol" :disabled="!!editing" @change="protocolChanged" class="form-input-base mt-2">
          <option v-for="protocol in protocolOptions.inbound" :key="protocol">{{ protocol }}</option>
        </select>
      </label>
      <p v-if="form.protocol==='wireguard'" class="text-xs text-ios-secondary">WireGuard使用原生加密和UDP接入。只有服务器报告支持WireGuard入站时才能发布；保存草稿不会立即启用。</p>
      <p v-if="form.protocol==='wireguard' && form.nodeId" class="text-xs" :class="nodes.find(node=>node.id===form.nodeId)?.capabilities?.protocols?.some(protocol=>protocol.protocol==='wireguard' && protocol.inbound) ? 'text-ios-green' : 'text-ios-orange'">{{ nodes.find(node=>node.id===form.nodeId)?.capabilities?.protocols?.some(protocol=>protocol.protocol==='wireguard' && protocol.inbound) ? '所选服务器已报告支持WireGuard入站，发布时仍会校验当前能力。' : '所选服务器尚未报告支持WireGuard入站，请先核对服务器与核心能力。' }}</p>
      <label class="block">
        监听地址
        <input v-model="form.listen" class="form-input-base mt-2" />
      </label>
      <label class="block">
        端口
        <input v-model.number="form.port" type="number" min="1" max="65535" class="form-input-base mt-2" />
      </label>
      <label class="block">
        <input v-model="form.internalRelay" :disabled="!!editing || form.protocol === 'forward'" type="checkbox" />
        内部中转入口
      </label>
      <label v-if="!form.internalRelay && form.protocol !== 'forward'" class="block">
        <input v-model="form.subscriptionVisible" type="checkbox" />
        显示在授权用户订阅中
      </label>
      <label class="block">
        默认出站
        <select v-model="form.defaultOutbound" class="form-input-base mt-2">
          <option value="direct">Direct</option>
          <option value="block">Block</option>
          <option v-for="outbound in outbounds.filter(item => item.nodeId === form.nodeId && item.enabled)" :key="outbound.id" :value="outbound.id">{{ outbound.name }}</option>
          <option v-for="group in outboundGroups.filter(item => item.nodeId === form.nodeId && item.enabled)" :key="group.id" :value="'group:' + group.id">出口组 · {{ group.name }}</option>
          <option v-for="chain in chains.filter(item => item.nodeIds[0] === form.nodeId && item.enabled)" :key="chain.id" :value="'chain:' + chain.id">代理链 · {{ chain.name }}</option>
        </select>
      </label>
      <div v-if="!form.internalRelay && form.protocol !== 'forward'" class="space-y-1.5">
        <div class="flex items-center justify-between">
          <label class="text-xs font-medium text-ios-secondary">允许用户</label>
          <span class="text-xs text-ios-tertiary">不勾选表示组内全部授权用户</span>
        </div>
        <div class="rounded-2xl p-3 bg-ios-bg border border-black/[0.06]">
          <div v-if="users.length" class="space-y-1.5 max-h-40 overflow-y-auto pr-1 scrollbar-thin">
            <label v-for="user in users" :key="user.id" :class="['flex items-center justify-between p-2 rounded-xl text-xs cursor-pointer select-none transition-all', form.allowedUserIds.includes(user.id) ? 'bg-white text-ios-label font-medium shadow-2xs border border-ios-blue/30' : 'bg-white/60 text-ios-secondary hover:bg-white hover:text-ios-label border border-transparent']">
              <div class="flex items-center gap-2.5 min-w-0 flex-1 truncate">
                <div :class="['w-4 h-4 rounded-md flex items-center justify-center transition-all shrink-0', form.allowedUserIds.includes(user.id) ? 'bg-ios-blue text-white' : 'border border-ios-border bg-white']">
                  <svg v-if="form.allowedUserIds.includes(user.id)" class="w-3 h-3" viewBox="0 0 20 20" fill="currentColor">
                    <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                  </svg>
                </div>
                <span class="truncate">{{ user.username }}</span>
              </div>
              <input v-model="form.allowedUserIds" type="checkbox" :value="user.id" class="sr-only" />
            </label>
          </div>
          <p v-else class="text-xs text-ios-tertiary py-1">暂无用户</p>
        </div>
      </div>
      <div v-if="form.protocol === 'forward' && form.settings.forward" class="space-y-2">
        <label class="block">
          目标主机
          <input v-model="form.settings.forward.host" class="form-input-base" />
        </label>
        <label class="block">
          目标端口
          <input v-model.number="form.settings.forward.port" type="number" min="1" max="65535" class="form-input-base" />
        </label>
        <label class="block">
          网络
          <select v-model="form.settings.forward.network" class="form-input-base">
            <option value="tcp">TCP</option>
            <option value="udp">UDP</option>
            <option value="tcp,udp">TCP + UDP</option>
          </select>
        </label>
        <p class="text-xs text-ios-secondary">端口转发不验证用户身份，不进入订阅；用户额度、设备限制与用户域名访问记录不适用。</p>
      </div>
      <template v-if="form.protocol !== 'wireguard' && form.protocol !== 'forward'">
        <label v-if="!['hysteria2','tuic'].includes(form.protocol)" class="block">
          传输
          <select v-model="form.settings.transport" class="form-input-base mt-2">
            <option value="tcp">TCP</option>
            <option value="ws">WebSocket</option>
            <option value="grpc">gRPC</option>
          </select>
        </label>
        <label class="block">
          安全方式
          <select v-model="form.settings.security" class="form-input-base mt-2">
            <option v-if="!['trojan','hysteria2','tuic'].includes(form.protocol)" value="none">None</option>
            <option value="tls">TLS</option>
          </select>
        </label>
        <label v-if="form.settings.transport === 'ws'" class="block">
          WebSocket 路径
          <input v-model="form.settings.path" class="form-input-base mt-2" />
        </label>
        <label v-if="form.settings.transport === 'grpc'" class="block">
          gRPC 服务名称
          <input v-model="form.settings.service_name" class="form-input-base mt-2" />
        </label>
        <template v-if="form.settings.security === 'tls'">
          <label class="block">
            服务器名称
            <input v-model="form.settings.server_name" class="form-input-base mt-2" />
          </label>
          <p v-if="form.internalRelay" class="text-xs text-ios-secondary">内部 TLS 证书由系统生成并固定信任。也可提供自有证书。</p>
          <label class="block">
            证书 PEM
            <textarea v-model="form.settings.certificate_pem" rows="4" class="form-input-base mt-2 font-mono text-xs"></textarea>
          </label>
          <label class="block">
            私钥 PEM
            <textarea v-model="form.settings.private_key_pem" rows="4" :placeholder="editing?.hasTlsKey ? '留空保留现有私钥' : ''" class="form-input-base mt-2 font-mono text-xs"></textarea>
          </label>
        </template>
        <template v-if="form.settings.quic">
          <p class="text-xs text-ios-secondary">使用原生 QUIC 与 TLS，需要支持该协议的 sing-box 服务器。</p>
          <template v-if="form.protocol === 'hysteria2'">
            <label class="block">
              上传带宽 Mbps（0 使用默认）
              <input v-model.number="form.settings.quic.up_mbps" type="number" min="0" max="100000" class="form-input-base mt-2" />
            </label>
            <label class="block">
              下载带宽 Mbps（0 使用默认）
              <input v-model.number="form.settings.quic.down_mbps" type="number" min="0" max="100000" class="form-input-base mt-2" />
            </label>
          </template>
          <label v-if="form.protocol === 'tuic'" class="block">
            拥塞控制
            <select v-model="form.settings.quic.congestion_control" class="form-input-base mt-2">
              <option value="">默认</option>
              <option value="cubic">cubic</option>
              <option value="new_reno">new_reno</option>
              <option value="bbr">bbr</option>
            </select>
          </label>
        </template>
        <label v-if="form.protocol === 'shadowsocks'" class="block">
          加密方式
          <select v-model="form.settings.method" class="form-input-base mt-2">
            <option>chacha20-ietf-poly1305</option>
            <option>aes-256-gcm</option>
            <option>aes-128-gcm</option>
          </select>
        </label>
      </template>
      <template v-else-if="form.protocol === 'wireguard'">
        <label class="block">
          隧道地址
          <textarea v-model="wireguardAddresses" rows="3" placeholder="10.77.0.1/24&#10;fd77::1/64" class="form-input-base mt-2 font-mono"></textarea>
        </label>
        <label class="block">
          MTU
          <input v-model.number="wireguardMTU" type="number" min="576" max="9000" class="form-input-base mt-2" />
        </label>
        <p class="text-xs text-ios-secondary">每行一个接口地址，每个地址族的首个子网用于自动分配。服务端密钥由系统生成；用户公钥按授权设备展开。</p>
        <p v-if="form.internalRelay" class="text-xs text-ios-secondary">保存时同步关联中转的地址、端口和公钥草稿。请生成相关服务器配置版本，并先发布目标服务器，再发布源服务器。</p>
      </template>
      <label class="block">
        <input v-model="form.enabled" type="checkbox" />
        启用
      </label>
      <p class="text-xs text-ios-secondary">保存后不会立即修改服务器。请生成配置版本，并先发布中转目标，再发布入口服务器。</p>
    </div>
  </FormDrawer>
</template>
<script setup lang="ts">
import { ref, reactive, watch, onUnmounted } from 'vue';
import { proxyAPI, protocolOptions, loadProtocolSupport, choices, type Inbound, type InboundInput, type Outbound, type OutboundGroup, type ProxyChain } from '../api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { userAPI, type ProxyUser } from '../../users/api';
import { isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
const props = defineProps<{ serverId: string }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const drawer = ref(true), saving = ref(false), editing = ref<Inbound | null>(null);
const nodes = ref<NodeView[]>([]), users = ref<ProxyUser[]>([]), outbounds = ref<Outbound[]>([]), outboundGroups = ref<OutboundGroup[]>([]), chains = ref<ProxyChain[]>([]);
const wireguardAddresses = ref('10.77.0.1/24'), wireguardMTU = ref(1420);
const form = reactive<InboundInput>({ name: '', nodeId: props.serverId, protocol: 'vless', listen: '0.0.0.0', port: 443, enabled: true, subscriptionVisible: true, internalRelay: false, defaultOutbound: 'direct', allowedUserIds: [], settings: { transport: 'tcp', security: 'none' } });
function protocolChanged() {
  if (form.protocol === 'forward') { form.settings = { forward: { host: '', port: 443, network: 'tcp' } }; form.subscriptionVisible = false; form.internalRelay = false; form.allowedUserIds = []; return; }
  form.settings = form.protocol === 'wireguard' ? {} : { transport: ['hysteria2', 'tuic'].includes(form.protocol) ? 'quic' : 'tcp', quic: ['hysteria2', 'tuic'].includes(form.protocol) ? {} : undefined, security: ['trojan', 'hysteria2', 'tuic'].includes(form.protocol) ? 'tls' : 'none', method: form.protocol === 'shadowsocks' ? 'chacha20-ietf-poly1305' : undefined };
}
async function save() {
  if (saving.value || !isOwner.value) return;
  saving.value = true;
  try {
    if (!form.nodeId || !nodes.value.some(node => node.id === form.nodeId)) throw new Error('请选择可用服务器');
    const value = JSON.parse(JSON.stringify(form)) as InboundInput;
    if (value.internalRelay) value.subscriptionVisible = false;
    if (value.protocol === 'wireguard') value.settings = { wireguard: { secret_key: '', addresses: wireguardAddresses.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean), peers: [], mtu: wireguardMTU.value } };
    await proxyAPI.saveInbound(null, value); showToast('节点草稿已保存，请在服务器详情发布配置'); emit('saved'); drawer.value = false;
  } catch (e) { showToast((e as Error).message, 'error'); }
  finally { saving.value = false; }
}
let generation = 0;
onUnmounted(() => { generation++; });
watch(() => props.serverId, async id => {
  const current = ++generation;
  form.nodeId = id;
  try {
    await loadProtocolSupport();
    const [servers, people, outs, groups, routes] = await Promise.all([choices((page) => nodeAPI.list(page, '')), choices((page, size) => userAPI.list(page, '', size)), choices((page, size) => proxyAPI.outbounds('', page, size)), choices((page, size) => proxyAPI.groups('', page, size)), choices((page, size) => proxyAPI.chains('', page, size))]);
    if (current !== generation) return;
    nodes.value = servers.filter(node => node.enabled && !node.deletedAt); users.value = people; outbounds.value = outs; outboundGroups.value = groups; chains.value = routes;
    if (!nodes.value.some(node => node.id === id)) showToast('预选服务器不可用，请选择其他服务器', 'error');
  } catch (e) { if (current === generation) showToast((e as Error).message, 'error'); }
}, { immediate: true });
watch(drawer, open => { if (!open) { form.settings.private_key_pem = ''; emit('close'); } });
</script>
