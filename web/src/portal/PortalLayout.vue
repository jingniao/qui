<template>
  <div class="min-h-screen bg-[#F8F8FA] text-ios-label">
    <header class="bg-white border-b border-black/[0.08] px-6 py-4 flex justify-between items-center">
      <span class="font-semibold">我的账号 <span v-if="preview" class="text-ios-blue text-xs">只读预览</span></span>
      <router-link v-if="preview" to="/users" class="ios-btn-secondary">返回用户管理</router-link>
      <button v-else class="ios-btn-secondary" @click="logout">退出登录</button>
    </header>
    <main class="max-w-6xl mx-auto p-4 sm:p-6 space-y-6">
      <nav class="flex flex-wrap gap-2">
        <button v-for="item in sections" :key="item.id" class="ios-btn-secondary"
          :class="section === item.id ? 'text-ios-blue bg-blue-50' : ''" @click="navigate(item.id)">{{ item.label }}</button>
      </nav>
      <PortalPage :section="section" :preview="preview" :snapshot="snapshot" />
    </main>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { portalAPI, type Snapshot } from './api';
import PortalPage from './PortalPage.vue';
const props = defineProps<{ preview?: boolean; snapshot?: Snapshot }>();
const route = useRoute(), router = useRouter(), previewSection = ref('overview');
const sections = [{ id: 'overview', label: '概览' }, { id: 'subscription', label: '订阅' }, { id: 'nodes', label: '节点' }, { id: 'traffic', label: '流量' }, { id: 'account', label: '账号' }];
const section = computed(() => props.preview ? previewSection.value : String(route.params.section || 'overview'));
function navigate(id: string) { if (props.preview) previewSection.value = id; else void router.push('/portal/' + id); }
async function logout() {
  try { await portalAPI.logout(); await router.replace('/portal/login'); }
  catch (e) { window.alert((e as Error).message); }
}
</script>
