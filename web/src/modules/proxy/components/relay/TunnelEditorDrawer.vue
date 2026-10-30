<template>
  <FormDrawer :model-value="true" :title="drawerTitle" width="2xl" @update:model-value="close">
    <section v-if="resourceMode" class="space-y-1.5">
      <h4 class="text-[15px] font-semibold text-ios-label">共享隧道</h4>
      <label class="block space-y-1.5">
        <span class="text-xs font-medium text-ios-secondary">隧道名称（全站唯一）</span>
        <input v-model="resourceName" class="form-input-base" :disabled="saving" placeholder="例如 香港 → 日本 中转" />
      </label>
      <p class="text-xs text-ios-tertiary">
        共享隧道是独立资源：引用它的每个服务器方案共用同一份参数。编辑并保存后，所有引用方案的连线会一起更新；仍需要在各方案中发布才会下发到服务器。
      </p>
    </section>
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
      <p class="text-xs text-ios-tertiary">
        {{ resourceMode
          ? '共享隧道的两端是服务器引用，不依赖当前画布：保存后可在任意服务器方案中复用。'
          : '选择尚未在画布上的服务器，保存时会自动添加服务器框。端口转发可在下方填写自定义目标主机。' }}
      </p>
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
      <p v-for="(conflict, i) in conflicts" :key="i" class="text-xs text-ios-red">{{ conflictText(conflict) }}</p>
    </section>
    <section class="space-y-3 border-t border-ios-border pt-4">
      <div class="flex items-center justify-between gap-3">
        <h4 class="text-[15px] font-semibold text-ios-label">④ 确认</h4>
        <button v-if="!resourceMode" type="button" class="ios-btn-secondary" :disabled="scheme.executionLocation === 'client' || !!validation || checking || saving" @click="run">重新检查</button>
      </div>
      <template v-if="resourceMode">
        <p v-if="affectedText" class="text-sm" :class="affectedSchemes.length ? 'text-ios-orange' : 'text-ios-secondary'">{{ affectedText }}</p>
        <p class="text-xs text-ios-tertiary">
          资源保存不会部署；引用方案仍需各自发布。方案中的连线只记录对此隧道的引用，在这里修改参数会覆盖它们原先的内嵌参数。
        </p>
        <label v-if="canAddToScheme" class="inline-flex items-center gap-2 text-xs text-ios-secondary cursor-pointer">
          <input v-model="addToScheme" type="checkbox" :disabled="saving" />
          保存后加入当前方案「{{ scheme.name }}」
        </label>
        <p v-else class="text-xs text-ios-tertiary">当前方案为客户端执行，客户端链不支持共享隧道；保存后可在服务器方案中拖入使用。</p>
      </template>
      <template v-else>
        <p v-if="status && !checking && !error" class="text-sm" :class="status.error ? 'text-ios-red' : 'text-ios-green'">本隧道：{{ status.text }}</p>
        <p class="text-xs text-ios-tertiary">{{ scheme.executionLocation === 'client' ? '客户端链请返回画布进行本地结构检查；发布时将由服务端验证。' : '以下为包含本隧道的方案在相关服务器上的发布变更，可能包含同服务器的其他隧道。' }}</p>
        <p v-if="connection?.tunnelId" class="text-xs text-ios-blue">
          本连线引用共享隧道，参数以隧道资源为准。需要修改参数时，请在左侧「隧道」资源库中编辑该隧道。
        </p>
        <TunnelPlanSummary :plan="plan" :checking="checking" :error="error" :node-ids="affectedNodes" :tunnel-names="tunnelNames" />
      </template>
      <p v-if="saveError" class="text-sm text-ios-red" role="alert">{{ saveError }}</p>
    </section>
    <template #footer>
      <button v-if="connection" type="button" class="ios-dialog-btn-danger mr-auto" :disabled="saving" @click="confirmDelete = true">
        {{ connection.tunnelId ? '从本方案移除引用' : '删除隧道' }}
      </button>
      <button type="button" class="ios-dialog-btn-secondary" :disabled="saving" @click="close">取消</button>
      <button type="button" class="ios-dialog-btn-primary" :disabled="!!validation || checking || saving" @click="save">
        {{ saving ? '保存中…' : saveLabel }}
      </button>
    </template>
  </FormDrawer>
  <ConfirmDialog
    v-model="confirmDelete"
    :title="connection?.tunnelId ? '从本方案移除引用' : '删除隧道'"
    :message="connection?.tunnelId
      ? '只删除本方案中的这条连线，共享隧道资源和引用它的其他方案不受影响。'
      : '从方案草稿中删除此隧道？发布方案后才会移除服务器上的相关配置。'"
    @confirm="remove"
  />
