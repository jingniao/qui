<template>
  <section class="panel-card p-6 space-y-4 text-sm">
    <div class="flex justify-between gap-3"><h2 class="font-semibold">节点证书</h2><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新证书状态</button></div>
    <p v-if="error" class="text-red-600">{{ error }}</p>
    <template v-if="status">
      <p v-if="!status.currentSigningAvailable" class="rounded-xl border border-amber-200 bg-amber-50 p-3 text-amber-900">当前CA不能签发新身份或续期。请保留原材料并核对有效期与时钟；历史认证通过不表示当前可签发，刷新状态不会修复CA。</p>
	  <NodeEnrollmentPanel :key="'enrollment-'+status.nodeId" :node-id="status.nodeId" :identity-snapshot-digest="status.identitySnapshotDigest" />
      <p v-if="!status.active" class="text-gray-500">尚无已注册证书。</p>
      <dl v-else class="grid md:grid-cols-2 gap-4">
        <div><dt class="text-gray-500">当前身份</dt><dd class="mt-1">{{ label(status.active) }}</dd></div>
        <div><dt class="text-gray-500">有效期</dt><dd class="mt-1">{{ status.active.expiresAt ? formatTime(status.active.expiresAt) : '无法确认' }}</dd></div>
        <div><dt class="text-gray-500">当前证书签发来源</dt><dd class="mt-1">{{ issuerNames[status.active.issuer || 'unknown'] || '无法确认' }}</dd></div>
        <div class="md:col-span-2"><dt class="text-gray-500">证书指纹</dt><dd class="mt-1 font-mono break-all text-xs">{{ status.active.fingerprint || '无法确认' }}</dd></div>
        <div v-if="status.active.revokedAt"><dt class="text-gray-500">吊销时间</dt><dd class="mt-1">{{ formatTime(status.active.revokedAt) }}</dd></div>
      </dl>
      <div v-if="status.pending" class="rounded-xl border border-amber-200 bg-amber-50 p-4 space-y-2 text-amber-900">
        <p class="font-semibold">{{ purposeNames[status.pending.purpose] }} · {{ label(status.pending) }}</p>
        <p>候选来源：{{ issuerNames[status.pending.issuer || 'unknown'] || '无法确认' }}</p>
        <p>申请时间：{{ formatTime(status.pending.createdAt) }} · 候选到期：{{ status.pending.expiresAt ? formatTime(status.pending.expiresAt) : '无法确认' }}</p>
        <p class="font-mono break-all text-xs">{{ status.pending.fingerprint || '候选指纹无法确认' }}</p>
        <p class="text-xs">中央尚未确认候选身份激活。节点保存成功后需使用新证书连接确认；不要删除身份或重新注册来处理等待状态。</p>
      </div>
      <p v-else-if="status.active" class="text-xs text-gray-500">中央没有待确认的更换记录。</p>
      <p class="text-xs text-gray-500">查询时间：{{ formatTime(status.observedAt) }}。此处展示中央身份记录；不证明节点在线或代理可用。</p>
    </template>
    <p v-else-if="loading" class="text-gray-500">正在读取证书状态…</p>
    <p class="text-xs text-gray-500">Agent 自动续期沿用原私钥。普通主动续期或私钥轮换使用停止节点后的固定操作；过期证书可按上方Owner流程签发，再在节点导入和确认。</p>
  </section>
</template>
<script setup lang="ts">
import {ref,onMounted,onUnmounted,watch} from 'vue';
import {nodeAPI,type NodeView,type CertificateStatus,type CertificateObservation} from '../api';
import {formatTime} from '../../../app/auth';
import NodeEnrollmentPanel from './NodeEnrollmentPanel.vue';
const props=defineProps<{node:NodeView}>();
const status=ref<CertificateStatus|null>(null),error=ref(''),loading=ref(false);
let generation=0,stopped=false;
const statusNames:Record<string,string>={unknown:'无法确认',valid:'有效',renewal_due:'即将到期，进入自动续期窗口',expired:'已过期',not_yet_valid:'尚未生效',revoked:'已吊销',disabled:'节点已禁用',source_mismatch:'原身份不匹配，不能确认',awaiting_confirmation:'等待新身份确认'};
const purposeNames={renewal:'证书续期',key_rotation:'私钥轮换',unknown:'身份更换类型无法确认'};
const issuerNames:Record<string,string>={current:'当前签发 CA',unrecognized:'签发来源不匹配，需核对',unknown:'无法确认'};
function label(item:CertificateObservation){const state=statusNames[item.status]||'无法确认';return item.integrity==='invalid'?`${state} · 证书记录不一致`:state;}
async function load(){if(loading.value||stopped)return;const current=++generation;loading.value=true;try{const result=await nodeAPI.certificateStatus(props.node.id);if(current===generation){status.value=result;error.value='';}}catch(e){if(current===generation){status.value=null;error.value=(e as Error).message;}}finally{if(current===generation)loading.value=false;}}
watch(()=>props.node.id,()=>{generation++;loading.value=false;status.value=null;error.value='';void load();});
watch(()=>[props.node.enabled,props.node.lastHeartbeat],()=>{void load();});
onMounted(load);onUnmounted(()=>{stopped=true;generation++;});
</script>
