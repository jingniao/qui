<template>
    <!-- Import result details: the bounded reasons of the last attempt. Rendered
         from the stored result, so a failure with no recorded cause says so. -->
    <AppDialog
      :model-value="true"
      :title="`导入结果 · ${importDetails.name}`"
      :show-confirm="false"
      @update:model-value="emit('close')"
    >
      <div class="space-y-3 text-xs">
        <p class="text-ios-secondary">{{ describeImportResult(importDetails.result) }}</p>
        <p class="font-mono text-xs text-ios-tertiary">{{ importCountsLine(importDetails.result) }}</p>
        <div v-if="importDetails.result.diagnostics.length" class="space-y-1.5 max-h-[320px] overflow-y-auto pr-1">
          <div
            v-for="(item, index) in importDetails.result.diagnostics"
            :key="index"
            class="p-2 rounded-xl bg-black/[0.02] border border-black/[0.06] space-y-0.5"
          >
            <div class="flex flex-wrap items-center gap-1.5">
              <span class="px-1.5 py-0.5 rounded-md bg-black/[0.04] text-xs font-mono">#{{ item.entryIndex + 1 }}</span>
              <span v-if="item.protocol" class="text-xs text-ios-secondary font-mono">{{ item.protocol }}</span>
              <span
                class="px-1.5 py-0.5 rounded-md text-xs"
                :class="item.stage === 'duplicate' ? 'bg-sky-500/10 text-sky-700' : 'bg-amber-500/10 text-amber-700'"
              >
                {{ item.stage === 'duplicate' ? '重复' : '解析' }}
              </span>
              <span class="text-xs text-ios-tertiary font-mono">{{ item.code }}</span>
            </div>
            <p class="text-xs leading-relaxed text-ios-label">{{ item.message }}</p>
            <p v-if="item.field" class="text-xs text-ios-tertiary">字段：{{ item.field }}</p>
            <p v-if="item.keptEntryIndex !== undefined" class="text-xs text-ios-tertiary">保留第 {{ item.keptEntryIndex + 1 }} 条</p>
          </div>
        </div>
        <p v-else class="text-xs text-ios-tertiary">该次尝试没有逐条原因。</p>
        <p v-if="importDetails.result.diagnosticsTruncated" class="text-xs text-amber-700">
          原因过多，仅保留前 {{ importDetails.result.diagnostics.length }} 条。
        </p>
      </div>
    </AppDialog>
</template>

<script setup lang="ts">
import AppDialog from '../../../components/AppDialog.vue';
import { describeImportResult, importCountsLine, type ImportResult } from '../importApi';

defineProps<{ importDetails: { name: string; result: ImportResult } }>();
const emit = defineEmits<{ close: [] }>();
</script>
