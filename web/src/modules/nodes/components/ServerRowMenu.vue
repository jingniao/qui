<template>
  <details v-if="!node.deletedAt" class="text-sm">
    <summary :class="compact ? 'ios-action-btn-secondary' : 'ios-btn-secondary'" class="cursor-pointer">更多</summary>
    <div class="panel-card p-3 mt-2 flex flex-col gap-2">
      <RouterLink :to="`/nodes/${node.id}?tab=settings`" class="text-ios-blue">编辑与续费设置</RouterLink>
      <RouterLink :to="`/nodes/${node.id}?tab=config`" class="text-ios-blue">发布 / 回滚配置</RouterLink>
      <RouterLink :to="`/nodes/${node.id}?tab=testing`" class="text-ios-blue">线路探测</RouterLink>
      <RouterLink to="/nodes/versions" class="text-ios-blue">版本与更新</RouterLink>
      <button v-if="node.status === 'pending'" type="button" class="ios-btn-secondary" @click="$emit('install')">重新生成安装命令</button>
      <button type="button" class="ios-btn-danger" @click="$emit('delete')">{{ node.status === 'pending' ? '取消登记 / 删除' : '删除服务器' }}</button>
    </div>
  </details>
</template>
<script setup lang="ts">
import type { NodeView } from '../api';
defineProps<{ node: NodeView; compact?: boolean }>();
defineEmits<{ install: []; delete: [] }>();
</script>
