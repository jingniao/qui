<template>
  <main class="min-h-screen bg-[#F2F2F7] flex items-center justify-center p-4">
    <section class="panel-card w-full max-w-lg p-8 space-y-6">
      <div><h1 class="text-xl font-bold">初始化 Proxy Control</h1><p class="text-sm text-[#8E8E93] mt-2">创建唯一的 Owner，并设置平台名称与时区。</p></div>
      <p class="text-xs text-[#8E8E93]">首次启动时，面板会在日志中打印一次性安装令牌，并写入数据目录下的 setup-token 文件。请粘贴该令牌以完成初始化。</p>
      <p v-if="error" role="alert" class="p-3 rounded-xl bg-red-50 text-red-700 text-sm">{{ error }}</p>
      <form @submit.prevent="submit" class="space-y-4">
        <label class="block text-sm">安装令牌<input v-model="token" autocomplete="off" required class="form-input-base mt-2" /></label>
        <label class="block text-sm">平台名称<input v-model="platformName" required maxlength="40" class="form-input-base mt-2" /></label>
        <label class="block text-sm">Owner 用户名<input v-model="username" autocomplete="username" required minlength="3" maxlength="64" pattern="[a-zA-Z0-9_-]+" class="form-input-base mt-2" /></label>
        <label class="block text-sm">密码<input v-model="password" type="password" autocomplete="new-password" required minlength="12" maxlength="128" class="form-input-base mt-2" /></label>
        <label class="block text-sm">确认密码<input v-model="confirmPassword" type="password" autocomplete="new-password" required class="form-input-base mt-2" /></label>
        <label class="block text-sm">时区<select v-model="timezone" class="form-input-base mt-2"><option v-for="zone in zones" :key="zone">{{ zone }}</option></select></label>
        <button :disabled="busy" class="ios-btn-primary w-full justify-center disabled:opacity-50">{{ busy ? '正在初始化…' : '创建 Owner 并进入平台' }}</button>
      </form>
    </section>
  </main>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import { completeSetup } from '../../../app/auth';
const router = useRouter();
const token = ref('');
const platformName = ref('Proxy Control'); const username = ref('');
const password = ref(''); const confirmPassword = ref(''); const timezone = ref('Asia/Shanghai');
const zones = ['Asia/Shanghai', 'Asia/Tokyo', 'America/Los_Angeles', 'Europe/London', 'UTC'];
const busy = ref(false); const error = ref('');
async function submit() {
  if (busy.value) return;
  if (password.value !== confirmPassword.value) { error.value = '两次输入的密码不一致。'; return; }
  busy.value = true; error.value = '';
  try { await completeSetup(token.value, username.value, password.value, platformName.value, timezone.value); password.value = ''; confirmPassword.value = ''; await router.replace('/dashboard'); }
  catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
</script>

