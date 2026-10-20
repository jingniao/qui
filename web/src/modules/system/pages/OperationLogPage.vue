<template>
  <div class="space-y-6">
    <PageHeader title="管理员操作日志" parent="系统" description="记录真实后台操作结果，日志不包含密码、Token 或私钥。" />

    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索操作人、操作或目标…"
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
    </form>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />
    <EmptyState v-else-if="!operations.length" title="暂无操作记录" />
    <DataTable v-else>
      <template #header>
        <th>时间</th>
        <th>操作人</th>
        <th>操作</th>
        <th>目标</th>
        <th>结果</th>
        <th>来源 IP</th>
      </template>
      <template #body>
        <tr v-for="operation in operations" :key="operation.id">
          <td class="text-xs font-mono tabular-nums text-ios-tertiary">{{ formatTime(operation.timestamp) }}</td>
          <td class="text-sm font-medium text-ios-label">{{ operation.operator }}</td>
          <td>
            <span class="px-2 py-0.5 rounded-full text-xs font-medium bg-black/[0.05] text-ios-secondary">
              {{ operation.action }}
            </span>
          </td>
          <td class="text-xs font-mono tabular-nums text-ios-secondary">{{ operation.target }}</td>
          <td>
            <span
              :class="[
                'px-2 py-0.5 rounded-full text-[11px] font-semibold border',
                operation.result === 'success' || operation.result === '成功'
                  ? 'bg-emerald-50 text-emerald-800 border-emerald-200'
                  : 'bg-rose-50 text-rose-800 border-rose-200'
              ]"
            >
              {{ operation.result === 'success' ? '成功' : (operation.result === 'failed' ? '失败' : operation.result) }}
            </span>
          </td>
          <td class="text-xs font-mono tabular-nums text-ios-tertiary">{{ operation.ip }}</td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :page-size="20" :current-page="page" @update:page="changePage" />
      </template>
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
