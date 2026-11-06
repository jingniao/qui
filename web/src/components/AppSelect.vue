<template>
  <div
    ref="containerRef"
    :class="[
      'relative text-sm',
      isOpen ? 'z-40' : 'z-auto',
      disabled ? 'opacity-50 pointer-events-none' : ''
    ]"
  >
    <!-- 触发框 (Trigger Box) -->
    <button
      type="button"
      :disabled="disabled"
      @click="toggleDropdown"
      @keydown.down.prevent="navigateOptions(1)"
      @keydown.up.prevent="navigateOptions(-1)"
      @keydown.enter.prevent="selectHighlighted"
      @keydown.esc.prevent="closeDropdown"
      :class="[
        'w-full text-left rounded-xl border bg-white font-medium text-neutral-800 flex items-center justify-between cursor-pointer transition-all focus:outline-none shadow-2xs select-none',
        size === 'sm' ? 'px-2.5 py-1 text-xs' : 'px-3 py-2 text-sm',
        isOpen
          ? 'border-ios-blue ring-2 ring-ios-blue/20 bg-white'
          : 'border-black/[0.12] hover:border-black/[0.22] hover:bg-black/[0.01]',
        triggerClass
      ]"
    >
      <div class="flex items-center gap-1.5 min-w-0 flex-1 truncate pr-2">
        <span v-if="selectedOption?.icon" class="text-sm shrink-0">{{ selectedOption.icon }}</span>
        <span class="truncate font-medium text-neutral-800">
          {{ selectedOption?.label || placeholder || '请选择…' }}
        </span>
        <span
          v-if="selectedOption?.badge"
          class="px-1.5 py-0.5 rounded text-xs font-semibold bg-blue-50 text-ios-blue border border-blue-100/60 shrink-0"
        >
          {{ selectedOption.badge }}
        </span>
      </div>

      <!-- Chevron 下拉指示箭头 -->
      <svg
        class="w-3.5 h-3.5 text-neutral-400 shrink-0 transition-transform duration-200 pointer-events-none ml-1"
        :class="{ 'rotate-180 text-ios-blue': isOpen }"
        fill="none"
        viewBox="0 0 24 24"
        stroke="currentColor"
      >
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
      </svg>
    </button>

    <!-- 浮层下拉菜单 (Floating Dropdown Menu) -->
    <Transition
      enter-active-class="transition duration-150 ease-out"
      :enter-from-class="actualPlacement === 'top' ? 'transform scale-95 opacity-0 translate-y-1' : 'transform scale-95 opacity-0 -translate-y-1'"
      enter-to-class="transform scale-100 opacity-100 translate-y-0"
      leave-active-class="transition duration-100 ease-in"
      leave-from-class="transform scale-100 opacity-100 translate-y-0"
      :leave-to-class="actualPlacement === 'top' ? 'transform scale-95 opacity-0 translate-y-1' : 'transform scale-95 opacity-0 -translate-y-1'"
    >
      <div
        v-if="isOpen"
        :class="[
          'absolute bg-white/95 backdrop-blur-md rounded-2xl border border-black/[0.08] shadow-xl p-1.5 z-50 overflow-y-auto space-y-0.5 text-xs text-left',
          actualPlacement === 'top' ? 'bottom-full mb-1.5' : 'top-full mt-1.5',
          align === 'right' ? 'right-0' : 'left-0',
          hasDescriptions ? 'min-w-[240px] w-full' : 'w-full min-w-full',
          menuClass
        ]"
        :style="{ maxHeight: dynamicMaxHeight + 'px' }"
      >
        <template v-for="(option, idx) in normalizedOptions" :key="String(option.value) + '-' + idx">
          <!-- 分组标题 (Group Header) -->
          <div
            v-if="option.group && (idx === 0 || normalizedOptions[idx - 1].group !== option.group)"
            class="px-3 pt-2 pb-1 text-xs font-bold text-neutral-400 uppercase tracking-wider select-none border-t border-black/[0.04] first:border-0 first:pt-1"
          >
            {{ option.group }}
          </div>

          <!-- 选项项 (Option Item) -->
          <div
            @click.stop="selectOption(option)"
            @mouseenter="highlightedIndex = idx"
            :class="[
              'px-3 py-2 rounded-xl cursor-pointer flex items-center justify-between gap-3 transition-colors select-none',
              isSelected(option.value)
                ? 'bg-blue-50/70 text-ios-blue font-semibold'
                : highlightedIndex === idx
                  ? 'bg-neutral-100/80 text-neutral-900'
                  : 'text-neutral-700 hover:bg-neutral-100/80'
            ]"
          >
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-1.5 flex-wrap">
                <span v-if="option.icon" class="text-sm shrink-0">{{ option.icon }}</span>
                <span class="font-medium truncate">{{ option.label }}</span>
                <span
                  v-if="option.badge"
                  class="px-1.5 py-0.5 rounded text-xs font-semibold bg-blue-100/60 text-ios-blue shrink-0"
                >
                  {{ option.badge }}
                </span>
              </div>
              <p v-if="option.desc" class="text-xs text-neutral-400 mt-0.5 font-normal leading-relaxed">
                {{ option.desc }}
              </p>
            </div>

            <!-- 选中指示对勾 (Checkmark) -->
            <svg
              v-if="isSelected(option.value)"
              class="w-4 h-4 text-ios-blue shrink-0"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
            </svg>
          </div>
        </template>
      </div>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, nextTick, onMounted, onUnmounted } from 'vue';

