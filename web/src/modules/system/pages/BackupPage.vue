<template>
  <div class="space-y-6">
    <PageHeader title="数据备份与恢复" parent="系统" description="创建可携带到其他机器的加密备份，并验证数据库、配置和密钥材料。"><template #actions><button class="ios-btn-primary" @click="openCreate">创建加密备份</button><button class="ios-btn-secondary" @click="openImport">导入加密备份</button><button class="ios-btn-secondary" @click="load">刷新</button></template></PageHeader>
    <section class="panel-card p-5 space-y-2 text-sm"><p>手动备份已接入真实服务，包含 SQLite 一致性快照、启动配置、中央证书与原始配置密钥，以及本地规则文件。</p><p class="text-gray-500">手动备份使用创建时设置的独立密码；自动备份使用系统设置中的密码，加密保存供后台执行。请单独妥善保管密码，下载的是加密文件。</p><p class="text-gray-500">自动备份与保留轮换可在系统设置中配置。完整恢复仍需核对节点账本与已消费额度，托管中央服务可在恢复预览后提交恢复；服务会短暂重启，旧会话失效，节点继续使用最后成功配置。</p></section>
    <RestoreRecordsPanel />
    <p v-if="isOwner" class="rounded-xl border border-amber-200 bg-amber-50 p-4 text-sm text-amber-900">较早备份中的CA可能已经过期。可在独立私有目录使用固定历史档案检查命令保存完整认证报告；历史检查通过不授予恢复、安装或启动许可，也不更新本页“上次内容验证”。需要有效的正式信任接续后才能执行恢复，请保留原材料。</p>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="loading && !items.length" /><EmptyState v-else-if="!items.length" title="暂无系统备份" description="创建并验证首份加密备份，下载后保存到独立存储。" />
    <DataTable v-else min-width="min-w-[1000px]"><template #header><th class="p-4">备份时间与编号</th><th>类型</th><th>文件大小</th><th>文件校验和</th><th>上次内容验证</th><th>创建人</th><th>操作</th></template><template #body><tr v-for="item in items" :key="item.id" class="border-t"><td class="p-4"><p>{{ formatTime(item.createdAt) }}</p><p class="font-mono text-xs text-gray-500 mt-1 break-all">{{ item.id }}</p></td><td>{{ item.origin === 'imported' ? '外部导入' : kindLabel(item.kind) }}</td><td><TrafficValue :bytes="item.fileBytes" /></td><td class="font-mono text-xs" :title="item.sha256">{{ item.sha256.slice(0, 12) }}…</td><td class="text-xs">{{ formatTime(item.verifiedAt) }}</td><td>{{ item.createdBy }}</td><td><a :href="backupAPI.downloadUrl(item.id)" :download="'proxy-control-' + item.id + '.pcb'" class="ios-action-btn-primary">下载</a><button @click="openVerify(item)" class="ios-action-btn-primary">验证内容</button><button @click="openRestorePreview(item)" class="ios-action-btn-primary">恢复预览</button><button @click="promptDelete(item)" class="ios-action-btn-danger">删除</button></td></tr></template><template #footer><Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" /></template></DataTable>
    <section class="space-y-4"><h2 class="text-lg font-semibold">自动备份执行记录</h2><ErrorState v-if="runError" :message="runError" @retry="loadRuns" /><EmptyState v-else-if="!runs.length" title="暂无自动备份记录" description="在系统设置中启用自动备份后，后台按实际执行结果记录。" /><DataTable v-else min-width="min-w-[900px]"><template #header><th class="p-4">类别与周期</th><th>状态</th><th>尝试次数</th><th>最近尝试</th><th>重试时间</th><th>备份登记</th></template><template #body><tr v-for="run in runs" :key="run.id" class="border-t"><td class="p-4">{{ kindLabel(run.kind) }} · {{ run.periodKey }}</td><td>{{ runStatus(run.status) }}<p v-if="run.errorCode" class="text-xs text-red-600">执行失败，后台将按重试计划继续。</p></td><td>{{ run.attempts }}</td><td class="text-xs">{{ formatTime(run.lastAttemptAt) }}</td><td class="text-xs">{{ run.retryAt ? formatTime(run.retryAt) : '—' }}</td><td class="text-xs">{{ run.backupId ? run.backupId.slice(0, 12) : run.status === 'success' ? '文件登记已删除，周期记录保留' : '尚未生成' }}</td></tr></template><template #footer><Pagination :total="runTotal" :page-size="20" :current-page="runPage" @update:page="changeRunPage" /></template></DataTable></section>
    <FormDrawer v-model="passwordDrawer" :title="mode === 'create' ? '创建加密备份' : '验证备份内容'" :description="mode === 'create' ? '设置独立备份密码。系统完成加密和完整性验证后才登记备份。' : '请输入这份备份创建时的密码。验证不会替换当前数据库或修改节点配置。'" :loading="saving" :confirm-text="mode === 'create' ? '创建并验证' : '验证内容'" @confirm="submitPassword" @close="clearPassword"><div class="space-y-4"><label class="block text-sm">备份密码<input v-model="password" type="password" :autocomplete="mode === 'create' ? 'new-password' : 'off'" maxlength="256" class="form-input-base mt-2" /></label><label v-if="mode === 'create'" class="block text-sm">再次输入密码<input v-model="passwordConfirm" type="password" autocomplete="new-password" maxlength="256" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">密码须包含 12～128 个字符。大备份处理可能需要一些时间，请等待实际结果。</p><p v-if="operationError" role="alert" class="text-sm text-red-600">{{ operationError }}</p></div></FormDrawer>
    <FormDrawer v-model="importDrawer" title="导入加密备份" description="上传已下载的 .pcb 文件并验证。导入只登记文件，不覆盖当前系统或发布节点配置。" :loading="importing" confirm-text="上传并验证" @confirm="importArchive" @close="clearImport"><div class="space-y-4"><label class="block text-sm">加密备份文件<input :key="fileInputKey" type="file" accept=".pcb" class="form-input-base mt-2" @change="selectImportFile" /></label><label class="block text-sm">原备份密码<input v-model="importPassword" type="password" autocomplete="off" maxlength="256" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">使用备份创建时的密码。文件和内部数据库校验通过后才登记；导入文件独立保留，不参加本机自动轮换。文件最大约 1 GiB，上传期间请保持连接。</p><p v-if="importError" role="alert" class="text-sm text-red-600">{{ importError }}</p></div></FormDrawer>
    <FormDrawer v-model="restoreDrawer" title="恢复前校验与影响预览" description="读取完整验证后的备份和当前数据库，列出恢复时需要处理的同步问题。" width="2xl" :loading="previewLoading || restoreSubmitting" confirm-text="校验并读取影响" @confirm="readRestorePreview" @close="clearRestorePreview"><div class="space-y-4"><label class="block text-sm">原备份密码<input v-model="restorePassword" type="password" autocomplete="off" maxlength="256" class="form-input-base mt-2" /></label><p class="text-xs text-gray-500">预览只读。执行恢复将停止中央写入并重新核对账本，恢复后需要重新登录；离线消费不能视为零。</p><p v-if="previewError" role="alert" class="text-sm text-red-600">{{ previewError }}</p><RestorePreviewPanel v-if="restoreResult" :result="restoreResult" /><button v-if="restoreResult?.executionAvailable" class="ios-btn-primary" :disabled="restoreSubmitting" @click="confirmRestore = true">执行已预览备份的恢复</button><p v-if="restoreAccepted" class="text-sm text-amber-700">恢复已受理，编号 {{ restoreAccepted }}。中央服务正在核对并重启，请重新登录后查看恢复运行记录；已受理不代表恢复成功。</p></div></FormDrawer>
    <ConfirmDialog v-model="confirmRestore" title="确认恢复系统" message="恢复选定备份的业务配置，保留经过核对的较新消费与额度。中央服务将短暂停写并重启，旧会话失效。" warning-details="执行前请重新输入原备份密码。节点保留最后成功配置；具体结果请重新登录后查看恢复记录。" confirm-text="确认开始恢复" :is-danger="true" @confirm="executeRestore" />
    <ConfirmDialog v-model="deleteConfirm" title="删除加密备份" :message="`删除 ${formatTime(deleteTarget?.createdAt)} 创建的备份文件与登记记录。此操作不会修改当前业务配置，请确认已保留需要的外部副本。`" confirm-text="确认删除" @confirm="remove" />
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch } from 'vue';
import { backupAPI, type Backup, type BackupRun, type RestorePreview } from '../backupApi';
import { formatTime, isOwner } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import Pagination from '../../../components/Pagination.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
import RestorePreviewPanel from '../components/RestorePreviewPanel.vue';
import RestoreRecordsPanel from '../components/RestoreRecordsPanel.vue';

