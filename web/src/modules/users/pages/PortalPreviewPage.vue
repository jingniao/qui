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
      <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-if="loading" />
    </div>
    <PortalLayout v-if="snapshot && !loading" :key="selected" class="portal-preview-content" preview :snapshot="snapshot" />
  </div>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { userAPI, type ProxyUser } from '../api';
import { request } from '../../../app/apiClient';
import type { Snapshot } from '../../../portal/api';
import PortalLayout from '../../../portal/PortalLayout.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const route = useRoute(), users = ref<ProxyUser[]>([]), selected = ref(''), snapshot = ref<Snapshot>(), error = ref(''), loading = ref(false), page = ref(1), total = ref(0);
let generation = 0;
async function list(target: number) {
  try { const result = await userAPI.list(target, '', 50); users.value = result.items; total.value = result.total; page.value = target; }
  catch (e) { error.value = (e as Error).message; }
}
async function load() {
  const current = ++generation; snapshot.value = undefined; error.value = ''; loading.value = false;
  if (!selected.value) return;
  loading.value = true;
  try { const result = await request<Snapshot>(`/users/${encodeURIComponent(selected.value)}/portal-preview`); if (current === generation) snapshot.value = result; }
  catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
onMounted(async () => { await list(1); if (typeof route.query.user === 'string') { selected.value = route.query.user; await load(); } });
</script>

<style scoped>
/* The preview toolbar already provides the return link. */
.portal-preview-content :deep(header > a[href="/users"]) {
  display: none;
}
</style>
