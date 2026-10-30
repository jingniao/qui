<template>
            <!-- View Switcher (List vs Grid) -->
            <div class="flex items-center bg-black/[0.04] p-0.5 rounded-xl border border-black/[0.04]">
              <button
                type="button"
                @click="setDisplayMode('list')"
                :class="[
                  'px-2.5 py-1 rounded-lg text-xs font-medium transition-all inline-flex items-center gap-1.5',
                  displayMode === 'list'
                    ? 'bg-white text-ios-label shadow-xs font-semibold'
                    : 'text-ios-tertiary hover:text-ios-label'
                ]"
                title="列表视图"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h16" />
                </svg>
                <span>列表</span>
              </button>
              <button
                type="button"
                @click="setDisplayMode('grid')"
                :class="[
                  'px-2.5 py-1 rounded-lg text-xs font-medium transition-all inline-flex items-center gap-1.5',
                  displayMode === 'grid'
                    ? 'bg-white text-ios-label shadow-xs font-semibold'
                    : 'text-ios-tertiary hover:text-ios-label'
                ]"
                title="卡片视图"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2V6zM14 6a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2V6zM4 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2H6a2 2 0 01-2-2v-2zM14 16a2 2 0 012-2h2a2 2 0 012 2v2a2 2 0 01-2 2h-2a2 2 0 01-2-2v-2z" />
                </svg>
                <span>卡片</span>
              </button>
            </div>

</template>
<script setup lang="ts">
defineProps<{ displayMode: 'list' | 'grid' }>();
const emit = defineEmits<{ change: [mode: 'list' | 'grid'] }>();
function setDisplayMode(mode: 'list' | 'grid') { emit('change', mode); }
</script>
