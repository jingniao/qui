<template>
  <div class="h-full w-full flex flex-col overflow-hidden bg-[#F5F5F7] select-none relative">
    <div v-if="!currentScheme.id" class="absolute inset-0 z-40 bg-ios-background flex flex-col items-center justify-center gap-4"><p class="text-ios-secondary">暂无中转方案</p><button class="ios-btn-primary" @click="createNewScheme">新建方案</button></div>
    <!-- 1. Top Integrated Studio Toolbar -->
    <header class="h-14 px-4 bg-white/95 backdrop-blur-md border-b border-black/[0.08] flex items-center justify-between gap-3 shrink-0 shadow-2xs z-30">
      <!-- Left: Panel Toggle & Scheme Switcher -->
      <div class="flex items-center gap-2.5 min-w-0">
        <!-- Toggle Left Library Button -->
        <button
          type="button"
          @click="leftPanelOpen = !leftPanelOpen"
          :class="leftPanelOpen ? 'bg-ios-blue/10 text-ios-blue border-ios-blue/20' : 'bg-black/[0.03] hover:bg-black/[0.06] text-ios-secondary border-black/[0.06]'"
          class="px-2.5 py-1.5 rounded-xl border transition-all flex items-center gap-1.5 text-xs font-semibold shrink-0"
          :title="leftPanelOpen ? '收起资源库' : '展开资源库'"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7" />
          </svg>
          <span class="hidden sm:inline">资源库</span>
          <span class="w-1.5 h-1.5 rounded-full" :class="leftPanelOpen ? 'bg-ios-blue' : 'bg-neutral-300'"></span>
        </button>

        <div class="h-4 w-px bg-black/[0.08] shrink-0"></div>

        <!-- 1. Unified Scheme Studio Capsule with Custom Popover -->
        <div class="relative" @click.stop>
          <div
            class="flex items-center bg-black/[0.03] hover:bg-black/[0.05] border border-black/[0.06] rounded-xl p-1 transition-all min-w-0 shadow-2xs"
            :class="{ 'ring-2 ring-ios-blue/20 border-ios-blue/30 bg-white shadow-xs': schemeDropdownOpen }"
          >
            <!-- Custom Dropdown Trigger Button -->
            <button
              type="button"
              @click="toggleSchemeDropdown"
              class="flex items-center gap-1.5 pl-1.5 pr-2 py-0.5 text-xs font-semibold text-ios-label hover:text-ios-blue transition-colors min-w-0 max-w-[140px] sm:max-w-[200px] md:max-w-[260px] text-left"
              title="切换中转方案"
            >
              <svg class="w-4 h-4 text-ios-blue shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 17V7m0 10a2 2 0 01-2 2H5a2 2 0 01-2-2V7a2 2 0 012-2h2a2 2 0 012 2m0 10a2 2 0 002 2h2a2 2 0 002-2M9 7a2 2 0 012-2h2a2 2 0 012 2m0 10V7m0 10a2 2 0 002 2h2a2 2 0 002-2V7a2 2 0 00-2-2h-2a2 2 0 00-2 2" />
              </svg>
              <span class="truncate">{{ currentScheme.name || '选择方案' }}</span>
              <svg
                class="w-3.5 h-3.5 text-ios-tertiary shrink-0 transition-transform duration-200"
                :class="{ 'rotate-180 text-ios-blue': schemeDropdownOpen }"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
              >
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
              </svg>
            </button>

            <!-- Divider -->
            <div class="h-3.5 w-px bg-black/[0.08] mx-0.5 shrink-0"></div>

            <!-- Live Status Indicator -->
            <div
              class="px-1.5 py-0.5 flex items-center gap-1.5 text-[11px] font-medium shrink-0 cursor-default"
              :title="!currentScheme.published ? '当前方案为草稿（未发布）' : (currentScheme.hasUnpublishedChanges ? '当前方案有未发布的修改' : '当前方案已发布生效')"
            >
              <span
                class="w-1.5 h-1.5 rounded-full"
                :class="!currentScheme.published ? 'bg-neutral-400' : (currentScheme.hasUnpublishedChanges ? 'bg-amber-500 animate-pulse' : 'bg-emerald-500')"
              ></span>
              <span
                :class="!currentScheme.published ? 'text-ios-tertiary' : (currentScheme.hasUnpublishedChanges ? 'text-amber-700 font-semibold' : 'text-emerald-700 font-semibold')"
              >
                {{ !currentScheme.published ? '草稿' : (currentScheme.hasUnpublishedChanges ? '未发布' : '已发布') }}
              </span>
            </div>

            <!-- Divider -->
            <div class="h-3.5 w-px bg-black/[0.08] mx-0.5 shrink-0"></div>

            <!-- New Scheme Action (+) -->
            <button
              type="button"
              @click="createNewScheme"
              class="p-1 rounded-lg text-ios-tertiary hover:text-ios-blue hover:bg-black/5 active:bg-black/10 transition-colors shrink-0"
              title="新建中转方案"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
              </svg>
            </button>
          </div>

          <!-- Custom Apple Studio Dropdown Popover Menu -->
          <transition
            enter-active-class="transition duration-150 ease-out"
            enter-from-class="transform scale-95 opacity-0 -translate-y-1"
            enter-to-class="transform scale-100 opacity-100 translate-y-0"
            leave-active-class="transition duration-100 ease-in"
            leave-from-class="transform scale-100 opacity-100 translate-y-0"
            leave-to-class="transform scale-95 opacity-0 -translate-y-1"
          >
            <div
              v-if="schemeDropdownOpen"
              class="absolute left-0 top-full mt-2 w-80 max-h-[380px] bg-white/95 backdrop-blur-xl rounded-2xl shadow-[0_12px_40px_rgba(0,0,0,0.14)] border border-black/[0.08] p-1.5 z-50 flex flex-col divide-y divide-black/[0.04]"
            >
              <!-- Popover Header -->
              <div class="px-3 py-2 flex items-center justify-between">
                <span class="text-[11px] font-semibold text-neutral-400 uppercase tracking-wider">
                  中转方案 ({{ proxyNodeStore.schemes.length }})
                </span>
                <button
                  type="button"
                  @click="createNewScheme(); schemeDropdownOpen = false"
                  class="text-xs text-ios-blue hover:text-blue-700 font-semibold inline-flex items-center gap-1 transition-colors"
                >
                  <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>新建方案</span>
                </button>
              </div>

              <!-- Scheme Items List -->
              <div class="py-1 space-y-0.5 overflow-y-auto max-h-[250px]">
                <div
                  v-for="s in proxyNodeStore.schemes"
                  :key="s.id"
                  @click="selectScheme(s.id)"
                  class="px-3 py-2 rounded-xl cursor-pointer flex items-center justify-between gap-2.5 transition-all group"
                  :class="s.id === activeSchemeId ? 'bg-ios-blue/[0.08] text-ios-blue font-semibold shadow-2xs' : 'hover:bg-black/[0.04] text-neutral-800'"
                >
                  <!-- Left: Scheme Name + Details -->
                  <div class="min-w-0 flex-1">
                    <div class="flex items-center gap-1.5">
                      <span class="text-xs truncate font-medium" :class="s.id === activeSchemeId ? 'text-ios-blue font-bold' : 'text-neutral-900'">
                        {{ s.name }}
                      </span>
                    </div>
                    <div class="text-[11px] text-neutral-400 flex items-center gap-2 mt-0.5 font-normal">
                      <span>{{ s.nodes?.length || 0 }} 节点 · {{ s.connections?.length || 0 }} 连接</span>
                      <span>·</span>
                      <span class="px-1 py-0.2 rounded bg-black/[0.04] text-[10px] text-neutral-500">
                        {{ s.executionLocation === 'client' ? '客户端' : '服务端' }}
                      </span>
                    </div>
                  </div>

                  <!-- Right: Status Pill & Checkmark -->
                  <div class="flex items-center gap-2 shrink-0">
                    <span
                      class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded-full text-[10px] font-medium"
                      :class="!s.published ? 'bg-neutral-100 text-neutral-500' : (s.hasUnpublishedChanges ? 'bg-amber-50 text-amber-700 border border-amber-200/50' : 'bg-emerald-50 text-emerald-700 border border-emerald-200/50')"
                    >
                      <span
                        class="w-1.5 h-1.5 rounded-full shrink-0"
                        :class="!s.published ? 'bg-neutral-400' : (s.hasUnpublishedChanges ? 'bg-amber-500' : 'bg-emerald-500')"
                      ></span>
                      <span>{{ !s.published ? '草稿' : (s.hasUnpublishedChanges ? '未发布' : '已发布') }}</span>
                    </span>

                    <svg
                      v-if="s.id === activeSchemeId"
                      class="w-4 h-4 text-ios-blue shrink-0"
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M5 13l4 4L19 7" />
                    </svg>
                  </div>
                </div>
              </div>

              <!-- Popover Footer Action -->
              <div class="pt-1.5">
                <button
                  type="button"
                  @click="createNewScheme(); schemeDropdownOpen = false"
                  class="w-full py-2 px-3 rounded-xl bg-black/[0.02] hover:bg-black/[0.05] active:bg-black/[0.08] text-neutral-700 hover:text-ios-blue text-xs font-semibold flex items-center justify-center gap-1.5 transition-colors"
                >
                  <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>创建新中转方案</span>
                </button>
              </div>
            </div>
          </transition>
        </div>

        <!-- 2. Refined Segmented Execution Location Switcher -->
        <div class="hidden md:flex items-center p-1 rounded-xl bg-black/[0.03] border border-black/[0.06] text-xs shadow-2xs shrink-0">
          <button
            type="button"
            @click="setExecutionLocation('client')"
            :class="currentScheme.executionLocation === 'client' ? 'bg-white text-ios-blue shadow-xs font-semibold' : 'text-ios-secondary hover:text-ios-label hover:bg-black/[0.02] font-normal'"
            class="px-2.5 sm:px-3 py-1 rounded-lg transition-all inline-flex items-center gap-1.5"
            title="在客户端本地 (Clash / Mihomo 等) 执行中转链"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9.75 17L9 20l-1 1h8l-1-1-.75-3M3 13h18M5 17h14a2 2 0 002-2V5a2 2 0 00-2-2H5a2 2 0 00-2 2v10a2 2 0 002 2z" />
            </svg>
            <span>客户端执行</span>
          </button>
          <button
            type="button"
            @click="setExecutionLocation('server')"
            :class="currentScheme.executionLocation === 'server' ? 'bg-white text-ios-blue shadow-xs font-semibold' : 'text-ios-secondary hover:text-ios-label hover:bg-black/[0.02] font-normal'"
            class="px-2.5 sm:px-3 py-1 rounded-lg transition-all inline-flex items-center gap-1.5"
            title="通过已部署 Agent 的服务器自动编译下发 Sing-box / WireGuard 中转服务"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2" />
            </svg>
            <span>服务器执行</span>
          </button>
        </div>

        <!-- Local migration notice -->
        <div
          v-if="pendingLocalSchemes.length"
          class="hidden 2xl:flex items-center gap-1.5 px-2 py-0.5 rounded-xl bg-amber-50 border border-amber-200/70 text-[11px] text-amber-800 shrink-0"
        >
          <span>发现本地方案 {{ pendingLocalSchemes.length }} 个</span>
          <button type="button" @click="importLocalSchemes" class="font-bold text-amber-900 hover:underline">导入</button>
          <button type="button" @click="confirmIgnoreLocal = true" class="text-amber-700 hover:underline">忽略</button>
        </div>
      </div>

      <!-- Right: Action Buttons & Inspector Toggle -->
      <div class="flex items-center gap-2 shrink-0">

        <!-- Validate Button -->
        <button
          type="button"
          @click="validateScheme"
          class="ios-btn-secondary !py-1 text-xs inline-flex items-center gap-1"
        >
          <svg class="w-3.5 h-3.5 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span class="hidden sm:inline">校验链路</span>
        </button>

        <!-- Save Draft Button -->
        <button
          type="button"
          @click="saveDraft"
          class="ios-btn-secondary !py-1 text-xs inline-flex items-center gap-1"
        >
          <svg class="w-3.5 h-3.5 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7H5a2 2 0 00-2 2v9a2 2 0 002 2h14a2 2 0 002-2V9a2 2 0 00-2-2h-3m-1 4l-3 3m0 0l-3-3m3 3V4" />
          </svg>
          <span class="hidden sm:inline">保存草稿</span>
        </button>

        <button type="button" class="ios-btn-secondary !py-1 text-xs hidden md:inline" @click="checkScheme">检查</button>

        <!-- Primary Action: Generate Sub or Publish to Server -->
        <button
          v-if="currentScheme.executionLocation === 'client'"
          type="button"
          @click="openPublishModal = true"
          class="ios-btn-primary !py-1 text-xs inline-flex items-center gap-1 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 10v6m0 0l-3-3m3 3l3-3m2 8H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
          </svg>
          <span>生成订阅</span>
        </button>
        <button
          v-else
          type="button"
          @click="openPublishModal = true"
          class="ios-btn-primary !py-1 text-xs inline-flex items-center gap-1 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12" />
          </svg>
          <span>发布到服务器</span>
        </button>

        <div class="h-4 w-px bg-black/[0.08] shrink-0"></div>

        <!-- Toggle Right Inspector Button -->
        <button
          type="button"
          @click="rightPanelOpen = !rightPanelOpen"
          :class="rightPanelOpen ? 'bg-ios-blue/10 text-ios-blue border-ios-blue/20' : 'bg-black/[0.03] hover:bg-black/[0.06] text-ios-secondary border-black/[0.06]'"
          class="px-2.5 py-1.5 rounded-xl border transition-all flex items-center gap-1.5 text-xs font-semibold shrink-0"
          :title="rightPanelOpen ? '收起属性面板' : '展开属性面板'"
        >
          <span class="hidden xl:inline">{{ selectedNode ? '节点属性' : '方案概况' }}</span>
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
          </svg>
          <span class="w-1.5 h-1.5 rounded-full" :class="rightPanelOpen ? 'bg-ios-blue' : 'bg-neutral-300'"></span>
        </button>
      </div>
    </header>

    <!-- 2. Main Studio Workspace (Left Library + Canvas + Right Inspector) -->
    <div class="flex-1 flex min-h-0 overflow-hidden relative">
      <!-- ================= Left: Collapsible Resource Library ================= -->
      <aside
        class="transition-all duration-300 ease-in-out shrink-0 overflow-hidden bg-white border-r border-black/[0.08] flex flex-col z-20 shadow-xs"
        :class="leftPanelOpen ? 'w-64 xl:w-72' : 'w-0 border-r-0'"
        aria-label="拓扑资源库"
      >
        <!-- Resource Search & Header -->
        <div class="p-3 border-b border-black/[0.05] space-y-2 shrink-0 w-64 xl:w-72">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-1.5">
              <span class="font-bold text-xs text-ios-label">拓扑资源库</span>
              <span class="px-1.5 py-0.2 rounded-full text-[10px] bg-black/[0.05] text-ios-secondary font-mono">
                {{ filteredServers.length + selfHostedNodes.length + subscriptionNodes.length + manualNodes.length + filteredSharedTunnels.length }}
              </span>
            </div>
            <div class="flex items-center gap-1">
              <span class="text-[10px] text-ios-tertiary">拖拽上画布</span>
              <button
                type="button"
                @click="leftPanelOpen = false"
                class="p-1 rounded-lg hover:bg-black/5 text-ios-tertiary hover:text-ios-label transition-colors"
                title="收起资源库"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 19l-7-7 7-7" />
                </svg>
              </button>
            </div>
          </div>
          <div class="relative">
            <svg class="w-3.5 h-3.5 text-ios-tertiary absolute left-2.5 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              v-model="resourceSearch"
              placeholder="搜索服务器或代理节点…"
              class="w-full pl-8 pr-2.5 py-1.5 bg-[#F5F5F7] rounded-xl text-xs text-ios-label placeholder-neutral-400 focus:outline-none focus:ring-1 focus:ring-ios-blue"
            />
          </div>
        </div>

        <!-- Groups Tree -->
        <div class="flex-1 overflow-y-auto p-2 space-y-2 text-xs w-64 xl:w-72">
          <!-- Group 1: 我的服务器 (Servers) -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <button
              type="button"
              @click="toggleGroup('servers')"
              class="w-full px-2.5 py-2 flex items-center justify-between text-left font-bold text-ios-label hover:bg-black/[0.03] transition-colors"
            >
              <div class="flex items-center gap-1.5">
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.servers ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>我的服务器</span>
              </div>
              <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                {{ filteredServers.length }}
              </span>
            </button>

            <!-- Server Items (Draggable) -->
            <div v-show="groupExpanded.servers" class="p-1 space-y-1 border-t border-black/[0.04] bg-white">
              <div
                v-for="srv in filteredServers"
                :key="'srv-' + srv.id"
                draggable="true"
                @dragstart="onDragStart($event, { type: 'server', id: srv.id, name: srv.name, server: srv.publicIp || srv.hostname || '未分配 IP', protocol: 'server', status: srv.status === 'online' ? 'online' : 'offline', isManageable: true })"
                class="p-2 rounded-xl border border-black/[0.04] hover:border-ios-blue/40 hover:bg-blue-50/30 cursor-grab active:cursor-grabbing transition-all select-none group"
                :title="`IP: ${srv.publicIp || '未分配'}`"
              >
                <div class="flex items-center justify-between gap-1">
                  <span class="font-semibold text-ios-label truncate max-w-[150px]">{{ srv.name }}</span>
                  <span class="w-1.5 h-1.5 rounded-full" :class="srv.status === 'online' ? 'bg-emerald-500' : 'bg-neutral-400'"></span>
                </div>
                <div class="flex items-center justify-between text-[10px] text-ios-tertiary mt-0.5">
                  <span class="font-mono truncate">{{ srv.publicIp || srv.hostname || '未分配 IP' }}</span>
                  <span class="text-ios-blue font-medium">Agent 部署</span>
                </div>
              </div>
            </div>
          </div>

          <!-- Group 2: 我的自建节点 -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <button
              type="button"
              @click="toggleGroup('selfHosted')"
              class="w-full px-2.5 py-2 flex items-center justify-between text-left font-bold text-ios-label hover:bg-black/[0.03] transition-colors"
            >
              <div class="flex items-center gap-1.5">
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.selfHosted ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>我的自建节点</span>
              </div>
              <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                {{ selfHostedNodes.length }}
              </span>
            </button>

            <div v-show="groupExpanded.selfHosted" class="p-1 space-y-1 border-t border-black/[0.04] bg-white">
              <div v-if="!selfHostedNodes.length" class="p-2.5 text-center text-[10px] text-ios-tertiary">
                暂无自建节点
              </div>
              <div
                v-for="node in selfHostedNodes"
                :key="'node-' + node.id"
                draggable="true"
                @dragstart="onDragStart($event, { type: 'node', id: node.id, name: node.name, server: `${node.server}:${node.port}`, protocol: node.protocol, status: node.status, isManageable: false })"
                class="p-2 rounded-xl border border-black/[0.04] hover:border-ios-blue/40 hover:bg-blue-50/30 cursor-grab active:cursor-grabbing transition-all select-none"
                :title="`协议: ${node.protocol.toUpperCase()} 地址: ${node.server}:${node.port}`"
              >
                <div class="flex items-center justify-between gap-1">
                  <span class="font-semibold text-ios-label truncate max-w-[140px]">{{ node.name }}</span>
                  <span class="px-1 py-0.2 rounded text-[9px] font-mono uppercase bg-black/[0.05]">{{ node.protocol }}</span>
                </div>
                <div class="flex items-center justify-between text-[10px] text-ios-tertiary mt-0.5">
                  <span class="font-mono truncate">{{ node.server }}:{{ node.port }}</span>
                  <span :class="node.status === 'online' ? 'text-emerald-600' : 'text-neutral-400'">
                    {{ node.status === 'online' ? '在线' : '未检测' }}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <!-- Group 3: 机场导入节点 -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <button
              type="button"
              @click="toggleGroup('subscription')"
              class="w-full px-2.5 py-2 flex items-center justify-between text-left font-bold text-ios-label hover:bg-black/[0.03] transition-colors"
            >
              <div class="flex items-center gap-1.5">
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.subscription ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>机场导入节点</span>
              </div>
              <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                {{ subscriptionNodes.length }}
              </span>
            </button>

            <div v-show="groupExpanded.subscription" class="p-1 space-y-1 border-t border-black/[0.04] bg-white">
              <div v-if="!subscriptionNodes.length" class="p-2.5 text-center text-[10px] text-ios-tertiary">
                暂无机场订阅节点
              </div>
              <div
                v-for="node in subscriptionNodes"
                :key="'sub-' + node.id"
                draggable="true"
                @dragstart="onDragStart($event, { type: 'node', id: node.id, name: node.name, server: `${node.server}:${node.port}`, protocol: node.protocol, status: node.status, isManageable: false })"
                class="p-2 rounded-xl border border-black/[0.04] hover:border-ios-blue/40 hover:bg-blue-50/30 cursor-grab active:cursor-grabbing transition-all select-none"
                :title="`来源: ${node.sourceName} 地址: ${node.server}:${node.port}`"
              >
                <div class="flex items-center justify-between gap-1">
                  <span class="font-semibold text-ios-label truncate max-w-[140px]">{{ node.name }}</span>
                  <span class="px-1 py-0.2 rounded text-[9px] font-mono uppercase bg-purple-100 text-purple-700">{{ node.protocol }}</span>
                </div>
                <div class="flex items-center justify-between text-[10px] text-ios-tertiary mt-0.5">
                  <span class="truncate">{{ node.sourceName }}</span>
                  <span :class="node.status === 'online' ? 'text-emerald-600' : 'text-neutral-400'">
                    {{ node.status === 'online' ? '在线' : '未检测' }}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <!-- Group 4: 单独导入节点 -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <button
              type="button"
              @click="toggleGroup('manual')"
              class="w-full px-2.5 py-2 flex items-center justify-between text-left font-bold text-ios-label hover:bg-black/[0.03] transition-colors"
            >
              <div class="flex items-center gap-1.5">
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.manual ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>单独导入节点</span>
              </div>
              <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                {{ manualNodes.length }}
              </span>
            </button>

            <div v-show="groupExpanded.manual" class="p-1 space-y-1 border-t border-black/[0.04] bg-white">
              <div v-if="!manualNodes.length" class="p-2.5 text-center text-[10px] text-ios-tertiary">
                暂无单独导入节点
              </div>
              <div
                v-for="node in manualNodes"
                :key="'man-' + node.id"
                draggable="true"
                @dragstart="onDragStart($event, { type: 'node', id: node.id, name: node.name, server: `${node.server}:${node.port}`, protocol: node.protocol, status: node.status, isManageable: false })"
                class="p-2 rounded-xl border border-black/[0.04] hover:border-ios-blue/40 hover:bg-blue-50/30 cursor-grab active:cursor-grabbing transition-all select-none"
                :title="`地址: ${node.server}:${node.port}`"
              >
                <div class="flex items-center justify-between gap-1">
                  <span class="font-semibold text-ios-label truncate max-w-[140px]">{{ node.name }}</span>
                  <span class="px-1 py-0.2 rounded text-[9px] font-mono uppercase bg-blue-100 text-ios-blue">{{ node.protocol }}</span>
                </div>
                <div class="flex items-center justify-between text-[10px] text-ios-tertiary mt-0.5">
                  <span class="font-mono truncate">{{ node.server }}:{{ node.port }}</span>
                  <span :class="node.status === 'online' ? 'text-emerald-600' : 'text-neutral-400'">
                    {{ node.status === 'online' ? '在线' : '未检测' }}
                  </span>
                </div>
              </div>
            </div>
          </div>

          <!-- Group 5: 隧道 (Shared Tunnels across schemes) -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <div class="w-full px-2.5 py-2 flex items-center justify-between hover:bg-black/[0.03] transition-colors select-none">
              <button
                type="button"
                @click="toggleGroup('tunnels')"
                class="flex items-center gap-1.5 text-left font-bold text-ios-label flex-1"
              >
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.tunnels ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>隧道</span>
              </button>
              <div class="flex items-center gap-1.5 shrink-0">
                <button
                  type="button"
                  @click.stop="openEditor()"
                  class="text-[10px] text-ios-blue font-semibold hover:underline px-1 py-0.5 rounded hover:bg-blue-50"
                  title="新建隧道"
                >
                  + 新建
                </button>
                <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                  {{ filteredSharedTunnels.length }}
                </span>
              </div>
            </div>

            <!-- Tunnel Items (Draggable to canvas or clickable to add) -->
            <div v-show="groupExpanded.tunnels" class="p-1 space-y-1.5 border-t border-black/[0.04] bg-white">
              <div v-if="!filteredSharedTunnels.length" class="p-3 text-center text-ios-tertiary text-[11px] leading-relaxed">
                暂无隧道<br/>
                <button type="button" @click="openEditor()" class="text-ios-blue font-semibold hover:underline mt-1">
                  + 新建第一条隧道
                </button>
              </div>

              <div
                v-for="tunnel in filteredSharedTunnels"
                :key="tunnel.key"
                draggable="true"
                @dragstart="onDragStart($event, { type: 'tunnel', tunnel })"
                @click="onSharedTunnelClick(tunnel)"
                class="p-2 rounded-xl border transition-all select-none group cursor-grab active:cursor-grabbing relative"
                :class="[
                  isTunnelInCurrentScheme(tunnel)
                    ? 'border-blue-200 bg-blue-50/40 hover:border-ios-blue'
                    : 'border-black/[0.04] hover:border-ios-blue/40 hover:bg-neutral-50'
                ]"
                :title="isTunnelInCurrentScheme(tunnel) ? '已在当前方案中（点击聚焦）' : '拖拽到画布或点击加入方案'"
              >
                <!-- Title: Source → Target -->
                <div class="flex items-center justify-between gap-1">
                  <div class="font-semibold text-ios-label truncate text-[11px] flex items-center gap-1 min-w-0">
                    <span class="truncate max-w-[80px]" :title="tunnel.sourceName">{{ tunnel.sourceName }}</span>
                    <span class="text-ios-tertiary shrink-0">→</span>
                    <span class="truncate max-w-[80px]" :title="tunnel.targetName">{{ tunnel.targetName }}</span>
                  </div>
                  <div class="flex items-center gap-1 shrink-0">
                    <span
                      v-if="isTunnelInCurrentScheme(tunnel)"
                      class="text-[9px] font-medium text-ios-blue bg-blue-100/70 px-1 py-0.2 rounded"
                    >
                      已在方案
                    </span>
                    <button
                      v-else
                      type="button"
                      @click.stop="applySharedTunnelToScheme(tunnel)"
                      class="text-[9px] text-ios-blue font-semibold bg-blue-50 hover:bg-blue-100 px-1.5 py-0.5 rounded transition-colors"
                      title="加入当前方案"
                    >
                      + 加入
                    </button>
                    <button
                      type="button"
                      @click.stop="deleteSharedTunnel(tunnel)"
                      class="text-neutral-300 hover:text-rose-600 p-0.5 rounded opacity-0 group-hover:opacity-100 transition-opacity"
                      title="从资源库删除此隧道"
                    >
                      <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                      </svg>
                    </button>
                  </div>
                </div>

                <!-- Subtitle: Protocol, Port, Type -->
                <div class="flex items-center justify-between text-[10px] text-ios-secondary mt-1 font-mono">
                  <span class="truncate">{{ tunnel.subtitle }}</span>
                  <span
                    v-if="tunnel.statusText"
                    :class="tunnel.hasError ? 'text-rose-500' : 'text-emerald-600'"
                    class="shrink-0 font-sans text-[9px]"
                  >
                    {{ tunnel.statusText }}
                  </span>
                </div>
              </div>
            </div>
          </div>

        </div>
      </aside>

      <!-- ================= Center: Infinite Visual Canvas ================= -->
      <main
        ref="canvasViewport" class="flex-1 bg-[#F9F9FB] relative overflow-hidden select-none cursor-default min-w-0"
        @wheel.prevent="onWheel"
        @mousedown="onCanvasMouseDown"
        @dragover.prevent="onCanvasDragOver"
        @drop="onCanvasDrop"
      >
        <!-- Floating Quick Open Button for Left Library when collapsed -->
        <button
          v-if="!leftPanelOpen"
          type="button"
          @click="leftPanelOpen = true"
          class="absolute left-4 top-4 z-20 bg-white/95 backdrop-blur-md border border-black/[0.08] shadow-md px-3 py-1.5 rounded-xl text-xs font-semibold text-ios-label hover:text-ios-blue hover:shadow-lg transition-all flex items-center gap-1.5"
          title="展开拓扑资源库"
        >
          <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 6h16M4 12h16M4 18h7" />
          </svg>
          <span>拓扑资源库</span>
        </button>

        <!-- Floating Quick Open Button for Right Inspector when collapsed -->
        <button
          v-if="!rightPanelOpen"
          type="button"
          @click="rightPanelOpen = true"
          class="absolute right-4 top-4 z-20 bg-white/95 backdrop-blur-md border border-black/[0.08] shadow-md px-3 py-1.5 rounded-xl text-xs font-semibold text-ios-label hover:text-ios-blue hover:shadow-lg transition-all flex items-center gap-1.5"
          :title="selectedNode ? '查看选中节点属性' : '查看方案全局概况'"
        >
          <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
          </svg>
          <span>{{ selectedNode ? '节点属性' : '方案概况' }}</span>
          <span v-if="selectedNode" class="w-2 h-2 rounded-full bg-ios-blue animate-pulse"></span>
        </button>

        <!-- Canvas Transform Layer -->
        <div
          class="absolute inset-0 origin-top-left"
          :style="{
            transform: `translate(${panX}px, ${panY}px) scale(${zoom})`,
            backgroundImage: `radial-gradient(circle, rgba(0,0,0,0.12) 1.2px, transparent 1.2px)`,
            backgroundSize: `24px 24px`
          }"
        >
          <!-- SVG Connections Layer -->
          <svg class="absolute inset-0 w-[4000px] h-[4000px] pointer-events-none overflow-visible">
            <defs>
              <!-- Traffic Direction Marker (Arrow) -->
              <marker
                id="traffic-arrow"
                markerWidth="8"
                markerHeight="8"
                refX="7"
                refY="4"
                orient="auto"
                markerUnits="strokeWidth"
              >
                <path d="M0,1 L7,4 L0,7 Z" fill="#0071E3" />
              </marker>
              <marker
                id="traffic-arrow-selected"
                markerWidth="8"
                markerHeight="8"
                refX="7"
                refY="4"
                orient="auto"
                markerUnits="strokeWidth"
              >
                <path d="M0,1 L7,4 L0,7 Z" fill="#FF9500" />
              </marker>
            </defs>

            <!-- Render All Connections -->
            <g v-for="conn in currentScheme.connections" :key="conn.id">
              <!-- Background clickable thick path -->
              <path
                :d="calculatePath(conn)"
                class="stroke-transparent fill-none pointer-events-stroke cursor-pointer hover:stroke-black/10"
                stroke-width="18"
                @click.stop="selectConnection(conn)"
              />
              <!-- Foreground flow path with Arrow -->
              <path
                :d="calculatePath(conn)"
                :class="selectedConnection?.id === conn.id ? 'stroke-[#FF9500]' : 'stroke-ios-blue'"
                fill="none"
                stroke-width="2.5"
                stroke-dasharray="6,4"
                :marker-end="selectedConnection?.id === conn.id ? 'url(#traffic-arrow-selected)' : 'url(#traffic-arrow)'"
              >
                <animate attributeName="stroke-dashoffset" from="40" to="0" dur="1.2s" repeatCount="indefinite" />
              </path>
            </g>

            <!-- Dynamic Connecting Line -->
            <path
              v-if="connectingSource"
              :d="connectingPath"
              class="stroke-ios-blue fill-none"
              stroke-width="2"
              stroke-dasharray="4,4"
            />
          </svg>

          <!-- Canvas Node Instances -->
          <div
            v-for="item in currentScheme.nodes"
            :key="item.instanceId"
            class="absolute w-56 rounded-2xl bg-white border shadow-md p-3 transition-shadow select-none group cursor-move"
            :class="[
              selectedNode?.instanceId === item.instanceId
                ? 'border-ios-blue ring-2 ring-ios-blue/30 shadow-lg'
                : 'border-black/[0.08] hover:border-black/20'
            ]"
            :style="{ left: `${item.x}px`, top: `${item.y}px` }"
            @mousedown.stop="onNodeMouseDown($event, item)"
            @click.stop="selectNode(item)"
          >
            <!-- Card Header -->
            <div class="flex items-center justify-between gap-1 mb-1.5">
              <span
                class="px-1.5 py-0.2 rounded text-[10px] font-bold uppercase tracking-wider"
                :class="item.isManageable ? 'bg-blue-100 text-ios-blue' : 'bg-purple-100 text-purple-700'"
              >
                {{ item.isManageable ? '服务器' : '代理节点' }}
              </span>
              <span
                class="px-1.5 py-0.2 rounded-full text-[10px] font-bold"
                :class="roleBadgeClass(item.role)"
              >
                {{ roleBadgeText(item.role) }}
              </span>
            </div>

            <!-- Card Body -->
            <div class="space-y-1">
              <div class="font-bold text-xs text-ios-label truncate" :title="item.name">
                {{ item.name }}
              </div>
              <div class="text-[10px] font-mono text-ios-secondary truncate">
                {{ item.serverAddress }}
              </div>
              <div v-if="item.listenPort" class="text-[10px] font-mono text-ios-blue">
                监听端口: {{ item.listenPort }}
              </div>
            </div>

            <!-- Ports (Input Left, Output Right) -->
            <!-- Input Port (Target) -->
            <div
              class="absolute -left-2.5 top-1/2 -translate-y-1/2 w-5 h-5 rounded-full bg-white border-2 border-ios-blue flex items-center justify-center hover:scale-125 transition-transform cursor-crosshair shadow-2xs"
              title="连线终点 (接入端口)"
              @mouseup.stop="onPortMouseUp(item, 'input')"
            >
              <div class="w-1.5 h-1.5 rounded-full bg-ios-blue"></div>
            </div>

            <!-- Output Port (Source) -->
            <div
              class="absolute -right-2.5 top-1/2 -translate-y-1/2 w-5 h-5 rounded-full bg-white border-2 border-emerald-500 flex items-center justify-center hover:scale-125 transition-transform cursor-crosshair shadow-2xs"
              title="连线起点 (出口端口，点击并拖拽到下一节点)"
              @mousedown.stop="startConnecting($event, item)"
            >
              <div class="w-1.5 h-1.5 rounded-full bg-emerald-500"></div>
            </div>

            <!-- Quick Delete Floating Button -->
            <button
              type="button"
              @click.stop="removeNodeInstance(item.instanceId)"
              class="absolute -top-2 -right-2 w-5 h-5 rounded-full bg-rose-500 text-white flex items-center justify-center opacity-0 group-hover:opacity-100 hover:scale-110 transition-all shadow-xs"
              title="从画布删除此实例"
            >
              <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.5" d="M6 18L18 6M6 6l12 12" />
              </svg>
            </button>
          </div>
        </div>

        <!-- Canvas Floating View Controls (Zoom / Reset) -->
        <div class="absolute bottom-4 right-4 bg-white/90 backdrop-blur-md rounded-2xl border border-black/[0.08] p-1.5 shadow-md flex items-center gap-1 z-20">
          <button
            type="button"
            @click="zoomIn"
            class="p-1.5 rounded-xl hover:bg-black/5 text-ios-secondary hover:text-ios-label transition-colors"
            title="放大"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
          </button>
          <span class="text-[11px] font-mono font-bold text-ios-label px-1">
            {{ Math.round(zoom * 100) }}%
          </span>
          <button
            type="button"
            @click="zoomOut"
            class="p-1.5 rounded-xl hover:bg-black/5 text-ios-secondary hover:text-ios-label transition-colors"
            title="缩小"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M20 12H4" />
            </svg>
          </button>
          <button
            type="button"
            @click="resetView"
            class="px-2 py-1 rounded-xl hover:bg-black/5 text-[11px] font-medium text-ios-secondary hover:text-ios-label transition-colors"
            title="重置视角"
          >
            居中
          </button>
        </div>

        <!-- Canvas Bottom Tip -->
        <div class="absolute bottom-4 left-4 bg-black/70 backdrop-blur-md text-white text-[11px] px-3 py-1.5 rounded-xl pointer-events-none z-10 flex items-center gap-2">
          <span>💡 提示：从绿色右锚点拖出连线连接到蓝色左锚点，箭头表示流量流动方向。</span>
        </div>

      </main>

      <!-- ================= Right: Collapsible Inspector Property Panel ================= -->
      <aside
        class="transition-all duration-300 ease-in-out shrink-0 overflow-hidden bg-white border-l border-black/[0.08] flex flex-col z-20 shadow-xs"
        :class="rightPanelOpen ? 'w-72 xl:w-80' : 'w-0 border-l-0'"
        aria-label="属性检查器"
      >
        <!-- Header with dynamic title and close button -->
        <div class="p-3.5 border-b border-black/[0.06] flex items-center justify-between shrink-0 w-72 xl:w-80">
          <div class="flex items-center gap-2">
            <span class="w-5 h-5 rounded-md flex items-center justify-center text-xs" :class="selectedNode ? 'bg-ios-blue/10 text-ios-blue' : 'bg-black/[0.05] text-ios-secondary'">
              {{ selectedNode ? '⚙️' : '📋' }}
            </span>
            <h3 class="font-bold text-xs text-ios-label">
              {{ selectedNode ? '节点属性配置' : '方案全局概况' }}
            </h3>
            <span
              class="px-1.5 py-0.2 rounded-full text-[10px] font-semibold"
              :class="selectedNode ? 'bg-blue-100 text-ios-blue' : 'bg-black/[0.05] text-ios-secondary'"
            >
              {{ selectedNode ? (selectedNode.isManageable ? '服务器' : '代理节点') : '全局' }}
            </span>
          </div>
          <button
            type="button"
            @click="rightPanelOpen = false"
            class="p-1 rounded-lg hover:bg-black/5 text-ios-tertiary hover:text-ios-label transition-colors"
            title="收起属性面板"
          >
            <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
            </svg>
          </button>
        </div>

        <!-- Inspector Body -->
        <div class="flex-1 overflow-y-auto p-4 space-y-4 text-xs w-72 xl:w-80">
          <!-- Selected Node Inspector -->
          <div v-if="selectedNode" class="space-y-4">
          <div class="space-y-3 text-xs">
            <label class="block">
              <span class="font-medium text-ios-secondary">实例名称（来自节点，只读）</span>
              <input :value="selectedNode.name" readonly class="form-input-base text-xs mt-1" />
            </label>

            <div class="p-2.5 bg-[#F5F5F7] rounded-xl space-y-1">
              <div class="flex justify-between">
                <span class="text-ios-tertiary">节点类型</span>
                <span class="font-bold text-ios-label">{{ selectedNode.isManageable ? '自有服务器' : '外部代理节点' }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-ios-tertiary">原始地址</span>
                <span class="font-mono text-ios-label truncate max-w-[150px]">{{ selectedNode.serverAddress }}</span>
              </div>
            </div>

            <label class="block">
              <span class="font-medium text-ios-secondary">链路角色</span>
              <select v-model="selectedNode.role" @change="saveDraft" class="form-input-base text-xs mt-1">
                <option value="entry">入口节点 (Entry)</option>
                <option value="relay">中转跳点 (Relay)</option>
                <option value="exit">出口节点 (Exit)</option>
              </select>
            </label>

            <div v-if="selectedNode.isManageable" class="space-y-2 pt-2 border-t border-black/[0.05]">
              <div class="flex items-center justify-between">
                <span class="font-bold text-ios-label text-[11px]">服务器转发配置</span>
                <button
                  type="button"
                  @click="pickListenPortForSelectedNode"
                  class="text-[11px] text-ios-blue hover:text-blue-700 font-medium inline-flex items-center gap-1"
                >
                  <svg class="w-3 h-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                  </svg>
                  随机分配
                </button>
              </div>
              <label class="block">
                <div class="flex items-center justify-between">
                  <span class="text-ios-secondary">监听转发端口</span>
                  <span v-if="!selectedNodePortRange" class="text-[10px] text-ios-secondary">未设置范围；随机按钮使用编辑器默认范围 10000–65535，留空由后端分配</span>
                  <span v-if="selectedNodePortRange" class="text-[10px] text-ios-blue bg-ios-blue/10 px-1.5 py-0.5 rounded">
                    范围: {{ selectedNodePortRange.start }}-{{ selectedNodePortRange.end }}
                  </span>
                </div>
                <input
                  v-model.number="selectedNode.listenPort"
                  type="number"
                  placeholder="例如 10080"
                  class="form-input-base text-xs mt-1 font-mono"
                  @change="saveDraft"
                />
              </label>
            </div>
            <div v-else class="p-2.5 rounded-xl bg-amber-50/70 border border-amber-200/60 text-amber-800 text-[11px] leading-relaxed">
              ⚠️ 提示：外部导入节点只能作为代理链终点或出站跳，无法部署 Agent 安装转发服务。
            </div>

            <div class="pt-2">
              <button
                type="button"
                @click="removeNodeInstance(selectedNode.instanceId)"
                class="w-full ios-action-btn-danger justify-center"
              >
                从画布移除此节点
              </button>
            </div>
          </div>
  
          </div>

          <!-- Global Scheme Inspector -->
          <div v-else class="space-y-4">
          <div class="space-y-3 text-xs">
            <label class="block">
              <span class="font-medium text-ios-secondary">方案名称</span>
              <input v-model="currentScheme.name" @change="saveDraft" class="form-input-base text-xs mt-1" />
            </label>

            <div class="p-3 bg-[#F5F5F7] rounded-xl space-y-2">
              <div class="flex justify-between">
                <span class="text-ios-tertiary">执行位置</span>
                <span class="font-bold text-ios-label">
                  {{ currentScheme.executionLocation === 'client' ? '客户端执行' : '服务器执行' }}
                </span>
              </div>
              <div class="flex justify-between">
                <span class="text-ios-tertiary">画布节点数</span>
                <span class="font-mono font-bold text-ios-label">{{ currentScheme.nodes.length }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-ios-tertiary">中转连线条数</span>
                <span class="font-mono font-bold text-ios-label">{{ currentScheme.connections.length }}</span>
              </div>
              <div class="flex justify-between">
                <span class="text-ios-tertiary">发布状态</span>
                <span
                  class="font-bold"
                  :class="currentScheme.published ? 'text-emerald-600' : 'text-amber-600'"
                >
                  {{ publishing ? '发布中…' : (currentScheme.published ? '已发布生效' : '草稿 (未发布)') }}
                </span>
              </div>
            </div>

            <div v-if="publishResult && publishResult.nodes.length" class="p-2.5 rounded-xl bg-neutral-50 border border-black/[0.06] space-y-1">
              <div class="flex justify-between text-[11px] text-ios-tertiary">
                <span>最近一次发布</span>
                <span class="font-mono">{{ publishResult.batchId ? `批次 ${publishResult.batchId.slice(0, 8)}` : '单节点修订' }}</span>
              </div>
              <div v-for="node in publishResult.nodes" :key="node.nodeId" class="flex justify-between text-[11px]">
                <span class="text-ios-label font-medium">{{ node.name || node.nodeId }}</span>
                <span class="font-mono text-ios-secondary">#{{ node.revision }} · {{ publishStatusLabel(node.status) }}</span>
              </div>
              <div v-if="publishResult.warnings.length" class="text-amber-600 text-[11px]">{{ publishResult.warnings.join('；') }}</div>
            </div>

            <div class="p-2.5 rounded-xl bg-blue-50/70 border border-blue-200/60 text-ios-blue text-[11px] leading-relaxed">
              💡 提示：点击画布中的任意节点或连线可在本面板编辑详细属性；从左侧将服务器或代理节点拖入即可快速扩展链路。
            </div>

            <div class="pt-2">
              <button
                type="button"
                @click="confirmDeleteScheme = true"
                class="w-full ios-action-btn-danger justify-center"
              >
                删除此方案
              </button>
            </div>
          </div>
        </div>
      </div>
    </aside>
    </div>

    <TunnelEditorDrawer v-if="editor" :key="editor.key" :scheme="currentScheme" :servers="servers" :context="canvasContext()"
      :connection="editor.connection" :source="editor.source" :target="editor.target" :persist="persistTunnelResult" @close="closeEditor" />
    <AppDialog v-model="openPlanSummary" title="方案发布检查" :show-confirm="false">
      <TunnelPlanSummary :plan="plan" :checking="checking" :error="planError" />
    </AppDialog>

    <!-- Schemes / Modals -->
    <AppDialog
      v-model="openPublishModal"
      title="发布方案"
      description="选择方案执行位置并发布"
      confirm-text="确认发布"
      :loading="publishing"
      @confirm="handleConfirmPublish"
    >
      <div class="space-y-4 text-xs">
        <div class="flex items-center gap-4">
          <label class="flex items-center gap-1.5 cursor-pointer">
            <input
              type="radio"
              name="publishLocation"
              value="client"
              :checked="currentScheme.executionLocation === 'client'"
              @change="setExecutionLocation('client')"
              class="text-ios-blue focus:ring-ios-blue"
            />
            <span class="font-medium text-ios-label">客户端执行</span>
          </label>
          <label class="flex items-center gap-1.5 cursor-pointer">
            <input
              type="radio"
              name="publishLocation"
              value="server"
              :checked="currentScheme.executionLocation === 'server'"
              @change="setExecutionLocation('server')"
              class="text-ios-blue focus:ring-ios-blue"
            />
            <span class="font-medium text-ios-label">服务器执行</span>
          </label>
        </div>

        <div v-if="currentScheme.executionLocation === 'client'" class="space-y-3">
          <div class="flex items-center gap-2">
            <span class="text-ios-tertiary font-medium">客户端格式:</span>
            <select v-model="currentScheme.clientType" class="form-input-base text-xs !w-auto">
              <option value="clash">Clash / Clash Verge (Meta / Mihomo)</option>
            </select>
          </div>

          <div class="p-3 bg-[#F5F5F7] rounded-xl font-mono text-[11px] break-all border border-black/[0.04]">
            发布后加入已授权用户的 mihomo 订阅，其他格式会省略客户端链。
          </div>

          <p class="text-ios-tertiary">
            每一跳须为导入节点或自建入站，并授权给同一用户。发布会保存布局快照，不向 Agent 下发。
          </p>
        </div>

        <div v-else class="space-y-3">
          <p class="text-ios-label font-medium">
            即将通过各节点部署的 Agent 自动编译下发 Sing-box / WireGuard 隧道与中转核心配置。
          </p>
          <div class="p-3 bg-blue-50/70 border border-blue-200/60 rounded-xl space-y-1 text-ios-blue text-[11px]">
            <div>• 目标管理服务器: {{ currentScheme.nodes.filter(n => n.isManageable).length }} 台</div>
            <div>• 计划建立隧道: {{ currentTunnels.length }} 条</div>
            <div>• 状态: 发布时由服务端校验配置与冲突</div>
          </div>
        </div>
      </div>
    </AppDialog>

    <ConfirmDialog
      v-model="confirmIgnoreLocal"
      title="忽略本地方案"
      message="忽略后将从本浏览器删除这些方案，服务器上已有的方案不受影响。"
      confirm-text="忽略并删除"
      @confirm="ignoreLocalSchemes"
    />
    <ConfirmDialog
      v-model="confirmDeleteScheme"
      title="删除中转方案"
      :message="`删除「${currentScheme.name}」？只删除画布方案，入站、出站与代理链不受影响。`"
      @confirm="removeScheme"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, nextTick, watch } from 'vue';
import TunnelEditorDrawer from '../components/relay/TunnelEditorDrawer.vue';
import TunnelPlanSummary from '../components/relay/TunnelPlanSummary.vue';
import { useTunnelPlan } from '../components/relay/useTunnelPlan';
import { tunnelSummary, type TunnelPlan } from '../components/relay/tunnelEditorModel';
import { useRoute } from 'vue-router';
import PageHeader from '../../../components/PageHeader.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import { loadImports } from '../importApi';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { loadNodeMeta, getNodePortRange, getRandomPortForNode } from '../../nodes/nodeMeta';
import { proxyAPI, choices, canvasAPI, canvasSchemeToRelayScheme, relaySchemeToCanvasInput, type CanvasPublishResult, type CanvasSchemeContext, type Inbound } from '../api';
import {
  proxyNodeStore,
  loadStoredData,
  loadCanvasSchemes,
  persistCanvasScheme,
  createCanvasScheme,
  deleteCanvasScheme,
  LEGACY_SCHEMES_KEY,
  type ProxyNodeItem,
  type CanvasNodeInstance,
  type CanvasConnection,
  type ConnectionType,
  type RelayScheme
} from '../proxyNodes';

const route = useRoute();
const servers = ref<NodeView[]>([]);
const canvasViewport = ref<HTMLElement | null>(null);
const gestureCleanups = new Set<() => void>();
const resourceSearch = ref('');
const activeSchemeId = ref('');
const schemeDropdownOpen = ref(false);

function toggleSchemeDropdown() {
  schemeDropdownOpen.value = !schemeDropdownOpen.value;
}

function selectScheme(id: string) {
  activeSchemeId.value = id;
  schemeDropdownOpen.value = false;
  switchScheme();
}

function handleWindowClick() {
  schemeDropdownOpen.value = false;
}

// Group collapse states
const groupExpanded = reactive({
  servers: true,
  selfHosted: true,
  subscription: true,
  manual: false,
  tunnels: true
});

// Canvas Transform State
const zoom = ref(1);
const panX = ref(60);
const panY = ref(60);
const isPanning = ref(false);
const panStart = { x: 0, y: 0 };

// Selection states
const selectedNode = ref<CanvasNodeInstance | null>(null);
const selectedNodePortRange = computed(() => {
  if (!selectedNode.value || selectedNode.value.resourceType !== 'server') return null;
  return getNodePortRange(selectedNode.value.resourceId);
});

function pickListenPortForSelectedNode() {
  if (!selectedNode.value) return;
  const range = selectedNodePortRange.value;
  const port = getRandomPortForNode(selectedNode.value.resourceId);
  selectedNode.value.listenPort = port;
  saveDraft();
  if (range) {
    showToast(`已从服务器端口范围 (${range.start}-${range.end}) 选择候选端口: ${port}`, 'info');
  } else {
    showToast(`已从编辑器默认范围 10000–65535 选择候选端口: ${port}`, 'info');
  }
}
const selectedConnection = ref<CanvasConnection | null>(null);
const leftPanelOpen = ref(true);
const rightPanelOpen = ref(true);

function setExecutionLocation(loc: 'client' | 'server') {
  currentScheme.value.executionLocation = loc;
  handleExecutionLocationChange();
}

// Connecting state
const connectingSource = ref<CanvasNodeInstance | null>(null);
const connectingMousePos = reactive({ x: 0, y: 0 });

// Modals
const editor = ref<{ key: number; connection?: CanvasConnection; source?: string; target?: string } | null>(null);
const openPlanSummary = ref(false);
const openPublishModal = ref(false);
// The last publication's per-node result, as the server reported it.
const publishResult = ref<CanvasPublishResult | null>(null);
const publishing = ref(false);

// Data the canvas needs besides the schemes themselves: a reference is resolved
// against these lists on load, so display fields are never stored on the server.
const inbounds = ref<Inbound[]>([]);
// Schemes an earlier build kept in this browser, offered once for import.
const pendingLocalSchemes = ref<RelayScheme[]>([]);
const confirmIgnoreLocal = ref(false);
const confirmDeleteScheme = ref(false);

function canvasContext(): CanvasSchemeContext {
  return { servers: servers.value, inbounds: inbounds.value, imported: proxyNodeStore.manualNodes };
}

// An empty display state is never added to the store or persisted.
const emptyScheme: RelayScheme = { id: '', name: '', executionLocation: 'server', nodes: [], connections: [], published: false, updatedAt: '' };
const currentScheme = computed<RelayScheme>(() => proxyNodeStore.schemes.find(s => s.id === activeSchemeId.value) || proxyNodeStore.schemes[0] || emptyScheme);
watch(() => proxyNodeStore.schemes.map(s => s.id), ids => {
  if (!ids.includes(activeSchemeId.value)) activeSchemeId.value = ids[0] || '';
}, { immediate: true });

const currentTunnels = computed(() => currentScheme.value.connections);
const planInput = computed(() => relaySchemeToCanvasInput(currentScheme.value, canvasContext()));
const { plan, checking, error: planError, run: runPlan } = useTunnelPlan(
  () => JSON.stringify([currentScheme.value.id, currentScheme.value.serverUpdatedAt, planInput.value]),
  () => {
    if (!currentScheme.value.id) return null;
    if (currentScheme.value.executionLocation === 'client') return Promise.resolve(validateClientChain());
    return currentScheme.value.serverUpdatedAt ? canvasAPI.plan(currentScheme.value.id, planInput.value.layout) : null;
  }
);
watch(() => currentScheme.value.id, () => { editor.value = null; });
async function checkScheme() { openPlanSummary.value = true; await runPlan(); }
function validateClientChain(): TunnelPlan {
  const { instances, connections } = planInput.value.layout;
  let message = '';
  const ids = new Set(instances.map(i => i.instanceId));
  const incoming = new Map<string, number>(), outgoing = new Map<string, string>();
  if (instances.length < 2 || connections.length !== instances.length - 1) message = '客户端链需至少两个节点并连接为单一路径';
  if (ids.size !== instances.length) message = '节点实例标识重复';
  for (const i of instances) {
    if (!/^(import|inbound):.+/.test(i.ref)) message = '客户端链仅支持导入节点与入站引用';
    if (i.ref.startsWith('import:') && !proxyNodeStore.manualNodes.some(n => 'import:' + n.id === i.ref)) message = '导入节点引用已失效';
    if (i.ref.startsWith('inbound:') && !inbounds.value.some(n => 'inbound:' + n.id === i.ref)) message = '入站引用已失效';
  }
  for (const c of connections) {
    if (!ids.has(c.source) || !ids.has(c.target) || c.source === c.target || c.type !== 'proxy_chain') message = '客户端链包含无效连接或服务端隧道';
    if (outgoing.has(c.source) || incoming.has(c.target)) message = '客户端链不支持分支或汇合';
    outgoing.set(c.source, c.target); incoming.set(c.target, 1);
  }
  const starts = instances.filter(i => !incoming.has(i.instanceId));
  if (starts.length !== 1) message = '客户端链需有且仅有一个入口且无环路';
  const seen = new Set<string>();
  let cursor = starts[0]?.instanceId;
  while (cursor && !seen.has(cursor)) { seen.add(cursor); cursor = outgoing.get(cursor); }
  if (cursor || seen.size !== instances.length) message = '客户端链存在环路或不连通节点';
  return { ok: !message, message: message || '本地结构检查通过；发布时仍由服务端验证节点授权、协议与客户端兼容性。', servers: [], connections: [], conflicts: [], warnings: ['发布时将再次验证客户端链'] };
}
function openEditor(connection?: CanvasConnection, source?: string, target?: string) {
  selectedNode.value = null;
  selectedConnection.value = connection || null;
  editor.value = { key: Date.now(), connection, source, target };
}
function closeEditor() { editor.value = null; selectedConnection.value = null; }
async function persistTunnelResult(result: RelayScheme) {
  const scheme = currentScheme.value;
  if (result.id !== scheme.id) throw new Error('方案已切换，请重新编辑');
  const before = { nodes: scheme.nodes, connections: scheme.connections };
  scheme.nodes = result.nodes;
  scheme.connections = result.connections;
  syncTunnelsFromScheme(scheme);
  try { await saveDraft(true); showToast('已保存到方案'); }
  catch (e) { Object.assign(scheme, before); throw e; }
}


export interface SharedTunnel {
  key: string;
  sourceResourceId: string;
  sourceResourceType: 'server' | 'node';
  sourceName: string;
  sourceAddress: string;
  sourceStatus?: string;
  sourceIsManageable: boolean;
  targetResourceId: string;
  targetResourceType: 'server' | 'node';
  targetName: string;
  targetAddress: string;
  targetStatus?: string;
  targetIsManageable: boolean;
  type: ConnectionType;
  tunnelProtocol?: 'tls' | 'wireguard' | 'gost' | 'brook' | 'nftables';
  tunnelPort?: number;
  listenPort?: number;
  targetHost?: string;
  targetPort?: number;
  supportTcp: boolean;
  supportUdp: boolean;
  title: string;
  subtitle: string;
  statusText?: string;
  hasError?: boolean;
}

const SHARED_TUNNELS_KEY = 'qui_shared_tunnels_v1';

function loadStoredSharedTunnels(): SharedTunnel[] {
  try {
    const raw = localStorage.getItem(SHARED_TUNNELS_KEY);
    if (raw) return JSON.parse(raw);
  } catch {}
  return [];
}

function saveStoredSharedTunnels(list: SharedTunnel[]) {
  try {
    localStorage.setItem(SHARED_TUNNELS_KEY, JSON.stringify(list));
  } catch {}
}

const sharedTunnelPool = ref<SharedTunnel[]>(loadStoredSharedTunnels());

function updateSharedTunnelPool(tunnel: SharedTunnel) {
  const list = [...sharedTunnelPool.value];
  const idx = list.findIndex(t => t.key === tunnel.key);
  if (idx >= 0) {
    list[idx] = tunnel;
  } else {
    list.push(tunnel);
  }
  sharedTunnelPool.value = list;
  saveStoredSharedTunnels(list);
}

function syncTunnelsFromScheme(scheme: RelayScheme) {
  for (const conn of scheme.connections) {
    const src = scheme.nodes.find(n => n.instanceId === conn.sourceInstanceId);
    const tgt = scheme.nodes.find(n => n.instanceId === conn.targetInstanceId);
    if (!src || !tgt) continue;

    const summary = tunnelSummary(scheme, conn);
    const key = `${src.resourceType}:${src.resourceId}->${tgt.resourceType}:${tgt.resourceId}:${conn.type}:${conn.tunnelProtocol || ''}:${conn.tunnelPort || ''}:${conn.listenPort || ''}:${conn.targetPort || ''}`;
    updateSharedTunnelPool({
      key,
      sourceResourceId: src.resourceId,
      sourceResourceType: src.resourceType,
      sourceName: src.name,
      sourceAddress: src.serverAddress,
      sourceStatus: src.status,
      sourceIsManageable: src.isManageable,
      targetResourceId: tgt.resourceId,
      targetResourceType: tgt.resourceType,
      targetName: tgt.name,
      targetAddress: tgt.serverAddress,
      targetStatus: tgt.status,
      targetIsManageable: tgt.isManageable,
      type: conn.type,
      tunnelProtocol: conn.tunnelProtocol,
      tunnelPort: conn.tunnelPort,
      listenPort: conn.listenPort,
      targetHost: conn.targetHost,
      targetPort: conn.targetPort,
      supportTcp: conn.supportTcp,
      supportUdp: conn.supportUdp,
      title: summary.title || `${src.name} → ${tgt.name}`,
      subtitle: `${summary.kind} ${summary.network}`,
      statusText: conn.invalidReason ? '异常' : '可用',
      hasError: !!conn.invalidReason
    });
  }
}

watch(() => proxyNodeStore.schemes, (schemes) => {
  schemes.forEach(s => syncTunnelsFromScheme(s));
}, { immediate: true, deep: true });

const allSharedTunnels = computed<SharedTunnel[]>(() => {
  const map = new Map<string, SharedTunnel>();
  for (const t of sharedTunnelPool.value) {
    map.set(t.key, t);
  }
  for (const s of proxyNodeStore.schemes) {
    for (const conn of s.connections) {
      const src = s.nodes.find(n => n.instanceId === conn.sourceInstanceId);
      const tgt = s.nodes.find(n => n.instanceId === conn.targetInstanceId);
      if (!src || !tgt) continue;
      const summary = tunnelSummary(s, conn);
      const key = `${src.resourceType}:${src.resourceId}->${tgt.resourceType}:${tgt.resourceId}:${conn.type}:${conn.tunnelProtocol || ''}:${conn.tunnelPort || ''}:${conn.listenPort || ''}:${conn.targetPort || ''}`;
      map.set(key, {
        key,
        sourceResourceId: src.resourceId,
        sourceResourceType: src.resourceType,
        sourceName: src.name,
        sourceAddress: src.serverAddress,
        sourceStatus: src.status,
        sourceIsManageable: src.isManageable,
        targetResourceId: tgt.resourceId,
        targetResourceType: tgt.resourceType,
        targetName: tgt.name,
        targetAddress: tgt.serverAddress,
        targetStatus: tgt.status,
        targetIsManageable: tgt.isManageable,
        type: conn.type,
        tunnelProtocol: conn.tunnelProtocol,
        tunnelPort: conn.tunnelPort,
        listenPort: conn.listenPort,
        targetHost: conn.targetHost,
        targetPort: conn.targetPort,
        supportTcp: conn.supportTcp,
        supportUdp: conn.supportUdp,
        title: summary.title || `${src.name} → ${tgt.name}`,
        subtitle: `${summary.kind} ${summary.network}`,
        statusText: conn.invalidReason ? '异常' : '可用',
        hasError: !!conn.invalidReason
      });
    }
  }
  return Array.from(map.values());
});

const filteredSharedTunnels = computed(() => {
  if (!resourceSearch.value.trim()) return allSharedTunnels.value;
  const q = resourceSearch.value.trim().toLowerCase();
  return allSharedTunnels.value.filter(t =>
    t.title.toLowerCase().includes(q) ||
    t.sourceName.toLowerCase().includes(q) ||
    t.targetName.toLowerCase().includes(q) ||
    t.subtitle.toLowerCase().includes(q)
  );
});

function getTunnelInCurrentScheme(tunnel: SharedTunnel): CanvasConnection | undefined {
  const scheme = currentScheme.value;
  return scheme.connections.find(c => {
    const src = scheme.nodes.find(n => n.instanceId === c.sourceInstanceId);
    const tgt = scheme.nodes.find(n => n.instanceId === c.targetInstanceId);
    if (!src || !tgt) return false;
    return src.resourceId === tunnel.sourceResourceId &&
           src.resourceType === tunnel.sourceResourceType &&
           tgt.resourceId === tunnel.targetResourceId &&
           tgt.resourceType === tunnel.targetResourceType &&
           c.type === tunnel.type;
  });
}

function isTunnelInCurrentScheme(tunnel: SharedTunnel): boolean {
  return !!getTunnelInCurrentScheme(tunnel);
}

function onSharedTunnelClick(tunnel: SharedTunnel) {
  const existingConn = getTunnelInCurrentScheme(tunnel);
  if (existingConn) {
    selectConnection(existingConn);
    showToast(`已高亮当前方案中的隧道: ${tunnel.title}`);
  } else {
    applySharedTunnelToScheme(tunnel);
  }
}

function deleteSharedTunnel(tunnel: SharedTunnel) {
  sharedTunnelPool.value = sharedTunnelPool.value.filter(t => t.key !== tunnel.key);
  saveStoredSharedTunnels(sharedTunnelPool.value);
  const conn = getTunnelInCurrentScheme(tunnel);
  if (conn) {
    currentScheme.value.connections = currentScheme.value.connections.filter(c => c.id !== conn.id);
    saveDraft();
  }
  showToast(`已删除隧道: ${tunnel.title}`);
}

function applySharedTunnelToScheme(tunnel: SharedTunnel, canvasX = 300, canvasY = 200) {
  const scheme = currentScheme.value;

  // 1. Locate or create source node instance
  let sourceInst = scheme.nodes.find(
    n => n.resourceType === tunnel.sourceResourceType && n.resourceId === tunnel.sourceResourceId
  );
  if (!sourceInst) {
    const serverObj = servers.value.find(s => s.id === tunnel.sourceResourceId);
    sourceInst = {
      instanceId: `inst-${Date.now()}-src`,
      resourceId: tunnel.sourceResourceId,
      resourceType: tunnel.sourceResourceType,
      name: tunnel.sourceName,
      protocol: tunnel.sourceResourceType === 'server' ? 'server' : 'custom',
      serverAddress: tunnel.sourceAddress || serverObj?.publicIp || '',
      status: (serverObj?.status as any) || (tunnel.sourceStatus as any) || 'online',
      isManageable: tunnel.sourceIsManageable,
      x: Math.max(20, canvasX - 160),
      y: Math.max(20, canvasY - 30),
      role: 'entry',
      listenPort: tunnel.listenPort
    };
    scheme.nodes.push(sourceInst);
  }

  // 2. Locate or create target node instance
  let targetInst = scheme.nodes.find(
    n => n.resourceType === tunnel.targetResourceType && n.resourceId === tunnel.targetResourceId
  );
  if (!targetInst) {
    const serverObj = servers.value.find(s => s.id === tunnel.targetResourceId);
    targetInst = {
      instanceId: `inst-${Date.now()}-tgt`,
      resourceId: tunnel.targetResourceId,
      resourceType: tunnel.targetResourceType,
      name: tunnel.targetName,
      protocol: tunnel.targetResourceType === 'server' ? 'server' : 'custom',
      serverAddress: tunnel.targetAddress || serverObj?.publicIp || '',
      status: (serverObj?.status as any) || (tunnel.targetStatus as any) || 'online',
      isManageable: tunnel.targetIsManageable,
      x: Math.max(20, canvasX + 160),
      y: Math.max(20, canvasY - 30),
      role: scheme.executionLocation === 'client' ? 'exit' : 'relay',
      targetPort: tunnel.targetPort
    };
    scheme.nodes.push(targetInst);
  }

  // 3. Check if connection exists
  const existingConn = scheme.connections.find(
    c => c.sourceInstanceId === sourceInst!.instanceId && c.targetInstanceId === targetInst!.instanceId
  );
  if (existingConn) {
    selectedConnection.value = existingConn;
    selectedNode.value = null;
    showToast('当前方案中已存在该隧道连接', 'warning');
    return;
  }

  // 4. Create and link connection
  const newConn: CanvasConnection = {
    id: `conn-${Date.now()}-${Math.floor(Math.random() * 1000)}`,
    sourceInstanceId: sourceInst.instanceId,
    targetInstanceId: targetInst.instanceId,
    type: tunnel.type || 'tunnel',
    tunnelProtocol: tunnel.tunnelProtocol,
    tunnelPort: tunnel.tunnelPort,
    listenPort: tunnel.listenPort,
    targetHost: tunnel.targetHost,
    targetPort: tunnel.targetPort,
    supportTcp: tunnel.supportTcp ?? true,
    supportUdp: tunnel.supportUdp ?? true,
    status: 'valid'
  };

  scheme.connections.push(newConn);
  selectedConnection.value = newConn;
  selectedNode.value = null;
  saveDraft();
  showToast(`已将隧道「${tunnel.title}」添加到当前方案`, 'success');
}

// Filtered resources
const filteredServers = computed(() => {
  return servers.value.filter(s => {
    if (resourceSearch.value.trim()) {
      const q = resourceSearch.value.trim().toLowerCase();
      return (s.name || '').toLowerCase().includes(q) || (s.publicIp || '').includes(q);
    }
    return true;
  });
});

const filteredNodesGroup = computed(() => {
  return proxyNodeStore.manualNodes.filter(n => {
    if (resourceSearch.value.trim()) {
      const q = resourceSearch.value.trim().toLowerCase();
      return (n.name || '').toLowerCase().includes(q) || (n.server || '').includes(q);
    }
    return true;
  });
});
const selfHostedNodes = computed(() => {
  return filteredNodesGroup.value.filter(n => n.sourceType === 'self_hosted');
});

const subscriptionNodes = computed(() => {
  return filteredNodesGroup.value.filter(n => n.sourceType === 'subscription');
});

const manualNodes = computed(() => {
  return filteredNodesGroup.value.filter(n => n.sourceType === 'manual');
});


function toggleGroup(g: 'servers' | 'selfHosted' | 'subscription' | 'manual' | 'tunnels') {
  groupExpanded[g] = !groupExpanded[g];
}

// Switching Execution Location (Core Requirement 2: Default expansion)
function applyExecutionDefaults() {
  if (currentScheme.value.executionLocation === 'client') {
    groupExpanded.servers = false;
    groupExpanded.selfHosted = true;
    groupExpanded.subscription = true;
    groupExpanded.manual = true;
  } else {
    groupExpanded.servers = true;
    groupExpanded.selfHosted = true;
    groupExpanded.subscription = false;
    groupExpanded.manual = false;
  }
  groupExpanded.tunnels = true;
}

function handleExecutionLocationChange() {
  applyExecutionDefaults();
  saveDraft();
}

function switchScheme() {
  selectedNode.value = null;
  selectedConnection.value = null;
  // Switching reads another scheme; it is not an edit of the one being left.
  applyExecutionDefaults();
}

async function createNewScheme() {
  try {
    const usedNames = new Set(proxyNodeStore.schemes.map(s => s.name));
    let number = 1;
    while (usedNames.has('未命名方案 ' + number)) number++;
    const scheme = await createCanvasScheme('未命名方案 ' + number, 'server', canvasContext());
    activeSchemeId.value = scheme.id;
    selectedNode.value = null;
    selectedConnection.value = null;
    showToast('已创建新方案');
  } catch (error) {
    showToast(`创建方案失败: ${(error as Error).message}`, 'error');
  }
}

// Deleting a scheme removes the drawing only: the objects it draws stay.
async function removeScheme() {
  const scheme = currentScheme.value;
  if (!scheme.id) return;
  try {
    await flushPendingSaves();
    await deleteCanvasScheme(scheme.id);
    selectedNode.value = null;
    selectedConnection.value = null;
    activeSchemeId.value = proxyNodeStore.schemes[0]?.id || '';
    showToast('已删除方案');
  } catch (error) {
    showToast(`删除方案失败: ${(error as Error).message}`, 'error');
  }
}

// ----------------- Drag & Drop from Library to Canvas -----------------
let draggedPayload: any = null;

function onDragStart(e: DragEvent, payload: any) {
  draggedPayload = payload;
  if (e.dataTransfer) {
    e.dataTransfer.setData('text/plain', JSON.stringify(payload));
  }
}

function onCanvasDragOver(e: DragEvent) {
  e.preventDefault();
}

function onCanvasDrop(e: DragEvent) {
  e.preventDefault();
  if (!draggedPayload) return;

  const rect = (e.currentTarget as HTMLElement).getBoundingClientRect();
  const clientX = e.clientX - rect.left;
  const clientY = e.clientY - rect.top;

  // Transform coordinates back to canvas matrix
  const canvasX = Math.round((clientX - panX.value) / zoom.value);
  const canvasY = Math.round((clientY - panY.value) / zoom.value);

  if (draggedPayload.type === 'tunnel') {
    applySharedTunnelToScheme(draggedPayload.tunnel, canvasX, canvasY);
    draggedPayload = null;
    return;
  }

  const role = currentScheme.value.nodes.length === 0 ? 'entry' : 'relay';
  let initialPort: number | undefined = undefined;
  // Keep the port absent: the backend allocates relay listeners from node metadata.

  // Requirement 2: Repeated drops create new instance without modifying original resource
  const newInst: CanvasNodeInstance = {
    instanceId: 'inst-' + Date.now() + '-' + Math.floor(Math.random() * 1000),
    resourceId: draggedPayload.id,
    resourceType: draggedPayload.type,
    name: draggedPayload.name,
    protocol: draggedPayload.protocol,
    serverAddress: draggedPayload.server,
    status: draggedPayload.status,
    isManageable: draggedPayload.isManageable,
    x: Math.max(20, canvasX - 100),
    y: Math.max(20, canvasY - 30),
    role,
    listenPort: initialPort
  };

  currentScheme.value.nodes.push(newInst);
  selectedNode.value = newInst;
  selectedConnection.value = null;
  draggedPayload = null;
  saveDraft();
}

// ----------------- Canvas Pan & Zoom -----------------
function onWheel(e: WheelEvent) {
  const delta = e.deltaY > 0 ? -0.1 : 0.1;
  const newZoom = Math.min(Math.max(zoom.value + delta, 0.4), 2.0);
  zoom.value = Math.round(newZoom * 10) / 10;
}

function zoomIn() {
  if (zoom.value < 2.0) zoom.value = Math.round((zoom.value + 0.1) * 10) / 10;
}

function zoomOut() {
  if (zoom.value > 0.4) zoom.value = Math.round((zoom.value - 0.1) * 10) / 10;
}

function resetView() {
  zoom.value = 1;
  panX.value = 60;
  panY.value = 60;
}

function onCanvasMouseDown(e: MouseEvent) {
  if (e.target !== e.currentTarget && !(e.target as HTMLElement).classList.contains('absolute')) return;
  selectedNode.value = null;
  selectedConnection.value = null;
  isPanning.value = true;
  panStart.x = e.clientX - panX.value;
  panStart.y = e.clientY - panY.value;

  const onMouseMove = (ev: MouseEvent) => {
    if (isPanning.value) {
      panX.value = ev.clientX - panStart.x;
      panY.value = ev.clientY - panStart.y;
    }
  };

  const onMouseUp = () => {
    isPanning.value = false;
    window.removeEventListener('mousemove', onMouseMove);
    window.removeEventListener('mouseup', onMouseUp, true);
    window.removeEventListener('blur', onMouseUp);
    gestureCleanups.delete(onMouseUp);
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp, true);
  window.addEventListener('blur', onMouseUp);
  gestureCleanups.add(onMouseUp);
}

// Node dragging inside canvas
function onNodeMouseDown(e: MouseEvent, node: CanvasNodeInstance) {
  selectedNode.value = node;
  selectedConnection.value = null;
  const startX = e.clientX;
  const startY = e.clientY;
  const initNodeX = node.x;
  const initNodeY = node.y;

  const onMouseMove = (ev: MouseEvent) => {
    const dx = (ev.clientX - startX) / zoom.value;
    const dy = (ev.clientY - startY) / zoom.value;
    node.x = Math.max(0, Math.round(initNodeX + dx));
    node.y = Math.max(0, Math.round(initNodeY + dy));
  };

  const onMouseUp = () => {
    window.removeEventListener('mousemove', onMouseMove);
    window.removeEventListener('mouseup', onMouseUp, true);
    window.removeEventListener('blur', onMouseUp);
    gestureCleanups.delete(onMouseUp);
    saveDraft();
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp, true);
  window.addEventListener('blur', onMouseUp);
  gestureCleanups.add(onMouseUp);
}

// ----------------- Directed Connecting Logic -----------------
function startConnecting(e: MouseEvent, source: CanvasNodeInstance) {
  connectingSource.value = source;
  connectingMousePos.x = source.x + 224;
  connectingMousePos.y = source.y + 45;

  const onMouseMove = (ev: MouseEvent) => {
    const rect = canvasViewport.value?.getBoundingClientRect();
    if (rect) {
      connectingMousePos.x = (ev.clientX - rect.left - panX.value) / zoom.value;
      connectingMousePos.y = (ev.clientY - rect.top - panY.value) / zoom.value;
    }
  };

  const onMouseUp = () => {
    setTimeout(() => { if (connectingSource.value === source) connectingSource.value = null; }, 0);
    window.removeEventListener('mousemove', onMouseMove);
    window.removeEventListener('mouseup', onMouseUp, true);
    window.removeEventListener('blur', onMouseUp);
    gestureCleanups.delete(onMouseUp);
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp, true);
  window.addEventListener('blur', onMouseUp);
  gestureCleanups.add(onMouseUp);
}

function onPortMouseUp(target: CanvasNodeInstance, portType: 'input') {
  if (!connectingSource.value || connectingSource.value.instanceId === target.instanceId) return;

  const source = connectingSource.value;
  openEditor(undefined, source.instanceId, target.instanceId);
  connectingSource.value = null;
}

// Compute smooth cubic bezier path for connections
function calculatePath(conn: CanvasConnection): string {
  const source = currentScheme.value.nodes.find(n => n.instanceId === conn.sourceInstanceId);
  const target = currentScheme.value.nodes.find(n => n.instanceId === conn.targetInstanceId);
  if (!source || !target) return '';

  const x1 = source.x + 224; // Source right edge port
  const y1 = source.y + 45;
  const x2 = target.x;       // Target left edge port
  const y2 = target.y + 45;

  const dx = Math.abs(x2 - x1) * 0.5;
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
}

const connectingPath = computed(() => {
  if (!connectingSource.value) return '';
  const x1 = connectingSource.value.x + 224;
  const y1 = connectingSource.value.y + 45;
  const x2 = connectingMousePos.x;
  const y2 = connectingMousePos.y;
  const dx = Math.abs(x2 - x1) * 0.5;
  return `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
});

function selectNode(item: CanvasNodeInstance) {
  selectedNode.value = item;
  selectedConnection.value = null;
  rightPanelOpen.value = true;
}

function selectConnection(conn: CanvasConnection) { openEditor(conn); }

function removeNodeInstance(instanceId: string) {
  currentScheme.value.nodes = currentScheme.value.nodes.filter(n => n.instanceId !== instanceId);
  currentScheme.value.connections = currentScheme.value.connections.filter(
    c => c.sourceInstanceId !== instanceId && c.targetInstanceId !== instanceId
  );
  selectedNode.value = null;
  selectedConnection.value = null;
  saveDraft();
  showToast('已从画布移除实例');
}

function getNodeNameByInst(instId: string): string {
  return currentScheme.value.nodes.find(n => n.instanceId === instId)?.name || '未知节点';
}

function roleBadgeClass(role: string): string {
  if (role === 'entry') return 'bg-emerald-100 text-emerald-800';
  if (role === 'relay') return 'bg-amber-100 text-amber-800';
  return 'bg-blue-100 text-ios-blue';
}

function roleBadgeText(role: string): string {
  if (role === 'entry') return '入口 (Entry)';
  if (role === 'relay') return '中转 (Relay)';
  return '出口 (Exit)';
}

// ----------------- Scheme Validation & Draft (Core Requirement 5) -----------------
function validateScheme() { void checkScheme(); }

function saveDraft(immediate: boolean | Event = false): Promise<void> | void {
  const scheme = currentScheme.value;
  if (!scheme.id) return;
  scheme.updatedAt = new Date().toISOString();
  if (scheme.published) {
    scheme.hasUnpublishedChanges = true;
  }
  if (immediate === true) {
    const pending = saveTimers.get(scheme);
    if (pending) { clearTimeout(pending); saveTimers.delete(scheme); }
    const flushed = enqueueWrite(() => persistCanvasScheme(scheme, canvasContext()));
    saveQueue = flushed.catch(() => undefined);
    return flushed;
  }
  scheduleSave(scheme);
}

// Canvas edits arrive per mousemove while a node is dragged, so the write waits
// for the gesture to settle. Every path that used to call saveSchemes comes
// through here. Saves are serialized: the second write needs the version the
// first one stored, otherwise the server would rightly call it stale.
const SAVE_DEBOUNCE_MS = 800;
const saveTimers = new Map<RelayScheme, ReturnType<typeof setTimeout>>();
let saveQueue: Promise<void> = Promise.resolve();
let queuedWrites = 0;
function enqueueWrite(write: () => Promise<void>): Promise<void> {
  queuedWrites++;
  const result = saveQueue.then(write).finally(() => { queuedWrites--; });
  saveQueue = result.catch(() => undefined);
  return result;
}
async function flushPendingSaves() {
  for (const [scheme, timer] of saveTimers) {
    clearTimeout(timer); saveTimers.delete(scheme);
    void enqueueWrite(() => persistScheme(scheme));
  }
  await saveQueue;
}
function warnUnsaved(event: BeforeUnloadEvent) {
  if (saveTimers.size || queuedWrites) { event.preventDefault(); event.returnValue = ''; }
}
window.addEventListener('beforeunload', warnUnsaved);

function scheduleSave(scheme: RelayScheme) {
  const pending = saveTimers.get(scheme);
  if (pending) clearTimeout(pending);
  saveTimers.set(scheme, setTimeout(() => {
    saveTimers.delete(scheme);
    void enqueueWrite(() => persistScheme(scheme));
  }, SAVE_DEBOUNCE_MS));
}

async function persistScheme(scheme: RelayScheme) {
  const previousId = scheme.id;
  try {
    await persistCanvasScheme(scheme, canvasContext());
    if (previousId !== scheme.id && activeSchemeId.value === previousId) activeSchemeId.value = scheme.id;
  } catch (error) {
    if (error instanceof APIError && error.code === 'stale_scheme') {
      showToast('方案已在其他地方修改，请刷新', 'error');
      await reloadSchemes();
      if (!proxyNodeStore.schemes.some(item => item.id === activeSchemeId.value)) {
        activeSchemeId.value = proxyNodeStore.schemes[0]?.id || '';
      }
      return;
    }
    showToast(`保存方案失败: ${(error as Error).message}`, 'error');
  }
}

async function reloadSchemes() {
  try {
    await loadCanvasSchemes(canvasContext());
  } catch (error) {
    showToast(`载入中转方案失败: ${(error as Error).message}`, 'error');
  }
}

// An earlier build kept schemes in this browser. Offer them once; the key is
// only removed after a complete import or an explicit ignore.
function readLocalSchemes() {
  try {
    const raw = localStorage.getItem(LEGACY_SCHEMES_KEY);
    if (!raw) {
      pendingLocalSchemes.value = [];
      return;
    }
    const stored: RelayScheme[] = JSON.parse(raw);
    const pending = Array.isArray(stored) ? stored.filter(item => !proxyNodeStore.schemes.some(existing => existing.name === item.name)) : [];
    pendingLocalSchemes.value = pending;
    if (!pending.length) localStorage.removeItem(LEGACY_SCHEMES_KEY);
  } catch {
    pendingLocalSchemes.value = [];
    localStorage.removeItem(LEGACY_SCHEMES_KEY);
  }
}

async function importLocalSchemes() {
  const failed: RelayScheme[] = [];
  let imported = 0;
  for (const scheme of pendingLocalSchemes.value) {
    if (proxyNodeStore.schemes.some(item => item.name === scheme.name)) continue;
    try {
      const stored = await canvasAPI.create({ name: scheme.name, execution: scheme.executionLocation, layout: { instances: [], connections: [] } });
      const local = canvasSchemeToRelayScheme(stored, canvasContext());
      local.nodes = scheme.nodes;
      local.connections = scheme.connections;
      await persistCanvasScheme(local, canvasContext());
      imported += 1;
    } catch (error) {
      failed.push(scheme);
      showToast(`导入方案「${scheme.name}」失败: ${(error as Error).message}`, 'error');
    }
  }
  pendingLocalSchemes.value = failed;
  if (!failed.length) localStorage.removeItem(LEGACY_SCHEMES_KEY);
  await reloadSchemes();
  if (!proxyNodeStore.schemes.some(item => item.id === activeSchemeId.value)) {
    activeSchemeId.value = proxyNodeStore.schemes[0]?.id || '';
  }
  if (imported) showToast(`已导入 ${imported} 个本地方案`);
}

function ignoreLocalSchemes() {
  localStorage.removeItem(LEGACY_SCHEMES_KEY);
  pendingLocalSchemes.value = [];
  showToast('已忽略本地方案');
}

// Publishing a server scheme makes the server create the relay objects the
// drawing describes and deploy them through the normal batches, exit first and
// entry last. The answer names each node's revision and status, which is what
// the panel shows; nothing is fabricated before the server answers.
const publishStatusNames: Record<string, string> = {
  pending: '待应用',
  waiting: '等待前置节点',
  dispatching: '下发中',
  dispatching_rollback: '回滚中',
  applying: '应用中',
  applied: '已生效',
  success: '已生效',
  failed: '失败',
  rolled_back: '已回滚'
};

function publishStatusLabel(status: string): string {
  return publishStatusNames[status] || status;
}

async function handleConfirmPublish() {
  openPublishModal.value = false;
  const scheme = currentScheme.value;

  if (!scheme.id) {
    showToast('方案尚未保存，无法发布', 'error');
    return;
  }
  const invalid = scheme.connections.find(connection => connection.invalidReason);
  if (invalid) {
    showToast(`发布前请先修复: ${invalid.invalidReason}`, 'error');
    return;
  }
  publishing.value = true;
  try {
    // Flush the pending edit first, so the server publishes what is on screen.
    // The save joins the same queue as the debounced saves: a save still in
    // flight would otherwise race this one with the same version and get a
    // false "modified elsewhere" conflict.
    const pending = saveTimers.get(scheme);
    if (pending) {
      clearTimeout(pending);
      saveTimers.delete(scheme);
    }
    const flushed = enqueueWrite(() => persistCanvasScheme(scheme, canvasContext()));
    saveQueue = flushed.catch(() => undefined);
    await flushed;
    const result = await canvasAPI.publish(scheme.id);
    publishResult.value = result;
    scheme.published = true;
    scheme.hasUnpublishedChanges = false;
    scheme.publishedAt = new Date().toISOString();
    showToast(scheme.executionLocation === 'client' ? '客户端方案已发布到用户订阅' : `已发布到 ${result.nodes.length} 台服务器${result.batchId ? '' : '（单节点修订）'}`, 'success');
    await reloadSchemes();
  } catch (error) {
    showToast(`发布失败: ${(error as Error).message}`, 'error');
  } finally {
    publishing.value = false;
  }
}

onMounted(async () => {
  // U5 reads the granted source metadata from the panel, not browser records.
  proxyNodeStore.manualNodes = [];
  proxyNodeStore.subscriptions = [];
  try {
    await loadImports();
    const [nodeRes, inboundRes] = await Promise.all([
      choices(page => nodeAPI.list(page, '')),
      choices((page, size) => proxyAPI.inbounds('', page, size))
    ]);
    servers.value = nodeRes;
    await Promise.all(nodeRes.map(node => loadNodeMeta(node.id, true)));
    inbounds.value = inboundRes;
  } catch (e) { showToast(`载入画布资源失败: ${(e as Error).message}`, 'error'); }

  // Schemes are stored by the server now; the browser keeps no copy of them.
  await reloadSchemes();
  readLocalSchemes();

  const qSchemeId = route.query.schemeId as string | undefined;
  const qFocusConnId = route.query.focusConnId as string | undefined;
  const qFocusInstId = route.query.focusInstId as string | undefined;

  if (qSchemeId && proxyNodeStore.schemes.some(s => s.id === qSchemeId)) {
    activeSchemeId.value = qSchemeId;
  }
  applyExecutionDefaults();

  window.addEventListener('click', handleWindowClick);
  setTimeout(() => {
    if (qFocusConnId) {
      const conn = currentScheme.value.connections.find(c => c.id === qFocusConnId);
      if (conn) {
        selectConnection(conn);
        const srcNode = currentScheme.value.nodes.find(n => n.instanceId === conn.sourceInstanceId);
        const tgtNode = currentScheme.value.nodes.find(n => n.instanceId === conn.targetInstanceId);
        if (srcNode && tgtNode) {
          const midX = (srcNode.x + tgtNode.x) / 2;
          const midY = (srcNode.y + tgtNode.y) / 2;
          panX.value = Math.round(350 - midX * zoom.value);
          panY.value = Math.round(250 - midY * zoom.value);
        }
        showToast('已定位线路: ' + getNodeNameByInst(conn.sourceInstanceId) + ' → ' + getNodeNameByInst(conn.targetInstanceId));
      }
    } else if (qFocusInstId) {
      const inst = currentScheme.value.nodes.find(n => n.instanceId === qFocusInstId);
      if (inst) {
        selectedNode.value = inst;
        selectedConnection.value = null;
        panX.value = Math.round(350 - inst.x * zoom.value);
        panY.value = Math.round(250 - inst.y * zoom.value);
        if (inst.role === 'exit') {
          showToast('已定位落地目标: ' + inst.name + '，可从左侧拖入中转服务器连线');
        } else {
          showToast('已聚焦节点: ' + inst.name);
        }
      }
    }
  }, 100);
});

onUnmounted(() => {
  for (const cleanup of [...gestureCleanups]) cleanup();
  void flushPendingSaves();
  window.removeEventListener('beforeunload', warnUnsaved);
  window.removeEventListener('click', handleWindowClick);
});
</script>
