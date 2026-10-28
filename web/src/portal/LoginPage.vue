<template>
  <main class="min-h-screen bg-[#F8F8FA] flex items-center justify-center p-6">
    <form class="panel-card p-6 space-y-5 w-full max-w-sm" @submit.prevent="login">
      <h1 class="text-xl font-semibold text-ios-label">用户登录</h1>
      <label class="block text-sm">用户名<input v-model="username" autocomplete="username" required class="form-input-base mt-2" /></label>
      <label class="block text-sm">密码<input v-model="password" type="password" autocomplete="current-password" required class="form-input-base mt-2" /></label>
      <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
      <button :disabled="busy" class="ios-btn-primary w-full">{{ busy ? '登录中…' : '登录' }}</button>
    </form>
  </main>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { portalAPI } from './api';
const router = useRouter();
const username = ref(''), password = ref(''), error = ref(''), busy = ref(false);
async function login() {
  busy.value = true; error.value = '';
  try { await portalAPI.login(username.value, password.value); password.value = ''; await router.replace('/portal/overview'); }
  catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
</script>
