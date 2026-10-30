<template>
  <FormDrawer :model-value="true" :title="connection ? '编辑隧道' : '新建隧道'" width="2xl" @update:model-value="close">
    <section class="space-y-3">
      <h4 class="text-[15px] font-semibold text-ios-label">① 起点与终点</h4>
      <div class="grid sm:grid-cols-2 gap-4">
        <label v-for="side in sides" :key="side.key" class="block space-y-1.5">
          <span class="text-xs font-medium text-ios-secondary">{{ side.label }}</span>
          <select v-model="draft[side.key]" class="form-input-base" :disabled="saving">
            <option value="">请选择服务器 / 画布节点</option>
            <option v-for="choice in options" :key="choice.key" :value="choice.key">{{ choice.name }}</option>
          </select>
          <span v-if="selected(side.key)" class="block text-xs text-ios-tertiary font-mono tabular-nums break-all">
            {{ statusLabel(selected(side.key)?.status) }} · {{ selected(side.key)?.address || '无公网 IP' }} · {{ selected(side.key)?.core }}
          </span>
        </label>
      </div>
      <p class="text-xs text-ios-tertiary">选择尚未在画布上的服务器，保存时会自动添加服务器框。端口转发可在下方填写自定义目标主机。</p>
    </section>
    <section class="space-y-3">
      <h4 class="text-[15px] font-semibold text-ios-label">② 类型</h4>
      <div class="grid sm:grid-cols-2 gap-3">
        <button v-for="kind in kinds" :key="kind.value" type="button" :disabled="saving"
          class="rounded-xl border p-3 text-left active:scale-[0.98] transition-transform"
          :class="draft.kind === kind.value ? 'border-ios-blue bg-ios-blue/10' : 'border-ios-border bg-ios-card'"
          :aria-pressed="draft.kind === kind.value" @click="draft.kind = kind.value">
          <span class="block text-sm font-semibold text-ios-label">{{ kind.label }}</span>
          <span class="block text-xs text-ios-secondary mt-1">{{ kind.description }}</span>
        </button>
      </div>
    </section>
    <section class="space-y-3">
      <h4 class="text-[15px] font-semibold text-ios-label">③ 参数</h4>
      <div v-if="draft.kind === 'encrypted'" class="grid sm:grid-cols-2 gap-4">
        <label class="block space-y-1.5">
          <span class="text-xs font-medium text-ios-secondary">隧道协议</span>
          <select v-model="draft.protocol" class="form-input-base" :disabled="saving" @change="onProtocolChange">
            <option value="tls">TLS（内部 Trojan）</option>
            <option value="wireguard">WireGuard</option>
            <option v-for="protocol in ['gost', 'brook', 'nftables']" :key="protocol" :value="protocol" disabled>{{ protocol }} — 需要 GOST 核心（后续阶段）</option>
          </select>
        </label>
        <label class="block space-y-1.5">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-ios-secondary">目标服务器中转端口</span>
            <button type="button" @click="pickTunnelPort" class="text-ios-blue text-[11px] hover:underline">随机端口</button>
          </div>
          <input v-model.number="draft.port" type="number" min="1" max="65535" class="form-input-base font-mono tabular-nums" :disabled="saving" />
          <span v-if="getNodePortRange(resolveServerId(draft.target))" class="text-[10px] text-ios-blue font-mono block">
            服务器分配范围: {{ getNodePortRange(resolveServerId(draft.target))?.start }} - {{ getNodePortRange(resolveServerId(draft.target))?.end }}
          </span>
        </label>
      </div>
      <div v-else class="space-y-3">
        <label class="block space-y-1.5">
          <span class="text-xs font-medium text-ios-secondary">目标主机（留空使用目标服务器公网 IP）</span>
          <input v-model="draft.targetHost" :placeholder="selected('target')?.address || '目标公网 IP'" class="form-input-base font-mono" :disabled="saving" />
        </label>
        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block space-y-1.5">
            <div class="flex items-center justify-between">
              <span class="text-xs font-medium text-ios-secondary">源服务器监听端口</span>
              <button type="button" @click="pickListenPort" class="text-ios-blue text-[11px] hover:underline">随机端口</button>
            </div>
            <input v-model.number="draft.listenPort" type="number" min="1" max="65535" class="form-input-base font-mono tabular-nums" :disabled="saving" />
            <span v-if="getNodePortRange(resolveServerId(draft.source))" class="text-[10px] text-ios-blue font-mono block">
              服务器分配范围: {{ getNodePortRange(resolveServerId(draft.source))?.start }} - {{ getNodePortRange(resolveServerId(draft.source))?.end }}
            </span>
          </label>
          <label class="block space-y-1.5">
            <div class="flex items-center justify-between">
              <span class="text-xs font-medium text-ios-secondary">目标端口</span>
              <button type="button" @click="pickTargetPort" class="text-ios-blue text-[11px] hover:underline">随机端口</button>
            </div>
            <input v-model.number="draft.targetPort" type="number" min="1" max="65535" class="form-input-base font-mono tabular-nums" :disabled="saving" />
            <span v-if="getNodePortRange(resolveServerId(draft.target))" class="text-[10px] text-ios-blue font-mono block">
              服务器分配范围: {{ getNodePortRange(resolveServerId(draft.target))?.start }} - {{ getNodePortRange(resolveServerId(draft.target))?.end }}
            </span>
          </label>
        </div>
        <p class="text-xs text-ios-secondary">L10：核心端口转发不验证用户身份，用户额度、设备限制、规则与用户访问审计不适用。</p>
      </div>
      <div class="flex gap-4 text-sm text-ios-secondary">
        <label class="inline-flex items-center gap-2 cursor-pointer"><input v-model="draft.tcp" type="checkbox" :disabled="saving" />TCP</label>
        <label class="inline-flex items-center gap-2 cursor-pointer"><input v-model="draft.udp" type="checkbox" :disabled="saving" />UDP</label>
      </div>
      <p v-if="connection?.invalidReason" class="text-xs text-ios-red">原连线：{{ connection.invalidReason }}</p>
      <p v-if="validation" class="text-xs text-ios-red">{{ validation }}</p>
      <p v-for="(conflict, i) in conflicts" :key="i" class="text-xs text-ios-red">端口冲突 :{{ conflict.port }}（{{ conflict.with }}）</p>
    </section>
    <section class="space-y-3 border-t border-ios-border pt-4">
      <div class="flex items-center justify-between gap-3">
        <h4 class="text-[15px] font-semibold text-ios-label">④ 确认</h4>
        <button type="button" class="ios-btn-secondary" :disabled="scheme.executionLocation === 'client' || !!validation || checking || saving" @click="run">重新检查</button>
      </div>
      <p v-if="status && !checking && !error" class="text-sm" :class="status.error ? 'text-ios-red' : 'text-ios-green'">本隧道：{{ status.text }}</p>
      <p class="text-xs text-ios-tertiary">{{ scheme.executionLocation === 'client' ? '客户端链请返回画布进行本地结构检查；发布时将由服务端验证。' : '以下为包含本隧道的方案在相关服务器上的发布变更，可能包含同服务器的其他隧道。' }}</p>
      <TunnelPlanSummary :plan="plan" :checking="checking" :error="error" :node-ids="affectedNodes" />
      <p v-if="saveError" class="text-sm text-ios-red" role="alert">{{ saveError }}</p>
    </section>
    <template #footer>
      <button v-if="connection" type="button" class="ios-dialog-btn-danger mr-auto" :disabled="saving" @click="confirmDelete = true">删除隧道</button>
      <button type="button" class="ios-dialog-btn-secondary" :disabled="saving" @click="close">取消</button>
      <button type="button" class="ios-dialog-btn-primary" :disabled="!!validation || checking || saving" @click="save">{{ saving ? '保存中…' : '保存到方案' }}</button>
    </template>
  </FormDrawer>
  <ConfirmDialog v-model="confirmDelete" title="删除隧道" message="从方案草稿中删除此隧道？发布方案后才会移除服务器上的相关配置。" @confirm="remove" />
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import FormDrawer from '../../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import type { CanvasConnection, RelayScheme } from '../../proxyNodes';
import { canvasAPI, relaySchemeToCanvasInput, type CanvasSchemeContext } from '../../api';
import { applyTunnel, createTunnelDraft, defaultPort, tunnelChoices, tunnelStatus, validateTunnel, type TunnelServer } from './tunnelEditorModel';
import { getNodePortRange, getRandomPortForNode } from '../../../nodes/nodeMeta';
import TunnelPlanSummary from './TunnelPlanSummary.vue';
import { useTunnelPlan } from './useTunnelPlan';

