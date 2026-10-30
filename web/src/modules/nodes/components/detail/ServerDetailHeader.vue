<template>
  <section class="panel-card p-5 space-y-3 sticky top-0 z-10">
    <div class="flex flex-wrap justify-between gap-3">
      <div>
        <div class="flex items-center gap-3">
          <h1 class="text-xl font-semibold text-ios-label">{{ node.name }}</h1>
          <StatusBadge :status="status.tone" :label="status.label" />
        </div>
        <p class="mt-2 text-sm text-ios-secondary">
          {{ meta.region || '—' }}
          ·
          <span class="font-mono tabular-nums">{{ node.publicIp || '—' }}</span>
        </p>
      </div>
      <div v-if="isOwner && !node.deletedAt" class="flex flex-wrap items-center gap-2">
        <button class="ios-btn-primary" @click="$emit('publish')">发布配置</button>
        <button class="ios-btn-danger" :disabled="!restartEligible" @click="capturedRevision = node.heartbeat.current_revision ?? 0; restartOpen = true">重启核心</button>
        <details class="relative">
          <summary class="ios-btn-secondary cursor-pointer">更多</summary>
          <div class="absolute right-0 mt-2 p-3 panel-card space-y-2 w-48 z-20">
            <button class="ios-btn-secondary w-full" @click="$emit('programs')">核心更新</button>
            <button class="ios-btn-secondary w-full" @click="$emit('install')">安装命令</button>
            <button class="ios-btn-danger w-full" @click="$emit('delete')">删除服务器</button>
          </div>
        </details>
      </div>
    </div>
    <p class="text-xs text-ios-secondary leading-relaxed">核心 {{ node.capabilities?.core_name || '—' }} {{ node.capabilities?.core_version || '—' }} · {{ health }} · Agent {{ node.agentVersion || '—' }} · 当前配置 {{ revision }}{{ latest }} · 心跳 {{ relativeHeartbeat(node.lastHeartbeat, now) }}</p>
    <p class="text-xs text-ios-secondary">接入：{{ node.deletedAt ? '已删除' : !node.enabled ? '已禁用' : node.maintenance ? '维护中（暂不分发）' : !node.allowSubscription ? '不分发订阅' : '允许订阅' }}</p>
    <div class="flex flex-wrap items-center gap-2 text-xs text-ios-secondary">
      <span>线路组：</span>
      <template v-for="group in memberships" :key="group.id">
        <router-link v-if="isOwner" :to="{ path: '/proxy/groups', query: { group: group.id } }" class="rounded-full bg-ios-blue/10 px-3 py-1 text-ios-blue">{{ group.name }}{{ group.enabled ? '' : '（已停用）' }}</router-link>
        <span v-else class="rounded-full bg-ios-bg px-3 py-1">{{ group.name }}</span>
      </template>
      <span v-if="!memberships.length">{{ isOwner ? '未加入线路组' : '当前权限无法读取线路组' }}</span>
      <button v-if="isOwner && !node.deletedAt" class="ios-btn-secondary" @click="openGroups">编辑</button>
      <span class="ml-auto font-mono tabular-nums">续费：{{ meta.renewDate || '—' }}{{ meta.renewDate ? `（${calculateRemainingDays(meta.renewDate).label}）` : '' }}</span>
    </div>
    <p v-if="node.heartbeat?.core_healthy === false" role="status" class="text-sm text-ios-red">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '代理已停止，流量消费需要核对；已采集流量继续补传。' : '最后心跳报告核心未通过健康检查，请检查程序与配置发布结果。' }}</p>
    <FormDrawer v-model="groupsOpen" title="编辑所属线路组" :loading="saving" @confirm="saveGroups">
      <label v-for="group in editableGroups" :key="group.id" class="flex items-center gap-2 text-sm text-ios-secondary cursor-pointer">
        <input v-model="selected" type="checkbox" :value="group.id" />
        {{ group.name }}
        ·
        {{ groupPurposeNames[group.purpose] }}
      </label>
      <p v-if="groupsLoaded && !editableGroups.length" class="text-sm text-ios-secondary">尚无线路组，请先创建线路组。</p>
      <button v-if="groupError && !groupsLoaded" class="ios-btn-secondary" @click="openGroups">重新读取</button>
      <p v-if="groupError" role="alert" class="text-sm text-ios-red">{{ groupError }}</p>
    </FormDrawer>
    <ConfirmDialog v-model="restartOpen" title="重启当前代理核心" :message="`服务器 ${node.name}，配置 #${capturedRevision}。现有连接会中断，保留当前成功配置与授权账本。`" @confirm="restart" />
    <p v-if="restartError" role="alert" class="text-xs text-ios-red">{{ restartError }}</p>
  </section>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { nodeAPI, type NodeView } from '../../api';
