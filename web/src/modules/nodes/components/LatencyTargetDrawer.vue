<template>
  <FormDrawer
    :model-value="modelValue"
    title="检测目标设置"
    :loading="saving || loading"
    :show-footer="isOwner"
    confirm-text="保存设置"
    @update:model-value="$emit('update:modelValue', $event)"
    @confirm="handleSave"
  >
    <div class="space-y-5 text-sm">
      <LoadingSkeleton v-if="loading && !libraryTargets.length" />

      <template v-else-if="!isOwner">
        <div class="space-y-3">
          <p class="text-xs text-ios-secondary">
            当前服务器检测目标配置（Viewer 仅可查看当前生效目标）：
          </p>
          <div class="p-3 rounded-lg border border-ios-separator bg-ios-bg/50">
            <p class="font-medium text-ios-label mb-2">
              模式：{{ isCustom ? '自定义目标' : '使用系统默认' }}
            </p>
            <ul v-if="effectiveTargets.length" class="space-y-1 text-xs">
              <li v-for="target in effectiveTargets" :key="target.id" class="flex items-center gap-2">
                <span class="text-ios-blue">●</span>
                <span class="font-medium text-ios-label">{{ target.label }}</span>
                <span class="font-mono text-ios-secondary">{{ target.host }}:{{ target.port }}</span>
              </li>
            </ul>
            <p v-else class="text-xs text-ios-secondary">当前无生效的检测目标。</p>
          </div>
        </div>
      </template>

      <template v-else>
        <p v-if="error" role="alert" class="text-xs text-ios-red">{{ error }}</p>

        <div class="space-y-3">
          <label class="flex items-start gap-3 cursor-pointer">
            <input
              v-model="customMode"
              type="radio"
              value="default"
              class="mt-1"
            />
            <div class="space-y-1">
              <span class="font-medium text-ios-label">使用系统默认</span>
              <p class="text-xs text-ios-secondary">使用系统设置中配置的默认检测目标。</p>
              <div class="mt-2 p-2.5 rounded-lg border border-ios-separator bg-ios-bg/30">
                <p class="text-xs text-ios-secondary mb-1.5 font-medium">系统默认目标：</p>
                <ul v-if="defaultTargets.length" class="space-y-1 text-xs">
                  <li v-for="t in defaultTargets" :key="t.id" class="flex items-center gap-2">
                    <span class="text-ios-blue">●</span>
                    <span>{{ t.label }}</span>
                    <span class="font-mono text-ios-secondary">{{ t.host }}:{{ t.port }}</span>
                  </li>
                </ul>
                <p v-else class="text-xs text-ios-secondary">系统尚未配置默认目标。</p>
              </div>
            </div>
          </label>

          <label class="flex items-start gap-3 cursor-pointer pt-2">
            <input
              v-model="customMode"
              type="radio"
              value="custom"
              class="mt-1"
            />
            <div class="space-y-1 flex-1">
              <span class="font-medium text-ios-label">自定义目标</span>
              <p class="text-xs text-ios-secondary">
                最多勾选 5 个目标。未勾选任何目标则此服务器不进行延迟检测。
              </p>
            </div>
          </label>
        </div>

        <div v-if="customMode === 'custom'" class="pl-7 space-y-4 pt-1">
          <div class="flex items-center justify-between">
            <span class="text-xs text-ios-secondary">
              已选 {{ selectedIds.length }}/5 个
              <span v-if="selectedIds.length >= 5" class="text-ios-orange ml-1">（已达上限）</span>
            </span>
            <button
              type="button"
              class="ios-action-btn-secondary"
              @click="restoreDefault"
            >
              恢复默认
            </button>
          </div>

          <div v-if="libraryTargets.length" class="space-y-2 max-h-60 overflow-y-auto pr-1">
            <label
              v-for="target in libraryTargets"
              :key="target.id"
              class="flex items-center gap-2 p-2 rounded-lg border border-ios-separator hover:bg-ios-bg/40 cursor-pointer text-xs"
            >
              <input
                v-model="selectedIds"
                type="checkbox"
                :value="target.id"
                :disabled="!selectedIds.includes(target.id) && selectedIds.length >= 5"
              />
              <span class="font-medium text-ios-label">{{ target.label }}</span>
              <span class="font-mono text-ios-secondary">{{ target.host }}:{{ target.port }}</span>
              <span v-if="target.allowNonPublic" class="text-ios-quaternary text-2xs ml-auto">非公网</span>
            </label>
          </div>
          <p v-else class="text-xs text-ios-secondary">目标库暂无目标，请点击下方“+ 新目标”创建。</p>

          <div class="pt-2">
            <button
              v-if="!showingNewTarget"
              type="button"
              class="ios-action-btn-secondary"
              @click="showingNewTarget = true"
            >
              + 新目标
            </button>
            <div v-else class="p-3 rounded-lg border border-ios-separator bg-ios-bg/40 space-y-2">
              <p class="text-xs font-semibold text-ios-label">添加新目标到目标库</p>
              <ProbeTargetForm
                :busy="creating"
                show-cancel
                @cancel="showingNewTarget = false"
                @submit="handleCreateTarget"
              />
            </div>
          </div>
        </div>

      </template>
    </div>
  </FormDrawer>
