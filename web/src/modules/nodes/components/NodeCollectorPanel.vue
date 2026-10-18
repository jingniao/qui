<template>
 <section class="panel-card p-6 space-y-3">
  <h2 class="font-semibold">域名记录采集状态</h2>
  <p class="text-sm" :class="fresh && sample?.status !== 'ok' ? 'text-amber-700' : 'text-gray-500'">{{ label }}</p>
  <dl v-if="fresh && sample" class="grid sm:grid-cols-2 gap-4 text-sm">
   <div><dt class="text-gray-500">待聚合观察 / 内存聚合记录</dt><dd>{{ sample.pending_observations ?? 0 }} / {{ sample.aggregated_rollups ?? 0 }}</dd></div>
   <div><dt class="text-gray-500">本进程允许 / 阻断观察丢弃</dt><dd>{{ sample.dropped_allowed_observations ?? 0 }} / {{ sample.dropped_blocked_observations ?? 0 }}</dd></div>
   <div><dt class="text-gray-500">解析、归属或提交拒绝</dt><dd>{{ sample.rejected_observations ?? 0 }}</dd></div>
   <div><dt class="text-gray-500">本进程持久化失败尝试</dt><dd>{{ sample.persistence_failures ?? 0 }}</dd></div>
   <div><dt class="text-gray-500">最近成功写入队列</dt><dd>{{ time(sample.last_flush_success_at) }}</dd></div>
   <div><dt class="text-gray-500">采集进程开始</dt><dd>{{ time(sample.started_at) }}</dd></div>
  </dl>
  <p v-if="fresh && sample?.counters_saturated" class="text-xs text-amber-700">计数已达到上限，显示值仅为下界。</p>
  <div v-if="fresh && sample?.persisted_loss" class="rounded-lg bg-gray-50 p-3 space-y-2 text-xs">
   <p>{{ lossLabel }}</p>
   <dl v-if="sample.persisted_loss.measured" class="grid sm:grid-cols-2 gap-3">
    <div><dt class="text-gray-500">跨进程已保存允许 / 阻断观察丢弃</dt><dd>{{ sample.persisted_loss.dropped_allowed_observations ?? 0 }} / {{ sample.persisted_loss.dropped_blocked_observations ?? 0 }}</dd></div>
    <div><dt class="text-gray-500">跨进程已保存拒绝 / 写入失败尝试</dt><dd>{{ sample.persisted_loss.rejected_observations ?? 0 }} / {{ sample.persisted_loss.persistence_failures ?? 0 }}</dd></div>
    <div><dt class="text-gray-500">前序结束未确认进程</dt><dd>{{ sample.persisted_loss.unconfirmed_processes ?? 0 }}</dd></div>
    <div><dt class="text-gray-500">最近损失检查点</dt><dd>{{ time(sample.persisted_loss.checkpoint_at) }}</dd></div>
    <div><dt class="text-gray-500">本队列损失记录起点</dt><dd>{{ time(sample.persisted_loss.baseline_at) }}</dd></div>
   </dl>
   <p>跨进程总数只覆盖本队列成功保存的检查点，不与本进程数相加；初次接入前、检查点之后和强制退出前未保存的损失仍未知。拒绝与丢弃可能重叠，不是流量或连接数，队列淘汰另计。</p>
  </div>
  <p class="text-xs text-gray-500">计数只覆盖当前采集进程，重启会重置；不代表连接数、访问总数或流量。拒绝与丢弃可能重叠，不能相加。持久队列淘汰数另列在缓存面板。阻断观察在进入持久队列前也可能因内存积压丢失。</p>
  <p class="text-xs text-gray-500">失败写入保留内存聚合供重试，但进程退出前仍写入失败时无法保证保留。旧节点、离线或超过45秒样本显示未知，不把缺数据当零。</p>
 </section>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../api';
import { formatTime } from '../../../app/auth';
const props=defineProps<{node:NodeView;now:number}>();
const sample=computed(()=>props.node.heartbeat.collector_status);
const fresh=computed(()=>{const observed=(sample.value?.observed_at?.seconds ?? 0)*1000;return !!(props.node.enabled && ['online','degraded'].includes(props.node.status) && sample.value && props.now-observed>=-5000 && props.now-observed<45000 && props.now-Date.parse(props.node.lastHeartbeat)<45000);});
const label=computed(()=>{if(!fresh.value)return '尚未上报或样本已过期';switch(sample.value?.status){case 'ok':return '本进程尚无记录损失，最近写入未报告失败';case 'observation_loss':return '本进程已出现记录丢弃或拒绝，查看计数';case 'persistence_degraded':return '最近记录持久化失败，内存聚合仍待写入';default:return '采集状态无法确认';}});
const lossLabel=computed(()=>{const s=sample.value?.persisted_loss;if(!s?.measured)return '损失检查点暂不可保存或读取，跨进程计数未知；保留原队列并核对存储';if(s.counters_saturated)return '已保存计数达到上限，只能作为下界';const stamp=(s.checkpoint_at?.seconds ?? 0)*1000;if(!stamp || props.now-stamp>=75000 || props.now-stamp < -5000)return '检查点已过期，下列数值仅为最后保存的历史';return '跨进程检查点已保存，覆盖仍不完整';});
function time(value:{seconds?:number;nanos?:number}|undefined){return value?.seconds ? formatTime(new Date(value.seconds*1000).toISOString()) : '尚无记录';}
</script>
