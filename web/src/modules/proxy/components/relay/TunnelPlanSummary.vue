<template>
  <div class="space-y-2 text-sm" aria-live="polite">
    <p v-if="checking" class="text-ios-tertiary">检查中…</p>
    <p v-if="error" class="text-ios-red">{{ error }}</p>
    <p v-if="plan?.message" class="text-ios-red">{{ plan.message }}</p>
    <p v-if="plan && !checking && !error" :class="plan.ok ? 'text-ios-green' : 'text-ios-red'">
      {{ plan.ok ? '计划检查通过' : '计划检查未通过，发布已被阻止' }}
    </p>
    <ul v-if="lines.length" class="space-y-2 text-ios-secondary">
      <li v-for="(line, i) in lines" :key="i" class="break-all">{{ line }}</li>
    </ul>
    <p v-else-if="plan?.ok" class="text-ios-tertiary">没有服务器对象变更。</p>
    <ul v-if="tunnelLines.length" class="space-y-1 text-ios-secondary">
      <li v-for="(line, i) in tunnelLines" :key="`tunnel-${i}`" class="break-all">{{ line }}</li>
    </ul>
    <ul v-if="conflicts.length" class="space-y-1">
      <li v-for="(conflict, i) in conflicts" :key="`conflict-${i}`" class="text-ios-red tabular-nums break-all">
        {{ conflictLabel(conflict) }}
      </li>
    </ul>
    <p v-if="blocked" class="text-ios-red">
      存在 {{ conflicts.length }} 个冲突，必须先解决才能发布。历史对象未认领时，请在上方「待认领的历史对象」中认领，或先在旧方案中发布清理。
    </p>
    <p v-for="(warning, i) in plan?.warnings || []" :key="`warning-${i}`" class="text-ios-orange">{{ warning }}</p>
    <p class="text-xs text-ios-tertiary">检查仅读取当前方案，不会部署。实际发布时会再次校验，运行时部署结果以节点回执为准。</p>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { planLines, tunnelConflictText, tunnelPlanLines, type PlanConflict, type TunnelPlan } from './tunnelEditorModel';
const props = defineProps<{
  plan: TunnelPlan | null;
  checking: boolean;
  error: string;
  nodeIds?: string[];
  // Tunnel ids named by the plan, so affected schemes can be read as names.
  tunnelNames?: Record<string, string>;
}>();
const lines = computed(() => planLines(props.plan, props.nodeIds));
const tunnelLines = computed(() => tunnelPlanLines(props.plan, id => props.tunnelNames?.[id] || ''));
const conflicts = computed(() => props.plan?.conflicts || []);
const blocked = computed(() => !!props.plan && !props.checking && (!props.plan.ok || conflicts.value.length > 0));
function conflictLabel(conflict: PlanConflict): string {
  const node = conflict.nodeId
    ? `${props.plan?.servers?.find(server => server.nodeId === conflict.nodeId)?.name || conflict.nodeId}：`
    : '';
  return node + tunnelConflictText(conflict);
}
</script>
