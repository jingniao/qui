<template>
  <div class="space-y-6">
    <PageHeader title="配置版本" parent="系统" description="创建不可变配置版本，确认发布后由 Agent 校验与应用。"><template #actions><button @click="openCreate" class="ios-btn-primary">创建配置版本</button><button @click="openBatchCreate" class="ios-btn-secondary">创建代理链批次</button><button @click="openRelayBatchCreate" class="ios-btn-secondary">创建关联节点批次</button><button @click="load" class="ios-btn-secondary">刷新</button></template></PageHeader>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !revisions.length" />
    <EmptyState v-else-if="!revisions.length" title="尚未创建配置版本" description="选择已注册的节点，创建首个入站与默认出站。" />
    <DataTable v-else>
      <template #header><th>版本</th><th>说明</th><th>创建人</th><th>状态</th><th>结果</th><th>创建时间</th><th>操作</th></template>
      <template #body><tr v-for="revision in revisions" :key="revision.id"><td class="p-4 font-mono">#{{ revision.id }}</td><td>{{ revision.summary }}</td><td>{{ revision.author }}</td><td><StatusBadge :status="revision.status" /></td><td class="text-xs">{{ resultLabel(revision.resultCode) }}</td><td class="text-xs">{{ formatTime(revision.createdAt) }}</td><td><button @click="openDiff(revision)" class="ios-action-btn-primary">查看差异</button><a v-if="revision.batchId" href="#deployment-batches" class="ios-action-btn-primary">由代理链批次管理</a><button v-else-if="revision.status === 'draft'" @click="confirmPublish(revision)" class="ios-action-btn-primary">发布</button><button v-else-if="revision.status === 'success'" @click="confirmNodeRollback(revision)" class="ios-action-btn-primary">恢复此版本</button><span v-else class="text-xs text-gray-500">{{ revision.status === 'pending' ? '等待节点执行' : '已处理' }}</span></td></tr></template>
      <template #footer><Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" /></template>
    </DataTable>
    <section id="deployment-batches" class="space-y-4">
      <h2 class="text-lg font-semibold">节点发布批次</h2>
      <p class="text-sm text-gray-500">按出口 → 中转 → 入口发布；失败或手动回滚时按相反顺序恢复发布前版本。正在执行的批次会预留全部节点。</p>
      <EmptyState v-if="!batches.length" title="尚无发布批次" description="选择代理链或发生变更的节点，生成包含依赖节点配置的批次草稿。" />
      <DataTable v-else>
        <template #header><th>批次</th><th>发布顺序与结果</th><th>状态</th><th>创建时间</th><th>操作</th></template>
        <template #body><tr v-for="batch in batches" :key="batch.id">
          <td class="p-4"><span class="font-semibold">{{ batch.name }}</span><p class="text-xs text-gray-500">{{ batch.chainId ? '代理链批次' : '关联节点批次' }} · {{ batch.author }}</p></td>
          <td class="py-3"><ol class="space-y-1 text-xs"><li v-for="item in batch.items" :key="item.nodeId">{{ item.position + 1 }}. {{ nodeName(item.nodeId) }} · #{{ item.revision }} · {{ batchStatus(item.status) }}<span class="text-gray-500"> · {{ batch.status === 'draft' ? '回滚目标在发布时记录' : '回滚到 ' + (item.previousRevision ? '#' + item.previousRevision : '空配置') }}</span></li></ol></td>
          <td class="text-sm">{{ batchStatus(batch.status) }}</td><td class="text-xs">{{ formatTime(batch.createdAt) }}</td>
          <td><button v-if="batch.status === 'draft'" @click="confirmBatch(batch, false)" class="ios-action-btn-primary">发布批次</button><button v-else-if="batch.status === 'success' || batch.status === 'rollback_failed'" @click="confirmBatch(batch, true)" class="ios-action-btn-primary">{{ batch.status === 'rollback_failed' ? '重试回滚' : '回滚批次' }}</button></td>
        </tr></template>
        <template #footer><Pagination :total="batchTotal" :page-size="20" :current-page="batchPage" @update:page="changeBatchPage" /></template>
      </DataTable>
    </section>
    <AppDialog v-model="diffVisible" title="配置版本差异" description="默认与中央确认的节点运行版本比较，也可指定同一节点的历史版本。" max-width="3xl" @close="clearDiff">
      <form class="flex flex-wrap items-end gap-3" @submit.prevent="loadDiff"><label class="text-sm flex-1">比较版本编号<input v-model="diffBase" type="number" min="0" step="1" placeholder="留空使用节点运行版本；0 表示空配置" class="form-input-base mt-2" /></label><button :disabled="diffLoading" class="ios-btn-primary disabled:opacity-40">比较</button></form>
      <ErrorState v-if="diffError" :message="diffError" @retry="loadDiff" /><LoadingSkeleton v-else-if="diffLoading" /><ConfigDiffViewer v-else-if="diffResult" :diff="diffResult" />
    </AppDialog>
    <FormDrawer v-model="batchDrawer" :title="batchForm.mode === 'relay' ? '创建关联节点发布批次' : '创建代理链发布批次'" description="保存各节点的不可变草稿，确认发布后按顺序执行。" :loading="saving" @confirm="createBatch" confirm-text="保存批次草稿">
      <div class="space-y-4"><label v-if="batchForm.mode === 'chain'" class="block text-sm">代理链<select v-model="batchForm.chainId" class="form-input-base mt-2"><option value="">请选择</option><option v-for="chain in chains" :key="chain.id" :value="chain.id">{{ chain.name }} · {{ chain.nodeIds.length }} 节点</option></select></label><label v-else class="block text-sm">发生变更的节点<select v-model="batchForm.nodeId" class="form-input-base mt-2"><option value="">请选择</option><option v-for="node in relayBatchNodes" :key="node.id" :value="node.id">{{ node.name }}</option></select></label><p v-if="batchForm.mode === 'relay'" class="text-xs text-gray-500">包含该节点的所有已启用上游调用方、下游目标及相互关联的中转节点。生成后请查看批次中的完整节点顺序和各版本差异，再确认发布。</p><label class="block text-sm">变更说明<input v-model="batchForm.summary" maxlength="512" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">每个节点的完整代理草稿都会进入此批次；发布会替换节点当前配置。回滚目标在正式发布时记录。</p></div>
    </FormDrawer>
    <FormDrawer v-model="drawer" title="创建配置版本" description="保存为草稿，发布前可在列表中确认。" :loading="saving" @confirm="create" confirm-text="保存配置草稿">
      <div class="space-y-4">
        <label class="block text-sm">目标节点<select v-model="form.nodeId" class="form-input-base mt-2"><option value="">请选择</option><option v-for="node in nodes" :key="node.id" :value="node.id">{{ node.name }} · {{ node.status }}</option></select></label>
        <label class="block text-sm">变更说明<input v-model="form.summary" maxlength="120" class="form-input-base mt-2" /></label>
        <label class="block text-sm">入站协议<select v-model="form.protocol" class="form-input-base mt-2"><option value="socks5">SOCKS5</option><option value="http">HTTP CONNECT</option></select></label>
        <label class="block text-sm">监听地址<select v-model="form.listen" class="form-input-base mt-2"><option value="127.0.0.1">127.0.0.1（本机接入）</option><option value="0.0.0.0">0.0.0.0（外部接入）</option></select></label>
        <label class="block text-sm">监听端口<input v-model.number="form.port" type="number" min="1" max="65535" class="form-input-base mt-2" /></label>
        <p class="text-xs text-gray-500">接入会保存为该节点的普通入站，并绑定已授权用户的凭据。用户接入会按节点组展开授权、过滤禁用及过期用户，发布时保留该节点的其他入站、出站与分流配置。</p>
      </div>
    </FormDrawer>
    <ConfirmDialog v-model="confirmVisible" title="发布配置版本" :message="`将版本 #${target?.id || ''} 发布到目标节点。此版本会替换节点当前配置，成功后生效，启动或健康检查失败时恢复上一成功版本。`" @confirm="publish" confirm-text="确认发布" />
    <ConfirmDialog v-model="batchConfirm" :title="batchRollback ? '回滚节点批次' : '发布节点批次'" :message="batchRollback ? '按发布顺序的反向恢复发布前版本；首次发布的节点会停止代理核心。已有后续配置变更时，系统会拒绝覆盖。' : '按列表中的目标优先顺序替换各节点配置。执行期间相关节点不能进行其他发布；失败时自动反向回滚。'" @confirm="executeBatch" :confirm-text="batchRollback ? '确认回滚' : '确认发布'" />
    <ConfirmDialog v-model="nodeRollbackConfirm" title="恢复节点配置版本" :message="`将节点恢复到成功版本 #${rollbackTarget?.id || ''}，替换其当前配置。节点必须仍保留此版本，且中转依赖仍需可用。`" @confirm="executeNodeRollback" confirm-text="确认恢复" />
  </div>
