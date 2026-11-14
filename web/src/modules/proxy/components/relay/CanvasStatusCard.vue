<template>
  <section class="rounded-xl border border-black/[0.08] bg-white p-3 space-y-3 text-sm" aria-label="方案分项状态">
    <div class="flex items-center justify-between gap-2">
      <h3 class="text-base font-semibold leading-normal text-ios-label">方案状态</h3>
      <button type="button" class="text-sm text-ios-blue font-medium active:scale-[0.98]" :disabled="loading" @click="refreshKey++">刷新</button>
    </div>
    <p v-if="loading" role="status" class="text-ios-secondary">正在读取方案状态…</p>
    <p v-else-if="error" role="alert" class="text-ios-red break-words">{{ error }}</p>
    <dl v-else class="divide-y divide-black/[0.06]">
      <div v-for="item in items" :key="item.key" class="py-2 space-y-1">
        <dt class="font-medium text-ios-label">{{ item.label }}</dt>
        <dd :class="toneClasses[item.tone]">{{ item.value }}</dd>
        <dd v-if="item.details.length">
          <ul class="space-y-1 text-xs text-ios-secondary break-words font-mono tabular-nums">
            <li v-for="(detail, index) in item.details" :key="index">{{ detail }}</li>
          </ul>
        </dd>
        <dd v-if="item.note" class="text-xs text-ios-secondary leading-relaxed">{{ item.note }}</dd>
      </div>
    </dl>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue';
import { canvasAPI, type CanvasSchemeStatus } from '../../api';
import { canvasBatchInProgress, canvasStatusItems } from '../../canvasStatus';
import { pollCanvasStatus } from '../../canvasStatusPolling';

const props = defineProps<{ schemeId: string; version?: string; publishedAt?: string; refresh: number }>();
const status = ref<CanvasSchemeStatus | null>(null);
const loading = ref(false);
const error = ref('');
const refreshKey = ref(0);
let displayedScheme = '';
const visible = ref(!document.hidden);
function visibilityChanged() { visible.value = !document.hidden; }
document.addEventListener('visibilitychange', visibilityChanged);
onUnmounted(() => document.removeEventListener('visibilitychange', visibilityChanged));
const items = computed(() => status.value ? canvasStatusItems(status.value) : []);
const toneClasses = { neutral: 'text-ios-secondary', success: 'text-ios-green', warning: 'text-ios-orange', error: 'text-ios-red' };

watch(() => [props.schemeId, props.version, props.publishedAt, props.refresh, refreshKey.value, visible.value], (_, __, onCleanup) => {
  if (!visible.value) return;
  if (displayedScheme !== props.schemeId) status.value = null;
  displayedScheme = props.schemeId;
  error.value = '';
  loading.value = !status.value;
  const id = props.schemeId;
  const stop = pollCanvasStatus(
    () => canvasAPI.status(id),
    value => { status.value = value; loading.value = false; },
    cause => { error.value = `读取方案状态失败：${(cause as Error).message}`; loading.value = false; },
    canvasBatchInProgress
  );
  // Vue runs this cleanup on a scheme/version change and when unmounting.
  onCleanup(stop);
}, { immediate: true });
</script>
