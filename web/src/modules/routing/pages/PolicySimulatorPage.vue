<template>
  <div class="space-y-6">
    <!-- Header -->
    <PageHeader
      title="策略模拟"
      parent="分流控制"
      description="独立模拟客户端与节点端的分流匹配逻辑。输入目标地址与访问条件，实时排查命中规则、执行动作与匹配原因。"
    />

    <!-- Notice Banner -->
    <div class="p-3.5 rounded-2xl bg-ios-blue/[0.06] border border-ios-blue/15 text-xs text-neutral-600 leading-relaxed">
      <strong>双端模拟差异说明</strong>：客户端直连与节点端直连在网络架构上完全独立。客户端直连由用户设备发起，不经服务器；节点端直连由服务器出站发起，消耗服务器出口带宽。两端策略请针对目标场景分别验证。
    </div>

    <!-- Dual Mode Tabs -->
    <div class="flex items-center gap-2 border-b border-black/[0.06] pb-3">
      <button
        type="button"
        @click="activeTab = 'client'"
        :class="[
          'px-4 py-2 rounded-xl text-sm font-semibold transition-all flex items-center gap-2',
          activeTab === 'client'
            ? 'bg-ios-blue text-white shadow-sm'
            : 'text-neutral-600 hover:text-black hover:bg-black/[0.04]'
        ]"
      >
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
        </svg>
        <span>客户端策略模拟 (Clash / Mihomo)</span>
      </button>

      <button
        type="button"
        @click="activeTab = 'node'"
        :class="[
          'px-4 py-2 rounded-xl text-sm font-semibold transition-all flex items-center gap-2',
          activeTab === 'node'
            ? 'bg-ios-blue text-white shadow-sm'
            : 'text-neutral-600 hover:text-black hover:bg-black/[0.04]'
        ]"
      >
        <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
        </svg>
        <span>节点策略模拟 (Xray / Sing-box)</span>
      </button>
    </div>

    <!-- Error State -->
    <ErrorState v-if="error" :message="error" @retry="load" />

    <!-- ==================== Tab 1: 客户端策略模拟 ==================== -->
    <div v-if="activeTab === 'client'" class="grid lg:grid-cols-2 gap-6">
      <!-- Input Panel -->
      <form @submit.prevent="runClientSimulation" class="panel-card p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h2 class="font-semibold text-neutral-900">客户端访问请求条件</h2>
          <span class="text-xs text-neutral-400 font-mono">Clash 路由内核仿真</span>
        </div>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">目标地址 (域名或 IP)</span>
          <input
            v-model="clientForm.address"
            required
            placeholder="例如: api.openai.com 或 1.1.1.1 或 192.168.1.1"
            class="form-input-base mt-1.5 font-mono text-xs"
          />
        </label>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="text-sm">
            <span class="text-xs font-medium text-neutral-600">目标端口</span>
            <input
              v-model.number="clientForm.port"
              type="number"
              min="1"
              max="65535"
              required
              class="form-input-base mt-1.5 font-mono text-xs"
            />
          </label>
          <label class="text-sm">
            <span class="text-xs font-medium text-neutral-600">协议网络</span>
            <select v-model="clientForm.network" class="form-input-base mt-1.5">
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </select>
          </label>
        </div>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">测试用户上下文 (可选)</span>
          <select v-model="clientForm.userId" class="form-input-base mt-1.5">
            <option value="">默认全局客户端模板</option>
            <option v-for="user in users" :key="user.id" :value="user.id">
              {{ user.username }} ({{ user.displayName || '标准用户' }})
            </option>
          </select>
        </label>

        <button type="submit" class="ios-btn-primary w-full justify-center mt-2">
          模拟客户端路由裁决
        </button>
      </form>

      <!-- Output Panel -->
      <section class="panel-card p-6">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-semibold text-neutral-900">客户端裁决结果</h2>
          <span v-if="clientDecision" class="text-xs font-mono text-neutral-400">Match First 裁决</span>
        </div>

        <div v-if="!clientDecision" class="text-sm text-neutral-400 py-12 text-center">
          输入目标地址并点击模拟后，查看命中规则、优先级及客户端实际发起的出站动作。
        </div>

        <template v-else>
          <!-- Hero Decision Box -->
          <div
            class="p-4 rounded-2xl mb-5 border space-y-1"
            :class="
              clientDecision.runtime
                ? 'bg-amber-50/80 border-amber-200 text-amber-900'
                : clientDecision.target === 'REJECT'
                ? 'bg-red-50/80 border-red-200 text-red-900'
                : clientDecision.target === 'DIRECT'
                ? 'bg-emerald-50/80 border-emerald-200 text-emerald-900'
                : 'bg-blue-50/80 border-blue-200 text-blue-900'
            "
          >
            <div class="flex items-center justify-between">
              <span class="text-xs font-bold uppercase tracking-wider font-mono">
                {{ clientDecision.runtime ? `第 #${clientDecision.matchedRule?.priority} 优先级规则需客户端运行时判断` : clientDecision.matched ? `命中第 #${clientDecision.matchedRule?.priority} 优先级规则` : '未命中规则' }}
              </span>
              <span class="px-2 py-0.5 rounded-full text-[11px] font-bold bg-white/70 shadow-sm">
                {{ clientDecision.target }}
              </span>
            </div>
            <h3 class="text-base font-bold tracking-tight">
              {{ clientDecision.actionLabel }}
            </h3>
            <p class="text-xs opacity-90 leading-relaxed pt-1">
              {{ clientDecision.actionDesc }}
            </p>
          </div>

          <!-- Decision Details -->
          <dl class="space-y-4 text-xs">
            <div>
              <dt class="text-neutral-400 font-medium">命中原因说明</dt>
              <dd class="mt-1 font-semibold text-neutral-800 text-[13px] leading-relaxed">
                {{ clientDecision.reason }}
              </dd>
            </div>

            <div>
              <dt class="text-neutral-400 font-medium">路由执行细节</dt>
              <dd class="mt-1 space-y-1 font-mono text-neutral-600">
                <p v-for="(detail, i) in clientDecision.details" :key="i" class="p-1.5 rounded-lg bg-neutral-50 border border-black/[0.04]">
                  {{ detail }}
                </p>
              </dd>
            </div>

            <div class="pt-2 border-t border-black/[0.05] text-neutral-400">
              提示：客户端直连直接经由操作系统网络栈外发，免除任何服务器中转流量与凭证消耗。
            </div>
          </dl>
        </template>
      </section>
    </div>

    <!-- ==================== Tab 2: 节点策略模拟 ==================== -->
    <div v-else class="grid lg:grid-cols-2 gap-6">
      <!-- Input Panel -->
      <form @submit.prevent="runNodeSimulation" class="panel-card p-6 space-y-4">
        <div class="flex items-center justify-between">
          <h2 class="font-semibold text-neutral-900">服务器请求条件</h2>
          <span class="text-xs text-neutral-400 font-mono">Xray / Sing-box 内核仿真</span>
        </div>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">入口节点 (必须)</span>
          <select v-model="nodeForm.nodeId" required @change="nodeForm.inboundId = ''" class="form-input-base mt-1.5">
            <option value="">请选择入口服务器...</option>
            <option v-for="node in nodes" :key="node.id" :value="node.id">
              {{ node.name }} ({{ node.publicIp || '未分配 IP' }})
            </option>
          </select>
        </label>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">入站协议 (可选，用于确定默认入站规则)</span>
          <select v-model="nodeForm.inboundId" class="form-input-base mt-1.5">
            <option value="">默认 Direct 入站</option>
            <option v-for="inbound in inbounds.filter(item => item.nodeId === nodeForm.nodeId)" :key="inbound.id" :value="inbound.id">
              {{ inbound.name }} · {{ inbound.protocol }}
            </option>
          </select>
        </label>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="text-sm">
            <span class="text-xs font-medium text-neutral-600">入站用户 (可选)</span>
            <select v-model="nodeForm.userId" class="form-input-base mt-1.5">
              <option value="">未指定用户</option>
              <option v-for="user in users" :key="user.id" :value="user.id">{{ user.username }}</option>
            </select>
          </label>
        </div>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">请求域名</span>
          <input v-model="nodeForm.domain" placeholder="api.example.com (不含 http:// 或路径)" class="form-input-base mt-1.5 font-mono text-xs" />
        </label>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">目标 IP (可选)</span>
          <input v-model="nodeForm.targetIp" placeholder="192.0.2.10" class="form-input-base mt-1.5 font-mono text-xs" />
        </label>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="text-sm">
            <span class="text-xs font-medium text-neutral-600">端口</span>
            <input v-model.number="nodeForm.port" type="number" min="1" max="65535" required class="form-input-base mt-1.5 font-mono text-xs" />
          </label>
          <label class="text-sm">
            <span class="text-xs font-medium text-neutral-600">网络</span>
            <select v-model="nodeForm.network" class="form-input-base mt-1.5">
              <option value="tcp">TCP</option>
              <option value="udp">UDP</option>
            </select>
          </label>
        </div>

        <label class="block text-sm">
          <span class="text-xs font-medium text-neutral-600">请求时间 ({{ authState.timezone }})</span>
          <input v-model="timeLocal" type="datetime-local" class="form-input-base mt-1.5 text-xs" />
        </label>

        <p v-if="nodeSimulationError" role="alert" class="text-xs text-red-600">{{ nodeSimulationError }}</p>

        <button :disabled="busy" class="ios-btn-primary w-full justify-center disabled:opacity-40 mt-2">
          {{ busy ? '正在模拟节点策略…' : '执行服务器策略裁决' }}
        </button>
      </form>

      <!-- Output Panel -->
      <section class="panel-card p-6">
        <div class="flex items-center justify-between mb-4">
          <h2 class="font-semibold text-neutral-900">节点端裁决结果</h2>
          <span v-if="nodeDecision" class="text-xs font-mono text-neutral-400">Agent 真实仿真</span>
        </div>

        <p v-if="!nodeDecision" class="text-sm text-neutral-400 py-12 text-center">
          选择入口节点并提交请求条件后，查看服务器端执行动作、多跳路径与失败容灾策略。
        </p>

        <template v-else>
          <!-- Hero Decision Box -->
          <div
            class="p-4 rounded-2xl mb-5 border space-y-1"
            :class="
              nodeDecision.action.type === 'Block'
                ? 'bg-red-50/80 border-red-200 text-red-900'
                : nodeDecision.action.type === 'Direct'
                ? 'bg-emerald-50/80 border-emerald-200 text-emerald-900'
                : 'bg-blue-50/80 border-blue-200 text-blue-900'
            "
          >
            <div class="flex items-center justify-between">
              <span class="text-xs font-bold uppercase tracking-wider font-mono">
                {{ nodeDecision.matched ? `策略: ${nodeDecision.policyName || nodeDecision.policyId}` : '入站默认路由' }}
              </span>
              <span class="px-2 py-0.5 rounded-full text-[11px] font-bold bg-white/70 shadow-sm">
                {{ nodeDecision.matched ? `优先级 #${nodeDecision.priority}` : 'Default' }}
              </span>
            </div>
            <h3 class="text-base font-bold tracking-tight">
              {{ actionNames[nodeDecision.action.type] || nodeDecision.action.type }}
            </h3>
            <p class="text-xs opacity-90 leading-relaxed pt-1">
              {{ nodeDecision.reason || (nodeDecision.matched ? '已完全匹配范围、目标地址、端口、网络与时间窗口。' : '未命中启用策略，使用入站默认出站。') }}
            </p>
          </div>

          <!-- Detail Attributes -->
          <dl class="space-y-4 text-xs">
            <div>
              <dt class="text-neutral-400 font-medium">配置网络路径</dt>
              <dd class="mt-1 font-mono text-neutral-800 font-medium">
                {{ nodeDecision.path?.map(id => nodes.find(node => node.id === id)?.name || id).join(' → ') || '—' }}
              </dd>
            </div>

            <div v-if="nodeDecision.candidatePaths?.length">
              <dt class="text-neutral-400 font-medium">
                候选出口路径 (策略: {{ strategyNames[nodeDecision.selectionStrategy || ''] || nodeDecision.selectionStrategy }})
              </dt>
              <dd v-for="(path, index) in nodeDecision.candidatePaths" :key="index" class="mt-1 font-mono text-neutral-700">
                {{ path.map(id => nodes.find(node => node.id === id)?.name || id).join(' → ') }}
              </dd>
            </div>

            <div>
              <dt class="text-neutral-400 font-medium">最终配置出口</dt>
              <dd class="mt-1 font-medium text-neutral-800">
                {{ nodes.find(node => node.id === nodeDecision.finalEgressNodeId)?.name || nodeDecision.finalEgressNodeId || (nodeDecision.action.type === 'OutboundGroup' ? '由节点调度选择' : (nodeDecision.action.type === 'Direct' ? '服务器本地直接出站' : '阻断拦截')) }}
              </dd>
            </div>

            <div>
              <dt class="text-neutral-400 font-medium">失败容灾策略 (Failover)</dt>
              <dd class="mt-1 text-neutral-700 font-mono">
                {{ nodeDecision.action.failover || 'Block 默认阻断' }}
              </dd>
            </div>

            <div>
              <dt class="text-neutral-400 font-medium">日志记录设置</dt>
              <dd class="mt-1 text-neutral-700">
                {{ nodeDecision.logging?.log_domain ? '记录请求域名' : '未开启域名审计' }} ·
                {{ nodeDecision.logging?.log_traffic ? '记录流量统计' : '未开启流量审计' }}
              </dd>
            </div>

            <div class="pt-2 border-t border-black/[0.05] text-neutral-400">
              提示：服务器端 Direct 代表由该服务器发起出站网络请求，不属于用户客户端直连。
            </div>
          </dl>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { useRoute } from 'vue-router';
