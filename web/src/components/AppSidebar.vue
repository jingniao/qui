<template>
  <aside
    :class="[
      'h-full bg-[#F8F8FA]/95 backdrop-blur-xl text-[#1C1C1E] flex flex-col border-r border-black/[0.06] transition-all duration-300 z-40 select-none w-full'
    ]"
  >
    <!-- Brand Logo & Title (Apple Squircle Vibe with comfortable spacing) -->
    <div class="h-16 px-4 flex items-center justify-between border-b border-black/[0.05] shrink-0">
      <router-link to="/dashboard" class="flex items-center gap-2.5 min-w-0 flex-1 overflow-hidden">
        <div class="w-8.5 h-8.5 rounded-[10px] bg-gradient-to-tr from-[#007AFF] to-[#5856D6] flex items-center justify-center text-white shrink-0 shadow-[0_3px_10px_rgba(0,122,255,0.25)]">
          <svg class="w-4.5 h-4.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
        </div>
        <div v-if="!isCollapsed" class="flex flex-col min-w-0 truncate">
          <span class="font-bold text-[#1C1C1E] text-sm tracking-tight truncate leading-snug">{{ authState.platformName }}</span>
          <span class="text-[11px] text-[#8E8E93] font-medium leading-none truncate">轻量代理控制平台</span>
        </div>
      </router-link>

      <button
        v-if="!isCollapsed"
        type="button"
        @click="toggleCollapse"
        class="hidden lg:flex w-7 h-7 rounded-lg text-[#8E8E93] hover:text-[#1C1C1E] hover:bg-black/[0.05] items-center justify-center transition-colors shrink-0 ml-1.5"
        title="折叠导航栏"
      >
        <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 19l-7-7 7-7m8 14l-7-7 7-7" />
        </svg>
      </button>
    </div>

    <!-- Top Action bar: Quick All Expand / Collapse toggle -->
    <div v-if="!isCollapsed" class="px-4 pt-3 pb-1 flex items-center justify-between text-[11px] text-[#8E8E93]">
      <span class="font-semibold uppercase tracking-wider">业务导航分栏</span>
      <button
        type="button"
        @click="toggleAllSections"
        class="px-2 py-0.5 rounded-full hover:bg-black/[0.05] hover:text-[#007AFF] font-medium transition-all"
      >
        {{ areAllSectionsOpen ? '折叠全部' : '展开全部' }}
      </button>
    </div>

    <!-- Navigation Menu Items (Collapsible Section Groups) -->
    <div class="flex-1 overflow-y-auto px-3 py-2 space-y-1 scrollbar-thin">
      <!-- 1. Standalone Top Item: Dashboard (系统总览) -->
      <router-link
        to="/dashboard"
        :class="[
          'flex items-center gap-2.5 px-3 py-2 rounded-xl text-[13px] font-medium transition-all group',
          isCurrentPath('/dashboard') ? 'bg-[#007AFF] text-white shadow-[0_2px_8px_rgba(0,122,255,0.28)] font-semibold' : 'text-[#3C3C43] hover:text-[#1C1C1E] hover:bg-black/[0.04]'
        ]"
      >
        <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
        </svg>
        <span v-if="!isCollapsed">系统总览</span>
      </router-link>

      <!-- 2. Collapsible Sections (默认为折叠) -->
      <div v-for="sec in navSections" :key="sec.id" v-show="!isViewer || sec.id === 'nodes'" class="pt-0.5">
        <!-- Section Collapsible Header Button -->
        <button
          type="button"
          @click="toggleSection(sec.id)"
          :class="[
            'w-full flex items-center justify-between px-3 py-2 rounded-xl text-[13px] font-medium transition-all group select-none',
            isSectionActive(sec.id) && !openSections[sec.id]
              ? 'bg-[#007AFF]/10 text-[#007AFF] font-semibold'
              : 'text-[#8E8E93] hover:text-[#1C1C1E] hover:bg-black/[0.04]'
          ]"
          :title="sec.label"
        >
          <div class="flex items-center gap-2.5 truncate">
            <!-- Section Icon -->
            <svg
              class="w-4 h-4 shrink-0 transition-colors"
              :class="isSectionActive(sec.id) ? 'text-[#007AFF]' : 'text-[#8E8E93] group-hover:text-[#1C1C1E]'"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" :d="sec.iconPath" />
            </svg>
            <span v-if="!isCollapsed" class="tracking-wide truncate">{{ sec.label }}</span>
          </div>

          <div v-if="!isCollapsed" class="flex items-center gap-2 shrink-0">
            <!-- Active Indicator Pill when collapsed -->
            <span
              v-if="!openSections[sec.id] && isSectionActive(sec.id)"
              class="w-2 h-2 rounded-full bg-[#007AFF] shadow-[0_0_6px_rgba(0,122,255,0.7)]"
              title="当前页面属于该分栏"
            ></span>
            <!-- Section Chevron Arrow -->
            <svg
              class="w-3.5 h-3.5 transition-transform duration-200"
              :class="openSections[sec.id] ? 'rotate-90 text-[#007AFF]' : 'rotate-0 text-[#8E8E93] group-hover:text-[#1C1C1E]'"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M9 5l7 7-7 7" />
            </svg>
          </div>
        </button>

        <!-- Section Children Items (Visible only when expanded) -->
        <transition name="accordion">
          <div v-show="openSections[sec.id]" class="pl-2 space-y-0.5 mt-0.5 border-l border-black/[0.08] ml-4">
            <template v-for="item in sec.items" :key="item.path">
              <router-link
                v-if="(!item.ownerOnly || isOwner) && (!isViewer || item.path === '/nodes')"
                :to="item.path"
                :class="navItemClass(item.path)"
              >
                <!-- Child Item Icon -->
                <svg
                  class="w-3.5 h-3.5 shrink-0 text-current opacity-80"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" :d="item.iconPath" />
                </svg>
                <span v-if="!isCollapsed" class="truncate text-[13px]">{{ item.label }}</span>
              </router-link>
            </template>
          </div>
        </transition>
      </div>
    </div>

    <!-- Sidebar Footer -->
    <div class="p-3.5 border-t border-black/[0.05] flex items-center justify-between text-xs text-[#8E8E93]">
      <span v-if="!isCollapsed" class="font-mono text-[11px]">开发版本</span>
      <button
        @click="toggleCollapse"
        class="w-full lg:w-auto p-1.5 rounded-lg hover:bg-black/[0.05] text-[#8E8E93] hover:text-[#1C1C1E] flex items-center justify-center transition-colors"
      >
        <span v-if="isCollapsed">→</span>
        <span v-else class="text-xs">收起侧栏</span>
      </button>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue';
