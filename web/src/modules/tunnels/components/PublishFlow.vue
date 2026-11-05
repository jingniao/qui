<template>
  <div>
    <button  :class="dialogButton ? 'ios-dialog-btn-primary' : 'ios-btn-primary'" :disabled="disabled" @click="open">发布</button>
    <FormDrawer :model-value="visible" title="发布隧道与规则" width="2xl" :show-footer="false" @update:model-value="value => { if (!busy) visible = value; }">
      <fieldset :disabled="busy || !!result" class="space-y-3" @change="plan = null">
        <p class="text-xs text-ios-tertiary">选择需要发布的草稿；服务端会计算依赖及出口到入口的发布顺序。</p>
        <div class="grid sm:grid-cols-2 gap-4">
          <section class="space-y-2"><h4 class="text-[15px] font-semibold text-ios-label">隧道</h4><label v-for="t in tunnels" :key="t.id" class="flex gap-2 items-center text-sm text-ios-label"><input v-model="selection.tunnelIds" type="checkbox" :value="t.id" />{{ t.name }}<PublishBadge :status="t.status" /></label></section>
          <section class="space-y-2"><h4 class="text-[15px] font-semibold text-ios-label">规则</h4><label v-for="r in rules" :key="r.id" class="flex gap-2 items-center text-sm text-ios-label"><input v-model="selection.ruleIds" type="checkbox" :value="r.id" />{{ r.name }}<PublishBadge :status="r.status" /></label></section>
        </div>
        
      </fieldset>
      <p v-if="error" class="text-sm text-ios-red" role="alert">{{ error }}</p>
      <section v-if="plan" class="space-y-3" aria-live="polite">
        <p :class="plan.ok ? 'text-ios-green' : 'text-ios-red'" class="text-sm">{{ plan.message || (plan.ok ? '计划检查通过' : '计划检查未通过') }}</p>
        <DataTable><template #header><th>顺序</th><th>服务器</th><th>监听端口 / 网络 / 用途</th><th>路由</th><th>变更</th></template><template #body><tr v-for="row in planToRows(plan)" :key="row.nodeId"><td class="p-4 font-mono tabular-nums">{{ row.order }}</td><td>{{ row.nodeName }}</td><td class="font-mono tabular-nums">{{ row.listenerText || '—' }}</td><td><p v-for="route in row.routes" :key="route" class="font-mono tabular-nums">{{ route }}</p></td><td>{{ row.changeText || '无变更' }}</td></tr></template></DataTable>
        <p v-for="(c, i) in plan.conflicts" :key="i" class="text-sm text-ios-red font-mono tabular-nums">{{ nodeName(c.nodeId) }}:{{ c.port }} 已被 {{ c.usedBy }} 占用</p>
        <p v-for="(w, i) in plan.warnings" :key="i" class="text-sm text-ios-orange">{{ w }}</p>
      </section>
      <section v-if="result" class="space-y-3" aria-live="polite"><h4 class="text-[15px] font-semibold text-ios-label">发布结果</h4><DataTable><template #header><th>服务器</th><th>版本</th><th>状态</th><th>说明</th></template><template #body><tr v-for="s in result.servers" :key="s.nodeId"><td class="p-4">{{ s.nodeName }}</td><td class="font-mono tabular-nums">{{ s.revision ?? '—' }}</td><td><StatusBadge :status="s.status === 'applied' || s.status === 'unchanged' ? 'success' : s.status === 'rolled_back' ? 'rollback' : s.status === 'failed' ? 'failed' : 'disabled'" :label="resultLabels[s.status]" /></td><td>{{ s.message || '—' }}</td></tr></template></DataTable></section>
      <div class="flex justify-end gap-3 pt-3">
        <button class="ios-dialog-btn-secondary" :disabled="busy" @click="visible = false">{{ result ? '关闭' : '取消' }}</button>
        <button v-if="!result" class="ios-dialog-btn-secondary" :disabled="busy || !hasSelection" @click="check">检查发布计划</button>
        <button v-if="!result" class="ios-dialog-btn-primary" :disabled="busy || !plan?.ok || !!plan?.conflicts.length" @click="publish">{{ busy ? '处理中…' : '确认发布' }}</button>
      </div>
    </FormDrawer>
  </div>
</template>
<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import PublishBadge from './PublishBadge.vue';
import { tunnelAPI, tunnelError, type Tunnel, type TunnelRule, type PublishPlan, type PublishResult, type PublishSelection } from '../api';
import { planToRows, resultLabels } from '../model';
const props = defineProps<{ tunnels: Tunnel[]; rules: TunnelRule[]; disabled?: boolean; dialogButton?: boolean; preselect?: PublishSelection }>();
const emit = defineEmits<{ published: [selection: PublishSelection, result: PublishResult] }>();
const visible = ref(false), busy = ref(false), error = ref(''), plan = ref<PublishPlan | null>(null), result = ref<PublishResult | null>(null);
const selection = reactive<PublishSelection>({ tunnelIds: [], ruleIds: [] });
const hasSelection = computed(() => selection.tunnelIds.length + selection.ruleIds.length > 0);
function reset() { plan.value = null; result.value = null; error.value = ''; }
function open() {
  reset();
  selection.tunnelIds = props.preselect?.tunnelIds || props.tunnels.filter(t => t.status !== 'published').map(t => t.id);
  selection.ruleIds = props.preselect?.ruleIds || props.rules.filter(r => r.status !== 'published').map(r => r.id);
  visible.value = true;
}
function nodeName(id: string) { return plan.value?.servers.find(s => s.nodeId === id)?.nodeName || id; }
async function check() {
  busy.value = true; error.value = ''; plan.value = null;
  try { plan.value = await tunnelAPI.plan(selection); } catch (e) { error.value = tunnelError(e); } finally { busy.value = false; }
}
async function publish() {
  if (!plan.value?.ok || plan.value.conflicts.length || busy.value) return;
  busy.value = true; error.value = '';
  try { result.value = await tunnelAPI.publish(selection); emit('published', { tunnelIds: [...selection.tunnelIds], ruleIds: [...selection.ruleIds] }, result.value); } catch (e) { error.value = tunnelError(e); plan.value = null; } finally { busy.value = false; }
}
</script>
