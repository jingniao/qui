<template>
  <section class="panel-card min-w-0 p-5 bg-white space-y-2">
    <div class="flex items-center justify-between gap-2">
      <span class="text-xs font-semibold text-ios-tertiary">服务器剩余价值</span>
      <AppSelect v-if="exchangeRates && !loading && !error && !nodesError" v-model="currency" :options="currencyOptions" size="sm" align="right" class="w-24" />
    </div>

    <p v-if="nodesError" class="text-sm text-ios-red break-words" role="alert">{{ nodesError }}</p>
    <p v-else-if="loading" class="text-sm text-ios-secondary" role="status">正在读取财务数据</p>
    <p v-else-if="error" class="text-sm text-ios-red break-words" role="alert">{{ error }}</p>
    <template v-else-if="exchangeRates">
      <div class="grid grid-cols-3 gap-2">
        <div v-for="metric in metrics" :key="metric.label" class="min-w-0 space-y-1">
          <div class="text-xs text-ios-tertiary" :title="metric.description">{{ metric.label }}</div>
          <div class="text-base font-bold font-mono tabular-nums text-ios-label break-all">
            {{ metric.available ? `${CURRENCY_SYMBOLS[currency]}${metric.value.toFixed(2)}` : '—' }}
          </div>
        </div>
      </div>

      <div class="space-y-1 border-t border-black/[0.04] pt-2">
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
          <p>已计价 {{ summary.priced }} / {{ metadata.length }} 台<span v-if="summary.unpriced">，{{ summary.unpriced }} 台未设置价格</span></p>
          <p v-if="summary.monthlyMissing || summary.residualMissing" title="月均支出排除不支持的续费周期；剩余价值排除缺少到期日或周期不支持的服务器">
            未计入：月均 {{ summary.monthlyMissing }} 台，剩余 {{ summary.residualMissing }} 台
          </p>
        </div>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import { dashboardAPI, type ExchangeRates } from '../api';
import type { NodeView } from '../../nodes/api';
import {
  CURRENCY_LIST, CURRENCY_SYMBOLS, loadNodeMeta, parseRenewPrice,
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

const metrics = computed(() => [
  { label: '总价值', description: '各服务器登记的单个续费周期价格合计', value: summary.value.total, available: summary.value.priced > 0 },
  { label: '月均支出', description: '按续费周期折算的月均费用，一次性费用不计入', value: summary.value.monthly, available: summary.value.priced > summary.value.monthlyMissing },
  { label: '剩余价值', description: '按剩余天数与续费周期估算的剩余金额', value: summary.value.residual, available: summary.value.priced > summary.value.residualMissing }
]);

watch(() => props.nodes, async (nodes, _previous, onCleanup) => {
  let current = true;
  onCleanup(() => { current = false; });
  loading.value = true;
  error.value = '';
  try {
    const [metas, rates] = await Promise.all([
      Promise.all(nodes.filter(node => !node.deletedAt).map(node => loadNodeMeta(node.id, false))),
      dashboardAPI.exchangeRates()
    ]);
    for (const meta of metas) {
      const price = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
      if (price.price && !(rates.rates[price.currency] > 0)) {
        throw new Error(`未取得 ${price.currency} 汇率，无法计算服务器价值`);
      }
    }
    if (!current) return;
    metadata.value = metas;
    exchangeRates.value = rates;
  } catch (e) {
    if (!current) return;
    exchangeRates.value = null;
    error.value = (e as Error).message;
  } finally {
    if (current) loading.value = false;
  }
}, { immediate: true });
</script>
