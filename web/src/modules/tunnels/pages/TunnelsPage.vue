<template>
  <div class="space-y-6">
    <PageHeader title="隧道管理" parent="节点配置" description="保存隧道与规则草稿，检查计划后统一发布。"><template #actions>
      <button class="ios-btn-secondary" :disabled="loading" @click="load">刷新</button>
      <button v-if="isOwner" class="ios-btn-primary" :disabled="loading || !!error" @click="editingTunnel = null; editingRule = null; editor = true">{{ rulesTab ? '创建规则' : '创建隧道' }}</button>
      <PublishFlow v-if="isOwner" :tunnels="tunnels" :rules="rules" :disabled="loading || !!error || mutating" @published="load" />
    </template></PageHeader>
    <nav class="ios-segmented-container inline-flex" aria-label="隧道管理页签"><RouterLink to="/proxy/tunnels" class="ios-segmented-item" :class="{ active: !rulesTab }">隧道</RouterLink><RouterLink to="/proxy/tunnel-rules" class="ios-segmented-item" :class="{ active: rulesTab }">规则</RouterLink></nav>
    <p v-if="mockEnabled()" class="text-xs text-ios-orange">开发演示数据 · 刷新页面后重置</p>
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
          <td v-if="isOwner"><div class="flex gap-2"><template v-if="t.status !== 'pending_delete'"><button class="ios-action-btn-primary" :disabled="mutating" @click="editingTunnel = t; editor = true">编辑</button><button class="ios-action-btn-secondary" :disabled="mutating" @click="ask({ kind: 'rotate', tunnel: t })">轮换凭据</button><button class="ios-action-btn-danger" :disabled="mutating" @click="ask({ kind: 'tunnel', tunnel: t })">删除</button></template></div></td>
        </tr></template>
      </DataTable>
    </div>
    <div v-else class="space-y-5">
      <EmptyState v-if="!groups.length" title="暂无规则" description="创建转发规则，或调整筛选条件。" />
      <section v-for="group in groups" :key="group.id || 'direct'" class="space-y-3">
        <h3 class="text-ios-label text-lg font-semibold leading-snug">{{ group.name }} <span class="text-sm font-normal text-ios-secondary">{{ group.route }}</span></h3>
        <DataTable><template #header><th>规则 / 转发路径</th><th>所属节点</th><th>对用户可见</th><th>启用</th><th>状态</th><th v-if="isOwner">操作</th></template>
          <template #body><tr v-for="r in group.rules" :key="r.id">
            <td class="p-4"><p class="text-base font-semibold text-ios-label">{{ r.name }}</p><p class="text-sm text-ios-secondary mt-0.5"><span class="font-mono tabular-nums">{{ r.entryNodeName }}:{{ r.listenPort }}</span> → {{ r.tunnelName ? `经 ${r.tunnelName}` : '直接转发' }} → <span v-if="r.target.mode === 'linked'">{{ r.target.nodeName }} · {{ r.target.inboundName }} · {{ r.target.protocol.toUpperCase() }} · </span><span class="font-mono tabular-nums">{{ endpoint(r.target.host, r.target.port) }}</span></p><p class="text-xs text-ios-tertiary">{{ r.network.toUpperCase() }}</p></td>
            <td>{{ nodes.find(n => n.id === r.ownerNodeId)?.name || r.ownerNodeId || '—' }}</td><td>{{ r.subscriptionVisible ? '可见' : '隐藏' }}</td><td><StatusBadge :status="r.enabled ? 'active' : 'disabled'" /></td><td><PublishBadge :status="r.status" /></td>
            <td v-if="isOwner"><div class="flex gap-2"><template v-if="r.status !== 'pending_delete'"><button class="ios-action-btn-primary" :disabled="mutating" @click="editingRule = r; editor = true">编辑</button><button class="ios-action-btn-danger" :disabled="mutating" @click="ask({ kind: 'rule', rule: r })">删除</button></template></div></td>
          </tr></template>
        </DataTable>
      </section>
    </div>
    <TunnelEditor v-if="editor && !rulesTab" :tunnel="editingTunnel" :nodes="nodes" :linked-rules="rules.filter(r => r.tunnelId === editingTunnel?.id)" @close="editor = false" @saved="editor = false; load()" />
    <RuleEditor v-if="editor && rulesTab" :rule="editingRule" :tunnels="tunnels" :nodes="nodes" :inbounds="inbounds" @close="editor = false" @saved="editor = false; load()" />
    <ConfirmDialog v-model="confirmVisible" :title="confirmAction?.kind === 'rotate' ? '轮换隧道凭据' : '删除草稿'" :message="confirmMessage" @confirm="performAction" />
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
import PublishFlow from '../components/PublishFlow.vue';
import PublishBadge from '../components/PublishBadge.vue';
import { loadTunnelChoices, mockEnabled, tunnelAPI, tunnelRuleAPI, tunnelError, type Tunnel, type TunnelRule } from '../api';
import { endpoint, groupRules, routeLabel } from '../model';
const route = useRoute(), rulesTab = computed(() => route.path === '/proxy/tunnel-rules');
const tunnels = ref<Tunnel[]>([]), rules = ref<TunnelRule[]>([]);
const nodes = ref<Awaited<ReturnType<typeof loadTunnelChoices>>['nodes']>([]), inbounds = ref<Awaited<ReturnType<typeof loadTunnelChoices>>['inbounds']>([]);
const loading = ref(true), error = ref(''), mutating = ref(false), editor = ref(false), editingTunnel = ref<Tunnel | null>(null), editingRule = ref<TunnelRule | null>(null);
const tunnelFilter = ref(''), nodeFilter = ref('');
const groups = computed(() => groupRules(rules.value.filter(r => (!nodeFilter.value || r.entryNodeId === nodeFilter.value) && (!tunnelFilter.value || (tunnelFilter.value === 'direct' ? !r.tunnelId : r.tunnelId === tunnelFilter.value))), tunnels.value));
type Action = { kind: 'rotate' | 'tunnel'; tunnel: Tunnel } | { kind: 'rule'; rule: TunnelRule };
const confirmAction = ref<Action | null>(null), confirmVisible = ref(false), blockedVisible = ref(false), blockers = ref<{ id: string; name: string }[]>([]);
const confirmMessage = computed(() => confirmAction.value?.kind === 'rotate' ? `轮换 ${confirmAction.value.tunnel.name} 的凭据？下一次发布时生效，部署期间隧道可能短暂中断。` : `删除 ${confirmAction.value?.kind === 'rule' ? confirmAction.value.rule.name : confirmAction.value?.tunnel.name || ''}？运行配置的移除与端口释放需发布后生效。`);
function ask(action: Action) { confirmAction.value = action; confirmVisible.value = true; }
async function load() {
  loading.value = true; error.value = '';
  try { const [ts, rs, choices] = await Promise.all([tunnelAPI.list(), tunnelRuleAPI.list(), loadTunnelChoices(isOwner.value)]); tunnels.value = ts.items; rules.value = rs.items; nodes.value = choices.nodes; inbounds.value = choices.inbounds; }
  catch (e) { error.value = tunnelError(e); } finally { loading.value = false; }
}
async function performAction() {
  const action = confirmAction.value;
  if (!action || mutating.value) return;
  mutating.value = true;
  try {
    if (action.kind === 'rule') await tunnelRuleAPI.remove(action.rule.id);
    else if (action.kind === 'rotate') await tunnelAPI.rotate(action.tunnel.id, action.tunnel.updatedAt);
    else await tunnelAPI.remove(action.tunnel.id);
    showToast(action.kind === 'rotate' ? '凭据已轮换，请发布配置' : '删除已保存；已发布对象需发布后清理运行配置'); await load();
  } catch (e) {
    if (e instanceof APIError && e.code === 'tunnel_in_use') { blockers.value = (e.details.rules || []) as { id: string; name: string }[]; blockedVisible.value = true; }
    else showToast(tunnelError(e), 'error');
  } finally { mutating.value = false; }
}
watch(rulesTab, () => { editor.value = false; });
onMounted(load);
</script>
