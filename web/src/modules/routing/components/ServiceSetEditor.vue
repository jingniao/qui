<template>
  <FormDrawer
    v-if="!deletingService"
    :model-value="modelValue"
    :title="editingId ? `编辑规则集 · v${service?.version || 1}` : '创建自定义规则集'"
    description="统一维护目标域名、后缀、关键字与 IP/CIDR，并可引入外部规则来源。保存后版本号自动增加，客户端与节点端均可引用。"
    :confirm-text="editingId ? '保存修改' : '创建规则集'"
    :loading="saving"
    @update:model-value="emit('update:modelValue', $event)"
    @confirm="save"
  >
    <div class="space-y-4 text-sm">
      <p class="text-xs text-amber-700">这是共享规则集，修改会影响下列全部引用。保存立即写入，独立于配置草稿；放弃配置草稿不会撤销这里的保存。客户端更新规则集、节点发布配置后生效。</p>
      <div v-if="editingId" class="space-y-2 text-xs">
        <p v-if="referencesError" role="alert" class="text-red-600">{{ referencesError }}</p>
        <p class="font-semibold">共享引用</p>
        <p v-for="policy in references.nodePolicies" :key="policy.id">节点策略：{{ policy.name }}（{{ policy.enabled ? '启用' : '禁用' }}）</p>
        <div v-for="profile in references.clientProfiles" :key="profile.id">
          <p>客户端配置：{{ profile.name }}（{{ profile.active ? '活动' : '未启用' }}）</p>
          <p v-for="rule in profile.rules" :key="rule.id" class="pl-3">规则 {{ rule.id }}（{{ rule.enabled ? '启用' : '禁用' }}）</p>
        </div>
        <p v-if="!referencesError && !references.nodePolicies.length && !references.clientProfiles.length">暂无已保存引用</p>
      </div>
      <p v-if="!form.enabled" class="text-xs text-amber-700">禁用后规则集下载返回 404；客户端可能继续使用缓存或报告错误，不会立即停止使用缓存规则。启用的 RULE-SET 规则将无法通过配置保存校验。</p>
      <label class="block">
        <span class="text-xs font-medium text-neutral-600">规则集名称</span>
        <input
          v-model="form.name"
          placeholder="例如: OpenAI、常见流媒体"
          class="form-input-base mt-1.5"
          required
        />
      </label>

      <label class="block">
        <span class="text-xs font-medium text-neutral-600">描述说明 (可选)</span>
        <textarea
          v-model="form.description"
          rows="2"
          maxlength="300"
          placeholder="说明该规则集的收录范围与用途…"
          class="form-input-base mt-1.5"
        ></textarea>
      </label>

      <div>
        <span class="text-xs font-medium text-neutral-600">完整域名 (每行一个精确域名)</span>
        <textarea
          v-model="domainsText"
          rows="3"
          placeholder="api.example.com&#10;login.example.com"
          class="form-input-base mt-1.5 font-mono text-xs"
        ></textarea>
      </div>

      <div>
        <span class="text-xs font-medium text-neutral-600">域名后缀 (每行一个，子域名自动命中)</span>
        <textarea
          v-model="suffixesText"
          rows="3"
          placeholder="example.com&#10;openai.com"
          class="form-input-base mt-1.5 font-mono text-xs"
        ></textarea>
      </div>

      <div>
        <span class="text-xs font-medium text-neutral-600">域名关键字 (每行一个，包含该子串即命中)</span>
        <textarea
          v-model="keywordsText"
          rows="3"
          placeholder="openai&#10;google"
          class="form-input-base mt-1.5 font-mono text-xs"
        ></textarea>
      </div>

      <div>
        <span class="text-xs font-medium text-neutral-600">IP 网段 / CIDR (每行一个)</span>
        <textarea
          v-model="cidrsText"
          rows="3"
          placeholder="192.168.1.0/24&#10;10.0.0.0/8"
          class="form-input-base mt-1.5 font-mono text-xs"
        ></textarea>
      </div>

      <!-- 外部规则来源 -->
      <div class="space-y-3 pt-2 border-t border-black/[0.06]">
        <div class="flex items-center justify-between">
          <span class="text-xs font-semibold text-neutral-800">外部规则来源 (最多 10 个)</span>
          <button
            v-if="form.sources.length < 10"
            type="button"
            @click="addSource"
            class="text-xs text-ios-blue hover:underline font-medium"
          >
            + 添加外部来源
          </button>
        </div>
        <p v-if="props.service && !props.clone && form.sources.length && !props.service.autoRefresh" class="text-xs text-neutral-500">未被启用的节点策略引用，不自动刷新；节点策略引用后会立即抓取，也可以手动刷新。客户端订阅直接下载来源地址，不受影响。</p>
        <div v-if="!form.sources.length" class="text-xs text-neutral-400 py-1">
          未配置外部来源（支持纯文本、YAML 或 MRS 规则集）
        </div>
        <div
          v-for="(source, idx) in form.sources"
          :key="source.id || idx"
          class="p-3 bg-neutral-50 rounded-xl border border-black/[0.06] space-y-2.5"
        >
          <div class="flex items-center justify-between gap-2">
            <span class="text-xs font-bold text-neutral-700">来源 #{{ idx + 1 }}</span>
            <div class="flex items-center gap-2">
              <span v-if="source.nodeUsable === false" class="px-1.5 py-0.5 rounded text-xs font-bold bg-amber-50 text-amber-700">
                仅客户端可用
              </span>
              <span v-else-if="source.nodeUsable" class="px-1.5 py-0.5 rounded text-xs font-bold bg-emerald-50 text-emerald-700">
                节点可用
              </span>
              <button
                v-if="editingId && source.id"
                type="button"
                @click="refreshSource(source)"
                :disabled="refreshingSourceId === source.id"
                class="text-xs text-ios-blue hover:underline font-medium"
              >
                {{ refreshingSourceId === source.id ? '刷新中…' : '刷新' }}
              </button>
              <button
                type="button"
                @click="removeSource(idx)"
                class="text-xs text-red-500 hover:underline"
              >
                删除
              </button>
            </div>
          </div>
          <label class="block">
            <span class="text-xs text-neutral-500">来源 URL</span>
            <input v-model="source.url" placeholder="https://example.com/rules.txt" class="form-input-base mt-1 text-xs" required />
          </label>
          <div class="grid grid-cols-3 gap-2">
            <label class="block">
              <span class="text-xs text-neutral-500">格式</span>
              <select v-model="source.format" @change="onFormatChange(source)" class="form-input-base mt-1 text-xs py-1">
                <option value="text">text</option>
                <option value="yaml">yaml</option>
                <option value="mrs">mrs</option>
              </select>
            </label>
            <label class="block">
              <span class="text-xs text-neutral-500">行为</span>
              <select v-model="source.behavior" class="form-input-base mt-1 text-xs py-1">
                <option v-if="source.format !== 'mrs'" value="classical">classical</option>
                <option value="domain">domain</option>
                <option value="ipcidr">ipcidr</option>
              </select>
            </label>
            <label class="block">
              <span class="text-xs text-neutral-500">刷新间隔 (秒)</span>
              <input v-model.number="source.intervalSeconds" type="number" min="3600" max="604800" class="form-input-base mt-1 text-xs py-1" />
            </label>
          </div>
          <div v-if="source.lastStatus" class="flex flex-wrap items-center gap-x-3 text-xs text-neutral-500 pt-1 border-t border-black/[0.04]">
            <span>状态: <strong :class="source.lastStatus === 'ok' ? 'text-emerald-600' : 'text-red-600'">{{ source.lastStatus }}</strong></span>
            <span v-if="source.entryCount != null">条目: {{ source.entryCount }}</span>
            <span v-if="source.skippedCount != null">跳过: {{ source.skippedCount }}</span>
            <span v-if="source.lastFetchAt">更新: {{ source.lastFetchAt }}</span>
          </div>
        </div>
      </div>

      <label class="flex items-center gap-2 pt-1 cursor-pointer">
        <input type="checkbox" v-model="form.enabled" class="rounded text-ios-blue focus:ring-ios-blue" />
        <span class="text-xs font-medium text-neutral-700">启用此规则集</span>
      </label>
      <button v-if="editingId && !service?.builtin" type="button" class="ios-action-btn-secondary text-red-600" :disabled="saving" @click="deletingService = true">删除规则集</button>
    </div>
  </FormDrawer>
  <ConfirmDialog v-model="deletingService" title="删除共享规则集" message="确定删除此规则集？有已保存引用时将拒绝删除并显示引用。" confirm-text="删除规则集" :loading="saving" @confirm="remove" />
