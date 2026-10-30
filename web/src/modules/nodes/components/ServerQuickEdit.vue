<template>
    <FormDrawer
      v-model="editDrawer"
      title="编辑服务器"
      description="修改服务器接入地址、维护状态、续费价格与扩展信息。"
      :loading="editingSaving"
      @confirm="saveEdit"
    >
      <div class="space-y-4 text-sm" v-if="editTarget">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">服务器名称</span>
          <input v-model="editForm.name" maxlength="40" class="form-input-base mt-1" />
        </label>
        <label class="block">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-neutral-600">地区</span>
            <span class="text-[11px] text-neutral-400">优先采用此地区展示国旗，留空则根据 IP 归属自动识别</span>
          </div>
          <input v-model="editForm.region" placeholder="例如 香港、日本东京、美国洛杉矶" class="form-input-base mt-1" />
        </label>
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
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-800 font-semibold text-xs px-3 py-2.5 border-r border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                >
                  <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
                </select>
                <input
                  v-model="editForm.renewPrice"
                  type="number"
                  step="any"
                  min="0"
                  placeholder="输入续费金额，例如 120"
                  class="flex-1 min-w-0 px-3 py-2 text-xs font-mono font-medium text-neutral-900 placeholder:text-neutral-400 focus:outline-none bg-transparent"
                />
                <select
                  v-model="editForm.renewCycle"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-700 font-semibold text-xs px-3 py-2.5 border-l border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
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
import { loadNodeMeta, saveNodeMeta, parseRenewPrice, calculateRemainingDays, CURRENCY_LIST, CYCLE_LIST } from '../nodeMeta';
import { showToast } from '../../../app/api';
import FormDrawer from '../../../components/FormDrawer.vue';
const emit = defineEmits<{ changed: [] }>();
const editDrawer = ref(false);
const editingSaving = ref(false);
const editTarget = ref<NodeView | null>(null);
const editForm = reactive({
  name: '',
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  enabled: true,
  allowSubscription: true,
  maintenance: false
});

async function openEditDrawer(node: NodeView) {
  try {
    const [latest, meta] = await Promise.all([nodeAPI.get(node.id), loadNodeMeta(node.id, true)]);
    if (latest.deletedAt) return;
    node = latest;
    editTarget.value = latest;
    const parsed = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
    editForm.name = node.name;
    editForm.region = meta.region || '';
    editForm.remark = meta.remark || '';
    editForm.renewDate = meta.renewDate || '';
    editForm.renewCurrency = parsed.currency;
    editForm.renewPrice = parsed.price;
    editForm.renewCycle = parsed.cycle;
    editForm.enabled = node.enabled;
    editForm.allowSubscription = node.allowSubscription;
    editForm.maintenance = node.maintenance;
    editDrawer.value = true;
  } catch (e) { showToast((e as Error).message, 'error'); }
}

async function saveEdit() {
  if (!editTarget.value || editingSaving.value) return;
  editingSaving.value = true;
  try {
    const [latest, meta] = await Promise.all([nodeAPI.get(editTarget.value.id), loadNodeMeta(editTarget.value.id, true)]);
    await nodeAPI.save({
      id: editTarget.value.id,
      name: editForm.name,
      publicIp: latest.publicIp || '',
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
      renewCycle: editForm.renewCycle
    });

    showToast('服务器设置已更新');
    editDrawer.value = false;
    emit('changed');
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    editingSaving.value = false;
  }
}

defineExpose({ open: openEditDrawer });
</script>