</template>
<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue';
import FormDrawer from '../../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
import type { CanvasConnection, RelayScheme } from '../../proxyNodes';
import { canvasAPI, canvasTunnelAffectedText, relaySchemeToCanvasInput, type CanvasSchemeContext, type CanvasTunnel, type CanvasTunnelInput } from '../../api';
import {
  applyTunnel,
  createTunnelDraft,
  createTunnelResourceDraft,
  defaultPort,
  tunnelChoices,
  tunnelConflictText,
  tunnelResourceInput,
  tunnelStatus,
  validateTunnel,
  validateTunnelResource,
  type PlanConflict,
  type TunnelServer
} from './tunnelEditorModel';
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
  // Shared tunnel (K4) being edited, or null with createTunnel when a new resource
  // is being created. The parent owns the request and the reload after it.
  sharedTunnel?: CanvasTunnel | null;
  createTunnel?: boolean;
  // The arrow references a tunnel the page could not load; saving is refused.
  sharedMissing?: boolean;
  saveResource?: (id: string | null, input: CanvasTunnelInput) => Promise<CanvasTunnel>;
}>();

const emit = defineEmits<{ close: []; 'resource-saved': [payload: { tunnel: CanvasTunnel; addToScheme: boolean }] }>();
const resourceMode = computed(() => props.createTunnel === true || !!props.sharedTunnel);
const editingResource = computed(() => !!props.sharedTunnel);
const drawerTitle = computed(() => props.sharedTunnel ? '编辑共享隧道' : props.createTunnel ? '新建共享隧道' : props.connection ? '编辑隧道' : '新建隧道');
const saveLabel = computed(() => props.sharedTunnel ? '保存隧道' : props.createTunnel ? '创建隧道' : '保存到方案');
const draft = reactive(props.sharedTunnel ? createTunnelResourceDraft(props.sharedTunnel) : createTunnelDraft(props.scheme, props.connection, props.source, props.target));
const resourceName = ref(props.sharedTunnel?.name || '');
const addToScheme = ref(true);
const saving = ref(false), saveError = ref(''), confirmDelete = ref(false);
const sides = [{ key: 'source', label: '起点' }, { key: 'target', label: '终点' }] as const;
const kinds = [
  { value: 'encrypted', label: '加密中转', description: '保留用户身份，额度、规则与审计适用；A→B→C 形成多跳。' },
  { value: 'forward', label: '端口转发', description: '将源端口转给目标主机，不保留用户身份（L10）。' }
] as const;

// A shared tunnel names servers, so its choices are the servers themselves and do
// not depend on the drawing.
const options = computed(() => resourceMode.value
  ? props.servers.map(server => ({
      key: `server:${server.id}`,
      name: server.name,
      address: server.publicIp || '',
      status: server.status || '未知',
      core: server.capabilities?.core_name || '—'
    }))
  : tunnelChoices(props.scheme, props.servers));
const selected = (side: 'source' | 'target') => options.value.find(o => o.key === draft[side]);
function statusLabel(status?: string) {
  return ({ online: '在线', offline: '离线', untested: '未知', pending: '待注册', degraded: '降级', error: '异常', disabled: '禁用', applying: '应用中', rollback: '回滚中' } as Record<string, string>)[status || ''] || '未知';
}

