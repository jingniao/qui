<template>
  <AppDialog v-model="open" :title="preview?.mode === 'cancel' ? '取消服务器登记' : '删除服务器'" :show-footer="false">
    <div class="space-y-4 text-sm text-ios-secondary">
      <p v-if="loading">正在检查服务器引用与发布状态…</p>
      <div v-else-if="preview?.blockers.length">
        <p>服务器仍被以下配置引用或有进行中的发布，请先处理：</p>
        <ul class="mt-3 space-y-2"><li v-for="blocker in preview.blockers" :key="blocker.kind + blocker.id">{{ labels[blocker.kind] || blocker.kind }} · {{ blocker.name }}</li></ul>
      </div>
      <p v-else-if="preview?.mode === 'cancel'">确认取消服务器 {{ target?.name }} 的登记？记录将直接移除。</p>
      <p v-else-if="preview">确认删除服务器 {{ target?.name }}？证书将立即吊销，退出所有节点分组，历史流量与访问记录保留。</p>
      <p v-if="error" role="alert" class="text-ios-red">{{ error }}</p>
      <div class="flex justify-end gap-3">
        <button class="ios-dialog-btn-secondary" :disabled="deleting" @click="open = false">关闭</button>
        <button v-if="preview" class="ios-dialog-btn-danger" :disabled="loading || deleting || preview.blockers.length > 0 || preview.mode === 'deleted'" @click="remove">{{ deleting ? '处理中…' : preview.mode === 'cancel' ? '确认取消登记' : '确认删除' }}</button>
      </div>
    </div>
  </AppDialog>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { nodeAPI, type NodeView, type DeletionPreview, type DeletionBlocker } from '../api';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import AppDialog from '../../../components/AppDialog.vue';
const emit = defineEmits<{ changed: [] }>();
const open = ref(false), loading = ref(false), deleting = ref(false), error = ref('');
const target = ref<NodeView | null>(null), preview = ref<DeletionPreview | null>(null);
const labels: Record<string, string> = { inbound: '入站', outbound: '出站', outbound_group: '出口组', outbound_target: '其他服务器的出站', proxy_chain: '代理链', policy: '分流规则', deployment_batch: '进行中的批量发布' };
let generation = 0;
async function prompt(node: NodeView) {
  const current = ++generation;
  target.value = node; preview.value = null; error.value = ''; open.value = true; loading.value = true;
  try { const result = await nodeAPI.deletionPreview(node.id); if (current === generation) preview.value = result; }
  catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
async function remove() {
  if (!target.value || !preview.value || preview.value.blockers.length || deleting.value) return;
  deleting.value = true; error.value = '';
  try {
    await nodeAPI.deleteNode(target.value.id);
    showToast(preview.value.mode === 'cancel' ? '服务器登记已取消' : '服务器已删除');
    open.value = false; emit('changed');
  } catch (e) {
    error.value = (e as Error).message;
    const blockers = (e as APIError).details?.blockers;
    if (Array.isArray(blockers)) preview.value = { ...preview.value, blockers: blockers as DeletionBlocker[] };
  } finally { deleting.value = false; }
}
defineExpose({ prompt });
</script>
