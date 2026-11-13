<template>
  <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
    <div class="flex justify-between items-center gap-3"><h2 class="text-base font-semibold leading-normal">管理员登录地址</h2><button class="ios-btn-secondary" :disabled="loading || saving" @click="load">刷新已保存地址</button></div>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="!saved" />
    <template v-else>
      <label class="block">登录地址路径<input v-model.trim="path" maxlength="64" placeholder="/login" class="form-input-base mt-2 font-mono" /></label>
      <p class="text-xs text-gray-500 leading-relaxed">当前完整地址：<span class="font-mono">{{ adminEntryURL(origin, saved.path) }}</span>。用户在面板根地址 <span class="font-mono">{{ origin }}/</span> 登录门户；管理员从这里设置的地址登录。填写以 / 开头的单段路径，4～64 个字符，只含小写字母、数字、- 和 _，例如 /manage-7f3k。</p>
      <p class="text-xs text-gray-500">这只是隐藏入口、减少扫描，不是新的安全边界：认证、CSRF、登录限流与会话规则都不变，已登录的管理员会话不受影响。忘记地址时可在服务器上运行 qui 菜单重置。</p>
      <p v-if="formError" role="alert" class="text-sm text-ios-red">{{ formError }}</p>
      <div class="flex flex-wrap justify-between items-center gap-3"><span class="text-xs text-gray-500">{{ saved.revision ? `版本 ${saved.revision} · ${saved.updatedBy} · ${formatTime(saved.updatedAt)}` : '当前为默认地址' }}</span><button class="ios-btn-primary" :disabled="loading || saving" @click="requestSave">{{ saving ? '保存中…' : '保存管理员登录地址' }}</button></div>
    </template>
    <ConfirmDialog v-model="confirmVisible" title="确认修改管理员登录地址" :message="`新的管理员登录地址：${adminEntryURL(origin, pending)}`" :warning-details="ADMIN_ENTRY_CONFIRM_NOTE" confirm-text="确认保存" :is-danger="false" @confirm="save" />
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { rememberAdminEntry } from '../../../app/adminEntry';
import { APIError } from '../../../app/apiClient';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import { settingsAPI, type AdminEntry } from '../settingsApi';
import { ADMIN_ENTRY_CONFIRM_NOTE, adminEntryPathError, adminEntryURL } from '../adminEntry';

const origin = window.location.origin;
const saved = ref<AdminEntry | null>(null), path = ref(''), pending = ref(''), formError = ref(''), error = ref('');
const loading = ref(false), saving = ref(false), confirmVisible = ref(false);

async function load() {
  if (loading.value || saving.value) return;
  loading.value = true;
  try { const result = await settingsAPI.adminEntry(); saved.value = result; path.value = result.path; error.value = ''; formError.value = ''; }
  catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
function requestSave() {
  formError.value = '';
  if (!saved.value) return;
  const value = path.value.trim();
  const problem = adminEntryPathError(value);
  if (problem) { formError.value = problem; return; }
  if (value === saved.value.path) { formError.value = '地址没有变化'; return; }
  pending.value = value;
  confirmVisible.value = true;
}
async function save() {
  if (saving.value || !saved.value) return;
  saving.value = true;
  try {
    const result = await settingsAPI.saveAdminEntry(pending.value, saved.value.revision);
    rememberAdminEntry(result.path);
    saved.value = result; path.value = result.path; formError.value = '';
    showToast(`管理员登录地址已更新：${adminEntryURL(origin, result.path)}`);
  } catch (e) {
    if (e instanceof APIError && e.code === 'settings_conflict') formError.value = '设置已被其他会话修改，请刷新后重试';
    else formError.value = (e as Error).message;
  } finally { saving.value = false; }
}
onMounted(load);
</script>
