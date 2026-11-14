<template>
  <FormDrawer v-model="createOpen" title="新增服务器" description="① 填写服务器信息 → ② 在目标服务器执行一次性安装命令" :loading="creating" confirm-text="生成安装命令" @confirm="create">
    <label class="block text-xs text-ios-secondary">服务器名称<input v-model.trim="name" maxlength="40" class="form-input-base mt-1.5" placeholder="例如 东京 01" /></label>
    <label class="block text-xs text-ios-secondary mt-3">客户端连接地址（可选）<input v-model.trim="connectHost" maxlength="253" class="form-input-base mt-1.5 font-mono" placeholder="可填域名或 IP，留空使用公网 IP" /></label>
    <details class="text-sm text-ios-secondary">
      <summary class="cursor-pointer">续费信息（可选）</summary>
      <div class="space-y-3 mt-3">
        <label class="block">续费到期日<input v-model="renewDate" type="date" class="form-input-base" /></label>
        <label class="block">币种<select v-model="currency" class="form-input-base"><option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option></select></label>
        <label class="block">续费价格<input v-model="price" type="number" min="0" step="any" class="form-input-base" /></label>
        <label class="block">周期<select v-model="cycle" class="form-input-base"><option v-for="c in CYCLE_LIST" :key="c">{{ c }}</option></select></label>
      </div>
    </details>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
  </FormDrawer>
  <FormDrawer v-model="installOpen" title="一键安装命令" description="② 在服务器终端以 root 用户运行命令，完成 Agent 部署与证书信任配置。" :show-footer="false" width="2xl">
    <div v-if="installAvailable" class="space-y-3">
      <div class="text-xs text-ios-secondary">运行内核
        <div class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl bg-ios-bg px-3 py-2">
          <label v-for="option in CORE_INSTALL_OPTIONS" :key="option.name" class="inline-flex items-center gap-1.5 text-ios-label cursor-pointer">
            <input v-model="cores" type="checkbox" :value="option.name" class="accent-ios-blue" />
            <span>{{ option.label }}</span>
            <span v-if="option.hint" class="text-xs text-ios-tertiary">（{{ option.hint }}）</span>
          </label>
        </div>
      </div>
      <p v-if="coreValidationError" role="alert" class="text-xs text-ios-red">{{ coreValidationError }}</p>
      <WebUpgradeOption v-model="webUpgrade" />
      <p class="text-xs text-ios-tertiary">面板地址：<code class="font-mono">{{ host }}:{{ port }}</code>（{{ fromSettings ? '取自 系统设置 → 节点安装连接地址' : '当前浏览器地址' }}）；如需修改请到 系统设置 → 节点安装连接地址。</p>
      <p class="text-xs text-ios-tertiary">Agent gRPC 地址与端口默认取面板地址与面板实际监听端口，可在 系统设置 → 节点安装连接地址 里覆盖。命令按所选内核写入重复的 <code class="font-mono">--core</code> 参数（例如 <code class="font-mono">--core xray --core sing-box</code>）；sing-box 仍使用默认的 qui 同版本来源。</p>
      <CodePreview v-if="command" :code="command" title="一键安装命令" />
      <p v-else class="text-sm text-ios-red">面板地址无效，请在 系统设置 → 节点安装连接地址 中设置</p>
    </div>
    <div v-else class="space-y-2">
      <CodePreview :code="enrollment?.enrollmentToken || ''" />
      <p class="text-xs text-ios-tertiary">本地 / 开发构建无法生成一键命令，请使用注册码手动登记。</p>
    </div>
    <p class="text-xs text-ios-tertiary">命令含一次性注册码，{{ formatTime(enrollment?.expiresAt || '') }} 前有效。重新生成会撤销旧注册码。</p>
    <p v-if="metaWarning" role="alert" class="text-sm text-ios-orange">{{ metaWarning }}</p>
    <div v-if="enrollmentStageInfo" class="rounded-xl border border-black/[0.08] bg-black/[0.02] p-3 text-xs flex items-center justify-between">
      <span class="text-ios-secondary">登记状态：<span class="font-semibold text-ios-label">{{ enrollmentStageInfo.label }}</span></span>
      <span v-if="enrollmentStageInfo.done" class="text-emerald-600 font-medium">✓ 就绪</span>
      <span v-else class="inline-block w-3.5 h-3.5 border-2 border-ios-blue border-t-transparent rounded-full animate-spin"></span>
    </div>
    <button type="button" class="ios-dialog-btn-secondary" @click="closeInstall">关闭</button>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue';
