<template>
  <div class="space-y-3 text-sm text-ios-secondary">
    <section v-if="groups.length">
      <p>将同时删除：</p>
      <div v-for="group in groups" :key="group.label" class="mt-3">
        <h4 class="text-base font-semibold">{{ group.label }}</h4>
        <ul class="mt-1 space-y-1"><li v-for="item in group.items" :key="item.id" class="break-words">{{ item.name }}</li></ul>
      </div>
    </section>
    <section v-if="preview.blockers.length">
      <p>以下引用需先处理：</p>
      <ul class="mt-2 space-y-1"><li v-for="item in preview.blockers" :key="item.kind + item.id" class="break-words">{{ blockerLabel(item.kind) }} · {{ item.name }}</li></ul>
    </section>
  </div>
</template>
<script setup lang="ts">
import { computed } from 'vue';
import { deletionGroups, type DeletionPreview } from '../app/deletion';
const props = defineProps<{ preview: DeletionPreview }>();
const groups = computed(() => deletionGroups(props.preview));
function blockerLabel(kind: string): string {
  const labels: Record<string, string> = {
    inbound: '入站', outbound: '出站', outbound_group: '出口组', outbound_target: '其他服务器的出站',
    proxy_chain: '代理链', policy: '分流规则', deployment_batch: '进行中的批量发布',
  };
  return labels[kind] ?? kind;
}
</script>
