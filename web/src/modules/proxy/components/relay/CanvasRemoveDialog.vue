<template>
  <AppDialog :model-value="true" title="停用并移除" max-width="2xl" @update:model-value="close">
    <div class="space-y-4 text-sm text-ios-label">
      <p class="font-semibold">{{ scheme.name }}</p>
      <p class="text-ios-orange">撤除期间相关用户连接会短暂中断</p>
      <p v-if="error" role="alert" class="text-ios-red whitespace-pre-wrap">{{ error }}</p>
      <p v-if="loading" role="status">正在读取影响预览…</p>
      <p v-if="refreshingTargets" role="status">正在核对恢复目标并更新资源影响…</p>
      <template v-if="view">
        <section class="space-y-2">
          <h4 class="text-base font-semibold">用户入口与分组</h4>
          <p v-if="!view.entries.length" class="text-ios-secondary">无用户入口需要删除或恢复</p>
          <div v-for="entry in view.entries" :key="entry.bindingId" class="rounded-xl border border-ios-border p-3 space-y-2 break-words">
            <p class="font-semibold">{{ entry.name }} · {{ entry.nodeName }}</p>
            <p>{{ entry.actionText }}</p>
            <p>{{ entry.protocol }} · {{ entry.core || '主核心' }} · {{ entry.listen }}:{{ entry.port }}</p>
            <p>当前出站：{{ routeName(entry.currentRoute) }}</p>
            <p>所属分组：{{ entry.groupsText || '无' }}</p>
            <p v-if="entry.referrersText" class="text-ios-red">引用阻止删除：{{ entry.referrersText }}</p>
            <label v-if="entry.needsRestoreTarget || entry.bindingId in targets" class="block space-y-1">
              <span>{{ entry.restoreReason || '恢复目标' }}；请选择恢复目标</span>
              <select v-model="targets[entry.bindingId]" class="ios-input w-full" :disabled="busy || loading">
                <option value="">请选择恢复目标</option>
                <option v-for="option in options(entry.nodeId)" :key="option.value" :value="option.value">{{ option.label }}</option>
              </select>
            </label>
          </div>
        </section>
        <section class="space-y-2">
          <h4 class="text-base font-semibold">将清理的通道资源</h4>
          <p v-if="!view.removeObjects.length" class="text-ios-secondary">无</p>
          <p v-for="item in view.removeObjects" :key="`${item.kind}:${item.id}`" class="break-words">{{ item.kindText }} · {{ item.name }} · {{ item.nodeName }}{{ item.port ? ` · ${item.port}` : '' }}</p>
        </section>
        <section class="space-y-2">
          <h4 class="text-base font-semibold">保留的共享资源及引用者</h4>
          <p v-if="!view.keepObjects.length" class="text-ios-secondary">无</p>
          <div v-for="item in view.keepObjects" :key="`${item.kind}:${item.id}`" class="break-words">
            <p>{{ item.kindText }} · {{ item.name }} · {{ item.nodeName }}</p>
            <p class="text-ios-secondary">{{ item.referrersText }}</p>
          </div>
        </section>
        <section class="space-y-2">
          <h4 class="text-base font-semibold">受影响服务器与顺序</h4>
          <p>先停止或恢复用户入口，等待确认后按以下顺序清理通道。</p>
          <p v-for="node in view.nodes" :key="node.id">{{ node.order }}. {{ node.name }}</p>
        </section>
        <section v-if="view.changes.length" class="space-y-2">
          <h4 class="text-base font-semibold">人工改动与待确认配置</h4>
          <p class="text-ios-secondary">{{ plan?.changeDetectionRule }}</p>
          <p v-for="(change, index) in view.changes" :key="index" class="break-words">{{ change }}</p>
          <label class="flex items-start gap-2"><input v-model="acknowledged" type="checkbox" :disabled="busy || loading" class="mt-1" />我已了解上述改动，继续撤除</label>
        </section>
      </template>
      <p v-if="busy" role="status">正在等待服务器确认撤除，请勿离开页面…</p>
    </div>
    <template #footer>
      <button type="button" class="ios-dialog-btn-secondary" :disabled="busy" @click="close">关闭</button>
      <button v-if="error && !busy" type="button" class="ios-dialog-btn-secondary" :disabled="loading" @click="loadPreview">刷新预览</button>
      <button v-if="canConfirm" type="button" class="ios-btn-danger" :disabled="busy || loading" @click="remove">{{ busy ? '撤除中…' : '确认停用并移除' }}</button>
    </template>
  </AppDialog>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import AppDialog from '../../../../components/AppDialog.vue';
