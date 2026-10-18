<template>
 <section class="panel-card p-6 space-y-4 text-sm"><div class="flex flex-wrap gap-3 justify-between"><h2 class="font-semibold">核心健康检查</h2><div class="flex gap-3"><button class="ios-btn-secondary" :disabled="loading" @click="load">刷新结果</button><button class="ios-btn-primary" :disabled="busy || pending || !eligible" @click="requestCheck">{{ busy ? '申请中…' : '执行健康检查' }}</button></div></div>
 <p class="text-xs text-gray-500">检查当前配置版本的核心健康，不重启或重载、不执行自定义命令。任务受理后等待节点回执；检查通过也不表示所有网站和出口可用。</p>
 <p v-if="error" class="text-red-600">{{ error }}</p><p v-if="!items.length" class="text-gray-500">暂无检查记录。</p>
 <div v-for="item in items" :key="item.id" class="border-b border-gray-100 py-3 flex flex-wrap justify-between gap-3"><div><p>配置 #{{ item.revision }} · {{ formatTime(item.createdAt) }}</p><p class="text-xs text-gray-500 mt-1">{{ label(item) }}</p></div><span>{{ item.status === 'queued' ? '等待节点回执' : item.status === 'expired' ? '已过期' : item.result?.healthy ? '通过' : '未通过' }}</span></div>
 </section>
</template>
<script setup lang="ts">
import {computed,onMounted,onUnmounted,ref,watch} from 'vue';import {nodeAPI,type NodeView,type HealthCheck} from '../api';import {formatTime} from '../../../app/auth';import {showToast} from '../../../app/api';
const props=defineProps<{node:NodeView}>();const items=ref<HealthCheck[]>([]),error=ref(''),busy=ref(false),loading=ref(false);let timer:ReturnType<typeof setTimeout>|null=null;let generation=0;let stopped=false;
const pending=computed(()=>items.value.some(item=>item.status==='queued'));const eligible=computed(()=>props.node.enabled&&['online','degraded'].includes(props.node.status)&&(props.node.heartbeat.current_revision ?? 0)>0);
function label(item:HealthCheck){if(!item.result)return item.status==='expired'?'未在有效期内收到结果':'任务有效期 '+formatTime(item.deadline);return ({healthy:'核心健康检查通过',core_unhealthy:'核心未通过健康检查',revision_changed:'配置版本已变化，请刷新后重试',expired:'任务有效期已过',unsupported:'节点不支持此操作'} as Record<string,string>)[item.result.code] || '结果未知';}
async function load(){if(loading.value||stopped)return;const current=++generation;loading.value=true;try{const result=await nodeAPI.healthChecks(props.node.id);if(current===generation){items.value=result.items;error.value='';}}catch(e){if(current===generation)error.value=(e as Error).message;}finally{if(current===generation){loading.value=false;if(timer)clearTimeout(timer);timer=null;if(pending.value&&!stopped)timer=setTimeout(()=>{timer=null;void load();},2500);}}}
async function requestCheck(){if(busy.value||!eligible.value)return;busy.value=true;try{await nodeAPI.requestHealth(props.node.id,props.node.heartbeat.current_revision ?? 0);showToast('健康检查已受理，等待节点回执');await load();}catch(e){error.value=(e as Error).message;}finally{busy.value=false;}}
watch(()=>props.node.id,()=>{generation++;loading.value=false;items.value=[];void load();});onMounted(load);onUnmounted(()=>{stopped=true;generation++;if(timer)clearTimeout(timer);});
</script>
