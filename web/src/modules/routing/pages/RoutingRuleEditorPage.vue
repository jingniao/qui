<template>
  <div class="space-y-6">
    <PageHeader
      :title="editingId ? '编辑节点策略' : '新建节点策略'"
      parent="分流控制"
      description="管理服务器端的分流与访问控制。保存为草稿后，通过 Agent 任务发布到对应节点生效。"
    >
      <template #actions>
        <router-link to="/routing/rules" class="ios-btn-secondary">
          返回节点策略
        </router-link>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />

    <form v-else @submit.prevent="save" class="space-y-6">
      <!-- 1. 基础信息 -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-4">基础信息</h2>
        <div class="grid md:grid-cols-2 gap-4">
          <label class="text-sm">
            策略名称
            <input v-model="form.name" required maxlength="80" placeholder="例如：海外流媒体解锁出口 / 广告域名服务器拦截" class="form-input-base mt-2" />
          </label>
          <label class="text-sm">
            优先级 (数字越小越先执行)
            <input v-model.number="form.priority" type="number" min="0" max="1000000" required class="form-input-base mt-2" />
          </label>
        </div>
        <label class="block text-sm mt-4">
          <input v-model="form.enabled" type="checkbox" />
          <span class="ml-2 font-medium">启用此节点策略</span>
        </label>
      </section>

      <!-- 2. 生效范围 -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-1">生效范围 (Scope)</h2>
        <p class="text-xs text-neutral-500 mb-4">未勾选的项目表示适用于全部；多类别共同约束入站流量。</p>
        <div class="grid md:grid-cols-2 gap-6">
          <fieldset>
            <legend class="text-sm font-medium mb-2 text-neutral-700">限定用户 (User Scope)</legend>
            <div class="max-h-44 overflow-y-auto space-y-1.5 p-2 bg-neutral-50 rounded-xl border border-black/[0.04]">
              <label v-for="user in users" :key="user.id" class="flex items-center gap-2 text-sm text-neutral-700 hover:text-black">
                <input v-model="form.scope.user_ids" type="checkbox" :value="user.id" />
                <span>{{ user.username }}</span>
              </label>
              <div v-if="!users.length" class="text-xs text-neutral-400 py-1">暂无用户</div>
            </div>
          </fieldset>

          <fieldset>
            <legend class="text-sm font-medium mb-2 text-neutral-700">限定入口服务器节点 (Node Scope)</legend>
            <div class="max-h-44 overflow-y-auto space-y-1.5 p-2 bg-neutral-50 rounded-xl border border-black/[0.04]">
              <label v-for="node in nodes" :key="node.id" class="flex items-center gap-2 text-sm text-neutral-700 hover:text-black">
                <input v-model="form.scope.node_ids" type="checkbox" :value="node.id" />
                <span>{{ node.name }}</span>
              </label>
              <div v-if="!nodes.length" class="text-xs text-neutral-400 py-1">暂无节点</div>
            </div>
          </fieldset>

          <fieldset>
            <legend class="text-sm font-medium mb-2 text-neutral-700">限定入口线路组 (Group Scope)</legend>
            <div class="max-h-44 overflow-y-auto space-y-1.5 p-2 bg-neutral-50 rounded-xl border border-black/[0.04]">
              <label v-for="group in groups" :key="group.id" class="flex items-center gap-2 text-sm text-neutral-700 hover:text-black">
                <input v-model="form.scope.group_ids" type="checkbox" :value="group.id" />
                <span>{{ group.name }}</span>
              </label>
              <div v-if="!groups.length" class="text-xs text-neutral-400 py-1">暂无线路组</div>
            </div>
          </fieldset>
        </div>
      </section>

      <!-- 3. 地址与服务规则集匹配 -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-1">地址与规则集匹配 (Match)</h2>
        <p class="text-xs text-neutral-500 mb-4">
          域名、后缀、IP/CIDR 与引用的统一规则集作为目标匹配条件，任一命中即可触发。<br />
          注意：IP / CIDR 仅匹配流量中的字面 IP 地址，不发起 DNS 解析，也不将域名解析为 IP 后二次匹配。
        </p>
        <div class="grid md:grid-cols-2 gap-4">
          <label class="text-sm">
            精确 / 通配域名（每行一个）
            <textarea
              v-model="domainsText"
              rows="4"
              placeholder="api.example.com&#10;*.stream.example"
              class="form-input-base mt-2 font-mono text-xs"
            ></textarea>
          </label>

          <label class="text-sm">
            域名后缀（每行一个）
            <textarea
              v-model="suffixesText"
              rows="4"
              placeholder="netflix.com&#10;openai.com"
              class="form-input-base mt-2 font-mono text-xs"
            ></textarea>
          </label>

          <label class="text-sm">
            IP / CIDR 网段（每行一个）
            <textarea
              v-model="cidrsText"
              rows="3"
              placeholder="192.0.2.0/24&#10;2001:db8::/32"
              class="form-input-base mt-2 font-mono text-xs"
            ></textarea>
          </label>

          <fieldset>
            <legend class="text-sm mb-2 font-medium text-neutral-700">关联统一规则集</legend>
            <div class="max-h-36 overflow-y-auto space-y-1.5 p-2 bg-neutral-50 rounded-xl border border-black/[0.04]">
              <label v-for="service in services" :key="service.id" class="flex items-center gap-2 text-sm text-neutral-700 hover:text-black">
                <input v-model="form.match.service_ids" type="checkbox" :value="service.id" />
                <span>{{ service.name }}</span>
                <span v-if="!service.enabled" class="text-xs text-amber-600 font-mono">（已禁用）</span>
              </label>
              <div v-if="!services.length" class="text-xs text-neutral-400 py-1">暂无规则集</div>
            </div>
          </fieldset>
        </div>
      </section>

      <!-- 4. 端口、网络与时间 -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-4">端口、网络与生效时间</h2>
        <div class="grid md:grid-cols-2 gap-4">
          <label class="text-sm">
            端口或范围（逗号分隔）
            <input v-model="portsText" placeholder="80,443,8000-9000" class="form-input-base mt-2" />
          </label>
          <label class="text-sm">
            协议网络
            <select v-model="form.match.network" class="form-input-base mt-2">
              <option value="all">TCP + UDP</option>
              <option value="tcp">TCP 专有</option>
              <option value="udp">UDP 专有</option>
            </select>
            <p v-if="form.match.network !== 'tcp' && form.scope.user_ids?.length" class="text-xs text-amber-700 mt-3">当前 Xray 的 SOCKS5 UDP 不携带账户身份，涉及该入站的用户/设备 UDP 策略会被拒绝发布。请使用可执行该范围的协议或核心。</p>
          </label>
          <label class="text-sm">
            生效时间段（留空表示全天 24 小时）
            <input v-model="form.match.time_range" placeholder="例如: 23:00-06:00" class="form-input-base mt-2" />
          </label>
          <label class="text-sm">
            时间段时区
            <input v-model="form.timezone" placeholder="Asia/Shanghai" class="form-input-base mt-2" />
          </label>
        </div>
      </section>

      <!-- 5. 服务器端执行动作 (Action) -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-1">服务器端执行动作 (Action)</h2>
        <p class="text-xs text-neutral-500 mb-4">
          注意：<strong>服务器直连 (Direct)</strong> 指由服务器端出口直接发起访问，不等于用户设备本地直连。
        </p>
        <div class="grid md:grid-cols-2 gap-4">
          <label class="text-sm">
            执行动作
            <select v-model="form.action.type" class="form-input-base mt-2">
              <option value="Direct">Direct · 服务器直连（由服务器节点直接发起外部连接）</option>
              <option value="Block">Block · 服务器阻断</option>
              <option value="Outbound">指定出站落地</option>
              <option value="OutboundGroup">指定出口组</option>
              <option value="ProxyChain">指定多跳代理链</option>
            </select>
          </label>

          <label v-if="form.action.type === 'Outbound'" class="text-sm">
            目标出站
            <select v-model="form.action.target_id" class="form-input-base mt-2">
              <option value="">请选择出站...</option>
              <option v-for="outbound in outbounds.filter(item => item.enabled)" :key="outbound.id" :value="outbound.id">
                {{ outbound.name }} · {{ nodes.find(node => node.id === outbound.nodeId)?.name }}
              </option>
            </select>
          </label>

          <label v-if="form.action.type === 'OutboundGroup'" class="text-sm">
            目标出口组
            <select v-model="form.action.target_id" class="form-input-base mt-2">
              <option value="">请选择出口组...</option>
              <option v-for="group in outboundGroups.filter(item => item.enabled)" :key="group.id" :value="group.id">
                {{ group.name }} · {{ nodes.find(node => node.id === group.nodeId)?.name }}
              </option>
            </select>
          </label>

          <label v-if="form.action.type === 'ProxyChain'" class="text-sm">
            目标代理链
            <select v-model="form.action.target_id" class="form-input-base mt-2">
              <option value="">请选择代理链...</option>
              <option v-for="chain in chains.filter(item => item.enabled)" :key="chain.id" :value="chain.id">
                {{ chain.name }} · {{ chain.nodeIds.length }} 个节点
              </option>
            </select>
          </label>
        </div>
      </section>

      <!-- 6. 失败容灾处理 -->
      <section v-if="!['Direct', 'Block'].includes(form.action.type)" class="panel-card p-6 space-y-4">
        <h2 class="font-semibold">失败容灾处理</h2>
        <div class="grid md:grid-cols-2 gap-4">
          <label class="block text-sm">
            目标不可用时
            <select v-model="form.action.failover" class="form-input-base mt-2">
              <option v-for="mode in ['Block', 'Direct', 'BackupOutbound', 'BackupOutboundGroup', 'BackupProxyChain']" :key="mode" :value="mode">
                {{ failureNames[mode] }}
              </option>
            </select>
            <p v-if="form.action.failover === 'Direct'" class="text-xs text-amber-700">目标不可用时将从入口直连；固定出口场景建议使用阻断或备用出口。</p>
          </label>

          <label v-if="form.action.failover?.startsWith('Backup')" class="block text-sm">
            备用目标
            <select v-model="form.action.backup_id" class="form-input-base mt-2">
              <option value="">请选择备用目标...</option>
              <option v-for="backup in backupOptions" :key="backup.id" :value="backup.id">
                {{ backup.name }}
              </option>
            </select>
          </label>
        </div>
      </section>

      <!-- 7. 审计与备注 -->
      <section class="panel-card p-6">
        <h2 class="font-semibold mb-4">记录与备注</h2>
        <div class="flex flex-wrap gap-5 text-sm">
          <label class="inline-flex items-center gap-2">
            <input v-model="form.logging.log_domain" type="checkbox" />
            <span>记录请求域名</span>
          </label>
          <label class="inline-flex items-center gap-2">
            <input v-model="form.logging.log_traffic" type="checkbox" />
            <span>记录流量统计</span>
          </label>
          <label class="inline-flex items-center gap-2">
            <input v-model="form.logging.alert_on_block" type="checkbox" />
            <span>阻断时触发告警</span>
          </label>
        </div>
        <label class="block text-sm mt-4">
          备注说明
          <textarea v-model="form.remark" maxlength="300" rows="2" placeholder="说明此策略的业务意图…" class="form-input-base mt-2"></textarea>
        </label>
      </section>

      <!-- Bottom Actions -->
      <div class="flex justify-end gap-3 pt-2">
        <router-link to="/routing/rules" class="ios-btn-secondary">
          取消
        </router-link>
        <button :disabled="saving" class="ios-btn-primary disabled:opacity-40">
          {{ saving ? '保存中…' : '保存节点策略草稿' }}
        </button>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { policyAPI, defaults, input, type PolicyInput, type ServiceSet } from '../api';