import { useRoute } from 'vue-router';
import { authState, isOwner, isViewer } from '../app/auth';

interface Props {
  collapsed?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
  collapsed: false
});

const emit = defineEmits<{
  (e: 'toggle'): void;
}>();

const route = useRoute();
const internalCollapsed = ref(false);

const isCollapsed = computed(() => {
  return props.collapsed ?? internalCollapsed.value;
});

function toggleCollapse() {
  internalCollapsed.value = !internalCollapsed.value;
  emit('toggle');
}

// 默认为折叠 (All sections default to false / collapsed as requested)
const openSections = ref<Record<string, boolean>>({
  users: false,
  nodes: false,
  proxy: false,
  routing: false,
  access: false,
  system: false
});

const areAllSectionsOpen = computed(() => {
  return Object.values(openSections.value).every(Boolean);
});

function toggleSection(secId: string) {
  openSections.value[secId] = !openSections.value[secId];
}

function toggleAllSections() {
  const target = !areAllSectionsOpen.value;
  for (const key of Object.keys(openSections.value)) {
    openSections.value[key] = target;
  }
}

// Navigation Structure: 6 Collapsible Sections with direct SVG path strings
const navSections = [
  {
    id: 'users',
    label: '用户与凭据',
    iconPath: 'M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z',
    items: [
      { label: '用户列表', path: '/users', iconPath: 'M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z' },
      { label: '设备管理', path: '/devices', iconPath: 'M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z' },
      { label: '订阅管理', path: '/subscriptions', iconPath: 'M15 5v2m0 4v2m0 4v2M5 5a2 2 0 00-2 2v3a2 2 0 110 4v3a2 2 0 002 2h14a2 2 0 002-2v-3a2 2 0 110-4V7a2 2 0 00-2-2H5z' }
    ]
  },
  {
    id: 'nodes',
    label: '节点网络',
    iconPath: 'M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01',
    items: [
      { label: '节点列表', path: '/nodes', iconPath: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10' },
      { label: '节点组', path: '/node-groups', iconPath: 'M4 6h16M4 12h16M4 18h7' },
      { label: '节点拓扑', path: '/topology', iconPath: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z' }
    ]
  },
  {
    id: 'proxy',
    label: '代理配置',
    iconPath: 'M8 7h12m0 0l-4-4m4 4l-4 4m0 6H4m0 0l4 4m-4-4l4-4',
    items: [
      { label: '入站管理', path: '/proxy/inbounds', iconPath: 'M11 16l-4-4m0 0l4-4m-4 4h14m-5 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h7a3 3 0 013 3v1' },
      { label: '出站管理', path: '/proxy/outbounds', iconPath: 'M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1' },
      { label: '出口组', path: '/proxy/outbound-groups', iconPath: 'M4 6h16M4 10h16M4 14h16M4 18h16' },
      { label: '代理链', path: '/proxy/chains', iconPath: 'M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1' }
    ]
  },
  {
    id: 'routing',
    label: '分流控制',
    iconPath: 'M9 20l-5.447-2.724A1 1 0 013 16.382V5.618a1 1 0 011.447-.894L9 7m0 13l6-3m-6 3V7m6 10l4.553 2.276A1 1 0 0021 18.382V7.618a1 1 0 00-.553-.894L15 4m0 13V4m0 0L9 7',
    items: [
      { label: '分流规则', path: '/routing/rules', iconPath: 'M3 4h13M3 8h9m-9 4h6m4 0l4-4m0 0l4 4m-4-4v12' },
      { label: '服务规则', path: '/routing/services', iconPath: 'M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9' },
      { label: '网站控制', path: '/routing/websites', iconPath: 'M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z' },
      { label: '策略模拟', path: '/routing/simulator', iconPath: 'M14.752 11.168l-3.197-2.132A1 1 0 0010 9.87v4.263a1 1 0 001.555.832l3.197-2.132a1 1 0 000-1.664z M21 12a9 9 0 11-18 0 9 9 0 0118 0z' }
    ]
  },
  {
    id: 'access',
    label: '访问审计',
    iconPath: 'M9 17v-2m3 2v-4m3 4v-6m2 10H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z',
    items: [
      { label: '全部访问记录', path: '/access?tab=all', iconPath: 'M4 6h16M4 10h16M4 14h16M4 18h16' },
      { label: '已阻断拦截', path: '/access?tab=blocked', iconPath: 'M18.364 18.364A9 9 0 005.636 5.636m12.728 12.728A9 9 0 015.636 5.636m12.728 12.728L5.636 5.636' },
      { label: '域名资产统计', path: '/access?tab=stats', iconPath: 'M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z' },
    ]
  },
  {
    id: 'system',
    label: '系统管理',
    iconPath: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z',
    items: [
      { label: '管理员', path: '/system/admins', ownerOnly: true, iconPath: 'M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z' },
      { label: 'Agent 探针', path: '/system/agents', iconPath: 'M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z' },
      { label: '核心版本', path: '/system/cores', iconPath: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4' },
      { label: '配置版本', path: '/system/revisions', iconPath: 'M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z' },
      { label: '备份恢复', path: '/system/backups', iconPath: 'M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2' },
      { label: '操作日志', path: '/system/operations', iconPath: 'M12 6.253v13m0-13C10.832 5.477 9.246 5 7.5 5S4.168 5.477 3 6.253v13C4.168 18.477 5.754 18 7.5 18s3.332.477 4.5 1.253m0-13C13.168 5.477 14.754 5 16.5 5c1.747 0 3.332.477 4.5 1.253v13C19.832 18.477 18.247 18 16.5 18c-1.746 0-3.332.477-4.5 1.253' },
      { label: '系统设置', path: '/system/settings', iconPath: 'M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4' }
    ]
  }
];

function isCurrentPath(targetPath: string) {
  if (targetPath === '/dashboard') return route.path === '/dashboard';
  if (targetPath.includes('?')) {
    const [basePath, queryString] = targetPath.split('?');
    if (route.path !== basePath) return false;
    const params = new URLSearchParams(queryString);
    const tabParam = params.get('tab');
    if (tabParam) {
      return route.query.tab === tabParam || (!route.query.tab && tabParam === 'all');
    }
  }
  if (targetPath === '/access') {
    return route.path === '/access' && (!route.query.tab || route.query.tab === 'all');
  }
  return route.path.startsWith(targetPath);
}

function isSectionActive(sectionId: string) {
  const sec = navSections.find(s => s.id === sectionId);
  if (!sec) return false;
  return sec.items.some(item => {
    if (item.path.includes('?')) {
      const [base] = item.path.split('?');
      return route.path === base;
    }
    return route.path.startsWith(item.path);
  });
}

function navItemClass(targetPath: string) {
  const active = isCurrentPath(targetPath);
  return [
    'flex items-center gap-2.5 px-2.5 py-1.5 rounded-lg text-[13px] transition-all group select-none',
    active
      ? 'bg-[#007AFF] text-white font-medium shadow-[0_2px_8px_rgba(0,122,255,0.22)]'
      : 'text-[#48484A] hover:text-[#1C1C1E] hover:bg-black/[0.04]'
  ];
}
</script>

<style scoped>
.accordion-enter-active,
.accordion-leave-active {
  transition: all 0.2s cubic-bezier(0.16, 1, 0.3, 1);
  overflow: hidden;
}
.accordion-enter-from,
.accordion-leave-to {
  opacity: 0;
  max-height: 0px;
  transform: translateY(-4px);
}
.accordion-enter-to,
.accordion-leave-from {
  opacity: 1;
  max-height: 500px;
  transform: translateY(0);
}
</style>

