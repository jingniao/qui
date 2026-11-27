<template>
    <FormDrawer
      v-model="editDrawer"
      title="编辑服务器"
      width="lg"
      description="修改服务器信息、续费价格、整机流量与维护状态。"
      :loading="editingSaving || fetchingLatest"
      @confirm="saveEdit"
    >
      <div class="space-y-4 text-sm" v-if="editTarget">
        <div v-if="fetchingLatest" class="flex items-center gap-2 text-xs text-ios-secondary bg-black/[0.04] rounded-xl px-3 py-2">
          <svg class="animate-spin h-3.5 w-3.5 text-ios-blue shrink-0" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
          </svg>
          <span>正在读取最新信息</span>
        </div>
        <div v-else-if="fetchError" class="text-xs text-ios-red bg-ios-red/10 border border-ios-red/20 rounded-xl px-3 py-2">
          {{ fetchError }}
        </div>

        <ServerFormFields ref="fields" :form="editForm" :connection="connectionForm" :traffic="machineForm" :known-tags="knownTags" />
      </div>
    </FormDrawer>

</template>
<script setup lang="ts">
import { ref, reactive } from 'vue';
import { nodeAPI, type MachineTrafficSettings, type NodeView } from '../api';
import { formFromSettings, payloadFromForm, type MachineTrafficForm } from '../machineTrafficModel';
import { getNodeMeta, loadNodeMeta, saveNodeMeta, parseRenewPrice } from '../nodeMeta';
import { editedNodeMeta, mergeLatestEditForm, type ServerEditFormState } from './serverListModel';
import { addressChanges } from '../nodeAddress';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import FormDrawer from '../../../components/FormDrawer.vue';
import ServerFormFields from './ServerFormFields.vue';
const fields = ref<InstanceType<typeof ServerFormFields>>();
const emit = defineEmits<{ changed: [] }>();
withDefaults(defineProps<{ knownTags?: string[] }>(), { knownTags: () => [] });
const editDrawer = ref(false);
const editingSaving = ref(false);
const fetchingLatest = ref(false);
const fetchError = ref('');
const editTarget = ref<NodeView | null>(null);
const machineForm = ref<MachineTrafficForm | null>(null);
let machineBaseline: MachineTrafficSettings | null = null;
const connectionForm = reactive({ connectHost: '', portRange: '' });
let connectionBaseline = { ...connectionForm };
const editForm = reactive<ServerEditFormState>({
  name: '',
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  enabled: true,
  allowSubscription: true,
  maintenance: false,
  autoRenew: false,
  tags: []
});
let editBaseline = { ...editForm };
let loadToken = 0;

async function openEditDrawer(node: NodeView) {
  if (editingSaving.value) return;
  if (node.deletedAt) {
    showToast('服务器已被删除', 'info');
    return;
  }
  const cachedMeta = getNodeMeta(node.id);
  const parsed = parseRenewPrice(cachedMeta.renewPrice, cachedMeta.renewCurrency, cachedMeta.renewCycle);
  editForm.name = node.name;
  editForm.region = cachedMeta.region || '';
  editForm.tags = [...(cachedMeta.tags ?? node.tags ?? [])];
  editForm.remark = cachedMeta.remark || '';
  editForm.renewDate = cachedMeta.renewDate || '';
  editForm.renewCurrency = parsed.currency;
  editForm.renewPrice = parsed.price;
  editForm.renewCycle = parsed.cycle;
  editForm.autoRenew = cachedMeta.autoRenew ?? false;
  editForm.enabled = node.enabled;
  editForm.allowSubscription = node.allowSubscription;
  editForm.maintenance = node.maintenance;
  connectionForm.connectHost = node.connectHost;
  connectionForm.portRange = cachedMeta.portRangeStart ? `${cachedMeta.portRangeStart}-${cachedMeta.portRangeEnd}` : '';
  connectionBaseline = { ...connectionForm };
  machineForm.value = null;
  machineBaseline = null;

  const baseline: ServerEditFormState = { ...editForm };
  const connectionAtOpen = { ...connectionForm };
  editTarget.value = node;
  fetchingLatest.value = true;
  fetchError.value = '';
  editDrawer.value = true;

  const currentToken = ++loadToken;
  try {
    const [latest, meta, machineSettings] = await Promise.all([
      nodeAPI.get(node.id),
      loadNodeMeta(node.id),
      nodeAPI.machineTrafficSettings(node.id).catch((e: Error) => {
        throw new Error(`读取整机流量设置失败：${e.message}`);
      })
    ]);
    if (currentToken !== loadToken || !editDrawer.value || editTarget.value?.id !== node.id) return;
    if (latest.deletedAt) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    editTarget.value = latest;
    mergeLatestEditForm(editForm, baseline, latest, meta);
    editBaseline = { ...baseline };
    mergeLatestEditForm(editBaseline, baseline, latest, meta);
    const incomingConnection = {
      connectHost: latest.connectHost,
      portRange: meta.portRangeStart ? `${meta.portRangeStart}-${meta.portRangeEnd}` : ''
    };
    for (const key of ['connectHost', 'portRange'] as const) {
      if (Object.is(connectionForm[key], connectionAtOpen[key])) {
        Object.assign(connectionForm, { [key]: incomingConnection[key] });
      }
    }
    connectionBaseline = incomingConnection;
    machineForm.value = formFromSettings(machineSettings, 'GB');
    machineBaseline = machineSettings;
  } catch (e: unknown) {
    if (currentToken !== loadToken || !editDrawer.value || editTarget.value?.id !== node.id) return;
    const isNotFound = e instanceof APIError && e.status === 404;
    if (isNotFound) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    fetchError.value = (e as Error).message || '读取最新信息失败';
  } finally {
    if (currentToken === loadToken) {
      fetchingLatest.value = false;
    }
  }
}

