<template>
  <div ref="pageRoot" class="space-y-6">
    <!-- Header -->
    <div ref="pageHeading">
      <PageHeader
        title="规则集"
        parent="分流控制"
        description="统一维护可复用的域名、域名后缀、关键字、IP 与 CIDR 集合，并可引入外部规则来源。只定义匹配目标，不包含转发动作，供客户端策略与节点策略共同引用。"
      >
        <template #actions>
          <button type="button" @click="openCreate" class="ios-btn-primary inline-flex items-center gap-1.5">
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
            <span>创建规则集</span>
          </button>
        </template>
      </PageHeader>
    </div>

    <!-- Table States -->
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-if="loading && !display.ready" />
    <EmptyState
      v-else-if="display.ready && !services.length"
      title="暂无规则集"
      description="点击右上角“创建规则集”即可维护统一的域名和 IP 集合。"
    />

    <!-- Rule Sets Table -->
    <DataTable v-else-if="display.ready" class="service-rule-table">
      <template #header>
        <th>规则集名称</th>
        <th>来源 / 版本</th>
        <th>匹配规模 (域名 / 关键字 / IP)</th>
        <th>Clash Rule-Provider 引用</th>
        <th>状态</th>
        <th>更新时间</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="service in services" :key="service.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. Name & Description -->
          <td class="p-4">
            <div class="flex items-center gap-2 whitespace-nowrap">
              <span class="font-bold text-neutral-900 text-base">{{ service.name }}</span>
              <span v-if="service.builtin" class="px-1.5 py-0.2 rounded text-xs font-bold bg-blue-50 text-ios-blue uppercase">
                内置
              </span>
            </div>
            <p class="text-xs text-neutral-500 mt-0.5 line-clamp-1 max-w-sm">{{ service.description || '暂无说明' }}</p>
          </td>

          <!-- 2. Source & Version -->
          <td class="text-xs">
            <span class="font-mono font-bold text-neutral-700 bg-neutral-100 px-2 py-0.5 rounded-md">
              v{{ service.version }}
            </span>
            <template v-if="service.sources.length">
              <span class="ml-1 text-neutral-500 whitespace-nowrap">{{ service.sources.length }} 个外部来源</span>
              <span class="block mt-1 font-medium whitespace-nowrap" :class="sourceStatus(service).tone">{{ sourceStatus(service).label }}</span>
            </template>
          </td>

          <!-- 3. Size -->
          <td class="text-xs font-mono text-neutral-600">
            <div class="flex items-center gap-x-1 whitespace-nowrap">
              <span class="whitespace-nowrap">手写 {{ service.domains.length + service.suffixes.length }} 域名</span>
              <span class="text-neutral-300">/</span>
              <span class="whitespace-nowrap">{{ service.keywords?.length || 0 }} 关键字</span>
              <span class="text-neutral-300">/</span>
              <span class="whitespace-nowrap">{{ service.cidrs.length }} IP网段</span>
            </div>
            <div v-if="sourceSizeText(service)" class="mt-0.5 whitespace-nowrap">{{ sourceSizeText(service) }}</div>
          </td>

          <!-- 4. Rule Provider Quick Copy -->
          <td class="text-xs">
            <button
              type="button"
              @click="openProviderModal(service)"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-neutral-100 hover:bg-neutral-200 text-neutral-700 font-mono text-xs transition-colors"
              title="查看 rule-providers 配置片段"
            >
              <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
              </svg>
              <span>Provider 配置</span>
            </button>
          </td>

          <!-- 5. Status -->
          <td>
            <StatusBadge :status="service.enabled ? 'active' : 'disabled'" />
          </td>

          <!-- 6. Update Time -->
          <td class="text-xs font-mono text-neutral-400 whitespace-nowrap">
            {{ formatTime(service.updatedAt) }}
          </td>

          <!-- 7. Actions -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <button type="button" @click="openEdit(service)" class="ios-action-btn-primary">
              编辑
            </button>
            <button type="button" @click="copy(service)" class="ios-action-btn-secondary">
              克隆
            </button>
            <button type="button" @click="promptDelete(service)" class="ios-action-btn-danger">
              删除
            </button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="pageSize" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- Rule-Provider Dialog -->
    <ServiceProviderDialog v-model="providerModalVisible" :service="selectedService" />

    <ServiceSetEditor
      v-model="editorOpen"
      :service="editorTarget"
      :clone="editorClone"
      @saved="load"
      @deleted="load"
    />

    <!-- Confirm Delete -->
    <ConfirmDialog
      v-model="confirmVisible"
      title="删除规则集"
      :message="`确定删除规则集【${target?.name || ''}】？被客户端策略或节点策略引用的集合请先解除引用。`"
      @confirm="remove"
    />
  </div>
</template>

<script setup lang="ts">
import { retainedQuery, removePageItem } from '../../../app/retainedQuery';
import { ref, onMounted, onUnmounted } from 'vue';
import { policyAPI, type ServiceSet } from '../api';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ServiceSetEditor from '../components/ServiceSetEditor.vue';
import ServiceProviderDialog from '../components/ServiceProviderDialog.vue';

const services = ref<ServiceSet[]>([]);
const page = ref(1);
const pageSize = ref(1);
const pageRoot = ref<HTMLElement>();
const pageHeading = ref<HTMLElement>();
const total = ref(0);
const error = ref('');
const loading = ref(false);

