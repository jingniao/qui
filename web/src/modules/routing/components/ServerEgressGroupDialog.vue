<template>
  <AppDialog :model-value="true" :title="group ? '编辑出站服务器组' : '新建出站服务器组'" :confirm-text="group ? '保存' : '创建'" :loading="saving || loading"
    @update:model-value="value => { if (!value && !saving) emit('close'); }" @confirm="save">
    <fieldset :disabled="saving || loading" class="space-y-3">
      <label class="block text-sm">组名<input v-model="name" class="form-input-base mt-1" /></label>
      <fieldset class="max-h-48 overflow-auto space-y-1">
        <legend class="text-sm">手选服务器（按勾选顺序）</legend>
        <label v-for="server in servers" :key="server.id" class="flex gap-2 text-sm">
          <input v-model="serverIds" type="checkbox" :value="server.id" />{{ server.name }}
        </label>
        <label v-for="id in unavailableIds" :key="id" class="flex gap-2 text-sm">
          <input v-model="serverIds" type="checkbox" :value="id" />不可用的服务器（{{ id }}）
        </label>
      </fieldset>
      <label class="block text-sm">服务器名称正则
        <input v-model="namePattern" class="form-input-base mt-1" placeholder="例如 美国|US|洛杉矶" />
      </label>
      <button type="button" class="ios-btn-secondary" :disabled="previewing" @click="preview">预览成员</button>
      <p v-if="previewResult" class="text-sm">{{ previewResult }}</p>
    </fieldset>
    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>
  </AppDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import AppDialog from '../../../components/AppDialog.vue';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { nodeHasUserCore } from '../../nodes/coreSummary';
import { choices } from '../../proxy/api';
import { serverEgressGroupAPI, type ServerEgressGroup } from '../api';

const props = defineProps<{ group?: ServerEgressGroup | null }>();
const emit = defineEmits<{ close: []; saved: [group: ServerEgressGroup] }>();
const name = ref(props.group?.name || ''), namePattern = ref(props.group?.namePattern || '');
const serverIds = ref([...(props.group?.serverIds || [])]);
const servers = ref<NodeView[]>([]), error = ref(''), previewResult = ref('');
const loading = ref(true), previewing = ref(false), saving = ref(false);
const unavailableIds = computed(() => loading.value ? [] : serverIds.value.filter(id => !servers.value.some(server => server.id === id)));
let previewGeneration = 0;
watch([namePattern, serverIds], () => { previewResult.value = ''; previewGeneration++; });

function input() {
  return { name: name.value, serverIds: serverIds.value, namePattern: namePattern.value.trim() };
}

onMounted(async () => {
  try {
    servers.value = (await choices((page, size) => nodeAPI.choices(page, '', size)))
      .filter(server => server.enabled && !server.deletedAt && server.status !== 'pending' && server.status !== 'disabled' && nodeHasUserCore(server));
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
});

async function preview() {
  const generation = ++previewGeneration;
  error.value = '';
  previewing.value = true;
  try {
    const members = await serverEgressGroupAPI.preview(input());
    if (generation === previewGeneration) previewResult.value = members.map(member => member.name).join('、') || '没有匹配的服务器';
  } catch (e) {
    if (generation === previewGeneration) error.value = (e as Error).message;
  } finally {
    previewing.value = false;
  }
}

async function save() {
  if (saving.value || loading.value) return;
  error.value = '';
  saving.value = true;
  try {
    const group = await serverEgressGroupAPI.save(props.group?.id || null, input());
    emit('saved', group);
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    saving.value = false;
  }
}
</script>
