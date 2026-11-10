<template>
  <div class="space-y-4 text-sm text-ios-secondary">
    <template v-if="hasSecurity(f.protocol)">
      <label v-if="hasTransport(f.protocol)" class="block">传输
        <select :value="s.transport || 'tcp'" @change="setTransport" class="form-input-base mt-2"><option v-for="choice in transports" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select>
        <span class="block text-xs mt-1">客户端与节点之间的封装方式；不确定时保持 TCP。选 xhttp 时要求安全方式为 Reality 或 TLS。</span>
      </label>
      <label class="block">安全方式
        <select :value="s.security || 'none'" @change="setSecurity" :disabled="forcedTLS(f.protocol)" class="form-input-base mt-2"><option v-for="choice in securities" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select>
        <span class="block text-xs mt-1">{{ forcedTLS(f.protocol) ? '该协议只能用 TLS：请填写证书与私钥，或勾选内部中转使用自动证书。' : 'None 为明文；TLS 使用自有证书；Reality 由节点伪装成外部 HTTPS 站点，不需要证书。' }}</span>
      </label>
      <p v-if="securityNote" role="status" class="text-xs text-ios-blue">{{ securityNote }}</p>
      <p v-if="f.protocol === 'vless'" class="text-xs">Reality / xhttp 只列出所选核心实际报告的组合；未报告时需升级 Agent 并等待能力上报。切换选项会保留输入，保存时只提交当前组合。</p>
      <div v-if="supportError" role="alert" class="space-y-2 text-xs text-ios-orange">
        <p>{{ supportError }}</p>
        <button v-if="f.protocol === 'vless'" type="button" class="ios-btn-secondary" @click="useLegacy">改用 TCP + None（保留已填字段）</button>
      </div>
      <label v-if="f.protocol === 'vless'" class="block">Flow
        <select :value="s.flow || ''" @change="setFlow" class="form-input-base mt-2"><option v-for="choice in flows" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select>
        <span class="block text-xs mt-1">只有 VLESS + TCP + Reality/TLS 可以选 XTLS Vision（Reality TCP 推荐开启）；其他组合请选择“无”。</span>
      </label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'ws'" class="block">WebSocket 路径
        <input v-model="s.path" placeholder="例如 /ws" class="form-input-base mt-2 font-mono" />
        <span class="block text-xs mt-1">客户端必须填同一个路径；留空时客户端使用默认值。</span>
      </label>
      <label v-if="hasTransport(f.protocol) && s.transport === 'grpc'" class="block">gRPC 服务名称
        <input v-model="s.service_name" placeholder="例如 grpc" class="form-input-base mt-2 font-mono" />
        <span class="block text-xs mt-1">客户端必须填同一个服务名称；留空时客户端使用默认值。</span>
      </label>
      <template v-if="f.protocol === 'vless' && s.transport === 'xhttp' && s.xhttp">
        <label class="block">xhttp Host（可选）
          <input v-model="s.xhttp.host" maxlength="253" placeholder="例如 cdn.example.com；不确定就留空" class="form-input-base mt-2 font-mono" />
          <span class="block text-xs mt-1">只在客户端经 CDN 或其他域名接入时填写该域名（不带端口、路径或 URL）；直接连节点时留空。</span>
        </label>
        <label class="block">xhttp 路径
          <span class="flex items-start gap-2 mt-2">
            <input v-model="s.xhttp.path" maxlength="256" placeholder="例如 /8f3k2ab" class="form-input-base min-w-0 flex-1 font-mono" />
            <button type="button" class="ios-btn-secondary shrink-0" @click="setRandomPath">随机路径</button>
          </span>
          <span class="block text-xs mt-1">以 / 开头，最多 256 个字符，不能含控制字符；客户端的 path 必须与它完全一致。</span>
        </label>
        <label class="block">xhttp 模式
          <select v-model="s.xhttp.mode" class="form-input-base mt-2"><option v-if="!XHTTP_MODES.includes(s.xhttp.mode as typeof XHTTP_MODES[number])" :value="s.xhttp.mode">{{ s.xhttp.mode }}（现有值）</option><option v-for="mode in XHTTP_MODES" :key="mode" :value="mode">{{ mode }}</option></select>
          <span class="block text-xs mt-1">auto 为推荐值，按连接自动选择上下行模式；packet-up / stream-up / stream-one 只在排障或客户端明确要求时使用。</span>
        </label>
        <p class="text-xs">当前支持 TCP / HTTP/2，不提供 HTTP/3 或高级调优参数。经 CDN 接入时，设备数量估计可能合并或分散客户端。</p>
      </template>
      <template v-if="f.protocol === 'vless' && s.security === 'reality' && s.reality">
        <label class="block">Reality 握手目标
          <input v-model="s.reality.handshake_target" placeholder="例如 www.amd.com:443" class="form-input-base mt-2 font-mono" />
          <span class="block text-xs mt-1">填一个支持 TLS 1.3 与 HTTP/2 的境外常见 HTTPS 站点（不要用自己的域名，越靠近节点越好）；节点只把未通过认证的握手交给它。必须自行指定，不会自动选择。</span>
        </label>
        <label class="block">常用目标（可选）
          <select :value="''" class="form-input-base mt-2 font-mono" @change="pickPreset"><option value="">选择后填入上面的握手目标</option><option v-for="preset in REALITY_HANDSHAKE_PRESETS" :key="preset" :value="preset">{{ preset }}</option></select>
        </label>
        <label class="block">服务器名称 / SNI（每行一个）
          <span class="flex items-start gap-2 mt-2">
            <textarea :value="s.reality.server_names.join('\n')" @input="setRealityLines('server_names', $event)" rows="3" placeholder="例如 www.amd.com" class="form-input-base min-w-0 flex-1 font-mono" />
            <button v-if="targetSni" type="button" class="ios-btn-secondary shrink-0" @click="fillSni">从握手目标填入</button>
          </span>
          <span class="block text-xs mt-1">对外声明的域名，必须是 DNS 主机名（不能是 IP、通配符或带端口），通常与握手目标一致；订阅使用第一个。</span>
          <span v-if="sniStatus" role="status" class="block text-xs text-ios-blue">{{ sniStatus }}</span>
        </label>
        <label class="block">Short ID（每行一个）
          <span class="flex items-start gap-2 mt-2">
            <textarea :value="s.reality.short_ids.join('\n')" @input="setRealityLines('short_ids', $event)" rows="2" placeholder="例如 8f3a5b7c9d1e2f40（2–16 位偶数长度十六进制）" class="form-input-base min-w-0 flex-1 font-mono" />
            <button v-if="shortIdCount < 16" type="button" class="ios-btn-secondary shrink-0" @click="addShortId">生成</button>
          </span>
          <span class="block text-xs mt-1">客户端与节点约定的短标识，通常生成一个就够，最多 16 个；订阅使用第一个。</span>
        </label>
        <label class="block">Reality 公钥
          <input :value="s.reality.public_key || ''" readonly :placeholder="f.rotateRealityKey ? '保存时轮换，保存后返回新公钥' : '首次保存时由服务器生成'" class="form-input-base mt-2 font-mono" />
          <span class="block text-xs mt-1">由服务器在首次保存 Reality 时生成，这里不需要填写；保存后可在下方复制给客户端。</span>
        </label>
        <button v-if="s.reality.public_key" type="button" class="ios-btn-secondary" @click="copyPublicKey">复制当前公钥</button>
        <p v-if="copyStatus" role="status" class="text-xs">{{ copyStatus }}</p>
        <p class="text-xs">私钥由服务器加密保管，页面不读取也不需要填写；普通保存保留当前密钥。</p>
        <label v-if="state.original?.hasRealityKey" class="block text-xs"><input v-model="f.rotateRealityKey" type="checkbox" /> 保存时轮换 Reality 密钥</label>
        <p v-if="f.rotateRealityKey" class="text-xs text-ios-orange">保存会生成新密钥对；发布成功后，客户端须更新订阅以取得新公钥，旧公钥将不能连接。</p>
      </template>
      <template v-if="tlsShown">
        <div class="block">域名（服务器名称）
          <div class="flex items-center gap-2 mt-2">
            <input v-model="s.server_name" @blur="scheduleLookup" placeholder="例如 node.example.com（证书上的域名）" class="form-input-base font-mono flex-1" />
            <button type="button" class="ios-btn-secondary" :disabled="lookupBusy" @click="findCertificates">查找证书</button>
          </div>
          <span class="block text-xs mt-1">客户端用它校验证书；须是证书覆盖的域名。留空时客户端使用连接地址，此时证书必须覆盖连接地址。</span>
          <span v-if="quicTLS" class="block text-xs mt-1">域名须解析到这台服务器；使用 Cloudflare 时请设为“仅 DNS”（灰云），代理模式不转发 UDP。</span>
        </div>
        <div v-if="lookupBusy || lookupError || choices.length || requestStatus || (matches && !choices.length)" class="space-y-1">
          <p v-if="lookupBusy" class="text-xs">正在查找覆盖该域名的证书…</p>
          <p v-if="lookupError" role="alert" class="text-xs text-ios-red">{{ lookupError }}</p>
          <label v-for="(choice, index) in choices" :key="index" class="block text-xs">
            <input type="radio" name="certificate-choice" :checked="choiceKey === String(index)" @change="chooseCertificate(index)" /> {{ choice.label }}
          </label>
          <p v-if="!lookupBusy && matches && !choices.length && !lookupError" class="text-xs">没有找到覆盖该域名的证书；可在下方选择其他来源。</p>
          <p v-if="requestStatus" role="status" class="text-xs">{{ requestStatus }}</p>
        </div>
        <label v-if="!state.original" class="block text-xs"><input v-model="f.internalRelay" @change="f.internalRelay && (f.subscriptionVisible = false)" type="checkbox" /> 内部中转（使用自动证书；也可在高级设置中修改）</label>
        <p v-if="f.internalRelay" class="text-xs">内部 TLS 证书由系统生成并固定信任，也可提供自有证书。</p>
        <label v-else class="block">证书来源
          <select :value="source" @change="setSource" class="form-input-base mt-2"><option v-for="choice in sources" :key="choice.value" :value="choice.value">{{ choice.label }}</option></select>
        </label>
        <template v-if="source === 'self_signed' && !f.internalRelay">
          <p class="text-xs">面板为「{{ address || '连接地址' }}」生成自签名证书（10 年），客户端通过订阅固定信任它；sing-box 与 mihomo 订阅可用，通用 URI 订阅不包含 Hysteria2/TUIC 的自签名节点。</p>
          <p v-if="!address" role="alert" class="text-xs text-ios-orange">{{ SELF_SIGNED_REQUIRES_ADDRESS }}<router-link v-if="server" :to="`/nodes/${server.id}?tab=settings`" class="ml-2 text-ios-blue font-medium hover:underline">去设置连接地址</router-link></p>
          <label v-if="state.original?.tlsSource === 'self_signed'" class="block text-xs"><input v-model="f.regenerateCertificate" type="checkbox" /> 保存时重新生成证书</label>
          <p v-if="f.regenerateCertificate" class="text-xs text-ios-orange">保存会签发新证书；发布成功后客户端须更新订阅以取得新证书，旧证书将不能连接。</p>
        </template>
        <template v-else-if="source === 'acme' && !f.internalRelay">
          <label class="block">证书
            <select v-model="f.certificateId" class="form-input-base mt-2"><option value="" disabled>请选择证书</option><option v-for="item in acmeCertificates" :key="item.id" :value="item.id">{{ item.domains.join('、') }}（{{ formatTime(item.notAfter) }} 到期）</option></select>
          </label>
          <p class="text-xs">没有合适的证书？<router-link to="/system/settings?tab=base" class="text-ios-blue font-medium hover:underline">在「证书与 DNS」申请新证书</router-link></p>
          <p class="text-xs">证书由面板自动续期；续期后在「自动发布时段」内自动发布到节点，客户端无需改动。</p>
          <p v-if="acmeHint" role="alert" class="text-xs text-ios-orange">{{ acmeHint }}<router-link v-if="server" :to="`/nodes/${server.id}?tab=settings`" class="ml-2 text-ios-blue font-medium hover:underline">去设置连接地址</router-link></p>
        </template>
        <template v-else-if="source === 'file' && !f.internalRelay">
          <label class="block">证书文件路径
            <input v-model="s.certificate_path" maxlength="256" placeholder="/etc/proxy-agent/certs/example.com.crt" class="form-input-base mt-2 font-mono" />
          </label>
          <label class="block">私钥文件路径
            <input v-model="s.private_key_path" maxlength="256" placeholder="/etc/proxy-agent/certs/example.com.key" class="form-input-base mt-2 font-mono" />
          </label>
          <p v-if="pathError" role="alert" class="text-xs text-ios-red">{{ pathError }}</p>
          <p class="text-xs">证书须由公认 CA 签发（例如 certbot 或 acme.sh 申请的证书）并覆盖服务器名称；文件放在节点的 /etc/proxy-agent/certs/，属主 root、组 proxy-agent，私钥权限 0640。续期替换文件后，在节点详情里重启核心。</p>
        </template>
        <template v-else>
          <label class="block">证书 PEM<textarea v-model="s.certificate_pem" rows="4" :placeholder="state.original?.settings.certificate_pem ? '留空保留现有证书' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" />
            <span class="block text-xs mt-1">以 -----BEGIN CERTIFICATE----- 开头的完整证书链；首次填写时必填，编辑时留空表示沿用已存证书。</span>
          </label>
          <label class="block">私钥 PEM<textarea v-model="s.private_key_pem" rows="4" autocomplete="off" :placeholder="state.original?.hasTlsKey ? '留空保留现有私钥' : '必填（内部中转可自动生成）'" class="form-input-base mt-2 font-mono text-xs" />
            <span class="block text-xs mt-1">与上面证书配对的私钥，保存后不会回显；留空表示沿用已存私钥。</span>
          </label>
          <label class="block">信任 CA PEM（可选）<textarea v-model="s.trusted_ca_pem" rows="3" placeholder="一般留空" class="form-input-base mt-2 font-mono text-xs" />
            <span class="block text-xs mt-1">需要固定自签证书时才填客户端另外信任的 CA，一般留空。</span>
          </label>
        </template>
        <div v-if="certificateInfo" class="space-y-1 text-xs">
          <p>当前证书：<span class="font-mono">{{ coveredNames }}</span><span v-if="certificateInfo.selfSigned" class="ml-1.5 rounded bg-ios-fill px-1.5 py-0.5">自签名</span></p>
          <p>到期时间：{{ formatTime(certificateInfo.notAfter) }}</p>
          <p v-if="certificateInfo.coverageWarning" role="alert" class="text-ios-orange">{{ certificateInfo.coverageWarning }}</p>
        </div>
      </template>
    </template>
      <label v-if="f.protocol === 'shadowsocks'" class="block">加密方式
        <select v-model="s.method" class="form-input-base mt-2"><option>chacha20-ietf-poly1305</option><option>aes-256-gcm</option><option>aes-128-gcm</option></select>
        <span class="block text-xs mt-1">客户端必须选择同一加密方式；不确定时用 chacha20-ietf-poly1305。</span>
      </label>
      <template v-if="f.protocol === 'hysteria2'">
        <p class="text-xs">原生 QUIC 与 TLS。带宽填 0 表示使用默认/不限制。</p>
        <label v-for="field in bandwidthFields" :key="field.key" class="block">{{ field.label }} Mbps
          <input :value="s.quic?.[field.key]" @input="setBandwidth(field.key, $event)" type="number" min="0" max="100000" placeholder="0（不限）" class="form-input-base mt-2" />
        </label>
        <p class="text-xs">这里只是向客户端声明的带宽提示（Mbps），不限制真实速率。</p>
      </template>
      <label v-if="f.protocol === 'tuic'" class="block">拥塞控制
        <select :value="s.quic?.congestion_control || ''" @change="setCongestion" class="form-input-base mt-2"><option value="">默认</option><option>cubic</option><option>new_reno</option><option>bbr</option></select>
        <span class="block text-xs mt-1">留空为默认；cubic 通用，bbr 适合高延迟或跨境链路。</span>
      </label>
      <template v-if="f.protocol === 'wireguard'">
        <label class="block">隧道地址（每行一个 CIDR）<textarea v-model="state.addresses" @input="state.wireguardChanged = true" rows="3" placeholder="例如 10.77.0.1/24" class="form-input-base mt-2 font-mono" /></label>
        <label class="block">MTU<input v-model.number="state.mtu" @input="state.wireguardChanged = true" type="number" min="576" max="9000" placeholder="1420" class="form-input-base mt-2" /></label>
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
              <input :value="target.host" @input="setTargetHost(index, $event)" class="form-input-base flex-1" placeholder="例如 192.0.2.10" />
              <input :value="target.port || ''" @input="setTargetPort(index, $event)" type="number" min="1" max="65535" class="form-input-base w-24" />
              <button v-if="pool.length > 1" type="button" class="text-xs text-ios-secondary" @click="removeTarget(index)">删除</button>
            </div>
            <p class="text-xs">每个目标填主机与端口；只有一个目标时保持一行即可。轮询会依次使用全部健康目标。</p>
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
            <label class="block">失败等待（如 5s）
              <input :value="s.forward.fail_timeout || ''" @input="setFailTimeout" placeholder="5s（整数秒，后缀 s）" class="form-input-base mt-2" />
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
          <label class="block">目标主机<input v-model="s.forward.host" placeholder="例如 192.0.2.10 或 internal.example.com" class="form-input-base mt-2 font-mono" />
            <span class="block text-xs mt-1">本节点把流量转发到的地址，不经过用户身份、策略或订阅。</span>
          </label>
          <label class="block">目标端口<input v-model.number="s.forward.port" type="number" min="1" max="65535" placeholder="例如 443" class="form-input-base mt-2" /></label>
        </template>
        <label v-if="!relay" class="block">网络
          <select v-model="s.forward.network" class="form-input-base mt-2"><option value="tcp">TCP</option><option value="udp">UDP</option><option value="tcp,udp">TCP + UDP</option></select>
          <span class="block text-xs mt-1">转发哪种流量；两者都需要时选 TCP + UDP，会各占一个服务。</span>
        </label>
        <p v-if="!gost && !relay" class="text-xs">目标池、端口范围与限速只支持 GOST 核心（设置契约 2）。</p>
      </template>
  </div>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import type { NodeView } from '../../../nodes/api';