import { enrollmentStage, type EnrollmentStage } from '../enrollmentStage';
import { nodeAPI, installCommand, type CoreName, type Enrollment, type NodeView } from '../api';
import { CORE_INSTALL_OPTIONS, sortInstallCores, validateInstallCores, setReissuingNodeId, reissuingNodeId } from '../coreInstallOptions';
import { browserInstallEndpoint, resolveInstallEndpoint } from '../installEndpoint';
import { validConnectHost } from '../nodeAddress';
import { CURRENCY_LIST, CYCLE_LIST, saveNodeMeta } from '../nodeMeta';
import { formatTime } from '../../../app/auth';
import { nodeStatusMatches } from '../../../app/nodeStatusEvents';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
import CodePreview from '../../../components/CodePreview.vue';
import WebUpgradeOption from './WebUpgradeOption.vue';
const emit = defineEmits<{ changed: [] }>();

const createOpen = ref(false), installOpen = ref(false), creating = ref(false);
const name = ref(''), connectHost = ref(''), renewDate = ref(''), currency = ref('CNY'), price = ref(''), cycle = ref('/年');
const error = ref(''), metaWarning = ref('');
const enrollment = ref<Enrollment | null>(null);
const host = ref(''), port = ref(443), grpcHost = ref(''), grpcPort = ref(0), cores = ref<CoreName[]>(['xray']), webUpgrade = ref(false), fromSettings = ref(false);
const installAvailable = computed(() => Boolean(enrollment.value?.install?.version && enrollment.value?.install?.caSha256));

const coreValidation = computed(() => validateInstallCores(cores.value));
const coreValidationError = computed(() => coreValidation.value.error || '');

const command = computed(() => {
  const item = enrollment.value;
  if (!item || !installAvailable.value || !/^[A-Za-z0-9.-]+$/.test(host.value) || !Number.isInteger(port.value) || port.value < 1 || port.value > 65535) return '';
  if (!coreValidation.value.valid) return '';
  const orderedCores = sortInstallCores(cores.value);
  // grpcHost '' keeps the panel address; grpcPort 0 keeps the listening port the
  // enrollment response reported. Both are overridden only by the saved setting.
  return installCommand({ version: item.install.version, host: host.value, httpPort: port.value,
    grpcHost: grpcHost.value, grpcPort: grpcPort.value || item.install.grpcPort, caSha256: item.install.caSha256, token: item.enrollmentToken, cores: orderedCores, webUpgrade: webUpgrade.value });
});
// Each opening of the dialog gets its own number, so a late settings answer for
// an earlier opening is ignored. This cannot compare `enrollment.value` with
// `item`: the ref stores a reactive proxy, which is never === the raw object,
// and that comparison silently dropped the saved endpoint every time.

const enrollmentStageInfo = ref<EnrollmentStage | null>(null);
let stageWatcherTimer: any = null;
let stageNodeId = '', stageGeneration = 0;
let stageReading = -1;
function visibilityChanged() { if (document.visibilityState === 'visible') void checkStage(); }

function stopStageWatcher() {
  stageGeneration++;
  stageNodeId = "";
  document.removeEventListener('visibilitychange', visibilityChanged);
  if (stageWatcherTimer !== null) {
    clearInterval(stageWatcherTimer);
    stageWatcherTimer = null;
  }
  window.removeEventListener('proxy-node-status', handleNodeStatusEvent);
}

