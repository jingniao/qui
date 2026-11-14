<template>
  <div class="space-y-6">
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !ready" />
    <template v-else-if="ready">
      <PortalOverview v-if="section === 'overview'" :me="me!" :subscription="subscription!" :addresses="onlineAddresses" :preview="preview" />
      <PortalSubscription v-else-if="section === 'subscription'" :subscription="subscription!" :preview="preview" @update="subscription = $event" />
      <PortalNodes v-else-if="section === 'nodes'" :items="nodes" />
      <PortalTraffic v-else-if="section === 'traffic'" :traffic="traffic" :range="range" :preview="preview" @range="range = $event" />
      <PortalAccount v-else-if="section === 'account'" :me="me!" :preview="preview" />
    </template>
  </div>
</template>
<script setup lang="ts">
import { ref, watch, computed, defineAsyncComponent, onUnmounted } from 'vue';
import { portalAPI, type Snapshot, type Traffic, type Profile, type Subscription, type PortalNode } from './api';
import ErrorState from '../components/ErrorState.vue';
import LoadingSkeleton from '../components/LoadingSkeleton.vue';
const PortalOverview = defineAsyncComponent(() => import('./PortalOverview.vue'));
const PortalSubscription = defineAsyncComponent(() => import('./PortalSubscription.vue'));
const PortalNodes = defineAsyncComponent(() => import('./PortalNodes.vue'));
const PortalTraffic = defineAsyncComponent(() => import('./PortalTraffic.vue'));
const PortalAccount = defineAsyncComponent(() => import('./PortalAccount.vue'));
const props = defineProps<{ section: string; preview?: boolean; snapshot?: Snapshot }>();
const me = ref<Profile>(), subscription = ref<Subscription>(), nodes = ref<PortalNode[]>([]);
const loading = ref(false), error = ref('');
const loadedSection = ref('');
const ready = computed(() => loadedSection.value === props.section);
const onlineAddresses = ref<{ address: string; allowed: boolean; active: boolean }[]>([]);
const range = ref<'24h' | '7d' | '30d'>('7d'), traffic = ref<Traffic>({ items: [], granularity: 'daily' });
let generation = 0, trafficGeneration = 0;
async function loadTraffic() {
  const current = ++trafficGeneration;
  try {
    const result = await portalAPI.traffic(range.value);
    if (current === trafficGeneration) { traffic.value = result; error.value = ''; }
  } catch (e) { if (current === trafficGeneration) error.value = (e as Error).message; }
}
async function load() {
  const current = ++generation, section = props.section;
  trafficGeneration++;
  error.value = ''; loading.value = true;
  try {
    if (props.preview) {
      if (props.snapshot) { me.value = props.snapshot.me; nodes.value = props.snapshot.nodes.items; subscription.value = props.snapshot.subscription; }
    } else if (section === 'overview') {
      const [profile, sub, devices] = await Promise.all([portalAPI.me(), portalAPI.subscription(), portalAPI.devicesOnline()]);
      if (current !== generation) return;
      me.value = profile; subscription.value = sub; onlineAddresses.value = devices.items;
    } else if (section === 'subscription') {
      const result = await portalAPI.subscription(); if (current !== generation) return; subscription.value = result;
    } else if (section === 'nodes') {
      const result = await portalAPI.nodes(); if (current !== generation) return; nodes.value = result.items;
    } else if (section === 'account') {
      const result = await portalAPI.me(); if (current !== generation) return; me.value = result;
    } else if (section === 'traffic') { await loadTraffic(); }
    if (current === generation) loadedSection.value = section;
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { if (current === generation) loading.value = false; }
}
watch(() => [props.section, props.snapshot], () => void load(), { immediate: true });
watch(range, () => { if (!props.preview && props.section === 'traffic') void loadTraffic(); });
onUnmounted(() => { generation++; trafficGeneration++; });
</script>
