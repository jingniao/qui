<template>
  <!-- Expanded: the popover is positioned against the sidebar (which is relative), not
       the bell near its right edge, so a right-aligned 24rem panel never runs off-screen. -->
  <div :class="collapsed ? 'relative' : ''" ref="rootRef">
    <!-- Bell Button -->
    <button
      type="button"
      @click="toggleOpen"
      :class="[
        'relative rounded-lg flex items-center justify-center transition-colors',
        collapsed
          ? 'w-10 h-10 text-ios-tertiary hover:text-ios-label hover:bg-black/[0.05]'
          : 'w-7 h-7 text-ios-tertiary hover:text-ios-label hover:bg-black/[0.05]'
      ]"
      title="通知中心"
    >
      <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M15 17h5l-1.405-1.405A2.032 2.032 0 0118 14.158V11a6.002 6.002 0 00-4-5.659V5a2 2 0 10-4 0v.341C7.67 6.165 6 8.388 6 11v3.159c0 .538-.214 1.055-.595 1.436L4 17h5m6 0v1a3 3 0 11-6 0v-1m6 0H9" />
      </svg>
      <!-- Unread indicator -->
      <span
        v-if="unreadCount > 0"
        :class="[
          'absolute bg-ios-red text-white font-bold rounded-full flex items-center justify-center shadow-sm',
          collapsed
            ? 'top-2 right-2 w-2.5 h-2.5 ring-2 ring-white'
            : '-top-1 -right-1 min-w-5 h-5 px-1 text-xs'
        ]"
      >
        <span v-if="!collapsed">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
      </span>
    </button>

    <!-- Popover Dropdown -->
    <div
      v-if="isOpen"
      :class="[
        'absolute z-50 bg-white/95 backdrop-blur-xl rounded-2xl shadow-ios-lg border border-black/[0.08] flex flex-col w-80 sm:w-96 max-w-[calc(100vw-1.5rem)] max-h-[30rem] overflow-hidden select-text text-left',
        collapsed ? 'left-full ml-3 top-0' : 'left-3 top-14'
      ]"
    >
      <!-- Header -->
      <div class="px-4 py-3 border-b border-black/[0.06] flex items-center justify-between bg-[#F8F8FA]/90 shrink-0">
        <div class="flex items-center gap-2">
          <span class="text-sm font-semibold text-ios-label">通知中心</span>
          <span v-if="unreadCount > 0" class="px-1.5 py-0.5 text-xs font-medium bg-ios-red/10 text-ios-red rounded-full">
            {{ unreadCount }} 条未读
          </span>
        </div>
        <div class="flex items-center gap-2">
          <!-- read-all is Owner-only (403 for Viewer), so Viewers do not see it. -->
          <button
            v-if="isOwner && unreadCount > 0"
            type="button"
            @click="markAllAsRead"
            class="text-xs text-ios-blue hover:text-blue-700 transition-colors"
          >
            全部标为已读
          </button>
        </div>
      </div>

      <!-- Filter tabs -->
      <div class="px-4 py-2 border-b border-black/[0.04] bg-white flex items-center gap-2 shrink-0">
        <button
          type="button"
          @click="setFilter(false)"
          :class="[
            'px-2.5 py-1 text-xs rounded-lg transition-colors font-medium',
            !unreadFilter ? 'bg-black/[0.06] text-ios-label' : 'text-ios-tertiary hover:text-ios-label'
          ]"
        >
          全部
        </button>
        <button
          type="button"
          @click="setFilter(true)"
          :class="[
            'px-2.5 py-1 text-xs rounded-lg transition-colors font-medium',
            unreadFilter ? 'bg-black/[0.06] text-ios-label' : 'text-ios-tertiary hover:text-ios-label'
          ]"
        >
          只看未读
        </button>
      </div>

      <!-- List Content -->
      <div class="flex-1 overflow-y-auto divide-y divide-black/[0.04]">
        <div v-if="loading && items.length === 0" class="p-6 text-center text-xs text-ios-tertiary">
          加载中…
        </div>
        <div v-else-if="items.length === 0" class="p-8 text-center text-xs text-ios-tertiary">
          暂无通知
        </div>
        <div
          v-for="item in items"
          :key="item.id"
          :class="[
            'p-3.5 hover:bg-black/[0.02] transition-colors flex flex-col gap-1',
            !item.read ? 'bg-blue-50/20' : ''
          ]"
        >
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-1.5 min-w-0">
              <span
                :class="[
                  'px-1.5 py-0.5 text-xs font-semibold rounded shrink-0',
                  badgeClass(item.severity)
                ]"
              >
                {{ severityLabel(item.severity) }}
              </span>
              <span class="text-xs font-semibold text-ios-label truncate">{{ item.title }}</span>
            </div>
            <span class="text-xs text-ios-tertiary shrink-0">{{ formatTimeDisplay(item.at) }}</span>
          </div>

          <p class="text-xs text-ios-secondary break-words leading-relaxed pl-0.5">
            {{ item.body }}
          </p>

          <div v-if="item.nodeId" class="pt-1 flex items-center justify-end">
            <button
              type="button"
              @click="goToNode(item.nodeId)"
              class="text-xs text-ios-blue hover:text-blue-700 flex items-center gap-0.5"
            >
              查看节点 &rarr;
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue';
import { useRouter } from 'vue-router';
import { notificationsAPI, severityTone, type Notification } from '../modules/system/notificationsApi';
import { isOwner } from '../app/auth';

