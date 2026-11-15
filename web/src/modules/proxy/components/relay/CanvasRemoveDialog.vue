<template>
  <AppDialog :model-value="true" title="停用并移除" max-width="sm" @update:model-value="close">
    <div class="space-y-3 text-sm text-ios-label">
      <p>停用并移除「{{ scheme.name }}」？撤除期间相关用户连接会短暂中断。</p>
      <p v-if="error" role="alert" class="text-ios-red whitespace-pre-wrap">{{ error }}</p>
    </div>
    <template #footer>
      <button type="button" class="ios-dialog-btn-secondary" :disabled="busy" @click="close">取消</button>
      <button type="button" class="ios-dialog-btn-danger" :disabled="busy" @click="remove">{{ busy ? '移除中…' : '停用并移除' }}</button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
import { ref } from 'vue';
import AppDialog from '../../../../components/AppDialog.vue';
import { APIError } from '../../../../app/apiClient';
import { canvasAPI, type CanvasScheme, type CanvasRemoveResult } from '../../api';

const props = defineProps<{ scheme: CanvasScheme }>();
const emit = defineEmits<{ close: []; removed: [result: CanvasRemoveResult] }>();
const busy = ref(false);
const error = ref('');
function close() { if (!busy.value) emit('close'); }
async function remove() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    const result = await canvasAPI.remove(props.scheme.id, { updatedAt: props.scheme.updatedAt });
    emit('removed', result);
  } catch (cause) {
    error.value = cause instanceof APIError && cause.code === 'stale_scheme'
      ? '方案已被修改，请刷新后重试' : (cause as Error).message;
  } finally { busy.value = false; }
}
</script>
