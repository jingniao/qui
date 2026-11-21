<template>
  <div class="text-xs text-ios-secondary">
    <label class="inline-flex items-center gap-1.5 text-ios-label">
      <input v-model="enabled" type="checkbox" class="accent-ios-blue" />启用网页升级
    </label>
    <p class="mt-1 text-ios-tertiary">之后可直接在面板网页升级 Agent；不勾选则需先在服务器运行一次 <code class="font-mono">qui-update --component agent</code>。</p>
  </div>
</template>
<script setup lang="ts">
const enabled = defineModel<boolean>({ required: true });
</script>
