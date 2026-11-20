<template>
  <div class="space-y-4 text-sm">
    <p>当前阶段：{{ phase || result?.phase || '等待服务器执行' }}</p>
    <p v-if="dispatchError" role="alert" class="whitespace-pre-wrap break-words text-red-700">请求投递：{{ dispatchError }}</p>
    <template v-if="result">
      <div v-if="result.error" role="alert" class="whitespace-pre-wrap break-words text-red-700">{{ result.error }}</div>
      <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2">
        <dt>更新器退出码</dt><dd>{{ result.exitCode ?? '未取得' }}</dd>
        <dt>失败命令退出码</dt><dd>{{ result.commandExitCode ?? '未取得' }}{{ result.signal ? ` · ${result.signal}` : '' }}</dd>
        <dt>基础组件安装</dt><dd>{{ stageText(result.bootstrapStage) }}</dd>
        <dt>Agent 安装</dt><dd>{{ stageText(result.agentStage) }}</dd>
        <dt>基础组件版本</dt><dd>{{ versionText(result.bootstrapVersion) }}</dd>
        <dt>已安装 Agent</dt><dd>{{ versionText(result.installedVersion) }}</dd>
        <dt>运行中 Agent</dt><dd>{{ versionText(result.runningVersion) }}</dd>
        <dt>服务状态</dt><dd class="break-words">{{ result.serviceState }}{{ result.serviceError ? `：${result.serviceError}` : '' }}</dd>
        <dt>失败恢复</dt><dd class="break-words">{{ recoveryText }}</dd>
      </dl>
      <p v-for="(message, index) in evidenceErrors" :key="index" class="whitespace-pre-wrap break-words text-red-700">{{ message }}</p>
    </template>
    <div class="flex items-center gap-3">
      <button class="ios-btn-secondary" :disabled="loading" @click="loadOutput">{{ loading ? '读取中…' : '读取后续输出' }}</button>
      <button class="ios-btn-secondary" :disabled="!output" @click="copyOutput">{{ copied ? '已复制' : '复制已读输出' }}</button>
      <a :href="agentUpdatesAPI.logDownload(taskId, attemptId)" class="text-ios-blue">下载完整输出</a>
      <span class="text-xs text-ios-secondary">已读取 {{ offset }} 字节{{ complete ? '，输出已收齐' : '，等待后续输出' }}</span>
    </div>
    <p v-if="error" role="alert" class="whitespace-pre-wrap break-words text-red-700">{{ error }}</p>
    <p v-if="copyError" role="alert" class="whitespace-pre-wrap break-words text-red-700">{{ copyError }}</p>
    <pre class="max-h-[45vh] overflow-auto rounded-xl bg-neutral-950 p-4 text-sm text-neutral-100 whitespace-pre-wrap break-words">{{ output || '暂无任务输出' }}</pre>
  </div>
</template>
<script setup lang="ts">
import { computed, onMounted, ref } from 'vue';
import { agentUpdatesAPI, type AgentSystemUpdateResult, type SystemUpdateStage, type SystemUpdateVersion } from '../agentUpdatesApi';
const props = defineProps<{ taskId: string; attemptId: string; phase?: string; dispatchError?: string; result?: AgentSystemUpdateResult }>();
const output = ref(''), offset = ref(0), error = ref(''), loading = ref(false), complete = ref(false);
const copied = ref(false), copyError = ref('');
const evidenceErrors = computed(() => [props.result?.logError, props.result?.persistenceError].filter(Boolean));
const recoveryText = computed(() => {
  const recovery = props.result?.recovery;
  if (!recovery?.attempted) return '未执行';
  return `${recovery.success === true ? '恢复成功' : recovery.success === false ? '恢复失败' : '结果待确认'}${recovery.error ? `：${recovery.error}` : ''}`;
});
function stageText(stage: SystemUpdateStage): string {
  return `${stage.status}${stage.exitCode !== null ? `（退出码 ${stage.exitCode}）` : ''}${stage.error ? `：${stage.error}` : ''}`;
}
function versionText(version: SystemUpdateVersion): string { return [version.version, version.error].filter(Boolean).join('：') || '未取得'; }
async function copyOutput() {
  copyError.value = ''; copied.value = false;
  try { await navigator.clipboard.writeText(output.value); copied.value = true; }
  catch (e) { copyError.value = `复制失败：${(e as Error).message}`; }
}
async function loadOutput() {
  if (loading.value) return;
  loading.value = true; error.value = '';
  try {
    const result = await agentUpdatesAPI.logs(props.taskId, props.attemptId, offset.value);
    output.value += result.output;
    copied.value = false;
    offset.value = result.nextOffset;
    complete.value = result.complete;
    error.value = result.error;
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
onMounted(loadOutput);
</script>
