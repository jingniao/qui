<template>
  <section class="rounded-xl border border-gray-200 p-4 space-y-3 text-xs">
    <div class="flex items-center justify-between"><h3 class="font-semibold">原登记阶段</h3><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新登记阶段</button></div>
    <p v-if="loading" class="text-gray-500">正在核对原回执和当前中央状态…</p>
    <ErrorState v-if="error" :message="error" @retry="load" />
    <template v-if="status && !loading">
      <p class="text-gray-700">{{ status.summary }}</p>
      <dl class="grid md:grid-cols-2 gap-3">
        <div><dt class="text-gray-500">中央原登记回执</dt><dd>{{ status.receiptState === 'verified' ? '原内容已核对' : status.receiptState === 'missing' ? '缺失，不补造' : '未知，需核对' }}</dd></div>
        <div><dt class="text-gray-500">原 Token 窗口</dt><dd>{{ tokenLabels[status.tokenState] || '未知，需核对' }}<span v-if="status.tokenExpiresAt"> · {{ formatTime(status.tokenExpiresAt) }}</span></dd></div>
        <div><dt class="text-gray-500">当前身份与原回执</dt><dd>{{ status.receiptState !== 'verified' ? '无原回执可比较' : status.currentIdentityMatches ? '证书原字节、指纹与期限一致' : '已变化，原回执仍保留' }}</dd></div>
        <div><dt class="text-gray-500">当前中央证书校验</dt><dd>{{ status.currentCertificateValid ? '通过，仍需节点认证' : '未通过或不可确认' }} · {{ certificateLabels[status.currentCertificate.status] || '未知' }}</dd></div>
        <div><dt class="text-gray-500">节点启用 / 吊销</dt><dd>{{ status.enabled ? '启用' : '禁用' }} · {{ status.identityRevoked ? '已记录吊销' : '未记录吊销' }}</dd></div>
        <div><dt class="text-gray-500">中央确认消息 / 续期候选</dt><dd>{{ status.lastSequence > 0 ? '已有确认消息，初始登记接续受限' : '确认位置为零，仍需其他条件' }} · {{ status.pendingRenewal ? '存在候选' : '无候选' }}</dd></div>
      </dl>
      <p class="text-amber-700">{{ status.centralReplayConditionsMatch ? '中央条件吻合仍需同一私钥有效 CSR 和原机器请求；此页不执行重放。' : '当前中央条件未吻合，不能据原回执重新登记或扩展窗口。' }}</p>
      <details v-if="status.receiptState === 'verified'"><summary class="cursor-pointer">原回执公开信息</summary><div class="mt-2 space-y-1"><p class="font-mono break-all">原证书：{{ status.originalFingerprint }}</p><p>原登记：{{ formatTime(status.originalCreatedAt || '') }} · 原证书期限：{{ formatTime(status.originalExpiresAt || '') }}</p></div></details>
      <ul class="list-disc pl-5 space-y-1"><li v-for="step in status.nextSteps" :key="step">{{ step }}</li></ul>
      <p class="text-gray-500">本地安装尚未由此查询确认。中央记录不证明节点当前在线、代理可用或旧状态已结清；比较时间：{{ formatTime(status.observedAt) }}。</p>
    </template>
  </section>
</template>
<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue';
import { nodeAPI, type EnrollmentStatus } from '../api';
import { formatTime } from '../../../app/auth';
import ErrorState from '../../../components/ErrorState.vue';
const props=defineProps<{nodeId:string;identitySnapshotDigest:string}>();
const status=ref<EnrollmentStatus|null>(null),loading=ref(false),error=ref('');
const tokenLabels:Record<string,string>={unknown:'未确认',missing:'原 Token 不在中央，不重建',original_consumption_conflict:'原消费事实冲突',invalid_expiry:'期限材料无效',expired:'原窗口已过期',consumed_window_current:'已消费，原窗口仍在'};
const certificateLabels:Record<string,string>={unknown:'证书未知',not_yet_valid:'尚未生效',expired:'已过期',renewal_due:'临近续期',valid:'时效有效',revoked:'已吊销'};
let generation=0;
async function load(){const current=++generation,nodeId=props.nodeId;status.value=null;loading.value=true;error.value='';try{const result=await nodeAPI.enrollmentStatus(nodeId);if(current===generation && props.nodeId===nodeId)status.value=result;}catch(e){if(current===generation)error.value=(e as Error).message;}finally{if(current===generation)loading.value=false;}}
watch(()=>[props.nodeId,props.identitySnapshotDigest],()=>{void load();});
onMounted(load);onUnmounted(()=>{generation++;});
</script>
