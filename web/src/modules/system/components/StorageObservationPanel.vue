<template>
 <section class="panel-card p-5 space-y-3">
  <div class="flex justify-between gap-3"><h3 class="font-semibold">访问存储增长观察</h3><button class="ios-btn-secondary" :disabled="loading" @click="load">读取规模</button></div>
  <p class="text-xs text-gray-500">按需读取有界行数，不持续扫描全部历史数据。</p>
  <ErrorState v-if="error" :message="error" @retry="load" />
  <p v-if="loading" class="text-xs text-gray-500">正在读取存储规模…</p>
  <div v-if="sample" class="text-xs space-y-3">
   <p>观察时间：{{ formatTime(sample.observedAt) }}</p>
   <p>中央主数据库已分配：{{ bytes(sample.mainDatabaseAllocatedBytes) }}；其中可复用页：{{ bytes(sample.mainDatabaseReusableBytes) }}。</p>
   <p>空间回收方式：{{ reclaimModes[sample.mainDatabaseReclaimMode] || '尚未上报' }}。</p>
   <p>容量告警：{{ warningLabels[sample.warningLevel] }}<span v-if="sample.warningBytes">（提示 {{ bytes(sample.warningBytes) }} / 严重 {{ bytes(sample.criticalBytes) }}，按主库已分配大小判断，不含 WAL 与磁盘剩余空间）</span>。</p>
   <div class="rounded-lg bg-gray-50 p-3 space-y-2">
    <p>最近维护记录：{{ maintenanceLabel(sample.maintenance) }}</p>
    <p>尝试 {{ sample.maintenance.attempts }} 次；已记录失败 {{ sample.maintenance.failures }} 次；前序未确认结束 {{ sample.maintenance.unconfirmedAttempts }} 次。</p>
    <p v-if="sample.maintenance.updatedAt">最近持久记录：{{ formatTime(sample.maintenance.updatedAt) }}</p>
    <p>上次增量维护完成：{{ sample.maintenance.lastSuccessAt ? formatTime(sample.maintenance.lastSuccessAt) : '尚无完成记录' }}。完成不证明实际文件缩小。</p>
    <p class="text-gray-500">计数、阶段或原时间关系矛盾时拒绝读取；成功标签不能替代完整记录。时钟倒退时保留原状态，需核对后接续。</p>
    <p v-if="sample.maintenance.errorCode" class="text-amber-700">空间维护或存储暂不可用，后台会重试；访问清理已提交进度保留。请核对数据库与磁盘，不删除收据或游标。</p>
   </div>
   <table class="w-full text-left"><thead><tr><th class="py-2">数据</th><th>行数</th><th>保留边界</th></tr></thead><tbody><tr v-for="item in sample.tables" :key="item.kind"><td class="py-2">{{ names[item.kind] || '待核对数据' }}</td><td>{{ item.atLeast ? '至少 ' : '' }}{{ item.rows.toLocaleString() }}</td><td>{{ retentionNames[item.retention] }}</td></tr></tbody></table>
   <p class="text-gray-500">每类最多读取 {{ sample.countLimit.toLocaleString() }} 行，达到上限只显示下界，不能当作实际总量。主数据库页包含其他业务数据，不是访问模块单独占用；不含 WAL、共享内存、备份和磁盘剩余空间。</p>
  </div>
  <div class="rounded-lg bg-gray-50 p-3 space-y-2 text-xs">
   <p class="font-medium">存储容量告警阈值（Owner）</p>
   <p class="text-gray-500">阈值只用于展示告警级别，不会自动删除收据、游标、账务或身份历史，也不触发维护。未设置时显示“未配置”，不以默认值假装安全。请按实测日增长设置；实际磁盘余量和维护窗口另行核对。</p>
   <p v-if="thresholds">当前：提示 {{ bytes(thresholds.warningBytes) }} / 严重 {{ bytes(thresholds.criticalBytes) }}；版本 {{ thresholds.revision }}<span v-if="thresholds.updatedAt">，更新于 {{ formatTime(thresholds.updatedAt) }}</span>。</p>
   <p v-else-if="thresholdError" class="text-red-600">{{ thresholdError }}</p>
   <p v-else class="text-gray-500">尚未设置阈值；告警级别保持“未配置”。</p>
   <div class="flex flex-wrap items-end gap-2">
    <label class="flex flex-col">提示阈值 MiB<input class="form-input-base w-28" type="number" min="1" step="1" v-model.number="warningMiB" /></label>
    <label class="flex flex-col">严重阈值 MiB<input class="form-input-base w-28" type="number" min="1" step="1" v-model.number="criticalMiB" /></label>
    <button class="ios-btn-secondary" :disabled="savingThresholds || !thresholdInputValid" @click="thresholdConfirming=true">保存阈值</button>
   </div>
   <p v-if="thresholdNotice" class="text-blue-700">{{ thresholdNotice }}</p>
  </div>
  <p class="text-xs text-gray-500">按期限清理不会删除日汇总、批次收据或累计消费游标。增量模式在清理完成后每次最多回收32个空闲页，不改变旧数据库格式；可复用页和清理成功不代表文件已经缩小，不含 WAL 收缩证明。</p>
  <ConfirmDialog v-model="thresholdConfirming" title="保存存储容量告警阈值" :message="`将以主库已分配大小为基准，把提示阈值设为 ${bytes(warningBytes)}、严重阈值设为 ${bytes(criticalBytes)}。仅影响展示级别，不执行任何删除或维护。`" confirm-text="确认保存" @confirm="saveThresholds" />
 </section>
