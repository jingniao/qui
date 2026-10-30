<template>
  <div v-if="isOwner && !node.deletedAt" class="space-y-6">
    <form class="panel-card p-6 space-y-4" @submit.prevent="save('basic')">
      <h2 class="font-semibold text-ios-label">基本信息与续费</h2>
      <div class="grid sm:grid-cols-2 gap-4 text-xs text-ios-secondary">
        <label>
          服务器名称
          <input v-model.trim="form.name" required maxlength="40" class="form-input-base mt-1.5" />
        </label>
        <label>
          订阅接入 IP / 域名
          <input v-model.trim="form.publicIp" class="form-input-base mt-1.5" />
        </label>
        <label>
          地区
          <input v-model.trim="metaForm.region" class="form-input-base mt-1.5" />
        </label>
        <label>
          备注
          <input v-model.trim="metaForm.remark" class="form-input-base mt-1.5" />
        </label>
        <label>
          续费日期
          <input v-model="metaForm.renewDate" type="date" class="form-input-base mt-1.5" />
        </label>
        <label>
          提前提醒天数
          <input v-model.number="metaForm.renewNoticeDays" type="number" min="0" max="60" class="form-input-base mt-1.5" />
        </label>
        <label>
          币种
          <select v-model="metaForm.renewCurrency" class="form-input-base mt-1.5">
            <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
          </select>
        </label>
        <label>
          续费价格
          <input v-model="metaForm.renewPrice" type="number" min="0" step="any" class="form-input-base mt-1.5" />
        </label>
        <label>
          周期
          <select v-model="metaForm.renewCycle" class="form-input-base mt-1.5">
            <option v-for="cycle in CYCLE_LIST" :key="cycle">{{ cycle }}</option>
          </select>
        </label>
      </div>
      <p v-if="metaForm.renewDate" class="text-xs text-ios-secondary">{{ calculateRemainingDays(metaForm.renewDate).label }}</p>
      <p v-if="errors.basic" role="alert" class="text-sm text-ios-red">{{ errors.basic }}</p>
      <button class="ios-btn-primary" :disabled="!!saving">保存基本信息与续费</button>
    </form>
    <form class="panel-card p-6 space-y-4" @submit.prevent="save('access')">
      <h2 class="font-semibold text-ios-label">订阅与维护</h2>
      <label class="flex items-center gap-2 text-sm text-ios-secondary cursor-pointer">
        <input v-model="form.enabled" type="checkbox" />
        启用服务器控制连接（停用会拒绝心跳与指令）
      </label>
      <label class="flex items-center gap-2 text-sm text-ios-secondary cursor-pointer">
        <input v-model="form.allowSubscription" type="checkbox" />
        允许用户订阅
      </label>
      <label class="flex items-center gap-2 text-sm text-ios-secondary cursor-pointer">
        <input v-model="form.maintenance" type="checkbox" />
        维护模式（临时排除在订阅之外）
      </label>
      <p v-if="errors.access" role="alert" class="text-sm text-ios-red">{{ errors.access }}</p>
      <button class="ios-btn-primary" :disabled="!!saving">保存订阅与维护</button>
    </form>
    <form class="panel-card p-6 space-y-4" @submit.prevent="save('network')">
      <h2 class="font-semibold text-ios-label">出站网络</h2>
      <p class="text-xs text-ios-secondary">核心源 IP / 网卡仅影响代理核心流量；服务器强制出站作用于整台主机经该网卡新建的出站连接。保存后需发布配置，留空表示清除。</p>
      <div class="grid sm:grid-cols-2 gap-4 text-xs text-ios-secondary">
        <label v-for="field in networkFields" :key="field.key">
          {{ field.label }}
          <input v-model.trim="form[field.key]" class="form-input-base mt-1.5" />
        </label>
      </div>
      <p v-if="errors.network" role="alert" class="text-sm text-ios-red">{{ errors.network }}</p>
      <button class="ios-btn-primary" :disabled="!!saving">保存出站网络</button>
    </form>
    <section id="server-programs" class="panel-card p-6 space-y-5">
      <h2 class="font-semibold text-ios-label">程序</h2>
      <div class="flex flex-wrap gap-3">
        <router-link to="/nodes/versions" class="ios-btn-secondary">版本与更新</router-link>
        <button class="ios-btn-secondary" @click="$emit('install')">生成安装命令</button>
      </div>
      <details ref="coreDetails">
        <summary class="cursor-pointer text-sm text-ios-secondary">核心更新</summary>
        <CoreUpdatesPanel :nodes="[node]" />
      </details>
      <NodeRestartPanel :node="node" />
      <NodeCertificatePanel :node="node" />
    </section>
    <section class="panel-card p-6 space-y-4 border border-ios-red/20">
      <h2 class="font-semibold text-ios-red">危险区域</h2>
      <p class="text-xs text-ios-secondary">删除前检查配置引用与发布状态；删除后吊销身份，历史流量和访问记录保留。</p>
      <div class="flex flex-wrap gap-3">
        <button class="ios-btn-danger" @click="$emit('delete')">删除服务器</button>
        <button class="ios-btn-danger" @click="revokeOpen = true">吊销证书</button>
      </div>
      <p v-if="errors.revoke" role="alert" class="text-sm text-ios-red">{{ errors.revoke }}</p>
    </section>
    <ConfirmDialog v-model="revokeOpen" title="吊销服务器证书" message="吊销后此服务器不能再使用当前证书建立控制连接，已有连接也会在下一次消息校验时关闭。" @confirm="revoke" />
  </div>
