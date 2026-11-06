<template>
  <div class="space-y-4 text-sm text-ios-secondary">
    <template v-if="hasSecurity(f.protocol)">
      <label v-if="hasTransport(f.protocol)" class="block">传输
        <select :value="s.transport || 'tcp'" @change="setTransport" class="form-input-base mt-2"><option v-for="choice in transports" :key="choice.value" :value="choice.value" :disabled="!!choice.reason" :title="choice.reason">{{ choice.label }}{{ choice.reason ? '（不可用）' : '' }}</option></select>
      </label>
      <label class="block">安全方式
        <select :value="s.security || 'none'" @change="setSecurity" :disabled="forcedTLS(f.protocol)" class="form-input-base mt-2"><option v-for="choice in securities.filter(c => !forcedTLS(f.protocol) || c.value === 'tls')" :key="choice.value" :value="choice.value" :disabled="!!choice.reason" :title="choice.reason">{{ choice.label }}{{ choice.reason ? '（不可用）' : '' }}</option></select>
      </label>
      <p v-if="f.protocol === 'vless'" class="text-xs">Reality / xhttp 按所选核心实际报告的组合开放；未报告时需升级 Agent 并等待能力上报。切换选项会保留输入，保存时只提交当前组合。</p>
      <details v-if="unavailableChoices.length" class="text-xs space-y-2">
        <summary class="cursor-pointer">不可用选项与原因</summary>
        <p v-for="choice in unavailableChoices" :key="choice.label">{{ choice.label }}：{{ choice.reason }}</p>
      </details>
      <div v-if="supportError" role="alert" class="space-y-2 text-xs text-ios-orange">
        <p>{{ supportError }}</p>
        <button v-if="f.protocol === 'vless'" type="button" class="ios-btn-secondary" @click="useLegacy">改用 TCP + None（保留已填字段）</button>
      </div>
      <label v-if="f.protocol === 'vless'" class="block">Flow
        <select :value="s.flow || ''" @change="s.flow = ($event.target as HTMLSelectElement).value" class="form-input-base mt-2"><option v-for="choice in flows" :key="choice.value" :value="choice.value" :disabled="!!choice.reason" :title="choice.reason">{{ choice.label }}{{ choice.reason ? '（不可用）' : '' }}</option></select>
      </label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'ws'" class="block">WebSocket 路径<input v-model="s.path" class="form-input-base mt-2" /></label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'grpc'" class="block">gRPC 服务名称<input v-model="s.service_name" class="form-input-base mt-2" /></label>
      <template v-if="f.protocol === 'vless' && s.transport === 'xhttp' && s.xhttp">
        <label class="block">xhttp Host（可选）<input v-model="s.xhttp.host" maxlength="253" placeholder="不带端口的主机名，与 TLS 服务器名称分开" class="form-input-base mt-2" /></label>
        <label class="block">xhttp 路径<input v-model="s.xhttp.path" maxlength="256" class="form-input-base mt-2" /></label>
        <label class="block">xhttp 模式<select v-model="s.xhttp.mode" class="form-input-base mt-2"><option v-if="!XHTTP_MODES.includes(s.xhttp.mode as typeof XHTTP_MODES[number])" :value="s.xhttp.mode" disabled>{{ s.xhttp.mode }}（不可用）</option><option v-for="mode in XHTTP_MODES" :key="mode">{{ mode }}</option></select></label>
        <p class="text-xs">当前支持 TCP / HTTP/2，不提供 HTTP/3 或高级调优参数。经 CDN 接入时，设备数量估计可能合并或分散客户端。</p>
      </template>
      <template v-if="f.protocol === 'vless' && s.security === 'reality' && s.reality">
        <label class="block">Reality 握手目标<input v-model="s.reality.handshake_target" placeholder="主机:端口（必须自行指定）" class="form-input-base mt-2" /></label>
        <p class="text-xs">节点会连接这个目标处理未通过认证的握手。面板检查格式，实际连接和配置由 Agent 校验。</p>
        <label class="block">服务器名称 / SNI（每行一个）<textarea :value="s.reality.server_names.join('\n')" @input="setRealityLines('server_names', $event)" rows="3" class="form-input-base mt-2 font-mono" /></label>
        <label class="block">Short ID（每行一个）<textarea :value="s.reality.short_ids.join('\n')" @input="setRealityLines('short_ids', $event)" rows="2" placeholder="2–16 位偶数长度十六进制" class="form-input-base mt-2 font-mono" /></label>
        <p class="text-xs">订阅使用保存列表中的第一个 SNI 与 Short ID。</p>
        <label class="block">Reality 公钥<input :value="s.reality.public_key || ''" readonly :placeholder="f.rotateRealityKey ? '保存时轮换，保存后返回新公钥' : '首次保存时由服务器生成'" class="form-input-base mt-2 font-mono text-xs" /></label>
        <button v-if="s.reality.public_key" type="button" class="ios-btn-secondary" @click="copyPublicKey">复制当前公钥</button>
        <p v-if="copyStatus" role="status" class="text-xs">{{ copyStatus }}</p>
        <p class="text-xs">密钥对在服务器首次保存 Reality 时生成。私钥由服务器加密保管，页面不读取或填写。普通保存保留当前密钥。</p>
        <label v-if="state.original?.hasRealityKey" class="block text-xs"><input v-model="f.rotateRealityKey" type="checkbox" /> 保存时轮换 Reality 密钥</label>
        <p v-if="f.rotateRealityKey" class="text-xs text-ios-orange">保存会生成新密钥对；发布成功后，客户端须更新订阅以取得新公钥，旧公钥将不能连接。</p>
      </template>
      <template v-if="forcedTLS(f.protocol) || s.security === 'tls'">
        <label class="block">服务器名称<input v-model="s.server_name" class="form-input-base mt-2" /></label>
        <label v-if="!state.original" class="block text-xs"><input v-model="f.internalRelay" @change="f.internalRelay && (f.subscriptionVisible = false)" type="checkbox" /> 内部中转（使用自动证书；也可在高级设置中修改）</label>
        <p v-if="f.internalRelay" class="text-xs">内部 TLS 证书由系统生成并固定信任，也可提供自有证书。</p>
        <label class="block">证书 PEM<textarea v-model="s.certificate_pem" rows="4" :placeholder="state.original?.settings.certificate_pem ? '留空保留现有证书' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" /></label>
        <label class="block">私钥 PEM<textarea v-model="s.private_key_pem" rows="4" autocomplete="off" :placeholder="state.original?.hasTlsKey ? '留空保留现有私钥' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" /></label>
        <label class="block">信任 CA PEM（可选）<textarea v-model="s.trusted_ca_pem" rows="3" class="form-input-base mt-2 font-mono text-xs" /></label>
      </template>
    </template>
      <label v-if="f.protocol === 'shadowsocks'" class="block">加密方式
        <select v-model="s.method" class="form-input-base mt-2"><option>chacha20-ietf-poly1305</option><option>aes-256-gcm</option><option>aes-128-gcm</option></select>
      </label>
      <template v-if="f.protocol === 'hysteria2'">
        <p class="text-xs">原生 QUIC 与 TLS。0 使用默认带宽。</p>
        <label v-for="field in bandwidthFields" :key="field.key" class="block">{{ field.label }} Mbps
          <input :value="s.quic?.[field.key]" @input="setBandwidth(field.key, $event)" type="number" min="0" max="100000" class="form-input-base mt-2" />
        </label>
      </template>
      <label v-if="f.protocol === 'tuic'" class="block">拥塞控制
        <select :value="s.quic?.congestion_control || ''" @change="setCongestion" class="form-input-base mt-2"><option value="">默认</option><option>cubic</option><option>new_reno</option><option>bbr</option></select>
      </label>
      <template v-if="f.protocol === 'wireguard'">
        <label class="block">隧道地址（每行一个 CIDR）<textarea v-model="state.addresses" @input="state.wireguardChanged = true" rows="3" class="form-input-base mt-2 font-mono" /></label>
        <label class="block">MTU<input v-model.number="state.mtu" @input="state.wireguardChanged = true" type="number" min="576" max="9000" class="form-input-base mt-2" /></label>
        <p class="text-xs">服务端密钥由系统生成。共享密钥一次只支持一个设备，不计入设备限制。内部中转需先发布目标服务器，再发布源服务器。</p>
      </template>
      <template v-if="f.protocol === 'forward' && s.forward">
        <p class="text-xs">端口转发不验证用户身份，不进入订阅；用户额度、设备限制与用户域名访问记录不适用。</p>
        <p v-if="relay" class="text-xs">这是 GOST 隧道的中继监听：目标、网络与限速由隧道资源管理，这里只能改名或启用/停用，保存时会沿用已存的隧道证书。</p>
        <template v-if="gost && !relay">
          <div class="space-y-2">
            <div class="flex items-center justify-between">
              <span class="text-sm text-ios-label">目标池（按顺序）</span>
              <button type="button" class="text-xs text-ios-blue" @click="addTarget">添加目标</button>
            </div>
            <div v-for="(target, index) in pool" :key="index" class="flex items-center gap-2">
              <input :value="target.host" @input="setTargetHost(index, $event)" class="form-input-base flex-1" placeholder="目标主机" />
              <input :value="target.port || ''" @input="setTargetPort(index, $event)" type="number" min="1" max="65535" class="form-input-base w-24" />
              <button v-if="pool.length > 1" type="button" class="text-xs text-ios-secondary" @click="removeTarget(index)">删除</button>
            </div>
          </div>
          <label class="block">负载策略
            <select :value="s.forward.strategy || 'round'" @change="setStrategy" class="form-input-base mt-2">
              <option value="round">轮询（round）</option>
              <option value="fifo">主备（fifo）</option>
            </select>
          </label>
          <div class="grid grid-cols-2 gap-2">
            <label class="block">失败次数
              <input :value="s.forward.max_fails ?? ''" @input="setMaxFails" type="number" min="1" max="100" placeholder="1" class="form-input-base mt-2" />
            </label>
            <label class="block">失败等待（秒）
              <input :value="s.forward.fail_timeout || ''" @input="setFailTimeout" placeholder="5s" class="form-input-base mt-2" />
            </label>
          </div>
          <label class="block">结束端口（可选，含）
            <input :value="s.forward.port_end ?? ''" @input="setPortEnd" type="number" min="1" max="65535" class="form-input-base mt-2" />
          </label>
          <div class="grid grid-cols-2 gap-2">
            <label v-for="field in rateFields" :key="field.key" class="block">{{ field.label }} KiB/s
              <input :value="rateKib(field.key)" @input="setRate(field.key, $event)" type="number" min="0" class="form-input-base mt-2" placeholder="0（不限）" />
            </label>
          </div>
          <label class="block">TCP 连接上限（可选）
            <input :value="s.forward.max_connections ?? ''" @input="setMaxConnections" type="number" min="0" max="100000" class="form-input-base mt-2" placeholder="0（不限）" />
          </label>
          <p class="text-xs">每个监听端口各自生效：范围中的每个端口都有独立的限速与连接上限，不会共享额度。主备（fifo）切换会让一个请求失败一次；超过连接上限的连接会被接受后立即关闭，而不是拒绝握手。发布配置会重启该服务器的全部核心，GOST 会话会短暂中断。</p>
          <p v-if="expanded > 1" class="text-xs">当前范围将展开为 {{ expanded }} 个服务（{{ portCount }} 个端口 × {{ networkCount }} 个协议），全部计入该节点的节点总量，不计入用户额度。</p>
        </template>
        <template v-else-if="!relay">
          <label class="block">目标主机<input v-model="s.forward.host" class="form-input-base mt-2" /></label>
          <label class="block">目标端口<input v-model.number="s.forward.port" type="number" min="1" max="65535" class="form-input-base mt-2" /></label>
        </template>
        <label v-if="!relay" class="block">网络<select v-model="s.forward.network" class="form-input-base mt-2"><option value="tcp">TCP</option><option value="udp">UDP</option><option value="tcp,udp">TCP + UDP</option></select></label>
        <p v-if="!gost && !relay" class="text-xs">目标池、端口范围与限速只支持 GOST 核心（设置契约 2）。</p>
      </template>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import type { NodeView } from '../../../nodes/api';
