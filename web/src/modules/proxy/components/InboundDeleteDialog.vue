<template>
  <AppDialog v-model="open" title="删除入站" :show-footer="false">
    <div class="space-y-4 text-sm text-ios-secondary">
      <p>删除「{{ target?.name }}」？</p>
      <p v-if="loading">正在读取删除明细…</p>
      <DeletionDetails v-if="preview" :preview="preview" />
      <p v-if="error" role="alert" class="text-ios-red">{{ error }}</p>
      <div class="flex justify-end gap-3">
        <button class="ios-dialog-btn-secondary" :disabled="deleting" @click="open = false">取消</button>
        <button v-if="preview && !preview.blockers.length" class="ios-dialog-btn-danger" :disabled="deleting" @click="remove">{{ deleting ? '删除中…' : '删除' }}</button>
      </div>
    </div>
  </AppDialog>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import AppDialog from '../../../components/AppDialog.vue';
import DeletionDetails from '../../../components/DeletionDetails.vue';
import { proxyAPI } from '../api';
import { pendingCleanupMessage, type DeletionPreview } from '../../../app/deletion';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
const emit = defineEmits<{ changed: [] }>();
const open = ref(false), loading = ref(false), deleting = ref(false), error = ref('');
const target = ref<{ id: string; name: string }>();
const preview = ref<DeletionPreview>();
let generation = 0;
async function prompt(inbound: { id: string; name: string }) {
  const current = ++generation;
  target.value = inbound; preview.value = undefined; error.value = ''; loading.value = true; open.value = true;
  try { const result = await proxyAPI.inboundDeletionPreview(inbound.id); if (generation === current) preview.value = result; }
  catch (e) { if (generation === current) error.value = (e as Error).message; }
  finally { if (generation === current) loading.value = false; }
}
async function remove() {
  if (!target.value || !preview.value || preview.value.blockers.length || deleting.value) return;
  deleting.value = true; error.value = '';
  try {
    const result = await proxyAPI.deleteInbound(target.value.id);
    showToast('入站已删除' + pendingCleanupMessage(result?.pendingNodes));
    open.value = false; emit('changed');
  } catch (e) {
    error.value = (e as Error).message;
    if (e instanceof APIError && Array.isArray(e.details.blockers)) preview.value = { ...preview.value, blockers: e.details.blockers as DeletionPreview['blockers'] };
  } finally { deleting.value = false; }
}
defineExpose({ prompt });
</script>
