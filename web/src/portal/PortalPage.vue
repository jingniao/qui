<template>
  <div class="space-y-6">
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading" />
    <template v-else-if="data">
      <PortalOverview v-if="section === 'overview'" :me="data.me" :subscription="data.subscription" :addresses="onlineAddresses" :preview="preview" />
      <PortalSubscription v-else-if="section === 'subscription'" :subscription="data.subscription" :preview="preview" @update="data.subscription = $event" />
      <PortalNodes v-else-if="section === 'nodes'" :items="data.nodes.items" />
      <PortalTraffic v-else-if="section === 'traffic'" :traffic="traffic" :range="range" :preview="preview" @range="range = $event" />
      <PortalAccount v-else-if="section === 'account'" :me="data.me" :preview="preview" />
    </template>
  </div>
</template>
<script setup lang="ts">
import { ref, watch } from 'vue';
import { portalAPI, type Snapshot, type Traffic } from './api';
import ErrorState from '../components/ErrorState.vue';
import LoadingSkeleton from '../components/LoadingSkeleton.vue';
import PortalOverview from './PortalOverview.vue';
import PortalSubscription from './PortalSubscription.vue';
import PortalNodes from './PortalNodes.vue';
import PortalTraffic from './PortalTraffic.vue';
import PortalAccount from './PortalAccount.vue';
const props = defineProps<{ section: string; preview?: boolean; snapshot?: Snapshot }>();
const data = ref<Snapshot>(), loading = ref(false), error = ref('');
const onlineAddresses = ref<{ address: string; allowed: boolean; active: boolean }[]>([]);
const range = ref<'24h' | '7d' | '30d'>('7d'), traffic = ref<Traffic>({ items: [], granularity: 'daily' });
async function load() {
  error.value = ''; loading.value = true;
  try {
    if (props.preview) { data.value = props.snapshot; return; }
    const [me, nodes, subscription] = await Promise.all([portalAPI.me(), portalAPI.nodes(), portalAPI.subscription()]);
    data.value = { me, nodes, subscription };
    onlineAddresses.value = props.section === 'overview' ? (await portalAPI.devicesOnline()).items : [];
    if (props.section === 'traffic') traffic.value = await portalAPI.traffic(range.value);
  } catch (e) { error.value = (e as Error).message; }
  finally { loading.value = false; }
}
watch(() => [props.section, props.snapshot, range.value], () => void load(), { immediate: true });
</script>