import { request, type Page } from '../../../app/apiClient';
import { authState } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { userAPI, type ProxyUser } from '../../users/api';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { type NodeView } from '../../nodes/api';
import { proxyAPI, type Outbound, type OutboundGroup, type ProxyChain, choices, failureNames } from '../../proxy/api';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const outboundGroups = ref<OutboundGroup[]>([]);
const chains = ref<ProxyChain[]>([]);
const targetOwner = computed(() =>
  form.action.type === 'Outbound'
    ? outbounds.value.find(item => item.id === form.action.target_id)?.nodeId
    : form.action.type === 'OutboundGroup'
    ? outboundGroups.value.find(item => item.id === form.action.target_id)?.nodeId
    : chains.value.find(item => item.id === form.action.target_id)?.nodeIds[0]
);

const backupOptions = computed(() =>
  form.action.failover === 'BackupOutboundGroup'
    ? outboundGroups.value.filter(item => item.enabled && item.nodeId === targetOwner.value)
    : form.action.failover === 'BackupProxyChain'
    ? chains.value.filter(item => item.enabled && item.nodeIds[0] === targetOwner.value)
    : outbounds.value.filter(item => item.enabled && item.nodeId === targetOwner.value)
);

const route = useRoute();
const router = useRouter();
const editingId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''));

