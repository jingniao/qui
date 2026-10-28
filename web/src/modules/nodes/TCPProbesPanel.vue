<template>
  <div class="space-y-3">
    <h3 class="font-semibold text-sm text-ios-label">TCP 出口延迟与丢包</h3>
    <p class="text-xs text-ios-secondary">由节点连接指定目标；每个目标保留最近 100 次结果。此值不代表带宽或双向网络丢包。</p>
    <p v-if="error" class="text-sm text-red-600">{{ error }}</p>
    <form class="flex flex-wrap gap-2" @submit.prevent="add">
      <input v-model="target.label" class="ios-input" placeholder="目标标签" required maxlength="120" />
      <input v-model="target.host" class="ios-input" placeholder="主机或 IP" required />
      <input v-model.number="target.port" class="ios-input" type="number" min="1" max="65535" aria-label="目标端口" required />
      <label class="text-xs"><input v-model="target.allowNonPublic" type="checkbox" />明确允许非公网地址</label>
      <button class="ios-btn-secondary" :disabled="busy">添加目标</button>
    </form>
    <div class="flex flex-wrap gap-2">
      <select v-model="selected" class="ios-input" @change="refresh"><option value="">选择目标</option><option v-for="t in targets" :key="t.id" :value="t.id">{{ t.label }} ({{ t.host }}:{{ t.port }})</option></select>
      <label class="text-xs">次数 <input v-model.number="attempts" type="number" min="1" max="10" class="ios-input" /></label>
      <label class="text-xs">每次超时 ms <input v-model.number="timeoutMs" type="number" min="1" max="3000" class="ios-input" /></label>
      <button class="ios-btn-primary" :disabled="busy || !selected" @click="run">测试</button>
      <button class="ios-btn-secondary" :disabled="busy" @click="refresh">刷新结果</button>
      <button class="ios-btn-secondary" :disabled="busy || !selected" @click="remove">删除目标</button>
    </div>
    <p v-for="row in results" :key="row.id" class="text-xs text-ios-secondary">{{ row.status }} · 丢失 {{ row.result ? `${row.result.lost || 0}/${row.result.attempted || 0}` : '—' }} · 中位数 {{ row.result?.median_ms == null ? '—' : `${row.result.median_ms.toFixed(2)} ms` }}</p>
    <p v-if="!results.length" class="text-xs text-ios-tertiary">暂无结果</p>
  </div>
</template>

<script setup lang="ts">
import {ref,reactive,watch} from 'vue';
import {request} from '../../app/apiClient';
const props=defineProps<{nodeId:string}>();
interface Target {id:string;label:string;host:string;port:number;allowNonPublic:boolean}
interface Row {id:string;status:string;result?:{attempted?:number;lost?:number;median_ms?:number}}
const targets=ref<Target[]>([]),results=ref<Row[]>([]),selected=ref(''),error=ref(''),busy=ref(false);
const target=reactive({label:'',host:'',port:443,allowNonPublic:false}),attempts=ref(3),timeoutMs=ref(1000);
async function guard(action:()=>Promise<void>){busy.value=true;error.value='';try{await action()}catch(e){error.value=(e as Error).message}finally{busy.value=false}}
async function history(){results.value=[];if(selected.value){const id=props.nodeId,key=selected.value;const r=await request<{items:Row[]}>(`/nodes/${encodeURIComponent(id)}/tcp-probes?targetId=${encodeURIComponent(key)}`);if(id===props.nodeId&&key===selected.value)results.value=r.items}}
async function refresh(){await guard(async()=>{targets.value=(await request<{items:Target[]}>('/probe-targets')).items;await history()})}
async function add(){await guard(async()=>{const t=await request<Target>('/probe-targets','POST',target);targets.value.push(t);selected.value=t.id;await history()})}
async function remove(){await guard(async()=>{await request(`/probe-targets/${encodeURIComponent(selected.value)}`,'DELETE');targets.value=targets.value.filter(t=>t.id!==selected.value);selected.value='';results.value=[]})}
async function run(){await guard(async()=>{await request(`/nodes/${encodeURIComponent(props.nodeId)}/tcp-probes`,'POST',{targetId:selected.value,attempts:attempts.value,timeoutMs:timeoutMs.value});await history()})}
watch(()=>props.nodeId,()=>{results.value=[];void refresh()},{immediate:true});
</script>
