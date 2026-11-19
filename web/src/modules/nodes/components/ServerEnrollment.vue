<template>
  <FormDrawer v-model="createOpen" title="新增服务器" width="lg" description="① 填写服务器信息 → ② 在目标服务器执行一次性安装命令" :loading="creating" confirm-text="生成安装命令" @confirm="create">
    <ServerFormFields ref="fields" :form="form" :connection="connectionForm" :traffic="machineForm" :known-tags="knownTags" />
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
      <fieldset v-if="recoveryId" class="space-y-2 text-sm">
        <legend class="font-semibold">安装位置</legend>
        <label class="mr-4 inline-flex items-center gap-1"><input v-model="replacementTarget" type="radio" value="current" /> 覆盖当前机器</label>
        <label class="inline-flex items-center gap-1"><input v-model="replacementTarget" type="radio" value="new" /> 在新机器登记</label>
      </fieldset>
      <p v-if="recoveryId && replacementTarget === 'new'" role="note" class="text-sm text-ios-orange">请在干净的新机器执行。新 Agent 登记成功后旧证书立即失效。旧机队列仍留在旧机，此命令不会补传或清理旧机数据。</p>
      <p v-else-if="recoveryId" role="note" class="text-sm text-ios-orange">请在旧 Agent 所在服务器运行。命令会停止代理，最后一批流量和访问记录经面板确认后，删除旧 Agent 的本机数据并重新登记。面板配置和历史保留。</p>
      <WebUpgradeOption v-model="webUpgrade" />
      <p class="text-xs text-ios-tertiary">面板地址：<code class="font-mono">{{ host }}:{{ port }}</code>（{{ fromSettings ? '取自 系统设置 → 节点安装连接地址' : '当前浏览器地址' }}）；如需修改请到 系统设置 → 节点安装连接地址。</p>
      <p class="text-xs text-ios-tertiary">Agent gRPC 地址与端口默认取面板地址与面板实际监听端口，可在 系统设置 → 节点安装连接地址 里覆盖。命令按所选内核写入重复的 <code class="font-mono">--core</code> 参数（例如 <code class="font-mono">--core xray --core sing-box</code>）；sing-box 仍使用默认的 qui 同版本来源。</p>
      <CodePreview v-if="command" :code="command" title="一键安装命令" />
      <p v-else class="text-sm text-ios-red">面板地址无效，请在 系统设置 → 节点安装连接地址 中设置</p>
    </div>
    <p v-else-if="recoveryId" role="alert" class="text-sm text-ios-red">当前面板缺少发布版本或证书摘要，无法生成重装命令。</p>
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
import { computed, onUnmounted, reactive, ref, watch } from 'vue';
import { enrollmentStage, recoveryEnrollmentStage, type EnrollmentStage } from '../enrollmentStage';
import { nodeAPI, installCommand, type CoreName, type Enrollment, type NodeView } from '../api';
import { CORE_INSTALL_OPTIONS, sortInstallCores, reportedInstallCores, validateInstallCores, setReissuingNodeId, reissuingNodeId } from '../coreInstallOptions';
import { browserInstallEndpoint, resolveInstallEndpoint } from '../installEndpoint';
import { saveNodeMeta } from '../nodeMeta';
import { formFromSettings, payloadFromForm } from '../machineTrafficModel';
import type { ServerEditFormState } from './serverListModel';
import ServerFormFields from './ServerFormFields.vue';
import { formatTime } from '../../../app/auth';
import { nodeStatusMatches } from '../../../app/nodeStatusEvents';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
import CodePreview from '../../../components/CodePreview.vue';
import WebUpgradeOption from './WebUpgradeOption.vue';
const emit = defineEmits<{ changed: [] }>();
withDefaults(defineProps<{ knownTags?: string[] }>(), { knownTags: () => [] });

const createOpen = ref(false), installOpen = ref(false), creating = ref(false);
const fields = ref<InstanceType<typeof ServerFormFields>>();
const form = reactive<ServerEditFormState>({ name: '', region: '', remark: '', renewDate: '', renewCurrency: 'CNY', renewPrice: '', renewCycle: '/年', enabled: true, allowSubscription: true, maintenance: false, tags: [] });
const connectionForm = reactive({ connectHost: '', portRange: '' });
const machineForm = ref(formFromSettings(null, 'GB'));
const error = ref(''), metaWarning = ref('');
const recoveryId = ref('');
const reportedPrimary = ref<CoreName>();
const replacementTarget = ref<'current' | 'new'>('current');
const enrollment = ref<Enrollment | null>(null);
const host = ref(''), port = ref(443), grpcHost = ref(''), grpcPort = ref(0), cores = ref<CoreName[]>(['xray']), webUpgrade = ref(false), fromSettings = ref(false);
const installAvailable = computed(() => Boolean(enrollment.value?.install?.version && enrollment.value?.install?.caSha256));

