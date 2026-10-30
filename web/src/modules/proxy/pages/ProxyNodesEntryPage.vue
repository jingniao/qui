<!-- One root element: MainLayout wraps pages in <transition mode="out-in">,
     and a multi-root page never finishes leaving, which blanks the screen. -->
<template>
  <div>
    <ProxyNodeListPage :key="generation" />
    <ServerNodeCreate v-if="isOwner && route.query.create === '1'" :key="String(route.query.server || '')" :server-id="typeof route.query.server === 'string' ? route.query.server : ''" @close="close" @saved="generation++" />
  </div>
</template>
<script setup lang="ts">
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { isOwner } from '../../../app/auth';
import ProxyNodeListPage from './ProxyNodeListPage.vue';
import ServerNodeCreate from '../components/ServerNodeCreate.vue';
const route = useRoute(), router = useRouter(), generation = ref(0);
function close() { void router.replace({ query: { ...route.query, create: undefined, server: undefined } }); }
</script>
