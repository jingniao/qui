<template>
  <AppDialog :model-value="true" title="遗留资源" max-width="3xl" @update:model-value="close">
    <div class="space-y-5 text-sm text-ios-label">
      <p>重新挂接只建立方案草稿；完整撤除会先建立草稿，再打开现有撤除影响预览。</p>
      <p v-if="error" role="alert" class="text-ios-red whitespace-pre-wrap">{{ error }}</p>
      <section v-for="section in sections" :key="section.kind" class="space-y-3">
        <h4 class="text-base font-semibold leading-normal">{{ section.label }}（{{ section.items.length }}）</h4>
        <template v-if="section.kind !== 'unowned'">
          <div v-for="group in section.items" :key="group.id" class="rounded-xl border border-ios-border p-4 space-y-3 break-words">
            <h5 class="text-lg font-bold">{{ group.bindings[0]?.name || group.tunnels[0]?.name || '遗留连通组' }}</h5>
            <div v-for="binding in group.bindings" :key="binding.id" class="space-y-1">
              <p>入口：{{ binding.name }} · {{ binding.nodeName || nodeName(binding.nodeId) }} · {{ binding.mode === 'create' ? '独立入口' : '复用入口' }}</p>
              <p class="text-ios-secondary">原出站：{{ routeName(binding.originalDefaultOutbound) }}；当前出站：{{ routeName(binding.currentDefaultOutbound) }}</p>
            </div>
            <div v-for="tunnel in group.tunnels" :key="tunnel.id" class="space-y-1">
              <p class="font-semibold">通道：{{ tunnel.name }}</p>
              <p class="text-ios-secondary">{{ endpointName(tunnel.source) }} → {{ endpointName(tunnel.target) }}</p>
              <p v-for="object in tunnel.objects" :key="`${object.kind}:${object.id}`">{{ objectKinds[object.kind] }}：{{ object.name }} · {{ nodeName(object.nodeId) }}<span v-if="object.port" class="font-mono tabular-nums"> · {{ object.port }}</span></p>
            </div>
            <p v-if="group.referrers.length" class="text-ios-orange">引用者：{{ legacyReferrersText(group.referrers) }}</p>
            <p v-for="reason in legacyResourceActions(group).reasons" :key="reason" class="text-ios-secondary">{{ reason }}</p>
            <button v-if="legacyEmptyTunnelToDelete(group)" type="button" class="ios-btn-danger" :disabled="busy" @click="deleteEmptyTunnel(group)">清除空通道</button>
            <template v-if="legacyResourceActions(group).showReattach">
              <label class="block space-y-1"><span>新方案名称</span><input v-model="names[group.id]" class="ios-input w-full" :disabled="busy" placeholder="请输入新方案名称" /></label>
              <div class="flex flex-wrap gap-2">
                <button type="button" class="ios-btn-primary" :disabled="busy" @click="reattach(group, false)">重新挂接为方案</button>
                <button v-if="legacyResourceActions(group).showRemove" type="button" class="ios-btn-danger" :disabled="busy" @click="reattach(group, true)">完整撤除</button>
              </div>
            </template>
          </div>
        </template>
        <template v-else>
          <div v-for="object in section.items" :key="`${object.kind}:${object.id}`" class="rounded-xl border border-ios-border p-4 space-y-3 break-words">
            <h5 class="text-lg font-bold">{{ object.name }}</h5>
            <p>{{ objectKinds[object.kind] }} · {{ nodeName(object.nodeId) }}<span v-if="object.port" class="font-mono tabular-nums"> · {{ object.port }}</span></p>
            <p class="text-ios-orange">归属待核实；请核实对象与目标通道后显式认领。</p>
            <template v-if="object.candidateTunnels?.length">
              <label class="block space-y-1"><span>认领到通道</span><select v-model="adoptTargets[`${object.kind}:${object.id}`]" class="ios-input w-full" :disabled="busy"><option value="">请选择通道</option><option v-for="id in object.candidateTunnels" :key="id" :value="id">{{ tunnelName(id) }}</option></select></label>
              <button type="button" class="ios-btn-secondary" :disabled="busy" @click="adopt(object)">确认认领</button>
            </template>
            <p v-else class="text-ios-secondary">没有可认领的兼容通道</p>
          </div>
        </template>
      </section>
    </div>
    <template #footer><button type="button" class="ios-dialog-btn-secondary" :disabled="busy" @click="close">关闭</button></template>
  </AppDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import AppDialog from '../../../../components/AppDialog.vue';
