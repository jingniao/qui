<template>
  <div class="space-y-6">
    <PageHeader title="隧道管理" parent="节点配置" description="创建、修改或删除隧道与规则后立即下发到相关服务器。"><template #actions>
      <button class="ios-btn-secondary" :disabled="loading" @click="load">刷新</button>
      <button v-if="isOwner" class="ios-btn-primary" :disabled="loading || !!error" @click="editingTunnel = null; editingRule = null; editor = true">{{ rulesTab ? '创建规则' : '创建隧道' }}</button>
    </template></PageHeader>
    <nav class="ios-segmented-container inline-flex" aria-label="隧道管理页签"><RouterLink to="/proxy/tunnels" class="ios-segmented-item" :class="{ active: !rulesTab }">隧道</RouterLink><RouterLink to="/proxy/tunnel-rules" class="ios-segmented-item" :class="{ active: rulesTab }">规则</RouterLink></nav>
    <p v-if="mockEnabled()" class="text-xs text-ios-orange">开发演示数据 · 刷新页面后重置</p>
    <p v-if="applyNote" :class="{ info: 'text-ios-secondary', error: 'text-ios-red', warn: 'text-ios-orange' }[applyNote.tone]" class="text-sm" role="status" aria-live="polite">{{ applyNote.text }}</p>
    <div v-if="rulesTab" class="flex flex-wrap gap-3">
      <select v-model="tunnelFilter" class="ios-filter-select" aria-label="按隧道筛选"><option value="">全部隧道</option><option value="direct">直接转发</option><option v-for="t in tunnels" :key="t.id" :value="t.id">{{ t.name }}</option></select>
      <select v-model="nodeFilter" class="ios-filter-select" aria-label="按入口节点筛选"><option value="">全部入口节点</option><option v-for="n in nodes" :key="n.id" :value="n.id">{{ n.name }}</option></select>
    </div>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <p v-else-if="loading" class="text-sm text-ios-tertiary">加载中…</p>
    <div v-else-if="!rulesTab">
      <EmptyState v-if="!tunnels.length" title="暂无隧道" description="创建入口到出口的加密隧道，再添加转发规则。" />
      <DataTable v-else><template #header><th>名称 / 路径</th><th>传输</th><th>接收端口</th><th>规则数</th><th>状态</th><th v-if="isOwner">操作</th></template>
        <template #body><tr v-for="t in tunnels" :key="t.id">
          <td class="p-4"><p class="text-base font-semibold text-ios-label">{{ t.name }}</p><p class="text-sm text-ios-secondary mt-0.5">{{ routeLabel(t) }}</p></td><td>TLS</td>
          <td class="font-mono tabular-nums text-xs"><p v-for="h in t.hops.filter(h => h.role !== 'entry')" :key="h.role">{{ h.nodeName }}:{{ h.listenPort }}</p></td><td class="font-mono tabular-nums">{{ t.ruleCount }}</td><td><PublishBadge :status="t.status" /></td>
          <td v-if="isOwner"><div class="flex gap-2"><button v-if="t.status !== 'published'" class="ios-action-btn-primary" :disabled="mutating" @click="apply({ tunnelIds: [t.id], ruleIds: [] }, t.status === 'pending_delete' ? '隧道已删除' : '隧道已生效')">重试</button><template v-if="t.status !== 'pending_delete'"><button class="ios-action-btn-primary" :disabled="mutating" @click="editingTunnel = t; editor = true">编辑</button><button class="ios-action-btn-secondary" :disabled="mutating" @click="ask({ kind: 'rotate', tunnel: t })">轮换凭据</button><button class="ios-action-btn-danger" :disabled="mutating" @click="ask({ kind: 'tunnel', tunnel: t })">删除</button></template></div></td>
        </tr></template>
      </DataTable>
    </div>
    <div v-else class="space-y-5">
      <EmptyState v-if="!groups.length" title="暂无规则" description="创建转发规则，或调整筛选条件。" />
      <section v-for="group in groups" :key="group.id || 'direct'" class="space-y-3">
        <h3 class="text-ios-label text-lg font-semibold leading-snug">{{ group.name }} <span class="text-sm font-normal text-ios-secondary">{{ group.route }}</span></h3>
        <DataTable><template #header><th>规则 / 转发路径</th><th>所属节点</th><th>对用户可见</th><th>启用</th><th>状态</th><th v-if="isOwner">操作</th></template>
          <template #body><tr v-for="r in group.rules" :key="r.id">
            <td class="p-4"><p class="text-base font-semibold text-ios-label">{{ r.name }}</p><p class="text-sm text-ios-secondary mt-0.5"><span class="font-mono tabular-nums">{{ r.entryNodeName }}:{{ r.listenPort }}</span> → {{ r.tunnelName ? `经 ${r.tunnelName}` : '直接转发' }} → <span>{{ ruleTargetLabel(r.target) }}</span></p><p class="text-xs text-ios-tertiary">{{ r.network.toUpperCase() }}</p></td>
            <td>{{ nodes.find(n => n.id === r.ownerNodeId)?.name || r.ownerNodeId || '—' }}</td><td>{{ r.subscriptionVisible ? '可见' : '隐藏' }}</td><td><StatusBadge :status="r.enabled ? 'active' : 'disabled'" /></td><td><PublishBadge :status="r.status" /></td>
            <td v-if="isOwner"><div class="flex gap-2"><button v-if="r.status !== 'published'" class="ios-action-btn-primary" :disabled="mutating" @click="apply({ tunnelIds: [], ruleIds: [r.id] }, r.status === 'pending_delete' ? '规则已删除' : '规则已生效')">重试</button><template v-if="r.status !== 'pending_delete'"><button class="ios-action-btn-primary" :disabled="mutating" @click="editingRule = r; editor = true">编辑</button><button class="ios-action-btn-danger" :disabled="mutating" @click="ask({ kind: 'rule', rule: r })">删除</button></template></div></td>
          </tr></template>
        </DataTable>
      </section>
    </div>
    <TunnelEditor v-if="editor && !rulesTab" :tunnel="editingTunnel" :nodes="nodes" :linked-rules="rules.filter(r => r.tunnelId === editingTunnel?.id)" @close="editor = false" @saved="selection => { editor = false; apply(selection, '隧道已保存并生效'); }" />
    <RuleEditor v-if="editor && rulesTab" :rule="editingRule" :tunnels="tunnels" :nodes="nodes" :inbounds="inbounds" @close="editor = false" @saved="selection => { editor = false; apply(selection, '规则已保存并生效'); }" />
    <ConfirmDialog v-model="confirmVisible" :title="confirmAction?.kind === 'rotate' ? '轮换隧道凭据' : '删除'" :message="confirmMessage" @confirm="performAction" />
    <FormDrawer v-model="blockedVisible" title="隧道仍被规则使用" :show-footer="false"><p class="text-sm text-ios-secondary">请先移除以下规则对隧道的引用：</p><ul class="space-y-2 text-sm text-ios-label"><li v-for="r in blockers" :key="r.id">{{ r.name }}</li></ul><div class="flex justify-end"><button class="ios-dialog-btn-secondary" @click="blockedVisible = false">关闭</button></div></FormDrawer>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { isOwner } from '../../../app/auth';
