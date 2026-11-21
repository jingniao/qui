<template>
  <div class="space-y-5">
    <PageHeader
      title="版本与更新"
      parent="服务器"
      description="查看服务器程序版本，管理 Agent 与代理核心更新任务。"
    >
      <template #actions>
        <button
          v-if="isOwner"
          type="button"
          @click="triggerUpgradeAll('core')"
          class="ios-btn-primary whitespace-nowrap shadow-[0_2px_8px_rgba(0,113,227,0.28)] py-2 px-3.5"
          title="批量升级在线服务器的代理核心"
        >升级全部核心</button>
        <button
          v-if="isOwner"
          type="button"
          @click="triggerUpgradeAll('agent')"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)] py-2 px-3.5"
          title="批量升级在线服务器的 Agent 到最新版本"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          <span>升级全部 Agent</span>
        </button>
      </template>
    </PageHeader>

    <NodeVersionPanel ref="panelRef" />
  </div>
</template>

<script setup lang="ts">
import { isOwner } from '../../../app/auth';
import { ref } from 'vue';
import PageHeader from '../../../components/PageHeader.vue';
import NodeVersionPanel from '../../system/components/NodeVersionPanel.vue';

const panelRef = ref<InstanceType<typeof NodeVersionPanel> | null>(null);

function triggerUpgradeAll(kind: 'agent' | 'core') {
  panelRef.value?.upgradeAllFor(kind);
}
</script>