import { APIError } from '../../../../app/apiClient';
import { canvasLegacyAPI, canvasTunnelAPI, type CanvasLegacyResources, type CanvasLegacyGroup, type CanvasTunnelObject, type Inbound, type Outbound } from '../../api';
import { legacyResourceActions, legacyResourceSections, legacyReferrersText, legacyEmptyTunnelToDelete } from '../../legacyResources';

const props = defineProps<{ resources: CanvasLegacyResources; servers: { id: string; name: string }[]; inbounds: Inbound[]; outbounds: Outbound[] }>();
const emit = defineEmits<{ close: []; changed: []; reattached: [schemeId: string, remove: boolean] }>();
const sections = computed(() => legacyResourceSections(props.resources).filter(section => section.items.length));
const names = reactive<Record<string, string>>({});
const adoptTargets = reactive<Record<string, string>>({});
const busy = ref(false);
const error = ref('');
const objectKinds = { inbound: '内部监听', outbound: '出站', chain: '代理链' };
function nodeName(id: string) { return props.servers.find(node => node.id === id)?.name || id; }
function routeName(route: string) { return route === 'direct' ? '直连' : route === 'block' ? '阻断' : props.outbounds.find(item => item.id === route)?.name || route || '未登记'; }
function endpointName(ref: string) {
  if (ref.startsWith('server:')) return nodeName(ref.slice(7));
  const item = ref.startsWith('inbound:') ? props.inbounds.find(item => item.id === ref.slice(8)) : ref.startsWith('outbound:') ? props.outbounds.find(item => item.id === ref.slice(9)) : undefined;
  return item ? `${nodeName(item.nodeId)} · ${item.name}` : ref;
}
function tunnelName(id: string) { return [...props.resources.detached, ...props.resources.orphanTunnels].flatMap(group => group.tunnels).find(tunnel => tunnel.id === id)?.name || id; }
function close() { if (!busy.value) emit('close'); }
function showError(cause: unknown) {
  const details = cause instanceof APIError && cause.code === 'canvas_legacy_conflict' ? cause.details as { reasons: string[] } : null;
  error.value = details ? details.reasons.join('\n') : (cause as Error).message;
}
async function reattach(group: CanvasLegacyGroup, remove: boolean) {
  if (busy.value) return;
  const name = names[group.id]?.trim();
  if (!name) { error.value = '请输入新方案名称'; return; }
  busy.value = true;
  error.value = '';
  try {
    const result = await canvasLegacyAPI.reattach({ bindingIds: group.bindingIds, tunnelIds: group.tunnelIds, name });
    emit('reattached', result.schemeId, remove);
  } catch (cause) { showError(cause); }
  finally { busy.value = false; }
}
async function adopt(object: CanvasTunnelObject) {
  if (busy.value) return;
  const target = adoptTargets[`${object.kind}:${object.id}`];
  if (!target) { error.value = '请选择认领到的通道'; return; }
  busy.value = true;
  error.value = '';
  try { await canvasTunnelAPI.adopt(target, [{ kind: object.kind, id: object.id }]); emit('changed'); }
  catch (cause) { showError(cause); }
  finally { busy.value = false; }
}
async function deleteEmptyTunnel(group: CanvasLegacyGroup) {
  const id = legacyEmptyTunnelToDelete(group);
  if (busy.value || !id) return;
  busy.value = true;
  error.value = '';
  try { await canvasTunnelAPI.delete(id); emit('changed'); }
  catch (cause) { showError(cause); }
  finally { busy.value = false; }
}
</script>
