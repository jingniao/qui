<template>
    <div
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in nodes"
        :key="node.id"
        v-visible="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <!-- Card Top Details -->
        <div class="space-y-3.5">
          <!-- Row 1: Name & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="`${getNodeCountry(node).name} (${getNodeCountry(node).source === 'admin_region' ? '管理员指定地区' : 'IP归属识别'})`"
              >
                {{ getNodeCountry(node).flag }}
              </span>
              <router-link
                :to="`/nodes/${node.id}`"
                class="font-bold text-[17px] text-neutral-900 hover:text-ios-blue transition-colors truncate tracking-tight"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span v-if="getNodeRegion(node)" class="rounded bg-black/[0.04] px-1.5 py-0.5 text-[10px] font-semibold text-neutral-600 shrink-0 truncate max-w-[80px]" :title="getNodeRegion(node)">
                {{ getNodeRegion(node) }}
              </span>
              <span v-if="node.deletedAt" class="rounded-full bg-black/[0.05] px-1.5 py-0.2 text-[10px] font-semibold text-neutral-400 shrink-0">
                已删除
              </span>
            </div>

            <!-- Status Badge Capsule -->
            <span
              class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold shrink-0 select-none"
              :class="statusCapsuleClass(node)"
            >
              <span class="w-1.5 h-1.5 rounded-full" :class="statusDotClass(node)"></span>
              <span>{{ statusCapsuleText(node) }}</span>
            </span>
          </div>

          <!-- Row 2: Sub-info (IP · Core Version · Running Time) -->
          <div class="text-[12px] text-neutral-400 font-mono tracking-tight flex items-center gap-1.5 flex-wrap">
            <span class="text-neutral-500">{{ node.publicIp || node.hostname || '未分配 IP' }}</span>
            <span class="text-neutral-300">·</span>
            <span>{{ node.capabilities?.core_version || '—' }}</span>
            <span class="text-neutral-300">·</span>
            <span>已运行 {{ calculateRunningTime(node) }}</span>
          </div>

          <!-- Row 3: CPU / 内存 / 磁盘 3栏微型仪表 -->
          <div class="grid grid-cols-3 gap-3 pt-0.5">
            <!-- CPU -->
            <div>
              <div class="flex items-center justify-between text-[11px] mb-1">
                <span class="text-neutral-400 font-medium">CPU</span>
                <span class="font-bold text-neutral-800 font-mono tabular-nums">{{ formatPercent(node.heartbeat?.cpu_percent) }}</span>
              </div>
              <div class="h-1 w-full bg-black/[0.05] rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full transition-all duration-300"
                  :class="barColor(node.heartbeat?.cpu_percent)"
                  :style="{ width: `${Math.min(node.heartbeat?.cpu_percent ?? 0, 100)}%` }"
                ></div>
              </div>
            </div>

            <!-- 内存 -->
            <div>
              <div class="flex items-center justify-between text-[11px] mb-1">
                <span class="text-neutral-400 font-medium">内存</span>
                <span class="font-bold text-neutral-800 font-mono tabular-nums">{{ formatPercent(node.heartbeat?.memory_percent) }}</span>
              </div>
              <div class="h-1 w-full bg-black/[0.05] rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full transition-all duration-300"
                  :class="barColor(node.heartbeat?.memory_percent)"
                  :style="{ width: `${Math.min(node.heartbeat?.memory_percent ?? 0, 100)}%` }"
                ></div>
              </div>
            </div>

            <!-- 磁盘 -->
            <div>
              <div class="flex items-center justify-between text-[11px] mb-1">
                <span class="text-neutral-400 font-medium">磁盘</span>
                <span class="font-bold text-neutral-800 font-mono tabular-nums">{{ formatPercent(node.heartbeat?.disk_percent) }}</span>
              </div>
              <div class="h-1 w-full bg-black/[0.05] rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full transition-all duration-300"
                  :class="barColor(node.heartbeat?.disk_percent)"
                  :style="{ width: `${Math.min(node.heartbeat?.disk_percent ?? 0, 100)}%` }"
                ></div>
              </div>
            </div>
          </div>

          <!-- Row 4: 网速与本月流量 -->
          <div class="grid grid-cols-2 gap-2 pt-1 border-t border-black/[0.04]">
            <!-- 网速 -->
            <div>
              <p class="text-[11px] text-neutral-400 font-medium">网速</p>
              <div class="text-[12px] font-mono mt-0.5 text-neutral-700 flex items-center gap-1.5 tabular-nums">
                <span class="text-emerald-600 font-semibold flex items-center">↑ {{ nicRate(hostMetrics(node, clockNow), 'transmit') }}</span>
                <span class="text-ios-blue font-semibold flex items-center">↓ {{ nicRate(hostMetrics(node, clockNow), 'receive') }}</span>
              </div>
            </div>
            <!-- 本月流量 -->
            <div>
              <p class="text-[11px] text-neutral-400 font-medium" title="面板时区内仍保留的账户流量样本合计；受保留策略影响">本月流量（保留记录）</p>
              <div class="text-[12px] font-mono mt-0.5 flex items-baseline gap-1 tabular-nums">
                <span class="font-bold text-neutral-900 text-sm">
                  <TrafficValue :bytes="getNodeMonthTraffic(node)" />
                </span>
                <span class="text-[10px] text-neutral-400 font-normal">
                  ↑{{ formatCompactBytes(nodeMonthTraffic[node.id]?.uploadBytes) }} ↓{{ formatCompactBytes(nodeMonthTraffic[node.id]?.downloadBytes) }}
                </span>
              </div>
            </div>
          </div>

          <!-- Row 5: 延迟与测试点卡片 (3 个测试点) -->
          <div class="pt-1 border-t border-black/[0.04] space-y-1.5">
            <div class="flex items-center justify-between text-[11px]">
              <span class="text-neutral-400 font-medium">延迟</span>
              <span class="text-neutral-400 font-mono text-[10px]">{{ latencyStatus(node.id) }}</span>
            </div>

            <div class="grid grid-cols-3 gap-1.5">
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">{{ latencyTile(node.id, 0).label }}</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">{{ latencyTile(node.id, 0).value }}</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">{{ latencyTile(node.id, 1).label }}</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">{{ latencyTile(node.id, 1).value }}</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">{{ latencyTile(node.id, 2).label }}</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">{{ latencyTile(node.id, 2).value }}</p>
              </div>
            </div>
          </div>

          <!-- Row 6: 到期时间与续费价格 (Renewal Date & Price Info) -->
          <div class="pt-2 border-t border-black/[0.04] flex items-center justify-between text-xs">
            <!-- Left: 到期时间与剩余天数 -->
            <div class="flex items-center gap-1.5 min-w-0">
              <span class="text-neutral-400 shrink-0">到期:</span>
              <span
                v-if="getRenewalDate(node)"
                class="font-mono truncate"
                :class="getRenewalInfo(node).isNear ? 'text-amber-600 font-bold' : 'text-neutral-700 font-medium'"
              >
                {{ getRenewalDate(node) }}
                <span class="text-[10px] font-normal" :class="getRenewalInfo(node).isNear ? 'text-amber-600' : 'text-neutral-400'">
                  ({{ getRenewalInfo(node).label }})
                </span>
              </span>
              <span v-else class="text-neutral-400 font-mono">未设置</span>
            </div>

            <!-- Right: 续费价格 -->
            <div class="flex items-center gap-1 shrink-0">
              <span class="text-neutral-400">续费:</span>
              <span
                v-if="getRenewalPriceDisplay(node)"
                class="font-mono font-bold text-neutral-900 bg-neutral-100 px-1.5 py-0.5 rounded-md text-[11px]"
              >
                {{ getRenewalPriceDisplay(node) }}
              </span>
              <span v-else class="text-neutral-400 font-mono text-[11px]">—</span>
            </div>
          </div>
        </div>

        <!-- Row 7: 彩色操作网格按钮 (安装, 升级, 调优, 编辑, 删除, 详情) -->
        <div class="pt-2 border-t border-black/[0.05] grid grid-cols-3 gap-1.5">
          <!-- 1. 安装 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="emit('install', node)"
            class="py-1.5 rounded-xl bg-[#ECFDF3] text-[#027A48] hover:bg-[#D1FADF] font-semibold text-xs text-center transition-colors"
          >
            安装
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 2. 升级 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="router.push('/nodes/versions?tab=agents')"
            class="py-1.5 rounded-xl bg-[#FEF6EE] text-[#B54708] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            升级
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 3. 调优 -->
          <router-link
            v-if="!node.deletedAt"
            :to="`/nodes/${node.id}?tab=settings`"
            class="py-1.5 rounded-xl bg-[#ECFDF3] text-[#027A48] hover:bg-[#D1FADF] font-semibold text-xs text-center transition-colors"
          >
            调优
          </router-link>
          <div v-else class="py-1.5"></div>

          <!-- 7. 编辑 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="emit('edit', node)"
            class="py-1.5 rounded-xl bg-[#F0F9FF] text-[#026AA2] hover:bg-[#E0F2FE] font-semibold text-xs text-center transition-colors"
          >
            编辑
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 8. 删除 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="emit('delete', node)"
            class="py-1.5 rounded-xl bg-[#FEF3F2] text-[#B42318] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            删除
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 9. 详情 -->
          <router-link
            :to="`/nodes/${node.id}`"
            class="py-1.5 rounded-xl bg-neutral-100 text-neutral-600 hover:bg-neutral-200 font-semibold text-xs text-center transition-colors"
          >
            详情
          </router-link>
        </div>
      </div>
    </div>
</template>
<script setup lang="ts">
import { toRef } from 'vue';
import { useRouter } from 'vue-router';
import type { NodeView } from '../api';
import { isOwner } from '../../../app/auth';
import { hostMetrics, nicRate } from '../metrics';
import TrafficValue from '../../../components/TrafficValue.vue';
import { useServerCardValues } from './serverCardValues';
import { useServerCardLatency } from './serverCardLatency';
const props = defineProps<{ nodes: NodeView[]; clockNow: number; nodeMonthTraffic: Record<string, { uploadBytes: number; downloadBytes: number }> }>();
const emit = defineEmits<{ install: [node: NodeView]; edit: [node: NodeView]; delete: [node: NodeView] }>();
const router = useRouter();
const { statusCapsuleClass, statusCapsuleText, statusDotClass, calculateRunningTime, formatPercent,
  barColor, formatCompactBytes, getNodeCountry, getNodeRegion, getRenewalDate,
  getRenewalPriceDisplay, getRenewalInfo, getNodeMonthTraffic } = useServerCardValues(toRef(props, 'clockNow'), toRef(props, 'nodeMonthTraffic'));
const { vVisible, latencyTile, latencyStatus } = useServerCardLatency();
</script>
