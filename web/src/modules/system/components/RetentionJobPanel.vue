<template>
 <section class="panel-card p-5 space-y-3">
  <div class="flex justify-between gap-3"><h3 class="font-semibold">保留清理进度</h3><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新状态</button></div>
  <ErrorState v-if="error" :message="error" @retry="load" />
  <p v-if="loading" class="text-xs text-gray-500">正在读取持久清理记录…</p>
  <div v-if="job" class="space-y-2 text-xs">
   <p :class="job.errorCode ? 'text-amber-700' : 'text-gray-600'">{{ state(job) }}</p>
   <p v-if="job.generation">第 {{ job.generation }} 轮 · {{ phase(job.phase) }} · 每批最多 {{ job.batchRows }} 行</p>
   <p>本轮已提交删除 {{ job.deletedRun }} 行；累计 {{ job.deletedTotal }} 行；失败尝试 {{ job.failures }} 次。</p>
   <p v-if="job.generation">本轮期限：允许 {{ job.policy.allowedDays }} 天，阻断 {{ job.policy.blockedDays }} 天，小时流量和账户明细 {{ job.policy.hourlyDays }} 天。</p>
   <p v-if="job.updatedAt">最近持久记录：{{ formatTime(job.updatedAt) }}</p>
   <p>上次整轮完成：{{ job.lastSuccessAt ? formatTime(job.lastSuccessAt) : '尚无完成记录' }}</p>
   <p v-if="job.errorCode" class="text-amber-700">存储或清理暂不可用，后台会重试；请核对磁盘、数据库和迁移。不能据失败记录认为超期数据已清除。</p>
   <p class="text-gray-500">中断后继续原断点；策略变化按新期限重新开始。此记录不证明服务当前仍在运行；磁盘故障也可能使失败状态无法保存。</p>
   <p class="text-gray-500">失败只记录到该尝试实际读取且仍匹配的原检查点。后续批次或新策略已推进时，旧错误不会覆盖较新结果；未能读取原检查点的故障不计入这里。</p>
   <p class="text-gray-500">任务阶段、错误计数或时间关系矛盾时，状态读取会拒绝，不把成功标签当作完成证明。上轮成功后新尝试失败时，仍保留原成功时间，并显示当前错误。</p>
  </div>
 </section>
</template>
<script setup lang="ts">
import { onMounted,onUnmounted,ref } from 'vue';
import { retentionJobAPI,type RetentionJob } from '../retentionJobApi';
import { formatTime } from '../../../app/auth';
import ErrorState from '../../../components/ErrorState.vue';
const job=ref<RetentionJob|null>(null),loading=ref(false),error=ref('');let generation=0,stopped=false;
async function load(){const current=++generation;loading.value=true;error.value='';job.value=null;try{const result=await retentionJobAPI();if(current===generation && !stopped)job.value=result;}catch(e){if(current===generation && !stopped)error.value=(e as Error).message;}finally{if(current===generation)loading.value=false;}}
function state(value:RetentionJob){if(value.errorCode)return '最近清理尝试失败，保留已提交进度';return ({idle:'尚未开始清理',running:'本轮清理未完成，等待后台接续',error:'本轮清理失败，等待后台重试',success:'上次整轮清理已提交'} as Record<string,string>)[value.status] || '记录状态需核对';}
function phase(value:number){return ['允许访问记录','阻断访问记录','域名小时流量','账户小时流量','账户流量明细','本轮已完成'][value] || '阶段需核对';}
onMounted(load);onUnmounted(()=>{stopped=true;generation++;});
</script>
