<template>
  <div>
    <button  :class="dialogButton ? 'ios-dialog-btn-primary' : 'ios-btn-primary'" :disabled="disabled" @click="open">发布</button>
    <FormDrawer :model-value="visible" title="发布隧道与规则" width="2xl" :show-footer="false" @update:model-value="value => { if (!busy) visible = value; }">
      <fieldset :disabled="busy || !!result" class="space-y-3" @change="plan = null">
        <p class="text-xs text-ios-tertiary">选择需要发布的草稿；服务端会计算依赖及出口到入口的发布顺序。</p>
        <div class="grid sm:grid-cols-2 gap-4">
          <section class="space-y-2"><h4 class="text-ios-label text-base font-semibold leading-normal">隧道</h4><label v-for="t in tunnels" :key="t.id" class="flex gap-2 items-center text-sm text-ios-label"><input v-model="selection.tunnelIds" type="checkbox" :value="t.id" />{{ t.name }}<PublishBadge :status="t.status" /></label></section>
          <section class="space-y-2"><h4 class="text-ios-label text-base font-semibold leading-normal">规则</h4><label v-for="r in rules" :key="r.id" class="flex gap-2 items-center text-sm text-ios-label"><input v-model="selection.ruleIds" type="checkbox" :value="r.id" />{{ r.name }}<PublishBadge :status="r.status" /></label></section>
        </div>
        
      </fieldset>
      <p v-if="error" class="text-sm text-ios-red" role="alert">{{ error }}</p>
      <p v-if="progress" class="text-sm text-ios-secondary" aria-live="polite">{{ progress }}</p>
      <p v-if="plan?.installGost?.length" class="text-sm text-ios-orange">将先为以下服务器安装 GOST 3.3.2：{{ plan.installGost.map(s => s.name).join('、') }}</p>
      <p v-for="line in installResults" :key="line" class="text-sm text-ios-secondary">{{ line }}</p>
      <p v-if="result?.gostRemovable?.length" class="text-sm text-ios-secondary">以下服务器的 GOST 已不再使用，可在「版本与更新 → 代理核心」中移除：{{ result.gostRemovable.map(s => s.name).join('、') }}。<RouterLink to="/nodes/versions" class="text-ios-blue">版本与更新 → 代理核心</RouterLink></p>
      <section v-if="plan" class="space-y-3" aria-live="polite">
        <p :class="plan.ok ? 'text-ios-green' : 'text-ios-red'" class="text-sm">{{ plan.message || (plan.ok ? '计划检查通过' : '计划检查未通过') }}</p>
        <DataTable><template #header><th>顺序</th><th>服务器</th><th>监听端口 / 网络 / 用途</th><th>路由</th><th>变更</th></template><template #body><tr v-for="row in planToRows(plan)" :key="row.nodeId"><td class="p-4 font-mono tabular-nums">{{ row.order }}</td><td>{{ row.nodeName }}</td><td class="font-mono tabular-nums">{{ row.listenerText || '—' }}</td><td><p v-for="route in row.routes" :key="route" class="font-mono tabular-nums">{{ route }}</p></td><td>{{ row.changeText || '无变更' }}</td></tr></template></DataTable>
        <p v-for="(c, i) in plan.conflicts" :key="i" class="text-sm text-ios-red font-mono tabular-nums">{{ nodeName(c.nodeId) }}:{{ c.port }} 已被 {{ c.usedBy }} 占用</p>
        <p v-for="(w, i) in plan.warnings" :key="i" class="text-sm text-ios-orange">{{ w }}</p>
      </section>
      <section v-if="result" class="space-y-3" aria-live="polite"><h4 class="text-ios-label text-base font-semibold leading-normal">发布结果</h4><DataTable><template #header><th>服务器</th><th>版本</th><th>状态</th><th>说明</th></template><template #body><tr v-for="s in result.servers" :key="s.nodeId"><td class="p-4">{{ s.nodeName }}</td><td class="font-mono tabular-nums">{{ s.revision ?? '—' }}</td><td><StatusBadge :status="s.status === 'applied' || s.status === 'unchanged' ? 'success' : s.status === 'rolled_back' ? 'rollback' : s.status === 'failed' ? 'failed' : 'disabled'" :label="resultLabels[s.status]" /></td><td>{{ s.message || '—' }}</td></tr></template></DataTable></section>
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
import { APIError } from '../../../app/apiClient';
import { nodeAPI } from '../../nodes/api';
import { confirmedRevision, needsBaseline, publishBaselineAndWait } from '../../system/nodeBaseline';
import { gostInstallRelease } from '../../system/coreReleases';
import { coreUpdatesAPI } from '../../system/coreUpdatesApi';
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
const progress = ref(''), installResults = ref<string[]>([]);
const hasSelection = computed(() => selection.tunnelIds.length + selection.ruleIds.length > 0);
function reset() { plan.value = null; result.value = null; error.value = ''; progress.value = ''; installResults.value = []; }
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
  try {
    installResults.value = [];
    let failed = false;
    for (const server of plan.value.installGost || []) {
      progress.value = `正在为 ${server.name} 安装 GOST…`;
      try {
        let node = await nodeAPI.get(server.nodeId);
        if (!node.capabilities?.cores?.some(core => core.name === 'gost')) {
          if (needsBaseline(node)) {
            progress.value = `正在为 ${server.name} 发布基础配置…`;
            await publishBaselineAndWait(node);
          }
          progress.value = `正在为 ${server.name} 安装 GOST…`;
          const release = await gostInstallRelease(node);
          await coreUpdatesAPI.queue({ nodeId: server.nodeId, revision: confirmedRevision(node), component: 'gost', ...release });
          const deadline = Date.now() + 300000;
          while (!node.capabilities?.cores?.some(core => core.name === 'gost')) {
            const remaining = deadline - Date.now();
            if (remaining <= 0) throw new Error('等待 GOST 安装超时（5 分钟）');
            await new Promise(resolve => setTimeout(resolve, Math.min(3000, remaining)));
            if (Date.now() >= deadline) throw new Error('等待 GOST 安装超时（5 分钟）');
            node = await nodeAPI.get(server.nodeId);
          }
        }
        installResults.value.push(`${server.name}：GOST 已就绪`);
      } catch (e) { failed = true; installResults.value.push(`${server.name}：${tunnelError(e)}`); }
    }
    if (failed) { error.value = 'GOST 安装未全部成功，未发布隧道；已安装的核心保留。'; return; }
    progress.value = '正在发布隧道…';
    result.value = await tunnelAPI.publish(selection);
    emit('published', { tunnelIds: [...selection.tunnelIds], ruleIds: [...selection.ruleIds] }, result.value);
  } catch (e) {
    if (e instanceof APIError && e.status === 409 && e.code === 'gost_install_required') {
      const ids = e.details.nodeIds;
      if (Array.isArray(ids) && ids.every(id => typeof id === 'string')) {
        plan.value.installGost = ids.map(nodeId => ({ nodeId, name: nodeName(nodeId) }));
        error.value = '服务器状态已变化，请再次确认发布以安装上述 GOST。';
      } else { error.value = tunnelError(e); plan.value = null; }
    } else { error.value = tunnelError(e); plan.value = null; }
  } finally { busy.value = false; progress.value = ''; }
}
</script>
