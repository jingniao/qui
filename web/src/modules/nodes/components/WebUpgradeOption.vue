<template>
  <div class="text-xs text-ios-secondary">
    <label class="inline-flex items-center gap-1.5 text-ios-label">
      <input v-model="enabled" type="checkbox" class="accent-ios-blue" />启用网页升级
    </label>
    <p class="mt-1 text-ios-tertiary">勾选后，以后可直接在面板网页上升级 Agent，无需再到服务器运行 qui-update --component agent；不勾选则需要时在服务器上运行一次该命令启用。</p>
  </div>
</template>
<script setup lang="ts">
const enabled = defineModel<boolean>({ required: true });
</script>