import { certificateMatches as findCertificateMatches, type CertificateMatches, type TLSSource } from '../../api';
import { certificatesAPI, type ManagedCertificate } from '../../../system/certificatesApi';
import { connectAddress } from '../../../nodes/nodeAddress';
import { formatTime } from '../../../../app/auth';
import { appendShortId, applyCertificateChoice, certificateChoices, certificateFilesSupported, certificatePathError, changeSecurity, changeTransport, applyXhttpSecurity, fillServerNamesFromTarget, flowChoices, internalRelaySettingsReason, randomXhttpPath, REALITY_HANDSHAKE_PRESETS, securityChoices, serverSettingsReason, sniFromTarget, SELF_SIGNED_REQUIRES_ADDRESS, tlsBlockShown, acmeAddressHint, tlsSourceChoices, tlsSourceFor, usableCertificates, transportChoices, visibleChoices, XHTTP_MODES, forcedTLS, forwardListenPorts, forwardPool, hasSecurity, hasTransport, relayMode, type CertificateChoice, type EditorState } from './nodeEditorModel';
const props = defineProps<{ state: EditorState; gost?: boolean; server?: NodeView; certificates?: ManagedCertificate[] }>();
const f = computed(() => props.state.form), s = computed(() => props.state.form.settings);
// The TLS block is shown for both the TLS-only protocols and an explicit TLS
// security; the certificate source inside it names the one material it presents.
const tlsShown = computed(() => tlsBlockShown(f.value));
const source = computed(() => tlsSourceFor(props.state, props.server));
const acmeCertificates = computed(() => usableCertificates(props.certificates || []));
const sources = computed(() => tlsSourceChoices(certificateFilesSupported(props.server, f.value.core || ''), source.value, acmeCertificates.value.length > 0));
// Section 6: look certificates up by domain before picking a source. Node-side
// files are offered only when the core reports that it can read them.
const matches = ref<CertificateMatches | null>(null);
const choiceKey = ref('');
const lookupBusy = ref(false);
const lookupError = ref('');
const requestStatus = ref('');
const filesSupported = computed(() => certificateFilesSupported(props.server, f.value.core || ''));
const choices = computed(() => matches.value ? certificateChoices(matches.value, filesSupported.value) : []);
const lookupDomain = computed(() => (s.value.server_name || '').trim());
let lookupTimer: ReturnType<typeof setTimeout> | undefined;
let pollTimer: ReturnType<typeof setInterval> | undefined;
async function findCertificates() {
  if (lookupTimer) { clearTimeout(lookupTimer); lookupTimer = undefined; }
  const domain = lookupDomain.value;
  if (!domain || !f.value.nodeId) { matches.value = null; choiceKey.value = ''; lookupError.value = ''; return; }
  lookupBusy.value = true; lookupError.value = '';
  try { matches.value = await findCertificateMatches(f.value.nodeId, domain); }
  catch { matches.value = null; lookupError.value = '查找证书失败，请稍后重试'; }
  finally { lookupBusy.value = false; }
}
function scheduleLookup() {
  if (lookupTimer) clearTimeout(lookupTimer);
  lookupTimer = setTimeout(findCertificates, 400);
}
function chooseCertificate(index: number) {
  const choice: CertificateChoice | undefined = choices.value[index];
  if (!choice) return;
  if (choice.kind === 'request') { void requestCertificate(); return; }
  choiceKey.value = String(index);
  applyCertificateChoice(f.value, choice);
}
async function requestCertificate() {
  const domain = lookupDomain.value;
  if (!domain) return;
  requestStatus.value = `正在为 ${domain} 申请证书…`;
  try {
    const created = await certificatesAPI.create([domain], false, '');
    if (pollTimer) clearInterval(pollTimer);
    pollTimer = setInterval(async () => {
      try {
        const current = (await certificatesAPI.list()).find(item => item.id === created.id);
        if (!current) return;
        if (current.status === 'valid') {
          if (pollTimer) { clearInterval(pollTimer); pollTimer = undefined; }
          applyCertificateChoice(f.value, { kind: 'acme', certificateId: current.id, label: '' });
          requestStatus.value = `${domain} 的证书已签发并选中，保存后生效`;
        } else if (current.status === 'failed') {
          if (pollTimer) { clearInterval(pollTimer); pollTimer = undefined; }
          requestStatus.value = `证书申请失败：${current.lastErrorText || current.lastErrorCode || '未知原因'}`;
        }
      } catch { /* keep polling until the certificate reaches a terminal state */ }
    }, 5000);
  } catch { requestStatus.value = '证书申请未能发起，请检查 Cloudflare 配置'; }
}
watch(lookupDomain, () => { matches.value = null; choiceKey.value = ''; requestStatus.value = ''; });
onUnmounted(() => { if (lookupTimer) clearTimeout(lookupTimer); if (pollTimer) clearInterval(pollTimer); });
function setSource(event: Event) {
  f.value.tlsSource = (event.target as HTMLSelectElement).value as TLSSource;
  if (f.value.tlsSource === 'acme' && !f.value.certificateId) f.value.certificateId = acmeCertificates.value[0]?.id || '';
}
const address = computed(() => connectAddress(props.server));
// The saved inbound already shows its own coverage warning for an unchanged certificate.
const acmeHint = computed(() => {
  if (props.state.original?.certificateInfo?.coverageWarning && f.value.certificateId === props.state.original.certificateId) return '';
  return acmeAddressHint(acmeCertificates.value.find(item => item.id === f.value.certificateId), address.value);
});
// Warnings only once a path is typed: an empty pair is the state before the Owner
// starts, and the save-time message comes from validateStep anyway.
const pathError = computed(() => {
  const certificate = (s.value.certificate_path || '').trim(), key = (s.value.private_key_path || '').trim();
  if (source.value !== 'file' || f.value.internalRelay || (!certificate && !key)) return '';
  return certificatePathError(certificate, key);
});
// Hysteria2 and TUIC run over QUIC, which a proxied (orange cloud) DNS record does
// not carry.
const quicTLS = computed(() => ['hysteria2', 'tuic'].includes(f.value.protocol));
const certificateInfo = computed(() => props.state.original?.certificateInfo || null);
const coveredNames = computed(() => [...(certificateInfo.value?.dnsNames || []), ...(certificateInfo.value?.ipAddresses || [])].join('、') || '—');
const transports = computed(() => visibleChoices(transportChoices(props.server, f.value.core || '', f.value.protocol, s.value), s.value.transport || 'tcp'));
const securities = computed(() => visibleChoices(securityChoices(props.server, f.value.core || '', f.value.protocol, s.value), s.value.security || 'none')
  .filter(choice => !forcedTLS(f.value.protocol) || choice.value === 'tls'));
