<template>
  <div v-if="summary" class="flex flex-wrap items-center gap-1.5 text-xs text-ios-secondary">
    <span class="rounded px-1.5 py-0.5 font-semibold" :class="summary.type === 'relay' ? 'bg-ios-blue/10 text-ios-blue' : 'bg-ios-bg text-ios-secondary'">{{ routeSummaryLabel(summary) }}</span>
    <span v-if="routeSummaryPath(summary)" class="break-words">{{ routeSummaryPath(summary) }}</span>
    <span v-if="summary.pathIncomplete" class="text-ios-orange">路径无法解析，请在画布中检查方案</span>
    <RouterLink v-if="showSchemeLink && routeSummaryLink(summary)" :to="routeSummaryLink(summary)!" class="text-ios-blue hover:underline" @click.stop>所属方案：{{ summary.schemeName }}</RouterLink>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router';
import type { RouteSummary } from '../api';
import { routeSummaryLabel, routeSummaryPath, routeSummaryLink } from '../routeSummary';

withDefaults(defineProps<{ summary?: RouteSummary; showSchemeLink?: boolean }>(), { showSchemeLink: true });
</script>