const items = ref<Backup[]>([]); const total = ref(0); const page = ref(1); const loading = ref(false); const error = ref(''); let loadRequest = 0;
const passwordDrawer = ref(false); const password = ref(''); const passwordConfirm = ref(''); const mode = ref<'create' | 'verify'>('create'); const saving = ref(false); const operationError = ref(''); const verifyTarget = ref<Backup | null>(null);
const importDrawer = ref(false), importing = ref(false), importPassword = ref(''), importError = ref(''), importFile = ref<File | null>(null), fileInputKey = ref(0);
function clearImport() { importPassword.value = ''; importFile.value = null; importError.value = ''; fileInputKey.value++; }
watch(importDrawer, value => { if (!value) clearImport(); });
function openImport() { if (importing.value) return; clearImport(); importDrawer.value = true; }
function selectImportFile(event: Event) { importFile.value = (event.target as HTMLInputElement).files?.[0] || null; importError.value = ''; }
async function importArchive() {
  if (importing.value) return;
  if (!importFile.value || importFile.value.size < 40 || importFile.value.size > (1 << 30) + (16 << 20)) { importError.value = '请选择有效大小的加密备份文件'; return; }
  const length = Array.from(importPassword.value).length;
  if (length < 12 || length > 128) { importError.value = '请输入原备份的 12～128 字符密码'; return; }
  importing.value = true; importError.value = '';
  try { await backupAPI.import(importFile.value, importPassword.value); importDrawer.value = false; clearImport(); page.value = 1; showToast('备份已验证并登记，当前系统数据未恢复或替换'); await load(); }
  catch (e) { importError.value = (e as Error).message; }
  finally { importing.value = false; }
}
const restoreDrawer = ref(false), restorePassword = ref(''), previewLoading = ref(false), previewError = ref(''), restoreTarget = ref<Backup | null>(null), restoreResult = ref<RestorePreview | null>(null); let previewRequest = 0;
const confirmRestore = ref(false), restoreSubmitting = ref(false), restoreAccepted = ref('');
function clearRestorePreview() { previewRequest++; restorePassword.value = ''; previewError.value = ''; restoreResult.value = null; previewLoading.value = false; restoreTarget.value = null; confirmRestore.value = false; restoreAccepted.value = ''; }
watch(restoreDrawer, value => { if (!value) clearRestorePreview(); });
function openRestorePreview(item: Backup) { clearRestorePreview(); restoreTarget.value = item; restoreDrawer.value = true; }
async function readRestorePreview() {
  if (!restoreTarget.value || previewLoading.value) return;
  const length = Array.from(restorePassword.value).length;
  if (length < 12 || length > 128) { previewError.value = '请输入原备份的 12～128 字符密码'; return; }
  const serial = ++previewRequest; const id = restoreTarget.value.id; previewLoading.value = true; previewError.value = ''; restoreResult.value = null;
  try { const result = await backupAPI.restorePreview(id, restorePassword.value); if (serial === previewRequest && restoreDrawer.value) { restoreResult.value = result; restorePassword.value = ''; } }
  catch (e) { if (serial === previewRequest && restoreDrawer.value) previewError.value = (e as Error).message; }
  finally { if (serial === previewRequest) previewLoading.value = false; }
}
async function executeRestore() {
  if (!restoreTarget.value || !restoreResult.value?.executionAvailable || restoreSubmitting.value) return;
  const length = Array.from(restorePassword.value).length;
  if (length < 12 || length > 128) { previewError.value = '执行恢复前请重新输入原备份密码'; confirmRestore.value = false; return; }
  const selected = restoreTarget.value.id; const digest = restoreResult.value.archiveSha256; const serial = previewRequest;
  restoreSubmitting.value = true; previewError.value = ''; restoreAccepted.value = '';
  try { const record = await backupAPI.restore(selected, restorePassword.value, digest); if (serial !== previewRequest || restoreTarget.value?.id !== selected) return; restorePassword.value = ''; restoreAccepted.value = record.id; confirmRestore.value = false; showToast('恢复已受理，服务将重启，请重新登录后核对结果'); }
  catch (e) { if (serial === previewRequest && restoreTarget.value?.id === selected) previewError.value = (e as Error).message; }
  finally { restoreSubmitting.value = false; }
}
const deleteConfirm = ref(false); const deleteTarget = ref<Backup | null>(null); const deleting = ref(false);
function kindLabel(kind: string) { return ({ manual: '手动', daily: '每日', weekly: '每周', monthly: '每月' } as Record<string, string>)[kind] || kind; }
async function load() { const serial = ++loadRequest; loading.value = true; try { const result = await backupAPI.list(page.value); if (serial !== loadRequest) return; items.value = result.items; total.value = result.total; error.value = ''; } catch (e) { if (serial === loadRequest) error.value = (e as Error).message; } finally { if (serial === loadRequest) loading.value = false; } }
function changePage(value: number) { page.value = value; load(); }
function clearPassword() { password.value = ''; passwordConfirm.value = ''; operationError.value = ''; }
watch(passwordDrawer, value => { if (!value) clearPassword(); });
function openCreate() { if (saving.value) return; clearPassword(); verifyTarget.value = null; mode.value = 'create'; passwordDrawer.value = true; }
function openVerify(item: Backup) { if (saving.value) return; clearPassword(); verifyTarget.value = item; mode.value = 'verify'; passwordDrawer.value = true; }
async function submitPassword() {
  if (saving.value) return;
  const length = Array.from(password.value).length;
  if (length < 12 || length > 128) { operationError.value = '请输入包含 12～128 个字符的备份密码'; return; }
  if (mode.value === 'create' && password.value !== passwordConfirm.value) { operationError.value = '两次输入的密码不一致'; return; }
  saving.value = true; operationError.value = '';
  try { if (mode.value === 'create') { await backupAPI.create(password.value); page.value = 1; showToast('加密备份已创建，数据库和材料校验通过'); } else if (verifyTarget.value) { await backupAPI.verify(verifyTarget.value.id, password.value); showToast('备份内容校验通过，未执行恢复'); } passwordDrawer.value = false; clearPassword(); await load(); }
  catch (e) { operationError.value = (e as Error).message; }
  finally { saving.value = false; }
}
function promptDelete(item: Backup) { deleteTarget.value = item; deleteConfirm.value = true; }
async function remove() { if (!deleteTarget.value || deleting.value) return; deleting.value = true; try { await backupAPI.remove(deleteTarget.value.id); deleteConfirm.value = false; if (items.value.length === 1 && page.value > 1) page.value--; showToast('备份文件与登记记录已删除'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } finally { deleting.value = false; } }
const runs = ref<BackupRun[]>([]), runTotal = ref(0), runPage = ref(1), runError = ref(''); let runRequest = 0;
function runStatus(status: string) { return ({ running: '执行中', success: '成功', failed: '失败' } as Record<string, string>)[status] || status; }
async function loadRuns() { const serial = ++runRequest; try { const result = await backupAPI.runs(runPage.value); if (serial !== runRequest) return; runs.value = result.items; runTotal.value = result.total; runError.value = ''; } catch (e) { if (serial === runRequest) runError.value = (e as Error).message; } }
function changeRunPage(value: number) { runPage.value = value; void loadRuns(); }
function backupChanged() { void load(); void loadRuns(); }
onMounted(() => { backupChanged(); window.addEventListener('proxy-backup-status', backupChanged); });
onUnmounted(() => { loadRequest++; runRequest++; clearPassword(); clearImport(); clearRestorePreview(); window.removeEventListener('proxy-backup-status', backupChanged); });
</script>
