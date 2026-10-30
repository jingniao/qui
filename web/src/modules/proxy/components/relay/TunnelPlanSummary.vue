<template>
  <div class="space-y-2 text-sm" aria-live="polite">
    <p v-if="checking" class="text-ios-tertiary">检查中…</p>
    <p v-if="error" class="text-ios-red">{{ error }}</p>
    <p v-if="plan?.message" class="text-ios-red">{{ plan.message }}</p>
    <p v-if="plan && !checking && !error" :class="plan.ok ? 'text-ios-green' : 'text-ios-red'">{{ plan.ok ? '计划检查通过' : '计划检查未通过' }}</p>
    <ul v-if="lines.length" class="space-y-2 text-ios-secondary">
      <li v-for="(line, i) in lines" :key="i" class="break-all">{{ line }}</li>
    </ul>
    <p v-else-if="plan?.ok" class="text-ios-tertiary">没有服务器对象变更。</p>
    <p v-for="(conflict, i) in plan?.conflicts || []" :key="`conflict-${i}`" class="text-ios-red font-mono tabular-nums">
      {{ plan?.servers?.find(s => s.nodeId === conflict.nodeId)?.name || conflict.nodeId }}：端口冲突 :{{ conflict.port }}（{{ conflict.with }}）
    </p>
    <p v-for="(warning, i) in plan?.warnings || []" :key="`warning-${i}`" class="text-ios-orange">{{ warning }}</p>
    <p class="text-xs text-ios-tertiary">检查仅读取当前方案，不会部署。实际发布时会再次校验，运行时部署结果以节点回执为准。</p>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { planLines, type TunnelPlan } from './tunnelEditorModel';
const props = defineProps<{ plan: TunnelPlan | null; checking: boolean; error: string; nodeIds?: string[] }>();
const lines = computed(() => planLines(props.plan, props.nodeIds));
</script>