import { APIError } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import TunnelEditor from '../components/TunnelEditor.vue';
import RuleEditor from '../components/RuleEditor.vue';
import PublishBadge from '../components/PublishBadge.vue';
import { loadTunnelChoices, mockEnabled, tunnelAPI, tunnelRuleAPI, tunnelError, type PublishSelection, type Tunnel, type TunnelRule } from '../api';
import { applyProblem, applyTunnelChanges } from '../applyTunnels';
import { ruleTargetLabel, endpoint, groupRules, routeLabel } from '../model';
const route = useRoute(), rulesTab = computed(() => route.path === '/proxy/tunnel-rules');
const tunnels = ref<Tunnel[]>([]), rules = ref<TunnelRule[]>([]);
const nodes = ref<Awaited<ReturnType<typeof loadTunnelChoices>>['nodes']>([]), inbounds = ref<Awaited<ReturnType<typeof loadTunnelChoices>>['inbounds']>([]);
const loading = ref(true), error = ref(''), mutating = ref(false), editor = ref(false), editingTunnel = ref<Tunnel | null>(null), editingRule = ref<TunnelRule | null>(null);
const tunnelFilter = ref(''), nodeFilter = ref('');
const groups = computed(() => groupRules(rules.value.filter(r => (!nodeFilter.value || r.entryNodeId === nodeFilter.value) && (!tunnelFilter.value || (tunnelFilter.value === 'direct' ? !r.tunnelId : r.tunnelId === tunnelFilter.value))), tunnels.value));
type Action = { kind: 'rotate' | 'tunnel'; tunnel: Tunnel } | { kind: 'rule'; rule: TunnelRule };
const confirmAction = ref<Action | null>(null), confirmVisible = ref(false), blockedVisible = ref(false), blockers = ref<{ id: string; name: string }[]>([]);
const confirmMessage = computed(() => confirmAction.value?.kind === 'rotate' ? `轮换 ${confirmAction.value.tunnel.name} 的凭据？新凭据立即下发，切换期间隧道可能短暂中断。` : `删除 ${confirmAction.value?.kind === 'rule' ? confirmAction.value.rule.name : confirmAction.value?.tunnel.name || ''}？将立即从服务器移除运行配置并释放端口。`);
function ask(action: Action) { confirmAction.value = action; confirmVisible.value = true; }
// Every change is applied at once; a failed apply keeps the saved draft, shown with a 重试 action.
const applyNote = ref<{ tone: 'info' | 'error' | 'warn'; text: string } | null>(null);
async function apply(selection: PublishSelection, done: string) {
  if (mutating.value) return;
  mutating.value = true; applyNote.value = { tone: 'info', text: '正在应用…' };
  try {
    const result = await applyTunnelChanges(selection, text => { applyNote.value = { tone: 'info', text }; });
    const problem = applyProblem(result);
    if (result.status === 'publishing') applyNote.value = { tone: 'warn', text: problem };
    else if (problem) applyNote.value = { tone: 'error', text: `应用失败，已恢复原配置：${problem}。可修改后保存，或点「重试」。` };
    else {
      applyNote.value = result.gostRemovable?.length ? { tone: 'info', text: `以下服务器的 GOST 已不再使用，可在「版本与更新 → 代理核心」中移除：${result.gostRemovable.map(s => s.name).join('、')}` } : null;
      showToast(done);
    }
  } catch (e) { applyNote.value = { tone: 'error', text: `应用失败：${tunnelError(e)}。修改已保存，可点「重试」。` }; }
  finally { mutating.value = false; await load(); }
}
async function load() {
  loading.value = true; error.value = '';
  try { const [ts, rs, choices] = await Promise.all([tunnelAPI.list(), tunnelRuleAPI.list(), loadTunnelChoices(isOwner.value)]); tunnels.value = ts.items; rules.value = rs.items; nodes.value = choices.nodes; inbounds.value = choices.inbounds; }
  catch (e) { error.value = tunnelError(e); } finally { loading.value = false; }
}
async function performAction() {
  const action = confirmAction.value;
  if (!action || mutating.value) return;
  mutating.value = true;
  // A never-published draft is removed outright; anything else needs the servers updated.
  let next: [PublishSelection, string] | null = null;
  try {
    if (action.kind === 'rule') {
      await tunnelRuleAPI.remove(action.rule.id);
      if (action.rule.status !== 'draft') next = [{ tunnelIds: [], ruleIds: [action.rule.id] }, '规则已删除'];
    } else if (action.kind === 'rotate') {
      await tunnelAPI.rotate(action.tunnel.id, action.tunnel.updatedAt);
      next = [{ tunnelIds: [action.tunnel.id], ruleIds: [] }, '凭据已轮换并生效'];
    } else {
      await tunnelAPI.remove(action.tunnel.id);
      if (action.tunnel.status !== 'draft') next = [{ tunnelIds: [action.tunnel.id], ruleIds: [] }, '隧道已删除'];
    }
    if (!next) { showToast('已删除'); await load(); }
  } catch (e) {
    if (e instanceof APIError && e.code === 'tunnel_in_use') { blockers.value = (e.details.rules || []) as { id: string; name: string }[]; blockedVisible.value = true; }
    else showToast(tunnelError(e), 'error');
  } finally { mutating.value = false; }
  if (next) await apply(...next);
}
watch(rulesTab, () => { editor.value = false; });
onMounted(load);
</script>