import {
  policyAPI,
  clientPolicyAPI,
  actionNames,
  simulateClientPolicy,
  type Decision,
  type ClientSimulationResult,
  type ServiceSet
} from '../api';
import { request, type Page } from '../../../app/apiClient';
import { authState } from '../../../app/auth';
import { utcInput } from '../../../app/time';
import { userAPI, type ProxyUser } from '../../users/api';
import { type NodeView } from '../../nodes/api';
import { proxyAPI, type Inbound, strategyNames } from '../../proxy/api';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';

const route = useRoute();
const activeTab = ref<'client' | 'node'>(route.query.tab === 'client' ? 'client' : 'node');

// Client Simulation State
const clientForm = reactive({
  address: typeof route.query.domain === 'string' ? route.query.domain : '',
  port: 443,
  network: 'tcp',
  userId: ''
});
const clientDecision = ref<ClientSimulationResult | null>(null);

// Node Simulation State
const nodeForm = reactive({
  nodeId: '',
  inboundId: '',
  userId: '',
  domain: typeof route.query.domain === 'string' ? route.query.domain : '',
  targetIp: '',
  port: 443,
  network: 'tcp'
});
const timeLocal = ref('');
const nodeDecision = ref<Decision | null>(null);
const nodeSimulationError = ref('');
const busy = ref(false);

