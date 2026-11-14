<template>
  <AppDialog :model-value="true" :title="mode === 'rename' ? '改名' : '删除导入节点'" max-width="md"
    :confirm-text="mode === 'rename' ? '保存' : preview && confirmation.canConfirm ? '确认删除' : undefined"
    :loading="busy || loading" @update:model-value="close" @confirm="save">
    <div class="space-y-4 text-sm">
      <template v-if="mode === 'rename'">
        <label class="block space-y-2">节点名称
          <input v-model="name" class="form-input-base" :disabled="busy" @keyup.enter="save" />
        </label>
        <p class="text-xs text-ios-secondary">名称须为 1–200 UTF-8 字节。改名可能改变关键词策略的匹配结果。</p>
      </template>
      <template v-else-if="preview">
        <p>删除「{{ preview.node.name }}」？</p>
        <div v-if="preview.groups.length">
          <h4 class="text-base font-semibold">节点分组</h4>
          <ul class="list-disc pl-5"><li v-for="group in preview.groups" :key="group.id">{{ group.name }}</li></ul>
        </div>
        <div v-if="preview.clientPolicies.length">
          <h4 class="text-base font-semibold">客户端策略</h4>
          <ul class="list-disc pl-5"><li v-for="policy in preview.clientPolicies" :key="policy.profileId">
            {{ policy.profileName }}（{{ policy.active ? '活动配置' : '非活动配置' }}）：{{ policy.groups.join('、') }}
          </li></ul>
        </div>
        <div v-if="preview.canvases.length" class="space-y-2">
          <p class="text-ios-red">节点被画布引用，请先修改并发布以下方案后再删除。</p>
          <ul class="list-disc pl-5"><li v-for="canvas in preview.canvases" :key="canvas.schemeId">
            <RouterLink :to="{ path: '/proxy/relay', query: { schemeId: canvas.schemeId } }" class="text-ios-blue" @click="close">{{ canvas.schemeName }}</RouterLink>
          </li></ul>
        </div>
        <label v-else-if="confirmation.needsRemoval" class="flex items-start gap-2">
          <input v-model="removeReferences" type="checkbox" class="mt-1" :disabled="busy" />
          同时从这些分组和策略中移除
        </label>
        <p v-if="preview.lastInSource" class="text-ios-secondary">这是该来源的最后一个节点，来源会一并删除。</p>
      </template>
      <p v-else-if="loading">正在读取删除影响…</p>
      <p v-if="error" role="alert" class="text-ios-red whitespace-pre-wrap">{{ error }}</p>
      <ul v-if="conflicts.length" class="list-disc pl-5 text-ios-red">
        <li v-for="(conflict, index) in conflicts" :key="index">{{ conflictType(conflict.type) }}：{{ conflict.name }}</li>
      </ul>
    </div>
  </AppDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { RouterLink } from 'vue-router';
import AppDialog from '../../../components/AppDialog.vue';
import { APIError } from '../../../app/apiClient';
import { importAPI, type ImportDeletionPreview } from '../importApi';
import { importDeletionState } from '../importIdentity';
import type { ProxyNodeItem } from '../proxyNodes';

const props = defineProps<{ node: ProxyNodeItem; mode: 'rename' | 'delete' }>();
const emit = defineEmits<{ close: []; saved: [] }>();
const name = ref(props.node.name);
const busy = ref(false);
const loading = ref(false);
const error = ref('');
const conflicts = ref<{ type: string; name: string }[]>([]);
const preview = ref<ImportDeletionPreview | null>(null);
const removeReferences = ref(false);
const confirmation = computed(() => preview.value
  ? importDeletionState(preview.value, removeReferences.value)
  : { needsRemoval: false, canConfirm: false });

function close() { if (!busy.value) emit('close'); }
function conflictType(type: string) {
  return ({ content: '单独导入', url: '机场订阅', 'self-built': '自建节点' } as Record<string, string>)[type] || type;
}
function failure(cause: unknown) {
  if (!(cause instanceof APIError)) { error.value = (cause as Error).message; return; }
  const messages: Record<string, string> = {
    stale: '节点已被修改，请刷新列表后重试。',
    import_node_missing: '导入节点不存在或已被删除，请刷新列表。',
    import_rename_unsupported: '订阅连接的节点名称跟随机场，不能改名。',
    import_delete_unsupported: '订阅连接的节点跟随机场，不能单独删除。',
    import_invalid_name: '名称须为 1–200 UTF-8 字节，且不能包含控制字符。',
    import_name_conflict: '节点名称已存在，请使用其他名称。',
    import_in_use_by_canvas: '节点被画布引用，请先修改并发布方案。',
    import_rename_failed: '导入节点改名失败。',
    import_delete_failed: '删除导入节点失败。',
  };
  error.value = messages[cause.code] || cause.message;
  if (cause.code === 'import_name_conflict') conflicts.value = cause.details.conflicts as typeof conflicts.value;
  if (preview.value && cause.code === 'import_in_use_by_canvas') {
    preview.value.canvases = cause.details.canvases as ImportDeletionPreview['canvases'];
  }
  if (preview.value && cause.code === 'import_referenced' && cause.details.groups) {
    preview.value.groups = cause.details.groups as ImportDeletionPreview['groups'];
    preview.value.clientPolicies = cause.details.clientPolicies as ImportDeletionPreview['clientPolicies'];
    removeReferences.value = false;
  }
}
async function save() {
  if (busy.value || loading.value || (props.mode === 'delete' && !confirmation.value.canConfirm)) return;
  error.value = '';
  conflicts.value = [];
  busy.value = true;
  try {
    if (props.mode === 'rename') await importAPI.rename(props.node.id, { name: name.value, expectedRevision: props.node.revision! });
    else await importAPI.removeProxy(props.node.id, { expectedRevision: props.node.revision!, removeReferences: removeReferences.value });
    emit('saved');
  } catch (cause) { failure(cause); }
  finally { busy.value = false; }
}
onMounted(async () => {
  if (props.mode !== 'delete') return;
  loading.value = true;
  try { preview.value = await importAPI.deletionPreview(props.node.id); }
  catch (cause) { failure(cause); }
  finally { loading.value = false; }
});
</script>
