<template>
  <div v-if="isOwner && node.enabled && node.status !== 'pending' && !node.deletedAt" class="rounded-xl border border-gray-200 p-4 space-y-3 text-sm">
    <div class="flex items-center justify-between gap-3">
      <h3 class="text-base font-semibold leading-normal">原位身份恢复</h3>
      <button class="ios-btn-secondary" :disabled="statusLoading" @click="refresh">刷新恢复状态</button>
    </div>
    <p class="text-xs text-ios-secondary">主机重装后不必新建服务器记录：签发一次性恢复码，在重装的主机上运行下面的安装命令重新登记。节点 ID、入站、出站、节点组、隧道、代理链与历史配置原样保留，旧证书随核销永久作废，已发布配置会在新 Agent 首次能力上报后自动重新下发。</p>
    <div class="grid gap-3 sm:grid-cols-2">
      <label class="text-xs text-ios-secondary">恢复码有效期
        <select v-model.number="ttl" class="form-input-base mt-1.5">
          <option :value="900">15 分钟</option>
          <option :value="1800">30 分钟</option>
          <option :value="3600">60 分钟</option>
        </select>
      </label>
      <label class="mt-1 flex items-center gap-2 self-end text-sm text-ios-label">
        <input v-model="revokeNow" type="checkbox" class="accent-ios-blue" />立即吊销当前证书
      </label>
    </div>
    <p class="text-xs text-ios-secondary">勾选“立即吊销”会在签发时立刻吊销现有证书并断开当前 Agent，节点在重新登记前一直离线；不勾选则旧证书在恢复核销时才作废。</p>
    <div class="flex justify-end">
      <button class="ios-btn-primary" :disabled="loading || statusLoading" @click="ask">{{ loading ? '正在签发…' : '签发恢复码' }}</button>
    </div>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
    <div v-if="blockers.length" class="space-y-2 rounded-xl border border-amber-200 bg-amber-50 p-3 text-amber-900">
      <p>节点存在未完成任务，请先处理下列项目：</p>
      <ul class="list-disc space-y-1 pl-5">
        <li v-for="item in blockers" :key="item.kind + item.id">{{ blockerLabels[item.kind] || item.kind }} · {{ blockerDetail(item) }}</li>
      </ul>
    </div>
    <div v-if="result" class="space-y-3 rounded-xl border border-amber-200 bg-amber-50 p-4 text-amber-900">
      <p class="font-semibold">恢复码只显示一次，请立即复制</p>
      <p>有效期至 {{ formatTime(result.expiresAt) }}。离开本页后无法再次查看；需要新码只能重新签发，重新签发会作废此码。</p>
      <CodePreview :code="result.enrollmentToken" title="一次性恢复码" />
      <div v-if="installAvailable" class="text-xs text-amber-900">运行内核
        <div class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl bg-white px-3 py-2">
          <label v-for="option in CORE_INSTALL_OPTIONS" :key="option.name" class="inline-flex items-center gap-1.5 text-ios-label cursor-pointer">
            <input v-model="cores" type="checkbox" :value="option.name" class="accent-ios-blue" />
            <span>{{ option.label }}</span>
            <span v-if="option.hint" class="text-xs text-ios-tertiary">（{{ option.hint }}）</span>
          </label>
        </div>
      </div>
      <WebUpgradeOption v-if="installAvailable" v-model="webUpgrade" />
      <p v-if="installAvailable" class="text-xs">面板地址：<code class="font-mono">{{ host }}:{{ port }}</code>（{{ fromSettings ? '取自 系统设置 → 节点安装连接地址' : '当前浏览器地址' }}）；如需修改请到 系统设置 → 节点安装连接地址。</p>
      <CodePreview v-if="command" :code="command" title="一键安装命令" />
      <p v-else-if="!installAvailable" class="text-sm">本地或开发构建无法生成一键命令，请在重装的主机上用恢复码手动登记。</p>
      <p v-else-if="coreValidationError" role="alert" class="text-sm text-ios-red">{{ coreValidationError }}</p>
      <p v-else class="text-sm text-ios-red">面板地址无效，请在 系统设置 → 节点安装连接地址 中设置。</p>
      <p class="text-xs">主机若仍留有旧的 <code class="font-mono">/var/lib/proxy-agent</code>，Agent 会以已有身份拒绝登记；重装系统后无需额外处理。</p>
    </div>
    <p v-if="statusError" role="alert" class="text-sm text-ios-red">{{ statusError }}</p>
    <p v-else-if="noRecovery" class="text-ios-secondary">尚未签发过恢复码。</p>
    <dl v-else-if="status" class="grid gap-3 md:grid-cols-2">
      <div><dt class="text-gray-500">最近一次恢复</dt><dd class="mt-1">{{ statusLabels[status.status] || '无法确认' }}</dd></div>
      <div><dt class="text-gray-500">重新下发</dt><dd class="mt-1">{{ redeployLabels[status.redeployState] || '无法确认' }}<span v-if="status.redeployRevision > 0"> · r{{ status.redeployRevision }}</span><span v-if="status.redeployCode"> · {{ status.redeployCode }}</span></dd></div>
      <div><dt class="text-gray-500">签发时间</dt><dd class="mt-1">{{ formatTime(status.createdAt) }}</dd></div>
      <div><dt class="text-gray-500">恢复码有效期至</dt><dd class="mt-1">{{ formatTime(status.expiresAt) }}</dd></div>
      <div><dt class="text-gray-500">核销时间</dt><dd class="mt-1">{{ status.consumedAt ? formatTime(status.consumedAt) : '尚未核销' }}</dd></div>
      <div><dt class="text-gray-500">签发人</dt><dd class="mt-1">{{ status.owner || '无法确认' }}</dd></div>
      <div class="md:col-span-2"><dt class="text-gray-500">被替换的证书指纹</dt><dd class="mt-1 font-mono break-all text-xs">{{ status.previousFingerprint }}</dd></div>
      <div v-if="status.newFingerprint" class="md:col-span-2"><dt class="text-gray-500">恢复后的证书指纹</dt><dd class="mt-1 font-mono break-all text-xs">{{ status.newFingerprint }}</dd></div>
    </dl>
    <p v-if="status?.revokedOnIssue" class="text-xs text-ios-orange">签发时已吊销当时的证书。</p>
    <p class="text-xs text-ios-secondary">恢复码等同该服务器的接管凭据，请只在受控渠道传递；面板只保存它的哈希，无法再次查看。此处的记录不证明节点已上线或代理可用。</p>
    <ConfirmDialog v-model="confirm" title="签发节点恢复码" :message="confirmMessage" :warning-details="confirmDetails" confirm-text="确认签发" @confirm="issue" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue';