import { groupAPI, type NodeGroup } from '../../groupsApi';
import { groupInput, groupPurposeNames } from '../../../proxy/lineGroups';
import { choices } from '../../../proxy/api';
import { type NodeMeta, calculateRemainingDays } from '../../nodeMeta';
import { serverStatus, relativeHeartbeat } from '../serverListModel';
import { isOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import StatusBadge from '../../../../components/StatusBadge.vue';
import FormDrawer from '../../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
const props = defineProps<{ node: NodeView; groups: NodeGroup[]; meta: NodeMeta; now: number; latestRevision?: number }>();
const emit = defineEmits<{ publish: []; programs: []; install: []; delete: []; changed: [] }>();
const status = computed(() => serverStatus(props.node, props.now));
const fresh = computed(() => { const age = props.now - Date.parse(props.node.lastHeartbeat); return age >= -5000 && age < 45000; });
const health = computed(() => !fresh.value ? '—' : props.node.heartbeat?.core_healthy === true ? '健康' : props.node.heartbeat?.core_healthy === false ? props.node.heartbeat.core_error === 'accounting_interrupted' ? '记账中断' : '核心异常' : '—');
const revision = computed(() => props.node.heartbeat?.current_revision ? `#${props.node.heartbeat.current_revision}` : '—');
const latest = computed(() => props.latestRevision && props.node.heartbeat?.current_revision ? props.latestRevision === props.node.heartbeat.current_revision ? '（已是最新）' : '（存在更新版本）' : '');
const memberships = computed(() => props.groups.filter(group => group.nodeIds.includes(props.node.id)));
const groupsOpen = ref(false), saving = ref(false), groupsLoaded = ref(false), groupError = ref(''), selected = ref<string[]>([]), editableGroups = ref<NodeGroup[]>([]);
async function openGroups() {
  groupError.value = ''; groupsOpen.value = true; saving.value = true; groupsLoaded.value = false;
  try { editableGroups.value = await choices(groupAPI.list); selected.value = editableGroups.value.filter(g => g.nodeIds.includes(props.node.id)).map(g => g.id); groupsLoaded.value = true; }
  catch (e) { groupError.value = (e as Error).message; }
  finally { saving.value = false; }
}
async function saveGroups() {
  if (saving.value || !groupsLoaded.value) return;
  saving.value = true; groupError.value = '';
  try {
    const latest = await choices(groupAPI.list);
    for (const group of latest) {
      const before = editableGroups.value.find(item => item.id === group.id);
      if (!before || before.nodeIds.includes(props.node.id) === selected.value.includes(group.id)) continue;
      const ids = group.nodeIds.filter(id => id !== props.node.id);
      if (selected.value.includes(group.id)) ids.push(props.node.id);
      await groupAPI.save(group.id, groupInput(group, ids));
    }
    groupsOpen.value = false; emit('changed');
  } catch (e) { groupError.value = (e as Error).message; emit('changed'); }
  finally { saving.value = false; }
}
const restartOpen = ref(false), restartError = ref(''), restarting = ref(false), capturedRevision = ref(0);
const restartEligible = computed(() => !restarting.value && fresh.value && props.node.enabled && ['online', 'degraded'].includes(props.node.status) && (props.node.heartbeat?.current_revision ?? 0) > 0);
async function restart() {
  if (!restartEligible.value) return;
  restarting.value = true; restartError.value = '';
  try { await nodeAPI.restartCore(props.node.id, capturedRevision.value); showToast('重启任务已受理，请在设置的程序区域查看结果'); emit('changed'); }
  catch (e) { restartError.value = (e as Error).message; }
  finally { restarting.value = false; }
}
</script>