import { changeSecurity, changeTransport, flowChoices, internalRelaySettingsReason, securityChoices, serverSettingsReason, transportChoices, XHTTP_MODES, forcedTLS, forwardListenPorts, forwardPool, hasSecurity, hasTransport, relayMode, type EditorState } from './nodeEditorModel';
const props = defineProps<{ state: EditorState; gost?: boolean; server?: NodeView }>();
const f = computed(() => props.state.form), s = computed(() => props.state.form.settings);
const transports = computed(() => transportChoices(props.server, f.value.core || '', f.value.protocol, s.value));
const securities = computed(() => securityChoices(props.server, f.value.core || '', f.value.protocol, s.value));
const flows = computed(() => flowChoices(props.server, f.value.core || '', f.value.protocol, s.value));
const supportError = computed(() => internalRelaySettingsReason(f.value.internalRelay, s.value) || serverSettingsReason(props.server, f.value.core || '', f.value.protocol, s.value));
const unavailableChoices = computed(() => [...transports.value, ...securities.value, ...(f.value.protocol === 'vless' ? flows.value : [])].filter(c => c.reason));
function setTransport(event: Event) { changeTransport(props.state, (event.target as HTMLSelectElement).value); }
function setSecurity(event: Event) { changeSecurity(props.state, (event.target as HTMLSelectElement).value); }
function useLegacy() { changeTransport(props.state, 'tcp'); changeSecurity(props.state, 'none'); s.value.flow = ''; }
function setRealityLines(key: 'server_names' | 'short_ids', event: Event) { s.value.reality![key] = (event.target as HTMLTextAreaElement).value.split(/\r?\n/); }
const copyStatus = ref('');
async function copyPublicKey() {
  try { await navigator.clipboard.writeText(s.value.reality?.public_key || ''); copyStatus.value = '公钥已复制'; }
  catch { copyStatus.value = '未能复制，请在公钥框中选择并手动复制'; }
}
const gost = computed(() => props.gost === true && f.value.protocol === 'forward');
// A GOST tunnel's exit listener: no target, network or limit of its own to edit.
const relay = computed(() => relayMode(s.value));
const bandwidthFields = [{ key: 'up_mbps', label: '上传带宽' }, { key: 'down_mbps', label: '下载带宽' }] as const;
const rateFields = [{ key: 'bandwidth_in_bps', label: '客户端 → 目标' }, { key: 'bandwidth_out_bps', label: '目标 → 客户端' }] as const;
function setBandwidth(key: 'up_mbps' | 'down_mbps', event: Event) {
  const value = (event.target as HTMLInputElement).value;
  (s.value.quic ||= {})[key] = value === '' ? undefined : Number(value);
}
function setCongestion(event: Event) { (s.value.quic ||= {}).congestion_control = (event.target as HTMLSelectElement).value; }