export interface SelectOption {
  value: string | number | boolean;
  label: string;
  desc?: string;
  badge?: string;
  icon?: string;
  group?: string;
}

const props = withDefaults(
  defineProps<{
    modelValue: string | number | boolean | null | undefined;
    options: Array<SelectOption | string>;
    placeholder?: string;
    disabled?: boolean;
    size?: 'sm' | 'md';
    align?: 'left' | 'right';
    placement?: 'auto' | 'top' | 'bottom';
    triggerClass?: string;
    menuClass?: string;
  }>(),
  {
    placeholder: '请选择…',
    disabled: false,
    size: 'md',
    align: 'left',
    placement: 'auto',
    triggerClass: '',
    menuClass: ''
  }
);

const emit = defineEmits<{
  (e: 'update:modelValue', value: any): void;
  (e: 'change', value: any): void;
}>();

const containerRef = ref<HTMLElement | null>(null);
const isOpen = ref(false);
const highlightedIndex = ref(-1);
const actualPlacement = ref<'top' | 'bottom'>('bottom');
const dynamicMaxHeight = ref<number>(256);

function updatePlacement() {
  if (!containerRef.value) return;

  if (props.placement === 'top') {
    actualPlacement.value = 'top';
    dynamicMaxHeight.value = 256;
    return;
  }
  if (props.placement === 'bottom') {
    actualPlacement.value = 'bottom';
    dynamicMaxHeight.value = 256;
    return;
  }

  const rect = containerRef.value.getBoundingClientRect();

  // 寻找最近的具有滚动或截断特性的父容器（如抽屉 body、弹窗、滚动面板）
  let parent = containerRef.value.parentElement;
  let scrollParent: HTMLElement | null = null;
  while (parent && parent !== document.documentElement) {
    const style = window.getComputedStyle(parent);
    const overflowY = style.overflowY;
    if (overflowY === 'auto' || overflowY === 'scroll' || overflowY === 'hidden') {
      scrollParent = parent;
      break;
    }
    parent = parent.parentElement;
  }

  const viewportHeight = window.innerHeight;
  const parentRect = scrollParent ? scrollParent.getBoundingClientRect() : null;

  // 下边界与上边界：受制于视口和父容器的可用可见区间
  const bottomLimit = parentRect ? Math.min(parentRect.bottom, viewportHeight) : viewportHeight;
  const topLimit = parentRect ? Math.max(parentRect.top, 0) : 0;

  const spaceBelow = bottomLimit - rect.bottom;
  const spaceAbove = rect.top - topLimit;

  // 下拉列表期望的理想展示高度（约 200~260px）
  const preferredHeight = 220;

  // 当下方剩余空间不足且上方空间大于下方，或下方剩余小于 180px 且上方足够时，自动向上弹出
  if (
    (spaceBelow < preferredHeight && spaceAbove > spaceBelow) ||
    (spaceBelow < 180 && spaceAbove >= 180)
  ) {
    actualPlacement.value = 'top';
    dynamicMaxHeight.value = Math.max(120, Math.min(256, Math.floor(spaceAbove - 16)));
  } else {
    actualPlacement.value = 'bottom';
    dynamicMaxHeight.value = Math.max(120, Math.min(256, Math.floor(spaceBelow - 16)));
  }
}

const normalizedOptions = computed<SelectOption[]>(() => {
  return props.options.map(opt => {
    if (typeof opt === 'string') {
      return { value: opt, label: opt };
    }
    return opt;
  });
});

const hasDescriptions = computed(() => {
  return normalizedOptions.value.some(o => !!o.desc);
});

const selectedOption = computed<SelectOption | undefined>(() => {
  return normalizedOptions.value.find(opt => opt.value === props.modelValue);
});

function isSelected(val: string | number | boolean): boolean {
  return props.modelValue === val;
}

function toggleDropdown() {
  if (props.disabled) return;
  if (!isOpen.value) {
    updatePlacement();
    isOpen.value = true;
    const currentIdx = normalizedOptions.value.findIndex(o => o.value === props.modelValue);
    highlightedIndex.value = currentIdx >= 0 ? currentIdx : 0;
    nextTick(() => {
      updatePlacement();
    });
  } else {
    closeDropdown();
  }
}

function closeDropdown() {
  isOpen.value = false;
  highlightedIndex.value = -1;
}

function selectOption(option: SelectOption) {
  emit('update:modelValue', option.value);
  emit('change', option.value);
  closeDropdown();
}

function navigateOptions(delta: number) {
  if (!isOpen.value) {
    toggleDropdown();
    return;
  }
  const total = normalizedOptions.value.length;
  if (total === 0) return;
  highlightedIndex.value = (highlightedIndex.value + delta + total) % total;
}

function selectHighlighted() {
  if (!isOpen.value) {
    toggleDropdown();
    return;
  }
  if (highlightedIndex.value >= 0 && highlightedIndex.value < normalizedOptions.value.length) {
    selectOption(normalizedOptions.value[highlightedIndex.value]);
  }
}

function handleClickOutside(event: MouseEvent) {
  if (containerRef.value && !containerRef.value.contains(event.target as Node)) {
    closeDropdown();
  }
}

function onScrollOrResize() {
  if (isOpen.value) {
    updatePlacement();
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside);
  window.addEventListener('resize', onScrollOrResize, { passive: true });
  window.addEventListener('scroll', onScrollOrResize, { capture: true, passive: true });
});

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside);
  window.removeEventListener('resize', onScrollOrResize);
  window.removeEventListener('scroll', onScrollOrResize, { capture: true });
});
</script>