</template>
<script setup lang="ts">
import { computed,onMounted,onUnmounted,ref } from 'vue';
import { saveStorageThresholdsAPI,storageObservationAPI,storageThresholdsAPI,type StorageObservation,type SpaceMaintenanceJob,type StorageThresholds } from '../storageObservationApi';
import { formatTime } from '../../../app/auth';
import ErrorState from '../../../components/ErrorState.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
const sample=ref<StorageObservation|null>(null),loading=ref(false),error=ref('');let generation=0,stopped=false;
const names:Record<string,string>={domain_minutes:'访问分钟记录',domain_hours:'访问记录小时流量',account_hours:'账户小时流量',account_samples:'账户采样明细',domain_days:'访问记录日流量',account_days:'账户日汇总',access_receipts:'访问批次原收据',account_receipts:'账户批次原收据',account_cursors:'账户累计游标'};
const retentionNames={policy:'按保留策略清理',long_term:'长期保留',replay_protection:'幂等与消费保护，保留'};
const reclaimModes={none:'旧格式不支持在线增量回收，保留空闲页供复用',full:'数据库原生自动回收，实际文件缩小仍需观察',incremental:'支持有界增量回收，实际文件缩小仍需观察'};
const warningLabels={unknown:'未配置，不判断安全或超限',ok:'低于提示阈值',warning:'已达到提示阈值',critical:'已达到严重阈值'};
const thresholds=ref<StorageThresholds|null>(null),thresholdError=ref(''),thresholdNotice=ref('');
const warningMiB=ref<number|null>(null),criticalMiB=ref<number|null>(null);
const savingThresholds=ref(false),thresholdConfirming=ref(false);
const MiB=1024*1024;
const warningBytes=computed(()=>Math.round((warningMiB.value||0)*MiB));
const criticalBytes=computed(()=>Math.round((criticalMiB.value||0)*MiB));
const thresholdInputValid=computed(()=>{const w=warningBytes.value,c=criticalBytes.value;return w>=MiB && c>w;});
function bytes(value:number){return (value/(1024*1024)).toFixed(1)+' MiB';}
function maintenanceLabel(value:SpaceMaintenanceJob){return {idle:'尚无维护尝试',running:'开始已记录，结束结果尚未确认；不能据此判断当前仍在执行',skipped:'本轮未执行增量操作，保留原数据库模式',success:'本轮增量维护流程已完成，不代表全库缩减',error:'本轮维护失败，保留原清理结果'}[value.status] || '记录需核对';}
async function load(){if(loading.value)return;const current=++generation;loading.value=true;error.value='';sample.value=null;try{const result=await storageObservationAPI();if(current===generation && !stopped)sample.value=result;}catch(e){if(current===generation && !stopped)error.value=(e as Error).message;}finally{if(current===generation && !stopped)loading.value=false;}}
async function loadThresholds(){thresholdError.value='';try{const result=await storageThresholdsAPI();if(stopped)return;thresholds.value=result;warningMiB.value=result.warningBytes?Math.round(result.warningBytes/MiB):null;criticalMiB.value=result.criticalBytes?Math.round(result.criticalBytes/MiB):null;}catch(e){if(!stopped){thresholdError.value=(e as Error).message;thresholds.value=null;}}}
async function saveThresholds(){thresholdConfirming.value=false;if(savingThresholds.value||!thresholdInputValid.value)return;savingThresholds.value=true;thresholdError.value='';thresholdNotice.value='';try{const current=thresholds.value;const result=await saveStorageThresholdsAPI({warningBytes:warningBytes.value,criticalBytes:criticalBytes.value,expectedRevision:current?current.revision:0});if(stopped)return;thresholds.value=result;thresholdNotice.value='阈值已保存；刷新存储观察后按新阈值显示告警级别。';}catch(e){if(!stopped){thresholdError.value=(e as Error).message;thresholdNotice.value='结果未确认，请重新读取阈值，不自动重试。';}}finally{if(!stopped)savingThresholds.value=false;}}
onMounted(()=>{void loadThresholds();});
onUnmounted(()=>{stopped=true;generation++;});
</script>