</template>
<script setup lang="ts">
import { ref, reactive, onMounted, onUnmounted } from 'vue';
import { request, type Page } from '../../../app/apiClient';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { deploymentAPI, type Revision, type DeploymentBatch, type ChainChoice, type RevisionDiff } from '../deploymentApi';
import AppDialog from '../../../components/AppDialog.vue';
import ConfigDiffViewer from '../../../components/ConfigDiffViewer.vue';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import Pagination from '../../../components/Pagination.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const revisions = ref<Revision[]>([]); const page = ref(1); const total = ref(0); const loading = ref(false); const error = ref('');
const diffVisible = ref(false); const diffLoading = ref(false); const diffError = ref(''); const diffBase = ref<string | number>(''); const diffTarget = ref<Revision | null>(null); const diffResult = ref<RevisionDiff | null>(null); let diffRequest = 0;
const drawer = ref(false); const saving = ref(false); const nodes = ref<NodeView[]>([]); const target = ref<Revision | null>(null); const confirmVisible = ref(false);
const batches = ref<DeploymentBatch[]>([]); const batchPage = ref(1); const batchTotal = ref(0);
const batchDrawer = ref(false); const chains = ref<ChainChoice[]>([]); const relayBatchNodes = ref<NodeView[]>([]); const batchForm = reactive({ mode: 'chain', chainId: '', nodeId: '', summary: '' });
const batchConfirm = ref(false); const batchRollback = ref(false); const batchTarget = ref<DeploymentBatch | null>(null);
const nodeRollbackConfirm = ref(false); const rollbackTarget = ref<Revision | null>(null); const executing = ref(false);
const form = reactive({ nodeId: '', summary: '', protocol: 'socks5', listen: '127.0.0.1', port: 1080 });
function resultLabel(code: string) { return ({success:'已应用且健康',validation_failed:'核心校验失败',compile_failed:'配置编译失败',apply_failed:'应用失败',start_failed:'启动失败',rollback_failed:'回滚失败',stop_failed:'停止失败',persistence_failed:'保存成功清单失败，已恢复旧配置'} as Record<string,string>)[code] || code || '—'; }
async function load() { loading.value = true; try { const [result, batchResult, nodeResult] = await Promise.all([request<Page<Revision>>(`/revisions?page=${page.value}&pageSize=20`), deploymentAPI.batches(batchPage.value), nodeAPI.list(1, '')]); revisions.value = result.items; total.value = result.total; batches.value = batchResult.items; batchTotal.value = batchResult.total; nodes.value = nodeResult.items; error.value = ''; } catch (e) { error.value = (e as Error).message; } finally { loading.value = false; } }
function nodeName(id: string) { return nodes.value.find(node => node.id === id)?.name || id.slice(0, 12); }
function batchStatus(status: string) { return ({ draft: '草稿', waiting: '等待前序节点', pending: '等待节点确认', deploying: '发布中', success: '成功', failed: '失败', rolling_back: '回滚中', rollback_pending: '等待回滚确认', rolled_back: '已回滚', rollback_failed: '回滚失败' } as Record<string, string>)[status] || status; }
function changeBatchPage(value: number) { batchPage.value = value; load(); }
async function openBatchCreate() { try { const result = await deploymentAPI.chains(); chains.value = result.items.filter(chain => chain.enabled); if (result.total > 100) { showToast('代理链数量超过选择上限，请先筛选或整理代理链', 'error'); return; } Object.assign(batchForm, { mode: 'chain', chainId: '', nodeId: '', summary: '' }); batchDrawer.value = true; } catch (e) { showToast((e as Error).message, 'error'); } }
async function openRelayBatchCreate() { try { const result = await request<Page<NodeView>>('/nodes?page=1&pageSize=100'); if (result.total > 100) throw new Error('节点数量超过选择上限，请整理节点后重试'); relayBatchNodes.value = result.items.filter(node => node.enabled && node.status !== 'pending'); Object.assign(batchForm, { mode: 'relay', chainId: '', nodeId: '', summary: '' }); batchDrawer.value = true; } catch (e) { showToast((e as Error).message, 'error'); } }
async function createBatch() { if (saving.value) return; const relay = batchForm.mode === 'relay'; if (!(relay ? batchForm.nodeId : batchForm.chainId) || !batchForm.summary.trim()) { showToast(relay ? '请选择发生变更的节点并填写说明' : '请选择代理链并填写变更说明', 'error'); return; } saving.value = true; try { if (relay) await deploymentAPI.createRelayBatch(batchForm.nodeId, batchForm.summary); else await deploymentAPI.createBatch(batchForm.chainId, batchForm.summary); batchDrawer.value = false; batchPage.value = 1; page.value = 1; showToast('批次草稿已创建，请查看完整顺序和差异后确认发布'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; } }
function confirmBatch(batch: DeploymentBatch, rollback: boolean) { batchTarget.value = batch; batchRollback.value = rollback; batchConfirm.value = true; }
async function executeBatch() { if (!batchTarget.value || executing.value) return; executing.value = true; try { if (batchRollback.value) await deploymentAPI.rollbackBatch(batchTarget.value.id); else await deploymentAPI.deployBatch(batchTarget.value.id); showToast('批次操作已排队，等待节点确认', 'info'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { executing.value = false; } }
function confirmNodeRollback(revision: Revision) { rollbackTarget.value = revision; nodeRollbackConfirm.value = true; }
async function executeNodeRollback() { if (!rollbackTarget.value || executing.value) return; executing.value = true; try { await deploymentAPI.rollbackNode(rollbackTarget.value.nodeId, rollbackTarget.value.id); showToast('回滚已排队，等待节点确认', 'info'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { executing.value = false; } }
function changePage(value: number) { page.value = value; load(); }
function clearDiff() { diffRequest++; diffResult.value = null; diffError.value = ''; diffLoading.value = false; diffTarget.value = null; }
function openDiff(revision: Revision) { clearDiff(); diffTarget.value = revision; diffBase.value = ''; diffVisible.value = true; loadDiff(); }
async function loadDiff() {
  if (!diffTarget.value) return;
  const serial = ++diffRequest; const revision = diffTarget.value.id;
  const base = diffBase.value === '' ? undefined : Number(diffBase.value);
  diffResult.value = null; diffError.value = '';
  if (base !== undefined && (!Number.isSafeInteger(base) || base < 0)) { diffError.value = '请输入有效的非负版本编号'; diffLoading.value = false; return; }
  diffLoading.value = true;
  try { const result = await deploymentAPI.diff(revision, base); if (serial === diffRequest && diffVisible.value) diffResult.value = result; }
  catch (e) { if (serial === diffRequest && diffVisible.value) diffError.value = (e as Error).message; }
  finally { if (serial === diffRequest) diffLoading.value = false; }
}
async function openCreate() { try { const result = await nodeAPI.list(1, ''); nodes.value = result.items.filter(node => node.enabled && node.status !== 'pending'); Object.assign(form, { nodeId: '', summary: '', protocol: 'socks5', listen: '0.0.0.0', port: 1080 }); drawer.value = true; } catch (e) { showToast((e as Error).message, 'error'); } }
async function create() {
  if (saving.value) return;
  if (!form.nodeId || !form.summary) { showToast('请选择节点并填写变更说明', 'error'); return; }
  saving.value = true;
  try { await request('/revisions/user-access', 'POST', { nodeId: form.nodeId, summary: form.summary, protocol: form.protocol, listen: form.listen, port: form.port }); drawer.value = false; showToast('用户接入草稿已创建，请确认发布'); await load(); }
  catch (e) { showToast((e as Error).message, 'error'); } finally { saving.value = false; }
}
function confirmPublish(revision: Revision) { target.value = revision; confirmVisible.value = true; }
async function publish() { if (!target.value) return; try { await request(`/revisions/${target.value.id}/deploy`, 'POST'); showToast('发布已排队，节点确认后会更新结果', 'info'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } }
onMounted(() => { load(); window.addEventListener('proxy-node-status', load); window.addEventListener('proxy-deployment-status', load); });
onUnmounted(() => { clearDiff(); window.removeEventListener('proxy-node-status', load); window.removeEventListener('proxy-deployment-status', load); });
</script>



