<template>
  <div class="space-y-6">
    <PageHeader title="系统设置" parent="系统" description="查看当前平台信息并管理访问记录保留策略。" />
    <div class="ios-segmented-container inline-flex overflow-x-auto max-w-full"><button v-for="tab in tabs" :key="tab.key" @click="activeTab = tab.key" :class="['ios-segmented-item whitespace-nowrap', activeTab === tab.key ? 'active' : '']">{{ tab.label }}</button></div>
    <!-- The panel updates itself from the web UI; only the Owner sees the card and
         it heads the 系统信息 tab, above the read-only runtime information. -->
    <PanelUpdatePanel v-if="isOwner && activeTab === 'info'" />
    <section v-if="activeTab === 'base'" class="panel-card p-6 max-w-3xl space-y-5 text-sm">
      <div class="flex justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">平台基础设置</h2><button class="ios-btn-secondary" :disabled="baseLoading || baseSaving" @click="loadBase">刷新已保存设置</button></div>
      <ErrorState v-if="baseError" :message="baseError" @retry="loadBase" /><LoadingSkeleton v-else-if="!baseSaved" />
      <template v-else>
        <label class="block">平台名称<input v-model="baseForm.platformName" maxlength="120" class="form-input-base mt-2" /></label>
        <label class="block">系统时区<input v-model="baseForm.timezone" list="system-timezones" placeholder="例如 Asia/Shanghai" class="form-input-base mt-2" /><datalist id="system-timezones"><option value="Asia/Shanghai" /><option value="UTC" /><option value="Asia/Tokyo" /><option value="America/New_York" /><option value="Europe/London" /></datalist></label>
        <p class="text-xs text-gray-500">时间仍以 UTC 保存，页面按所选时区显示。已发布的节点规则保留原时区；后续规则和配置生成读取新设置。正在编辑的当地时间请按新时区重新核对。</p>
        <div class="flex flex-wrap justify-between items-center gap-3"><span class="text-xs text-gray-500">{{ baseSaved.revision ? `版本 ${baseSaved.revision} · ${baseSaved.updatedBy} · ${formatTime(baseSaved.updatedAt)}` : '首次初始化的设置' }}</span><button class="ios-btn-primary" :disabled="baseLoading || baseSaving" @click="confirmBase">{{ baseSaving ? '保存中…' : '保存基础设置' }}</button></div>
      </template>
    </section>
    <section v-else-if="activeTab === 'accessLog'" class="panel-card p-6 max-w-3xl space-y-5">
      <div class="flex flex-wrap justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">访问记录保留</h2><button class="ios-btn-secondary" :disabled="loading || saving" @click="load">刷新已保存策略</button></div>
      <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="!saved" />
      <template v-else>
        <div class="grid grid-cols-1 md:grid-cols-3 gap-4 text-sm"><label>允许访问记录（天）<input v-model.number="form.allowedDays" type="number" min="1" max="3650" step="1" class="form-input-base mt-2" /></label><label>阻断记录（天）<input v-model.number="form.blockedDays" type="number" min="1" max="3650" step="1" class="form-input-base mt-2" /></label><label>小时流量及账户明细（天）<input v-model.number="form.hourlyDays" type="number" min="1" max="3650" step="1" class="form-input-base mt-2" /></label></div>
        <p class="text-xs text-gray-500 leading-relaxed">可设置 1～3650 天。缩短保留时间后，超期记录将在后台清理；删除后不能通过再次延长时间找回。迟到上报按当前策略处理。日流量长期保留。</p>
        <p class="text-xs text-gray-500">系统只记录域名或目标 IP 等访问元数据，不记录网页正文和完整网址。</p>
        <div class="flex flex-wrap justify-between items-center gap-3 text-sm"><span class="text-gray-500">{{ saved.revision ? `策略版本 ${saved.revision} · ${saved.updatedBy} · ${formatTime(saved.updatedAt)}` : '当前使用默认策略' }}</span><button class="ios-btn-primary" :disabled="loading || saving" @click="confirmSave">{{ saving ? '保存中…' : '保存访问记录策略' }}</button></div>
      </template>
    </section>
    <LatencyTargetsPanel v-else-if="activeTab === 'latency'" />
    <BackupSchedulePanel v-else-if="activeTab === 'backup'" />
    <RuntimeSettingsPanel v-else :key="activeTab" :kind="runtimeTab" />
    <section v-if="isOwner && activeTab === 'base'" class="panel-card p-6 max-w-3xl space-y-5 text-sm">
      <div class="flex flex-wrap justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">节点安装连接地址</h2><button class="ios-btn-secondary" :disabled="endpointLoading || endpointSaving" @click="loadEndpoint">刷新已保存地址</button></div>
      <ErrorState v-if="endpointError" :message="endpointError" @retry="loadEndpoint" /><LoadingSkeleton v-else-if="!endpointSaved" />
      <template v-else>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <label>面板地址<input v-model.trim="endpointForm.host" maxlength="253" placeholder="panel.example.com 或 203.0.113.10" class="form-input-base mt-2" /></label>
          <label>面板 Web 端口<input v-model.number="endpointForm.httpPort" type="number" min="1" max="65535" placeholder="22550" class="form-input-base mt-2" /></label>
          <label>Agent gRPC 地址<input v-model.trim="endpointForm.grpcHost" maxlength="253" placeholder="留空表示与面板地址相同" class="form-input-base mt-2" /></label>
          <label>Agent gRPC 端口<input v-model.number="endpointForm.grpcPort" type="number" min="1" max="65535" placeholder="留空表示用面板实际监听端口" class="form-input-base mt-2" /></label>
        </div>
        <p class="text-xs text-gray-500 leading-relaxed">节点安装 Agent 时用面板地址和面板 Web（HTTPS）端口下载面板证书并登记；Agent 随后用 Agent gRPC 地址和端口建立 mTLS 连接。地址填节点能访问到的公网域名或 IPv4（不含协议和端口）。</p>
        <p class="text-xs text-gray-500">Agent gRPC 地址留空表示与面板地址相同；端口留空表示用面板实际监听端口。只有面板 Web 位于 CDN 或反向代理后面、gRPC 单独暴露时才需要另填 gRPC 地址；该地址必须被面板证书覆盖，且不能经过终止 TLS 的代理。</p>
        <p class="text-xs text-gray-500">面板地址留空则使用当前浏览器访问面板的地址。修改只影响之后生成的安装命令，不改动已有节点。</p>
        <p v-if="endpointFormError" role="alert" class="text-sm text-ios-red">{{ endpointFormError }}</p>
        <div class="flex flex-wrap justify-between items-center gap-3">
          <span class="text-xs text-gray-500">{{ endpointSaved.revision ? `地址版本 ${endpointSaved.revision} · ${endpointSaved.updatedBy} · ${formatTime(endpointSaved.updatedAt)}` : '当前未设置，安装命令使用浏览器地址' }}</span>
          <button class="ios-btn-primary" :disabled="endpointSaving" @click="saveEndpoint">{{ endpointSaving ? '保存中…' : '保存安装连接地址' }}</button>
        </div>
      </template>
    </section>
    <ConfirmDialog v-model="baseConfirmation" title="确认修改平台基础设置" :message="`平台名称：${baseConfirm.platformName}，系统时区：${baseConfirm.timezone}。`" warning-details="时区变化会影响页面时间与后续配置。已发布规则仍按原时区执行。" confirm-text="确认保存" :is-danger="false" @confirm="saveBase" />
    <RetentionJobPanel v-if="isOwner && activeTab === 'accessLog'" />
    <StorageObservationPanel v-if="isOwner && activeTab === 'accessLog'" />
    <ConfirmDialog v-model="confirmation" title="确认修改访问记录保留策略" :message="`允许记录保留 ${retentionConfirm.allowedDays} 天，阻断记录 ${retentionConfirm.blockedDays} 天，小时流量和账户明细 ${retentionConfirm.hourlyDays} 天。`" warning-details="缩短时间会清理超期记录。日流量仍长期保留。" confirm-text="确认保存" :is-danger="isShortening" @confirm="save" />
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { formatTime, isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import { refreshSettingsView } from '../../../app/events';
import { clearInstallEndpointCache } from '../../nodes/installEndpoint';
import { settingsAPI, type BaseSettings, type InstallEndpoint, type RetentionSettings } from '../settingsApi';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import BackupSchedulePanel from '../components/BackupSchedulePanel.vue';
import LatencyTargetsPanel from '../components/LatencyTargetsPanel.vue';
import RuntimeSettingsPanel from '../components/RuntimeSettingsPanel.vue';
import PanelUpdatePanel from '../components/PanelUpdatePanel.vue';
import RetentionJobPanel from '../components/RetentionJobPanel.vue';
import StorageObservationPanel from '../components/StorageObservationPanel.vue';
const route = useRoute();
const router = useRouter();
const tabs = [
  { key: 'base', label: '基础设置' },
  { key: 'network', label: '网络设置' },
  { key: 'security', label: '安全设置' },
  { key: 'accessLog', label: '访问记录设置' },
  { key: 'latency', label: '延迟监控' },
  { key: 'subscription', label: '订阅设置' },
  { key: 'backup', label: '备份设置' },
  { key: 'info', label: '系统信息' }
];
const activeTab = ref('accessLog'), saved = ref<RetentionSettings | null>(null), loading = ref(false), saving = ref(false), error = ref(''), confirmation = ref(false);
const form = reactive({ allowedDays: 30, blockedDays: 90, hourlyDays: 90 });
const retentionConfirm = ref({ allowedDays: 0, blockedDays: 0, hourlyDays: 0 });
const runtimeTab = computed(() => {
  if (['network', 'security', 'subscription', 'info'].includes(activeTab.value)) {
    return activeTab.value as 'network' | 'security' | 'subscription' | 'info';
  }
  return 'info';
});

watch(
  () => route.query.tab,
  (tab) => {
    if (tab === 'clashRules' || tab === 'clash') {
      router.replace('/routing/client');
      return;
    } else if (tab && typeof tab === 'string' && tabs.some(t => t.key === tab)) {
      activeTab.value = tab;
    }
  },
  { immediate: true }
);

const isShortening = computed(() => !!saved.value && (retentionConfirm.value.allowedDays < saved.value.policy.allowedDays || retentionConfirm.value.blockedDays < saved.value.policy.blockedDays || retentionConfirm.value.hourlyDays < saved.value.policy.hourlyDays));
async function load() { if (loading.value || saving.value) return; loading.value = true; try { const result = await settingsAPI.retention(); saved.value = result; Object.assign(form, result.policy); error.value = ''; } catch (e) { error.value = (e as Error).message; } finally { loading.value = false; } }
function confirmSave() { if (saving.value || !saved.value) return; if (Object.values(form).some(value => !Number.isInteger(value) || value < 1 || value > 3650)) { showToast('保留天数须为 1～3650 的整数', 'error'); return; } retentionConfirm.value = { allowedDays: form.allowedDays, blockedDays: form.blockedDays, hourlyDays: form.hourlyDays }; confirmation.value = true; }
async function save() { if (saving.value || !saved.value) return; saving.value = true; try { const result = await settingsAPI.saveRetention({ ...retentionConfirm.value }, saved.value.revision); saved.value = result; Object.assign(form, result.policy); showToast('保留策略已保存，后台将按新策略清理'); error.value = ''; } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; } }
const baseSaved = ref<BaseSettings | null>(null), baseLoading = ref(false), baseSaving = ref(false), baseError = ref(''), baseConfirmation = ref(false);
const baseForm = reactive({ platformName: '', timezone: '' });
const baseConfirm = ref({ platformName: '', timezone: '' });
let baseRefreshGeneration = 0;
async function loadBase() {
  if (baseSaving.value) return;
  const generation = ++baseRefreshGeneration; baseLoading.value = true;
  try { const result = await settingsAPI.base(); if (generation !== baseRefreshGeneration) return; baseSaved.value = result; Object.assign(baseForm, { platformName: result.platformName, timezone: result.timezone }); baseError.value = ''; }
  catch (e) { if (generation === baseRefreshGeneration) baseError.value = (e as Error).message; }
  finally { if (generation === baseRefreshGeneration) baseLoading.value = false; }
}
function confirmBase() { if (baseSaving.value || !baseSaved.value) return; if (!baseForm.platformName.trim() || !baseForm.timezone.trim()) { showToast('请填写平台名称和明确的系统时区', 'error'); return; } baseConfirm.value = { platformName: baseForm.platformName, timezone: baseForm.timezone }; baseConfirmation.value = true; }
async function saveBase() {
  if (baseSaving.value || !baseSaved.value) return;
  baseSaving.value = true;
  try { const result = await settingsAPI.saveBase(baseConfirm.value.platformName, baseConfirm.value.timezone, baseSaved.value.revision); baseSaved.value = result; Object.assign(baseForm, { platformName: result.platformName, timezone: result.timezone }); await refreshSettingsView(); showToast('基础设置已保存'); baseError.value = ''; }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally { baseSaving.value = false; }
}
function settingsChanged() { if (baseSaved.value && (baseForm.platformName !== baseSaved.value.platformName || baseForm.timezone !== baseSaved.value.timezone)) { showToast('其他页面已更新基础设置。请核对时区并刷新后再提交', 'warning'); return; } void loadBase(); }

