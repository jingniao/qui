<template>
  <div class="space-y-6">
    <!-- Header -->
    <PageHeader
      title="规则集"
      parent="分流控制"
      description="统一维护可复用的域名、域名后缀、IP 与 CIDR 集合。只定义匹配目标，不包含转发动作，供客户端策略与节点策略共同引用。"
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

    <!-- Important Notice Banner -->
    <div class="p-4 rounded-2xl bg-ios-blue/[0.06] border border-ios-blue/15 space-y-2">
      <div class="flex items-center gap-2 text-ios-blue font-semibold text-xs tracking-wide">
        <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>规则集定位与两端更新生效机制</span>
      </div>
      <p class="text-xs text-neutral-600 leading-relaxed">
        • <strong>纯目标定义</strong>：规则集仅定义域名、IP 或网段的匹配目标（如 OpenAI、常见广告黑名单、内网私有地址），<strong>不包含任何转发或阻断动作</strong>。动作在“客户端策略”与“节点策略”中自由绑定。<br />
        • <strong>两端更新方式（保存不承诺立即生效）</strong>：<br />
        &nbsp;&nbsp;1. <strong>客户端端</strong>：Clash / Mihomo 需等待其 rule-providers 的定时拉取周期（默认 24h）或在客户端中手动点击“刷新规则集”；<br />
        &nbsp;&nbsp;2. <strong>节点端</strong>：保存后规则集版本号递增，需通过 Agent 发布配置草稿后，目标服务器才会加载最新集合。
      </p>
    </div>

    <!-- Table States -->
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !services.length" />
    <EmptyState
      v-else-if="!services.length"
      title="暂无规则集"
      description="点击右上角“创建规则集”即可维护统一的域名和 IP 集合。"
    />

    <!-- Rule Sets Table -->
    <DataTable v-else>
      <template #header>
        <th>规则集名称</th>
        <th>来源 / 版本</th>
        <th>匹配规模 (域名 / IP)</th>
        <th>Clash Rule-Provider 引用</th>
        <th>状态</th>
        <th>更新时间</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="service in services" :key="service.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. Name & Description -->
          <td class="p-4">
            <div class="flex items-center gap-2">
              <span class="font-bold text-neutral-900 text-[15px]">{{ service.name }}</span>
              <span v-if="service.builtin" class="px-1.5 py-0.2 rounded text-[10px] font-bold bg-blue-50 text-ios-blue uppercase">
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
          </td>

          <!-- 3. Size -->
          <td class="text-xs font-mono text-neutral-600">
            <span>{{ service.domains.length + service.suffixes.length }} 域名</span>
            <span class="text-neutral-300 mx-1">/</span>
            <span>{{ service.cidrs.length }} IP网段</span>
          </td>

          <!-- 4. Rule Provider Quick Copy -->
          <td class="text-xs">
            <button
              type="button"
              @click="openProviderModal(service)"
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-neutral-100 hover:bg-neutral-200 text-neutral-700 font-mono text-[11px] transition-colors"
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
          <td class="text-xs font-mono text-neutral-400">
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
            <button v-if="!service.builtin" type="button" @click="promptDelete(service)" class="ios-action-btn-danger">
              删除
            </button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- Rule-Provider Dialog -->
    <AppDialog
      v-model="providerModalVisible"
      :title="`规则集【${selectedService?.name || ''}】的 Clash / Mihomo 引用`"
      :show-footer="false"
      class="max-w-2xl"
    >
      <div class="space-y-4">
        <p class="text-xs text-neutral-500 leading-relaxed">
          在「客户端策略」中用 <code class="font-mono text-ios-blue">RULE-SET</code> 选择此规则集后，用户的 Clash / mihomo 订阅会自动带上对应的 rule-provider，无需手工配置。地址以用户自己的订阅 Token 鉴权，下列信息供手工配置时参考：
        </p>

        <div class="relative">
          <pre class="bg-neutral-900 text-neutral-100 p-4 rounded-2xl text-xs font-mono overflow-x-auto leading-relaxed">{{ providerYamlSnippet }}</pre>
          <button
            type="button"
            @click="copySnippet"
            class="absolute top-3 right-3 px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white text-xs font-medium backdrop-blur-md transition-colors"
          >
            {{ snippetCopied ? '已复制！' : '复制代码' }}
          </button>
        </div>

        <div class="space-y-1.5 pt-1">
          <span class="text-xs font-medium text-neutral-600">直接 URL 地址:</span>
          <div class="flex items-center gap-2">
            <input
              readonly
              :value="providerDirectUrl"
              class="form-input-base text-xs font-mono text-neutral-700 bg-neutral-50"
            />
            <button type="button" @click="copyUrl" class="ios-btn-secondary shrink-0 text-xs py-2">
              复制 URL
            </button>
          </div>
        </div>

        <div class="flex justify-end pt-2">
          <button type="button" @click="providerModalVisible = false" class="ios-btn-secondary">
            关闭
          </button>
        </div>
      </div>
    </AppDialog>

    <!-- Form Drawer: Edit / Create -->
    <FormDrawer
      v-model="drawer"
      :title="editing ? `编辑规则集 · v${editing.version}` : '创建自定义规则集'"
      description="统一维护目标域名、后缀与 IP/CIDR。保存后版本号自动增加，两端需按各自机制刷新生效。"
      :loading="saving"
      confirm-text="保存规则集"
      @confirm="save"
    >
      <div class="space-y-4 text-sm">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">规则集名称</span>
          <input v-model="form.name" maxlength="40" placeholder="例如: OpenAI 官方服务 / 国内白名单" class="form-input-base mt-1.5" required />
        </label>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">业务说明</span>
          <textarea v-model="form.description" rows="2" maxlength="300" placeholder="说明该规则集的收录范围与用途…" class="form-input-base mt-1.5"></textarea>
        </label>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">精确 / 通配域名（每行一个）</span>
          <textarea
            v-model="domainsText"
            rows="4"
            placeholder="api.openai.com&#10;chat.openai.com&#10;*.oaistatic.com"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
        </label>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">域名后缀（每行一个）</span>
          <textarea
            v-model="suffixesText"
            rows="4"
            placeholder="openai.com&#10;anthropic.com&#10;claude.ai"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
        </label>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">IP / CIDR 网段（每行一个）</span>
          <textarea
            v-model="cidrsText"
            rows="3"
            placeholder="192.168.0.0/16&#10;10.0.0.0/8&#10;172.16.0.0/12"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
          <span class="block text-xs text-neutral-400 mt-1">
            仅用于字面 IP / CIDR 匹配，不发起反向 DNS 解析。
          </span>
        </label>

        <label class="block pt-1">
          <label class="inline-flex items-center gap-2 text-xs text-neutral-700">
            <input v-model="form.enabled" type="checkbox" />
            <span>启用此规则集</span>
          </label>
        </label>

        <div class="p-3 rounded-xl bg-neutral-100 text-[11px] text-neutral-500 leading-relaxed">
          <strong>生效机制提示</strong>：保存修改后系统将递增该规则集的版本号并记录更新时间。客户端与节点端均需按照各自更新流程同步最新内容，保存操作本身不保证流量即刻按新规则路由。
        </div>
      </div>
    </FormDrawer>

    <!-- Confirm Delete -->
    <ConfirmDialog
      v-model="confirmVisible"
      title="删除自定义规则集"
      :message="`确定删除规则集【${target?.name || ''}】？被客户端策略或节点策略引用的集合请先解除引用。`"
      @confirm="remove"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { policyAPI, type ServiceSet, type ServiceInput } from '../api';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const services = ref<ServiceSet[]>([]);
