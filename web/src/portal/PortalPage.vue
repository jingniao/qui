<template>
  <div class="space-y-6">
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-if="loading && !ready" />
    <template v-else-if="ready">
      <PortalOverview v-if="section === 'overview'" :me="me!" :subscription="subscription!" :addresses="onlineAddresses" :preview="preview" />
      <PortalSubscription v-else-if="section === 'subscription'" :subscription="subscription!" :preview="preview" @update="subscription = $event" />
      <PortalNodes v-else-if="section === 'nodes'" :items="nodes" />
      <PortalTraffic v-else-if="section === 'traffic'" :traffic="traffic" :range="displayedRange" :preview="preview" @range="selectRange" />
      <PortalAccount v-else-if="section === 'account'" :me="me!" :preview="preview" />
    </template>
  </div>
</template>
<script setup lang="ts">
import { readTogether } from '../app/readTogether';
import { ref, watch, computed, defineAsyncComponent, onMounted, onUnmounted } from 'vue';
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
const displayedRange = ref(range.value);
let generation = 0, trafficGeneration = 0;
async function loadTraffic() {
  const current = ++trafficGeneration, requested = range.value;
  try {
    const result = await portalAPI.traffic(requested);
    if (current === trafficGeneration) { traffic.value = result; displayedRange.value = requested; loadedSection.value = 'traffic'; error.value = ''; }
  } catch (e) { if (current === trafficGeneration) showReadError(e); }
}
function showReadError(errorValue: unknown) {
  if ([401, 403, 404].includes((errorValue as { status: number }).status)) {
    loadedSection.value = ''; me.value = undefined; subscription.value = undefined;
    nodes.value = []; onlineAddresses.value = []; traffic.value = { items: [], granularity: 'daily' };
  }
  error.value = (errorValue as Error).message;
}
async function load() {
  const current = ++generation, section = props.section;
  trafficGeneration++;
  error.value = ''; loading.value = true;
  try {
    if (props.preview) {
      if (props.snapshot) { me.value = props.snapshot.me; nodes.value = props.snapshot.nodes.items; subscription.value = props.snapshot.subscription; }
    } else if (section === 'overview') {
      const [profile, sub, devices] = await readTogether([portalAPI.me(), portalAPI.subscription(), portalAPI.devicesOnline()]);
      if (current !== generation) return;
      me.value = profile; subscription.value = sub; onlineAddresses.value = devices.items;
    } else if (section === 'subscription') {
      const result = await portalAPI.subscription(); if (current !== generation) return; subscription.value = result;
    } else if (section === 'nodes') {
      const result = await portalAPI.nodes(); if (current !== generation) return; nodes.value = result.items;
    } else if (section === 'account') {
      const result = await portalAPI.me(); if (current !== generation) return; me.value = result;
    } else if (section === 'traffic') { await loadTraffic(); return; }
    if (current === generation) loadedSection.value = section;
  } catch (e) { if (current === generation) showReadError(e); }
  finally { if (current === generation) loading.value = false; }
}
watch(() => [props.section, props.snapshot], () => void load(), { immediate: true });
function selectRange(next: typeof range.value) {
  range.value = next;
  if (!props.preview && props.section === 'traffic') void loadTraffic();
}
function refreshNodes() {
  if (!props.preview && props.section === 'nodes' && !document.hidden && !loading.value) void load();
}
let nodeTimer: ReturnType<typeof setInterval>;
onMounted(() => {
  nodeTimer = setInterval(refreshNodes, 30000);
  document.addEventListener('visibilitychange', refreshNodes);
});
onUnmounted(() => {
  generation++; trafficGeneration++;
  clearInterval(nodeTimer);
  document.removeEventListener('visibilitychange', refreshNodes);
});
</script>
