<template>
    <div
      class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-5"
    >
      <div
        v-for="(node, index) in nodes"
        :key="node.id"
        v-visible="node.id"
        :draggable="reorderable"
        @dragstart="onDragStart($event, node.id)"
        @dragover.prevent="dropTarget = reorderable && dragging ? node.id : ''"
        @dragleave="dropTarget = dropTarget === node.id ? '' : dropTarget"
        @drop.prevent="onDrop(node.id)"
        @dragend="dragging = ''; dropTarget = ''"
        @click="handleCardClick(node)"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4 cursor-pointer group"
        :class="{ 'ring-2 ring-ios-blue/40': dropTarget === node.id && dragging !== node.id, 'opacity-50': dragging === node.id }"
      >
        <!-- Card Top Details -->
        <div class="space-y-3.5">
          <!-- Row 1: Name & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex flex-wrap items-center gap-x-2 gap-y-1 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="getNodeCountryTitle(node)"
              >
                {{ getNodeCountry(node).flag }}
              </span>
              <router-link
                :to="`/nodes/${node.id}`"
                @click.stop
                class="min-w-0 max-w-full font-bold text-lg text-neutral-900 group-hover:text-ios-blue transition-colors truncate tracking-normal"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span v-if="getNodeRegion(node)" class="rounded bg-black/[0.04] px-1.5 py-0.5 text-xs font-semibold text-neutral-600 shrink-0 truncate max-w-[80px]" :title="getNodeRegion(node)">
                {{ getNodeRegion(node) }}
              </span>
              <span v-for="item in node.tags || []" :key="item" class="rounded bg-ios-blue/[0.08] px-1.5 py-0.5 text-xs font-semibold text-ios-blue shrink-0 truncate max-w-[96px]" :title="`服务器分组：${item}`">
                {{ item }}
              </span>
              <span v-if="node.tuningAdviceCount && node.tuningAdviceCount > 0" class="rounded bg-amber-500/10 text-amber-600 border border-amber-500/20 px-1.5 py-0.5 text-xs font-semibold shrink-0" :title="`有 ${node.tuningAdviceCount} 条调优建议`">
                调优建议 {{ node.tuningAdviceCount }}
              </span>
              <span v-if="node.deletedAt" class="rounded-full bg-black/[0.05] px-1.5 py-0.2 text-xs font-semibold text-neutral-400 shrink-0">
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

          <!-- Row 2: Sub-info (Address · Core Version · Running Time) -->
          <div class="text-xs text-neutral-400 font-mono tracking-normal flex items-center gap-1.5 flex-wrap">
            <span :class="connectAddress(node) ? 'text-neutral-500' : 'text-ios-orange font-sans'">{{ connectAddress(node) || '未设置连接地址' }}</span>
            <span class="text-neutral-300">·</span>
            <span>{{ coreSummaryLabel(node) }}</span>
            <span class="text-neutral-300">·</span>
            <span>已运行 {{ calculateRunningTime(node) }}</span>
            <span class="text-ios-secondary">{{ node.capabilities?.host?.osPrettyName || node.os || '—' }}</span>
          </div>

          <!-- Row 3: CPU / 内存 / 磁盘 3栏微型仪表 -->
          <div class="grid grid-cols-3 gap-3 pt-0.5">
            <!-- CPU -->
            <div>
              <div class="flex items-center justify-between text-xs mb-1">
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
              <div class="flex items-center justify-between text-xs mb-1">
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
              <div class="flex items-center justify-between text-xs mb-1">
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

          <!-- Row 4: 网速与本月流量 (横向展示) -->
          <div class="grid grid-cols-2 gap-2 pt-1 border-t border-black/[0.04] items-start">
            <!-- 网速 -->
            <div class="min-w-0">
              <p class="text-xs text-neutral-400 font-medium">网速</p>
              <div class="mt-0.5 flex items-center gap-x-2 gap-y-0.5 flex-wrap text-xs font-mono tabular-nums">
                <div class="flex items-center gap-0.5 shrink-0">
                  <span class="text-emerald-600 font-bold text-xs shrink-0">↑</span>
                  <span class="font-bold text-neutral-900 text-xs">
                    {{ formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'transmit')).value }}
                  </span>
                  <span
                    v-if="formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'transmit')).unit"
                    class="text-xs text-neutral-400 font-normal shrink-0"
                  >
                    {{ formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'transmit')).unit }}
                  </span>
                </div>
                <div class="flex items-center gap-0.5 shrink-0">
                  <span class="text-ios-blue font-bold text-xs shrink-0">↓</span>
                  <span class="font-bold text-neutral-900 text-xs">
                    {{ formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'receive')).value }}
                  </span>
                  <span
                    v-if="formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'receive')).unit"
                    class="text-xs text-neutral-400 font-normal shrink-0"
                  >
                    {{ formatSpeedParts(nicRate(hostMetrics(node, clockNow), 'receive')).unit }}
                  </span>
                </div>
              </div>
            </div>

            <!-- 本月整机流量 (T1: 单张网卡的本期整机用量，已取代此处的代理流量读数) -->
            <div class="min-w-0">
              <MachineTrafficUsage compact :summary="machineTraffic[node.id]" />
            </div>
          </div>

          <!-- Row 5: 延迟与测试点卡片 (3 个测试点) -->
          <div class="pt-1 border-t border-black/[0.04] space-y-1.5">
            <div class="flex items-center justify-between text-xs">
              <span class="text-neutral-400 font-medium">延迟</span>
              <span class="text-neutral-400 font-mono text-xs">{{ latencyStatus(node.id) }}</span>
            </div>

            <div class="grid grid-cols-3 gap-1.5">
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-xs text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">{{ latencyTile(node.id, 0).label }}</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">{{ latencyTile(node.id, 0).value }}</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-xs text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">{{ latencyTile(node.id, 1).label }}</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">{{ latencyTile(node.id, 1).value }}</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-xs text-neutral-500 font-medium truncate">
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
                <span class="text-xs font-normal" :class="getRenewalInfo(node).isNear ? 'text-amber-600' : 'text-neutral-400'">
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
                class="font-mono font-bold text-neutral-900 bg-neutral-100 px-1.5 py-0.5 rounded-md text-xs"
              >
                {{ getRenewalPriceDisplay(node) }}
              </span>
              <span v-else class="text-neutral-400 font-mono text-xs">—</span>
            </div>
          </div>
        </div>

        <!-- Row 7: 彩色操作按钮 (安装, 编辑, 删除) -->
        <div
          v-if="isOwner && !node.deletedAt"
          class="pt-2 border-t border-black/[0.05] grid grid-cols-3 gap-1.5"
          @click.stop
        >
          <!-- 1. 安装 -->
          <button
            type="button"
            :disabled="isNodeReissuing(node.id)"
            @click.stop="emit('install', node)"
            class="py-1.5 rounded-xl bg-[#ECFDF3] text-[#027A48] hover:bg-[#D1FADF] font-semibold text-xs text-center transition-colors disabled:opacity-50 disabled:cursor-not-allowed inline-flex items-center justify-center gap-1"
          >
            <span v-if="isNodeReissuing(node.id)" class="inline-block w-3 h-3 border-2 border-[#027A48] border-t-transparent rounded-full animate-spin"></span>
            <span>{{ isNodeReissuing(node.id) ? '正在签发…' : '安装' }}</span>
          </button>

          <!-- 2. 编辑 -->
          <button
            type="button"
            @click.stop="emit('edit', node)"
            class="py-1.5 rounded-xl bg-[#F0F9FF] text-[#026AA2] hover:bg-[#E0F2FE] font-semibold text-xs text-center transition-colors"
          >
            编辑
          </button>

          <!-- 3. 删除 -->
          <button
            type="button"
            @click.stop="emit('delete', node)"
            class="py-1.5 rounded-xl bg-[#FEF3F2] text-[#B42318] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            删除
          </button>
        </div>
        <button v-if="canAddRelay(node)" type="button" class="ios-btn-secondary w-full" @click.stop="emit('add-relay', node)">添加中转</button>
        <!-- Order controls for touch screens, where native dragging is unavailable -->
        <div v-if="reorderable" class="flex justify-end gap-1.5 -mt-2" @click.stop>
          <button v-if="index > 0" type="button" class="px-2 py-1 rounded-lg bg-black/[0.04] text-neutral-600 hover:bg-black/[0.08] text-xs font-semibold" @click.stop="emit('move', node.id, -1)">上移</button>
          <button v-if="index < nodes.length - 1" type="button" class="px-2 py-1 rounded-lg bg-black/[0.04] text-neutral-600 hover:bg-black/[0.08] text-xs font-semibold" @click.stop="emit('move', node.id, 1)">下移</button>
        </div>
      </div>
    </div>
