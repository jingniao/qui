<template>
  <div class="space-y-5">
    <PageHeader
      title="版本与更新"
      parent="服务器"
      description="查看服务器程序版本，管理 Agent 与代理核心更新任务。"
    >
      <template #actions>
        <button
          type="button"
          @click="triggerUpgradeAll"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)] text-xs py-2 px-3.5"
          :title="currentTab === 'cores' ? '全部升级在线服务器的代理核心' : '全部升级在线服务器的 Agent 到最新版本'"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          <span>全部升级</span>
        </button>
      </template>
    </PageHeader>

    <NodeVersionPanel
      ref="panelRef"
      :key="currentTab"
      :kind="currentTab === 'cores' ? 'core' : 'agent'"
      :current-tab="currentTab"
      @change-tab="selectTab"
      embedded
    />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import PageHeader from '../../../components/PageHeader.vue';
import NodeVersionPanel from '../../system/components/NodeVersionPanel.vue';

const route = useRoute();
const router = useRouter();
const currentTab = computed(() => (route.query.tab === 'cores' ? 'cores' : 'agents'));
const panelRef = ref<InstanceType<typeof NodeVersionPanel> | null>(null);

function selectTab(tab: string) {
  router.replace({ query: { ...route.query, tab } });
}

function triggerUpgradeAll() {
  panelRef.value?.upgradeAll();
}
</script>
