<template>
    <FormDrawer
      v-model="editDrawer"
      title="编辑服务器"
      description="修改服务器接入地址、维护状态、续费价格与扩展信息。"
      :loading="editingSaving || fetchingLatest"
      @confirm="saveEdit"
    >
      <div class="space-y-4 text-sm" v-if="editTarget">
        <div v-if="fetchingLatest" class="flex items-center gap-2 text-xs text-ios-secondary bg-black/[0.04] rounded-xl px-3 py-2">
          <svg class="animate-spin h-3.5 w-3.5 text-ios-blue shrink-0" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <span>正在读取最新信息</span>
        </div>
        <div v-else-if="fetchError" class="text-xs text-ios-red bg-ios-red/10 border border-ios-red/20 rounded-xl px-3 py-2">
          {{ fetchError }}
        </div>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">服务器名称</span>
          <input v-model="editForm.name" maxlength="40" class="form-input-base mt-1" />
        </label>
        <label class="block">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-neutral-600">地区</span>
            <span class="text-xs text-neutral-400">优先采用此地区展示国旗，留空则根据 IP 归属自动识别</span>
          </div>
          <input v-model="editForm.region" placeholder="例如 香港、日本东京、美国洛杉矶" class="form-input-base mt-1" />
        </label>
        <div class="block">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-neutral-600">服务器分组</span>
            <span class="text-xs text-neutral-400">用于服务器列表筛选，最多 10 个</span>
          </div>
          <div v-if="editForm.tags.length" class="flex flex-wrap gap-1.5 mt-1.5">
            <span v-for="item in editForm.tags" :key="item" class="inline-flex items-center gap-1 rounded-lg bg-ios-blue/[0.08] px-2 py-0.5 text-xs font-semibold text-ios-blue">
              {{ item }}
              <button type="button" class="text-ios-blue/70 hover:text-ios-blue" :aria-label="`删除标签 ${item}`" @click="editForm.tags = editForm.tags.filter(tag => tag !== item)">×</button>
            </span>
          </div>
          <div class="flex gap-2 mt-1.5">
            <input v-model="tagInput" list="server-tag-suggestions" placeholder="输入标签后回车，例如 香港、CN2" class="form-input-base flex-1" @keydown.enter.prevent="commitTag" />
            <button type="button" class="ios-btn-secondary shrink-0" @click="commitTag">添加</button>
          </div>
          <datalist id="server-tag-suggestions">
            <option v-for="item in knownTags.filter(tag => !editForm.tags.includes(tag))" :key="item" :value="item" />
          </datalist>
          <p v-if="tagError" class="text-xs text-ios-red mt-1">{{ tagError }}</p>
        </div>
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">服务器备注</span>
          <textarea v-model="editForm.remark" rows="2" placeholder="填写服务器规格等备注信息" class="form-input-base mt-1"></textarea>
        </label>

        <div class="border-t border-black/[0.06] pt-3.5 space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-xs font-semibold text-neutral-800">续费与财务信息</span>
            <span v-if="editForm.renewDate" class="text-xs text-neutral-500 font-mono">
              到期状态：<span class="font-bold text-neutral-800">{{ calculateRemainingDays(editForm.renewDate).label }}</span>
            </span>
          </div>

          <div class="space-y-3">
            <label class="block">
              <span class="text-xs font-medium text-neutral-600">续费到期日</span>
              <input v-model="editForm.renewDate" type="date" class="form-input-base mt-1" />
            </label>

            <div class="block">
              <span class="text-xs font-medium text-neutral-600">续费价格与周期</span>
              <div class="flex items-center mt-1 rounded-xl border border-black/15 bg-white overflow-hidden shadow-2xs focus-within:ring-2 focus-within:ring-ios-blue/20 focus-within:border-ios-blue transition-all">
                <select
                  v-model="editForm.renewCurrency"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-800 font-semibold px-3 py-2.5 border-r border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors text-sm"
                >
                  <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
                </select>
                <input
                  v-model="editForm.renewPrice"
                  type="number"
                  step="any"
                  min="0"
                  placeholder="输入续费金额，例如 120"
                  class="flex-1 min-w-0 px-3 py-2 font-mono font-medium text-neutral-900 placeholder:text-neutral-400 focus:outline-none bg-transparent text-sm"
                />
                <select
                  v-model="editForm.renewCycle"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-700 font-semibold px-3 py-2.5 border-l border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors text-sm"
                >
                  <option v-for="cycle in CYCLE_LIST" :key="cycle" :value="cycle">{{ cycle }}</option>
                </select>
              </div>
            </div>
          </div>
        </div>

        <div class="border-t border-black/[0.06] pt-3 space-y-2">
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.enabled" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>启用服务器控制连接</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.allowSubscription" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>允许用户订阅本服务器节点</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.maintenance" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>设为维护模式（暂时排除在订阅之外）</span>
          </label>
        </div>
      </div>
    </FormDrawer>