async function saveEdit() {
  if (!editTarget.value || editingSaving.value || fetchingLatest.value) return;
  if (!machineForm.value || !machineBaseline) {
    showToast(fetchError.value || '整机流量设置尚未读取，请重新打开弹窗', 'error');
    return;
  }
  const targetID = editTarget.value.id;
  const form: ServerEditFormState = { ...editForm, tags: [...editForm.tags] };
  const connection = { ...connectionForm };
  const trafficForm = { ...machineForm.value };
  editingSaving.value = true;
  try {
    const { portRangeStart, portRangeEnd } = fields.value!.connectionSettings();
    const range = connection.portRange.trim();
    const rangeChanged = range !== connectionBaseline.portRange;
    const traffic = payloadFromForm(trafficForm);
    const trafficChanged = traffic.method !== machineBaseline.method
      || traffic.allowanceBytes !== machineBaseline.allowanceBytes
      || traffic.resetDay !== machineBaseline.resetDay;
    const [latest, meta, latestTraffic] = await Promise.all([
      nodeAPI.get(targetID),
      loadNodeMeta(targetID),
      trafficChanged ? nodeAPI.machineTrafficSettings(targetID).catch((e: Error) => {
        throw new Error(`读取整机流量设置失败：${e.message}`);
      }) : Promise.resolve(null)
    ]);
    if (latest.deletedAt) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    // Only submit a changed client address; leave public IP and network settings untouched.
    await nodeAPI.save({
      id: targetID,
      name: form.name !== editBaseline.name ? form.name : latest.name,
      enabled: latest.enabled,
      allowSubscription: latest.allowSubscription,
      maintenance: form.maintenance !== editBaseline.maintenance ? form.maintenance : latest.maintenance,
      ...addressChanges({ connectHost: connectionBaseline.connectHost }, { connectHost: connection.connectHost })
    });

    await saveNodeMeta(targetID, {
      ...meta,
      ...(rangeChanged ? { portRangeStart, portRangeEnd } : {}),
      ...editedNodeMeta(form, editBaseline, meta)
    });

    if (latestTraffic) {
      // The quick editor does not edit the interface; preserve its latest value.
      const saved = await nodeAPI.saveMachineTrafficSettings(targetID, {
        ...traffic,
        interfaceName: latestTraffic.interfaceName
      }).catch((e: Error) => {
        throw new Error(`服务器基本信息已保存，整机流量设置保存失败：${e.message}`);
      });
      machineBaseline = saved;
    }

    showToast('服务器设置已更新');
    editDrawer.value = false;
    emit('changed');
  } catch (e: unknown) {
    const isNotFound = e instanceof APIError && e.status === 404;
    if (isNotFound) {
      editDrawer.value = false;
      showToast('服务器已被删除', 'info');
      return;
    }
    showToast((e as Error).message, 'error');
  } finally {
    editingSaving.value = false;
  }
}

defineExpose({ open: openEditDrawer });
</script>