import { APIError } from '../../../../app/apiClient';
import { canvasAPI, choices, proxyAPI, type CanvasScheme, type CanvasRemovePlan, type Outbound, type ProxyChain } from '../../api';
import { canvasRemoveView, canConfirmCanvasRemove, canvasRestoreOptions } from '../../canvasRemove';

const props = defineProps<{ scheme: CanvasScheme; servers: { id: string; name: string }[] }>();
const emit = defineEmits<{ close: []; removed: [id: string] }>();
const plan = ref<CanvasRemovePlan | null>(null);
const targets = ref<Record<string, string>>({});
const acknowledged = ref(false);
const loading = ref(false);
const busy = ref(false);
const refreshingTargets = ref(false);
const previewValid = ref(false);
const error = ref('');
const outbounds = ref<Outbound[]>([]);
const chains = ref<ProxyChain[]>([]);
const view = computed(() => plan.value ? canvasRemoveView(plan.value, id => props.servers.find(node => node.id === id)?.name || id, routeName) : null);
const canConfirm = computed(() => previewValid.value && !loading.value && !refreshingTargets.value && plan.value !== null && canConfirmCanvasRemove(plan.value, targets.value, acknowledged.value));
function routeName(value: string) {
  if (value === 'direct') return '直连';
  if (value === 'block') return '阻断';
  return (value.startsWith('chain:') ? chains.value.find(item => item.id === value.slice(6))?.name : outbounds.value.find(item => item.id === value)?.name) || value;
}
function options(nodeId: string) { return canvasRestoreOptions(nodeId, outbounds.value, chains.value); }
function close() { if (!busy.value) emit('close'); }
async function loadPreview() {
  if (loading.value || busy.value) return;
  targetRequest++;
  refreshingTargets.value = false;
  loading.value = true;
  previewValid.value = false;
  plan.value = null;
  targets.value = {};
  acknowledged.value = false;
  try {
    const scheme = await canvasAPI.get(props.scheme.id);
    const [preview, outboundItems, chainItems] = await Promise.all([
      canvasAPI.removePlan(scheme.id, { updatedAt: scheme.updatedAt }),
      choices((page, size) => proxyAPI.outbounds('', page, size)),
      choices((page, size) => proxyAPI.chains('', page, size))
    ]);
    outbounds.value = outboundItems;
    chains.value = chainItems;
    plan.value = preview;
    for (const entry of preview.entries) if (entry.needsRestoreTarget) targets.value[entry.bindingId] = '';
    previewValid.value = true;
  } catch (cause) { error.value = (cause as Error).message; }
  finally { loading.value = false; }
}
let targetRequest = 0;
watch(() => JSON.stringify(targets.value), async () => {
  if (loading.value || busy.value || !plan.value) return;
  const request = ++targetRequest;
  previewValid.value = false;
  refreshingTargets.value = false;
  if (Object.values(targets.value).some(value => !value)) return;
  refreshingTargets.value = true;
  try {
    const preview = await canvasAPI.removePlan(props.scheme.id, { updatedAt: plan.value.updatedAt, restoreTargets: { ...targets.value }, acknowledgeChanges: acknowledged.value });
    if (request !== targetRequest) return;
    plan.value = preview;
    previewValid.value = !preview.entries.some(entry => entry.needsRestoreTarget);
    error.value = '';
  } catch (cause) {
    if (request === targetRequest) {
      const stale = cause instanceof APIError && cause.code === 'stale_scheme';
      error.value = stale ? '方案已被修改，请刷新后重试' : (cause as Error).message;
      if (stale) await loadPreview();
    }
  } finally { if (request === targetRequest) refreshingTargets.value = false; }
}, { flush: 'sync' });
async function remove() {
  if (busy.value || !canConfirm.value || !plan.value) return;
  busy.value = true;
  error.value = '';
  let stale = false;
  try {
    const result = await canvasAPI.remove(props.scheme.id, { updatedAt: plan.value.updatedAt, restoreTargets: { ...targets.value }, acknowledgeChanges: acknowledged.value });
    if (!result.removed) throw new Error('服务器尚未确认方案已移除');
    emit('removed', result.schemeId);
  } catch (cause) {
    if (cause instanceof APIError && cause.code === 'stale_scheme') {
      error.value = '方案已被修改，请刷新后重试';
      stale = true;
    } else {
      error.value = (cause as Error).message;
      if (cause instanceof APIError && cause.code === 'canvas_remove_conflict') {
        plan.value = cause.details.details as CanvasRemovePlan;
        acknowledged.value = false;
        for (const entry of plan.value.entries) if (entry.needsRestoreTarget) targets.value[entry.bindingId] = '';
      }
    }
  } finally { busy.value = false; }
  if (stale) await loadPreview();
}
onMounted(loadPreview);
</script>
