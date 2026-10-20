<template>
  <div class="space-y-6">
    <PageHeader :title="title" parent="分流" :description="description">
      <template #actions>
        <router-link :to="website ? '/routing/rules/new?mode=website' : '/routing/rules/new'" class="ios-btn-primary">创建策略</router-link>
        <router-link to="/routing/simulator" class="ios-btn-secondary">策略模拟</router-link>
      </template>
    </PageHeader>

    <div class="p-3.5 rounded-2xl bg-ios-blue/[0.08] border border-ios-blue/15 text-ios-blue text-xs leading-relaxed">
      优先级数字越小越先执行。修改只保存草稿，生成并发布节点配置后生效；分流与网站控制共用策略。
    </div>

    <form @submit.prevent="search" class="flex flex-wrap items-center gap-3">
      <div class="relative flex-1 max-w-md">
        <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <input
          v-model="keyword"
          placeholder="搜索策略名称…"
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
    <LoadingSkeleton v-else-if="loading && !rules.length" />
    <EmptyState v-else-if="!rules.length" title="暂无策略" description="未命中策略时使用每个入站的默认出站。" />
    <DataTable v-else>
      <template #header>
        <th>优先级 / 名称</th>
        <th>生效范围</th>
        <th>匹配条件</th>
        <th>动作</th>
        <th>状态</th>
        <th>操作</th>
      </template>
      <template #body>
        <tr v-for="rule in rules" :key="rule.id">
          <td>
            <span class="font-mono tabular-nums text-xs text-ios-tertiary mr-2">#{{ rule.priority }}</span>
            <router-link :to="`/routing/rules/${rule.id}/edit`" class="text-ios-blue font-semibold hover:underline text-[15px]">
              {{ rule.name }}
            </router-link>
          </td>
          <td class="text-xs text-ios-secondary">{{ scopeSummary(rule) }}</td>
          <td class="text-xs max-w-64 text-ios-secondary">
            <p>{{ matchSummary(rule) }}</p>
            <p v-if="rule.match.time_range" class="text-ios-tertiary mt-0.5">{{ rule.match.time_range }} · {{ rule.timezone }}</p>
          </td>
          <td class="text-xs">
            <span class="font-medium text-ios-label">{{ actionNames[rule.action.type] }}</span>
            <p v-if="rule.action.target_id" class="text-xs text-ios-tertiary mt-0.5">{{ targetName(rule.action.target_id) }}</p>
          </td>
          <td><StatusBadge :status="rule.enabled ? 'active' : 'disabled'" /></td>
          <td class="space-x-2">
            <router-link :to="`/routing/rules/${rule.id}/edit`" class="ios-action-btn-primary">编辑</router-link>
            <button @click="prompt(rule, 'status')" class="ios-action-btn-secondary">
              {{ rule.enabled ? '停用' : '启用' }}
            </button>
            <button @click="prompt(rule, 'delete')" class="ios-action-btn-danger">删除</button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <ConfirmDialog
      v-model="confirmVisible"
      title="确认策略草稿修改"
      :message="`对策略 ${target?.name || ''} 执行${operation === 'delete' ? '删除' : '启用状态切换'}？节点运行状态会在下一次配置发布后更新。`"
      @confirm="execute"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue';
import { policyAPI, input, actionNames, type PolicyRecord, type ServiceSet } from '../api';
import { proxyAPI, type Outbound, type OutboundGroup, type ProxyChain, choices } from '../../proxy/api';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

withDefaults(defineProps<{ title?: string; description?: string; website?: boolean }>(), { title: '分流策略', description: '按用户、设备、节点、地址、网络和时间范围执行统一策略。', website: false });
const outboundGroups = ref<OutboundGroup[]>([]); const chains = ref<ProxyChain[]>([]); const rules = ref<PolicyRecord[]>([]); const services = ref<ServiceSet[]>([]); const outbounds = ref<Outbound[]>([]); const total = ref(0); const page = ref(1); const keyword = ref(''); const error = ref(''); const loading = ref(false); const target = ref<PolicyRecord | null>(null); const operation = ref(''); const confirmVisible = ref(false);
function targetName(id: string) { return outbounds.value.find(item => item.id === id)?.name || outboundGroups.value.find(item => item.id === id)?.name || chains.value.find(item => item.id === id)?.name || id; }
function scopeSummary(rule: PolicyRecord) { const parts = [[rule.scope.user_ids?.length, '用户'], [rule.scope.device_ids?.length, '设备'], [rule.scope.node_ids?.length, '节点'], [rule.scope.group_ids?.length, '节点组']].filter(item => item[0]).map(item => item[0] + ' ' + item[1]); return parts.join('、') || '全部范围'; }
function matchSummary(rule: PolicyRecord) { const addresses = [...(rule.match.domains || []), ...(rule.match.suffixes || []).map(value => '.' + value), ...(rule.match.cidrs || []), ...(rule.match.service_ids || []).map(id => services.value.find(service => service.id === id)?.name || id)]; return (addresses.slice(0, 4).join('、') || '全部地址') + (addresses.length > 4 ? ' …' : '') + ' · ' + (rule.match.network || 'all') + (rule.match.ports?.length ? ' · 端口 ' + rule.match.ports.join(',') : ''); }
async function load() { loading.value = true; try { const [result, sets, outs, gs, cs] = await Promise.all([policyAPI.list(page.value, keyword.value), policyAPI.services(1, 100), proxyAPI.outbounds('', 1, 100), choices((p,s) => proxyAPI.groups('',p,s)), choices((p,s) => proxyAPI.chains('',p,s))]); rules.value = result.items; total.value = result.total; services.value = sets.items; outbounds.value = outs.items; outboundGroups.value = gs; chains.value = cs; error.value = ''; } catch (e) { error.value = (e as Error).message; } finally { loading.value = false; } }
function search() { page.value = 1; load(); } function changePage(value: number) { page.value = value; load(); } function prompt(rule: PolicyRecord, action: string) { target.value = rule; operation.value = action; confirmVisible.value = true; }
async function execute() { if (!target.value) return; try { if (operation.value === 'delete') await policyAPI.delete(target.value.id); else await policyAPI.save(target.value.id, { ...input(target.value), enabled: !target.value.enabled }); showToast('策略草稿已更新'); await load(); } catch (e) { showToast((e as Error).message, 'error'); } } onMounted(load);
</script>