// Auxiliary Data
const nodes = ref<NodeView[]>([]);
const users = ref<ProxyUser[]>([]);
const inbounds = ref<Inbound[]>([]);
const serviceSets = ref<ServiceSet[]>([]);
const error = ref('');

async function load() {
  try {
    const [n, u, i, s] = await Promise.all([
      request<Page<NodeView>>('/nodes?page=1&pageSize=100'),
      userAPI.list(1, '', 100),
      proxyAPI.inbounds('', 1, 100),
      policyAPI.services(1, 100)
    ]);
    nodes.value = n.items;
    users.value = u.items;
    inbounds.value = i.items;
    serviceSets.value = s.items;
    error.value = '';

    // If address was in query, automatically run client simulation
    if (clientForm.address) {
      runClientSimulation();
    }
  } catch (e) {
    error.value = (e as Error).message;
  }
}

// The client tab simulates the saved template (the one users' profiles carry).
// Network and user do not change a client rule's outcome: the template has no
// network rule, and it is the same for every user.
async function runClientSimulation() {
  try {
    const config = await clientPolicyAPI.get();
    clientDecision.value = simulateClientPolicy(
      clientForm.address,
      clientForm.port,
      config,
      serviceSets.value
    );
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function runNodeSimulation() {
  if (busy.value) return;
  busy.value = true;
  nodeSimulationError.value = '';
  nodeDecision.value = null;
  try {
    nodeDecision.value = await policyAPI.simulate({
      ...nodeForm,
      time: timeLocal.value ? utcInput(timeLocal.value) : undefined
    });
  } catch (e) {
    nodeSimulationError.value = (e as Error).message;
  } finally {
    busy.value = false;
  }
}

onMounted(load);
</script>