const coreValidation = computed(() => validateInstallCores(cores.value));
const coreValidationError = computed(() => coreValidation.value.error || '');

const command = computed(() => {
  const item = enrollment.value;
  if (!item || !installAvailable.value || !/^[A-Za-z0-9.-]+$/.test(host.value) || !Number.isInteger(port.value) || port.value < 1 || port.value > 65535) return '';
  if (!coreValidation.value.valid) return '';
  const orderedCores = sortInstallCores(cores.value, reportedPrimary.value);
  // grpcHost '' keeps the panel address; grpcPort 0 keeps the listening port the
  // enrollment response reported. Both are overridden only by the saved setting.
  return installCommand({ version: item.install.version, host: host.value, httpPort: port.value,
    grpcHost: grpcHost.value, grpcPort: grpcPort.value || item.install.grpcPort, caSha256: item.install.caSha256, token: item.enrollmentToken, cores: orderedCores, webUpgrade: webUpgrade.value, replaceAgent: Boolean(recoveryId.value) && replacementTarget.value === 'current' });
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
    if (recoveryId.value) {
      const recovery = await nodeAPI.recoveryStatus(id);
      if (current !== stageGeneration) return;
      const waiting = recoveryEnrollmentStage(recovery.status, recovery.id === recoveryId.value);
      if (waiting) {
        enrollmentStageInfo.value = waiting;
        if (recovery.status !== 'issued' || recovery.id !== recoveryId.value) stopStageWatcher();
        return;
      }
    }
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
function showInstall(item: Enrollment, replacement = '', installedCores: CoreName[] = ['xray']) {
  reportedPrimary.value = replacement ? installedCores[0] : undefined;
  replacementTarget.value = 'current';
  recoveryId.value = replacement;
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
  cores.value = installedCores;
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
  if (!form.name.trim()) { error.value = '请输入服务器名称'; return; }
  creating.value = true; error.value = ''; metaWarning.value = '';
  try {
    const range = fields.value!.connectionSettings();
    const traffic = payloadFromForm(machineForm.value);
    const settings = { ...form, tags: [...form.tags] };
    const item = await nodeAPI.enroll(settings.name.trim(), connectionForm.connectHost.trim() || undefined);
    // Once created, keep the token available even if a following settings request fails.
    try {
      await saveNodeMeta(item.node.id, { region: settings.region.trim(), remark: settings.remark.trim(),
        renewDate: settings.renewDate, renewCurrency: settings.renewCurrency,
        renewPrice: String(settings.renewPrice).trim(), renewCycle: settings.renewCycle, tags: settings.tags, ...range });
    } catch (e) { metaWarning.value = `服务器已创建，服务器信息保存失败：${(e as Error).message}。请在编辑服务器中补填。`; }
    if (settings.maintenance) {
      try {
        await nodeAPI.save({ id: item.node.id, name: item.node.name, enabled: item.node.enabled,
          allowSubscription: item.node.allowSubscription, maintenance: settings.maintenance });
      } catch (e) { metaWarning.value += `${metaWarning.value ? '\n' : ''}维护模式保存失败：${(e as Error).message}。请在编辑服务器中重新设置。`; }
    }
    try {
      await nodeAPI.saveMachineTrafficSettings(item.node.id, traffic);
    } catch (e) { metaWarning.value += `${metaWarning.value ? '\n' : ''}整机流量设置保存失败：${(e as Error).message}。请在编辑服务器中重新设置。`; }
    showInstall(item); createOpen.value = false;
    emit('changed');
  } catch (e) { error.value = (e as Error).message; }
  finally { creating.value = false; }
}

async function reissue(node: NodeView) {
  if (reissuingNodeId.value) return;
  setReissuingNodeId(node.id);
  metaWarning.value = '';
  try {
    if (node.status === 'pending') {
      showInstall(await nodeAPI.reissueEnrollment(node.id));
    } else {
      const item = await nodeAPI.issueRecovery(node.id, { revokeNow: false, expiresInSeconds: 3600 });
      showInstall(item, item.recoveryId, reportedInstallCores(node.capabilities));
    }
  }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally {
    setReissuingNodeId(null);
  }
}
function installCode(id: string) { return enrollment.value?.node.id === id ? installAvailable.value ? command.value : recoveryId.value ? '' : enrollment.value.enrollmentToken : ''; }
function open() {
  error.value = '';
  Object.assign(form, { name: '', region: '', remark: '', renewDate: '', renewCurrency: 'CNY', renewPrice: '', renewCycle: '/年', enabled: true, allowSubscription: true, maintenance: false, tags: [] });
  Object.assign(connectionForm, { connectHost: '', portRange: '' });
  machineForm.value = formFromSettings(null, 'GB');
  createOpen.value = true;
}
defineExpose({ installCode, open, reissue });
</script>