</template>
<script setup lang="ts">
import { reactive, ref, watch } from 'vue';
import { nodeAPI, type NodeView, type NodeSettingsInput } from '../../api';
import { type NodeMeta, loadNodeMeta, saveNodeMeta, parseRenewPrice, calculateRemainingDays, CURRENCY_LIST, CYCLE_LIST } from '../../nodeMeta';
import { isOwner } from '../../../../app/auth';
import { showToast } from '../../../../app/api';
import NodeRestartPanel from '../NodeRestartPanel.vue';
import NodeCertificatePanel from '../NodeCertificatePanel.vue';
import CoreUpdatesPanel from '../../../system/components/CoreUpdatesPanel.vue';
import ConfirmDialog from '../../../../components/ConfirmDialog.vue';
const coreDetails = ref<HTMLDetailsElement>();
defineExpose({ openCoreUpdates: () => { if (coreDetails.value) coreDetails.value.open = true; } });
const props = defineProps<{ node: NodeView; meta: NodeMeta }>();
const emit = defineEmits<{ changed: []; install: []; delete: [] }>();
const form = reactive<NodeSettingsInput>({ ...props.node });
const metaForm = reactive({ region: '', remark: '', renewDate: '', renewCurrency: 'CNY', renewPrice: '', renewCycle: '/年', renewNoticeDays: 7 });
const saving = ref(''), revokeOpen = ref(false), errors = reactive<Record<string, string>>({});
const networkFields = [{ key: 'outboundSourceIp', label: '核心出站源 IP' }, { key: 'outboundInterface', label: '核心出站网卡' }, { key: 'hostSourceIpv4', label: '服务器强制出站 IPv4' }, { key: 'hostSourceIpv6', label: '服务器强制出站 IPv6' }] as const;
watch(() => props.node.id, () => {
  Object.assign(form, props.node);
  const meta = props.meta, price = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  Object.assign(metaForm, { region: meta.region || '', remark: meta.remark || '', renewDate: meta.renewDate || '', renewCurrency: price.currency, renewPrice: price.price, renewCycle: price.cycle, renewNoticeDays: meta.renewNoticeDays ?? 7 });
}, { immediate: true });
function ipv4(value: string) { return value.split('.').length === 4 && value.split('.').every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255); }
function ipv6(value: string) { try { return value.includes(':') && new URL(`http://[${value}]/`).hostname.startsWith('['); } catch { return false; } }
function validateNetwork() {
  if (form.outboundSourceIp && !ipv4(form.outboundSourceIp) && !ipv6(form.outboundSourceIp)) throw new Error('核心出站源 IP 需要有效 IPv4 或 IPv6 地址');
  if (form.outboundInterface && !/^[A-Za-z0-9_.:-]{1,15}$/.test(form.outboundInterface)) throw new Error('网卡名需为 1～15 个字母、数字或 _ . : -');
  if (form.hostSourceIpv4 && !ipv4(form.hostSourceIpv4)) throw new Error('服务器强制出站 IPv4 格式无效');
  if (form.hostSourceIpv6 && !ipv6(form.hostSourceIpv6)) throw new Error('服务器强制出站 IPv6 格式无效');
}
async function save(group: 'basic' | 'access' | 'network') {
  if (saving.value || !isOwner.value) return;
  saving.value = group; errors[group] = '';
  try {
    if (group === 'network') validateNetwork();
    const latest = await nodeAPI.get(props.node.id);
    const change = group === 'basic' ? { name: form.name, publicIp: form.publicIp } : group === 'access' ? { enabled: form.enabled, allowSubscription: form.allowSubscription, maintenance: form.maintenance } : Object.fromEntries(networkFields.map(field => [field.key, form[field.key]]));
    await nodeAPI.save({ ...latest, ...change });
    if (group === 'basic') {
      const latestMeta = await loadNodeMeta(props.node.id, true);
      await saveNodeMeta(props.node.id, { ...latestMeta, ...metaForm });
    }
    showToast(group === 'network' ? '出站网络已保存，发布配置后生效' : '设置已保存'); emit('changed');
  } catch (e) { errors[group] = (e as Error).message; emit('changed'); }
  finally { saving.value = ''; }
}
async function revoke() {
  if (saving.value) return;
  saving.value = 'revoke'; errors.revoke = '';
  try { await nodeAPI.revoke(props.node.id); showToast('服务器证书已吊销'); emit('changed'); }
  catch (e) { errors.revoke = (e as Error).message; }
  finally { saving.value = ''; }
}
</script>
