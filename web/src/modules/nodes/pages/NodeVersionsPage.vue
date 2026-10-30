<template>
  <div class="space-y-6">
    <PageHeader title="版本与更新" parent="服务器" description="查看服务器程序版本，管理 Agent 与代理核心更新任务。" />
    <div class="ios-segmented-container" role="tablist" aria-label="程序类型">
      <button v-for="tab in tabs" :key="tab.id" role="tab" :aria-selected="currentTab === tab.id"
        class="ios-segmented-item" :class="{ active: currentTab === tab.id }" @click="selectTab(tab.id)">{{ tab.label }}</button>
    </div>
    <NodeVersionPanel :key="currentTab" :kind="currentTab === 'cores' ? 'core' : 'agent'" embedded />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import PageHeader from '../../../components/PageHeader.vue';
import NodeVersionPanel from '../../system/components/NodeVersionPanel.vue';

const route = useRoute();
const router = useRouter();
const tabs = [{ id: 'agents', label: 'Agent' }, { id: 'cores', label: '代理核心' }];
const currentTab = computed(() => route.query.tab === 'cores' ? 'cores' : 'agents');
function selectTab(tab: string) {
  router.replace({ query: { ...route.query, tab } });
}
</script>