defineProps<{
  collapsed?: boolean;
}>();

const router = useRouter();
const rootRef = ref<HTMLElement | null>(null);
const isOpen = ref(false);
const unreadFilter = ref(false);
const unreadCount = ref(0);
const items = ref<Notification[]>([]);
const loading = ref(false);

function badgeClass(severity: string) {
  const tone = severityTone(severity);
  switch (tone) {
    case 'success':
      return 'bg-emerald-50 text-emerald-700 border border-emerald-200/50';
    case 'danger':
      return 'bg-rose-50 text-rose-700 border border-rose-200/50';
    case 'warning':
      return 'bg-amber-50 text-amber-700 border border-amber-200/50';
    default:
      return 'bg-blue-50 text-blue-700 border border-blue-200/50';
  }
}

function severityLabel(severity: string) {
  switch (severity) {
    case 'resolved':
      return '已恢复';
    case 'critical':
      return '严重';
    case 'warning':
      return '警告';
    default:
      return '提示';
  }
}

function formatTimeDisplay(at: string) {
  if (!at) return '';
  const date = new Date(at);
  if (isNaN(date.getTime())) return at;
  const now = new Date();
  const diffSec = Math.floor((now.getTime() - date.getTime()) / 1000);
  if (diffSec < 60) return '刚刚';
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)} 分钟前`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)} 小时前`;
  const m = String(date.getMonth() + 1).padStart(2, '0');
  const d = String(date.getDate()).padStart(2, '0');
  const h = String(date.getHours()).padStart(2, '0');
  const min = String(date.getMinutes()).padStart(2, '0');
  return `${m}-${d} ${h}:${min}`;
}

async function loadData() {
  try {
    loading.value = true;
    const res = await notificationsAPI.list(unreadFilter.value, 1, 20);
    items.value = res.items || [];
    unreadCount.value = res.unread || 0;
  } catch {
    // Keep stale data on error
  } finally {
    loading.value = false;
  }
}

async function loadUnreadOnly() {
  try {
    const res = await notificationsAPI.list(false, 1, 1);
    unreadCount.value = res.unread || 0;
  } catch {
    // Ignore error
  }
}

function toggleOpen() {
  isOpen.value = !isOpen.value;
  if (isOpen.value) {
    loadData();
  }
}

function setFilter(unread: boolean) {
  unreadFilter.value = unread;
  loadData();
}

async function markAllAsRead() {
  try {
    await notificationsAPI.readAll();
    unreadCount.value = 0;
    for (const it of items.value) {
      it.read = true;
    }
    if (unreadFilter.value) {
      items.value = [];
    }
  } catch {
    // Ignore
  }
}

function goToNode(nodeId: string) {
  isOpen.value = false;
  router.push(`/nodes/${nodeId}`);
}

function handleClickOutside(event: MouseEvent) {
  if (rootRef.value && !rootRef.value.contains(event.target as Node)) {
    isOpen.value = false;
  }
}

function handleNotificationEvent() {
  if (isOpen.value) {
    loadData();
  } else {
    loadUnreadOnly();
  }
}

onMounted(() => {
  loadUnreadOnly();
  document.addEventListener('click', handleClickOutside);
  window.addEventListener('proxy-notification', handleNotificationEvent);
});

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside);
  window.removeEventListener('proxy-notification', handleNotificationEvent);
});
</script>