const canAddToScheme = computed(() => props.createTunnel === true && !!props.scheme.id && props.scheme.executionLocation === 'server');
const affectedSchemes = computed(() => props.sharedTunnel?.schemes || []);
const affectedText = computed(() => resourceMode.value ? canvasTunnelAffectedText(affectedSchemes.value) : '');
const tunnelNames = computed<Record<string, string>>(() => (props.sharedTunnel ? { [props.sharedTunnel.id]: props.sharedTunnel.name } : {}));
function conflictText(conflict: PlanConflict) { return tunnelConflictText(conflict); }

const preview = computed(() => (resourceMode.value ? props.scheme : applyTunnel(props.scheme, draft, props.servers)));
const validation = computed(() => {
  if (resourceMode.value) return validateTunnelResource(draft, props.servers) || (resourceName.value.trim() ? '' : '请填写隧道名称');
  if (props.sharedMissing) return '共享隧道资源未载入，请刷新后重试';
  return validateTunnel(props.scheme, draft, props.servers);
});
const input = computed(() => relaySchemeToCanvasInput(preview.value, props.context));
const { plan, checking, error, run } = useTunnelPlan(
  () => JSON.stringify([props.scheme.id, props.scheme.serverUpdatedAt, input.value, validation.value, resourceMode.value]),
  () => resourceMode.value || validation.value || props.scheme.executionLocation === 'client' ? null : canvasAPI.plan(props.scheme.id, input.value.layout)
);

const conflicts = computed(() => (plan.value?.conflicts || []).filter(c => c.connectionId === draft.id));
const status = computed(() => tunnelStatus(plan.value, draft.id, props.scheme.executionLocation === 'client'));
const affectedNodes = computed(() => {
  if (resourceMode.value) return [];
  const connection = preview.value.connections.find(c => c.id === draft.id);
  if (!connection) return [];
  return preview.value.nodes
    .filter(n => [connection.sourceInstanceId, connection.targetInstanceId].includes(n.instanceId))
    .map(n => n.resourceId);
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
  if (!props.connection && !editingResource.value && newTarget) {
    const srvId = resolveServerId(newTarget);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.port = getRandomPortForNode(srvId);
      draft.targetPort = getRandomPortForNode(srvId);
    }
  }
});

watch(() => draft.source, (newSource) => {
  if (!props.connection && !editingResource.value && newSource) {
    const srvId = resolveServerId(newSource);
    const range = getNodePortRange(srvId);
    if (range) {
      draft.listenPort = getRandomPortForNode(srvId);
    }
  }
});

// Auto-pick from port ranges on initial mount for new tunnels
if (!props.connection && !resourceMode.value) {
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

// Saving a shared tunnel is a resource write, not a drawing write: the server
// re-targets every referencing scheme, so the parent reloads both after it.
async function saveResource() {
  const save = props.saveResource;
  if (!save) { saveError.value = '共享隧道接口不可用，请刷新后重试'; return; }
  saving.value = true;
  saveError.value = '';
  try {
    const value = tunnelResourceInput(draft, resourceName.value, props.sharedTunnel?.type);
    const tunnel = props.sharedTunnel
      ? await save(props.sharedTunnel.id, { ...value, updatedAt: props.sharedTunnel.updatedAt })
      : await save(null, value);
    emit('resource-saved', { tunnel, addToScheme: canAddToScheme.value && addToScheme.value });
    emit('close');
  } catch (e) {
    saveError.value = `保存失败：${(e as Error).message}`;
  } finally {
    saving.value = false;
  }
}

async function save() {
  if (validation.value) return;
  if (resourceMode.value) { await saveResource(); return; }
  await persist(preview.value);
}

// Removing an arrow that references a shared tunnel removes only the reference: the
// tunnel resource and the other schemes keep using it.
async function remove() {
  const connectionId = props.connection?.id || draft.id;
  await persist({ ...props.scheme, connections: props.scheme.connections.filter(c => c.id !== connectionId) });
}
</script>
