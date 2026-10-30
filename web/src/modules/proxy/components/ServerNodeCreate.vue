<template>
  <NodeEditorDrawer :server-id="serverId" origin="server" @close="emit('close')" @saved="emit('saved')" />
</template>
<script setup lang="ts">
import NodeEditorDrawer from './node-editor/NodeEditorDrawer.vue';
defineProps<{ serverId: string }>();
const emit = defineEmits<{ close: []; saved: [] }>();
</script>