const flows = computed(() => visibleChoices(flowChoices(props.server, f.value.core || '', f.value.protocol, s.value), s.value.flow || ''));
const supportError = computed(() => internalRelaySettingsReason(f.value.internalRelay, s.value) || serverSettingsReason(props.server, f.value.core || '', f.value.protocol, s.value));
// Picking xhttp needs Reality or TLS; the switch happens here and the reason stays
// visible as a 12px note instead of a silently broken combination.
const securityNote = ref('');
function setTransport(event: Event) {
  changeTransport(props.state, (event.target as HTMLSelectElement).value);
  securityNote.value = applyXhttpSecurity(props.state, props.server);
}
function setSecurity(event: Event) {
  changeSecurity(props.state, (event.target as HTMLSelectElement).value);
  securityNote.value = '';
}
function setFlow(event: Event) { s.value.flow = (event.target as HTMLSelectElement).value; }
function useLegacy() { changeTransport(props.state, 'tcp'); changeSecurity(props.state, 'none'); s.value.flow = ''; securityNote.value = ''; }
function setRealityLines(key: 'server_names' | 'short_ids', event: Event) { s.value.reality![key] = (event.target as HTMLTextAreaElement).value.split(/\r?\n/); }
// A filled SNI list is an Owner decision (sing-box takes exactly one name), so the
// derivation only fills it while it is still empty.
const targetSni = computed(() => sniFromTarget(s.value.reality?.handshake_target || ''));
const sniStatus = ref('');
watch(() => s.value.reality?.handshake_target, () => {
  sniStatus.value = '';
  const reality = s.value.reality;
  if (reality && fillServerNamesFromTarget(reality)) sniStatus.value = `已按握手目标填入 SNI：${reality.server_names[0] || ''}`;
});
function fillSni() {
  const reality = s.value.reality;
  if (!reality) return;
  const sni = sniFromTarget(reality.handshake_target);
  if (!sni) { sniStatus.value = '握手目标是 IP 地址或尚未填写，无法填入 SNI'; return; }
  reality.server_names = [sni];
  sniStatus.value = `已填入 SNI：${sni}`;
}
function pickPreset(event: Event) {
  const select = event.target as HTMLSelectElement;
  const reality = s.value.reality;
  if (reality && select.value) reality.handshake_target = select.value;
  select.value = '';
}
function addShortId() { if (s.value.reality && shortIdCount.value < 16) s.value.reality.short_ids = appendShortId(s.value.reality.short_ids); }
const shortIdCount = computed(() => (s.value.reality?.short_ids || []).filter(id => id.trim()).length);
function setRandomPath() { if (s.value.xhttp) s.value.xhttp.path = randomXhttpPath(); }
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