// The panel address a new node uses for one-click Agent installation. It is
// edited once here instead of per server in the install dialogs.
const endpointSaved = ref<InstallEndpoint | null>(null), endpointLoading = ref(false), endpointSaving = ref(false);
const endpointError = ref(''), endpointFormError = ref('');
// The API uses 0 for "no port saved" and the input has to show that as an empty
// field with its placeholder: a 0 in a number input reads as a real port that
// fails validation. The form therefore keeps '' and the helpers below translate
// at the API boundary.
const endpointForm = reactive<{ host: string; httpPort: number | ''; grpcHost: string; grpcPort: number | '' }>({ host: '', httpPort: '', grpcHost: '', grpcPort: '' });
let endpointGeneration = 0;
function endpointPortInput(port: number): number | '' { return port >= 1 ? port : ''; }
function fillEndpointForm(result: InstallEndpoint) {
  Object.assign(endpointForm, { host: result.host, httpPort: endpointPortInput(result.httpPort), grpcHost: result.grpcHost ?? '', grpcPort: endpointPortInput(result.grpcPort ?? 0) });
}
async function loadEndpoint() {
  if (endpointSaving.value) return;
  const generation = ++endpointGeneration; endpointLoading.value = true;
  try {
    const result = await settingsAPI.installEndpoint();
    if (generation !== endpointGeneration) return;
    endpointSaved.value = result;
    fillEndpointForm(result);
    endpointError.value = ''; endpointFormError.value = '';
  } catch (e) {
    if (generation === endpointGeneration) endpointError.value = (e as Error).message;
  } finally {
    if (generation === endpointGeneration) endpointLoading.value = false;
  }
}
async function saveEndpoint() {
  if (endpointSaving.value || !endpointSaved.value) return;
  const host = endpointForm.host.trim(), grpcHost = endpointForm.grpcHost.trim();
  // An empty field and a cleared number input both mean "未设置" (0 on the wire).
  const port = (value: number | '') => value === '' ? 0 : Math.trunc(Number(value));
  const rawHTTP = port(endpointForm.httpPort), rawGRPC = port(endpointForm.grpcPort);
  const httpPort = Number.isFinite(rawHTTP) ? rawHTTP : 0, grpcPort = Number.isFinite(rawGRPC) ? rawGRPC : 0;
  // Mirrors the server's and the installer's rule: a DNS name or IPv4 literal
  // without scheme or port, and each port or nothing.
  if (host && !/^[A-Za-z0-9.-]+$/.test(host)) { endpointFormError.value = '面板地址只能是域名或 IPv4（字母、数字、点、连字符，不含端口或协议）。'; return; }
  if (grpcHost && !/^[A-Za-z0-9.-]+$/.test(grpcHost)) { endpointFormError.value = 'Agent gRPC 地址只能是域名或 IPv4（字母、数字、点、连字符，不含端口或协议）。'; return; }
  if (httpPort !== 0 && (httpPort < 1 || httpPort > 65535)) { endpointFormError.value = '面板 Web 端口须为 1～65535 的整数；留空表示未设置。'; return; }
  if (grpcPort !== 0 && (grpcPort < 1 || grpcPort > 65535)) { endpointFormError.value = 'Agent gRPC 端口须为 1～65535 的整数；留空表示用面板实际监听端口。'; return; }
  if (httpPort !== 0 && httpPort === grpcPort) { endpointFormError.value = '面板 Web 端口与 Agent gRPC 端口不能相同：面板的两个监听不能共用一个端口。'; return; }
  endpointSaving.value = true; endpointFormError.value = '';
  try {
    const result = await settingsAPI.saveInstallEndpoint(host, httpPort, grpcHost, grpcPort, endpointSaved.value.revision);
    endpointSaved.value = result;
    fillEndpointForm(result);
    // Other install dialogs read this once per page load; drop that copy so the
    // next dialog uses the value just saved.
    clearInstallEndpointCache();
    endpointError.value = '';
    showToast('安装连接地址已保存');
  } catch (e) {
    // A stale revision means another session saved meanwhile: re-read and let
    // the Owner confirm against the current value.
    if (e instanceof APIError && e.status === 409) {
      showToast('安装连接地址已在其他页面修改，已刷新为最新值，请核对后重试', 'error');
      endpointSaving.value = false;
      void loadEndpoint();
    } else {
      endpointFormError.value = (e as Error).message;
    }
  } finally {
    endpointSaving.value = false;
  }
}
onUnmounted(() => { baseRefreshGeneration++; endpointGeneration++; window.removeEventListener('proxy-system-settings', settingsChanged); });
onMounted(() => { void load(); void loadBase(); if (isOwner.value) void loadEndpoint(); window.addEventListener('proxy-system-settings', settingsChanged); });
</script>