</template>
<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import { nodeAPI, type ProbeTarget } from '../api';
import FormDrawer from '../../../components/FormDrawer.vue';
import ProbeTargetForm from './ProbeTargetForm.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const props = defineProps<{
  modelValue: boolean;
  nodeId: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  saved: [];
}>();

const loading = ref(false);
const saving = ref(false);
const creating = ref(false);
const error = ref('');
const customMode = ref<'default' | 'custom'>('default');
const selectedIds = ref<string[]>([]);
const libraryTargets = ref<ProbeTarget[]>([]);
const defaultTargetIds = ref<string[]>([]);
const showingNewTarget = ref(false);

const isCustom = computed(() => customMode.value === 'custom');
const defaultTargets = computed(() => libraryTargets.value.filter(t => defaultTargetIds.value.includes(t.id)));
const effectiveTargets = computed(() => {
  if (isCustom.value) {
    return libraryTargets.value.filter(t => selectedIds.value.includes(t.id));
  }
  return defaultTargets.value;
});

async function loadSettings() {
  loading.value = true;
  error.value = '';
  try {
    const [targetsRes, libraryRes] = await Promise.all([
      nodeAPI.latencyTargets(props.nodeId),
      nodeAPI.probeTargets()
    ]);
    libraryTargets.value = libraryRes.items;
    defaultTargetIds.value = targetsRes.defaultTargetIds || [];
    selectedIds.value = [...(targetsRes.targetIds || [])];
    customMode.value = targetsRes.custom ? 'custom' : 'default';
  } catch (err: any) {
    error.value = err.message || '读取检测目标设置失败';
  } finally {
    loading.value = false;
  }
}

function restoreDefault() {
  customMode.value = 'default';
  selectedIds.value = [...defaultTargetIds.value];
}

async function handleCreateTarget(formData: { label: string; host: string; port: number; allowNonPublic: boolean }) {
  creating.value = true;
  error.value = '';
  try {
    const created = await nodeAPI.createProbeTarget(formData);
    libraryTargets.value = [...libraryTargets.value, created];
    customMode.value = 'custom';
    if (selectedIds.value.length < 5 && !selectedIds.value.includes(created.id)) {
      selectedIds.value = [...selectedIds.value, created.id];
    }
    showingNewTarget.value = false;
    showToast(`目标“${created.label}”已创建并预选`);
  } catch (err: any) {
    error.value = err.message || '创建目标失败';
  } finally {
    creating.value = false;
  }
}

async function handleSave() {
  saving.value = true;
  error.value = '';
  const isCust = customMode.value === 'custom';
  const targetIds = isCust ? selectedIds.value : [];
  try {
    await nodeAPI.saveLatencyTargets(props.nodeId, {
      custom: isCust,
      targetIds
    });
    showToast('检测目标设置已保存');
    emit('saved');
    emit('update:modelValue', false);
  } catch (err: any) {
    if (err instanceof APIError) {
      if (err.status === 409 || err.code === 'node_target_limit') {
        error.value = '最多只能选择 5 个检测目标';
      } else if (err.code === 'unknown_target') {
        error.value = '所选目标不存在或已删除，请刷新后重试';
      } else {
        error.value = err.message || '保存设置失败';
      }
    } else {
      error.value = (err as Error).message || '保存设置失败';
    }
  } finally {
    saving.value = false;
  }
}

watch(() => props.modelValue, (open) => {
  if (open) {
    showingNewTarget.value = false;
    void loadSettings();
  }
});
</script>
