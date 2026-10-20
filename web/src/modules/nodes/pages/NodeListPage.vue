<template>
  <div class="space-y-6">
    <PageHeader title="节点" parent="节点" description="Agent 使用独立证书主动连接中央端，状态来自真实心跳。">
      <template #actions><button v-if="isOwner" @click="createDrawer = true" class="ios-btn-primary">注册节点</button><button @click="load" class="ios-btn-secondary">刷新</button></template>
    </PageHeader>
    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索节点名称或 IP…"
          class="ios-search-input"
        />
        <button
          v-if="keyword"
          type="button"
          @click="keyword = ''; search()"
          class="absolute right-2.5 top-1/2 -translate-y-1/2 text-ios-tertiary hover:text-ios-label p-0.5 rounded-full hover:bg-black/5 transition-colors"
          title="清空"
        >
          <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
            <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
          </svg>
        </button>
      </div>
      <button class="ios-search-btn shrink-0">查询</button>
      <label v-if="isOwner" class="inline-flex items-center gap-2 text-sm font-medium text-ios-secondary shrink-0"><input v-model="showDeleted" type="checkbox" @change="search" /> 显示已删除</label>
    </form>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <EmptyState v-else-if="!nodes.length" :title="showDeleted ? '没有已删除的节点' : '还没有节点'" :description="showDeleted ? '删除后的节点会保留在这里，历史流量与访问记录不受影响。' : '创建注册码，然后在节点本机运行 Agent 完成注册。'" />
    <DataTable v-else>
      <template #header><th>节点</th><th>状态</th><th>系统</th><th>Agent / 核心</th><th>最后心跳</th><th>操作</th></template>
      <template #body><tr v-for="node in nodes" :key="node.id">
        <td class="p-4"><router-link :to="`/nodes/${node.id}`" class="text-ios-blue font-semibold hover:underline text-[15px]">{{ node.name }}</router-link><span v-if="node.deletedAt" class="ml-2 rounded-full bg-black/[0.05] px-2 py-0.5 text-[11px] font-semibold text-ios-secondary align-middle">已删除</span><p class="text-[13px] text-ios-secondary font-normal mt-0.5">{{ node.publicIp || node.hostname || '等待 Agent 注册' }}</p></td>
        <td><StatusBadge kind="node" :status="node.status" /></td><td class="text-xs text-ios-secondary">{{ node.os || '—' }}</td>
        <td><span class="text-xs font-mono tabular-nums text-ios-label font-medium">{{ node.agentVersion || '—' }}</span><p class="text-[13px] text-ios-secondary font-normal mt-0.5">{{ node.capabilities?.core_name || '尚未检测核心' }} {{ node.capabilities?.core_version }}</p></td>
        <td class="text-xs font-mono tabular-nums text-ios-tertiary">{{ formatTime(node.lastHeartbeat) }}</td>
        <td class="space-x-2"><router-link :to="`/nodes/${node.id}`" class="ios-action-btn-primary">详情</router-link><template v-if="isOwner && !node.deletedAt"><button @click="promptToggle(node)" class="ios-action-btn-secondary">{{ node.enabled ? '禁用' : '启用' }}</button><button @click="promptDelete(node)" class="ios-action-btn-danger">{{ node.status === 'pending' ? '取消登记' : '删除' }}</button></template></td>
      </tr></template>
      <template #footer><Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" /></template>
    </DataTable>
    <FormDrawer v-model="createDrawer" title="创建节点注册码" description="注册码一小时内有效，成功注册后立即失效。" :loading="creating" @confirm="create">
      <label class="block text-xs font-medium text-ios-secondary">节点名称<input v-model="name" maxlength="40" class="form-input-base mt-1.5" /></label>
    </FormDrawer>
    <AppDialog v-model="tokenDialog" title="一次性节点注册码" description="明文注册码仅在本次创建时展示，请复制并妥善保管。" :show-footer="false">
      <div class="space-y-4"><CodePreview :code="enrollment?.enrollmentToken || ''" /><p class="text-xs text-ios-tertiary">有效期至 {{ formatTime(enrollment?.expiresAt || '') }}</p>
        <div v-if="installAvailable" class="space-y-3 border-t border-black/[0.08] pt-4">
          <p class="text-xs font-medium text-ios-secondary">一键安装命令</p>
          <div class="grid gap-3 sm:grid-cols-3">
            <label class="block text-xs font-medium text-ios-secondary">面板地址<input v-model.trim="installHost" maxlength="253" class="form-input-base mt-1.5" /></label>
            <label class="block text-xs font-medium text-ios-secondary">HTTPS 端口<input v-model.number="installHTTPPort" type="number" min="1" max="65535" class="form-input-base mt-1.5" /></label>
            <label class="block text-xs font-medium text-ios-secondary">核心<select v-model="installCore" class="form-input-base mt-1.5"><option value="xray">xray</option><option value="sing-box">sing-box</option></select></label>
          </div>
          <CodePreview v-if="installCommandText" :code="installCommandText" title="一键安装命令" />
          <p v-else class="text-xs text-ios-red">面板地址或 HTTPS 端口无效，无法生成命令（地址只能是域名或 IPv4）。</p>
          <p class="text-xs text-ios-tertiary">在新服务器上以 root 运行；命令含一次性注册码，{{ formatTime(enrollment?.expiresAt || '') }} 前有效。</p>
        </div>
        <p v-else class="text-sm text-ios-secondary">当前面板不是正式发布版本或处于开发模式，无法生成一键安装命令，请按部署文档手动安装。</p>
        <p class="text-sm text-ios-secondary">在节点本机配置中央地址与可信 CA，将注册码写入受保护的本地文件，然后运行注册命令。</p><CodePreview code="proxy-agent --config /etc/proxy-agent/config.yaml --enroll --token-file /etc/proxy-agent/enrollment-token" /><p class="text-xs text-ios-tertiary">私钥由 Agent 本机生成，注册请求只提交公钥证书请求。</p></div>
    </AppDialog>
    <ConfirmDialog v-model="confirmVisible" title="确认节点状态修改" :message="`确认${target?.enabled ? '禁用' : '启用'}节点 ${target?.name || ''}？禁用会拒绝其控制连接。`" @confirm="toggle" />
    <AppDialog v-model="deleteDialog" :title="deletePreview?.mode === 'cancel' ? '取消节点登记' : '删除节点'" :show-footer="false">
      <div class="space-y-4">
        <div v-if="deleteLoading" class="text-sm text-ios-secondary">正在检查节点引用…</div>
        <template v-else-if="deleteBlockers.length">
          <p class="text-sm text-ios-secondary">节点仍被以下配置引用或有进行中的发布，请先处理：</p>
          <ul class="space-y-2">
            <li v-for="blocker in deleteBlockers" :key="blocker.kind + blocker.id" class="flex items-center gap-2 text-sm">
              <span class="shrink-0 rounded-full bg-black/[0.05] px-2 py-0.5 text-[11px] font-semibold text-ios-secondary">{{ blockerLabel(blocker.kind) }}</span>
              <span class="text-ios-label font-medium break-all">{{ blocker.name }}</span>
            </li>
          </ul>
        </template>
        <p v-else-if="deletePreview?.mode === 'cancel'" class="text-sm text-ios-secondary leading-relaxed">确认取消节点 {{ deleteTarget?.name || '' }} 的登记？未使用的注册码将立即失效，该节点记录会被移除。</p>
        <p v-else class="text-sm text-ios-secondary leading-relaxed">删除后该节点的证书立即吊销、退出所有节点组、不再出现在订阅中，历史流量与访问记录保留。面板无法停止服务器上的代理程序，请另行停止或卸载 Agent。离线节点尚未上报的记录将丢失。若其他节点已发布的配置仍经由该节点中转，请重新发布这些节点。</p>
        <p v-if="deleteError" role="alert" class="text-sm text-ios-red">{{ deleteError }}</p>
        <div class="flex items-center justify-end gap-2.5 pt-2">
          <button type="button" class="ios-dialog-btn-secondary" @click="deleteDialog = false">关闭</button>
          <button v-if="!deleteLoading && !deleteBlockers.length && deletePreview" type="button" :disabled="deleting" class="ios-dialog-btn-danger" @click="confirmDelete">{{ deletePreview.mode === 'cancel' ? '取消登记' : '删除节点' }}</button>
        </div>
      </div>
    </AppDialog>
  </div>