const props = defineProps<{
  scheme: RelayScheme;
  servers: TunnelServer[];
  context: CanvasSchemeContext;
  connection?: CanvasConnection;
  source?: string;
  target?: string;
  persist: (scheme: RelayScheme) => Promise<void>;
}>();

const emit = defineEmits<{ close: [] }>();
const draft = reactive(createTunnelDraft(props.scheme, props.connection, props.source, props.target));
const saving = ref(false), saveError = ref(''), confirmDelete = ref(false);
const sides = [{ key: 'source', label: '起点' }, { key: 'target', label: '终点' }] as const;
const kinds = [
  { value: 'encrypted', label: '加密中转', description: '保留用户身份，额度、规则与审计适用；A→B→C 形成多跳。' },
  { value: 'forward', label: '端口转发', description: '将源端口转给目标主机，不保留用户身份（L10）。' }
] as const;

const options = computed(() => tunnelChoices(props.scheme, props.servers));
const selected = (side: 'source' | 'target') => options.value.find(o => o.key === draft[side]);
function statusLabel(status?: string) {
  return ({ online: '在线', offline: '离线', untested: '未知', pending: '待注册', degraded: '降级', error: '异常', disabled: '禁用', applying: '应用中', rollback: '回滚中' } as Record<string, string>)[status || ''] || '未知';
}