const editorOpen = ref(false);
const editorTarget = ref<ServiceSet | null>(null);
const editorClone = ref(false);
const target = ref<ServiceSet | null>(null);
const confirmVisible = ref(false);

const providerModalVisible = ref(false);
const selectedService = ref<ServiceSet | null>(null);

let generation = 0;
const display = retainedQuery(() => { services.value = []; total.value = 0; });
async function load() {
  const size = pageSize.value;
  display.begin(page.value, size);
  const current = ++generation;
  loading.value = true; error.value = '';
  try {
    const result = await policyAPI.services(page.value, size);
    if (current !== generation) return;
    const lastPage = Math.max(1, Math.ceil(result.total / size));
    if (page.value > lastPage) {
      page.value = lastPage;
      await load();
      return;
    }
    display.ready = true;
    services.value = result.items;
    total.value = result.total;
    error.value = '';
  } catch (e) {
    if (current === generation) { display.reject(e); error.value = (e as Error).message; }
  } finally {
    if (current === generation) loading.value = false;
  }
}

function changePage(value: number) {
  page.value = value;
  load();
}

function openEditor(service: ServiceSet | null, clone = false) {
  editorTarget.value = service;
  editorClone.value = clone;
  editorOpen.value = true;
}

function openCreate() {
  openEditor(null);
}

function openEdit(service: ServiceSet) {
  openEditor(service);
}

function copy(service: ServiceSet) {
  openEditor(service, true);
}

// 列表里的外部来源状态：任一失败即失败，其次有待抓取，否则成功。
function sourceStatus(service: ServiceSet) {
  // 节点原生来源由节点下载，面板没有抓取状态，不参与。
  const statuses = service.sources.filter(s => s.format !== 'native').map(s => s.lastStatus);
  if (!statuses.length) return { label: '由节点下载', tone: 'text-sky-700' };
  if (statuses.includes('failed')) return { label: '失败', tone: 'text-red-600' };
  // v1.5.2: sources of a set no enabled node policy names are not fetched on a schedule.
  if (!service.autoRefresh && statuses.some(s => s !== 'ok')) return { label: '未被策略引用，不自动刷新', tone: 'text-neutral-500' };
  if (statuses.some(s => s !== 'ok')) return { label: '待抓取', tone: 'text-amber-700' };
  return { label: '成功', tone: 'text-emerald-600' };
}

// 外部来源缓存条目数：只统计面板解析的来源（节点可用且非节点原生）；最近一次失败时仍按上次成功的缓存计数，
// 从未成功的显示“未抓取”。仅客户端使用的来源（如 mrs）面板不解析，不计入；节点原生来源由节点下载，单独计数。
function sourceSizeText(service: ServiceSet) {
  const parsed = service.sources.filter(s => s.nodeUsable && s.format !== 'native');
  const nativeCount = service.sources.filter(s => s.format === 'native').length;
  const parts: string[] = [];
  if (parsed.length) {
    const fetched = parsed.filter(s => s.lastSuccessAt);
    const pending = parsed.length - fetched.length;
    if (!fetched.length) {
      parts.push('来源 未抓取');
    } else {
      const count = fetched.reduce((sum, s) => sum + (s.entryCount || 0), 0);
      parts.push(pending ? `来源 ${count} 条 · ${pending} 个未抓取` : `来源 ${count} 条`);
    }
  }
  if (nativeCount) parts.push(`节点原生 ${nativeCount} 个`);
  return parts.join(' · ');
}

function promptDelete(service: ServiceSet) {
  target.value = service;
  confirmVisible.value = true;
}

async function remove() {
  if (!target.value) return;
  try {
    const id = target.value.id;
    await policyAPI.deleteService(id);
    removePageItem(services, total, page, id, pageSize.value);
    showToast('规则集已删除');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

function openProviderModal(service: ServiceSet) {
  selectedService.value = service;
  providerModalVisible.value = true;
}

// The provider is served with the user's own subscription token (the old
// address was an Owner-only JSON API no client could fetch). Its name comes from
// the id, so two services with similar names never share a cache file.

let resizeObserver: ResizeObserver | undefined;
onMounted(() => {
  const heading = pageHeading.value!;
  const main = pageRoot.value!.closest('main')!;
  const fitPage = () => {
    const bottomPadding = parseFloat(getComputedStyle(main).paddingBottom);
    const available = main.getBoundingClientRect().bottom - heading.getBoundingClientRect().bottom - bottomPadding;
    // Leave room for the gap, table heading, footer and a horizontal scrollbar.
    const size = Math.min(100, Math.max(1, Math.floor((available - 144) / 76)));
    if (size === pageSize.value) return;
    const firstRow = (page.value - 1) * pageSize.value;
    pageSize.value = size;
    page.value = Math.floor(firstRow / size) + 1;
    void load();
  };
  fitPage();
  if (pageSize.value === 1) void load();
  resizeObserver = new ResizeObserver(fitPage);
  resizeObserver.observe(main);
  resizeObserver.observe(heading);
});
onUnmounted(() => resizeObserver?.disconnect());
</script>

<style scoped>
.service-rule-table :deep(tbody tr) {
  height: 76px;
}
</style>