const page = ref(1);
const total = ref(0);
const error = ref('');
const loading = ref(false);

const drawer = ref(false);
const saving = ref(false);
const editing = ref<ServiceSet | null>(null);
const target = ref<ServiceSet | null>(null);
const confirmVisible = ref(false);

const domainsText = ref('');
const suffixesText = ref('');
const cidrsText = ref('');

const providerModalVisible = ref(false);
const selectedService = ref<ServiceSet | null>(null);
const snippetCopied = ref(false);

const form = reactive<ServiceInput>({
  name: '',
  enabled: true,
  description: '',
  domains: [],
  suffixes: [],
  cidrs: []
});

async function load() {
  loading.value = true;
  try {
    const result = await policyAPI.services(page.value);
    services.value = result.items;
    total.value = result.total;
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function changePage(value: number) {
  page.value = value;
  load();
}

function openCreate() {
  editing.value = null;
  Object.assign(form, {
    name: '',
    enabled: true,
    description: '',
    domains: [],
    suffixes: [],
    cidrs: []
  });
  domainsText.value = '';
  suffixesText.value = '';
  cidrsText.value = '';
  drawer.value = true;
}

function openEdit(service: ServiceSet) {
  editing.value = service;
  Object.assign(form, {
    name: service.name,
    enabled: service.enabled,
    description: service.description,
    domains: [...service.domains],
    suffixes: [...service.suffixes],
    cidrs: [...service.cidrs]
  });
  domainsText.value = service.domains.join('\n');
  suffixesText.value = service.suffixes.join('\n');
  cidrsText.value = service.cidrs.join('\n');
  drawer.value = true;
}

function copy(service: ServiceSet) {
  openEdit(service);
  editing.value = null;
  form.name = service.name + '-副本';
}

function lines(value: string) {
  return value
    .split('\n')
    .map(item => item.trim())
    .filter(Boolean);
}

async function save() {
  if (saving.value) return;
  saving.value = true;
  try {
    await policyAPI.saveService(editing.value?.id || null, {
      ...form,
      domains: lines(domainsText.value),
      suffixes: lines(suffixesText.value),
      cidrs: lines(cidrsText.value)
    });
    drawer.value = false;
    showToast('规则集已保存（需按两端机制触发更新生效）');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

function promptDelete(service: ServiceSet) {
  target.value = service;
  confirmVisible.value = true;
}

async function remove() {
  if (!target.value) return;
  try {
    await policyAPI.deleteService(target.value.id);
    showToast('规则集已删除');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

function openProviderModal(service: ServiceSet) {
  selectedService.value = service;
  snippetCopied.value = false;
  providerModalVisible.value = true;
}

// The provider is served with the user's own subscription token (the old
// address was an Owner-only JSON API no client could fetch). Its name comes from
// the id, so two services with similar names never share a cache file.
const providerDirectUrl = computed(() => {
  if (!selectedService.value) return '';
  return `${window.location.origin}/sub/<订阅 Token>/rules/${selectedService.value.id}`;
});

const providerYamlSnippet = computed(() => {
  if (!selectedService.value) return '';
  return [
    `名称: qui-service-${selectedService.value.id}`,
    '类型: http · behavior: classical · format: text',
    `地址: ${providerDirectUrl.value}`,
    '刷新: 86400 秒'
  ].join('\n');
});

function copySnippet() {
  navigator.clipboard.writeText(providerYamlSnippet.value);
  snippetCopied.value = true;
  showToast('规则集引用信息已复制');
  setTimeout(() => {
    snippetCopied.value = false;
  }, 2500);
}

function copyUrl() {
  navigator.clipboard.writeText(providerDirectUrl.value);
  showToast('Provider URL 已复制');
}

onMounted(load);
</script>
