<template>
  <div class="space-y-6">
    <section class="panel-card p-6 space-y-2">
      <h2 class="text-base font-semibold leading-normal">账号信息</h2>
      <dl class="grid grid-cols-1 sm:grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
        <dt class="text-ios-secondary">用户名</dt><dd>{{ me.username }}</dd>
        <dt class="text-ios-secondary">显示名称</dt><dd>{{ me.displayName || '—' }}</dd>
        <dt v-if="sessionExpiresAt" class="text-ios-secondary">本次登录到期</dt><dd v-if="sessionExpiresAt">{{ formatDateTime(sessionExpiresAt) }}</dd>
      </dl>
    </section>
    <form class="panel-card p-6 space-y-4" @submit.prevent="submit">
      <h2 class="text-base font-semibold leading-normal">修改登录密码</h2>
      <label class="block text-sm">当前密码<input v-model="currentPassword" :readonly="preview" type="password" autocomplete="current-password" required class="form-input-base mt-2" /></label>
      <label class="block text-sm">新密码<input v-model="newPassword" :readonly="preview" type="password" autocomplete="new-password" required class="form-input-base mt-2" /></label>
      <label class="block text-sm">确认新密码<input v-model="confirmPassword" :readonly="preview" type="password" autocomplete="new-password" required class="form-input-base mt-2" /></label>
      <p class="text-xs text-ios-tertiary">{{ PASSWORD_RULE }}修改后其他登录会话立即退出。</p>
      <button v-if="!preview" :disabled="busy" class="ios-btn-primary">保存密码</button>
      <p v-if="notice" role="status" class="text-ios-blue text-sm">{{ notice }}</p>
    </form>
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { portalAPI, portalState, type Profile } from './api';
import { PASSWORD_RULE, formatDateTime } from './format';
const props = defineProps<{ me: Profile; preview?: boolean }>();
const sessionExpiresAt = props.preview ? '' : portalState.session?.expiresAt ?? '';
const currentPassword = ref(''), newPassword = ref(''), confirmPassword = ref(''), notice = ref(''), busy = ref(false);
async function submit() {
  if (props.preview || busy.value) return;
  if (newPassword.value !== confirmPassword.value) { notice.value = '两次输入的新密码不一致'; return; }
  const length = Array.from(newPassword.value).length;
  if (length < 12 || length > 128) { notice.value = PASSWORD_RULE; return; }
  busy.value = true;
  try { await portalAPI.password(currentPassword.value, newPassword.value); currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; notice.value = '密码已修改，其他会话已退出'; }
  catch (e) { notice.value = (e as Error).message; } finally { busy.value = false; }
}
</script>