const form = reactive<PolicyInput>(defaults());
const domainsText = ref('');
const suffixesText = ref('');
const cidrsText = ref('');
const portsText = ref('');
const users = ref<ProxyUser[]>([]);
const nodes = ref<NodeView[]>([]);
const groups = ref<NodeGroup[]>([]);
const services = ref<ServiceSet[]>([]);
const outbounds = ref<Outbound[]>([]);
const loading = ref(false);
const saving = ref(false);
const error = ref('');

async function load() {
  loading.value = true;
  try {
    const [u, n, g, s, o, og, pc] = await Promise.all([
      userAPI.list(1, '', 100),
      request<Page<NodeView>>('/nodes?page=1&pageSize=100'),
      groupAPI.list(1, 100),
      policyAPI.services(1, 100),
      proxyAPI.outbounds('', 1, 100),
      choices((p, s) => proxyAPI.groups('', p, s)),
      choices((p, s) => proxyAPI.chains('', p, s))
    ]);
    users.value = u.items;
    nodes.value = n.items;
    groups.value = g.items;
    services.value = s.items;
    outbounds.value = o.items;
    outboundGroups.value = og;
    chains.value = pc;

    if (editingId.value) {
      Object.assign(form, input(await policyAPI.get(editingId.value)));
    } else {
      Object.assign(form, defaults());
      form.timezone = authState.timezone;
    }

    domainsText.value = form.match.domains?.join('\n') || '';
    suffixesText.value = form.match.suffixes?.join('\n') || '';
    cidrsText.value = form.match.cidrs?.join('\n') || '';
    portsText.value = form.match.ports?.join(',') || '';
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
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
    const value: PolicyInput = JSON.parse(JSON.stringify(form));
    value.match.domains = lines(domainsText.value);
    value.match.suffixes = lines(suffixesText.value);
    value.match.cidrs = lines(cidrsText.value);
    value.match.ports = portsText.value
      .split(',')
      .map(item => item.trim())
      .filter(Boolean);

    if (value.action.type === 'Direct' || value.action.type === 'Block') {
      value.action.failover = 'Block';
      value.action.backup_id = '';
      value.action.target_id = '';
    }

    await policyAPI.save(editingId.value || null, value);
    showToast('节点策略草稿已保存，请通过 Agent 发布配置生效');
    await router.push('/routing/rules');
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
