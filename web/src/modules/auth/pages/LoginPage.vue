<template>
  <main class="min-h-screen bg-[#F2F2F7] flex items-center justify-center p-4">
    <section class="panel-card w-full max-w-md p-8 space-y-6">
      <div class="text-center"><h1 class="text-2xl font-bold">Proxy Control</h1><p class="text-sm text-[#8E8E93] mt-2">登录代理控制平台</p></div>
      <p v-if="errorMessage || authState.error" role="alert" class="rounded-xl bg-red-50 text-red-700 p-3 text-sm">{{ errorMessage || authState.error }}</p>
      <form @submit.prevent="handleLogin" class="space-y-4">
        <label class="block text-sm">用户名<input v-model="username" autocomplete="username" required maxlength="64" class="form-input-base mt-2" /></label>
        <label class="block text-sm">密码<input v-model="password" type="password" autocomplete="current-password" required maxlength="128" class="form-input-base mt-2" /></label>
        <button :disabled="isSubmitting" class="ios-btn-primary w-full justify-center disabled:opacity-50">{{ isSubmitting ? '正在登录…' : '登录' }}</button>
      </form>
      <button v-if="authState.error" @click="retryConnection" class="text-sm text-[#007AFF]">重新连接服务</button>
    </section>
  </main>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { authState, login, refreshSession } from '../../../app/auth';
const router = useRouter(); const route = useRoute();
const username = ref(''); const password = ref('');
const isSubmitting = ref(false); const errorMessage = ref('');
async function handleLogin() {
  if (isSubmitting.value) return;
  isSubmitting.value = true; errorMessage.value = '';
  try {
    await login(username.value, password.value);
    password.value = '';
    const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/dashboard';
    await router.replace(redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/dashboard');
  } catch (error) { errorMessage.value = (error as Error).message; }
  finally { isSubmitting.value = false; }
}
async function retryConnection() {
  try { await refreshSession(); if (!authState.isInitialized) await router.replace('/setup'); }
  catch (error) { errorMessage.value = (error as Error).message; }
}
</script>

