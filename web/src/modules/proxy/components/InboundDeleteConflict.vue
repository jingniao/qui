<template>
  <div v-if="conflict" role="alert" class="rounded-xl border border-red-200 p-4 text-sm space-y-2">
    <p>{{ conflict.message }}</p>
    <ul><li v-for="outbound in conflict.outbounds" :key="outbound.id">{{ outbound.name }}</li></ul>
    <p v-for="scheme in conflict.schemes" :key="scheme.id">{{ scheme.name }} <RouterLink :to="{ path: '/proxy/relay', query: { schemeId: scheme.id } }" class="text-ios-blue">在画布中查看</RouterLink></p>
  </div>
</template>
<script setup lang="ts">
import type { InboundConflict } from '../inboundDeleteConflict';
defineProps<{ conflict: InboundConflict | null }>();
</script>