</template>

<script setup lang="ts">
import { ref, reactive, watch } from 'vue';
import { policyAPI, type ServiceSet, type ServiceInput, type ServiceReferences, type SourceInput } from '../api';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';

// 共享规则集创建／编辑对话框：「分流控制 → 规则集」与「客户端策略 → 规则集」共用。
// service 为空表示新建；clone 为真表示以 service 为模板新建（不带引用与来源 ID）。
const props = defineProps<{ modelValue: boolean; service: ServiceSet | null; clone?: boolean }>();
const emit = defineEmits<{
  (e: 'update:modelValue', open: boolean): void;
  (e: 'saved', saved: ServiceSet & { references: ServiceReferences }): void;
  (e: 'deleted'): void;
}>();

const emptyReferences = (): ServiceReferences => ({ nodePolicies: [], clientProfiles: [] });

const editingId = ref('');
const references = ref<ServiceReferences>(emptyReferences());
const referencesError = ref('');
const deletingService = ref(false);
const saving = ref(false);
const refreshingSourceId = ref('');
const domainsText = ref('');
const suffixesText = ref('');
const keywordsText = ref('');
const cidrsText = ref('');
const form = reactive<{ name: string; enabled: boolean; description: string; sources: SourceInput[] }>({
  name: '',
  enabled: true,
  description: '',
  sources: []
});

