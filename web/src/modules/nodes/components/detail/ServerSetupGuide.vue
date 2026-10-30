<template>
  <section class="panel-card p-6 space-y-5">
    <h2 class="font-semibold text-ios-label">待登记 · 安装 Agent</h2>
    <ol class="list-decimal pl-5 space-y-3 text-sm text-ios-secondary"><li>生成一次性安装命令。</li><li>在目标服务器终端以 root 用户运行命令，完成 Agent 安装与证书登记。</li><li>等待首次心跳，服务器上线后继续创建节点。</li></ol>
    <button v-if="isOwner && !node.deletedAt" class="ios-btn-primary" @click="$emit('install')">生成安装命令</button>
    <CodePreview v-if="command" :code="command" title="安装命令 / 登记码" />
    <p class="text-sm text-ios-secondary">{{ isOwner ? '命令及复制按钮在安装抽屉中显示；重新生成会撤销旧注册码。' : '请联系 Owner 获取安装命令。' }}</p>
    <p class="text-sm text-ios-secondary" role="status">等待首次心跳…</p>
  </section>
</template>
<script setup lang="ts">
import CodePreview from '../../../../components/CodePreview.vue';
import type { NodeView } from '../../api';
import { isOwner } from '../../../../app/auth';
defineProps<{ node: NodeView; command?: string }>();
defineEmits<{ install: [] }>();
</script>
