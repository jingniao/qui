<template>
  <div class="min-h-screen bg-[#F8F8FA] text-ios-label">
    <header class="bg-white border-b border-black/[0.08] px-4 sm:px-6 py-4 flex justify-between items-center gap-3">
      <span class="font-semibold truncate">{{ name }} <span v-if="preview" class="text-ios-blue text-xs">只读预览</span></span>
      <router-link v-if="preview" to="/users" class="ios-btn-secondary shrink-0">返回用户管理</router-link>
      <button v-else class="ios-btn-secondary shrink-0" @click="logout">退出登录</button>
    </header>
    <main class="max-w-6xl mx-auto p-4 sm:p-6 space-y-6">
      <nav class="flex gap-2 overflow-x-auto flex-nowrap -mx-1 px-1 pb-1">
        <button v-for="item in sections" :key="item.id" class="ios-btn-secondary shrink-0 whitespace-nowrap"
          :class="section === item.id ? 'text-ios-blue bg-blue-50' : ''" @click="navigate(item.id)">{{ item.label }}</button>
      </nav>
      <PortalPage :section="section" :preview="preview" :snapshot="snapshot" />
    </main>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { portalAPI, portalState, type Snapshot } from './api';
import PortalPage from './PortalPage.vue';
const props = defineProps<{ preview?: boolean; snapshot?: Snapshot }>();
const route = useRoute(), router = useRouter(), previewSection = ref('overview');
const sections = [{ id: 'overview', label: '概览' }, { id: 'subscription', label: '订阅' }, { id: 'nodes', label: '节点' }, { id: 'traffic', label: '流量' }, { id: 'account', label: '账号' }];
const section = computed(() => props.preview ? previewSection.value : String(route.params.section || 'overview'));
const name = computed(() => {
  const me = props.preview ? props.snapshot?.me : portalState.session?.currentUser;
  return me?.displayName || me?.username || '我的账号';
});
function navigate(id: string) { if (props.preview) previewSection.value = id; else void router.push('/portal/' + id); }
async function logout() {
  try { await portalAPI.logout(); await router.replace('/'); }
  catch (e) { window.alert((e as Error).message); }
}
</script>
