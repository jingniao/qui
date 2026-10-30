<template>
  <div class="space-y-5">
    <PageHeader
      v-if="!embedded"
      :title="kind === 'agent' ? 'Agent 探针管理' : '代理核心版本'"
      parent="服务器"
      :description="kind === 'agent' ? '统一管理与监控各服务器上部署的 Agent 探针运行状态、心跳上报与自动更新任务。' : '查看 Agent 上报的核心与版本，仅支持更新当前核心；多内核即将支持。'"
    >
      <template #actions>
        <button v-if="isOwner"
          type="button"
          @click="upgradeAll"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)] text-xs py-2 px-3.5"
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
            <span class="text-[10px] px-2 py-0.2 rounded-full bg-purple-100 text-purple-800 font-semibold">多核心协同共存</span>
          </div>
          <p class="text-[11px] text-ios-secondary mt-0.5 leading-relaxed">
            支持 <strong>Xray-core</strong>（成熟稳定 · VLESS / VMess / Trojan / Shadowsocks / WireGuard）与 <strong>sing-box</strong>（新一代轻量 · Hysteria2 / TUIC / 自适应路由），当前每台服务器仅支持更新已上报的单一核心；多内核即将支持。
          </p>
        </div>
      </div>
      <div class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          @click="showCoreCatalogModal = true"
          class="ios-btn-secondary !text-xs !py-1.5 !px-3 inline-flex items-center gap-1.5 bg-white/80"
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
            class="ios-segmented-item whitespace-nowrap text-xs py-1.5 px-3.5"
            :class="{ active: currentTab === 'agents' }"
            @click="$emit('change-tab', 'agents')"
          >
            Agent
          </button>
          <button
            role="tab"
            :aria-selected="currentTab === 'cores'"
            class="ios-segmented-item whitespace-nowrap text-xs py-1.5 px-3.5"
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
              class="ios-search-input !py-1.5 !text-xs"
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
          <select v-model="statusFilter" @change="search" class="ios-filter-select !w-auto !py-1.5 !text-xs">
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
          <button class="ios-search-btn shrink-0 !py-1.5 !text-xs">查询</button>

          <!-- Refresh Button -->
          <button
            type="button"
            @click="load"
            class="ios-search-btn shrink-0 !py-1.5 !text-xs"
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
    <ErrorState v-if="error" :message="error" @retry="load" />
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
                :title="getCountryInfo(node).name"
              >
                {{ getCountryInfo(node).flag }}
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <router-link :to="`/nodes/${node.id}`" class="font-semibold text-ios-blue hover:underline text-[15px] truncate">
                    {{ node.name }}
                  </router-link>
                  <span v-if="node.deletedAt" class="px-1.5 py-0.2 rounded text-[10px] font-bold tracking-wide uppercase shrink-0 bg-black/[0.05] text-neutral-400">
                    已删除
                  </span>
                </div>
                <p class="text-[13px] text-ios-secondary tabular-nums truncate mt-0.5">
                  {{ node.publicIp || node.hostname || '等待 Agent 注册' }}
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
                :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500 font-normal')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
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
                >
                  {{ core.name }} {{ core.version }}
                </span>
              </div>
              <span v-else class="text-xs px-2 py-0.5 rounded-md text-amber-700 bg-amber-50 font-normal">
                未安装内核
              </span>
            </td>
            <td>
              <span
                class="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-xs font-semibold"
                :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600' : 'bg-neutral-100 text-neutral-500 font-normal')"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                <span>{{ coreHealth(node) }}</span>
              </span>
            </td>
            <td>
              <div v-if="getNodeAllProtocols(node).length" class="flex flex-wrap gap-1">
                <span
                  v-for="proto in getNodeAllProtocols(node)"
                  :key="proto"
                  class="rounded bg-black/[0.04] px-1.5 py-0.2 text-[10px] font-bold uppercase text-neutral-700"
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
              <button v-if="isOwner"
                type="button"
                @click="handleUpgradeSingle(node)"
                class="ios-action-btn-secondary"
                title="升级 Agent 到最新版本"
              >
                升级
              </button>
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
                  type="button"
                  @click="openUpgradeCore(node)"
                  class="ios-action-btn-secondary"
                  title="升级代理内核版本"
                >
                  升级
                </button>
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
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in filteredNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <div class="space-y-3.5">
          <!-- Row 1: Flag · Name · OS & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2.5 min-w-0">
              <div
                class="w-7 h-7 rounded-lg bg-blue-50/90 border border-blue-100 text-xs font-bold font-mono text-ios-blue flex items-center justify-center shrink-0 shadow-2xs select-none"
                :title="getCountryInfo(node).name"
              >
                {{ getCountryInfo(node).flag }}
              </div>
              <router-link
                :to="`/nodes/${node.id}`"
                class="font-bold text-[16px] text-neutral-900 truncate tracking-tight hover:text-ios-blue transition-colors"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span
                class="rounded bg-black/[0.04] px-1.5 py-0.5 text-[10px] font-bold uppercase text-neutral-600 shrink-0 border border-black/[0.04] truncate max-w-[90px]"
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
          <div class="text-[12px] text-neutral-400 tracking-tight flex items-center justify-between pt-0.5">
            <span class="flex items-center gap-1.5 text-neutral-600 truncate tabular-nums" :title="node.publicIp">
              <svg class="w-3.5 h-3.5 text-neutral-400 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9" />
              </svg>
              <span class="truncate">{{ node.publicIp || node.hostname || '未分配 IP' }}</span>
            </span>
            <span class="shrink-0 text-neutral-400 flex items-center gap-1 text-[11px] tabular-nums" :title="'最近心跳: ' + formatTime(node.lastHeartbeat)">
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
                  :class="node.agentVersion ? 'font-bold text-ios-blue bg-blue-50/90 border border-blue-100/70' : 'font-medium text-neutral-400 bg-neutral-100'"
                >
                  {{ node.agentVersion || '等待上报' }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">协同代理核心</span>
                <span
                  class="tabular-nums text-xs truncate max-w-[150px] px-2 py-0.5 rounded-lg"
                  :class="node.capabilities?.core_name ? 'font-semibold text-purple-700 bg-purple-50/90 border border-purple-100/70' : 'text-neutral-400 font-normal bg-neutral-100'"
                  :title="coreVersion(node)"
                >
                  {{ coreVersion(node) }}
                </span>
              </div>
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-500 font-medium">核心健康状况</span>
                <span
                  class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[11px] font-semibold"
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
                      class="tabular-nums px-2 py-0.5 rounded-lg text-xs font-bold"
                      :class="c.component === 'xray' ? 'text-ios-blue bg-blue-50/90 border border-blue-200/70' : 'text-purple-700 bg-purple-50/90 border border-purple-200/70'"
                    >
                      {{ c.name }} {{ c.version }}
                    </span>
                  </div>
                </div>

                <div class="flex items-center justify-between text-xs">
                  <span class="text-neutral-500 font-medium">健康运行状态</span>
                  <span
                    class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-[11px] font-semibold"
                    :class="node.heartbeat?.core_healthy === false ? 'bg-red-50 text-red-600 border border-red-100/70' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-50 text-emerald-600 border border-emerald-100/70' : 'bg-neutral-100 text-neutral-400 font-normal')"
                  >
                    <span class="w-1.5 h-1.5 rounded-full" :class="node.heartbeat?.core_healthy === false ? 'bg-red-500' : (node.heartbeat?.core_healthy === true ? 'bg-emerald-500' : 'bg-neutral-400')"></span>
                    <span>{{ coreHealth(node) }}</span>
                  </span>
                </div>

                <div class="space-y-1 pt-0.5">
                  <div class="text-[11px] text-neutral-400 font-medium">支持网络协议:</div>
                  <div v-if="getNodeAllProtocols(node).length" class="flex flex-wrap gap-1">
                    <span
                      v-for="proto in getNodeAllProtocols(node)"
                      :key="proto"
                      class="rounded bg-black/[0.04] px-1.5 py-0.2 text-[10px] font-bold uppercase text-neutral-700"
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
                <div class="p-2 rounded-xl bg-amber-50/50 border border-amber-100 text-amber-900 text-[11px] leading-relaxed">
                  当前服务器尚未部署代理内核。支持一键安装 <strong>Xray-core</strong> 与 <strong>sing-box</strong> 核心协同运行以开启转发。
                </div>
              </template>
            </template>
          </div>
        </div>

        <!-- Row 4: Action Buttons (Apple HIG Precision) -->
        <template v-if="kind === 'agent'">
          <div class="pt-2 border-t border-black/[0.04] grid grid-cols-2 gap-2.5">
            <button v-if="isOwner"
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
              type="button"
              @click="openUpgradeCore(node)"
              class="flex-1 py-2 px-3 rounded-xl bg-ios-blue/[0.08] text-ios-blue hover:bg-ios-blue/[0.15] font-semibold text-xs text-center transition-all flex items-center justify-center gap-1.5"
              title="升级代理内核版本"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
              </svg>
              <span>升级</span>
            </button>

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

    <p v-if="isOwner && !releasesConfigured" class="text-sm text-ios-orange">中央尚未配置发布公钥</p>
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

    <!-- ============================================================ -->
    <!-- Dialog 3: 内核管理弹窗 (Core Management Modal: 添加内核 & 删除内核) -->
    <!-- ============================================================ -->
    <AppDialog
      v-model="coreManagementModalVisible"
      :title="`内核管理 - ${coreManagementTargetNode?.name || ''}`"
      description="当前仅可更新节点上报的核心；多内核即将支持。"
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
                <span class="text-[10px] px-2 py-0.5 rounded-md bg-neutral-100 text-neutral-600 font-medium">
                  {{ formatOS(coreManagementTargetNode.os) }} ({{ detectArch(coreManagementTargetNode) || '—' }})
                </span>
              </div>
              <p class="text-[11px] text-neutral-500 tabular-nums mt-0.5">
                {{ coreManagementTargetNode.publicIp || coreManagementTargetNode.hostname || '未分配公网 IP' }}
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
            <h4 class="font-bold text-sm text-neutral-900 flex items-center gap-2">
              <svg class="w-4 h-4 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
              </svg>
              <span>已安装内核</span>
              <span class="text-xs font-normal text-neutral-400">({{ manageInstalledCores.length }})</span>
            </h4>
            <span class="text-[11px] text-neutral-400">多内核即将支持 · 当前仅可更新已上报核心</span>
          </div>

          <!-- Empty State when no cores installed -->
          <div v-if="!manageInstalledCores.length" class="p-5 rounded-2xl bg-neutral-50/80 border border-dashed border-neutral-200 text-center space-y-1">
            <div class="text-neutral-500 font-medium text-xs">当前服务器尚未安装任何代理内核</div>
            <p class="text-[11px] text-neutral-400">请在下方「添加内核」区域选择需要的代理核心进行部署。</p>
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
                <div class="space-y-1">
                  <div class="flex items-center gap-2">
                    <span class="font-bold text-sm text-neutral-900">{{ core.name }}</span>
                    <span class="tabular-nums px-2 py-0.2 rounded-md bg-neutral-100 text-neutral-700 font-bold text-xs">
                      v{{ core.version }}
                    </span>
                    <span
                      class="inline-flex items-center gap-1 px-2 py-0.2 rounded-full text-[10px] font-semibold"
                      :class="core.healthy ? 'bg-ios-green/10 text-ios-green' : core.healthy === false ? 'bg-ios-red/10 text-ios-red' : 'bg-ios-fill text-ios-tertiary'"
                    >
                      <span class="w-1.5 h-1.5 rounded-full" :class="core.healthy ? 'bg-ios-green' : core.healthy === false ? 'bg-ios-red' : 'bg-ios-tertiary'"></span>
                      <span>{{ core.healthy === undefined ? '—' : core.healthy ? '运行正常' : '异常' }}</span>
                    </span>
                  </div>
                  <div class="flex flex-wrap items-center gap-1 pt-0.5">
                    <span class="text-[10px] text-neutral-400 mr-0.5">协议:</span>
                    <span
                      v-for="p in core.protocols"
                      :key="p"
                      class="px-1.5 py-0.2 rounded bg-black/[0.04] text-neutral-700 font-bold text-[9px] uppercase"
                    >
                      {{ p }}
                    </span>
                  </div>
                </div>
              </div>

              <!-- Actions for this specific core: [升级] & [删除内核] -->
              <div class="flex items-center gap-2 shrink-0 self-end sm:self-center">
                <button
                  type="button"
                  @click="triggerCoreUpgradeFromModal(core)"
                  class="py-1.5 px-3 rounded-xl bg-ios-blue/[0.08] text-ios-blue hover:bg-ios-blue/[0.15] font-semibold text-xs transition-all flex items-center gap-1"
                  title="升级该内核版本"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
                  </svg>
                  <span>升级</span>
                </button>

                <!-- Delete Kernel Action Button -->
              </div>
            </div>
          </div>
        </div>

        <!-- SECTION 2: 添加内核 (支持多种内核兼容并存) -->
        <div class="pt-4 border-t border-black/[0.06] space-y-3.5">
          <div class="flex items-center justify-between">
            <h4 class="font-bold text-sm text-neutral-900 flex items-center gap-2">
              <svg class="w-4 h-4 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6v6m0 0v6m0-6h6m-6 0H6" />
              </svg>
              <span>添加内核</span>
            </h4>
            <span class="text-[11px] text-emerald-700 bg-emerald-50 px-2 py-0.5 rounded-full font-medium">
              多内核即将支持
            </span>
          </div>

          <!-- Pending Node Guidance if node is pending -->
          <div v-if="coreManagementTargetNode?.status === 'pending'" class="space-y-3">
            <div class="p-3 rounded-xl bg-amber-50 border border-amber-200/80 text-amber-900 space-y-1">
              <div class="font-bold flex items-center gap-1.5">
                <svg class="w-4 h-4 text-amber-700" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>服务器尚未完成 Agent 注册</span>
              </div>
              <p class="text-[11px] leading-relaxed">
                在终端运行一键部署命令，注册时安装单一核心。后续多内核即将支持。
              </p>
            </div>

            <!-- Core Selection Cards -->
            <div class="grid grid-cols-2 gap-2.5">
              <button v-if="coreManagementTargetNode?.capabilities?.core_name === 'xray'" type="button" @click="selectAddCoreComp('xray')"
                class="p-3 rounded-xl border text-left transition-all space-y-1.5"
                :class="addCoreForm.component === 'xray' ? 'border-ios-blue bg-blue-50/50 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
              >
                <div class="flex items-center justify-between">
                  <span class="font-bold text-sm text-ios-label">Xray-core</span>
                  <span class="text-[10px] px-1.5 py-0.2 rounded bg-blue-100 text-ios-blue font-semibold">推荐</span>
                </div>
                <p class="text-[11px] text-ios-secondary leading-snug">成熟稳定 · VLESS / VMess / Trojan / WireGuard</p>
              </button>

              <button v-if="coreManagementTargetNode?.capabilities?.core_name === 'sing-box'" type="button" @click="selectAddCoreComp('sing-box')"
                class="p-3 rounded-xl border text-left transition-all space-y-1.5"
                :class="addCoreForm.component === 'sing-box' ? 'border-ios-blue bg-blue-50/50 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
              >
                <div class="flex items-center justify-between">
                  <span class="font-bold text-sm text-ios-label">sing-box</span>
                  <span class="text-[10px] px-1.5 py-0.2 rounded bg-purple-100 text-purple-700 font-semibold">轻量新代</span>
                </div>
                <p class="text-[11px] text-ios-secondary leading-snug">现代轻量 · Hysteria2 / TUIC / 自适应路由</p>
              </button>
            </div>

            <div class="space-y-1.5">
              <div class="flex items-center justify-between text-xs">
                <span class="text-neutral-700 font-bold">一键安装命令</span>
                <span class="text-[11px] text-ios-tertiary">指定默认核心 --core {{ addCoreForm.component }}</span>
              </div>
              <div v-if="pendingEnrollmentLoading" class="p-4 text-center text-ios-tertiary">正在生成注册命令…</div>
              <CodePreview v-else-if="pendingEnrollmentCommand" :code="pendingEnrollmentCommand" title="一键安装命令" />
            </div>
          </div>

          <!-- Online Node: Add New Core Interactive Form -->
          <div v-else class="space-y-3.5 bg-neutral-50/70 p-4 rounded-2xl border border-black/[0.04]">
            <!-- Core Choice Cards -->
            <div>
              <label class="block text-neutral-700 font-bold mb-2">选择要添加的内核平台</label>
              <div class="grid grid-cols-2 gap-2.5">
                <button v-if="coreManagementTargetNode?.capabilities?.core_name === 'xray'" type="button" @click="selectAddCoreComp('xray')"
                  class="p-3 rounded-xl border text-left transition-all space-y-1.5 relative"
                  :class="addCoreForm.component === 'xray' ? 'border-ios-blue bg-blue-50/60 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
                >
                  <div class="flex items-center justify-between">
                    <span class="font-bold text-sm text-ios-label">Xray-core</span>
                    <span
                      class="text-[10px] px-1.5 py-0.2 rounded font-semibold"
                      :class="isCoreInstalled('xray') ? 'bg-emerald-100 text-emerald-800' : 'bg-blue-100 text-ios-blue'"
                    >
                      {{ isCoreInstalled('xray') ? '已部署 (可重新部署)' : '推荐添加' }}
                    </span>
                  </div>
                  <p class="text-[11px] text-ios-secondary leading-snug">成熟稳定 · VLESS / VMess / Trojan / Shadowsocks / WireGuard</p>
                </button>

                <button v-if="coreManagementTargetNode?.capabilities?.core_name === 'sing-box'" type="button" @click="selectAddCoreComp('sing-box')"
                  class="p-3 rounded-xl border text-left transition-all space-y-1.5 relative"
                  :class="addCoreForm.component === 'sing-box' ? 'border-ios-blue bg-blue-50/60 ring-2 ring-ios-blue/20' : 'border-black/[0.08] hover:border-black/20 bg-white'"
                >
                  <div class="flex items-center justify-between">
                    <span class="font-bold text-sm text-ios-label">sing-box</span>
                    <span
                      class="text-[10px] px-1.5 py-0.2 rounded font-semibold"
                      :class="isCoreInstalled('sing-box') ? 'bg-emerald-100 text-emerald-800' : 'bg-purple-100 text-purple-700'"
                    >
                      {{ isCoreInstalled('sing-box') ? '已部署 (可重新部署)' : '新一代轻量' }}
                    </span>
                  </div>
                  <p class="text-[11px] text-ios-secondary leading-snug">现代轻量 · Hysteria2 / TUIC / 自适应路由分流</p>
                </button>
              </div>
            </div>

            <!-- Version Presets -->
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">版本与 URL 辅助（SHA-256 需自行核验并粘贴）</label>
              <div class="flex flex-wrap gap-2">
                <button
                  v-for="preset in addCorePresets"
                  :key="preset.version"
                  type="button"
                  @click="applyAddCorePreset(preset)"
                  class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                  :class="addCoreForm.version === preset.version ? 'bg-ios-blue text-white border-ios-blue shadow-2xs font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'"
                >
                  {{ preset.label }}
                </button>
              </div>
            </div>

            <!-- Version & Core Architecture -->
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-neutral-600 font-medium mb-1.5">内核平台</label>
                <select v-model="addCoreForm.component" @change="onAddCoreCompChange" class="form-input-base"><option v-for="c in manageInstalledCores" :key="c.component" :value="c.component">{{ c.name }}</option></select>
              </div>
              <div>
                <label class="block text-neutral-600 font-medium mb-1.5">目标版本号</label>
                <input v-model.trim="addCoreForm.version" @input="autoFillAddCoreURL" placeholder="例如: 26.3.27" class="form-input-base w-full !text-xs !py-1.5 tabular-nums font-semibold" />
              </div>
            </div>

            <!-- URL -->
            <div>
              <div class="flex items-center justify-between mb-1.5">
                <label class="text-neutral-600 font-medium">官方发布资产地址 (Asset URL)</label>
                <button type="button" @click="autoFillAddCoreURL" class="text-ios-blue hover:underline text-[11px] font-medium">生成默认地址</button>
              </div>
              <input v-model.trim="addCoreForm.url" class="form-input-base w-full !text-xs !py-1.5 font-mono text-[11px]" />
            </div>

            <!-- SHA-256 -->
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">SHA-256 校验和 (64位十六进制)</label>
              <input v-model.trim="addCoreForm.sha256" placeholder="输入 64 位 SHA-256 摘要" class="form-input-base w-full !text-xs !py-1.5 font-mono text-[11px]" />
            </div>

            <!-- Submit Button: 确认添加内核 -->
            <div class="pt-2 flex justify-end">
              <button
                type="button"
                @click="submitAddNewCore"
                :disabled="addCoreBusy"
                class="ios-btn-primary inline-flex items-center gap-1.5 text-xs py-2 px-4 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
              >
                <svg v-if="addCoreBusy" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                <svg v-else class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                </svg>
                <span>确认添加内核 ({{ addCoreForm.component === 'xray' ? 'Xray-core' : 'sing-box' }})</span>
              </button>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>

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
          <!-- Official Release Presets (Pills) -->
          <div>
            <label class="block text-neutral-600 font-medium mb-1.5">版本与 URL 辅助（SHA-256 需自行核验并粘贴）</label>
            <div class="flex flex-wrap gap-2">
              <button
                v-for="preset in currentCorePresets"
                :key="preset.version"
                type="button"
                @click="applyCorePreset(preset)"
                class="px-2.5 py-1 rounded-lg border text-xs font-medium transition-all"
                :class="coreForm.version === preset.version ? 'bg-ios-blue text-white border-ios-blue shadow-2xs font-semibold' : 'bg-white text-neutral-700 border-black/[0.08] hover:border-black/20'"
              >
                {{ preset.label }}
              </button>
            </div>
          </div>

          <!-- Version & Architecture Inputs -->
          <div class="grid grid-cols-2 gap-3">
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">升级内核平台</label>
              <select v-model="coreForm.component" @change="onCoreComponentChange" class="form-input-base w-full !text-xs !py-1.5">
                <option v-if="!coreDialogTargetNode || coreDialogTargetNode.capabilities?.core_name === 'xray'" value="xray">Xray-core</option>
                <option v-if="!coreDialogTargetNode || coreDialogTargetNode.capabilities?.core_name === 'sing-box'" value="sing-box">sing-box</option>
              </select>
            </div>
            <div>
              <label class="block text-neutral-600 font-medium mb-1.5">目标版本号</label>
              <input v-model.trim="coreForm.version" @input="onCoreVersionInput" placeholder="例如: 26.3.27" class="form-input-base w-full !text-xs !py-1.5 tabular-nums font-semibold" />
            </div>
          </div>

          <div>
            <div class="flex items-center justify-between mb-1.5">
              <label class="text-neutral-600 font-medium">官方发布资产地址 (Asset URL)</label>
              <button type="button" @click="autoFillCoreURL" class="text-ios-blue hover:underline text-[11px] font-medium">生成默认地址</button>
            </div>
            <input v-model.trim="coreForm.url" placeholder="https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip" class="form-input-base w-full !text-xs !py-1.5 font-mono text-[11px]" />
            <p class="text-[10px] text-neutral-400 mt-1">必须为官方 releases 下载地址，节点下载后将比对文件指纹。</p>
          </div>

          <div>
            <label class="block text-neutral-600 font-medium mb-1.5">SHA-256 校验和 (64位十六进制)</label>
            <input v-model.trim="coreForm.sha256" placeholder="输入 64 位 SHA-256 摘要" class="form-input-base w-full !text-xs !py-1.5 font-mono text-[11px]" />
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
                  <h4 class="font-bold text-sm text-ios-label">Xray-core</h4>
                  <span class="text-[10px] text-ios-tertiary">XTLS 官方团队</span>
                </div>
              </div>
              <span class="text-[10px] px-2 py-0.5 rounded-full bg-blue-50 text-ios-blue font-semibold border border-blue-200/60">
                稳定成熟
              </span>
            </div>

            <p class="text-ios-secondary leading-relaxed text-[11px]">
              功能完备的高性能网络代理核心，深度优化 TLS/XTLS 协议栈与 Reality 伪装技术，兼容性极高。
            </p>

            <div class="space-y-1.5 pt-1 border-t border-black/[0.04]">
              <span class="text-neutral-500 font-medium block text-[11px]">支持协议矩阵:</span>
              <div class="flex flex-wrap gap-1">
                <span v-for="p in ['VLESS', 'VMess', 'Trojan', 'Shadowsocks', 'SOCKS5', 'HTTP', 'WireGuard']" :key="p" class="px-1.5 py-0.5 rounded bg-blue-50 text-ios-blue font-bold text-[10px]">
                  {{ p }}
                </span>
              </div>
            </div>

            <div class="text-[11px] text-ios-tertiary space-y-0.5 pt-1">
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
                  <h4 class="font-bold text-sm text-ios-label">sing-box</h4>
                  <span class="text-[10px] text-ios-tertiary">SagerNet 官方团队</span>
                </div>
              </div>
              <span class="text-[10px] px-2 py-0.5 rounded-full bg-purple-50 text-purple-700 font-semibold border border-purple-200/60">
                新一代轻量
              </span>
            </div>

            <p class="text-ios-secondary leading-relaxed text-[11px]">
              新一代通用代理平台框架，占用内存与 CPU 极低，原生支持 Hysteria2 等下一代基于 QUIC 的高速协议。
            </p>

            <div class="space-y-1.5 pt-1 border-t border-black/[0.04]">
              <span class="text-neutral-500 font-medium block text-[11px]">支持协议矩阵:</span>
              <div class="flex flex-wrap gap-1">
                <span v-for="p in ['Hysteria2', 'TUIC', 'Shadowsocks', 'VLESS', 'Trojan', 'Direct']" :key="p" class="px-1.5 py-0.5 rounded bg-purple-50 text-purple-700 font-bold text-[10px]">
                  {{ p }}
                </span>
              </div>
            </div>

            <div class="text-[11px] text-ios-tertiary space-y-0.5 pt-1">
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
import { onMounted, onUnmounted, ref, reactive, computed } from 'vue';
import { isOwner, formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { nodeAPI, installCommand, type Enrollment, type NodeView } from '../../nodes/api';
import { resolveCountryInfo, type CountryInfo } from '../../nodes/nodeMeta';
import { agentUpdatesAPI } from '../agentUpdatesApi';
import { coreUpdatesAPI, type ReleaseEntry } from '../coreUpdatesApi';
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
const releasesConfigured = ref(false);
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
    releasesConfigured.value = first.configured;
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

const batchAgentConfirmMessage = computed(() => {
  const count = eligibleOnlineNodes.value.length;
  return `确认遍历全部 ${count} 台符合条件的在线服务器，按各自 os/arch 下发最新签名版本？无完全匹配版本的服务器会跳过并报告原因，请逐台手动选择。`;
});

// ========================================================
// CORE PRESETS & DATA STRUCTURES
// ========================================================
interface CorePreset {
  version: string;
  label: string;
  amd64Url: string;
  arm64Url: string;
  sha256: string;
}

export interface InstalledCoreInfo {
  component: 'xray' | 'sing-box';
  name: string;
  version: string;
  healthy: boolean | undefined;
  protocols: string[];
}

const CORE_PRESETS: Record<string, CorePreset[]> = {
  xray: [
    {
      version: '25.10.15',
      label: 'v25.10.15 (版本辅助)',
      amd64Url: 'https://github.com/XTLS/Xray-core/releases/download/v25.10.15/Xray-linux-64.zip',
      arm64Url: 'https://github.com/XTLS/Xray-core/releases/download/v25.10.15/Xray-linux-arm64-v8a.zip',
      sha256: ''
    },
    {
      version: '26.3.27',
      label: 'v26.3.27 (版本辅助)',
      amd64Url: 'https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-64.zip',
      arm64Url: 'https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-arm64-v8a.zip',
      sha256: ''
    }
  ],
  'sing-box': [
    {
      version: '1.10.7',
      label: 'v1.10.7 (版本辅助)',
      amd64Url: 'https://github.com/SagerNet/sing-box/releases/download/v1.10.7/sing-box-1.10.7-linux-amd64.tar.gz',
      arm64Url: 'https://github.com/SagerNet/sing-box/releases/download/v1.10.7/sing-box-1.10.7-linux-arm64.tar.gz',
      sha256: ''
    },
    {
      version: '1.11.0',
      label: 'v1.11.0 (版本辅助)',
      amd64Url: 'https://github.com/SagerNet/sing-box/releases/download/v1.11.0/sing-box-1.11.0-linux-amd64.tar.gz',
      arm64Url: 'https://github.com/SagerNet/sing-box/releases/download/v1.11.0/sing-box-1.11.0-linux-arm64.tar.gz',
      sha256: 'b2c3d4e5f60718293a4b5c6d7e8f90123456789abcdef0123456789abcdef01'
    }
  ]
};

// ========================================================
// MULTI-CORE INSTALLED STATE MANAGEMENT
// ========================================================
function getNodeInstalledCores(node: NodeView): InstalledCoreInfo[] {
  const component = node.capabilities?.core_name;
  if (component !== 'xray' && component !== 'sing-box') return [];
  return [{ component, name: component === 'xray' ? 'Xray-core' : 'sing-box',
    version: node.capabilities?.core_version || '—', healthy: node.heartbeat?.core_healthy,
    protocols: protocols(node) }];
}

function getNodeAllProtocols(node: NodeView): string[] {
  const cores = getNodeInstalledCores(node);
  if (cores.length) {
    const set = new Set<string>();
    for (const c of cores) {
      for (const p of c.protocols) set.add(p);
    }
    return [...set].sort();
  }
  return protocols(node);
}

function detectArch(node?: NodeView | null): 'amd64' | 'arm64' | '' {
  if (!node) return '';
  const caps = node.capabilities as Record<string, unknown>;
  const arch = String(caps?.architecture || '');
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
function requireCoreTarget(node: NodeView, component: string) {
  const age = Date.now() - Date.parse(node.lastHeartbeat);
  if (!isOwner.value || !Number.isFinite(age) || age < -5000 || node.status !== 'online' || !node.enabled || node.deletedAt || Date.now() - Date.parse(node.lastHeartbeat) >= 45000 || !node.heartbeat?.current_revision) throw new Error('服务器需在线并上报当前配置版本');
  if (node.capabilities?.core_name !== component) throw new Error('只能更新节点当前上报的核心，多内核即将支持');
}

// ========================================================
// CORE MANAGEMENT MODAL (添加内核、删除内核、多内核共存)
// ========================================================
const coreManagementModalVisible = ref(false);
const coreManagementTargetNode = ref<NodeView | null>(null);

const manageInstalledCores = computed(() => {
  if (!coreManagementTargetNode.value) return [];
  return getNodeInstalledCores(coreManagementTargetNode.value);
});

function isCoreInstalled(comp: 'xray' | 'sing-box'): boolean {
  if (!coreManagementTargetNode.value) return false;
  return getNodeInstalledCores(coreManagementTargetNode.value).some(c => c.component === comp);
}

function openCoreManagementModal(node: NodeView) {
  if (!isOwner.value) return;
  addCoreForm.sha256 = ''; addCoreForm.url = '';
  coreManagementTargetNode.value = node;
  const component = node.capabilities?.core_name;
  if (component === 'xray' || component === 'sing-box') selectAddCoreComp(component);
  coreManagementModalVisible.value = true;

  if (node.status === 'pending') {
    pendingEnrollmentLoading.value = true;
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
  component: 'xray' as 'xray' | 'sing-box',
  version: '25.10.15',
  url: 'https://github.com/XTLS/Xray-core/releases/download/v25.10.15/Xray-linux-64.zip',
  sha256: ''
});

const addCorePresets = computed(() => {
  return CORE_PRESETS[addCoreForm.component] || [];
});

const addCoreBusy = ref(false);

function applyAddCorePreset(preset: CorePreset) {
    addCoreForm.sha256 = ''; addCoreForm.url = '';
  addCoreForm.version = preset.version;
  const arch = detectArch(coreManagementTargetNode.value);
  if (!arch) { showToast('架构未知，请手动填写匹配平台的下载地址', 'info'); return; }
  const isArm = arch === 'arm64';
  addCoreForm.url = isArm ? preset.arm64Url : preset.amd64Url;
  addCoreForm.sha256 = preset.sha256;
}

function selectAddCoreComp(comp: 'xray' | 'sing-box') {
  addCoreForm.component = comp;
  const presets = CORE_PRESETS[comp] || [];
  if (presets.length > 0) {
    applyAddCorePreset(presets[0]);
  } else {
    autoFillAddCoreURL();
  }
}

function onAddCoreCompChange() {
  selectAddCoreComp(addCoreForm.component);
}

function autoFillAddCoreURL() {
  const v = addCoreForm.version || (addCoreForm.component === 'xray' ? '25.10.15' : '1.10.7');
  const arch = detectArch(coreManagementTargetNode.value);
  if (!arch) { showToast('架构未知，请手动填写匹配平台的下载地址', 'info'); return; }
  const isArm = arch === 'arm64';
  if (addCoreForm.component === 'xray') {
    const asset = isArm ? 'Xray-linux-arm64-v8a.zip' : 'Xray-linux-64.zip';
    addCoreForm.url = `https://github.com/XTLS/Xray-core/releases/download/v${v}/${asset}`;
  } else {
    const asset = isArm ? `sing-box-${v}-linux-arm64.tar.gz` : `sing-box-${v}-linux-amd64.tar.gz`;
    addCoreForm.url = `https://github.com/SagerNet/sing-box/releases/download/v${v}/${asset}`;
  }
}

async function submitAddNewCore() {
  const node = coreManagementTargetNode.value;
  if (!node) return;

  if (node.status === 'pending') {
    showToast('该服务器等待 Agent 注册，请在终端执行一键命令即可完成安装', 'info');
    return;
  }

  if (!addCoreForm.version || !addCoreForm.url || !/^[a-f0-9]{64}$/i.test(addCoreForm.sha256)) {
    showToast('请完整填写版本号、官方发布地址和 64 位 SHA-256 校验和', 'warning');
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

    showToast('核心更新任务已受理，等待 Agent 确认');
    void load();
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
const currentCorePresets = computed(() => {
  return CORE_PRESETS[coreForm.component] || [];
});

function applyCorePreset(preset: CorePreset) {
    coreForm.sha256 = ''; coreForm.url = '';
  coreForm.version = preset.version;
  const arch = detectArch(coreDialogTargetNode.value);
  if (!arch) { showToast('架构未知，请手动填写匹配平台的下载地址', 'info'); return; }
  const isArm = arch === 'arm64';
  coreForm.url = isArm ? preset.arm64Url : preset.amd64Url;
  coreForm.sha256 = preset.sha256;
}

function selectCoreComponent(comp: 'xray' | 'sing-box') {
  coreForm.component = comp;
  const presets = CORE_PRESETS[comp] || [];
  if (presets.length > 0) {
    applyCorePreset(presets[0]);
  } else {
    autoFillCoreURL();
  }
}

const coreDialogVisible = ref(false);
const coreDialogTargetNode = ref<NodeView | null>(null);
const coreBusy = ref(false);

// Pending node enrollment state for 1-click install command
const pendingEnrollment = ref<Enrollment | null>(null);
const pendingEnrollmentLoading = ref(false);

const pendingEnrollmentCommand = computed(() => {
  const item = pendingEnrollment.value;
  if (!item || !item.install?.version || !item.install?.caSha256) return '';
  const host = window.location.hostname;
  const port = Number(window.location.port) || 443;
  return installCommand({
    version: item.install.version,
    host,
    httpPort: port,
    grpcPort: item.install.grpcPort,
    caSha256: item.install.caSha256,
    token: item.enrollmentToken,
    core: addCoreForm.component
  });
});

const coreDialogTitle = computed(() => {
  if (!coreDialogTargetNode.value) return '全部升级代理内核';
  return `升级代理内核 - ${coreDialogTargetNode.value.name}`;
});

const coreDialogDescription = computed(() => {
  if (!coreDialogTargetNode.value) {
    return `将对全部符合条件的在线服务器（共 ${eligibleOnlineNodes.value.length} 台）执行代理内核升级。`;
  }
  return '配置目标内核版本、官方下载地址与校验和。Agent 将进行 SHA-256 校验，切换可能中断连接，结果以节点回执为准。';
});

const coreDialogConfirmText = computed(() => {
  return '确认升级';
});

const savedCoreTarget = (() => {
  try {
    const raw = localStorage.getItem('qui_core_target_v1');
    return raw ? JSON.parse(raw) : null;
  } catch {
    return null;
  }
})();

const coreForm = reactive({
  component: savedCoreTarget?.component || 'xray',
  version: savedCoreTarget?.version || '25.10.15',
  url: savedCoreTarget?.url || 'https://github.com/XTLS/Xray-core/releases/download/v25.10.15/Xray-linux-64.zip',
  sha256: ''
});

function onCoreComponentChange() {
  selectCoreComponent(coreForm.component as 'xray' | 'sing-box');
}

function onCoreVersionInput() {
  coreForm.sha256 = '';
  autoFillCoreURL();
}

function autoFillCoreURL() {
  const v = coreForm.version || '26.3.27';
  const arch = detectArch(coreDialogTargetNode.value);
  if (!arch) { showToast('架构未知，请手动填写匹配平台的下载地址', 'info'); return; }
  const isArm = arch === 'arm64';
  if (coreForm.component === 'xray') {
    const asset = isArm ? 'Xray-linux-arm64-v8a.zip' : 'Xray-linux-64.zip';
    coreForm.url = `https://github.com/XTLS/Xray-core/releases/download/v${v}/${asset}`;
  } else {
    const asset = isArm ? `sing-box-${v}-linux-arm64.tar.gz` : `sing-box-${v}-linux-amd64.tar.gz`;
    coreForm.url = `https://github.com/SagerNet/sing-box/releases/download/v${v}/${asset}`;
  }
}

// Open Core Upgrade Modal for a specific node
function openUpgradeCore(node: NodeView) {
  const installed = getNodeInstalledCores(node);
  if (!installed.length) {
    openCoreManagementModal(node);
    return;
  }
  // Default to first installed core
  openUpgradeCoreForComponent(node, installed[0].component);
}

function openUpgradeCoreForComponent(node: NodeView, comp: 'xray' | 'sing-box') {
  if (!isOwner.value) return;
  coreForm.url = ''; coreForm.sha256 = '';
  coreDialogTargetNode.value = node;
  selectCoreComponent(comp);
  coreDialogVisible.value = true;
}

// Single node upgrade trigger
function handleUpgradeSingle(node: NodeView) {
  if (!isOwner.value) return;
  const revision = node.heartbeat?.current_revision;
  if (!revision || node.status !== 'online') {
    showToast(`服务器「${node.name}」当前未在线或尚未上报配置版本，暂无法升级`, 'warning');
    return;
  }

  if (props.kind === 'agent') {
    const release = findLatestAgentReleaseForNode(node, agentReleases.value);
    if (!releasesConfigured.value) { showToast('中央尚未配置发布公钥', 'warning'); return; }
    if (!release) {
      manualAgentNode.value = node; manualReleaseId.value = '';
      return;
    }
    singleAgentConfirmTarget.value = { node, release, revision };
    singleAgentConfirmVisible.value = true;
  } else {
    openUpgradeCore(node);
  }
}

// Batch upgrade trigger (from top-right "全部升级")
async function upgradeAll() {
  if (!isOwner.value) return;
  try { batchNodes.value = await readAllNodes(); await loadUpgradeContext(); }
  catch (e) { showToast((e as Error).message, 'error'); return; }
  const eligible = eligibleOnlineNodes.value;
  if (!eligible.length) {
    showToast('当前暂无满足升级条件的在线服务器 (需在线且已上报配置版本)', 'warning');
    return;
  }

  if (props.kind === 'agent') {
    if (!releasesConfigured.value) { showToast('中央尚未配置发布公钥', 'warning'); return; }
    if (!latestAgentRelease.value) {
      showToast('尚未在系统中登记可信 Agent 签名版本，请前往「系统管理 › 任务记录」登记签名清单', 'warning');
      return;
    }
    batchAgentConfirmVisible.value = true;
  } else {
    coreDialogTargetNode.value = null; // null means all eligible online nodes
    coreDialogVisible.value = true;
  }
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

// Submit Core upgrade action (single node or batch)
async function submitCoreAction() {
  if (!isOwner.value) return;
  if (!coreForm.version || !coreForm.url || !coreForm.sha256) {
    showToast('请完整填写版本号、官方发布地址和 64 位 SHA-256 校验和', 'warning');
    return;
  }
  if (!/^[a-f0-9]{64}$/i.test(coreForm.sha256)) {
    showToast('SHA-256 校验和必须为 64 位十六进制字符', 'warning');
    return;
  }

  coreBusy.value = true;
  try {
    try {
      localStorage.setItem('qui_core_target_v1', JSON.stringify(coreForm));
    } catch {}

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

      showToast(`已向「${node.name}」下发内核升级任务 (${coreForm.component} ${coreForm.version})`);
    } else {
      const eligible = eligibleOnlineNodes.value;
      if (!eligible.length) {
        showToast(`当前没有在线服务器`, 'warning');
        coreBusy.value = false;
        return;
      }

      let success = 0;
      let failed = 0;
      for (const node of eligible) {
        try {
          requireCoreTarget(node, coreForm.component);
          await coreUpdatesAPI.queue({
            nodeId: node.id,
            revision: node.heartbeat!.current_revision!,
            component: coreForm.component,
            version: coreForm.version,
            url: coreForm.url,
            sha256: coreForm.sha256
          });
          success++;
        } catch (e) {
          showToast(node.name + '：' + (e as Error).message, 'error');
          failed++;
        }
      }

      showToast(`已向 ${success} 台服务器下发内核升级任务${failed ? ` (${failed} 台跳过/失败)` : ''}`);
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
  handleUpgradeSingle,
  openCoreManagementModal,
  openUpgradeCore
});

let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  try {
    const result = await nodeAPI.list(page.value, keyword.value);
    if (current === generation) {
      nodes.value = result.items;
      total.value = result.total;
    }
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
  const cores = getNodeInstalledCores(node);
  if (cores.length) {
    return cores.map(c => `${c.name} ${c.version}`).join(', ');
  }
  if (!node.capabilities?.core_name) return '尚未检测核心';
  return `${node.capabilities.core_name} ${node.capabilities.core_version || '版本未上报'}`;
}

function coreHealth(node: NodeView) {
  if (node.heartbeat?.core_healthy === true) return '最近上报正常';
  if (node.heartbeat?.core_healthy === false) return '最近上报异常';
  return '尚未上报';
}

function protocols(node: NodeView) {
  return [...new Set((node.capabilities?.protocols || []).filter(item => item.inbound || item.outbound).map(item => item.protocol))].sort();
}

function getCountryInfo(node: NodeView): CountryInfo {
  return resolveCountryInfo(node.name, node.publicIp || node.hostname);
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
  void load();
  void loadUpgradeContext();
  window.addEventListener('proxy-node-status', load);
});

onUnmounted(() => {
  generation++;
  window.removeEventListener('proxy-node-status', load);
});
</script>
