<template>
  <section class="panel-card p-6 space-y-5">
    <div class="flex flex-wrap justify-between items-center gap-3">
      <div>
        <h2 class="font-semibold text-ios-label">延迟监控目标库</h2>
        <p class="text-xs text-ios-secondary mt-1">
          管理系统探针目标库，并设置全局默认延迟检测目标（最多 5 个）。自定义服务器检测目标可在各服务器详情中单独指定。
        </p>
      </div>
      <div v-if="isOwner" class="flex items-center gap-2">
        <button class="ios-btn-primary" :disabled="loading || saving" @click="startAdd">
          + 添加目标
        </button>
        <button class="ios-btn-secondary" :disabled="loading || saving" @click="load">
          刷新
        </button>
      </div>
    </div>

    <p v-if="error" role="alert" class="text-sm text-ios-red">{{ error }}</p>

    <div v-if="showingForm && isOwner" class="p-4 rounded-xl border border-ios-separator bg-ios-bg/40 max-w-2xl space-y-3">
      <h3 class="text-sm font-semibold text-ios-label">
        {{ editingTarget ? '编辑探测目标' : '添加探测目标' }}
      </h3>
      <ProbeTargetForm
        :model-value="editingTarget"
        :busy="saving"
        :submit-text="editingTarget ? '保存修改' : '添加目标'"
        show-cancel
        show-scheduled
        inline
        @cancel="cancelForm"
        @submit="handleFormSubmit"
      />
    </div>

    <LoadingSkeleton v-if="loading && !targets.length" />

    <template v-else-if="targets.length">
      <DataTable>
        <template #header>
          <th>目标标签</th>
          <th>主机与端口</th>
          <th>非公网地址</th>
          <th>系统默认 ({{ scheduledCount }}/5)</th>
          <th v-if="isOwner">操作</th>
        </template>
        <template #body>
          <tr v-for="target in targets" :key="target.id">
            <td>
              <span class="font-medium text-ios-label">{{ target.label }}</span>
            </td>
            <td>
              <span class="font-mono text-xs text-ios-secondary">{{ target.host }}:{{ target.port }}</span>
            </td>
            <td>
              <StatusBadge
                :status="target.allowNonPublic ? 'success' : 'neutral'"
                :label="target.allowNonPublic ? '允许' : '仅公网'"
              />
            </td>
            <td>
              <label v-if="isOwner" class="inline-flex items-center gap-2 cursor-pointer">
                <input
                  type="checkbox"
                  :checked="target.scheduled"
                  :disabled="saving || (!target.scheduled && scheduledCount >= 5)"
                  @change="toggleScheduled(target, $event)"
                />
                <span class="text-xs" :class="target.scheduled ? 'text-ios-blue font-medium' : 'text-ios-secondary'">
                  {{ target.scheduled ? '默认目标' : '未设' }}
                </span>
              </label>
              <StatusBadge
                v-else
                :status="target.scheduled ? 'success' : 'neutral'"
                :label="target.scheduled ? '系统默认' : '未设'"
              />
            </td>
            <td v-if="isOwner">
              <div class="flex gap-2">
                <button
                  class="ios-action-btn-secondary"
                  :disabled="saving"
                  @click="startEdit(target)"
                >
                  编辑
                </button>
                <button
                  class="ios-action-btn-danger"
                  :disabled="saving"
                  @click="deleting = target"
                >
                  删除
                </button>
              </div>
            </td>
          </tr>
        </template>
      </DataTable>
    </template>

    <EmptyState
      v-else-if="!loading"
      title="尚无探测目标"
      description="点击上方“添加目标”创建第一个探针目标。"
    />

    <ConfirmDialog
      :model-value="!!deleting"
      title="删除探测目标"
      :message="`确定要删除目标“${deleting?.label || ''}”吗？`"
      warning-details="警告：删除此目标将永久清理所有服务器针对该目标的历史延迟采样记录，并移除各服务器的自定义监控关联。"
      confirm-text="确认删除"
      :is-danger="true"
      @update:model-value="val => { if (!val) deleting = null; }"
      @confirm="confirmDelete"
    />
  </section>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import { nodeAPI, type ProbeTarget } from '../../nodes/api';
import ProbeTargetForm from '../../nodes/components/ProbeTargetForm.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const targets = ref<ProbeTarget[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref('');
const showingForm = ref(false);
const editingTarget = ref<ProbeTarget | null>(null);
const deleting = ref<ProbeTarget | null>(null);

const scheduledCount = computed(() => targets.value.filter(t => t.scheduled).length);

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const res = await nodeAPI.probeTargets();
    targets.value = res.items;
  } catch (err: any) {
    error.value = err.message || '读取探测目标失败';
  } finally {
    loading.value = false;
  }
}

function startAdd() {
  editingTarget.value = null;
  showingForm.value = true;
  error.value = '';
}

function startEdit(target: ProbeTarget) {
  editingTarget.value = target;
  showingForm.value = true;
  error.value = '';
}

function cancelForm() {
  showingForm.value = false;
  editingTarget.value = null;
  error.value = '';
}

async function handleFormSubmit(formData: { label: string; host: string; port: number; allowNonPublic: boolean; scheduled: boolean }) {
  saving.value = true;
  error.value = '';
  try {
    if (editingTarget.value) {
      await nodeAPI.saveProbeTarget({
        ...editingTarget.value,
        ...formData
      });
      showToast('目标已修改');
    } else {
      await nodeAPI.createProbeTarget(formData);
      showToast('目标已添加');
    }
    cancelForm();
    await load();
  } catch (err: any) {
    if (err instanceof APIError && (err.status === 409 || err.code === 'scheduled_target_limit')) {
      error.value = '最多只能设置 5 个系统默认检测目标';
    } else {
      error.value = err.message || '保存探测目标失败';
    }
  } finally {
    saving.value = false;
  }
}

async function toggleScheduled(target: ProbeTarget, event: Event) {
  const input = event.target as HTMLInputElement;
  const newScheduled = input.checked;
  input.checked = target.scheduled; // revert until confirmed
  saving.value = true;
  error.value = '';
  try {
    await nodeAPI.saveProbeTarget({
      ...target,
      scheduled: newScheduled
    });
    showToast(newScheduled ? '已设为系统默认目标' : '已取消系统默认目标');
    await load();
  } catch (err: any) {
    if (err instanceof APIError && (err.status === 409 || err.code === 'scheduled_target_limit')) {
      error.value = '最多只能设置 5 个系统默认检测目标';
    } else {
      error.value = err.message || '修改系统默认状态失败';
    }
  } finally {
    saving.value = false;
  }
}

async function confirmDelete() {
  if (!deleting.value) return;
  const target = deleting.value;
  saving.value = true;
  error.value = '';
  try {
    await nodeAPI.deleteProbeTarget(target.id);
    showToast(`目标“${target.label}”已删除`);
    deleting.value = null;
    await load();
  } catch (err: any) {
    error.value = err.message || '删除探测目标失败';
  } finally {
    saving.value = false;
  }
}

onMounted(() => {
  void load();
});
</script>