const preview = computed(() => applyTunnel(props.scheme, draft, props.servers));
const validation = computed(() => validateTunnel(props.scheme, draft, props.servers));
const input = computed(() => relaySchemeToCanvasInput(preview.value, props.context));
const { plan, checking, error, run } = useTunnelPlan(
  () => JSON.stringify([props.scheme.id, props.scheme.serverUpdatedAt, input.value, validation.value]),
  () => validation.value || props.scheme.executionLocation === 'client' ? null : canvasAPI.plan(props.scheme.id, input.value.layout)
);

const conflicts = computed(() => (plan.value?.conflicts || []).filter(c => c.connectionId === draft.id));
const status = computed(() => tunnelStatus(plan.value, draft.id, props.scheme.executionLocation === 'client'));
const affectedNodes = computed(() => {
  const c = preview.value.connections.find(c => c.id === draft.id)!;
  return preview.value.nodes.filter(n => [c.sourceInstanceId, c.targetInstanceId].includes(n.instanceId)).map(n => n.resourceId);
});

function resolveServerId(key: string): string {
  if (!key) return '';
  if (key.startsWith('server:')) return key.replace('server:', '');
  const node = props.scheme.nodes.find(n => n.instanceId === key);
  return node?.resourceType === 'server' ? node.resourceId : '';
}

function onProtocolChange() {
  const srvId = resolveServerId(draft.target);
  const range = getNodePortRange(srvId);
  draft.port = range ? getRandomPortForNode(srvId) : defaultPort(draft.protocol);
}

function pickTunnelPort() {
  const srvId = resolveServerId(draft.target);
  draft.port = getRandomPortForNode(srvId);
}

function pickListenPort() {
  const srvId = resolveServerId(draft.source);
  draft.listenPort = getRandomPortForNode(srvId);
}

function pickTargetPort() {
  const srvId = resolveServerId(draft.target);
  draft.targetPort = getRandomPortForNode(srvId);
}

watch(() => draft.target, (newTarget) => {
  if (!props.connection && newTarget) {
    const srvId = resolveServerId(newTarget);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.port = getRandomPortForNode(srvId);
      draft.targetPort = getRandomPortForNode(srvId);
    }
  }
});

watch(() => draft.source, (newSource) => {
  if (!props.connection && newSource) {
    const srvId = resolveServerId(newSource);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.listenPort = getRandomPortForNode(srvId);
    }
  }
});

// Auto-pick from port ranges on initial mount for new tunnels
if (!props.connection) {
  if (draft.target) {
    const srvId = resolveServerId(draft.target);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.port = getRandomPortForNode(srvId);
      draft.targetPort = getRandomPortForNode(srvId);
    }
  }
  if (draft.source) {
    const srvId = resolveServerId(draft.source);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.listenPort = getRandomPortForNode(srvId);
    }
  }
}

function close() { if (!saving.value) emit('close'); }
async function persist(value: RelayScheme) {
  saving.value = true;
  saveError.value = '';
  try { await props.persist(value); emit('close'); }
  catch (e) { saveError.value = `保存失败：${(e as Error).message}`; }
  finally { saving.value = false; }
}
async function save() { if (!validation.value) await persist(preview.value); }
async function remove() { await persist({ ...props.scheme, connections: props.scheme.connections.filter(c => c.id !== draft.id) }); }
</script>
