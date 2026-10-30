<template>
  <div class="flex h-screen w-screen overflow-hidden bg-ios-bg">
    <!-- Desktop & Tablet Sidebar -->
    <div :class="['hidden lg:block shrink-0 transition-all duration-300', sidebarCollapsed ? 'w-20' : 'w-[17rem]']">
      <AppSidebar :collapsed="sidebarCollapsed" @toggle="sidebarCollapsed = !sidebarCollapsed" />
    </div>

    <!-- Mobile Drawer Sidebar -->
    <div
      v-if="mobileSidebarOpen"
      class="fixed inset-0 z-50 lg:hidden flex"
    >
      <div class="fixed inset-0 bg-black/30 backdrop-blur-md transition-opacity" @click="mobileSidebarOpen = false"></div>
      <div class="relative z-10 w-72 h-full bg-[#F8F8FA]/95 backdrop-blur-2xl shadow-2xl border-r border-black/[0.06]">
        <AppSidebar :collapsed="false" />
      </div>
    </div>

    <!-- Main Content Area (Top bar removed as requested) -->
    <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden relative">
      <!-- Mobile Sidebar Toggle (Floating button on mobile when topbar is removed) -->
      <button
        v-if="!mobileSidebarOpen"
        type="button"
        @click="mobileSidebarOpen = true"
        aria-label="打开导航"
        class="lg:hidden fixed top-3 left-3 z-30 w-9 h-9 rounded-xl bg-white/90 backdrop-blur-md border border-black/10 shadow-sm flex items-center justify-center text-ios-label"
      >
        <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
        </svg>
      </button>

      <!-- Scrollable Main View (Edge-to-edge flush for Canvas Studio, padded for normal pages) -->
      <main
        :class="[
          'flex-1 min-w-0 bg-ios-bg relative',
          isFluidPage
            ? 'h-full overflow-hidden px-0 pb-0 pt-16 lg:pt-0'
            : 'overflow-y-auto px-4 sm:px-6 lg:px-8 pb-6 sm:pb-8 pt-16 lg:pt-8'
        ]"
      >
        <div :class="isFluidPage ? 'h-full w-full' : 'max-w-[1536px] mx-auto w-full'">
          <!-- Keyed by path (not fullPath) so a different :id always gets a fresh
               instance, while query-only changes such as ?tab= keep the page. -->
          <router-view v-slot="{ Component, route: viewRoute }">
            <transition name="fade-fast" mode="out-in">
              <component :is="Component" :key="viewRoute.path" />
            </transition>
          </router-view>
        </div>
      </main>
    </div>

    <!-- Global Toast Container -->
    <ToastContainer />
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue';
import { useRoute } from 'vue-router';
import AppSidebar from '../components/AppSidebar.vue';
import ToastContainer from '../components/ToastContainer.vue';

const route = useRoute();
const sidebarCollapsed = ref(false);
const mobileSidebarOpen = ref(false);

const isFluidPage = computed(() => {
  return route.meta?.fluid === true || route.path.startsWith('/proxy/relay');
});
</script>

<style scoped>
.fade-fast-enter-active,
.fade-fast-leave-active {
  transition: opacity 0.15s ease;
}
.fade-fast-enter-from,
.fade-fast-leave-to {
  opacity: 0;
}
</style>
