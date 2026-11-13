<template>
  <div class="space-y-6">
    <section class="panel-card p-6 space-y-3">
      <div class="flex flex-wrap items-center gap-3">
        <h2 class="text-base font-semibold leading-normal">账号状态</h2>
        <StatusBadge :status="me.reason || me.status" />
      </div>
      <p>{{ reasons[me.reason] || me.reason || '正常服务' }}</p>
      <p v-if="abnormal" role="alert" class="text-sm text-ios-red">{{ reasons[me.reason] || '账号当前不可用' }}，请联系管理员。</p>
    </section>

    <section class="panel-card p-6 space-y-3">
      <h2 class="text-base font-semibold leading-normal">流量</h2>
      <div v-if="me.trafficLimit" class="space-y-2">
        <div class="h-2 bg-black/[0.05] rounded-full overflow-hidden" role="progressbar" aria-label="流量使用比例" :aria-valuenow="Math.round(percent)" aria-valuemin="0" aria-valuemax="100">
          <div class="h-full" :class="percent >= 90 ? 'bg-ios-red' : 'bg-ios-blue'" :style="{ width: percent + '%' }"></div>
        </div>
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-2 text-sm">
          <span>已用 <b>{{ formatBytes(me.usedTraffic) }}</b></span>
          <span>剩余 <b>{{ formatBytes(Math.max(0, me.trafficLimit - me.usedTraffic)) }}</b></span>
          <span>额度 <b>{{ formatBytes(me.trafficLimit) }}</b></span>
        </div>
      </div>
      <p v-else class="text-sm">已用 <b>{{ formatBytes(me.usedTraffic) }}</b>，额度不限</p>
      <p v-if="me.nextTrafficResetAt" class="text-sm text-ios-secondary">下次流量重置：{{ formatDate(me.nextTrafficResetAt) }}<span v-if="cycle">（{{ cycle }}）</span></p>
    </section>

    <section class="panel-card p-6 space-y-2">
      <h2 class="text-base font-semibold leading-normal">到期时间</h2>
      <p v-if="!me.expiresAt">永不过期</p>
      <p v-else :class="soon ? 'text-amber-700 font-semibold' : ''">
        {{ formatDateTime(me.expiresAt) }}
        <span v-if="left !== null && left >= 0">（剩余 {{ left }} 天）</span>
        <span v-else-if="left !== null">（已到期）</span>
      </p>
      <p v-if="soon" class="text-sm text-amber-700">即将到期，请联系管理员续期。</p>
    </section>

    <section v-if="subscriptionUrl" class="panel-card p-6 space-y-3">
      <h2 class="text-base font-semibold leading-normal">订阅链接</h2>
      <button class="ios-btn-secondary" @click="copyDefault">复制订阅链接</button>
      <p class="text-xs text-ios-tertiary">复制的是自动识别链接；指定客户端格式请到「订阅」。</p>
    </section>

    <section class="panel-card p-6 space-y-3">
      <h2 class="text-base font-semibold leading-normal">在线设备 / 上限</h2>
      <p>{{ me.onlineDevices ?? '未知' }} / {{ me.deviceLimit === 0 ? '不限' : me.deviceLimit ?? '—' }}</p>
      <p v-if="me.deviceLimit !== null && me.deviceLimit > 0 && me.onlineDevices !== null && me.onlineDevices >= me.deviceLimit" class="text-amber-700">设备数量已达上限</p>
      <p v-if="!me.deviceLimitEnforced" class="text-xs text-amber-700">部分执行：{{ me.deviceLimitUnsupportedNodes?.join('、') }}</p>
      <p v-if="me.deviceLimitUdpUnsupportedProtocols?.length" class="text-xs text-ios-secondary">{{ udpUnsupportedText(me.deviceLimitUdpUnsupportedProtocols) }}</p>
      <p v-for="(item, index) in addresses" :key="index" class="text-xs font-mono">{{ item.address }} · {{ item.allowed ? '允许' : '受限' }} · {{ item.active ? '活跃' : '等待释放' }}</p>
      <details class="text-xs text-ios-secondary"><summary>设备上限的限制与隐私</summary><p v-for="help in deviceLimitHelp" :key="help" class="mt-2">{{ help }}</p></details>
      <p v-if="me.wireGuardNote" class="text-xs text-ios-secondary">{{ me.wireGuardNote }}</p>
    </section>
    <p v-if="notice" role="status" class="text-ios-blue text-sm">{{ notice }}</p>
  </div>
</template>
<script setup lang="ts">
import { computed, ref } from 'vue';
import { deviceLimitHelp, udpUnsupportedText } from '../app/deviceLimit';
import StatusBadge from '../components/StatusBadge.vue';
import type { Profile, Subscription } from './api';
import { daysLeft, formatBytes, formatDate, formatDateTime, quotaPercent, resetCycleLabel } from './format';
const props = defineProps<{ me: Profile; subscription?: Subscription; addresses: { address: string; allowed: boolean; active: boolean }[]; preview?: boolean }>();
const reasons: Record<string, string> = { disabled: '账号已禁用', expired: '账号已到期', quota_exceeded: '流量额度已用完', active: '正常服务' };
const notice = ref('');
const abnormal = computed(() => ['disabled', 'expired', 'quota_exceeded'].includes(props.me.reason) || ['disabled', 'expired', 'quota_exceeded'].includes(props.me.status));
const percent = computed(() => quotaPercent(props.me.usedTraffic, props.me.trafficLimit));
const cycle = computed(() => resetCycleLabel(props.me.trafficResetCycle));
const left = computed(() => daysLeft(props.me.expiresAt));
const soon = computed(() => left.value !== null && left.value >= 0 && left.value <= 7);
const subscriptionUrl = computed(() => props.subscription?.state === 'ready' ? props.subscription.path : '');
async function copyDefault() {
  try { await navigator.clipboard.writeText(new URL(subscriptionUrl.value!, window.location.origin).href); notice.value = '已复制订阅链接'; }
  catch { notice.value = '复制失败，请到「订阅」页手动复制'; }
}
</script>