function lines(value: string) {
  return value
    .split('\n')
    .map(item => item.trim())
    .filter(Boolean);
}

function addSource() {
  if (form.sources.length >= 10) return;
  form.sources.push({ url: '', format: 'text', behavior: 'domain', intervalSeconds: 86400 });
}

function removeSource(index: number) {
  form.sources.splice(index, 1);
}

function onFormatChange(source: SourceInput) {
  if (source.format === 'mrs' && source.behavior === 'classical') {
    source.behavior = 'domain';
  }
}

async function refreshSource(source: SourceInput) {
  if (!source.id || refreshingSourceId.value) return;
  refreshingSourceId.value = source.id;
  try {
    const updated = await policyAPI.refreshServiceSource(editingId.value, source.id);
    Object.assign(source, updated);
    showToast('规则集来源已刷新');
  } catch (e) {
    showToast('刷新来源失败: ' + (e as Error).message, 'error');
  } finally {
    refreshingSourceId.value = '';
  }
}

async function initForm() {
  const service = props.service;
  const source = props.clone ? null : service;
  editingId.value = source?.id || '';
  references.value = emptyReferences();
  referencesError.value = '';
  form.name = service ? service.name + (props.clone ? '-副本' : '') : '';
  form.enabled = service ? service.enabled !== false : true;
  form.description = service?.description || '';
  form.sources = (service?.sources || []).map(s => ({ ...s, ...(props.clone ? { id: undefined } : {}) }));
  domainsText.value = (service?.domains || []).join('\n');
  suffixesText.value = (service?.suffixes || []).join('\n');
  keywordsText.value = (service?.keywords || []).join('\n');
  cidrsText.value = (service?.cidrs || []).join('\n');
  if (!source) return;
  try {
    const refs = await policyAPI.serviceReferences(source.id);
    if (editingId.value === source.id) references.value = refs;
  } catch (e) {
    if (editingId.value === source.id) referencesError.value = '读取引用失败: ' + (e as Error).message;
  }
}

// 保存载荷总是带完整的 keywords 与 sources：后端按请求重写全部条目，漏带会清空数据。
function buildPayload(): ServiceInput {
  return {
    name: form.name.trim(),
    enabled: form.enabled,
    description: form.description.trim(),
    domains: lines(domainsText.value),
    suffixes: lines(suffixesText.value),
    keywords: lines(keywordsText.value),
    cidrs: lines(cidrsText.value),
    sources: form.sources.map(s => ({
      id: s.id,
      url: s.url.trim(),
      format: s.format,
      behavior: s.behavior,
      intervalSeconds: Number(s.intervalSeconds) || 86400
    }))
  };
}

async function save() {
  if (saving.value) return;
  if (!form.name.trim()) {
    showToast('请填写规则集名称', 'error');
    return;
  }
  saving.value = true;
  try {
    const saved = await policyAPI.saveService(editingId.value || null, buildPayload());
    showToast('规则集已保存');
    emit('update:modelValue', false);
    emit('saved', saved);
  } catch (e) {
    showToast('保存规则集失败: ' + (e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

async function remove() {
  saving.value = true;
  try {
    await policyAPI.deleteService(editingId.value);
    deletingService.value = false;
    emit('update:modelValue', false);
    emit('deleted');
    showToast('规则集已删除');
  } catch (e) {
    deletingService.value = false;
    if (e instanceof APIError && e.status === 409) {
      references.value = e.details as unknown as ServiceReferences;
      referencesError.value = e.message;
    } else {
      showToast('删除规则集失败: ' + (e as Error).message, 'error');
    }
  } finally {
    saving.value = false;
  }
}

watch(
  () => props.modelValue,
  open => {
    if (open) void initForm();
  },
  { immediate: true }
);
</script>
