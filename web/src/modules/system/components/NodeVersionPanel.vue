<template>
  <div class="space-y-5">
    <PageHeader
      v-if="!embedded"
      :title="kind === 'agent' ? 'Agent 探针管理' : '代理核心版本'"
      parent="服务器"
      :description="kind === 'agent' ? '统一管理与监控各服务器上部署的 Agent 探针运行状态、心跳上报与自动更新任务。' : '按服务器上报的核心查看版本与健康状态，支持添加、升级与移除内核；结果以节点回执与能力上报为准。'"
    >
      <template #actions>
        <button v-if="isOwner"
          type="button"
          @click="upgradeAll"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)] py-2 px-3.5"
          :title="kind === 'core' ? '全部升级在线服务器的代理核心' : '全部升级在线服务器的 Agent 到最新版本'"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          <span>全部升级</span>
        </button>
      </template>
    </PageHeader>

    <!-- Core Platform Highlights Banner (when kind === 'core') -->
    <div v-if="kind === 'core'" class="p-3.5 sm:p-4 rounded-2xl bg-gradient-to-r from-purple-50/70 via-indigo-50/50 to-blue-50/60 border border-purple-100/80 flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-xs shadow-2xs">
      <div class="flex items-start sm:items-center gap-3">
        <div class="w-8 h-8 rounded-xl bg-purple-600 text-white flex items-center justify-center shrink-0 shadow-sm">
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
        </div>
        <div>
          <div class="font-bold text-ios-label flex items-center gap-2">
            <span>支持的代理核心平台与生态</span>
            <span class="text-xs px-2 py-0.2 rounded-full bg-purple-100 text-purple-800 font-semibold">多核心协同共存</span>
          </div>
          <p class="text-xs text-ios-secondary mt-0.5 leading-relaxed">
            支持 <strong>Xray-core</strong>（成熟稳定 · VLESS / Trojan / Shadowsocks / WireGuard）与 <strong>sing-box</strong>（新一代轻量 · Hysteria2 / TUIC / 自适应路由）。一台服务器可同时运行多个已上报启用的核心，入站按所选核心归属；添加、升级与移除的结果以 Agent 回执和下一次能力上报为准。
          </p>
          <div v-if="isOwner" class="flex flex-wrap items-center gap-1.5 mt-1.5">
            <button v-for="item in CORE_ORDER" :key="item" type="button"
              class="text-xs px-2 py-0.5 rounded-full bg-white/80 border border-purple-200/70 text-purple-800 hover:bg-purple-50"
              @click="upgradeAllComponent(item)">全部升级 {{ item }}</button>
          </div>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          @click="showCoreCatalogModal = true"
          class="ios-btn-secondary !py-1.5 !px-3 inline-flex items-center gap-1.5 bg-white/80"
        >
          <svg class="w-3.5 h-3.5 text-purple-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span>查看核心特性对比</span>
        </button>
      </div>
    </div>

    <!-- Search, Filter, Tabs & View Mode Toolbar (Integrated Unified Row) -->
    <div class="flex flex-col xl:flex-row xl:items-center justify-between gap-3">
      <div class="flex flex-wrap items-center gap-2.5 flex-1">
        <!-- Segmented Tabs (Agent vs 代理核心) when embedded -->
        <div v-if="embedded" class="ios-segmented-container inline-flex shrink-0 mr-1" role="tablist" aria-label="程序类型">
          <button
            role="tab"
            :aria-selected="currentTab === 'agents'"
            class="ios-segmented-item whitespace-nowrap py-1.5 px-3.5"
            :class="{ active: currentTab === 'agents' }"
            @click="$emit('change-tab', 'agents')"
          >
            Agent
          </button>
          <button
            role="tab"
            :aria-selected="currentTab === 'cores'"
            class="ios-segmented-item whitespace-nowrap py-1.5 px-3.5"
            :class="{ active: currentTab === 'cores' }"
            @click="$emit('change-tab', 'cores')"
          >
            代理核心
          </button>
        </div>

        <!-- Search Form -->
        <form @submit.prevent="search" class="flex flex-wrap items-center gap-2 flex-1 min-w-[280px]">
          <!-- Search Input -->
          <div class="relative flex-1 min-w-[180px] max-w-sm">
            <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <input
              v-model="keyword"
              :placeholder="kind === 'agent' ? '搜索名称、IP、Agent 版本或系统…' : '搜索名称、IP、核心名称或版本…'"
              class="ios-search-input !py-1.5"
            />
            <button
              v-if="keyword"
              type="button"
              @click="keyword = ''; search()"
              class="absolute right-2.5 top-1/2 -translate-y-1/2 text-ios-tertiary hover:text-ios-label p-0.5 rounded-full hover:bg-black/5 transition-colors"
              title="清空"
            >
              <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
                <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
              </svg>
            </button>
          </div>

          <!-- Status Filter Dropdown -->
          <select v-model="statusFilter" @change="search" class="ios-filter-select !w-auto !py-1.5">
            <option value="all">全部状态</option>
            <option value="online">在线</option>
            <option value="degraded">核心异常</option>
            <option value="applying">发布中</option>
            <option value="rollback">回滚中</option>
            <option value="error">异常</option>
            <option value="offline">离线</option>
            <option value="pending">等待登记</option>
            <option value="disabled">已停用</option>
          </select>

          <!-- Query Button -->
          <button class="ios-search-btn shrink-0 !py-1.5">查询</button>

          <!-- Refresh Button -->
          <button
            type="button"
            @click="load()"
            class="ios-search-btn shrink-0 !py-1.5"
            :disabled="loading"
            title="刷新数据"
          >
            <svg class="w-3.5 h-3.5 text-ios-secondary" :class="{ 'animate-spin': loading }" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
            </svg>
            <span>刷新</span>
          </button>
        </form>
      </div>

      <!-- Right: View Mode Toggle (Unified ServerDisplayMode Component) -->
      <div class="flex items-center gap-2 shrink-0 self-start xl:self-auto">
        <ServerDisplayMode :display-mode="displayMode" @change="setDisplayMode" />
      </div>
    </div>

    <!-- State: Error, Loading, Empty -->
    <p v-if="error && nodes.length" role="status" class="text-sm text-ios-red">{{ error }}</p>
    <ErrorState v-if="error && !nodes.length" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <EmptyState
      v-else-if="!filteredNodes.length"
      title="暂无匹配服务器"
      description="未找到符合条件的服务器节点记录，请调整筛选或稍后刷新。"
    />

    <!-- ================= Mode 1: Table List View ================= -->
    <DataTable v-else-if="displayMode === 'list'">
      <template #header>
        <th>服务器</th>
        <th>运行状态</th>
        <th>操作系统 / 架构</th>
        <template v-if="kind === 'agent'">
          <th>Agent 探针版本</th>
          <th>协同代理核心</th>
          <th>核心健康状况</th>
        </template>
        <template v-else>
          <th>已安装核心 / 版本</th>
          <th>健康运行状态</th>
          <th>支持网络协议 (全核心)</th>
        </template>
        <th>最近心跳报告</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="node in filteredNodes" :key="node.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. 服务器名称与地区/IP -->
          <td class="p-4">
            <div class="flex items-center gap-2.5">
              <div
                class="w-7 h-7 rounded-lg bg-blue-50/90 border border-blue-100 text-xs font-bold font-mono text-ios-blue flex items-center justify-center shrink-0 shadow-2xs select-none"
                :title="countryTitle(getCountryInfo(node))"
              >
                {{ getCountryInfo(node).flag }}
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <router-link :to="`/nodes/${node.id}`" class="font-semibold text-ios-blue hover:underline text-base truncate">
                    {{ node.name }}
                  </router-link>
                  <span v-if="node.deletedAt" class="px-1.5 py-0.2 rounded text-xs font-bold tracking-wide uppercase shrink-0 bg-black/[0.05] text-neutral-400">
                    已删除
                  </span>
                </div>
                <p class="text-sm text-ios-secondary tabular-nums truncate mt-0.5">
                  {{ connectAddress(node) || '未设置连接地址' }}
                </p>
              </div>
            </div>
          </td>

          <!-- 2. 状态胶囊 StatusBadge -->
          <td>
            <StatusBadge
              :status="getNodeStatusTone(node)"
              :label="statusCapsuleText(node)"
            />
          </td>

          <!-- 3. 系统架构 -->
          <td class="text-xs text-ios-secondary">
            <span class="px-2 py-0.5 rounded-md bg-neutral-100 text-neutral-700 font-medium">
              {{ formatOS(node.os) }}
            </span>
          </td>

          <!-- 4. Agent 模式专有列 -->
          <template v-if="kind === 'agent'">
            <td>
              <span
                class="tabular-nums text-xs px-2 py-0.5 rounded-md"
                :class="node.agentVersion ? 'font-bold bg-blue-50 text-ios-blue' : 'text-neutral-400 bg-neutral-100 font-normal'"
              >
                {{ node.agentVersion || '等待注册' }}
              </span>
            </td>
            <td class="text-xs text-ios-secondary tabular-nums">
              {{ coreVersion(node) }}
            </td>
            <td>
              <span
                class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold"
                :class="coreHealthTone(node) === 'bad' ? 'bg-red-50 text-red-600' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500 font-normal')"
                :title="getNodeInstalledCores(node).map(c => `${c.name} ${c.version}: ${c.healthy === false ? '异常' : c.healthy === true ? '正常' : '—'}`).join('；')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="coreHealthTone(node) === 'bad' ? 'bg-red-500' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                <span>{{ coreHealth(node) }}</span>
              </span>
            </td>
          </template>

          <!-- 5. 核心模式专有列 (支持多核心展示) -->
          <template v-else>
            <td>
              <div v-if="getNodeInstalledCores(node).length" class="flex flex-wrap gap-1">
                <span
                  v-for="core in getNodeInstalledCores(node)"
                  :key="core.component"
                  class="text-xs px-2 py-0.5 rounded-md tabular-nums font-bold"
                  :class="core.component === 'xray' ? 'bg-blue-50 text-ios-blue' : 'bg-purple-50 text-purple-700'"
                  :title="core.error || (core.primary ? '主核心' : '')"
                >
                  {{ core.name }} {{ core.version }}
                  <span v-if="core.primary" class="ml-1 text-xs font-semibold opacity-70">主</span>
                </span>
                <span
                  v-if="corePendingLabel(node, CORE_ORDER[0]) || corePendingLabel(node, CORE_ORDER[1])"
                  class="text-xs px-1.5 py-0.5 rounded-md bg-ios-blue/10 text-ios-blue font-semibold"
                >{{ corePendingLabel(node, CORE_ORDER[0]) || corePendingLabel(node, CORE_ORDER[1]) }}</span>
              </div>
              <span v-else class="text-xs px-2 py-0.5 rounded-md text-amber-700 bg-amber-50 font-normal">
                未安装内核
              </span>
            </td>
            <td>
              <span
                class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold"
                :class="coreHealthTone(node) === 'bad' ? 'bg-red-50 text-red-600' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500 font-normal')"
                :title="getNodeInstalledCores(node).map(c => `${c.name} ${c.version}: ${c.healthy === false ? '异常' : c.healthy === true ? '正常' : '—'}`).join('；')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="coreHealthTone(node) === 'bad' ? 'bg-red-500' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                <span>{{ coreHealth(node) }}</span>
              </span>
            </td>
            <td>
              <div v-if="getNodeAllProtocols(node).length" class="flex flex-wrap gap-1">
                <span
                  v-for="proto in getNodeAllProtocols(node)"
                  :key="proto"
                  class="rounded bg-black/[0.04] px-1.5 py-0.2 text-xs font-bold uppercase text-neutral-700"
                >
                  {{ proto }}
                </span>
              </div>
              <span v-else class="text-xs text-ios-tertiary">尚未上报</span>
            </td>
          </template>

          <!-- 6. 心跳时间 -->
          <td class="text-xs tabular-nums text-ios-tertiary">
            {{ formatTime(node.lastHeartbeat) }}
          </td>

          <!-- 7. 操作 -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <template v-if="kind === 'agent'">
              <template v-if="node.agentUpdateSupported">
                <button v-if="isOwner && !upgradeUnavailableReason(node)"
                  type="button"
                  @click="handleUpgradeSingle(node)"
                  class="ios-action-btn-secondary"
                  title="升级 Agent 到最新版本"
                >
                  升级
                </button>
                <span v-else-if="isOwner" class="text-xs text-neutral-400">{{ upgradeUnavailableReason(node) }}</span>
              </template>
              <span v-else class="text-xs text-neutral-400">
                需在服务器上运行一次 qui-update --component agent 启用网页升级
              </span>
            </template>
            <template v-else>
              <!-- Core Actions in Table Mode -->
              <button
                v-if="isOwner && (!getNodeInstalledCores(node).length)"
                type="button"
                @click="openCoreManagementModal(node)"
                class="ios-action-btn-primary"
                title="为该服务器添加代理内核"
              >
                添加内核
              </button>
              <template v-else>
                <button
                  v-if="isOwner && !upgradeUnavailableReason(node)"
                  type="button"
                  @click="handleUpgradeSingle(node)"
                  class="ios-action-btn-secondary"
                  title="升级代理内核版本"
                >
                  升级
                </button>
                <span v-else-if="isOwner" class="text-xs text-neutral-400">{{ upgradeUnavailableReason(node) }}</span>
                <button v-if="isOwner"
                  type="button"
                  @click="openCoreManagementModal(node)"
                  class="ios-action-btn-secondary"
                  title="管理当前核心"
                >
                  编辑
                </button>
              </template>
            </template>
            <router-link :to="`/nodes/${node.id}`" class="ios-action-btn-primary">
              详情
            </router-link>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- ================= Mode 2: Grid Card View (Balanced Layout) ================= -->
    <div
      v-else-if="displayMode === 'grid'"
      class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in filteredNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <div class="space-y-3.5">
          <!-- Row 1: Flag · Name · OS & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex flex-wrap items-center gap-x-2.5 gap-y-1 min-w-0">
              <div
                class="w-7 h-7 rounded-lg bg-blue-50/90 border border-blue-100 text-xs font-bold font-mono text-ios-blue flex items-center justify-center shrink-0 shadow-2xs select-none"
                :title="countryTitle(getCountryInfo(node))"
              >
                {{ getCountryInfo(node).flag }}
              </div>
              <router-link
                :to="`/nodes/${node.id}`"
                class="min-w-0 max-w-full font-bold text-lg text-neutral-900 truncate tracking-normal hover:text-ios-blue transition-colors"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span
                class="rounded bg-black/[0.04] px-1.5 py-0.5 text-xs font-semibold uppercase text-neutral-600 shrink-0 border border-black/[0.04] truncate max-w-[90px]"
                :title="node.os || 'Linux'"
              >
                {{ formatOS(node.os) }}
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

          <!-- Row 2: Sub-info (IP & Last Heartbeat) -->
          <div class="text-xs font-mono tabular-nums text-neutral-400 tracking-normal flex flex-wrap gap-x-2 gap-y-1 items-center justify-between pt-0.5">
            <span class="flex items-center gap-1.5 text-neutral-600 truncate tabular-nums" :title="connectAddress(node)">
              <svg class="w-3.5 h-3.5 text-neutral-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9" />
              </svg>
              <span class="truncate" :class="connectAddress(node) ? '' : 'text-ios-orange font-sans'">{{ connectAddress(node) || '未设置连接地址' }}</span>
            </span>
            <span class="shrink-0 text-neutral-400 flex items-center gap-1 text-xs tabular-nums" :title="'最近心跳: ' + formatTime(node.lastHeartbeat)">
              <svg class="w-3 h-3 text-neutral-300 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
              <span>{{ formatTime(node.lastHeartbeat) }}</span>
            </span>
          </div>

          <!-- Row 3: Information Box (Grey rounded panel) -->
          <div class="bg-[#F8F9FA] rounded-2xl p-3.5 border border-black/[0.03] space-y-2.5">
            <!-- AGENT MODE -->
            <template v-if="kind === 'agent'">
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">Agent 探针版本</span>
                <span
                  class="tabular-nums px-2 py-0.5 rounded-lg text-xs"
                  :class="node.agentVersion ? 'font-mono font-semibold text-ios-blue bg-blue-50/90 border border-blue-100/70' : 'font-normal text-neutral-400 bg-neutral-100'"
                >
                  {{ node.agentVersion || '等待上报' }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">协同代理核心</span>
                <span
                  class="tabular-nums text-xs truncate max-w-[150px] px-2 py-0.5 rounded-lg"
                  :class="node.capabilities?.core_name ? 'font-mono font-semibold text-purple-700 bg-purple-50/90 border border-purple-100/70' : 'text-neutral-400 font-normal bg-neutral-100'"
                  :title="coreVersion(node)"
                >
                  {{ coreVersion(node) }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">核心健康状况</span>
                <span
                  class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold"
                  :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600 border border-red-100/70' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600 border border-emerald-100/70' : 'bg-neutral-100 text-neutral-400 font-normal')"
                >
                  <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                  <span>{{ coreHealth(node) }}</span>
                </span>
              </div>
            </template>

            <!-- CORE MODE: Multi-core Compatible Display -->
            <template v-else>
              <!-- Installed Core State (Supports Multiple Cores Coexisting) -->
              <template v-if="getNodeInstalledCores(node).length">
                <div class="flex items-start justify-between text-xs gap-2">
                  <span class="text-neutral-500 font-medium shrink-0 pt-0.5">核心与版本</span>
                  <div class="flex flex-wrap gap-1 justify-end">
                    <span
                      v-for="c in getNodeInstalledCores(node)"
                      :key="c.component"
                      class="font-mono tabular-nums px-2 py-0.5 rounded-lg text-xs font-semibold"
                      :class="c.component === 'xray' ? 'text-ios-blue bg-blue-50/90 border border-blue-200/70' : 'text-purple-700 bg-purple-50/90 border border-purple-200/70'"
                      :title="c.error || ''"
                    >
                      {{ c.name }} {{ c.version }}<span v-if="c.primary" class="ml-1 text-xs font-semibold opacity-70">主</span>
                    </span>
                    <span
                      v-if="corePendingLabel(node, CORE_ORDER[0]) || corePendingLabel(node, CORE_ORDER[1])"
                      class="tabular-nums px-2 py-0.5 rounded-lg text-xs font-semibold bg-ios-blue/10 text-ios-blue"
                    >{{ corePendingLabel(node, CORE_ORDER[0]) || corePendingLabel(node, CORE_ORDER[1]) }}</span>
                  </div>
                </div>

                <div class="flex items-center justify-between text-xs">
                  <span class="text-neutral-500 font-medium">健康运行状态</span>
                  <span
                    class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold"
                    :class="coreHealthTone(node) === 'bad' ? 'bg-red-50 text-red-600 border border-red-100/70' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-50 text-emerald-600 border border-emerald-100/70' : 'bg-neutral-100 text-neutral-400 font-normal')"
                    :title="getNodeInstalledCores(node).map(c => `${c.name} ${c.version}: ${c.healthy === false ? '异常' : c.healthy === true ? '正常' : '—'}`).join('；')"
                  >
                    <span class="w-1.5 h-1.5 rounded-full" :class="coreHealthTone(node) === 'bad' ? 'bg-red-500' : (coreHealthTone(node) === 'ok' ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                    <span>{{ coreHealth(node) }}</span>
                  </span>
                </div>

                <div class="space-y-1 pt-0.5">
                  <div class="text-xs text-neutral-400 font-medium">支持网络协议:</div>
                  <div v-if="getNodeAllProtocols(node).length" class="flex flex-wrap gap-1">
                    <span
                      v-for="proto in getNodeAllProtocols(node)"
                      :key="proto"
                      class="rounded bg-black/[0.04] px-1.5 py-0.2 text-xs font-bold uppercase text-neutral-700"
                    >
                      {{ proto }}
                    </span>
                  </div>
                  <div v-else class="text-xs text-neutral-400 italic">尚未上报协议</div>
                </div>
              </template>

              <!-- Uninstalled / Pending Core State -->
              <template v-else>
                <div class="flex items-center justify-between text-xs">
                  <span class="text-neutral-500 font-medium">核心状态</span>
                  <span class="px-2 py-0.5 rounded-lg text-xs font-medium text-amber-700 bg-amber-50 border border-amber-200/60">
                    尚未上报核心
                  </span>
                </div>
                <div class="p-2 rounded-xl bg-amber-50/50 border border-amber-100 text-amber-900 text-xs leading-relaxed">
                  当前服务器尚未部署代理内核。支持一键安装 <strong>Xray-core</strong> 与 <strong>sing-box</strong> 核心协同运行以开启转发。
                </div>
              </template>
            </template>
          </div>
        </div>

        <!-- Row 4: Action Buttons (Apple HIG Precision) -->
        <template v-if="kind === 'agent'">
          <div class="pt-2 border-t border-black/[0.04] grid grid-cols-2 gap-2.5">
            <template v-if="node.agentUpdateSupported">
              <button v-if="isOwner && !upgradeUnavailableReason(node)"
                type="button"
                @click="handleUpgradeSingle(node)"
                class="py-2 px-3 rounded-xl bg-ios-blue/[0.08] text-ios-blue hover:bg-ios-blue/[0.15] active:scale-[0.98] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1.5"
                title="升级 Agent 到最新版本"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                </svg>
                <span>升级</span>
              </button>
              <div v-else-if="isOwner" class="text-xs text-neutral-400 flex items-center justify-center text-center">{{ upgradeUnavailableReason(node) }}</div>
            </template>
            <div v-else class="text-xs text-neutral-400 flex items-center justify-center text-center">
              需在服务器上运行一次 qui-update --component agent 启用网页升级
            </div>

            <router-link
              :to="`/nodes/${node.id}`"
              class="py-2 px-3 rounded-xl bg-black/[0.04] text-neutral-700 hover:bg-black/[0.08] active:scale-[0.98] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1"
            >
              <span>详情</span>
              <svg class="w-3 h-3 text-neutral-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7" />
              </svg>
            </router-link>
          </div>
        </template>

        <!-- CORE MODE ACTIONS: [升级] + [编辑] + [详情] -->
        <template v-else>
          <!-- When uninstalled: prominent [+ 添加内核] & [详情] -->
          <div v-if="!getNodeInstalledCores(node).length" class="pt-2 border-t border-black/[0.04] flex items-center gap-2">
            <button v-if="isOwner"
              type="button"
              @click="openCoreManagementModal(node)"
              class="flex-1 py-2 px-3 rounded-xl bg-ios-blue text-white hover:bg-ios-blue/90 shadow-[0_2px_8px_rgba(0,113,227,0.25)] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1.5"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
              </svg>
              <span>添加内核</span>
            </button>

            <router-link
              :to="`/nodes/${node.id}`"
              class="py-2 px-3 rounded-xl bg-black/[0.04] text-neutral-700 hover:bg-black/[0.08] font-semibold text-xs transition-all flex items-center justify-center gap-1"
            >
              <span>详情</span>
            </router-link>
          </div>

          <!-- When installed: [升级] (flex-1) + [编辑] (Click opens modal for Add/Delete cores) + [详情] -->
          <div v-else class="pt-2 border-t border-black/[0.04] flex items-center gap-2">
            <button
              v-if="isOwner && !upgradeUnavailableReason(node)"
              type="button"
              @click="handleUpgradeSingle(node)"
              class="flex-1 py-2 px-3 rounded-xl bg-ios-blue/[0.08] text-ios-blue hover:bg-ios-blue/[0.15] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1.5"
              title="升级代理内核版本"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
              </svg>
              <span>升级</span>
            </button>
            <span v-else-if="isOwner" class="flex-1 text-xs text-neutral-400 text-center">{{ upgradeUnavailableReason(node) }}</span>

            <!-- Unified [编辑] Button: opens Add & Delete Core Modal -->
            <button v-if="isOwner"
              type="button"
              @click="openCoreManagementModal(node)"
              class="py-2 px-3.5 rounded-xl bg-black/[0.04] text-neutral-700 hover:bg-black/[0.08] active:scale-[0.98] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1"
              title="管理当前核心"
            >
              <svg class="w-3.5 h-3.5 text-neutral-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
              </svg>
              <span>编辑</span>
            </button>

            <router-link
              :to="`/nodes/${node.id}`"
              class="py-2 px-3 rounded-xl bg-black/[0.04] text-neutral-700 hover:bg-black/[0.08] font-semibold text-xs transition-all flex items-center justify-center gap-1"
              title="进入服务器详情"
            >
              <span>详情</span>
              <svg class="w-3 h-3 text-neutral-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M9 5l7 7-7 7" />
              </svg>
            </router-link>
          </div>
        </template>
      </div>
    </div>

    <!-- Dialog: Agent Upgrade Dialog -->
    <AppDialog
      v-model="agentUpgradeDialogVisible"
      :title="agentUpgradeTargetNode ? `升级 Agent - ${agentUpgradeTargetNode.name}` : '全部升级 Agent'"
      :description="agentUpgradeTargetNode ? '选择官方签名版本进行升级。如果版本尚未导入面板，将自动下载并导入本地签名目录。' : '为全部支持的在线服务器批量升级到选定的 Agent 版本。'"
      max-width="lg"
      :show-confirm="false"
      cancel-text="关闭"
    >
      <div class="space-y-3">
        <div v-if="agentUpgradeLoading" class="py-8 text-center text-sm text-neutral-400 flex flex-col items-center justify-center gap-2">
          <svg class="w-5 h-5 text-ios-blue animate-spin" fill="none" viewBox="0 0 24 24">
            <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
            <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"></path>
          </svg>
          <span>正在获取官方签名版本…</span>
        </div>
        <div v-else-if="!agentReleaseList.length" class="py-8 text-center text-sm text-neutral-400">
          暂未发现可用的官方 Agent 发布版本
        </div>
        <div v-else class="space-y-2.5 max-h-[60vh] overflow-y-auto pr-1">
          <div
            v-for="item in agentReleaseList"
            :key="item.version"
            class="flex items-center justify-between p-3.5 rounded-2xl border border-black/[0.06] bg-neutral-50/50 hover:bg-neutral-50 transition-colors gap-3"
          >
            <div class="space-y-1 min-w-0">
              <div class="flex items-center gap-2">
                <span class="font-bold text-sm text-neutral-900">v{{ item.version }}</span>
                <span
                  class="px-1.5 py-0.5 rounded text-2xs font-semibold uppercase tracking-wider"
                  :class="item.imported ? 'bg-emerald-50 text-emerald-700 border border-emerald-200' : 'bg-neutral-100 text-neutral-600'"
                >
                  {{ item.imported ? '已导入' : '未导入' }}
                </span>
              </div>
              <div class="text-xs text-neutral-500 flex items-center gap-2 flex-wrap">
                <span>支持架构: {{ item.architectures.join(', ') || 'linux/amd64, linux/arm64' }}</span>
                <span v-if="item.publishedAt">· {{ formatTime(item.publishedAt) }}</span>
              </div>
            </div>
            <button
              type="button"
              :disabled="agentUpgradeBusy"
              @click="selectAgentVersionForUpgrade(item)"
              class="shrink-0 py-1.5 px-3 rounded-xl text-xs font-semibold transition-all active:scale-[0.98] disabled:opacity-50"
              :class="item.imported ? 'bg-ios-blue text-white hover:bg-blue-600' : 'bg-neutral-900 text-white hover:bg-black'"
            >
              {{ item.imported ? (agentUpgradeTargetNode ? '升级' : '全部升级') : (agentUpgradeTargetNode ? '导入并升级' : '导入并全部升级') }}
            </button>
          </div>
        </div>
      </div>
    </AppDialog>
    <AppDialog :model-value="!!manualAgentNode" @update:model-value="v => { if (!v) manualAgentNode = null; }" title="手动选择 Agent 发布平台" description="节点架构未知或无完全匹配的版本，请核对目标主机后明确选择。" @confirm="prepareManualAgent">
      <select v-model="manualReleaseId" class="form-input-base"><option value="">选择已签名版本与平台</option><option v-for="r in agentReleases.filter(r => r.signed.manifest.component === 'proxy-agent')" :key="r.id" :value="r.id">{{ r.signed.manifest.version }} · {{ r.signed.manifest.os }}/{{ r.signed.manifest.architecture }}</option></select>
    </AppDialog>
    <!-- Dialog 1: Single Agent Upgrade Confirmation -->
    <ConfirmDialog
      v-model="singleAgentConfirmVisible"
      title="升级 Agent"
      :message="singleAgentConfirmMessage"
      warning-details="系统将下发经过签名验证的最新版本，由 Agent 校验、切换并上报结果。原有节点身份与已用额度保持不变。"
      confirm-text="确认升级"
      :is-danger="false"
      @confirm="executeSingleAgentUpgrade"
    />

    <!-- Dialog 2: Batch Agent Upgrade Confirmation -->
    <ConfirmDialog
      v-model="batchAgentConfirmVisible"
      title="全部升级 Agent"
      :message="batchAgentConfirmMessage"
      warning-details="系统将依次为全部符合条件的在线服务器下发最新 Agent 升级任务。离线服务器需等待重新上线。"
      confirm-text="开始全部升级"
      :is-danger="false"
      @confirm="executeBatchAgentUpgrade"
    />

    <!-- Baseline before upgrading a server that never published a configuration -->
    <ConfirmDialog
      v-model="baselineConfirmVisible"
      title="先发布基础配置"
      :message="baselineConfirmMessage"
      warning-details="将先发布一份不含任何入站的基础配置（代理核心不会启动、不影响任何用户），节点确认后自动继续升级。"
      confirm-text="发布并继续"
      :is-danger="false"
      @confirm="runBaselineThenContinue"
    />

    <!-- ============================================================ -->
    <!-- Dialog 3: 内核管理弹窗 (Core Management Modal: 添加内核 & 删除内核) -->
    <!-- ============================================================ -->
    <AppDialog
      v-model="coreManagementModalVisible"
      :title="`内核管理 - ${coreManagementTargetNode?.name || ''}`"
      description="按 Agent 上报的启用核心管理：升级、添加或移除内核。移除内核是让 Agent 禁用安装器持有的文件，重新安装会再次启用。"
      max-width="2xl"
      confirm-text="完成"
      :show-cancel="false"
      @confirm="coreManagementModalVisible = false"
    >
      <div class="space-y-5 text-xs">
        <!-- Target Server Hero Banner -->
        <div v-if="coreManagementTargetNode" class="p-3.5 bg-neutral-50 rounded-2xl border border-black/[0.04] flex items-center justify-between gap-3">
          <div class="flex items-center gap-2.5">
            <div class="w-8 h-8 rounded-xl bg-blue-50 text-ios-blue font-bold flex items-center justify-center text-sm shrink-0">
              {{ getCountryInfo(coreManagementTargetNode).flag }}
            </div>
            <div>
              <div class="font-bold text-sm text-neutral-900 flex items-center gap-2">
                <span>{{ coreManagementTargetNode.name }}</span>
                <span class="text-xs px-2 py-0.5 rounded-md bg-neutral-100 text-neutral-600 font-medium">
                  {{ formatOS(coreManagementTargetNode.os) }} ({{ detectArch(coreManagementTargetNode) || '—' }})
                </span>
              </div>
              <p class="text-xs text-neutral-500 tabular-nums mt-0.5">
                {{ connectAddress(coreManagementTargetNode) || '未设置连接地址' }}
              </p>
            </div>
          </div>
          <div class="text-right shrink-0">
            <span
              class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-semibold"
              :class="manageInstalledCores.length ? 'bg-purple-50 text-purple-700 border border-purple-200/60' : 'bg-amber-50 text-amber-700 border border-amber-200/60'"
            >
              <span class="w-1.5 h-1.5 rounded-full" :class="manageInstalledCores.length ? 'bg-purple-600' : 'bg-amber-500'"></span>
              <span>{{ manageInstalledCores.length ? `已部署 ${manageInstalledCores.length} 款内核` : '未部署内核' }}</span>
            </span>
          </div>
        </div>

        <!-- SECTION 1: 已安装内核列表 (支持删除内核与升级) -->
        <div class="space-y-2.5">
          <div class="flex items-center justify-between">
            <h4 class="text-neutral-900 flex items-center gap-2 text-base font-semibold leading-normal">
              <svg class="w-4 h-4 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
              </svg>
              <span>已安装内核</span>
              <span class="text-xs font-normal text-neutral-400">({{ manageInstalledCores.length }})</span>
            </h4>
            <span class="text-xs text-neutral-400">{{ coreFactsLoading ? '正在读取入站与任务…' : '版本、健康与任务状态均来自后端上报' }}</span>
          </div>

          <!-- Empty State when no cores installed -->
          <div v-if="!manageInstalledCores.length" class="p-5 rounded-2xl bg-neutral-50/80 border border-dashed border-neutral-200 text-center space-y-1">
            <div class="text-neutral-500 font-medium text-xs">当前服务器尚未安装任何代理内核</div>
            <p class="text-xs text-neutral-400">请在下方「添加内核」区域选择需要的代理核心进行部署。</p>
          </div>

          <!-- Installed Core Cards -->
          <div v-else class="space-y-2.5">
            <div
              v-for="core in manageInstalledCores"
              :key="core.component"
              class="p-4 rounded-2xl bg-white border border-neutral-200/90 shadow-2xs hover:border-neutral-300 transition-all flex flex-col sm:flex-row sm:items-center justify-between gap-3.5"
            >
              <div class="flex items-start gap-3">
                <div
                  class="w-9 h-9 rounded-xl flex items-center justify-center font-bold text-sm shrink-0 shadow-2xs"
                  :class="core.component === 'xray' ? 'bg-blue-50 text-ios-blue border border-blue-100' : 'bg-purple-50 text-purple-700 border border-purple-100'"
                >
                  {{ core.component === 'xray' ? 'X' : 'S' }}
                </div>
                <div class="min-w-0 space-y-1">
                  <div class="flex flex-wrap items-center gap-2">
                    <span class="font-bold text-sm text-neutral-900">{{ core.name }}</span>
                    <span class="tabular-nums px-2 py-0.2 rounded-md bg-neutral-100 text-neutral-700 font-bold text-xs">
                      {{ core.version }}
                    </span>
                    <span v-if="core.primary" class="text-xs px-1.5 py-0.2 rounded bg-blue-50 text-ios-blue font-semibold">主核心</span>
                    <span
                      class="inline-flex items-center gap-1 px-2 py-0.2 rounded-full text-xs font-semibold"
                      :class="core.healthy === true ? 'bg-ios-green/10 text-ios-green' : core.healthy === false ? 'bg-ios-red/10 text-ios-red' : 'bg-ios-fill text-ios-tertiary'"
                    >
                      <span class="w-1.5 h-1.5 rounded-full" :class="core.healthy === true ? 'bg-ios-green' : core.healthy === false ? 'bg-ios-red' : 'bg-ios-tertiary'"></span>
                      <span>{{ core.healthy === null ? '—' : core.healthy ? '运行正常' : '异常' }}</span>
                    </span>
                    <span v-if="manageCorePendingLabel(core.component)" class="text-xs px-1.5 py-0.2 rounded bg-ios-blue/10 text-ios-blue font-semibold">
                      {{ manageCorePendingLabel(core.component) }}
                    </span>
                  </div>
                  <p v-if="core.error" class="text-xs text-ios-red">{{ core.error }}</p>
                  <div class="flex flex-wrap items-center gap-1 pt-0.5">
                    <span class="text-xs text-neutral-400 mr-0.5">协议:</span>
                    <span
                      v-for="p in core.protocols"
                      :key="p"
                      class="px-1.5 py-0.2 rounded bg-black/[0.04] text-neutral-700 font-bold text-xs uppercase"
                    >
                      {{ p }}
                    </span>
                    <span v-if="!core.protocols.length" class="text-xs text-neutral-400">—</span>
                  </div>
                  <p class="text-xs text-neutral-500">
                    入站：<template v-if="coreFactsLoading">读取中…</template>
                    <template v-else-if="manageCoreInbounds(core.component).length">
                      <router-link v-for="item in manageCoreInbounds(core.component)" :key="item.id"
                        :to="inboundLink(coreManagementTargetNode?.id || '')"
                        class="text-ios-blue hover:underline mr-1.5">{{ item.name }}</router-link>
                    </template>
                    <template v-else>无（可安全移除）</template>
                  </p>
                  <p v-if="manageCoreTask(core.component)" class="text-xs text-neutral-500 flex flex-wrap items-center gap-1.5">
                    <span>最近任务</span>
                    <span class="px-1.5 py-0.2 rounded font-semibold" :class="coreTaskTone(manageCoreTask(core.component)?.status || '')">
                      {{ coreTaskStatusText(manageCoreTask(core.component)?.status || '') }}
                    </span>
                    <span class="tabular-nums">{{ manageCoreTask(core.component)?.version }}</span>
                    <span v-if="manageCoreTask(core.component)?.result?.code" class="font-mono text-xs text-neutral-400">
                      {{ manageCoreTask(core.component)?.result?.code }}
                    </span>
                  </p>
                </div>
              </div>

              <!-- Actions for this specific core: [升级] & [删除内核] -->
              <div class="flex items-center gap-2 shrink-0 self-end sm:self-center">
                <button
                  v-if="coreManagementTargetNode && !upgradeUnavailableReason(coreManagementTargetNode)"
                  type="button"
                  @click="withBaseline([coreManagementTargetNode], () => triggerCoreUpgradeFromModal(core))"
                  class="py-1.5 px-3 rounded-xl bg-ios-blue/[0.08] text-ios-blue hover:bg-ios-blue/[0.15] font-semibold text-xs transition-all flex items-center gap-1"
                  title="升级该内核版本"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                  </svg>
                  <span>升级</span>
                </button>

                <!-- Delete Kernel Action Button: hidden while an add/remove task is
                     unsettled or when this is the only enabled core. -->
                <button
                  v-if="isOwner && manageInstalledCores.length > 1 && !manageCorePendingLabel(core.component) && multiCoreSupported(coreManagementTargetNode)"
                  type="button"
                  title="删除内核"
                  @click="requestRemoveCore(core)"
                  class="py-1.5 px-3 rounded-xl bg-ios-red/[0.08] text-ios-red hover:bg-ios-red/[0.15] font-semibold text-xs transition-all"
                >
                  删除内核
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- 移除被拒绝：后端仍报告这些入站使用该内核 -->
        <div v-if="removeConflictInbounds.length" class="p-3 rounded-2xl bg-ios-red/[0.06] border border-ios-red/20 text-ios-red space-y-1.5">
          <div class="font-bold text-xs">后端拒绝了移除请求（core_in_use）</div>
          <p class="text-xs leading-relaxed">下列入站仍在当前草稿或已发布配置中使用该内核（含已禁用的入站）。请先删除或改派它们，再重新提交移除。</p>
          <div class="flex flex-wrap gap-1.5">
            <router-link v-for="item in removeConflictInbounds" :key="item.id" :to="inboundLink(coreManagementTargetNode?.id || '')"
              class="text-xs px-2 py-0.5 rounded-lg bg-white border border-ios-red/20 hover:underline">
              {{ item.name }}
            </router-link>
          </div>
        </div>

        <!-- SECTION 2: 添加内核 (需要 Agent 协商多核心支持) -->
        <div class="pt-4 border-t border-black/[0.06] space-y-3.5">
          <div class="flex items-center justify-between">
            <h4 class="text-neutral-900 flex items-center gap-2 text-base font-semibold leading-normal">
              <svg class="w-4 h-4 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              <span>添加内核</span>
            </h4>
            <span
              v-if="coreManagementTargetNode?.status !== 'pending'"
              class="text-xs px-2 py-0.5 rounded-full font-medium"
              :class="multiCoreSupported(coreManagementTargetNode) ? 'text-emerald-700 bg-emerald-50' : 'text-ios-orange bg-ios-orange/10'"
            >
              {{ multiCoreSupported(coreManagementTargetNode) ? 'Agent 已协商多核心' : 'Agent 未协商多核心' }}
            </span>
          </div>
          <p v-if="coreManagementTargetNode?.status !== 'pending' && !multiCoreSupported(coreManagementTargetNode)" class="p-3 rounded-xl bg-ios-orange/10 border border-ios-orange/20 text-xs text-ios-orange leading-relaxed">
            该服务器上报的 Agent 未协商多核心支持（旧版 Agent），只能升级当前核心。请先在「版本与更新 › Agent」升级 Agent，重连后再添加第二个内核。
          </p>

          <!-- Pending Node Guidance if node is pending -->
          <div v-if="coreManagementTargetNode?.status === 'pending'" class="space-y-3">
            <div class="p-3 rounded-xl bg-amber-50 border border-amber-200/80 text-amber-900 space-y-1">
              <div class="font-bold flex items-center gap-1.5">
                <svg class="w-4 h-4 text-amber-700" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>服务器尚未完成 Agent 注册</span>
              </div>
              <p class="text-xs leading-relaxed">
                在终端运行一键部署命令安装 Agent 与所选内核；注册完成后可在「内核管理」中添加其余内核。
              </p>
            </div>

            <!-- Core selection, shared with the other install dialogs -->
            <div class="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-xl bg-white px-3 py-2 text-xs">
              <label v-for="option in CORE_INSTALL_OPTIONS" :key="option.name" class="inline-flex items-center gap-1.5 text-ios-label cursor-pointer">
                <input v-model="pendingCores" type="checkbox" :value="option.name" class="accent-ios-blue" />
                <span>{{ option.label }}</span>
                <span v-if="option.hint" class="text-xs text-ios-tertiary">（{{ option.hint }}）</span>
              </label>
            </div>
            <p v-if="pendingCoreError" role="alert" class="text-xs text-ios-red">{{ pendingCoreError }}</p>

            <div class="space-y-1.5">
              <WebUpgradeOption v-model="pendingWebUpgrade" />
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-700 font-bold">一键安装命令</span>
              </div>
              <div v-if="pendingEnrollmentLoading" class="p-4 text-center text-ios-tertiary">正在生成注册命令…</div>
              <template v-else-if="pendingEnrollmentCommand">
                <p v-if="installEndpointFromSettings" class="text-xs text-ios-tertiary">安装连接地址默认取自 系统设置 → 节点安装连接地址；未填写的项使用浏览器地址或面板实际端口。</p>
                <CodePreview :code="pendingEnrollmentCommand" title="一键安装命令" />
              </template>
            </div>
          </div>

          <!-- Online Node: Add New Core Interactive Form (multi-core Agents only) -->
          <div v-else-if="multiCoreSupported(coreManagementTargetNode) && addCoreOptions.length" class="space-y-3.5 bg-neutral-50/70 p-4 rounded-2xl border border-black/[0.04]">
            <!-- Core Choice Cards -->
            <div>
              <label class="block text-neutral-700 font-bold mb-2">选择要添加的内核平台（仅可选择尚未上报启用的内核）</label>
              <div class="grid grid-cols-2 gap-2.5">
                <button v-if="!isCoreInstalled('xray')" type="button" @click="selectAddCoreComp('xray')"
                  class="p-3 rounded-xl border text-left transition-all space-y-1.5 relative"
                  :class="addCoreForm.component === 'xray' ? 'border-ios-blue bg-blue-50/60 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
                >
                  <div class="flex items-center justify-between">
                    <span class="font-bold text-sm text-ios-label">Xray-core</span>
                    <span class="text-xs px-1.5 py-0.2 rounded font-semibold bg-blue-100 text-ios-blue">推荐添加</span>
                  </div>
                  <p class="text-xs text-ios-secondary leading-snug">成熟稳定 · VLESS / Trojan / Shadowsocks / WireGuard</p>
                </button>

                <button v-if="!isCoreInstalled('sing-box')" type="button" @click="selectAddCoreComp('sing-box')"
                  class="p-3 rounded-xl border text-left transition-all space-y-1.5 relative"
                  :class="addCoreForm.component === 'sing-box' ? 'border-ios-blue bg-blue-50/60 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
                >
                  <div class="flex items-center justify-between">
                    <span class="font-bold text-sm text-ios-label">sing-box</span>
                    <span class="text-xs px-1.5 py-0.2 rounded font-semibold bg-purple-100 text-purple-700">新一代轻量</span>
                  </div>
                  <p class="text-xs text-ios-secondary leading-snug">现代轻量 · Hysteria2 / TUIC / 自适应路由分流</p>
                </button>

                <button v-if="addCoreOptions.includes('gost')" type="button" @click="selectAddCoreComp('gost')"
                  class="p-3 rounded-xl border text-left transition-all space-y-1.5 relative"
                  :class="addCoreForm.component === 'gost' ? 'border-ios-blue bg-blue-50/60 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
                >
                  <div class="flex items-center justify-between">
                    <span class="font-bold text-sm text-ios-label">GOST</span>
                    <span class="text-xs px-1.5 py-0.2 rounded font-semibold bg-emerald-100 text-emerald-700">固定 3.3.2</span>
                  </div>
                  <p class="text-xs text-ios-secondary leading-snug">只做端口转发与隧道 · 仅 linux/amd64</p>
                </button>
              </div>
            </div>

            <!-- Source (sing-box only): the qui build carries per-user statistics -->
            <div v-if="addCoreForm.component === 'sing-box' && quiSourceAllowed(coreManagementTargetNode)">
              <label class="block text-neutral-600 font-medium mb-1.5">构建来源</label>
              <div class="flex flex-wrap gap-2">
                <button type="button" @click="setAddCoreSource('qui')" class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                  :class="addCorePicker.source === 'qui' ? 'bg-ios-blue text-white border-ios-blue font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'">qui 构建（含用户流量统计，推荐）</button>
                <button type="button" @click="setAddCoreSource('official')" class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                  :class="addCorePicker.source === 'official' ? 'bg-ios-blue text-white border-ios-blue font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'">官方构建</button>
              </div>
            </div>

            <!-- Official releases: picking one fills version, URL and digest. GOST
                 is pinned to one reviewed release, filled in automatically. -->
            <div v-if="addCoreForm.component !== 'gost'">
              <div class="flex items-center justify-between mb-1.5">
                <label class="text-neutral-600 font-medium">选择版本（自动填写地址与官方摘要）</label>
                <button v-if="addCorePicker.state !== 'loading'" type="button" @click="loadCoreReleases(addCorePicker, addCoreForm, coreManagementTargetNode, true)" class="text-ios-blue hover:underline text-xs font-medium">刷新</button>
              </div>
              <p v-if="addCorePicker.state === 'loading'" class="text-xs text-ios-tertiary">正在读取官方发布…</p>
              <p v-else-if="addCorePicker.state === 'failed'" class="text-xs text-ios-orange">无法读取官方发布信息，可点「刷新」重试，或在下方手动填写版本、地址与摘要。</p>
              <p v-else-if="addCorePicker.state === 'ready' && !addCorePicker.items.length" class="text-xs text-ios-orange">没有找到可用的正式版本，请在下方手动填写。</p>
              <div v-else class="flex flex-wrap gap-2">
                <button
                  v-for="(item, index) in addCorePicker.items"
                  :key="item.version"
                  type="button"
                  @click="applyCoreRelease(addCorePicker, addCoreForm, item, coreManagementTargetNode)"
                  class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all tabular-nums"
                  :class="addCorePicker.selected?.version === item.version ? 'bg-ios-blue text-white border-ios-blue shadow-2xs font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'"
                >
                  v{{ item.version }}{{ index === 0 ? '（最新）' : '' }}
                </button>
              </div>
            </div>

            <!-- Component & Version -->
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-neutral-600 font-medium mb-1.5">内核平台</label>
                <select v-model="addCoreForm.component" @change="onAddCoreCompChange" class="form-input-base">
                  <option v-for="name in addCoreOptions" :key="name" :value="name">{{ name }}</option>
                </select>
              </div>
              <div>
                <label class="block text-neutral-600 font-medium mb-1.5">目标版本号</label>
                <input v-model.trim="addCoreForm.version" @input="onAddCoreVersionInput" placeholder="例如: 26.3.27" class="form-input-base w-full !py-1.5 tabular-nums font-semibold" />
              </div>
            </div>

            <!-- URL -->
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">官方发布资产地址 (Asset URL)</label>
              <input v-model.trim="addCoreForm.url" class="form-input-base w-full !py-1.5 font-mono" />
            </div>

            <!-- SHA-256 -->
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">SHA-256 校验和 (64位十六进制)</label>
              <input v-model.trim="addCoreForm.sha256" placeholder="选择版本后自动填写；也可粘贴官方公布的摘要" class="form-input-base w-full !py-1.5 font-mono" />
              <p v-if="releaseDigestNote(addCorePicker, addCoreForm)" class="text-xs mt-1" :class="addCoreForm.sha256 && addCoreForm.url ? 'text-ios-tertiary' : 'text-ios-orange'">{{ releaseDigestNote(addCorePicker, addCoreForm) }}</p>
            </div>

            <!-- Submit Button: 确认添加内核 -->
            <div class="pt-2 flex justify-end">
              <button
                type="button"
                @click="submitAddNewCore"
                :disabled="addCoreBusy"
                class="ios-btn-primary inline-flex items-center gap-1.5 py-2 px-4 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
              >
                <svg v-if="addCoreBusy" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                </svg>
                <span>确认添加内核 ({{ addCoreForm.component === 'xray' ? 'Xray-core' : coreLabel(addCoreForm.component) }})</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>

    <!-- Dialog 4b: 删除内核确认（真实 remove-core API） -->
    <ConfirmDialog
      v-model="removeConfirmVisible"
      title="移除内核"
      :message="removeConfirmMessage"
      warning-details="只会禁用安装器持有的核心文件；如果当前草稿或已发布配置仍有入站使用该内核，后端会以 core_in_use 拒绝并列出这些入站。"
      confirm-text="确认移除"
      :is-danger="true"
      @confirm="confirmRemoveCore"
    />

    <!-- Dialog 5: Core Upgrade / Batch Upgrade Modal -->
    <AppDialog
      v-model="coreDialogVisible"
      :title="coreDialogTitle"
      :description="coreDialogDescription"
      max-width="xl"
      :confirm-text="coreDialogConfirmText"
      :loading="coreBusy"
      @confirm="submitCoreAction"
    >
      <div class="space-y-4 text-xs">
        <!-- Target Node Info -->
        <div v-if="coreDialogTargetNode" class="p-3 bg-neutral-50 rounded-xl border border-black/[0.04] space-y-1">
          <div class="flex items-center justify-between">
            <span class="text-neutral-500 font-medium">目标服务器</span>
            <span class="font-bold text-neutral-800">{{ coreDialogTargetNode.name }}</span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-neutral-500 font-medium">当前运行内核</span>
            <span
              class="tabular-nums font-semibold"
              :class="getNodeInstalledCores(coreDialogTargetNode).length ? 'text-purple-700' : 'text-amber-700'"
            >
              {{ getNodeInstalledCores(coreDialogTargetNode).map(c => `${c.name} ${c.version}`).join(', ') || '尚未安装内核' }}
            </span>
          </div>
          <div class="flex items-center justify-between">
            <span class="text-neutral-500 font-medium">操作系统平台</span>
            <span class="text-neutral-600">{{ coreDialogTargetNode.os || 'Linux' }} ({{ detectArch(coreDialogTargetNode) || '—' }})</span>
          </div>
        </div>

        <!-- Upgrade Configuration Form -->
        <div class="space-y-3.5">
          <!-- Source (sing-box only) -->
          <div v-if="coreForm.component === 'sing-box' && coreQuiAllowed">
            <label class="block text-neutral-600 font-medium mb-1.5">构建来源</label>
            <div class="flex flex-wrap gap-2">
              <button type="button" @click="setCoreSource('qui')" class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                :class="corePicker.source === 'qui' ? 'bg-ios-blue text-white border-ios-blue font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'">qui 构建（含用户流量统计）</button>
              <button type="button" @click="setCoreSource('official')" class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                :class="corePicker.source === 'official' ? 'bg-ios-blue text-white border-ios-blue font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'">官方构建</button>
            </div>
          </div>

          <!-- Official releases: picking one fills version, URL and digest -->
          <div>
            <div class="flex items-center justify-between mb-1.5">
              <label class="text-neutral-600 font-medium">选择版本（自动填写地址与官方摘要）</label>
              <button v-if="corePicker.state !== 'loading'" type="button" @click="loadCoreReleases(corePicker, coreForm, coreDisplayNode, true)" class="text-ios-blue hover:underline text-xs font-medium">刷新</button>
            </div>
            <p v-if="corePicker.state === 'loading'" class="text-xs text-ios-tertiary">正在读取官方发布…</p>
            <p v-else-if="corePicker.state === 'failed'" class="text-xs text-ios-orange">无法读取官方发布信息，可点「刷新」重试，或在下方手动填写版本、地址与摘要。</p>
            <p v-else-if="corePicker.state === 'ready' && !corePicker.items.length" class="text-xs text-ios-orange">没有找到可用的正式版本，请在下方手动填写。</p>
            <div v-else class="flex flex-wrap gap-2">
              <button
                v-for="(item, index) in corePicker.items"
                :key="item.version"
                type="button"
                @click="applyCoreRelease(corePicker, coreForm, item, coreDisplayNode)"
                class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all tabular-nums"
                :class="corePicker.selected?.version === item.version ? 'bg-ios-blue text-white border-ios-blue shadow-2xs font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'"
              >
                v{{ item.version }}{{ index === 0 ? '（最新）' : '' }}
              </button>
            </div>
          </div>

          <!-- Version & Architecture Inputs -->
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">升级内核平台</label>
              <select v-if="coreDialogTargetNode" v-model="coreForm.component" @change="onCoreComponentChange" class="form-input-base w-full !py-1.5">
                <option v-for="core in getNodeInstalledCores(coreDialogTargetNode)" :key="core.component" :value="core.component">{{ core.name }} {{ core.version }}</option>
              </select>
              <select v-else v-model="coreForm.component" @change="onCoreComponentChange" :disabled="coreBatchComponentLocked" class="form-input-base w-full !py-1.5 disabled:opacity-60">
                <option v-for="item in coreBatchComponents" :key="item.name" :value="item.name">{{ item.name }}（{{ item.count }} 台）</option>
              </select>
            </div>
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">目标版本号</label>
              <input v-model.trim="coreForm.version" @input="onCoreVersionInput" placeholder="例如: 26.3.27" class="form-input-base w-full !py-1.5 tabular-nums font-semibold" />
            </div>
          </div>
          <p v-if="!coreDialogTargetNode" class="text-xs text-ios-secondary">
            将只向已运行 {{ coreForm.component }} 的 {{ coreBatchTargets.length }} 台在线服务器下发同一个版本；未运行该内核的服务器不会收到安装任务。
            <template v-if="coreBatchArchitectures > 1">这些服务器的架构不同：选择版本后每台自动使用对应的文件与摘要，下方只显示「{{ coreDisplayNode?.name }}」的。手动改过地址或摘要时，所有服务器都使用你填写的值。</template>
          </p>

          <div>
            <label class="block text-neutral-600 font-medium mb-1.5">官方发布资产地址 (Asset URL)</label>
            <input v-model.trim="coreForm.url" placeholder="https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip" class="form-input-base w-full !py-1.5 font-mono" />
            <p class="text-xs text-neutral-400 mt-1">必须为官方 releases 下载地址，节点下载后将比对文件指纹。</p>
          </div>

          <div>
            <label class="block text-neutral-600 font-medium mb-1.5">SHA-256 校验和 (64位十六进制)</label>
            <input v-model.trim="coreForm.sha256" placeholder="选择版本后自动填写；也可粘贴官方公布的摘要" class="form-input-base w-full !py-1.5 font-mono" />
            <p v-if="releaseDigestNote(corePicker, coreForm)" class="text-xs mt-1" :class="coreForm.sha256 && coreForm.url ? 'text-ios-tertiary' : 'text-ios-orange'">{{ releaseDigestNote(corePicker, coreForm) }}</p>
          </div>
        </div>
      </div>
    </AppDialog>

    <!-- Dialog 6: Core Catalog Feature Comparison Modal -->
    <AppDialog
      v-model="showCoreCatalogModal"
      title="代理核心特性对比 (支持的核心)"
      description="对比 Xray-core 与 sing-box 两个官方核心的技术定位、网络协议矩阵与适用场景。"
      max-width="2xl"
      confirm-text="知道了"
      :show-cancel="false"
      @confirm="showCoreCatalogModal = false"
    >
      <div class="space-y-4 text-xs">
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <!-- Xray-core Card -->
          <div class="p-4 rounded-2xl bg-white border border-blue-200/80 shadow-2xs space-y-3">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <div class="w-8 h-8 rounded-xl bg-blue-50 text-ios-blue flex items-center justify-center font-bold text-sm">
                  X
                </div>
                <div>
                  <h4 class="text-ios-label text-base font-semibold leading-normal">Xray-core</h4>
                  <span class="text-xs text-ios-tertiary">XTLS 官方团队</span>
                </div>
              </div>
              <span class="text-xs px-2 py-0.5 rounded-full bg-blue-50 text-ios-blue font-semibold border border-blue-200/60">
                稳定成熟
              </span>
            </div>

            <p class="text-ios-secondary leading-relaxed text-xs">
              功能完备的高性能网络代理核心，深度优化 TLS/XTLS 协议栈与 Reality 伪装技术，兼容性极高。
            </p>

            <div class="space-y-1.5 pt-1 border-t border-black/[0.04]">
              <span class="text-neutral-500 font-medium block text-xs">支持协议矩阵:</span>
              <div class="flex flex-wrap gap-1">
                <span v-for="p in ['VLESS', 'Trojan', 'Shadowsocks', 'SOCKS5', 'HTTP', 'WireGuard']" :key="p" class="px-1.5 py-0.5 rounded bg-blue-50 text-ios-blue font-bold text-xs">
                  {{ p }}
                </span>
              </div>
            </div>

            <div class="text-xs text-ios-tertiary space-y-0.5 pt-1">
              <div><strong>推荐版本:</strong> v25.10.15 (版本辅助) / v26.3.27</div>
              <div><strong>适用场景:</strong> 常规出入站、多用户订阅、抗封锁伪装</div>
            </div>
          </div>

          <!-- sing-box Card -->
          <div class="p-4 rounded-2xl bg-white border border-purple-200/80 shadow-2xs space-y-3">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-2">
                <div class="w-8 h-8 rounded-xl bg-purple-50 text-purple-600 flex items-center justify-center font-bold text-sm">
                  S
                </div>
                <div>
                  <h4 class="text-ios-label text-base font-semibold leading-normal">sing-box</h4>
                  <span class="text-xs text-ios-tertiary">SagerNet 官方团队</span>
                </div>
              </div>
              <span class="text-xs px-2 py-0.5 rounded-full bg-purple-50 text-purple-700 font-semibold border border-purple-200/60">
                新一代轻量
              </span>
            </div>

            <p class="text-ios-secondary leading-relaxed text-xs">
              新一代通用代理平台框架，占用内存与 CPU 极低，原生支持 Hysteria2 等下一代基于 QUIC 的高速协议。
            </p>

            <div class="space-y-1.5 pt-1 border-t border-black/[0.04]">
              <span class="text-neutral-500 font-medium block text-xs">支持协议矩阵:</span>
              <div class="flex flex-wrap gap-1">
                <span v-for="p in ['Hysteria2', 'TUIC', 'Shadowsocks', 'VLESS', 'Trojan', 'Direct']" :key="p" class="px-1.5 py-0.5 rounded bg-purple-50 text-purple-700 font-bold text-xs">
                  {{ p }}
                </span>
              </div>
            </div>

            <div class="text-xs text-ios-tertiary space-y-0.5 pt-1">
              <div><strong>推荐版本:</strong> v1.10.7 (版本辅助) / v1.11.0</div>
              <div><strong>适用场景:</strong> 弱网加速、UDP 直连加速、极小内存 VPS</div>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { needsBaseline, publishBaselineAndWait } from '../nodeBaseline';
import { onMounted, onUnmounted, ref, reactive, computed } from 'vue';
import { isOwner, formatTime } from '../../../app/auth';
import { throttleTrailing } from '../../../app/nodeStatusEvents';
import { showToast } from '../../../app/api';
import { deploymentAPI } from '../deploymentApi';
import { CORE_INSTALL_OPTIONS, sortInstallCores, validateInstallCores } from '../../nodes/coreInstallOptions';
import { APIError } from '../../../app/apiClient';
import { nodeAPI, installCommand, type CoreName, type Enrollment, type NodeView, type AgentReleaseVersion } from '../../nodes/api';
import { browserInstallEndpoint, resolveInstallEndpoint } from '../../nodes/installEndpoint';
import { proxyAPI, type Inbound } from '../../proxy/api';
import { blockedCores, coreLabel, coreSummaryLabel, isCoreName, multiCoreSupported, nodeCores, primaryCoreName } from '../../nodes/coreSummary';
import { getNodeMeta, resolveCountryInfo, countryTitle, type CountryInfo } from '../../nodes/nodeMeta';
import { connectAddress } from '../../nodes/nodeAddress';
import { agentUpdatesAPI } from '../agentUpdatesApi';
import { coreUpdatesAPI, type CoreRemoveTask, type CoreUpdateTask, type ReleaseEntry } from '../coreUpdatesApi';
import { coreReleaseFields, coreReleasesAPI, defaultCoreSource, deriveAssetURL, DIGEST_SOURCE_LABELS, nodeArchitecture, nodeUsesMusl, pickAsset, quiSourceAllowed, type CoreRelease, type CoreReleaseAsset, type CoreSource } from '../coreReleases';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import Pagination from '../../../components/Pagination.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import CodePreview from '../../../components/CodePreview.vue';
import ServerDisplayMode from '../../nodes/components/ServerDisplayMode.vue';
import WebUpgradeOption from '../../nodes/components/WebUpgradeOption.vue';

const props = withDefaults(
  defineProps<{
    kind: 'agent' | 'core';
    embedded?: boolean;
    currentTab?: 'agents' | 'cores';
  }>(),
  {
    embedded: false,
    currentTab: 'agents'
  }
);

defineEmits<{
  (e: 'change-tab', tab: string): void;
}>();

const nodes = ref<NodeView[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const statusFilter = ref<'all' | 'online' | 'offline' | 'pending' | 'degraded' | 'applying' | 'rollback' | 'error' | 'disabled'>('all');
const loading = ref(false);
const error = ref('');

// Display Mode: 'grid' (Cards) or 'list' (Table)
const displayMode = ref<'grid' | 'list'>(
  (localStorage.getItem(`qui_${props.kind}_display_mode`) as 'grid' | 'list') || 'grid'
);

function setDisplayMode(mode: 'grid' | 'list') {
  displayMode.value = mode;
  localStorage.setItem(`qui_${props.kind}_display_mode`, mode);
}

// Upgrade Context: Catalog Releases & Recent Core Tasks
const agentReleases = ref<ReleaseEntry[]>([]);
const showCoreCatalogModal = ref(false);

const latestAgentRelease = computed(() => {
  const list = agentReleases.value.filter(r => r.signed?.manifest?.component === 'proxy-agent');
  if (!list.length) return null;
  return list.slice().sort((a, b) => (b.signed?.manifest?.sequence || 0) - (a.signed?.manifest?.sequence || 0))[0];
});

function findLatestAgentReleaseForNode(node: NodeView, releases: ReleaseEntry[]): ReleaseEntry | undefined {
  const platform = nodePlatform(node);
  if (!platform) return undefined;
  return releases.filter(r => r.signed.manifest.component === 'proxy-agent' && r.signed.manifest.os + '/' + r.signed.manifest.architecture === platform)
    .sort((a,b) => b.signed.manifest.sequence - a.signed.manifest.sequence)[0];
}
const agentUpgradeDialogVisible = ref(false);
const agentUpgradeTargetNode = ref<NodeView | null>(null);
const agentReleaseList = ref<AgentReleaseVersion[]>([]);
const agentUpgradeLoading = ref(false);
const agentUpgradeBusy = ref(false);

async function loadAgentReleases(refresh = false) {
  agentUpgradeLoading.value = true;
  try {
    agentReleaseList.value = await nodeAPI.agentReleases(refresh);
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    agentUpgradeLoading.value = false;
  }
}

function findAgentReleaseForNode(node: NodeView, version: string, entries: ReleaseEntry[]): ReleaseEntry | undefined {
  const platform = nodePlatform(node);
  if (!platform) return undefined;
  return entries.find(r =>
    r.signed?.manifest?.component === 'proxy-agent' &&
    r.signed?.manifest?.version === version &&
    (r.signed?.manifest?.os + '/' + r.signed?.manifest?.architecture) === platform
  );
}

async function selectAgentVersionForUpgrade(versionItem: AgentReleaseVersion) {
  if (agentUpgradeBusy.value) return;
  agentUpgradeBusy.value = true;
  try {
    let entries: ReleaseEntry[] = [];
    if (!versionItem.imported) {
      entries = await nodeAPI.importAgentRelease(versionItem.version);
      versionItem.imported = true;
      await loadUpgradeContext();
    } else {
      await loadUpgradeContext();
      entries = agentReleases.value;
    }

    if (agentUpgradeTargetNode.value) {
      const node = agentUpgradeTargetNode.value;
      const targetRel = findAgentReleaseForNode(node, versionItem.version, entries);
      if (!targetRel) {
        throw new Error(`找不到与服务器平台匹配的 ${versionItem.version} 版本`);
      }
      await agentUpdatesAPI.queue(node.id, targetRel.id, node.heartbeat!.current_revision!);
      showToast(`已向「${node.name}」下发 Agent 升级任务 (v${versionItem.version})`);
      agentUpgradeDialogVisible.value = false;
    } else {
      const eligible = eligibleAgentOnlineNodes.value;
      if (!eligible.length) {
        throw new Error('没有符合升级条件的在线服务器');
      }
      let success = 0;
      let failed = 0;
      for (const node of eligible) {
        try {
          const targetRel = findAgentReleaseForNode(node, versionItem.version, entries);
          if (!targetRel) throw new Error('无完全匹配的平台版本');
          await agentUpdatesAPI.queue(node.id, targetRel.id, node.heartbeat!.current_revision!);
          success++;
        } catch {
          failed++;
        }
      }
      if (success > 0) {
        showToast(`已成功向 ${success} 台在线服务器下发 Agent 升级任务${failed ? ` (${failed} 台失败)` : ''}`);
        agentUpgradeDialogVisible.value = false;
      } else {
        showToast('批量升级任务下发失败，请检查各节点状态与版本配置', 'error');
      }
    }
    void load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    agentUpgradeBusy.value = false;
  }
}
const manualAgentNode = ref<NodeView | null>(null);
const manualReleaseId = ref('');
async function readAllNodes(): Promise<NodeView[]> {
  const first = await nodeAPI.list(1, '');
  const list = [...first.items];
  for (let page = 2; list.length < first.total; page++) {
    const next = await nodeAPI.list(page, '');
    if (!next.items.length) throw new Error('服务器分页读取不完整，请刷新重试');
    list.push(...next.items);
  }
  return list;
}
const batchNodes = ref<NodeView[]>([]);

async function loadUpgradeContext() {
  if (!isOwner.value) return;
  try {
    const first = await coreUpdatesAPI.releases(1, 100);
    const list = [...first.items];
    for (let page = 2; list.length < first.total; page++) {
      const next = await coreUpdatesAPI.releases(page, 100);
      if (!next.items.length) throw new Error('发布目录分页不完整');
      list.push(...next.items);
    }
    agentReleases.value = list;
  } catch (e) { error.value = (e as Error).message; showToast(error.value, 'error'); }
}

// Single Agent Upgrade Dialog State
const singleAgentConfirmVisible = ref(false);
const singleAgentConfirmTarget = ref<{ node: NodeView; release: ReleaseEntry; revision: number } | null>(null);

const singleAgentConfirmMessage = computed(() => {
  const t = singleAgentConfirmTarget.value;
  if (!t) return '';
  return `确认将服务器「${t.node.name}」的 Agent 升级为最新版本 v${t.release.signed.manifest.version}？（当前版本: ${t.node.agentVersion || '未上报'}，配置版本 #${t.revision}）`;
});

// Batch Agent Upgrade Dialog State
const batchAgentConfirmVisible = ref(false);

const eligibleOnlineNodes = computed(() => {
  return batchNodes.value.filter(n => n.status === 'online' && n.heartbeat?.current_revision);
});

const eligibleAgentOnlineNodes = computed(() => {
  return eligibleOnlineNodes.value.filter(n => n.agentUpdateSupported);
});

const batchAgentConfirmMessage = computed(() => {
  const count = eligibleAgentOnlineNodes.value.length;
  return `确认遍历全部 ${count} 台符合条件的在线服务器，按各自 os/arch 下发最新签名版本？无完全匹配版本的服务器会跳过并报告原因，请逐台手动选择。`;
});

// ========================================================
// CORE PRESETS & DATA STRUCTURES
// ========================================================
// The add and upgrade dialogs list the newest stable releases of the chosen core
// and source from GET /api/v1/core-releases (see ../coreReleases.ts): picking one
// fills the version, the official asset URL for the server's architecture and C
// library, and the digest the vendor published for that asset. The panel never
// computes a digest; an asset without a published one leaves the field empty
// for the Owner to paste, and every value stays editable.
export interface InstalledCoreInfo {
  component: CoreName;
  name: string;
  version: string;
  // null = the Agent reported no health observation for this core.
  healthy: boolean | null;
  error: string;
  protocols: string[];
  primary: boolean;
}

// ========================================================
// MULTI-CORE INSTALLED STATE MANAGEMENT
// ========================================================
const CORE_ORDER: CoreName[] = ['xray', 'sing-box', 'gost'];
// The list comes from the backend's per-core data (capabilities.cores with the
// heartbeat's per-core health). An old Agent reports no core list and is shown as
// exactly one core built from its scalar capability fields.
function getNodeInstalledCores(node: NodeView): InstalledCoreInfo[] {
  return nodeCores(node).map(core => ({
    component: core.name,
    name: core.label,
    version: core.version,
    healthy: core.healthy,
    error: core.error,
    protocols: core.protocols.filter(item => item.inbound || item.outbound).map(item => item.protocol),
    primary: core.primary
  }));
}

function getNodeAllProtocols(node: NodeView): string[] {
  const set = new Set<string>();
  for (const core of getNodeInstalledCores(node)) for (const proto of core.protocols) set.add(proto);
  if (!set.size) for (const item of node.capabilities?.protocols || []) if (item.inbound || item.outbound) set.add(item.protocol);
  return [...set].sort();
}

// A server without a confirmed revision is not refused here: upgrading it first
// publishes a baseline configuration (see withBaseline).
function upgradeUnavailableReason(node: NodeView): string {
  if (node.status === 'pending' || !node.agentVersion || !Number.isFinite(Date.parse(node.lastHeartbeat))) return '尚未收到 Agent 和心跳报告，请先完成登记';
  if (!node.enabled || node.deletedAt || (node.status !== 'online' && node.status !== 'degraded')) return '服务器需启用且在线后才能升级';
  const age = Date.now() - Date.parse(node.lastHeartbeat);
  if (age < -5000 || age >= 45000) return '心跳已过期，等待服务器上线';
  return '';
}

// v1.5.5 item 11: Agent and core updates are bound to the node's confirmed
// revision, which must be at least 1. A server that never published a
// configuration first gets an empty baseline revision; once the node confirms
// it, the original action continues with the refreshed node.
const baselineConfirmVisible = ref(false);
const baselineJob = ref<{ nodes: NodeView[]; next: () => void } | null>(null);
const baselineConfirmMessage = computed(() => {
  const names = (baselineJob.value?.nodes || []).map(node => `「${node.name}」`).join('、');
  return `${names} 还没有发布过配置，需要先发布基础配置才能升级。`;
});
function withBaseline(targets: NodeView[], next: () => void) {
  const pending = targets.filter(needsBaseline);
  if (!pending.length) { next(); return; }
  baselineJob.value = { nodes: pending, next };
  baselineConfirmVisible.value = true;
}
async function runBaselineThenContinue() {
  const job = baselineJob.value;
  baselineJob.value = null;
  if (!job) return;
  showToast(`正在为 ${job.nodes.length} 台服务器发布基础配置…`);
  const results = await Promise.allSettled(job.nodes.map(publishBaselineAndWait));
  results.forEach((result, index) => {
    if (result.status === 'rejected') showToast(`${job.nodes[index].name}：${(result.reason as Error).message}`, 'error');
  });
  void load();
  if (results.some(result => result.status === 'fulfilled')) job.next();
}

function detectArch(node?: NodeView | null): 'amd64' | 'arm64' | '' {
  if (!node) return '';
  const caps = node.capabilities as Record<string, unknown>;
  const arch = String(node.capabilities?.host?.architecture || caps?.architecture || '');
  if (arch === 'amd64' || arch === 'arm64') return arch;
  const platform = String(caps?.platform || node.os || '');
  return platform.endsWith('/amd64') ? 'amd64' : platform.endsWith('/arm64') ? 'arm64' : '';
}
function nodePlatform(node: NodeView): string {
  const caps = node.capabilities as Record<string, unknown>;
  if (typeof caps?.platform === 'string' && /^[^/]+\/[^/]+$/.test(caps.platform)) return caps.platform;
  if (/^[^/]+\/[^/]+$/.test(node.os || '')) return node.os;
  const os = String(caps?.os || node.os || '');
  return detectArch(node) && os === 'linux' ? os + '/' + detectArch(node) : '';
}
function requireCoreTarget(node: NodeView, component: CoreName) {
  if (!isOwner.value) throw new Error('只有 Owner 可以管理内核');
  const age = Date.now() - Date.parse(node.lastHeartbeat);
  if (!Number.isFinite(age) || age < -5000 || node.status !== 'online' || !node.enabled || node.deletedAt || age >= 45000 || !node.heartbeat?.current_revision) throw new Error('服务器需在线并上报当前配置版本');
  if (!multiCoreSupported(node) && node.capabilities?.core_name !== component) throw new Error('该节点 Agent 未协商多核心支持，只能更新当前上报的核心');
  if (blockedCores(node).includes(component)) throw new Error('core_pending：该内核正在添加或移除中，请等待 Agent 上报新的能力后再操作');
}
// True while a removal of this component is queued or still unanswered by the
// Agent. Only task rows count; nothing is assumed.
function corePendingRemoval(node: NodeView, component: CoreName): boolean {
  return (coreRemovals.value[node.id + ':' + component] || []).some(task => task.status === 'queued' || task.status === 'running');
}

// ========================================================
// CORE MANAGEMENT MODAL (添加内核 / 删除内核 / 每核心任务)
// ========================================================
const coreManagementModalVisible = ref(false);
const coreManagementTargetNode = ref<NodeView | null>(null);
// Per-(node, component) task rows read from GET /core-updates, the removal history
// of the two components and the inbounds that use each core. None of it is kept
// in browser storage: the dialog reloads it from the backend.
const coreTasks = ref<CoreUpdateTask[]>([]);
const coreRemovals = ref<Record<string, CoreRemoveTask[]>>({});
const coreInbounds = ref<Inbound[]>([]);
const coreFactsLoading = ref(false);

const manageInstalledCores = computed(() => {
  if (!coreManagementTargetNode.value) return [];
  return getNodeInstalledCores(coreManagementTargetNode.value);
});

function isCoreInstalled(comp: CoreName): boolean {
  if (!coreManagementTargetNode.value) return false;
  return getNodeInstalledCores(coreManagementTargetNode.value).some(c => c.component === comp);
}

// An already enabled core is not offered again: the option is hidden instead of
// shown greyed out (Owner rule).
// GOST is offered only where the Agent can install it: it reported
// gost_core_supported and runs on linux/amd64 (multi-core is checked by the form).
const addCoreOptions = computed(() => CORE_ORDER.filter(name => !isCoreInstalled(name)
  && (name !== 'gost' || Boolean(coreManagementTargetNode.value?.gostCoreSupported && detectArch(coreManagementTargetNode.value) === 'amd64'))));

function coreTasksOf(nodeId: string, component: string): CoreUpdateTask[] {
  return coreTasks.value.filter(task => task.nodeId === nodeId && task.component === component)
    .sort((a, b) => (b.sequence || 0) - (a.sequence || 0));
}
function latestCoreTask(nodeId: string, component: string): CoreUpdateTask | undefined {
  return coreTasksOf(nodeId, component)[0];
}
const CORE_TASK_STATUS: Record<string, string> = {
  queued: '排队中', running: '进行中', success: '成功', failed: '失败', failed_closed: '失败（已关闭）', cleared: '已清除',
  rejected: '已拒绝', unconfirmed: '未确认'
};
function coreTaskStatusText(status: string): string {
  return CORE_TASK_STATUS[status] || status || '—';
}
function coreTaskTone(status: string): string {
  if (status === 'success') return 'bg-ios-green/10 text-ios-green';
  if (status === 'failed' || status === 'failed_closed' || status === 'rejected') return 'bg-ios-red/10 text-ios-red';
  if (status === 'queued' || status === 'running') return 'bg-ios-blue/10 text-ios-blue';
  return 'bg-ios-fill text-ios-tertiary';
}
// Shown next to a core whose add/remove task is not confirmed yet. The core stays
// visible until a capability report drops it, exactly like the backend.
function corePendingLabel(node: NodeView, component: CoreName): string {
  if (corePendingRemoval(node, component)) return '移除中';
  const task = latestCoreTask(node.id, component);
  if (task && (task.status === 'queued' || task.status === 'running')) return task.adding ? '安装中' : '升级中';
  if (blockedCores(node).includes(component)) return '处理中';
  return '';
}
// Inbounds of this node that run on the component. An empty core follows the
// node's current primary core, so it belongs to whichever core is primary now.
function inboundsOnCore(node: NodeView, component: CoreName): Inbound[] {
  const primary = primaryCoreName(node);
  return coreInbounds.value.filter(item => item.nodeId === node.id && (item.core || primary) === component);
}
function inboundLink(nodeId: string): string {
  return '/nodes/' + nodeId + '?tab=nodes';
}
// Dialog-scoped helpers: they always read the node that is currently open.
function manageCoreInbounds(component: CoreName): Inbound[] {
  const node = coreManagementTargetNode.value;
  return node ? inboundsOnCore(node, component) : [];
}
function manageCoreTask(component: CoreName): CoreUpdateTask | undefined {
  const node = coreManagementTargetNode.value;
  return node ? latestCoreTask(node.id, component) : undefined;
}
function manageCorePendingLabel(component: CoreName): string {
  const node = coreManagementTargetNode.value;
  return node ? corePendingLabel(node, component) : '';
}

async function readAllInbounds(nodeId: string): Promise<Inbound[]> {
  const first = await proxyAPI.inbounds(nodeId, 1, 100);
  const list = [...first.items];
  for (let page = 2; list.length < first.total; page++) {
    const next = await proxyAPI.inbounds(nodeId, page, 100);
    if (!next.items.length) break;
    list.push(...next.items);
  }
  return list.filter(item => item.nodeId === nodeId);
}
async function readAllCoreTasks(): Promise<CoreUpdateTask[]> {
  const first = await coreUpdatesAPI.tasks(1, 100);
  const list = [...first.items];
  for (let page = 2; list.length < first.total; page++) {
    const next = await coreUpdatesAPI.tasks(page, 100);
    if (!next.items.length) break;
    list.push(...next.items);
  }
  return list;
}
// Backend facts for the dialog: which inbounds use which core, the latest task per
// component and the removal history. A read failure stays visible instead of
// inventing a state; every number shown comes from one of these responses.
async function loadCoreFacts(node: NodeView) {
  coreFactsLoading.value = true;
  removeConflictInbounds.value = [];
  try {
    const [inbounds, tasks] = await Promise.all([readAllInbounds(node.id), readAllCoreTasks()]);
    coreInbounds.value = inbounds;
    coreTasks.value = tasks;
    const removals = await Promise.all([
      coreUpdatesAPI.removals(node.id, 'xray').catch(() => ({ items: [] as CoreRemoveTask[] })),
      coreUpdatesAPI.removals(node.id, 'sing-box').catch(() => ({ items: [] as CoreRemoveTask[] })),
      coreUpdatesAPI.removals(node.id, 'gost').catch(() => ({ items: [] as CoreRemoveTask[] }))
    ]);
    coreRemovals.value = {
      [node.id + ':xray']: removals[0].items,
      [node.id + ':sing-box']: removals[1].items,
      [node.id + ':gost']: removals[2].items
    };
  } catch (e) {
    showToast('内核任务与入站信息读取失败：' + (e as Error).message, 'error');
  } finally {
    coreFactsLoading.value = false;
  }
}

// ========================================================
// REMOVE CORE (删除内核): Owner + CSRF + confirmNodeId on the real API
// ========================================================
const removeConfirmVisible = ref(false);
const removeTargetCore = ref<InstalledCoreInfo | null>(null);
const removeBusy = ref(false);
// Inbounds the backend reports as still using the component (409 core_in_use).
const removeConflictInbounds = ref<{ id: string; name: string }[]>([]);
const removeConfirmMessage = computed(() => {
  const node = coreManagementTargetNode.value, core = removeTargetCore.value;
  if (!node || !core) return '';
  return `确认从「${node.name}」移除 ${core.name} 内核？Agent 只会禁用安装器持有的文件，之后重新安装同一内核会再次启用；移除后无法回滚到使用该内核的历史配置版本。`;
});
function requestRemoveCore(core: InstalledCoreInfo) {
  if (!isOwner.value || !coreManagementTargetNode.value) return;
  removeConflictInbounds.value = [];
  removeTargetCore.value = core;
  removeConfirmVisible.value = true;
}
const REMOVE_ERROR_TEXT: Record<string, string> = {
  core_in_use: '该内核仍有入站使用（含已禁用的入站），请先删除或改派下列入站后再移除内核。',
  core_pending: '该内核正在添加、移除或升级中，请等待 Agent 回报后再试。',
  last_core: '这是该服务器最后一个已启用的内核，不能移除。',
  revision_mismatch: '服务器确认的配置版本已经变化，面板已刷新，请重新确认后再移除。',
  unsupported: '该 Agent 未协商多核心支持，或该内核未上报为启用，不能移除。',
  invalid_request: '请求被后端拒绝，请刷新后重试。'
};
async function confirmRemoveCore() {
  const node = coreManagementTargetNode.value, core = removeTargetCore.value;
  if (!isOwner.value || !node || !core || removeBusy.value) return;
  removeBusy.value = true;
  removeConflictInbounds.value = [];
  try {
    requireCoreTarget(node, core.component);
    await coreUpdatesAPI.remove(node.id, core.component, node.heartbeat!.current_revision!);
    showToast(`已提交移除 ${core.name} 内核任务，等待 Agent 回报；内核会保留到能力上报确认移除为止。`);
    await load();
    const fresh = nodes.value.find(item => item.id === node.id);
    if (fresh) { coreManagementTargetNode.value = fresh; void loadCoreFacts(fresh); }
  } catch (e) {
    const error = e as APIError;
    const ids = Array.isArray(error.details?.inboundIds) ? error.details.inboundIds as string[] : [];
    if (error.code === 'core_in_use' && ids.length) {
      removeConflictInbounds.value = ids.map(id => {
        const match = coreInbounds.value.find(item => item.id === id);
        return { id, name: match ? `${match.name} (${match.protocol.toUpperCase()} :${match.port})` : id };
      });
    }
    showToast(REMOVE_ERROR_TEXT[error.code] || error.message || '移除内核失败', 'error');
    if (error.code === 'revision_mismatch') void load();
  } finally {
    removeBusy.value = false;
  }
}

function openCoreManagementModal(node: NodeView) {
  if (!isOwner.value) return;
  addCoreForm.sha256 = ''; addCoreForm.url = '';
  removeConflictInbounds.value = [];
  coreInbounds.value = []; coreTasks.value = [];
  coreManagementTargetNode.value = node;
  // This dialog adds a core, so a core the server already reports is never
  // preselected: the first still-missing platform is offered instead.
  const component = node.capabilities?.core_name;
  const offered = addCoreOptions.value;
  if (isCoreName(component) && offered.includes(component)) selectAddCoreComp(component);
  else if (offered.length) selectAddCoreComp(offered[0]);
  coreManagementModalVisible.value = true;
  void loadCoreFacts(node);

  if (node.status === 'pending') {
    pendingEnrollmentLoading.value = true;
    pendingWebUpgrade.value = false;
    // Show the browser address at once and swap in the saved one when it
    // arrives, so the command never waits on the settings request.
    const fallback = browserInstallEndpoint();
    installEndpointHost.value = fallback.host;
    installEndpointPort.value = fallback.port;
    installEndpointGrpcHost.value = '';
    installEndpointGrpcPort.value = null;
    installEndpointFromSettings.value = false;
    void resolveInstallEndpoint().then(endpoint => {
      installEndpointHost.value = endpoint.host;
      installEndpointPort.value = endpoint.port;
      installEndpointGrpcHost.value = endpoint.grpcHost;
      installEndpointGrpcPort.value = endpoint.grpcPort;
      installEndpointFromSettings.value = endpoint.fromSettings;
    });
    void nodeAPI.reissueEnrollment(node.id).then(res => {
      pendingEnrollment.value = res;
    }).catch(e => {
      showToast((e as Error).message, 'error');
    }).finally(() => {
      pendingEnrollmentLoading.value = false;
    });
  }
}

// Add Core Form State inside Management Modal
const addCoreForm = reactive({
  component: 'xray' as CoreName,
  version: '',
  url: '',
  sha256: ''
});

const addCoreBusy = ref(false);

// One release picker per dialog: the releases of the chosen core and source, the
// release the Owner picked and the URL/digest it filled in. When the Owner edits
// the version, URL or digest afterwards the form is "manual" and is submitted as
// typed; the digest note then no longer claims a source.
interface ReleasePicker {
  source: CoreSource;
  items: CoreRelease[];
  state: 'idle' | 'loading' | 'ready' | 'failed';
  selected: CoreRelease | null;
  autoURL: string;
  autoSHA: string;
  digestSource: CoreReleaseAsset['digestSource'];
  generation: number;
}
type CoreFormFields = { component: CoreName; version: string; url: string; sha256: string };
function newReleasePicker(): ReleasePicker {
  return reactive<ReleasePicker>({ source: 'official', items: [], state: 'idle', selected: null, autoURL: '', autoSHA: '', digestSource: '', generation: 0 }) as ReleasePicker;
}
const addCorePicker = newReleasePicker();

async function loadCoreReleases(picker: ReleasePicker, form: CoreFormFields, node: NodeView | null, refresh = false) {
  const current = ++picker.generation;
  picker.state = 'loading'; picker.items = []; picker.selected = null; picker.digestSource = '';
  try {
    const list = await coreReleasesAPI.list(form.component, picker.source, refresh);
    if (current !== picker.generation) return;
    picker.items = list.items;
    picker.state = 'ready';
    if (list.items.length) applyCoreRelease(picker, form, list.items[0], node, true);
  } catch {
    if (current === picker.generation) picker.state = 'failed';
  }
}

// applyCoreRelease fills the form from one release for one server. A release
// without an archive for the server's architecture or C library fills no URL and
// no digest instead of guessing one.
function applyCoreRelease(picker: ReleasePicker, form: CoreFormFields, item: CoreRelease, node: NodeView | null, quiet = false) {
  picker.selected = item;
  Object.assign(form, coreReleaseFields(item, node));
  const asset = pickAsset(item, node);
  picker.autoURL = form.url; picker.autoSHA = form.sha256;
  picker.digestSource = asset?.sha256 ? asset.digestSource : '';
  if (!asset && node && !quiet) {
    showToast(`v${item.version} 没有适用于「${node.name}」（${nodeArchitecture(node) || '架构未知'}${nodeUsesMusl(node) ? '，musl' : ''}）的文件`, 'warning');
  }
}

function releaseFormManual(picker: ReleasePicker, form: CoreFormFields): boolean {
  return !picker.selected || form.version !== picker.selected.version || form.url !== picker.autoURL || form.sha256 !== picker.autoSHA;
}

function releaseDigestNote(picker: ReleasePicker, form: CoreFormFields): string {
  if (!picker.selected || releaseFormManual(picker, form)) return '';
  if (!form.url) return '该版本没有适用于这台服务器的文件，请换一个版本或手动填写。';
  if (!form.sha256) return '该文件没有可用的官方摘要（缺失或两处公布的值不一致），请从官方发布页核对后手动粘贴。';
  return `摘要已自动填写，来源：${DIGEST_SOURCE_LABELS[picker.digestSource]}；节点下载后仍会逐字节核对。`;
}

// Typing a version drops the picked release, its digest and the URL it filled:
// the URL is rebuilt from the official naming rule where one exists, and cleared
// where it cannot be derived (the qui build lives under a panel release tag and
// GOST is one pinned archive), so a hand-typed version never keeps a URL and a
// digest that belong to a different release.
function onReleaseVersionInput(picker: ReleasePicker, form: CoreFormFields, node: NodeView | null) {
  picker.selected = null; picker.digestSource = '';
  form.sha256 = '';
  form.url = deriveAssetURL(form.component, picker.source, form.version, node);
}

function onAddCoreVersionInput() {
  onReleaseVersionInput(addCorePicker, addCoreForm, coreManagementTargetNode.value);
}

function setAddCoreSource(source: CoreSource) {
  if (addCorePicker.source === source) return;
  addCorePicker.source = source;
  addCoreForm.version = ''; addCoreForm.url = ''; addCoreForm.sha256 = '';
  void loadCoreReleases(addCorePicker, addCoreForm, coreManagementTargetNode.value);
}

async function selectAddCoreComp(comp: CoreName) {
  addCoreForm.component = comp;
  addCoreForm.version = ''; addCoreForm.url = ''; addCoreForm.sha256 = '';
  addCorePicker.source = defaultCoreSource(comp, coreManagementTargetNode.value);
  await loadCoreReleases(addCorePicker, addCoreForm, coreManagementTargetNode.value);
  // GOST has exactly one pinned release; fill it in instead of offering a choice.
  const pinned = addCorePicker.items[0];
  if (comp === 'gost' && addCoreForm.component === 'gost' && pinned) applyCoreRelease(addCorePicker, addCoreForm, pinned, coreManagementTargetNode.value);
}

function onAddCoreCompChange() {
  selectAddCoreComp(addCoreForm.component);
}

async function submitAddNewCore() {
  const node = coreManagementTargetNode.value;
  if (!node) return;

  if (node.status === 'pending') {
    showToast('该服务器等待 Agent 注册，请在终端执行一键命令即可完成安装', 'info');
    return;
  }
  if (!multiCoreSupported(node)) {
    showToast('该节点 Agent 未协商多核心支持（旧版 Agent），只能升级当前上报的核心；请先按「版本与更新 › Agent」升级 Agent。', 'warning');
    return;
  }
  if (isCoreInstalled(addCoreForm.component)) {
    showToast('该内核已经上报为启用，请使用「升级」而不是「添加内核」', 'warning');
    return;
  }

  if (!addCoreForm.version || !addCoreForm.url || !/^[a-f0-9]{64}$/i.test(addCoreForm.sha256)) {
    showToast('请先选择一个版本；没有自动填好的项（地址或 SHA-256 摘要）请从官方发布页核对后手动填写', 'warning');
    return;
  }
  if (needsBaseline(node)) {
    withBaseline([node], () => void submitAddNewCore());
    return;
  }

  addCoreBusy.value = true;
  try {
    requireCoreTarget(node, addCoreForm.component);
    await coreUpdatesAPI.queue({
      nodeId: node.id,
      revision: node.heartbeat!.current_revision!,
      component: addCoreForm.component,
      version: addCoreForm.version,
      url: addCoreForm.url,
      sha256: addCoreForm.sha256
    });

    showToast(`已提交 ${addCoreForm.component} 安装任务，等待 Agent 校验、安装并回报能力；内核在选择列表中可用之前仍需一次能力上报。`);
    await load();
    const fresh = nodes.value.find(item => item.id === node.id);
    if (fresh) { coreManagementTargetNode.value = fresh; void loadCoreFacts(fresh); }
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    addCoreBusy.value = false;
  }
}

function triggerCoreUpgradeFromModal(core: InstalledCoreInfo) {
  if (!coreManagementTargetNode.value) return;
  openUpgradeCoreForComponent(coreManagementTargetNode.value, core.component);
}

// ========================================================
// CORE UPGRADE MODAL (升级内核)
// ========================================================
const corePicker = newReleasePicker();

// The server the upgrade form shows a URL for: the single target, or in a batch
// the first server that runs the component. A batch submits each server's own
// archive (pickAsset) unless the Owner edited the form by hand.
const coreDisplayNode = computed<NodeView | null>(() => coreDialogTargetNode.value ?? coreBatchTargets.value[0] ?? null);
const coreBatchArchitectures = computed(() => new Set(coreBatchTargets.value.map(node => `${nodeArchitecture(node)}${nodeUsesMusl(node) ? '/musl' : ''}`)).size);
// The qui build is offered only when every server the form targets accepts it.
const coreQuiAllowed = computed(() => {
  const targets = coreDialogTargetNode.value ? [coreDialogTargetNode.value] : coreBatchTargets.value;
  return targets.length > 0 && targets.every(node => quiSourceAllowed(node));
});

function selectCoreComponent(comp: CoreName) {
  coreForm.component = comp;
  coreForm.version = ''; coreForm.url = ''; coreForm.sha256 = '';
  const source = defaultCoreSource(comp, coreDisplayNode.value);
  corePicker.source = source === 'qui' && !coreQuiAllowed.value ? 'official' : source;
  void loadCoreReleases(corePicker, coreForm, coreDisplayNode.value);
}

function setCoreSource(source: CoreSource) {
  if (corePicker.source === source) return;
  corePicker.source = source;
  coreForm.version = ''; coreForm.url = ''; coreForm.sha256 = '';
  void loadCoreReleases(corePicker, coreForm, coreDisplayNode.value);
}

const coreDialogVisible = ref(false);
const coreDialogTargetNode = ref<NodeView | null>(null);
const coreBusy = ref(false);
// Batch upgrades run per component: only servers that already run it are queued,
// so "全部升级" can never install a core the Owner did not choose to install.
const coreBatchComponentLocked = ref(false);
const coreBatchComponents = computed(() => {
  const counts = new Map<CoreName, number>();
  for (const node of eligibleOnlineNodes.value) {
    for (const core of getNodeInstalledCores(node)) counts.set(core.component, (counts.get(core.component) || 0) + 1);
  }
  return [...counts.entries()].map(([name, count]) => ({ name, count }));
});
const coreBatchTargets = computed(() => eligibleOnlineNodes.value.filter(node =>
  getNodeInstalledCores(node).some(core => core.component === coreForm.component)));

// Pending node enrollment state for 1-click install command
const pendingEnrollment = ref<Enrollment | null>(null);
const pendingEnrollmentLoading = ref(false);
const pendingWebUpgrade = ref(false);
// The install command defaults to 系统设置 → 节点安装连接地址 when the Owner saved
// one, otherwise to the address this browser reached the panel on.
const installEndpointHost = ref(browserInstallEndpoint().host);
const installEndpointPort = ref(browserInstallEndpoint().port);
// '' keeps the panel address and null keeps the port the panel listens on; both
// are overridden only by the saved 节点安装连接地址 setting.
const installEndpointGrpcHost = ref('');
const installEndpointGrpcPort = ref<number | null>(null);
const installEndpointFromSettings = ref(false);

const pendingCores = ref<CoreName[]>(['xray']);
const pendingCoreError = computed(() => validateInstallCores(pendingCores.value).error || '');
const pendingEnrollmentCommand = computed(() => {
  const item = pendingEnrollment.value;
  if (!item || !item.install?.version || !item.install?.caSha256 || pendingCoreError.value) return '';
  const host = installEndpointHost.value;
  const port = installEndpointPort.value;
  return installCommand({
    version: item.install.version,
    host,
    httpPort: port,
    grpcHost: installEndpointGrpcHost.value,
    grpcPort: installEndpointGrpcPort.value ?? item.install.grpcPort,
    caSha256: item.install.caSha256,
    token: item.enrollmentToken,
    cores: sortInstallCores(pendingCores.value),
    webUpgrade: pendingWebUpgrade.value
  });
});

const coreDialogTitle = computed(() => {
  if (!coreDialogTargetNode.value) return `全部升级代理内核 - ${coreForm.component}`;
  return `升级代理内核 - ${coreDialogTargetNode.value.name}`;
});

const coreDialogDescription = computed(() => {
  if (!coreDialogTargetNode.value) {
    return `按内核分别升级：将对 ${coreBatchTargets.value.length} 台已运行 ${coreForm.component} 的在线服务器下发同一个版本，每台按自身架构使用对应文件与摘要。`;
  }
  return '选择目标版本，官方下载地址与摘要会自动填写。Agent 将进行 SHA-256 校验，切换可能中断连接，结果以节点回执为准。';
});

const coreDialogConfirmText = computed(() => {
  return '确认升级';
});

// Nothing about cores is kept in browser storage: the form starts empty and is
// filled from the release the Owner picks (or typed by hand).
const coreForm = reactive({
  component: 'xray' as CoreName,
  version: '',
  url: '',
  sha256: ''
});

function onCoreComponentChange() {
  selectCoreComponent(coreForm.component);
}

function onCoreVersionInput() {
  onReleaseVersionInput(corePicker, coreForm, coreDisplayNode.value);
}

// Open Core Upgrade Modal for a specific node (defaults to its primary core)
function openUpgradeCore(node: NodeView) {
  const installed = getNodeInstalledCores(node);
  if (!installed.length) {
    openCoreManagementModal(node);
    return;
  }
  const primary = installed.find(core => core.primary) || installed[0];
  openUpgradeCoreForComponent(node, primary.component);
}

function openUpgradeCoreForComponent(node: NodeView, comp: CoreName) {
  if (!isOwner.value) return;
  coreForm.url = ''; coreForm.sha256 = '';
  coreBatchComponentLocked.value = true;
  coreDialogTargetNode.value = node;
  selectCoreComponent(comp);
  coreDialogVisible.value = true;
}

// Single node upgrade trigger
function handleUpgradeSingle(node: NodeView) {
  if (!isOwner.value) return;
  const reason = upgradeUnavailableReason(node);
  if (reason) {
    showToast(`服务器「${node.name}」：${reason}`, 'warning');
    return;
  }
  withBaseline([node], () => {
    if (props.kind === 'agent') {
      agentUpgradeTargetNode.value = node;
      agentUpgradeDialogVisible.value = true;
      void loadAgentReleases();
    } else {
      openUpgradeCore(node);
    }
  });
}

// Batch upgrade trigger (from the top-right "全部升级"). Core upgrades are per
// component: the dialog picks one component and only servers that already run it
// are queued.
async function upgradeAll() {
  if (!isOwner.value) return;
  try { batchNodes.value = await readAllNodes(); await loadUpgradeContext(); }
  catch (e) { showToast((e as Error).message, 'error'); return; }
  withBaseline(batchNodes.value.filter(node => !upgradeUnavailableReason(node) && (props.kind !== 'agent' || node.agentUpdateSupported)), openBatchUpgrade);
}
function openBatchUpgrade() {
  const eligible = eligibleOnlineNodes.value;
  if (!eligible.length) {
    showToast('当前暂无满足升级条件的在线服务器 (需在线且已上报配置版本)', 'warning');
    return;
  }

  if (props.kind === 'agent') {
    agentUpgradeTargetNode.value = null;
    agentUpgradeDialogVisible.value = true;
    void loadAgentReleases();
  } else {
    if (!coreBatchComponents.value.length) {
      showToast('尚未有服务器上报已启用的内核，无法批量升级', 'warning');
      return;
    }
    coreForm.url = ''; coreForm.sha256 = '';
    coreBatchComponentLocked.value = false;
    coreDialogTargetNode.value = null; // null means "all servers running the component"
    coreForm.component = coreBatchComponents.value[0].name;
    selectCoreComponent(coreForm.component);
    coreDialogVisible.value = true;
  }
}

// Per-component batch entry: upgrade only the servers that run this component.
async function upgradeAllComponent(component: CoreName) {
  if (!isOwner.value) return;
  try { batchNodes.value = await readAllNodes(); await loadUpgradeContext(); }
  catch (e) { showToast((e as Error).message, 'error'); return; }
  const runs = (node: NodeView) => getNodeInstalledCores(node).some(core => core.component === component);
  withBaseline(batchNodes.value.filter(node => !upgradeUnavailableReason(node) && runs(node)), () => openComponentBatchUpgrade(component, runs));
}
function openComponentBatchUpgrade(component: CoreName, runs: (node: NodeView) => boolean) {
  const targeted = eligibleOnlineNodes.value.filter(runs);
  if (!targeted.length) {
    showToast(`当前没有在线服务器运行 ${component}`, 'warning');
    return;
  }
  coreForm.url = ''; coreForm.sha256 = '';
  coreBatchComponentLocked.value = true;
  coreDialogTargetNode.value = null;
  coreForm.component = component;
  selectCoreComponent(component);
  coreDialogVisible.value = true;
}

// Execute single Agent upgrade
function prepareManualAgent() {
  const node = manualAgentNode.value, release = agentReleases.value.find(r => r.id === manualReleaseId.value);
  if (!node || !release || !node.heartbeat?.current_revision) { showToast('请选择已签名版本', 'warning'); return; }
  singleAgentConfirmTarget.value = { node, release, revision: node.heartbeat.current_revision };
  manualAgentNode.value = null; singleAgentConfirmVisible.value = true;
}
async function executeSingleAgentUpgrade() {
  if (!isOwner.value) return;
  const target = singleAgentConfirmTarget.value;
  singleAgentConfirmTarget.value = null;
  if (!target) return;

  try {
    await agentUpdatesAPI.queue(target.node.id, target.release.id, target.revision);
    showToast(`已向「${target.node.name}」下发 Agent 升级任务 (v${target.release.signed.manifest.version})`);
    void load();
    void loadUpgradeContext();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// Execute batch Agent upgrade
async function executeBatchAgentUpgrade() {
  if (!isOwner.value) return;
  batchAgentConfirmVisible.value = false;
  const eligible = eligibleOnlineNodes.value;
  if (!eligible.length || !latestAgentRelease.value) return;

  let success = 0;
  let failed = 0;
  for (const node of eligible) {
    const rel = findLatestAgentReleaseForNode(node, agentReleases.value);
    try {
      if (!rel) throw new Error('无完全匹配的平台版本，请逐台手动选择');
      await agentUpdatesAPI.queue(node.id, rel.id, node.heartbeat!.current_revision!);
      success++;
    } catch (e) {
      showToast(node.name + '：' + (e as Error).message, 'error');
      failed++;
    }
  }

  if (success > 0) {
    showToast(`已成功向 ${success} 台在线服务器下发 Agent 升级任务${failed ? ` (${failed} 台失败)` : ''}`);
  } else {
    showToast(`批量升级任务下发失败，请检查各节点状态与版本配置`, 'error');
  }
  void load();
  void loadUpgradeContext();
}

// Submit Core upgrade action (single node, or per-component batch)
async function submitCoreAction() {
  if (!isOwner.value) return;
  if (!coreForm.version || !coreForm.url || !coreForm.sha256) {
    showToast('请先选择一个版本；没有自动填好的项（地址或 SHA-256 摘要）请从官方发布页核对后手动填写', 'warning');
    return;
  }
  if (!/^[a-f0-9]{64}$/i.test(coreForm.sha256)) {
    showToast('SHA-256 校验和必须为 64 位十六进制字符', 'warning');
    return;
  }

  coreBusy.value = true;
  try {
    if (coreDialogTargetNode.value) {
      const node = coreDialogTargetNode.value;
      requireCoreTarget(node, coreForm.component);
      await coreUpdatesAPI.queue({
        nodeId: node.id,
        revision: node.heartbeat!.current_revision!,
        component: coreForm.component,
        version: coreForm.version,
        url: coreForm.url,
        sha256: coreForm.sha256
      });

      // Installed state changes only after the Agent reports capabilities.
      showToast(`已向「${node.name}」下发内核升级任务 (${coreForm.component} ${coreForm.version})，结果以节点回执为准。`);
    } else {
      const targets = coreBatchTargets.value;
      if (!targets.length) {
        showToast(`当前没有运行 ${coreForm.component} 的在线服务器`, 'warning');
        return;
      }

      // A picked release resolves each server's own archive and digest; a form
      // the Owner edited by hand is sent unchanged to every server, as before.
      const perNode = !releaseFormManual(corePicker, coreForm) ? corePicker.selected : null;
      let success = 0;
      let failed = 0;
      for (const node of targets) {
        try {
          requireCoreTarget(node, coreForm.component);
          let url = coreForm.url, sha256 = coreForm.sha256;
          if (perNode) {
            const asset = pickAsset(perNode, node);
            if (!asset || !asset.sha256) throw new Error(`v${perNode.version} 没有适用于这台服务器（${nodeArchitecture(node) || '架构未知'}${nodeUsesMusl(node) ? '，musl' : ''}）且带官方摘要的文件，已跳过`);
            url = asset.url; sha256 = asset.sha256;
          }
          await coreUpdatesAPI.queue({
            nodeId: node.id,
            revision: node.heartbeat!.current_revision!,
            component: coreForm.component,
            version: coreForm.version,
            url,
            sha256
          });
          success++;
        } catch (e) {
          showToast(node.name + '：' + (e as Error).message, 'error');
          failed++;
        }
      }

      showToast(`已向 ${success} 台运行 ${coreForm.component} 的服务器下发升级任务${failed ? `（${failed} 台跳过/失败）` : ''}，结果以各节点回执为准。`);
    }

    coreDialogVisible.value = false;
    void load();
    void loadUpgradeContext();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    coreBusy.value = false;
  }
}

defineExpose({
  upgradeAll,
  upgradeAllComponent,
  handleUpgradeSingle,
  openCoreManagementModal,
  openUpgradeCore
});

let generation = 0;
let refreshing = false;
function nodeStatusChanged() {
  if (!loading.value) void load(true);
}
async function refreshNodes() {
  if (loading.value || refreshing || document.visibilityState !== 'visible') return;
  refreshing = true;
  const current = generation;
  try {
    const result = await nodeAPI.list(page.value, keyword.value);
    if (current === generation) { nodes.value = result.items; total.value = result.total; error.value = ''; }
  } catch (e) { if (current === generation) error.value = (e as Error).message; }
  finally { refreshing = false; }
}
let tasksReading = false, tasksGeneration = 0;
async function refreshTasks() {
  if (!isOwner.value || props.kind !== 'core' || tasksReading || document.visibilityState !== 'visible') return;
  tasksReading = true;
  const current = tasksGeneration;
  try { const tasks = await readAllCoreTasks(); if (current === tasksGeneration) coreTasks.value = tasks; }
  catch (e) { if (current === tasksGeneration) error.value = (e as Error).message; }
  finally { tasksReading = false; }
}
function visibilityChanged() {
  if (document.visibilityState === 'visible') { void nodeStatusChanged(); void refreshTasks(); }
}
let refreshTimer: ReturnType<typeof setInterval>;
const nodeStatusThrottled = throttleTrailing(() => nodeStatusChanged(), 5000);
async function load(silent = false) {
  if (silent) return refreshNodes();
  const current = ++generation;
  loading.value = true;
  error.value = '';
  try {
    const result = await nodeAPI.list(page.value, keyword.value);
    if (current === generation) {
      nodes.value = result.items;
      total.value = result.total;
    }
    // The per-(node, component) task rows make the "安装中 / 升级中" label possible
    // in the list and cards; they are read from the backend, never cached locally.
    if (current === generation) void refreshTasks();
  } catch (e) {
    if (current === generation) error.value = (e as Error).message;
  } finally {
    if (current === generation) loading.value = false;
  }
}

function search() {
  page.value = 1;
  void load();
}

function changePage(value: number) {
  page.value = value;
  void load();
}

const filteredNodes = computed(() => {
  return nodes.value.filter(n => {
    if (statusFilter.value !== 'all' && n.status !== statusFilter.value) {
      return false;
    }
    return true;
  });
});

function coreVersion(node: NodeView) {
  return coreSummaryLabel(node);
}

// Per-core health: a node-level "healthy" flag is the AND of the cores, so the
// per-core rows are what the Owner needs to see. Unknown stays "尚未上报".
function coreHealth(node: NodeView) {
  const cores = getNodeInstalledCores(node);
  if (!cores.length) return '尚未上报';
  const unhealthy = cores.filter(core => core.healthy === false);
  if (unhealthy.length) return `${unhealthy.map(core => core.name).join('、')} 异常`;
  if (cores.every(core => core.healthy === true)) return cores.length > 1 ? '全部内核正常' : '最近上报正常';
  if (cores.some(core => core.healthy === true)) return '部分内核正常';
  return '尚未上报';
}

function coreHealthTone(node: NodeView): 'ok' | 'bad' | 'unknown' {
  const cores = getNodeInstalledCores(node);
  if (cores.some(core => core.healthy === false)) return 'bad';
  if (cores.length && cores.every(core => core.healthy === true)) return 'ok';
  return 'unknown';
}

function getCountryInfo(node: NodeView): CountryInfo {
  return resolveCountryInfo({ region: getNodeMeta(node.id).region, countryCode: node.countryCode, name: node.name, hostname: node.hostname });
}

function formatOS(os?: string): string {
  if (!os) return 'Linux';
  const matchDebian = os.match(/Debian[^\d]*(\d+)/i);
  if (matchDebian) return `Debian ${matchDebian[1]}`;
  const matchUbuntu = os.match(/Ubuntu[^\d]*([\d.]+)/i);
  if (matchUbuntu) return `Ubuntu ${matchUbuntu[1]}`;
  const matchCentOS = os.match(/CentOS[^\d]*([\d.]+)/i);
  if (matchCentOS) return `CentOS ${matchCentOS[1]}`;
  const matchAlpine = os.match(/Alpine[^\d]*([\d.]+)/i);
  if (matchAlpine) return `Alpine ${matchAlpine[1]}`;
  if (os.length > 14) return os.slice(0, 12) + '…';
  return os;
}

function getNodeStatusTone(node: NodeView): 'active' | 'error' | 'warning' {
  if (node.status === 'online') return 'active';
  if (node.status === 'offline' || node.status === 'error') return 'error';
  return 'warning';
}

function statusCapsuleClass(node: NodeView): string {
  if (node.status === 'online') return 'bg-[#E8F8F0] text-[#12B76A]';
  if (node.status === 'offline') return 'bg-neutral-100 text-neutral-500';
  if (node.status === 'error') return 'bg-red-50 text-red-600';
  if (node.status === 'degraded') return 'bg-amber-50 text-amber-700';
  if (node.status === 'applying' || node.status === 'rollback') return 'bg-blue-50 text-ios-blue';
  if (node.status === 'pending') return 'bg-neutral-100 text-neutral-500';
  if (node.status === 'disabled') return 'bg-neutral-100 text-neutral-500';
  return 'bg-neutral-100 text-neutral-500';
}

function statusDotClass(node: NodeView): string {
  if (node.status === 'online') return 'bg-[#12B76A]';
  if (node.status === 'offline') return 'bg-neutral-400';
  if (node.status === 'error') return 'bg-red-500';
  if (node.status === 'degraded') return 'bg-amber-500';
  if (node.status === 'applying' || node.status === 'rollback') return 'bg-ios-blue';
  if (node.status === 'pending') return 'bg-neutral-400';
  if (node.status === 'disabled') return 'bg-neutral-400';
  return 'bg-neutral-400';
}

function statusCapsuleText(node: NodeView): string {
  if (node.status === 'online') return '在线';
  if (node.status === 'degraded') return '核心异常';
  if (node.status === 'applying') return '发布中';
  if (node.status === 'rollback') return '回滚中';
  if (node.status === 'error') return '异常';
  if (node.status === 'offline') return '离线';
  if (node.status === 'pending') return '等待登记';
  if (node.status === 'disabled') return '已停用';
  return node.status;
}

onMounted(() => {
  refreshTimer = setInterval(visibilityChanged, 60000);
  document.addEventListener('visibilitychange', visibilityChanged);
  void load();
  void loadUpgradeContext();
  window.addEventListener('proxy-node-status', nodeStatusThrottled);
});

onUnmounted(() => {
  tasksGeneration++;
  clearInterval(refreshTimer);
  document.removeEventListener('visibilitychange', visibilityChanged);
  generation++;
  nodeStatusThrottled.cancel();
  window.removeEventListener('proxy-node-status', nodeStatusThrottled);
});
</script>
