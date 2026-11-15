<template>
  <!-- Inline: the node card's single info line; the scheme moves into the path's link and tooltip. -->
  <span v-if="summary && inline" class="inline-flex min-w-0 items-center gap-1.5 text-xs text-ios-secondary">
    <span v-if="showType" class="shrink-0 rounded px-1.5 py-0.5 font-semibold" :class="summary.type === 'relay' ? 'bg-ios-blue/10 text-ios-blue' : 'bg-ios-bg text-ios-secondary'">{{ routeSummaryLabel(summary) }}</span>
    <span v-if="summary.pathIncomplete" class="shrink-0 text-ios-orange" title="路径无法解析，请在画布中检查方案">路径无法解析</span>
    <template v-else-if="routeSummaryPath(summary)">
      <RouterLink v-if="showSchemeLink && routeSummaryLink(summary)" :to="routeSummaryLink(summary)!" class="truncate text-ios-blue hover:underline" :title="inlineTitle(summary)" @click.stop>{{ routeSummaryPath(summary) }}</RouterLink>
      <span v-else class="truncate" :title="inlineTitle(summary)">{{ routeSummaryPath(summary) }}</span>
    </template>
  </span>
  <div v-else-if="summary" class="flex flex-wrap items-center gap-1.5 text-xs text-ios-secondary">
    <span v-if="showType" class="rounded px-1.5 py-0.5 font-semibold" :class="summary.type === 'relay' ? 'bg-ios-blue/10 text-ios-blue' : 'bg-ios-bg text-ios-secondary'">{{ routeSummaryLabel(summary) }}</span>
    <span v-if="routeSummaryPath(summary)" class="break-words">{{ routeSummaryPath(summary) }}</span>
    <span v-if="summary.pathIncomplete" class="text-ios-orange">路径无法解析，请在画布中检查方案</span>
    <RouterLink v-if="showSchemeLink && routeSummaryLink(summary)" :to="routeSummaryLink(summary)!" class="text-ios-blue hover:underline" @click.stop>所属方案：{{ summary.schemeName }}</RouterLink>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router';
import type { RouteSummary } from '../api';
import { routeSummaryLabel, routeSummaryPath, routeSummaryLink } from '../routeSummary';

withDefaults(defineProps<{ summary?: RouteSummary; showType?: boolean; showSchemeLink?: boolean; inline?: boolean }>(), { showType: true, showSchemeLink: true, inline: false });

function inlineTitle(summary: RouteSummary): string {
  const path = routeSummaryPath(summary);
  return summary.schemeName ? `${path}（所属方案：${summary.schemeName}）` : path;
}
</script>