</template>
<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { nodeAPI, installCommand, type NodeView, type Enrollment, type DeletionBlocker, type DeletionPreview } from '../api';
import { APIError } from '../../../app/apiClient';
import { isOwner, formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import Pagination from '../../../components/Pagination.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import AppDialog from '../../../components/AppDialog.vue';
import CodePreview from '../../../components/CodePreview.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const nodes = ref<NodeView[]>([]); const total = ref(0); const page = ref(1); const keyword = ref('');
// Owner only: the same list, restricted to logically deleted nodes.
const showDeleted = ref(false);
const loading = ref(false); const error = ref(''); const createDrawer = ref(false); const name = ref(''); const creating = ref(false);
const enrollment = ref<Enrollment | null>(null); const tokenDialog = ref(false); const target = ref<NodeView | null>(null); const confirmVisible = ref(false);
// Inputs of the one-command install block; the defaults describe this panel as
// the browser reached it.
const installHost = ref(''); const installHTTPPort = ref(443); const installCore = ref('xray');
const installAvailable = computed(() => Boolean(enrollment.value?.install?.version && enrollment.value?.install?.caSha256));
// The host charset the installer script accepts: a DNS name or an IPv4 address,
// never a port or a URL.
const installInputValid = computed(() => /^[A-Za-z0-9.-]+$/.test(installHost.value) && Number.isInteger(installHTTPPort.value) && installHTTPPort.value >= 1 && installHTTPPort.value <= 65535);
const installCommandText = computed(() => {
  const install = enrollment.value?.install;
  if (!install || !installAvailable.value || !installInputValid.value) return '';
  return installCommand({ version: install.version, host: installHost.value, httpPort: installHTTPPort.value, grpcPort: install.grpcPort, caSha256: install.caSha256, token: enrollment.value?.enrollmentToken || '', core: installCore.value });
});
let generation = 0;
async function load() {
  const current = ++generation; loading.value = true; error.value = '';
  try { const result = await nodeAPI.list(page.value, keyword.value, showDeleted.value); if (current === generation) { nodes.value = result.items; total.value = result.total; } }
  catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
function search() { page.value = 1; load(); }
function changePage(value: number) { page.value = value; load(); }
async function create() {
  if (creating.value) return;
  creating.value = true;
  try { enrollment.value = await nodeAPI.enroll(name.value); installHost.value = window.location.hostname; installHTTPPort.value = Number(window.location.port) || 443; installCore.value = 'xray'; createDrawer.value = false; tokenDialog.value = true; name.value = ''; await load(); }
  catch (e) { showToast((e as Error).message, 'error'); } finally { creating.value = false; }
}
function promptToggle(node: NodeView) { target.value = node; confirmVisible.value = true; }
async function toggle() { if (!target.value) return; try { await nodeAPI.update(target.value, !target.value.enabled); await load(); } catch (e) { showToast((e as Error).message, 'error'); } }
// Deleting is one action for both cases: the preview decides whether the unused
// registration is cancelled or the enrolled node is logically deleted.
const deleteTarget = ref<NodeView | null>(null);
const deletePreview = ref<DeletionPreview | null>(null);
const deleteDialog = ref(false);
const deleteLoading = ref(false);
const deleteError = ref('');
const deleting = ref(false);
const deleteBlockers = computed<DeletionBlocker[]>(() => deletePreview.value?.blockers ?? []);
const blockerLabels: Record<string, string> = {
  inbound: '入站',
  outbound: '出站',
  outbound_group: '出口组',
  outbound_target: '其他节点的出站',
  proxy_chain: '代理链',
  policy: '分流规则',
  deployment_batch: '进行中的批量发布'
};
function blockerLabel(kind: string) { return blockerLabels[kind] || kind; }
async function promptDelete(node: NodeView) {
  deleteTarget.value = node; deletePreview.value = null; deleteError.value = ''; deleteDialog.value = true; deleteLoading.value = true;
  try { deletePreview.value = await nodeAPI.deletionPreview(node.id); }
  catch (e) { deleteError.value = (e as Error).message; }
  finally { deleteLoading.value = false; }
}
async function confirmDelete() {
  if (!deleteTarget.value || !deletePreview.value || deleting.value) return;
  const mode = deletePreview.value.mode;
  deleting.value = true; deleteError.value = '';
  try {
    await nodeAPI.deleteNode(deleteTarget.value.id);
    showToast(mode === 'cancel' ? '节点登记已取消' : '节点已删除');
    deleteDialog.value = false;
    await load();
    // The last row of a later page leaves the list empty; step back one page.
    if (!nodes.value.length && page.value > 1) changePage(page.value - 1);
  } catch (e) {
    deleteError.value = (e as Error).message;
    // A refusal carries the blockers that appeared since the preview.
    const blockers = (e as APIError).details?.blockers;
    if (Array.isArray(blockers) && deletePreview.value) deletePreview.value = { ...deletePreview.value, blockers: blockers as DeletionBlocker[] };
  } finally {
    deleting.value = false;
  }
}
watch(tokenDialog, visible => { if (!visible) enrollment.value = null; });
onMounted(() => { load(); window.addEventListener('proxy-node-status', load); });
onUnmounted(() => { window.removeEventListener('proxy-node-status', load); generation++; });
</script>
