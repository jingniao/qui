<template>
  <details v-if="!node.deletedAt" class="text-sm">
    <summary :class="compact ? 'ios-action-btn-secondary' : 'ios-btn-secondary'" class="cursor-pointer">更多</summary>
    <div class="panel-card p-3 mt-2 flex flex-col gap-2">
      <RouterLink :to="`/nodes/${node.id}?tab=settings`" class="text-ios-blue">编辑与续费设置</RouterLink>
      <RouterLink :to="`/nodes/${node.id}?tab=config`" class="text-ios-blue">发布 / 回滚配置</RouterLink>
      <RouterLink :to="`/nodes/${node.id}?tab=testing`" class="text-ios-blue">线路探测</RouterLink>
      <RouterLink to="/nodes/versions" class="text-ios-blue">版本与更新</RouterLink>
      <button v-if="quickRelay.canOpen(node)" type="button" class="ios-btn-secondary" @click="quickRelay.open(node)">添加中转</button>
      <button
        v-if="node.status === 'pending'"
        type="button"
        class="ios-btn-secondary inline-flex items-center justify-center gap-1.5 disabled:opacity-50 disabled:cursor-not-allowed"
        :disabled="isNodeReissuing(node.id)"
        @click="$emit('install')"
      >
        <span v-if="isNodeReissuing(node.id)" class="inline-block w-3 h-3 border-2 border-ios-blue border-t-transparent rounded-full animate-spin"></span>
        <span>{{ isNodeReissuing(node.id) ? '正在签发…' : '重新生成安装命令' }}</span>
      </button>
      <button type="button" class="ios-btn-danger" @click="$emit('delete')">{{ node.status === 'pending' ? '取消登记 / 删除' : '删除服务器' }}</button>
    </div>
  </details>
</template>
<script setup lang="ts">
import { inject } from 'vue';
import type { NodeView } from '../api';
import { isNodeReissuing } from '../coreInstallOptions';
const quickRelay = inject<{ canOpen: (node: NodeView) => boolean; open: (node: NodeView) => Promise<void> }>('serverQuickRelay')!;
defineProps<{ node: NodeView; compact?: boolean }>();
defineEmits<{ install: []; delete: [] }>();
</script>
