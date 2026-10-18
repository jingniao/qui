<template>
  <div class="space-y-6">
    <PageHeader title="管理员操作日志" parent="系统" description="记录真实后台操作结果，日志不包含密码、Token 或私钥。" />
    <form @submit.prevent="search" class="flex gap-3"><input v-model="keyword" placeholder="搜索操作人、操作或目标" class="form-input-base" /><button class="ios-btn-primary shrink-0">查询</button></form>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />
    <EmptyState v-else-if="!operations.length" title="暂无操作记录" />
    <DataTable v-else>
      <template #header><th>时间</th><th>操作人</th><th>操作</th><th>目标</th><th>结果</th><th>来源 IP</th></template>
      <template #body><tr v-for="operation in operations" :key="operation.id"><td class="p-4">{{ formatTime(operation.timestamp) }}</td><td>{{ operation.operator }}</td><td>{{ operation.action }}</td><td>{{ operation.target }}</td><td>{{ operation.result }}</td><td>{{ operation.ip }}</td></tr></template>
      <template #footer><Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" /></template>
    </DataTable>
  </div>
</template>
<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { request, type Page } from '../../../app/apiClient';
import { formatTime } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
interface Operation { id: number; timestamp: string; operator: string; action: string; target: string; result: string; ip: string; }
const operations = ref<Operation[]>([]); const page = ref(1); const total = ref(0); const keyword = ref(''); const loading = ref(false); const error = ref('');
async function load() {
  loading.value = true; error.value = '';
  try { const result = await request<Page<Operation>>(`/operations?page=${page.value}&pageSize=20&keyword=${encodeURIComponent(keyword.value)}`); operations.value = result.items; total.value = result.total; }
  catch (e) { error.value = (e as Error).message; } finally { loading.value = false; }
}
function search() { page.value = 1; load(); }
function changePage(value: number) { page.value = value; load(); }
onMounted(load);
</script>

