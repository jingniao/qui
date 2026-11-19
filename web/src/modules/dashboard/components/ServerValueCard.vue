<template>
  <!-- Same layout as the other stat cards: title row (the currency picker takes
       the icon's place at the same 32px height), one large figure, then a
       divider and a single footer line. -->
  <section class="panel-card min-w-0 p-5 bg-white flex flex-col justify-between h-full">
    <div>
      <div class="flex items-center justify-between gap-2">
        <span class="text-xs font-semibold text-ios-tertiary">服务器剩余价值</span>
        <AppSelect v-if="ready" v-model="currency" :options="currencyOptions" size="sm" align="right" trigger-class="h-8" class="w-24 shrink-0" />
        <div v-else class="w-8 h-8 rounded-xl bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8c-1.657 0-3 .895-3 2s1.343 2 3 2 3 .895 3 2-1.343 2-3 2m0-8c1.11 0 2.08.402 2.599 1M12 8V7m0 1v8m0 0v1m0-1c-1.11 0-2.08-.402-2.599-1M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
        </div>
      </div>

      <p v-if="nodesError" class="mt-3 text-sm text-ios-red break-words" role="alert">{{ nodesError }}</p>
      <p v-else-if="loading" class="mt-3 text-sm text-ios-secondary" role="status">正在读取财务数据</p>
      <p v-else-if="error" class="mt-3 text-sm text-ios-red break-words" role="alert">{{ error }}</p>
      <!-- The rate table and pricing details stay hidden until the figure is
           hovered or focused; the layer floats below it without resizing the card. -->
      <div v-else-if="exchangeRates" class="group relative mt-3">
        <div class="flex items-baseline gap-1.5 cursor-default rounded-lg outline-none focus-visible:ring-2 focus-visible:ring-ios-blue/40" tabindex="0" aria-describedby="server-value-details">
          <span class="text-3xl font-bold text-ios-label tabular-nums tracking-normal">{{ money(residualMetric) }}</span>
          <span class="text-xs text-ios-tertiary font-medium">/ {{ money(totalMetric) }} 总值</span>
        </div>

        <div class="absolute left-0 right-0 top-full z-20 hidden pt-2 group-hover:block group-focus-within:block">
      <div id="server-value-details" role="tooltip" class="space-y-1 rounded-xl border border-black/[0.08] bg-white p-3 shadow-lg">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <span class="text-xs text-ios-tertiary">今日汇率（1 {{ currency }} 可兑换）</span>
        </div>
        <div class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
          <div v-for="item in CURRENCY_LIST" :key="item.code" class="flex flex-wrap justify-between gap-x-1">
            <span class="text-ios-tertiary">{{ item.code }}</span>
            <span class="font-mono tabular-nums text-ios-label">{{ (exchangeRates.rates[item.code] / exchangeRates.rates[currency]).toFixed(6) }}</span>
          </div>
        </div>
        <div class="text-xs text-ios-tertiary leading-normal space-y-1 pt-1">
          <p class="flex flex-wrap justify-between gap-x-2">
            <span>汇率日期：{{ rateDate }}</span>
            <a :href="exchangeRates.sourceUrl" target="_blank" rel="noopener noreferrer" class="hover:text-ios-blue">{{ exchangeRates.source }}</a>
          </p>
          <p v-if="summary.unpriced">{{ summary.unpriced }} 台未设置价格，不计入</p>
          <p v-if="summary.monthlyMissing || summary.residualMissing" title="月均支出排除不支持的续费周期；剩余价值排除缺少到期日或周期不支持的服务器">
            未计入：月均 {{ summary.monthlyMissing }} 台，剩余 {{ summary.residualMissing }} 台
          </p>
          <p v-for="metric in metrics" :key="metric.label">{{ metric.label }}：{{ metric.description }}</p>
        </div>
      </div>
        </div>
      </div>
    </div>

    <div v-if="ready" class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-xs">
      <span class="text-ios-secondary">月均支出 <span class="font-medium text-ios-label tabular-nums">{{ money(monthlyMetric) }}</span></span>
      <span class="text-ios-tertiary tabular-nums font-medium">已计价 {{ summary.priced }} / {{ metadata.length }} 台</span>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { dashboardAPI, type ExchangeRates } from '../api';
import type { NodeView } from '../../nodes/api';
import {
  CURRENCY_LIST, CURRENCY_SYMBOLS, loadNodeMetadata, parseRenewPrice,
  calculateMonthlyExpenditure, calculateResidualValue, calculateRemainingDays, type NodeMeta
} from '../../nodes/nodeMeta';
import { authState } from '../../../app/auth';
import AppSelect from '../../../components/AppSelect.vue';

const props = defineProps<{ nodes: NodeView[]; nodesError: string }>();
const currency = ref('CNY');
const currencyOptions = CURRENCY_LIST.map(item => ({ value: item.code, label: item.code }));
const metadata = ref<NodeMeta[]>([]);
const exchangeRates = ref<ExchangeRates | null>(null);
const loading = ref(true);
const error = ref('');
const rateDate = computed(() => exchangeRates.value ? new Intl.DateTimeFormat('zh-CN', {
  timeZone: authState.timezone, dateStyle: 'medium'
}).format(new Date(exchangeRates.value.updatedAt)) : '');

const summary = computed(() => {
  const totals = { total: 0, monthly: 0, residual: 0, priced: 0, unpriced: 0, monthlyMissing: 0, residualMissing: 0 };
  if (!exchangeRates.value) return totals;
  const rates = exchangeRates.value.rates;
  for (const meta of metadata.value) {
    const price = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
    if (!price.price) {
      totals.unpriced++;
      continue;
    }
    const amount = Number(price.price);
    const conversion = rates[currency.value] / rates[price.currency];
    totals.total += amount * conversion;
    totals.priced++;
    if (amount === 0) continue;

    const monthly = calculateMonthlyExpenditure(meta);
    if (monthly) totals.monthly += Number(monthly.value) * conversion;
    else if (price.cycle !== '一次性') totals.monthlyMissing++;

    const residual = calculateResidualValue(meta);
    if (residual) totals.residual += Number(residual.value) * conversion;
    else {
      const remaining = calculateRemainingDays(meta.renewDate);
      // Expired recurring contracts have zero residual value; missing dates and
      // one-time purchases cannot be valued by the existing per-day algorithm.
      if (!(monthly && remaining.days !== null && remaining.days <= 0)) totals.residualMissing++;
    }
  }
  return totals;
});

const ready = computed(() => !!exchangeRates.value && !loading.value && !error.value && !props.nodesError);
type Metric = { label: string; description: string; value: number; available: boolean };
function money(metric: Metric): string {
  return metric.available ? `${CURRENCY_SYMBOLS[currency.value]}${metric.value.toFixed(2)}` : '—';
}
const totalMetric = computed(() => metrics.value[0]);
const monthlyMetric = computed(() => metrics.value[1]);
const residualMetric = computed(() => metrics.value[2]);

const metrics = computed<Metric[]>(() => [
  { label: '总价值', description: '各服务器登记的单个续费周期价格合计', value: summary.value.total, available: summary.value.priced > 0 },
  { label: '月均支出', description: '按续费周期折算的月均费用，一次性费用不计入', value: summary.value.monthly, available: summary.value.priced > summary.value.monthlyMissing },
  { label: '剩余价值', description: '按剩余天数与续费周期估算的剩余金额', value: summary.value.residual, available: summary.value.priced > summary.value.residualMissing }
]);

// The dashboard refreshes the server list every 30 seconds. Financial data is
// read once per day in the panel timezone instead: rates and each server's
// pricing stay in this module's cache (which survives in-app navigation) until
// the date changes; a server added since the last read is fetched at once.
// A failed read is not cached, so the next refresh retries it.
function financeDay(): string {
  return new Intl.DateTimeFormat('en-CA', { timeZone: authState.timezone || undefined, dateStyle: 'short' }).format(new Date());
}

watch(() => props.nodes, async (nodes, _previous, onCleanup) => {
  let current = true;
  onCleanup(() => { current = false; });
  const day = financeDay();
  const ids = nodes.filter(node => !node.deletedAt).map(node => node.id);
  const staleIds = ids.filter(id => financeCache.metas.get(id)?.day !== day);
  const ratesStale = financeCache.rates?.day !== day;
  if (!ratesStale && !staleIds.length) {
    metadata.value = ids.map(id => financeCache.metas.get(id)!.meta);
    exchangeRates.value = financeCache.rates!.value;
    error.value = '';
    loading.value = false;
    return;
  }
  // Only the first read (or a retry after an error) shows the loading text;
  // the daily refresh keeps the current figures until the new ones arrive.
  if (!exchangeRates.value) loading.value = true;
  try {
    // A server deleted after the list was read (for example a cancelled pending
    // enrollment) answers 404; it is left out instead of failing the whole card.
    const [fetched, fetchedRates] = await Promise.all([
      staleIds.length ? loadNodeMetadata(false) : Promise.resolve({} as Record<string, NodeMeta>),
      ratesStale ? dashboardAPI.exchangeRates() : Promise.resolve(financeCache.rates!.value)
    ]);
    const fresh = new Map<string, NodeMeta>();
    const gone = new Set<string>();
    staleIds.forEach(id => {
      const meta = fetched[id];
      if (meta) fresh.set(id, meta);
      else gone.add(id);
    });
    const metas = ids.filter(id => !gone.has(id)).map(id => fresh.get(id) ?? financeCache.metas.get(id)!.meta);
    const rates = fetchedRates;
    for (const meta of metas) {
      const price = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
      if (price.price && !(rates.rates[price.currency] > 0)) {
        throw new Error(`未取得 ${price.currency} 汇率，无法计算服务器价值`);
      }
    }
    // Cache only a complete, usable read so a failure is retried on the next refresh.
    fresh.forEach((meta, id) => financeCache.metas.set(id, { day, meta }));
    financeCache.rates = { day, value: rates };
    if (!current) return;
    metadata.value = metas;
    exchangeRates.value = rates;
    error.value = '';
  } catch (e) {
    if (!current) return;
    exchangeRates.value = null;
    error.value = (e as Error).message;
  } finally {
    if (current) loading.value = false;
  }
}, { immediate: true });
</script>

<script lang="ts">
import type { ExchangeRates as CachedRates } from '../api';
import type { NodeMeta as CachedMeta } from '../../nodes/nodeMeta';

// Shared by every mount of the card so leaving and reopening the dashboard
// does not read the financial data again on the same day.
const financeCache: {
  rates: { day: string; value: CachedRates } | null;
  metas: Map<string, { day: string; meta: CachedMeta }>;
} = { rates: null, metas: new Map() };
</script>
