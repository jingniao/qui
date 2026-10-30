<template>
  <div class="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-4 gap-3">
    <!-- 节点价格 -->
    <div class="panel-card p-3.5 sm:p-4 bg-white flex flex-col justify-between space-y-2 transition-all duration-300 hover:-translate-y-0.5 hover:shadow-sm">
      <div class="flex items-center justify-between text-xs text-ios-secondary font-medium">
        <span>节点价格</span>
        <svg class="w-3.5 h-3.5 text-ios-tertiary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <rect x="2" y="4" width="20" height="16" rx="2" stroke-width="1.5"></rect>
          <line x1="2" y1="10" x2="22" y2="10" stroke-width="1.5"></line>
        </svg>
      </div>
      <div class="text-xl sm:text-2xl font-bold font-mono tracking-tight text-ios-label tabular-nums">
        <template v-if="priceInfo">
          <span>{{ priceInfo.symbol }}{{ priceInfo.price }}</span>
          <span v-if="priceInfo.cycle" class="text-xs text-ios-secondary font-normal font-sans ml-1">/ {{ priceInfo.cycle }}</span>
        </template>
        <span v-else class="text-ios-tertiary font-normal text-base">未设置</span>
      </div>
    </div>

    <!-- 月均支出 -->
    <div class="panel-card p-3.5 sm:p-4 bg-white flex flex-col justify-between space-y-2 transition-all duration-300 hover:-translate-y-0.5 hover:shadow-sm">
      <div class="flex items-center justify-between text-xs text-ios-secondary font-medium">
        <span>月均支出</span>
        <svg class="w-3.5 h-3.5 text-ios-tertiary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 14l6-6m-5.5.5h.01m4.99 5h.01M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16l3.5-2 3.5 2 3.5-2 3.5 2z"></path>
        </svg>
      </div>
      <div class="text-xl sm:text-2xl font-bold font-mono tracking-tight text-ios-label tabular-nums">
        <template v-if="monthly">
          <span>{{ monthly.symbol }}{{ monthly.value }} {{ monthly.currency }}</span>
          <span class="text-xs text-ios-secondary font-normal font-sans ml-1">/ 月</span>
        </template>
        <span v-else class="text-ios-tertiary font-normal text-base">—</span>
      </div>
    </div>

    <!-- 剩余时间 -->
    <div class="panel-card p-3.5 sm:p-4 bg-white flex flex-col justify-between space-y-2 transition-all duration-300 hover:-translate-y-0.5 hover:shadow-sm">
      <div class="flex items-center justify-between text-xs text-ios-secondary font-medium">
        <span>剩余时间</span>
        <svg class="w-3.5 h-3.5 text-ios-tertiary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <rect x="3" y="4" width="18" height="18" rx="2" stroke-width="1.5"></rect>
          <line x1="16" y1="2" x2="16" y2="6" stroke-width="1.5"></line>
          <line x1="8" y1="2" x2="8" y2="6" stroke-width="1.5"></line>
          <line x1="3" y1="10" x2="21" y2="10" stroke-width="1.5"></line>
        </svg>
      </div>
      <div class="text-xl sm:text-2xl font-bold font-mono tracking-tight tabular-nums">
        <template v-if="remaining.days !== null">
          <span v-if="remaining.days > 0" class="text-emerald-500 font-bold">{{ remaining.days }}</span>
          <span v-else-if="remaining.days === 0" class="text-amber-500 font-bold">今日到期</span>
          <span v-else class="text-rose-500 font-bold">已逾期 {{ Math.abs(remaining.days) }}</span>
          <span v-if="remaining.days !== 0" class="text-xs text-ios-secondary font-normal font-sans ml-1">天</span>
        </template>
        <span v-else class="text-ios-tertiary font-normal text-base">—</span>
      </div>
    </div>

    <!-- 剩余价值 -->
    <div class="panel-card p-3.5 sm:p-4 bg-white flex flex-col justify-between space-y-2 transition-all duration-300 hover:-translate-y-0.5 hover:shadow-sm">
      <div class="flex items-center justify-between text-xs text-ios-secondary font-medium">
        <span>剩余价值</span>
        <svg class="w-3.5 h-3.5 text-ios-tertiary" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <circle cx="8" cy="8" r="6" stroke-width="1.5"></circle>
          <path d="M18 12a6 6 0 0 1-6 6" stroke-width="1.5"></path>
        </svg>
      </div>
      <div class="text-xl sm:text-2xl font-bold font-mono tracking-tight text-ios-label tabular-nums">
        <template v-if="residual">
          <span>{{ residual.symbol }}{{ residual.value }}</span>
          <span class="text-xs text-ios-secondary font-normal font-sans ml-1">{{ residual.currency }}</span>
        </template>
        <span v-else class="text-ios-tertiary font-normal text-base">—</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import {
  type NodeMeta,
  parseRenewPrice,
  calculateRemainingDays,
  calculateMonthlyExpenditure,
  calculateResidualValue,
  CURRENCY_SYMBOLS
} from '../../nodeMeta';

const props = defineProps<{ meta: NodeMeta }>();

const priceInfo = computed(() => {
  const parsed = parseRenewPrice(props.meta.renewPrice, props.meta.renewCurrency, props.meta.renewCycle);
  if (!parsed.price) return null;
  const symbol = CURRENCY_SYMBOLS[parsed.currency] || parsed.currency;
  return {
    symbol,
    price: parsed.price,
    cycle: parsed.cycle ? parsed.cycle.replace(/^\//, '') : ''
  };
});

const monthly = computed(() => calculateMonthlyExpenditure(props.meta));
const remaining = computed(() => calculateRemainingDays(props.meta.renewDate));
const residual = computed(() => calculateResidualValue(props.meta));
</script>