// The pool is edited in place. A legacy single-target forward becomes an explicit
// pool only when the Owner touches a row, and collapsing back to one target
// restores the legacy shape, so an old row is never rewritten needlessly.
const pool = computed(() => forwardPool(s.value.forward));
function editablePool() {
  const forward = s.value.forward!;
  if (!forward.targets?.length) {
    forward.targets = forwardPool(forward);
    forward.host = '';
    forward.port = 0;
  }
  return forward.targets;
}
function setTargetHost(index: number, event: Event) { editablePool()[index].host = (event.target as HTMLInputElement).value; }
function setTargetPort(index: number, event: Event) {
  const value = (event.target as HTMLInputElement).value;
  editablePool()[index].port = value === '' ? 0 : Number(value);
}
function addTarget() { editablePool().push({ host: '', port: 443 }); }
function removeTarget(index: number) {
  const forward = s.value.forward!;
  const targets = editablePool();
  targets.splice(index, 1);
  if (targets.length === 1) {
    forward.host = targets[0].host;
    forward.port = targets[0].port;
    delete forward.targets;
  }
}
function setStrategy(event: Event) { s.value.forward!.strategy = (event.target as HTMLSelectElement).value as 'round' | 'fifo'; }
function setOptionalNumber(key: 'max_fails' | 'port_end' | 'max_connections', event: Event) {
  const value = (event.target as HTMLInputElement).value;
  const forward = s.value.forward!;
  if (value === '') delete forward[key];
  else forward[key] = Number(value);
}
const setMaxFails = (event: Event) => setOptionalNumber('max_fails', event);
const setPortEnd = (event: Event) => setOptionalNumber('port_end', event);
const setMaxConnections = (event: Event) => setOptionalNumber('max_connections', event);
function setFailTimeout(event: Event) {
  const value = (event.target as HTMLInputElement).value.trim();
  const forward = s.value.forward!;
  if (value === '') delete forward.fail_timeout;
  else forward.fail_timeout = value;
}
// Rates are entered in KiB/s, which is the smallest unit the panel accepts, and
// stored as the bytes per second the Agent renders.
function rateKib(key: 'bandwidth_in_bps' | 'bandwidth_out_bps'): number | '' {
  const bytes = s.value.forward?.[key];
  return bytes === undefined ? '' : bytes / 1024;
}
function setRate(key: 'bandwidth_in_bps' | 'bandwidth_out_bps', event: Event) {
  const value = (event.target as HTMLInputElement).value;
  const forward = s.value.forward!;
  if (value === '') delete forward[key];
  else forward[key] = Math.round(Number(value)) * 1024;
}
const portCount = computed(() => forwardListenPorts(f.value.port, s.value).length);
const networkCount = computed(() => (s.value.forward?.network === 'tcp,udp' ? 2 : 1));
const expanded = computed(() => portCount.value * networkCount.value);
</script>