</template>
<script setup lang="ts">
import { ref, reactive } from 'vue';
import { nodeAPI, type NodeView } from '../api';
import { getNodeMeta, loadNodeMeta, saveNodeMeta, parseRenewPrice, calculateRemainingDays, CURRENCY_LIST, CYCLE_LIST } from '../nodeMeta';
import { mergeLatestEditForm, type ServerEditFormState } from './serverListModel';
import { addTag } from '../serverOrder';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import FormDrawer from '../../../components/FormDrawer.vue';
const emit = defineEmits<{ changed: [] }>();
withDefaults(defineProps<{ knownTags?: string[] }>(), { knownTags: () => [] });
const tagInput = ref(''), tagError = ref('');
function commitTag() {
  const result = addTag(editForm.tags, tagInput.value);
  tagError.value = result.error || '';
  if (!result.error) { editForm.tags = result.tags; tagInput.value = ''; }
}
const editDrawer = ref(false);
const editingSaving = ref(false);
const fetchingLatest = ref(false);
const fetchError = ref('');
const editTarget = ref<NodeView | null>(null);
const editForm = reactive<ServerEditFormState>({
  name: '',
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  enabled: true,
  allowSubscription: true,
  maintenance: false,
  tags: []
});
let loadToken = 0;

async function openEditDrawer(node: NodeView) {
  if (node.deletedAt) {
    showToast('服务器已被删除', 'info');
    return;
  }
  const cachedMeta = getNodeMeta(node.id);
  const parsed = parseRenewPrice(cachedMeta.renewPrice, cachedMeta.renewCurrency, cachedMeta.renewCycle);
  editForm.name = node.name;
  editForm.region = cachedMeta.region || '';
  editForm.tags = [...(cachedMeta.tags ?? node.tags ?? [])];
  tagInput.value = ''; tagError.value = '';
  editForm.remark = cachedMeta.remark || '';
  editForm.renewDate = cachedMeta.renewDate || '';
  editForm.renewCurrency = parsed.currency;
  editForm.renewPrice = parsed.price;
  editForm.renewCycle = parsed.cycle;
  editForm.enabled = node.enabled;
  editForm.allowSubscription = node.allowSubscription;
  editForm.maintenance = node.maintenance;

  const baseline: ServerEditFormState = { ...editForm };
  editTarget.value = node;
  fetchingLatest.value = true;
  fetchError.value = '';
  editDrawer.value = true;

  const currentToken = ++loadToken;
  try {
    const [latest, meta] = await Promise.all([nodeAPI.get(node.id), loadNodeMeta(node.id, true)]);
    if (currentToken !== loadToken || !editDrawer.value || editTarget.value?.id !== node.id) return;
    if (latest.deletedAt) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    editTarget.value = latest;
    mergeLatestEditForm(editForm, baseline, latest, meta);
  } catch (e: unknown) {
    if (currentToken !== loadToken || !editDrawer.value || editTarget.value?.id !== node.id) return;
    const isNotFound = e instanceof APIError && e.status === 404;
    if (isNotFound) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    fetchError.value = (e as Error).message || '读取最新信息失败';
  } finally {
    if (currentToken === loadToken) {
      fetchingLatest.value = false;
    }
  }
}

async function saveEdit() {
  if (!editTarget.value || editingSaving.value || fetchingLatest.value) return;
  editingSaving.value = true;
  try {
    const [latest, meta] = await Promise.all([nodeAPI.get(editTarget.value.id), loadNodeMeta(editTarget.value.id, true)]);
    if (latest.deletedAt) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    // The address fields are left out on purpose: this dialog does not edit them,
    // and echoing the loaded public IP back would mark a detected address manual.
    await nodeAPI.save({
      id: editTarget.value.id,
      name: editForm.name,
      enabled: editForm.enabled,
      allowSubscription: editForm.allowSubscription,
      maintenance: editForm.maintenance,
      outboundSourceIp: latest.outboundSourceIp || '',
      outboundInterface: latest.outboundInterface || '',
      hostSourceIpv4: latest.hostSourceIpv4 || '',
      hostSourceIpv6: latest.hostSourceIpv6 || ''
    });

    await saveNodeMeta(editTarget.value.id, {
      ...meta,
      region: editForm.region.trim(),
      remark: editForm.remark.trim(),
      renewDate: editForm.renewDate,
      renewCurrency: editForm.renewCurrency,
      renewPrice: String(editForm.renewPrice).trim(),
      renewCycle: editForm.renewCycle,
      tags: editForm.tags
    });

    showToast('服务器设置已更新');
    editDrawer.value = false;
    emit('changed');
  } catch (e: unknown) {
    const isNotFound = e instanceof APIError && e.status === 404;
    if (isNotFound) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    showToast((e as Error).message, 'error');
  } finally {
    editingSaving.value = false;
  }
}

defineExpose({ open: openEditDrawer });
</script>
