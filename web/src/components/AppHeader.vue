<template>
  <header class="h-16 bg-white border-b border-black/[0.08] px-4 sm:px-6 flex items-center justify-between shrink-0 shadow-2xs gap-3">
    <div class="flex items-center gap-3 min-w-0">
      <button @click="$emit('toggle-sidebar')" aria-label="打开导航" class="lg:hidden text-xl text-ios-label">☰</button>
    </div>

    <!-- Center/Right Mode Switcher (Owner only) -->
    <div class="flex items-center gap-3 text-xs shrink-0">
      <router-link v-if="isOwner" to="/portal-preview" class="ios-btn-secondary text-xs">前台使用 · 只读预览</router-link>

      <div class="h-4 w-px bg-black/[0.08] hidden sm:block"></div>

      <!-- User Information -->
      <span class="font-medium text-ios-label hidden md:inline">{{ authState.currentUser?.displayName || authState.currentUser?.username }}</span>
      <span class="rounded-full bg-ios-blue/10 text-ios-blue border border-ios-blue/20 px-2 py-0.5 text-[11px] font-medium">{{ authState.currentUser?.role }}</span>
      <button @click="passwordVisible = true" class="text-ios-blue hover:underline font-medium">修改密码</button>
      <button @click="handleLogout" class="text-ios-red hover:underline font-medium">退出</button>
    </div>
  </header>

  <FormDrawer v-model="passwordVisible" title="修改登录密码" description="修改后所有会话将失效，需要重新登录。" @confirm="submitPassword" :loading="busy">
    <div class="space-y-4">
      <label class="block text-xs font-medium text-ios-secondary">当前密码<input v-model="currentPassword" type="password" class="form-input-base mt-1.5" autocomplete="current-password" /></label>
      <label class="block text-xs font-medium text-ios-secondary">新密码<input v-model="newPassword" type="password" class="form-input-base mt-1.5" autocomplete="new-password" /></label>
      <label class="block text-xs font-medium text-ios-secondary">确认新密码<input v-model="confirmPassword" type="password" class="form-input-base mt-1.5" autocomplete="new-password" /></label>
    </div>
  </FormDrawer>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { authState, isOwner, logout, clearSession } from '../app/auth';
import { request } from '../app/apiClient';
import { showToast } from '../app/api';
import FormDrawer from './FormDrawer.vue';

defineEmits<{
  (e: 'toggle-sidebar'): void;
}>();

const router = useRouter();

const passwordVisible = ref(false);
const currentPassword = ref('');
const newPassword = ref('');
const confirmPassword = ref('');
const busy = ref(false);

async function handleLogout() {
  try {
    await logout();
  } catch {
    // Ignore logout failures
  } finally {
    clearSession();
    router.push('/login');
  }
}

async function submitPassword() {
  if (!currentPassword.value || !newPassword.value) {
    showToast('请输入当前密码和新密码', 'error');
    return;
  }
  if (newPassword.value !== confirmPassword.value) {
    showToast('两次输入的新密码不一致', 'error');
    return;
  }
  busy.value = true;
  try {
    await request('/auth/password', 'PUT', { currentPassword: currentPassword.value, newPassword: newPassword.value });
    passwordVisible.value = false;
    currentPassword.value = '';
    newPassword.value = '';
    confirmPassword.value = '';
    showToast('密码已修改，请重新登录');
    clearSession();
    await router.push('/login');
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    busy.value = false;
  }
}
</script>
