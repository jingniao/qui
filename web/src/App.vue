<template>
  <router-view v-if="!denied" :key="route.meta.adminPage ? adminReadScope : undefined" />
</template>

<script setup lang="ts">
import { computed, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { viewerCanOpen } from './app/guards';
import { adminReadScope, isViewer } from './app/auth';
const route = useRoute(), router = useRouter();
const denied = computed(() => route.meta.adminPage && (!adminReadScope.value || (isViewer.value && !viewerCanOpen(route.path))));
watch(denied, value => { if (value) void router.replace(adminReadScope.value ? '/403' : '/'); });
</script>
