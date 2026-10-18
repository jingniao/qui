<template>
  <section class="space-y-4">
    <div class="flex items-center justify-between gap-3"><h2 class="text-lg font-semibold">恢复运行记录</h2><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新恢复记录</button></div>
    <p class="text-sm text-gray-500">展示已保存的阶段。启动记录在实际启动前保存，不证明当前服务仍运行或启动成功。中断需结合原日志与系统状态核对，不代表自动回滚。页面执行恢复仍在开发。</p>
    <ErrorState v-if="error" :message="error" @retry="load" /><LoadingSkeleton v-else-if="loading && !records.length" /><EmptyState v-else-if="!records.length" title="暂无恢复运行记录" description="本地恢复命令完成备份认证后会记录实际阶段。" />
    <DataTable v-else min-width="min-w-[900px]"><template #header><th class="p-4">开始时间</th><th>备份编号</th><th>运行状态</th><th>上次确认阶段</th><th>更新时间</th><th>记录版本</th></template><template #body><tr v-for="record in records" :key="record.id" class="border-t"><td class="p-4 text-xs">{{ formatTime(record.createdAt) }}</td><td class="text-xs font-mono" :title="record.archiveSha256">{{ record.backupId.slice(0, 12) }}…</td><td>{{ statuses[record.status] }}<p v-if="record.errorCode" class="text-xs text-amber-700">{{ record.status === 'interrupted' ? '进程退出后需要核对当前状态' : '执行失败，保留已确认阶段' }}</p></td><td>{{ stages[record.lastStage] || record.lastStage }}<p v-if="record.runtimeEnteredAt" class="text-xs text-gray-500">进入运行阶段回执：{{ formatTime(record.runtimeEnteredAt) }}；不证明当前仍在线或协议已验收。</p><p v-else-if="record.lastStage==='serving' || record.lastStage==='stopped'" class="text-xs text-amber-700">没有进入运行阶段回执，旧记录或本次结果未知。</p></td><td class="text-xs">{{ formatTime(record.updatedAt) }}</td><td>{{ record.sequence }}</td></tr></template><template #footer><Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" /></template></DataTable>
  </section>
</template>
<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { backupAPI, type RestoreRecord } from '../backupApi';
import { formatTime } from '../../../app/auth';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
const records = ref<RestoreRecord[]>([]), total = ref(0), page = ref(1), error = ref(''), loading = ref(false); let generation = 0;
const statuses = { active: '开始已记录，结束未确认', stopped: '停止结果已记录', failed: '执行失败', interrupted: '运行中断' };
const stages: Record<string, string> = { preparing: '准备候选', installing: '安装文件', committed: '文件已提交', cleaned: '暂存已清理', serving: '启动开始已记录，结果待确认', stopped: '服务已停止' };
async function load() { const request = ++generation; loading.value = true; try { const result = await backupAPI.restoreRecords(page.value); if (request !== generation) return; records.value = result.items; total.value = result.total; error.value = ''; } catch (e) { if (request === generation) error.value = (e as Error).message; } finally { if (request === generation) loading.value = false; } }
function changePage(value: number) { page.value = value; void load(); }
onMounted(load); onUnmounted(() => { generation++; });
</script>
