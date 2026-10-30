<template>
  <div class="space-y-4 text-sm text-ios-secondary">
    <template v-if="step === 3 && hasSecurity(f.protocol)">
      <label v-if="hasTransport(f.protocol)" class="block">传输
        <select v-model="s.transport" class="form-input-base mt-2"><option value="tcp">TCP</option><option value="ws">WebSocket</option><option value="grpc">gRPC</option></select>
      </label>
      <label class="block">安全方式
        <select v-model="s.security" :disabled="forcedTLS(f.protocol)" class="form-input-base mt-2"><option v-if="!forcedTLS(f.protocol)" value="none">None</option><option value="tls">TLS</option></select>
      </label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'ws'" class="block">WebSocket 路径<input v-model="s.path" class="form-input-base mt-2" /></label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'grpc'" class="block">gRPC 服务名称<input v-model="s.service_name" class="form-input-base mt-2" /></label>
      <template v-if="forcedTLS(f.protocol) || s.security === 'tls'">
        <label class="block">服务器名称<input v-model="s.server_name" class="form-input-base mt-2" /></label>
        <label v-if="!state.original" class="block text-xs"><input v-model="f.internalRelay" @change="f.internalRelay && (f.subscriptionVisible = false)" type="checkbox" /> 内部中转（使用自动证书；也可在高级设置中修改）</label>
        <p v-if="f.internalRelay" class="text-xs">内部 TLS 证书由系统生成并固定信任，也可提供自有证书。</p>
        <label class="block">证书 PEM<textarea v-model="s.certificate_pem" rows="4" :placeholder="state.original?.settings.certificate_pem ? '留空保留现有证书' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" /></label>
        <label class="block">私钥 PEM<textarea v-model="s.private_key_pem" rows="4" autocomplete="off" :placeholder="state.original?.hasTlsKey ? '留空保留现有私钥' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" /></label>
        <label class="block">信任 CA PEM（可选）<textarea v-model="s.trusted_ca_pem" rows="3" class="form-input-base mt-2 font-mono text-xs" /></label>
      </template>
    </template>
    <template v-if="step === 4">
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
        <label class="block">目标主机<input v-model="s.forward.host" class="form-input-base mt-2" /></label>
        <label class="block">目标端口<input v-model.number="s.forward.port" type="number" min="1" max="65535" class="form-input-base mt-2" /></label>
        <label class="block">网络<select v-model="s.forward.network" class="form-input-base mt-2"><option value="tcp">TCP</option><option value="udp">UDP</option><option value="tcp,udp">TCP + UDP</option></select></label>
        <p class="text-xs">端口转发不验证用户身份，不进入订阅；用户额度、设备限制与用户域名访问记录不适用。</p>
      </template>
      <p v-if="!['shadowsocks', 'wireguard', 'forward', 'hysteria2', 'tuic'].includes(f.protocol)" class="text-xs">此协议没有额外参数，可继续。</p>
    </template>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { forcedTLS, hasSecurity, hasTransport, type EditorState } from './nodeEditorModel';
const props = defineProps<{ state: EditorState; step: number }>();
const f = computed(() => props.state.form), s = computed(() => props.state.form.settings);
const bandwidthFields = [{ key: 'up_mbps', label: '上传带宽' }, { key: 'down_mbps', label: '下载带宽' }] as const;
function setBandwidth(key: 'up_mbps' | 'down_mbps', event: Event) {
  const value = (event.target as HTMLInputElement).value;
  (s.value.quic ||= {})[key] = value === '' ? undefined : Number(value);
}
function setCongestion(event: Event) { (s.value.quic ||= {}).congestion_control = (event.target as HTMLSelectElement).value; }
</script>