</template>
<script setup lang="ts">
import { ref, toRef } from 'vue';
import { useRouter } from 'vue-router';
import type { MachineTrafficSummary, NodeView } from '../api';
import { isOwner } from '../../../app/auth';
import { hostMetrics, nicRate } from '../metrics';
import { coreSummaryLabel } from '../coreSummary';
import { isNodeReissuing } from '../coreInstallOptions';
import { connectAddress } from '../nodeAddress';
import MachineTrafficUsage from './MachineTrafficUsage.vue';
import { useServerCardValues } from './serverCardValues';
import { useServerCardLatency } from './serverCardLatency';

const props = defineProps<{ nodes: NodeView[]; clockNow: number; machineTraffic: Record<string, MachineTrafficSummary>; reorderable?: boolean; canAddRelay: (node: NodeView) => boolean }>();
const emit = defineEmits<{ install: [node: NodeView]; edit: [node: NodeView]; delete: [node: NodeView]; 'add-relay': [node: NodeView]; move: [id: string, delta: -1 | 1]; reorder: [fromId: string, toId: string] }>();
const router = useRouter();

// Native drag and drop (no dependency); the page saves the new order.
const dragging = ref(''), dropTarget = ref('');
function onDragStart(event: DragEvent, id: string) {
  if (!props.reorderable) return;
  dragging.value = id;
  event.dataTransfer?.setData('text/plain', id);
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move';
}
function onDrop(id: string) {
  if (props.reorderable && dragging.value && dragging.value !== id) emit('reorder', dragging.value, id);
  dragging.value = ''; dropTarget.value = '';
}

const { statusCapsuleClass, statusCapsuleText, statusDotClass, calculateRunningTime, formatPercent,
  barColor, formatSpeedParts, getNodeCountry, getNodeCountryTitle, getNodeRegion, getRenewalDate,
  getRenewalPriceDisplay, getRenewalInfo } = useServerCardValues(toRef(props, 'clockNow'));
const { vVisible, latencyTile, latencyStatus } = useServerCardLatency();

function handleCardClick(node: NodeView) {
  const selection = window.getSelection();
  if (selection && selection.toString().trim().length > 0) return;
  router.push(`/nodes/${node.id}`);
}
</script>
