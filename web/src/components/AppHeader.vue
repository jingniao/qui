<template>
  <header class="h-16 bg-white border-b border-black/[0.06] px-4 sm:px-6 flex items-center justify-between shrink-0">
    <div class="flex items-center gap-3">
      <button @click="$emit('toggle-sidebar')" aria-label="打开导航" class="lg:hidden text-xl">☰</button>
      <span class="text-sm text-[#8E8E93]">{{ route.meta.parent }}</span><span class="text-sm font-semibold">{{ route.meta.title }}</span>
    </div>
    <div class="flex items-center gap-3 text-xs">
      <span>{{ authState.currentUser?.displayName || authState.currentUser?.username }}</span>
      <span class="rounded-full bg-blue-50 text-blue-700 px-2 py-1">{{ authState.currentUser?.role }}</span>
      <button @click="passwordVisible = true" class="text-[#007AFF]">修改密码</button>
      <button @click="handleLogout" class="text-red-600">退出</button>
    </div>
  </header>
  <FormDrawer v-model="passwordVisible" title="修改登录密码" description="修改后所有会话将失效，需要重新登录。" @confirm="submitPassword" :loading="busy">
    <div class="space-y-4">
      <label class="block text-sm">当前密码<input v-model="currentPassword" type="password" autocomplete="current-password" class="form-input-base mt-2" /></label>
      <label class="block text-sm">新密码<input v-model="newPassword" type="password" autocomplete="new-password" minlength="12" maxlength="128" class="form-input-base mt-2" /></label>
      <label class="block text-sm">确认新密码<input v-model="confirmPassword" type="password" autocomplete="new-password" class="form-input-base mt-2" /></label>
    </div>
  </FormDrawer>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { authState, logout, changePassword } from '../app/auth';
import { showToast } from '../app/api';
import FormDrawer from './FormDrawer.vue';
defineEmits<{ (event: 'toggle-sidebar'): void }>();
const route = useRoute(); const router = useRouter();
const passwordVisible = ref(false); const currentPassword = ref(''); const newPassword = ref(''); const confirmPassword = ref(''); const busy = ref(false);
async function handleLogout() {
  try { await logout(); await router.replace('/login'); }
  catch (error) { showToast((error as Error).message, 'error'); }
}
async function submitPassword() {
  if (busy.value) return;
  if (newPassword.value !== confirmPassword.value) { showToast('两次输入的新密码不一致', 'error'); return; }
  if (newPassword.value.length < 12) { showToast('新密码至少需要 12 个字符', 'error'); return; }
  busy.value = true;
  try { await changePassword(currentPassword.value, newPassword.value); passwordVisible.value = false; currentPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; await router.replace('/login'); }
  catch (error) { showToast((error as Error).message, 'error'); }
  finally { busy.value = false; }
}
</script>