async function checkStage() {
  if (!stageNodeId || stageReading === stageGeneration || document.visibilityState !== 'visible') return;
  const current = stageGeneration, id = stageNodeId;
  stageReading = current;
  try {
    const node = await nodeAPI.get(id);
    if (current !== stageGeneration) return;
    const stage = enrollmentStage(node.status);
    enrollmentStageInfo.value = stage;
    if (stage.done) {
      stopStageWatcher();
      emit('changed');
    }
  } catch (e) {
    if (current === stageGeneration) showToast((e as Error).message, 'error');
  } finally { if (stageReading === current) stageReading = -1; }
}

function handleNodeStatusEvent(event: Event) {
  if (nodeStatusMatches(event, stageNodeId)) void checkStage();
}

function startStageWatcher(nodeId: string) {
  stopStageWatcher();
  stageNodeId = nodeId;
  enrollmentStageInfo.value = enrollmentStage('pending');
  window.addEventListener('proxy-node-status', handleNodeStatusEvent);
  document.addEventListener('visibilitychange', visibilityChanged);
  stageWatcherTimer = setInterval(checkStage, 15000);
  void checkStage();
}

function closeInstall() {
  installOpen.value = false;
  stopStageWatcher();
}

watch(installOpen, open => {
  if (!open) { showGeneration++; stopStageWatcher(); }
});

onUnmounted(() => {
  showGeneration++;
  stopStageWatcher();
});

let showGeneration = 0;
function showInstall(item: Enrollment) {
  startStageWatcher(item.node.id);
  const current = ++showGeneration;
  enrollment.value = item;
  // The dialog opens with the browser address immediately, so the command is
  // usable even before the saved endpoint arrives; a saved address then replaces it.
  const fallback = browserInstallEndpoint();
  host.value = fallback.host;
  port.value = fallback.port;
  grpcHost.value = '';
  grpcPort.value = 0;
  fromSettings.value = false;
  cores.value = ['xray'];
  webUpgrade.value = false;
  installOpen.value = true;
  void resolveInstallEndpoint().then(endpoint => {
    if (current !== showGeneration) return;
    grpcHost.value = endpoint.grpcHost;
    grpcPort.value = endpoint.grpcPort ?? 0;
    host.value = endpoint.host;
    port.value = endpoint.port;
    fromSettings.value = endpoint.fromSettings;
  });
}
async function create() {
  if (creating.value) return;
  if (!name.value) { error.value = '请输入服务器名称'; return; }
  if (connectHost.value && !validConnectHost(connectHost.value)) {
    error.value = '客户端连接地址需为域名或 IP，不能包含协议、端口或方括号';
    return;
  }
  creating.value = true; error.value = ''; metaWarning.value = '';
  try {
    const item = await nodeAPI.enroll(name.value, connectHost.value.trim() || undefined);
    // The enrollment succeeded: always show its token, even if optional metadata fails.
    showInstall(item); createOpen.value = false;
    try {
      await saveNodeMeta(item.node.id, { renewDate: renewDate.value, renewCurrency: currency.value,
        renewPrice: price.value.trim(), renewCycle: cycle.value });
    } catch (e) { metaWarning.value = `服务器已创建，续费信息未保存：${(e as Error).message}。请在详情设置中补填。`; }
    name.value = ''; connectHost.value = ''; renewDate.value = ''; currency.value = 'CNY'; price.value = ''; cycle.value = '/年';
    emit('changed');
  } catch (e) { error.value = (e as Error).message; }
  finally { creating.value = false; }
}

async function reissue(node: NodeView) {
  if (reissuingNodeId.value) return;
  setReissuingNodeId(node.id);
  metaWarning.value = '';
  try {
    const item = await nodeAPI.reissueEnrollment(node.id);
    showInstall(item);
  }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally {
    setReissuingNodeId(null);
  }
}
function installCode(id: string) { return enrollment.value?.node.id === id ? installAvailable.value ? command.value : enrollment.value.enrollmentToken : ''; }
defineExpose({ installCode, open: () => { error.value = ''; connectHost.value = ''; createOpen.value = true; }, reissue });
</script>