import { nodeAPI, installCommand, type CoreName, type Enrollment, type NodeView, type RecoveryStatus } from '../api';
import { CORE_INSTALL_OPTIONS, sortInstallCores, validateInstallCores } from '../coreInstallOptions';
import { browserInstallEndpoint, resolveInstallEndpoint } from '../installEndpoint';
import { formatTime, isOwner } from '../../../app/auth';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import CodePreview from '../../../components/CodePreview.vue';
import WebUpgradeOption from './WebUpgradeOption.vue';
const props = defineProps<{ node: NodeView }>();
// The reinstall keeps the cores the node ran before: that set is what the
// installer has to place again, so it starts from the reported capabilities.
const statusLabels: Record<string, string> = { issued: '已签发', consumed: '已核销', superseded: '已被新码取代', expired: '已过期' };
const redeployLabels: Record<string, string> = { none: '—', pending: '等待节点上线', queued: '重新下发中', success: '重新下发成功', failed: '重新下发失败', not_needed: '无需下发' };
const blockerLabels: Record<string, string> = { deployment_target: '待完成的发布', deployment_command: '待执行的节点操作', deployment_batch: '进行中的批量发布', agent_update: 'Agent 更新任务', core_update: '核心更新任务', core_remove: '核心移除任务', node_pause: '暂停/恢复任务' };
const ttl = ref(900), revokeNow = ref(false), loading = ref(false), statusLoading = ref(false), confirm = ref(false);
const host = ref(''), port = ref(443), grpcHost = ref(''), grpcPort = ref(0), cores = ref<CoreName[]>(['xray']), webUpgrade = ref(false), fromSettings = ref(false);
const error = ref(''), statusError = ref(''), noRecovery = ref(false);
const result = ref<(Enrollment & { purpose: string }) | null>(null), status = ref<RecoveryStatus | null>(null);
const blockers = ref<{ kind: string; id: string; name: string }[]>([]);
const ttlLabel = computed(() => ttl.value === 1800 ? '30 分钟' : ttl.value === 3600 ? '60 分钟' : '15 分钟');
const confirmMessage = computed(() => `为服务器 ${props.node.name} 签发恢复码。恢复码等同该节点的接管凭据：拿到它的人可以领取该节点全部已下发配置，包括 Reality 私钥与用户凭据。有效期 ${ttlLabel.value}，只显示一次，面板只保存哈希。`);
const confirmDetails = computed(() => revokeNow.value
  ? '已勾选“立即吊销当前证书”：签发后面板立即吊销现有证书并断开当前 Agent，节点在重新登记前保持离线。'
  : '当前证书在恢复核销时作废；恢复登记前旧 Agent 仍可连接。');
