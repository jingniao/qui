<template>
  <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
    <div class="flex flex-wrap justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">证书与 DNS</h2><button class="ios-btn-secondary" :disabled="loading || saving" @click="reload">刷新</button></div>
    <ErrorState v-if="error" :message="error" @retry="reload" /><LoadingSkeleton v-else-if="!saved" />
    <template v-else>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <label>ACME 邮箱<input v-model.trim="form.email" type="email" maxlength="254" placeholder="admin@example.com" class="form-input-base mt-2" /></label>
        <label>CA 环境
          <select v-model="form.directory" class="form-input-base mt-2"><option value="production">正式（Let's Encrypt）</option><option value="staging">测试（staging，证书不受信任，用于先试流程）</option></select>
        </label>
        <label>自动发布开始（{{ timezone }}）
          <select v-model.number="form.deployStartHour" class="form-input-base mt-2"><option v-for="hour in hours" :key="hour" :value="hour">{{ hourText(hour) }}</option></select>
        </label>
        <label>自动发布结束（{{ timezone }}，不含）
          <select v-model.number="form.deployEndHour" class="form-input-base mt-2"><option v-for="hour in hours" :key="hour" :value="hour">{{ hourText(hour) }}</option></select>
        </label>
      </div>
      <p class="text-xs text-gray-500">续期后的证书在这个时段自动发布到节点；发布会重启该节点全部核心，现有连接会短暂中断。</p>
      <div class="space-y-2">
        <label class="block">Cloudflare API Token
          <input v-model="form.token" type="password" autocomplete="off" maxlength="256" placeholder="粘贴新的 Token 以替换" class="form-input-base mt-2 font-mono" />
        </label>
        <p class="text-xs text-gray-500">{{ cloudflareText }}<button v-if="saved.cloudflare.configured" type="button" class="ml-3 text-ios-red font-medium hover:underline" :disabled="saving" @click="clearConfirmation = true">清除</button></p>
        <p class="text-xs text-gray-500 leading-relaxed">在 Cloudflare 创建只授予「区域 → DNS → 编辑」权限的 API Token，不要使用 Global API Key。Token 加密保存，不会再显示。</p>
      </div>
      <p v-if="formError" role="alert" class="text-sm text-ios-red">{{ formError }}</p>
      <div class="flex flex-wrap justify-between items-center gap-3">
        <span class="text-xs text-gray-500">{{ saved.revision ? `设置版本 ${saved.revision}` : '当前未设置' }}</span>
        <button class="ios-btn-primary" :disabled="saving" @click="save()">{{ saving ? '保存中…' : '保存证书设置' }}</button>
      </div>

      <div class="border-t border-black/[0.06] pt-5 space-y-4">
        <div class="flex flex-wrap justify-between items-center gap-3">
          <h3 class="text-base font-semibold leading-normal">证书列表</h3>
          <button v-if="issuingReady" class="ios-btn-primary" @click="openRequest">申请证书</button>
        </div>
        <p v-if="!issuingReady" class="text-xs text-gray-500">请先填写并保存上面的 ACME 邮箱和 Cloudflare API Token，再申请证书。</p>
        <p v-if="listError" role="alert" class="text-sm text-ios-red">{{ listError }}</p>
        <p v-if="!certificates.length" class="text-xs text-gray-500">还没有证书。</p>
        <ul v-else class="space-y-3">
          <li v-for="item in certificates" :key="item.id" class="rounded-2xl border border-black/[0.08] p-4 space-y-2">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span class="font-mono break-all">{{ item.domains.join('、') }}</span>
              <span :class="['rounded px-2 py-0.5 text-xs font-medium', badgeClass(item.status)]">{{ CERTIFICATE_STATUS_LABELS[item.status] }}</span>
            </div>
            <p v-if="item.notAfter" class="text-xs text-gray-500">到期：{{ formatTime(item.notAfter) }}</p>
            <p v-if="item.lastErrorText" role="alert" class="text-xs text-ios-red">{{ item.lastErrorText }}</p>
            <div class="flex flex-wrap items-center justify-between gap-2">
              <span v-if="item.usedBy.length" class="text-xs text-gray-500">使用中：{{ item.usedBy.map(use => use.name).join('、') }}</span>
              <span v-else class="text-xs text-gray-500">未被入站使用</span>
              <span class="flex items-center gap-2">
                <button v-if="item.status === 'valid' || item.status === 'failed'" class="ios-btn-secondary" :disabled="busyId === item.id" @click="renew(item)">续期</button>
                <button v-if="!item.usedBy.length" class="ios-btn-secondary text-ios-red" :disabled="busyId === item.id" @click="deleting = item">删除</button>
              </span>
            </div>
          </li>
        </ul>
      </div>
    </template>
    <ConfirmDialog v-model="clearConfirmation" title="清除 Cloudflare Token" message="清除后不能再申请或续期证书，已签发的证书继续有效。" warning-details="自动申请的证书到期前须重新保存 Token，否则无法续期。" confirm-text="确认清除" @confirm="save('')" />
    <ConfirmDialog :model-value="!!deleting" title="删除证书" :message="`确认删除证书 ${deleting?.domains.join('、') || ''}？`" confirm-text="确认删除" @update:model-value="deleting = null" @confirm="removeCertificate" />
    <AppDialog v-model="requesting" title="申请证书" max-width="md">
      <div class="space-y-4 text-sm">
        <label class="block">域名（每行一个）
          <textarea v-model="domainText" rows="4" placeholder="node.example.com&#10;*.example.com" class="form-input-base mt-2 font-mono" />
          <span class="block text-xs text-gray-500 mt-1">1～10 个域名，可带前导 *.（通配）；域名须在 Cloudflare 账号的某个区域下。</span>
        </label>
        <label class="block"><input v-model="autoDns" type="checkbox" /> 自动把域名解析到节点</label>
        <label v-if="autoDns" class="block">节点
          <select v-model="dnsNodeId" class="form-input-base mt-2"><option value="" disabled>请选择节点</option><option v-for="node in dnsNodes" :key="node.id" :value="node.id">{{ node.name }}（{{ node.publicIp }}）</option></select>
          <span class="block text-xs text-gray-500 mt-1">会在 Cloudflare 写入 A/AAAA 记录并关闭代理（灰云）</span>
        </label>
        <p v-if="requestError" role="alert" class="text-sm text-ios-red">{{ requestError }}</p>
      </div>
      <template #footer>
        <button type="button" class="ios-dialog-btn-secondary" @click="requesting = false">取消</button>
        <button type="button" class="ios-dialog-btn-primary" :disabled="requestBusy" @click="submitRequest">{{ requestBusy ? '提交中…' : '申请' }}</button>
      </template>
    </AppDialog>
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue';
import { authState, formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { choices } from '../../proxy/api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import { CERTIFICATE_STATUS_LABELS, certificatesAPI, certificatesInProgress, deployWindowError, parseDomainLines, type CertificateSettings, type CertificateStatus, type ManagedCertificate } from '../certificatesApi';

const timezone = computed(() => authState.timezone);
const hours = Array.from({ length: 24 }, (_, hour) => hour);
const hourText = (hour: number) => `${String(hour).padStart(2, '0')}:00`;
const saved = ref<CertificateSettings | null>(null), loading = ref(false), saving = ref(false), error = ref(''), formError = ref('');
const form = reactive({ email: '', directory: 'production' as 'production' | 'staging', deployStartHour: 3, deployEndHour: 6, token: '' });
const certificates = ref<ManagedCertificate[]>([]), listError = ref(''), busyId = ref('');
const clearConfirmation = ref(false), deleting = ref<ManagedCertificate | null>(null);
const requesting = ref(false), requestBusy = ref(false), requestError = ref(''), domainText = ref(''), autoDns = ref(false), dnsNodeId = ref('');
const dnsNodes = ref<NodeView[]>([]);
let disposed = false, timer = 0;

const cloudflareText = computed(() => {
  const status = saved.value?.cloudflare;
  return status?.configured ? `已设置（末 4 位 ${status.last4}，验证于 ${formatTime(status.verifiedAt)}）` : '未设置';
});
// The saved values decide whether issuing can start, not the unsaved form.
const issuingReady = computed(() => !!saved.value?.email && !!saved.value.cloudflare.configured);
const badgeClass = (status: CertificateStatus) => status === 'valid' ? 'bg-ios-green/10 text-ios-green' : status === 'failed' ? 'bg-ios-red/10 text-ios-red' : 'bg-ios-blue/10 text-ios-blue';

function fill(result: CertificateSettings) {
  saved.value = result;
  Object.assign(form, { email: result.email, directory: result.directory, deployStartHour: result.deployStartHour, deployEndHour: result.deployEndHour, token: '' });
}
async function loadSettings() {
  loading.value = true;
  try { fill(await certificatesAPI.settings()); error.value = ''; }
  catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
async function loadList() {
  try {
    const items = await certificatesAPI.list();
    if (disposed) return;
    certificates.value = items; listError.value = '';
  } catch (e) { if (!disposed) listError.value = (e as Error).message; }
  schedulePoll();
}
// Issuance runs in the background and is not pushed over SSE, so the list is polled
// while a certificate is moving and the page is visible.
function schedulePoll() {
  window.clearTimeout(timer);
  if (disposed || !certificatesInProgress(certificates.value)) return;
  timer = window.setTimeout(() => { if (document.visibilityState === 'visible') void loadList(); else schedulePoll(); }, 5000);
}
function reload() { void loadSettings(); void loadList(); }

// token undefined keeps the stored one, '' clears it, a typed value replaces it.
async function save(clearToken?: ''): Promise<void> {
  if (saving.value || !saved.value) return;
  const windowError = deployWindowError(form.deployStartHour, form.deployEndHour);
  if (windowError) { formError.value = windowError; return; }
  const cloudflareToken = clearToken ?? (form.token.trim() || undefined);
  saving.value = true; formError.value = '';
  try {
    fill(await certificatesAPI.saveSettings({ email: form.email, directory: form.directory, deployStartHour: form.deployStartHour, deployEndHour: form.deployEndHour, cloudflareToken, expectedRevision: saved.value.revision }));
    showToast('证书设置已保存');
  } catch (e) {
    formError.value = (e as Error).message;
    if ((e as { status?: number }).status === 409 && (e as { code?: string }).code === 'settings_conflict') void loadSettings();
  } finally { saving.value = false; }
}

function openRequest() {
  domainText.value = ''; autoDns.value = false; dnsNodeId.value = ''; requestError.value = '';
  requesting.value = true;
  void choices((page, size) => nodeAPI.list(page, '', false, size)).then(nodes => { dnsNodes.value = nodes.filter(node => !node.deletedAt && node.publicIp); }, () => { dnsNodes.value = []; });
}
async function submitRequest() {
  const domains = parseDomainLines(domainText.value);
  if (!domains.length) { requestError.value = '请至少填写一个域名'; return; }
  if (autoDns.value && !dnsNodeId.value) { requestError.value = '请选择要解析到的节点'; return; }
  requestBusy.value = true; requestError.value = '';
  try {
    await certificatesAPI.create(domains, autoDns.value, dnsNodeId.value);
    requesting.value = false;
    showToast('已提交证书申请');
    await loadList();
  } catch (e) { requestError.value = (e as Error).message; }
  finally { requestBusy.value = false; }
}
async function renew(item: ManagedCertificate) {
  busyId.value = item.id;
  try { await certificatesAPI.renew(item.id); showToast('已提交续期'); await loadList(); }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally { busyId.value = ''; }
}
async function removeCertificate() {
  const item = deleting.value;
  if (!item) return;
  busyId.value = item.id;
  try { await certificatesAPI.remove(item.id); showToast('证书已删除'); await loadList(); }
  catch (e) { showToast((e as Error).message, 'error'); await loadList(); }
  finally { busyId.value = ''; deleting.value = null; }
}
onMounted(reload);
onUnmounted(() => { disposed = true; window.clearTimeout(timer); });
</script>
