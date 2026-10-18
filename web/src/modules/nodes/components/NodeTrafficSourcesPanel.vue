<template>
 <section class="panel-card p-6 space-y-3">
  <h2 class="font-semibold">账户字节采样来源</h2>
  <p class="text-sm text-gray-500">{{ label }}</p>
  <dl v-if="fresh && sample && sample.core_epoch" class="grid sm:grid-cols-2 gap-4 text-sm">
   <div><dt class="text-gray-500">配置来源 / 本进程曾完整观察来源</dt><dd>{{ sample.configured_sources ?? 0 }} / {{ sample.observed_sources ?? 0 }}</dd></div>
   <div><dt class="text-gray-500">采样配置版本</dt><dd>#{{ sample.revision }}</dd></div>
   <div><dt class="text-gray-500">最近成功读取原生计数</dt><dd>{{ time(sample.last_success_at) }}</dd></div>
   <div><dt class="text-gray-500">核心运行观察</dt><dd>{{ sample.core_running ? '运行中' : '已停止，来源数量为原进程历史' }}</dd></div>
  </dl>
  <p class="text-xs text-gray-500">一个设备可能有多个路由计数来源，来源数不是用户、设备或连接数。首次空响应和未出现来源保持未知，不表示零消费。曾出现来源消失会拒绝采样，保留原计费保护；成功读取不等于写入队列或中央已确认。</p>
  <p class="text-xs text-gray-500">来源只覆盖当前核心进程，重启重新观察；全部配置来源曾出现也不证明零损失、完整 UDP 或逐域名字节。旧节点、离线或超过45秒心跳显示未知；最近成功采样超过15秒时，不确认当前覆盖。</p>
 </section>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import type { NodeView } from '../api';
import { formatTime } from '../../../app/auth';
const props=defineProps<{node:NodeView;now:number}>();
const sample=computed(()=>props.node.heartbeat.traffic_source_status);
const fresh=computed(()=>{const observed=(sample.value?.observed_at?.seconds ?? 0)*1000;return !!(props.node.enabled && ['online','degraded'].includes(props.node.status) && sample.value && props.now-observed>=-5000 && props.now-observed<45000 && props.now-Date.parse(props.node.lastHeartbeat)<45000);});
const label=computed(()=>{
 if(!fresh.value)return '尚未上报或样本已过期，来源覆盖未知';
 const s=sample.value;
 if(!s)return '来源覆盖未知';
 if(s.status==='sampling_failed')return '最近采样失败，不能确认当前字节来源覆盖';
 if(s.status==='not_running')return '核心已停止，不确认当前字节来源覆盖';
 if(s.status==='busy')return '核心正在执行操作，本次未读取来源状态';
 if(s.status==='unavailable')return '当前没有可用的原生统计来源状态';
 if(s.status==='unobserved')return '尚未成功读取原生计数，字节覆盖未知';
 const succeeded=(s.last_success_at?.seconds ?? 0)*1000;
 if(!succeeded || props.now-succeeded>=15000 || props.now-succeeded < -5000)return '最近成功采样已过期，当前覆盖未知；下列数量为本进程历史';
 if(s.status==='covered')return '本进程已观察到全部配置计数来源；不代表全部流量零损失';
 if(s.status==='partial')return (s.observed_sources ?? 0) ? '部分配置来源尚未出现，其消费保持未知' : '读取成功，但尚无完整账户计数来源；不能当作零消费';
 return '来源状态未知';
});
function time(value?:{seconds?:number;nanos?:number}){return value?.seconds?formatTime(new Date(value.seconds*1000).toISOString()):'尚无成功读取';}
</script>