const installAvailable = computed(() => Boolean(result.value?.install?.version && result.value?.install?.caSha256));

const coreValidation = computed(() => validateInstallCores(cores.value));
const coreValidationError = computed(() => coreValidation.value.error || '');

const command = computed(() => {
  const item = result.value;
  if (!item || !installAvailable.value || !coreValidation.value.valid) return '';
  if (!/^[A-Za-z0-9.-]+$/.test(host.value) || !Number.isInteger(port.value) || port.value < 1 || port.value > 65535) return '';
  try {
    const orderedCores = sortInstallCores(cores.value);
    return installCommand({ version: item.install.version, host: host.value, httpPort: port.value,
      grpcHost: grpcHost.value, grpcPort: grpcPort.value || item.install.grpcPort, caSha256: item.install.caSha256, token: item.enrollmentToken, cores: orderedCores, webUpgrade: webUpgrade.value });
  } catch { return ''; }
});
function blockerDetail(item: { kind: string; id: string; name: string }) {
  return item.kind === 'deployment_target' ? `配置 #${item.id}` : item.name || item.id;
}
function defaultCores(): CoreName[] {
  const reported = (props.node.capabilities?.cores ?? []).map(core => core.name).filter((name): name is CoreName => CORE_INSTALL_OPTIONS.some(option => option.name === name));
  const selected: CoreName[] = reported.length ? [...new Set(reported)] : ['xray'];
  return sortInstallCores(selected);
}
let generation = 0, stopped = false;
async function refresh() {
  if (statusLoading.value || stopped) return;
  const current = ++generation, nodeId = props.node.id;
  statusLoading.value = true; statusError.value = ''; noRecovery.value = false;
  try {
    const item = await nodeAPI.recoveryStatus(nodeId);
    if (current === generation && props.node.id === nodeId) { status.value = item; statusError.value = ''; }
  } catch (e) {
    if (current !== generation) return;
    status.value = null;
    // 404 means this node never had a recovery issued, which is not an error.
    if (e instanceof APIError && e.status === 404) noRecovery.value = true;
    else statusError.value = (e as Error).message;
  } finally { if (current === generation) statusLoading.value = false; }
}
function ask() {
  if (loading.value || statusLoading.value) return;
  error.value = ''; blockers.value = [];
  confirm.value = true;
}
async function issue() {
  if (loading.value) return;
  loading.value = true; error.value = ''; blockers.value = [];
  try {
    result.value = await nodeAPI.issueRecovery(props.node.id, { expiresInSeconds: ttl.value, revokeNow: revokeNow.value });
    // Same defaults as the enrollment dialog: the saved panel address if any,
    // otherwise the browser address.
    const fallback = browserInstallEndpoint();
    host.value = fallback.host;
    port.value = fallback.port;
    grpcHost.value = '';
    grpcPort.value = 0;
    fromSettings.value = false;
    cores.value = defaultCores();
    webUpgrade.value = false;
    showToast('恢复码已签发，请立即复制');
    await refresh();
    const nodeId = props.node.id;
    void resolveInstallEndpoint().then(endpoint => {
      if (props.node.id !== nodeId) return;
      grpcHost.value = endpoint.grpcHost;
      grpcPort.value = endpoint.grpcPort ?? 0;
      host.value = endpoint.host;
      port.value = endpoint.port;
      fromSettings.value = endpoint.fromSettings;
    });
  } catch (e) {
    error.value = (e as Error).message;
    const items = e instanceof APIError ? e.details?.blockers : undefined;
    if (Array.isArray(items)) blockers.value = items as { kind: string; id: string; name: string }[];
  } finally { loading.value = false; }
}
watch(() => props.node.id, () => {
  generation++; loading.value = false; statusLoading.value = false;
  status.value = null; result.value = null; error.value = ''; statusError.value = ''; noRecovery.value = false; blockers.value = []; confirm.value = false;
  void refresh();
});
watch(() => [props.node.enabled, props.node.status], () => { void refresh(); });
onMounted(refresh);
onUnmounted(() => { stopped = true; generation++; });
</script>
