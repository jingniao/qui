<template>
  <div class="flex h-screen w-screen overflow-hidden bg-ios-bg">
    <!-- Desktop & Tablet Sidebar -->
    <div :class="['hidden lg:block shrink-0 transition-all duration-300', sidebarCollapsed ? 'w-20' : 'w-68']">
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

    <!-- Main Content Area -->
    <div class="flex-1 flex flex-col h-full min-w-0 overflow-hidden">
      <!-- Top Header -->
      <AppHeader @toggle-sidebar="mobileSidebarOpen = !mobileSidebarOpen" />

      <!-- Scrollable Main View -->
      <main class="flex-1 overflow-y-auto px-4 sm:px-6 lg:px-8 py-6 sm:py-8 bg-ios-bg">
        <div class="max-w-[1536px] mx-auto w-full">
          <router-view v-slot="{ Component }">
            <transition name="fade-fast" mode="out-in">
              <component :is="Component" />
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
import { ref } from 'vue';
import AppSidebar from '../components/AppSidebar.vue';
import AppHeader from '../components/AppHeader.vue';
import ToastContainer from '../components/ToastContainer.vue';

const sidebarCollapsed = ref(false);
const mobileSidebarOpen = ref(false);
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
