<template>
  <div class="bg-[#F8F8FA] min-h-screen">
    <div class="max-w-6xl mx-auto p-6 space-y-3">
      <label class="text-sm font-semibold">选择预览用户
        <select v-model="selected" class="form-input-base mt-2" @change="load">
          <option value="">请选择用户</option><option v-for="user in users" :key="user.id" :value="user.id">{{ user.displayName || user.username }}</option>
        </select>
      </label>
      <div class="flex gap-2"><button :disabled="page <= 1" class="ios-btn-secondary" @click="list(page - 1)">上一页</button><button :disabled="page * 50 >= total" class="ios-btn-secondary" @click="list(page + 1)">下一页</button><router-link to="/users" class="ios-btn-secondary">返回用户管理</router-link></div>
      <p class="text-xs text-ios-secondary">仅查看账号、节点与现有订阅，不创建 Token，不进入用户会话。</p>
      <ErrorState v-if="listError" :message="listError" @retry="list(requestedPage)" />
      <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-if="loading && !snapshot" />
    </div>
    <PortalLayout v-if="snapshot" :key="selected" class="portal-preview-content" preview :snapshot="snapshot" />
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { userAPI, type UserChoice } from '../api';
import { invalidatesRead, request } from '../../../app/apiClient';
import type { Snapshot } from '../../../portal/api';
import PortalLayout from '../../../portal/PortalLayout.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const route = useRoute(), users = ref<UserChoice[]>([]), selected = ref(''), snapshot = ref<Snapshot>(), error = ref(''), loading = ref(false), page = ref(1), total = ref(0);
let generation = 0, listGeneration = 0;
const requestedPage = ref(1), listError = ref('');
let snapshotUser = '';
async function list(target: number) {
  const current = ++listGeneration;
  requestedPage.value = target; listError.value = '';
  try { const result = await userAPI.choices(target, '', 50); if (current !== listGeneration) return; users.value = result.items; total.value = result.total; page.value = target; }
  catch (e) {
    if (current !== listGeneration) return;
    if (invalidatesRead(e)) { generation++; snapshot.value = undefined; users.value = []; total.value = 0; loading.value = false; }
    listError.value = (e as Error).message;
  }
}
async function load() {
  const current = ++generation;
  if (selected.value !== snapshotUser) snapshot.value = undefined;
  snapshotUser = selected.value; error.value = ''; loading.value = false;
  if (!selected.value) return;
  loading.value = true;
  try { const result = await request<Snapshot>(`/users/${encodeURIComponent(selected.value)}/portal-preview`); if (current === generation) snapshot.value = result; }
  catch (e) { if (current === generation) { if (invalidatesRead(e)) snapshot.value = undefined; error.value = (e as Error).message; } }
  finally { if (current === generation) loading.value = false; }
}
onMounted(() => { if (typeof route.query.user === 'string') selected.value = route.query.user; void load(); void list(1); });
</script>

<style scoped>
/* The preview toolbar already provides the return link. */
.portal-preview-content :deep(header > a[href="/users"]) {
  display: none;
}
</style>
