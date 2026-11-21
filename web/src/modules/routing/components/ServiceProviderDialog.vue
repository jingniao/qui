<template>
    <AppDialog
      v-model="providerModalVisible"
      :title="`规则集【${selectedService?.name || ''}】的 Clash / Mihomo 引用`"
      :show-footer="false"
      class="max-w-2xl"
    >
      <div class="space-y-4">
        <p class="text-xs text-neutral-500 leading-relaxed">
          在「客户端策略」中用 <code class="font-mono text-ios-blue">RULE-SET</code> 选择此规则集后，用户的 Clash / mihomo 订阅会自动带上对应的 rule-provider，无需手工配置。地址以用户自己的订阅 Token 鉴权，下列信息供手工配置时参考：
        </p>

        <div class="relative">
          <pre class="bg-neutral-900 text-neutral-100 p-4 rounded-2xl text-xs font-mono overflow-x-auto leading-relaxed">{{ providerYamlSnippet }}</pre>
          <button
            type="button"
            @click="copySnippet"
            class="absolute top-3 right-3 px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white text-xs font-medium backdrop-blur-md transition-colors"
          >
            {{ snippetCopied ? '已复制！' : '复制代码' }}
          </button>
        </div>

        <div class="space-y-1.5 pt-1">
          <span class="text-xs font-medium text-neutral-600">直接 URL 地址:</span>
          <div class="flex items-center gap-2">
            <input
              readonly
              :value="providerDirectUrl"
              class="form-input-base font-mono text-neutral-700 bg-neutral-50"
            />
            <button type="button" @click="copyUrl" class="ios-btn-secondary shrink-0 py-2">
              复制 URL
            </button>
          </div>
        </div>

        <div class="flex justify-end pt-2">
          <button type="button" @click="providerModalVisible = false" class="ios-btn-secondary">
            关闭
          </button>
        </div>
      </div>
    </AppDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import AppDialog from '../../../components/AppDialog.vue';
import { showToast } from '../../../app/api';
import type { ServiceSet } from '../api';

const providerModalVisible = defineModel<boolean>();
const props = defineProps<{ service: ServiceSet | null }>();
const selectedService = computed(() => props.service);
const snippetCopied = ref(false);
watch(providerModalVisible, () => { snippetCopied.value = false; });

const providerDirectUrl = computed(() => {
  if (!selectedService.value) return '';
  return `${window.location.origin}/sub/<订阅 Token>/rules/${selectedService.value.id}`;
});

const providerYamlSnippet = computed(() => {
  if (!selectedService.value) return '';
  return [
    `名称: qui-service-${selectedService.value.id}`,
    '类型: http · behavior: classical · format: text',
    `地址: ${providerDirectUrl.value}`,
    '刷新: 86400 秒'
  ].join('\n');
});

async function copySnippet() {
  await navigator.clipboard.writeText(providerYamlSnippet.value);
  snippetCopied.value = true;
  showToast('规则集引用信息已复制');
  setTimeout(() => {
    snippetCopied.value = false;
  }, 2500);
}

async function copyUrl() {
  await navigator.clipboard.writeText(providerDirectUrl.value);
  showToast('Provider URL 已复制');
}

</script>
