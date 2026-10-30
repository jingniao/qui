<template>
  <div class="space-y-6">
    <PageHeader
      title="服务器列表"
      parent="服务器"
      description="以“列表看状态、详情做管理”为原则，保持简洁清晰。"
    >
      <template #actions>
        <!-- Admin Actions -->
          <button
            v-if="isOwner"
            type="button"
            @click="createDrawer = true"
            class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
            </svg>
            <span>新增服务器</span>
          </button>


      </template>
    </PageHeader>

    <!-- ================= VIEW 1: ADMIN (服务器主机管理) ================= -->
    <!-- Tab 1: 服务器列表视图 -->

        <!-- Search & Filter Bar (Aligned with UserListPage) -->
        <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
          <form @submit.prevent="search" class="flex flex-wrap items-center gap-3 flex-1">
            <div class="relative flex-1 max-w-md">
              <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
              </svg>
              <input
                v-model="keyword"
                placeholder="搜索服务器名称、IP 或地区..."
                class="ios-search-input"
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
            <button class="ios-search-btn shrink-0">查询</button>

            <!-- Status Filter Dropdown -->
            <div class="relative">
              <button
                type="button"
                @click="filterDropdownOpen = !filterDropdownOpen"
                class="ios-btn-secondary inline-flex items-center gap-1.5 shrink-0"
              >
                <span>{{ currentStatusFilterLabel }}</span>
                <svg class="w-3.5 h-3.5 text-ios-tertiary transition-transform" :class="filterDropdownOpen ? 'rotate-180' : ''" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                </svg>
              </button>

              <div
                v-if="filterDropdownOpen"
                class="absolute left-0 mt-1.5 w-36 bg-white rounded-2xl shadow-xl border border-black/[0.08] py-1.5 z-50 animate-in fade-in slide-in-from-top-1"
              >
                <button
                  v-for="opt in statusFilterOptions"
                  :key="opt.value"
                  type="button"
                  @click="setStatusFilter(opt.value)"
                  class="w-full px-3.5 py-1.5 text-left text-xs font-medium transition-colors flex items-center justify-between"
                  :class="currentStatusFilter === opt.value ? 'bg-ios-blue/10 text-ios-blue font-semibold' : 'text-ios-label hover:bg-neutral-50'"
                >
                  <span>{{ opt.label }}</span>
                  <svg v-if="currentStatusFilter === opt.value" class="w-3.5 h-3.5 text-ios-blue" viewBox="0 0 20 20" fill="currentColor">
                    <path fill-rule="evenodd" d="M16.707 5.293a1 1 0 010 1.414l-8 8a1 1 0 01-1.414 0l-4-4a1 1 0 011.414-1.414L8 12.586l7.293-7.293a1 1 0 011.414 0z" clip-rule="evenodd" />
                  </svg>
                </button>
              </div>
            </div>
          </form>

          <!-- Right: View Mode Toggle & Refresh -->
          <div class="flex items-center gap-2 shrink-0 self-end sm:self-auto">
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

            <!-- Refresh Button -->
            <button
              type="button"
              @click="load"
              class="ios-btn-secondary inline-flex items-center gap-1.5"
              title="刷新数据"
            >
              <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              <span class="hidden sm:inline">刷新</span>
            </button>

            <!-- More Actions Dropdown -->
            <div class="relative">
              <button
                type="button"
                @click="moreDropdownOpen = !moreDropdownOpen"
                class="ios-btn-secondary inline-flex items-center gap-1 px-2.5"
                title="更多选项"
              >
                <span class="tracking-widest font-mono text-xs">···</span>
              </button>

              <div
                v-if="moreDropdownOpen"
                class="absolute right-0 mt-1.5 w-44 bg-white rounded-2xl shadow-xl border border-black/[0.08] py-1.5 z-50 animate-in fade-in slide-in-from-top-1"
              >
                <label class="px-3.5 py-2 text-xs font-medium text-ios-label hover:bg-neutral-50 flex items-center gap-2 cursor-pointer select-none">
                  <input v-model="showDeleted" type="checkbox" @change="search" class="rounded border-black/20 text-ios-blue" />
                  <span>显示已删除服务器</span>
                </label>
              </div>
            </div>
          </div>
        </div>

    <!-- State: Error, Loading, Empty -->
    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !nodes.length" />
    <EmptyState
      v-else-if="!filteredAdminNodes.length"
      :title="keyword || currentStatusFilter !== 'all' ? '未找到匹配的服务器' : (showDeleted ? '没有已删除的服务器' : '还没有注册服务器')"
      :description="keyword || currentStatusFilter !== 'all' ? '请尝试调整筛选条件或搜索关键词。' : '点击右上角“新增服务器”即可获取一键命令并连接节点。'"
    />

    <!-- ================= Mode 1: Table List View (Default & Unified) ================= -->
    <DataTable v-else-if="displayMode === 'list'">
      <template #header>
        <th>服务器</th>
        <th>状态</th>
        <th>系统资源</th>
        <th>本月流量 / 速率</th>
        <th>线路延迟</th>
        <th>到期时间 / 续费</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="node in filteredAdminNodes" :key="node.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. 服务器名称与地区/IP -->
          <td class="p-4">
            <div class="flex items-center gap-2.5">
              <div
                class="w-8 h-8 rounded-xl bg-blue-50 border border-blue-100 flex items-center justify-center shrink-0 text-base select-none cursor-default"
                :title="`${getNodeCountry(node).name} (${getNodeCountry(node).source === 'admin_region' ? '管理员指定' : 'IP识别'})`"
              >
                {{ getNodeCountry(node).flag }}
              </div>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <router-link :to="`/nodes/${node.id}`" class="font-semibold text-ios-blue hover:underline text-[15px] truncate">
                    {{ node.name }}
                  </router-link>
                  <span v-if="getNodeRegion(node)" class="px-1.5 py-0.2 rounded text-[10px] font-bold tracking-wide uppercase shrink-0 bg-black/[0.04] text-neutral-600">
                    {{ getNodeRegion(node) }}
                  </span>
                  <span v-if="node.deletedAt" class="px-1.5 py-0.2 rounded text-[10px] font-bold tracking-wide uppercase shrink-0 bg-black/[0.05] text-neutral-400">
                    已删除
                  </span>
                </div>
                <p class="text-[13px] text-ios-secondary font-mono truncate mt-0.5">
                  {{ node.publicIp || node.hostname || '未分配 IP' }}
                  <span v-if="getNodeMeta(node.id).remark" class="text-ios-tertiary ml-1.5 font-sans">· {{ getNodeMeta(node.id).remark }}</span>
                </p>
              </div>
            </div>
          </td>

          <!-- 2. 统一状态胶囊 StatusBadge -->
          <td>
            <StatusBadge
              :status="getNodeStatusTone(node)"
              :label="statusCapsuleText(node)"
            />
          </td>

          <!-- 3. 系统资源 -->
          <td class="text-xs font-mono tabular-nums text-ios-secondary">
            <div class="space-y-0.5">
              <div class="text-[13px] text-ios-label font-medium">
                CPU <span class="font-semibold">{{ formatPercent(node.heartbeat?.cpu_percent) }}</span>
              </div>
              <div class="text-[11px] text-ios-tertiary">
                内存 {{ formatPercent(node.heartbeat?.memory_percent) }} · 磁盘 {{ formatPercent(node.heartbeat?.disk_percent) }}
              </div>
            </div>
          </td>

          <!-- 4. 本月流量与速率 -->
          <td>
            <div class="flex items-center">
              <span class="font-semibold text-ios-label text-sm"><TrafficValue :bytes="getNodeMonthTraffic(node)" /></span>
              <span class="text-xs text-ios-tertiary ml-1.5 font-mono tabular-nums">(本月)</span>
            </div>
              <p class="text-[11px] text-ios-tertiary font-mono mt-0.5">
                ↑ — · ↓ —
              </p>
          </td>

          <!-- 5. 线路延迟 -->
          <td class="text-xs font-mono">
            <div class="space-y-0.5">
              <div class="text-ios-tertiary font-semibold text-sm">—</div>
              <div class="text-[11px] text-ios-tertiary">未配置测试点</div>
            </div>
          </td>

          <!-- 6. 到期时间与续费 -->
          <td class="text-xs font-mono">
            <div class="space-y-0.5">
              <p :class="getRenewalInfo(node).isNear ? 'text-amber-600 font-bold' : 'text-ios-label font-medium'">
                {{ getRenewalDate(node) || '—' }}
                <span v-if="getRenewalDate(node)" class="text-[11px] font-normal" :class="getRenewalInfo(node).isNear ? 'text-amber-600' : 'text-ios-tertiary'">
                  ({{ getRenewalInfo(node).label }})
                </span>
              </p>
              <p v-if="getRenewalPriceDisplay(node)" class="text-ios-secondary font-semibold text-[11px]">
                {{ getRenewalPriceDisplay(node) }}
              </p>
            </div>
          </td>

          <!-- 7. 操作列 (对齐 UserListPage 统一样式) -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <router-link :to="`/nodes/${node.id}`" class="ios-action-btn-secondary">详情</router-link>
            <button v-if="isOwner && !node.deletedAt" @click="openEditDrawer(node)" class="ios-action-btn-primary">编辑</button>
            <button v-if="isOwner && !node.deletedAt" @click="promptDelete(node)" class="ios-action-btn-danger">删除</button>
          </td>
        </tr>
      </template>
      <template #footer>
        <Pagination :total="total" :current-page="page" :page-size="20" @update:page="changePage" />
      </template>
    </DataTable>

    <!-- ================= Mode 2: Exact UI Card Layout (Restored) ================= -->
    <div
      v-else-if="displayMode === 'grid'"
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in filteredAdminNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <!-- Card Top Details -->
        <div class="space-y-3.5">
          <!-- Row 1: Name & Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="`${getNodeCountry(node).name} (${getNodeCountry(node).source === 'admin_region' ? '管理员指定地区' : 'IP归属识别'})`"
              >
                {{ getNodeCountry(node).flag }}
              </span>
              <router-link
                :to="`/nodes/${node.id}`"
                class="font-bold text-[17px] text-neutral-900 hover:text-ios-blue transition-colors truncate tracking-tight"
                :title="node.name"
              >
                {{ node.name }}
              </router-link>
              <span v-if="getNodeRegion(node)" class="rounded bg-black/[0.04] px-1.5 py-0.5 text-[10px] font-semibold text-neutral-600 shrink-0 truncate max-w-[80px]" :title="getNodeRegion(node)">
                {{ getNodeRegion(node) }}
              </span>
              <span v-if="node.deletedAt" class="rounded-full bg-black/[0.05] px-1.5 py-0.2 text-[10px] font-semibold text-neutral-400 shrink-0">
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

          <!-- Row 2: Sub-info (IP · Core Version · Running Time) -->
          <div class="text-[12px] text-neutral-400 font-mono tracking-tight flex items-center gap-1.5 flex-wrap">
            <span class="text-neutral-500">{{ node.publicIp || node.hostname || '未分配 IP' }}</span>
            <span class="text-neutral-300">·</span>
            <span>{{ node.capabilities?.core_version || '—' }}</span>
            <span class="text-neutral-300">·</span>
            <span>已运行 {{ calculateRunningTime(node) }}</span>
          </div>

          <!-- Row 3: CPU / 内存 / 磁盘 3栏微型仪表 -->
          <div class="grid grid-cols-3 gap-3 pt-0.5">
            <!-- CPU -->
            <div>
              <div class="flex items-center justify-between text-[11px] mb-1">
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
              <div class="flex items-center justify-between text-[11px] mb-1">
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
              <div class="flex items-center justify-between text-[11px] mb-1">
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

          <!-- Row 4: 网速与本月流量 -->
          <div class="grid grid-cols-2 gap-2 pt-1 border-t border-black/[0.04]">
            <!-- 网速 -->
            <div>
              <p class="text-[11px] text-neutral-400 font-medium">网速</p>
              <div class="text-[12px] font-mono mt-0.5 text-neutral-700 flex items-center gap-1.5 tabular-nums">
                <span class="text-emerald-600 font-semibold flex items-center">↑ {{ nicRate(hostMetrics(node, clockNow), 'transmit') }}</span>
                <span class="text-ios-blue font-semibold flex items-center">↓ {{ nicRate(hostMetrics(node, clockNow), 'receive') }}</span>
              </div>
            </div>
            <!-- 本月流量 -->
            <div>
              <p class="text-[11px] text-neutral-400 font-medium" title="面板时区内仍保留的账户流量样本合计；受保留策略影响">本月流量（保留记录）</p>
              <div class="text-[12px] font-mono mt-0.5 flex items-baseline gap-1 tabular-nums">
                <span class="font-bold text-neutral-900 text-sm">
                  <TrafficValue :bytes="getNodeMonthTraffic(node)" />
                </span>
                <span class="text-[10px] text-neutral-400 font-normal">
                  ↑{{ formatCompactBytes(nodeMonthTraffic[node.id]?.uploadBytes) }} ↓{{ formatCompactBytes(nodeMonthTraffic[node.id]?.downloadBytes) }}
                </span>
              </div>
            </div>
          </div>

          <!-- Row 5: 延迟与测试点卡片 (3 个测试点) -->
          <div class="pt-1 border-t border-black/[0.04] space-y-1.5">
            <div class="flex items-center justify-between text-[11px]">
              <span class="text-neutral-400 font-medium">延迟</span>
              <span class="text-neutral-400 font-mono text-[10px]">未配置</span>
            </div>

            <div class="grid grid-cols-3 gap-1.5">
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">测试点 1</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">—</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">测试点 2</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">—</p>
              </div>
              <div class="bg-[#F8F9FA] rounded-xl p-2 border border-black/[0.03] space-y-0.5">
                <div class="flex items-center gap-1 text-[10px] text-neutral-500 font-medium truncate">
                  <span class="w-1.5 h-1.5 rounded-full bg-neutral-300 shrink-0"></span>
                  <span class="truncate">测试点 3</span>
                </div>
                <p class="text-xs font-bold font-mono text-ios-tertiary">—</p>
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
                <span class="text-[10px] font-normal" :class="getRenewalInfo(node).isNear ? 'text-amber-600' : 'text-neutral-400'">
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
                class="font-mono font-bold text-neutral-900 bg-neutral-100 px-1.5 py-0.5 rounded-md text-[11px]"
              >
                {{ getRenewalPriceDisplay(node) }}
              </span>
              <span v-else class="text-neutral-400 font-mono text-[11px]">—</span>
            </div>
          </div>
        </div>

        <!-- Row 7: 彩色操作网格按钮 (安装, 升级, 调优, 编辑, 删除, 详情) -->
        <div class="pt-2 border-t border-black/[0.05] grid grid-cols-3 gap-1.5">
          <!-- 1. 安装 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="openInstallDialog(node)"
            class="py-1.5 rounded-xl bg-[#ECFDF3] text-[#027A48] hover:bg-[#D1FADF] font-semibold text-xs text-center transition-colors"
          >
            安装
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 2. 升级 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="openUpgradeDialog(node)"
            class="py-1.5 rounded-xl bg-[#FEF6EE] text-[#B54708] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            升级
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 3. 调优 -->
          <router-link
            v-if="!node.deletedAt"
            :to="`/nodes/${node.id}?tab=tuning`"
            class="py-1.5 rounded-xl bg-[#ECFDF3] text-[#027A48] hover:bg-[#D1FADF] font-semibold text-xs text-center transition-colors"
          >
            调优
          </router-link>
          <div v-else class="py-1.5"></div>

          <!-- 7. 编辑 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="openEditDrawer(node)"
            class="py-1.5 rounded-xl bg-[#F0F9FF] text-[#026AA2] hover:bg-[#E0F2FE] font-semibold text-xs text-center transition-colors"
          >
            编辑
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 8. 删除 -->
          <button
            v-if="isOwner && !node.deletedAt"
            type="button"
            @click="promptDelete(node)"
            class="py-1.5 rounded-xl bg-[#FEF3F2] text-[#B42318] hover:bg-[#FEE4E2] font-semibold text-xs text-center transition-colors"
          >
            删除
          </button>
          <div v-else class="py-1.5"></div>

          <!-- 9. 详情 -->
          <router-link
            :to="`/nodes/${node.id}`"
            class="py-1.5 rounded-xl bg-neutral-100 text-neutral-600 hover:bg-neutral-200 font-semibold text-xs text-center transition-colors"
          >
            详情
          </router-link>
        </div>
      </div>
    </div>
      <!-- Drawer: Create Server -->
    <FormDrawer
      v-model="createDrawer"
      title="新增服务器"
      description="创建一次性服务器注册码。在一小时内在目标服务器上执行安装命令完成连接。"
      :loading="creating"
      @confirm="create"
    >
      <div class="space-y-4 text-sm">
        <label class="block text-xs font-medium text-neutral-600">
          服务器名称
          <input v-model="name" placeholder="例如 香港 HKT" maxlength="40" class="form-input-base mt-1.5" />
        </label>
        <div class="space-y-3 pt-2 border-t border-black/[0.06]">
          <span class="text-xs font-semibold text-neutral-800">续费与财务信息 (可选)</span>
          <div class="space-y-3">
            <label class="block text-xs font-medium text-neutral-600">
              续费到期日
              <input v-model="createRenewDate" type="date" class="form-input-base mt-1" />
            </label>
            <div class="block text-xs font-medium text-neutral-600">
              续费价格与周期
              <div class="flex items-center mt-1 rounded-xl border border-black/15 bg-white overflow-hidden shadow-2xs focus-within:ring-2 focus-within:ring-ios-blue/20 focus-within:border-ios-blue transition-all">
                <select
                  v-model="createRenewCurrency"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-800 font-semibold text-xs px-3 py-2.5 border-r border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                >
                  <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
                </select>
                <input
                  v-model="createRenewPrice"
                  type="number"
                  step="any"
                  min="0"
                  placeholder="输入续费金额，例如 120"
                  class="flex-1 min-w-0 px-3 py-2 text-xs font-mono font-medium text-neutral-900 placeholder:text-neutral-400 focus:outline-none bg-transparent"
                />
                <select
                  v-model="createRenewCycle"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-700 font-semibold text-xs px-3 py-2.5 border-l border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                >
                  <option v-for="cycle in CYCLE_LIST" :key="cycle" :value="cycle">{{ cycle }}</option>
                </select>
              </div>
            </div>
          </div>
        </div>
      </div>
    </FormDrawer>

    <!-- Drawer: Quick Edit Server -->
    <FormDrawer
      v-model="editDrawer"
      title="编辑服务器"
      description="修改服务器接入地址、维护状态、续费价格与扩展信息。"
      :loading="editingSaving"
      @confirm="saveEdit"
    >
      <div class="space-y-4 text-sm" v-if="editTarget">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">服务器名称</span>
          <input v-model="editForm.name" maxlength="40" class="form-input-base mt-1" />
        </label>
        <label class="block">
          <div class="flex items-center justify-between">
            <span class="text-xs font-medium text-neutral-600">地区</span>
            <span class="text-[11px] text-neutral-400">优先采用此地区展示国旗，留空则根据 IP 归属自动识别</span>
          </div>
          <input v-model="editForm.region" placeholder="例如 香港、日本东京、美国洛杉矶" class="form-input-base mt-1" />
        </label>
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">服务器备注</span>
          <textarea v-model="editForm.remark" rows="2" placeholder="填写服务器规格等备注信息" class="form-input-base mt-1"></textarea>
        </label>

        <div class="border-t border-black/[0.06] pt-3.5 space-y-3">
          <div class="flex items-center justify-between">
            <span class="text-xs font-semibold text-neutral-800">续费与财务信息</span>
            <span v-if="editForm.renewDate" class="text-xs text-neutral-500 font-mono">
              到期状态：<span class="font-bold text-neutral-800">{{ calculateRemainingDays(editForm.renewDate).label }}</span>
            </span>
          </div>

          <div class="space-y-3">
            <label class="block">
              <span class="text-xs font-medium text-neutral-600">续费到期日</span>
              <input v-model="editForm.renewDate" type="date" class="form-input-base mt-1" />
            </label>

            <div class="block">
              <span class="text-xs font-medium text-neutral-600">续费价格与周期</span>
              <div class="flex items-center mt-1 rounded-xl border border-black/15 bg-white overflow-hidden shadow-2xs focus-within:ring-2 focus-within:ring-ios-blue/20 focus-within:border-ios-blue transition-all">
                <select
                  v-model="editForm.renewCurrency"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-800 font-semibold text-xs px-3 py-2.5 border-r border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                >
                  <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
                </select>
                <input
                  v-model="editForm.renewPrice"
                  type="number"
                  step="any"
                  min="0"
                  placeholder="输入续费金额，例如 120"
                  class="flex-1 min-w-0 px-3 py-2 text-xs font-mono font-medium text-neutral-900 placeholder:text-neutral-400 focus:outline-none bg-transparent"
                />
                <select
                  v-model="editForm.renewCycle"
                  class="bg-neutral-50 hover:bg-neutral-100 text-neutral-700 font-semibold text-xs px-3 py-2.5 border-l border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                >
                  <option v-for="cycle in CYCLE_LIST" :key="cycle" :value="cycle">{{ cycle }}</option>
                </select>
              </div>
            </div>
          </div>
        </div>

        <div class="border-t border-black/[0.06] pt-3 space-y-2">
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.enabled" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>启用服务器控制连接</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.allowSubscription" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>允许用户订阅本服务器节点</span>
          </label>
          <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-neutral-800">
            <input v-model="editForm.maintenance" type="checkbox" class="rounded border-black/20 text-ios-blue" />
            <span>设为维护模式（暂时排除在订阅之外）</span>
          </label>
        </div>
      </div>
    </FormDrawer>

    <!-- Dialog: One-click Install Command -->
    <AppDialog
      v-model="tokenDialog"
      title="一键安装命令"
      description="在服务器终端以 root 用户运行命令即可完成 Agent 自动部署与信任配置。"
      :show-footer="false"
    >
      <div class="space-y-4">
        <div v-if="installAvailable" class="space-y-3">
          <div class="grid gap-3 sm:grid-cols-3">
            <label class="block text-xs font-medium text-ios-secondary">面板地址<input v-model.trim="installHost" maxlength="253" class="form-input-base mt-1.5" /></label>
            <label class="block text-xs font-medium text-ios-secondary">HTTP 端口<input v-model.number="installHTTPPort" type="number" min="1" max="65535" class="form-input-base mt-1.5" /></label>
            <label class="block text-xs font-medium text-ios-secondary">核心<select v-model="installCore" class="form-input-base mt-1.5"><option value="xray">xray</option><option value="sing-box">sing-box</option></select></label>
          </div>
          <CodePreview v-if="installCommandText" :code="installCommandText" title="一键安装命令" />
          <p v-else class="text-xs text-ios-red">请填写有效的面板地址与端口</p>
          <p class="text-xs text-neutral-400">
            命令含一次性注册码，{{ formatTime(enrollment?.expiresAt || '') }} 前有效。
          </p>
        </div>
        <div v-else>
          <CodePreview :code="enrollment?.enrollmentToken || ''" />
          <p class="text-xs text-ios-tertiary mt-2">有效期至 {{ formatTime(enrollment?.expiresAt || '') }}</p>
          <p class="text-xs text-neutral-400 mt-2">本地/开发构建无法生成一键命令，请使用注册码手动登记。</p>
        </div>
        <div class="pt-2">
          <button type="button" class="ios-dialog-btn-secondary w-full" @click="tokenDialog = false">关闭</button>
        </div>
      </div>
    </AppDialog>

    <!-- Dialog: Upgrade Dialog -->
    <AppDialog
      v-model="upgradeDialog"
      title="升级服务器核心与 Agent"
      description="检查并下发远程升级任务至目标服务器。"
      :show-footer="false"
    >
      <div class="space-y-4 text-xs" v-if="upgradeTarget">
        <div class="p-3 rounded-xl bg-neutral-50 border border-neutral-100 space-y-1">
          <p class="text-neutral-500">当前服务器: <span class="font-bold text-neutral-800">{{ upgradeTarget.name }}</span></p>
          <p class="text-neutral-500">当前 Agent: <span class="font-mono text-neutral-800">{{ upgradeTarget.agentVersion || '—' }}</span></p>
          <p class="text-neutral-500">当前核心: <span class="font-mono text-neutral-800">{{ upgradeTarget.capabilities?.core_name || 'xray' }} {{ upgradeTarget.capabilities?.core_version || '' }}</span></p>
        </div>
        <p class="text-neutral-600">Agent 与核心升级在“Agent 管理 / 代理核心”页签中创建任务（Agent 使用签名清单，核心使用官方地址 + SHA-256）。</p>
        <div class="flex items-center justify-end gap-2 pt-2">
          <button type="button" class="ios-dialog-btn-secondary" @click="upgradeDialog = false">取消</button>
          <button type="button" class="ios-btn-primary" @click="confirmUpgrade">前往 Agent 升级</button>
        </div>
      </div>
    </AppDialog>

    <!-- Dialog: Rollback Dialog -->
    <AppDialog
      v-model="rollbackDialog"
      title="配置版本回退"
      :show-footer="false"
    >
      <div class="space-y-4">
        <p class="text-sm text-neutral-600">选择要回退的成功版本。</p>
        <div v-if="rollbackLoading" class="text-sm text-neutral-500">加载中...</div>
        <div v-else-if="rollbackChoices.length === 0" class="text-sm text-neutral-500">没有可回退的成功版本</div>
        <div v-else>
          <select v-model="rollbackSelectedId" class="form-input-base mt-1">
            <option v-for="r in rollbackChoices" :key="r.id" :value="r.id">#{{ r.id }} · {{ r.summary }} · {{ formatTime(r.createdAt) }}</option>
          </select>
        </div>
        <div class="flex items-center justify-end gap-2.5 pt-2">
          <button type="button" class="ios-dialog-btn-secondary" @click="rollbackDialog = false">取消</button>
          <button type="button" :disabled="rollbackLoading || rollbackChoices.length === 0 || rollbackSelectedId == null" class="ios-dialog-btn-danger" @click="executeRollback">确认回退</button>
        </div>
      </div>
    </AppDialog>

    <!-- Dialog: Delete Confirm Dialog -->
    <AppDialog
      v-model="deleteDialog"
      :title="deletePreview?.mode === 'cancel' ? '取消服务器登记' : '删除服务器'"
      :show-footer="false"
    >
      <div class="space-y-4">
        <div v-if="deleteLoading" class="text-sm text-neutral-500">正在检查服务器引用与发布状态…</div>
        <template v-else-if="deleteBlockers.length">
          <p class="text-sm text-neutral-600">服务器仍被以下配置引用或有进行中的发布，请先处理：</p>
          <ul class="space-y-2">
            <li v-for="blocker in deleteBlockers" :key="blocker.kind + blocker.id" class="flex items-center gap-2 text-sm">
              <span class="shrink-0 rounded-full bg-black/[0.05] px-2 py-0.5 text-[11px] font-semibold text-neutral-600">{{ blockerLabel(blocker.kind) }}</span>
              <span class="text-neutral-900 font-medium break-all">{{ blocker.name }}</span>
            </li>
          </ul>
        </template>
        <p v-else-if="deletePreview?.mode === 'cancel'" class="text-sm text-neutral-600 leading-relaxed">
          确认取消服务器 {{ deleteTarget?.name || '' }} 的登记？记录将直接移除。
        </p>
        <p v-else-if="deletePreview" class="text-sm text-neutral-600 leading-relaxed">
          确认删除服务器 {{ deleteTarget?.name || '' }}？删除后该服务器证书将立即吊销，退出所有线路组，历史流量与访问记录保留。
        </p>
        <p v-if="deleteError" role="alert" class="text-sm text-ios-red">{{ deleteError }}</p>
        <div class="flex items-center justify-end gap-2.5 pt-2">
          <button type="button" class="ios-dialog-btn-secondary" @click="deleteDialog = false">关闭</button>
          <button
            v-if="!deleteLoading && deletePreview"
            type="button"
            :disabled="deleteLoading || deleting || !deletePreview || deleteBlockers.length > 0"
            class="ios-dialog-btn-danger"
            @click="confirmDelete"
          >
            确认删除
          </button>
        </div>
      </div>
    </AppDialog>

    <!-- ================= VIEW 2: PORTAL (前台用户可用代理节点) ================= -->


  </div>
</template>

<script setup lang="ts">
import { useRouter, useRoute } from 'vue-router';

import { isOwner as authIsOwner } from '../../../app/auth';


import { generateQRCodeSVG } from '../../../app/qrcode';
import {
  proxyNodeStore,
  loadStoredData,
  getNodeRelayRoutes,
  buildProxyLink,
  type ProxyNodeItem,
  type NodeRelayRoute
} from '../../proxy/proxyNodes';
import { proxyAPI, type Inbound } from '../../proxy/api';
import { userAPI } from '../../users/api';
import { authState } from '../../../app/auth';


import { ref, reactive, computed, watch, onMounted, onUnmounted } from 'vue';
import { nodeAPI, installCommand, type NodeView, type Enrollment, type DeletionPreview } from '../api';
import { APIError, request } from '../../../app/apiClient';
import { groupAPI } from '../../nodes/groupsApi';
import { accessAPI } from '../../access/api';
import { deploymentAPI } from '../../system/deploymentApi';
import type { Revision } from '../../system/deploymentApi';
import {
  getNodeMeta,
  loadNodeMeta,
  saveNodeMeta,
  calculateRemainingDays,
  resolveCountryInfo,
  onGeoResolved,
  parseRenewPrice,
  formatRenewPrice,
  CURRENCY_LIST,
  CYCLE_LIST
} from '../nodeMeta';
import PageHeader from '../../../components/PageHeader.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import { isOwner, formatTime } from '../../../app/auth';
import { hostMetrics, nicRate } from '../metrics';
import { showToast } from '../../../app/api';
import DataTable from '../../../components/DataTable.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import Pagination from '../../../components/Pagination.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import AppDialog from '../../../components/AppDialog.vue';
import CodePreview from '../../../components/CodePreview.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const nodes = ref<NodeView[]>([]);
const total = ref(0);
const page = ref(1);
const keyword = ref('');
const showDeleted = ref(false);
const loading = ref(false);
const error = ref('');

// Display Mode: 'grid' (Cards) or 'list' (Table)
const displayMode = ref<'grid' | 'list'>(
  (localStorage.getItem('qui_server_display_mode') as 'grid' | 'list') || 'grid'
);

function setDisplayMode(mode: 'grid' | 'list') {
  displayMode.value = mode;
  localStorage.setItem('qui_server_display_mode', mode);
}

// Filter Dropdown
const filterDropdownOpen = ref(false);
const moreDropdownOpen = ref(false);

type StatusFilter = 'all' | 'online' | 'core_error' | 'maintenance' | 'offline' | 'disabled';
const currentStatusFilter = ref<StatusFilter>('all');
const statusFilterOptions: { label: string; value: StatusFilter }[] = [
  { label: '全部状态', value: 'all' },
  { label: '在线', value: 'online' },
  { label: '核心异常', value: 'core_error' },
  { label: '维护中', value: 'maintenance' },
  { label: '离线', value: 'offline' },
  { label: '已停用', value: 'disabled' },
];

const currentStatusFilterLabel = computed(() => {
  const f = statusFilterOptions.find(o => o.value === currentStatusFilter.value);
  return f ? f.label : '筛选';
});

function setStatusFilter(filter: StatusFilter) {
  currentStatusFilter.value = filter;
  filterDropdownOpen.value = false;
}

// Filtered Admin Nodes
const filteredAdminNodes = computed(() => {
  return nodes.value.filter(n => {
    if (keyword.value.trim()) {
      const q = keyword.value.trim().toLowerCase();
      const matchName = (n.name || '').toLowerCase().includes(q);
      const matchIp = (n.publicIp || n.hostname || '').toLowerCase().includes(q);
      const meta = getNodeMeta(n.id);
      const matchRemark = (meta.remark || '').toLowerCase().includes(q);
      const matchRegion = (meta.region || '').toLowerCase().includes(q);
      if (!matchName && !matchIp && !matchRemark && !matchRegion) {
        return false;
      }
    }

    if (currentStatusFilter.value === 'online') {
      return isAgentOnline(n) && n.heartbeat?.core_healthy === true;
    }
    if (currentStatusFilter.value === 'core_error') {
      return isAgentOnline(n) && (n.status === 'degraded' || n.heartbeat?.core_healthy === false);
    }
    if (currentStatusFilter.value === 'maintenance') {
      return n.maintenance === true;
    }
    if (currentStatusFilter.value === 'offline') {
      return !isAgentOnline(n) && n.enabled && !n.deletedAt;
    }
    if (currentStatusFilter.value === 'disabled') {
      return !n.enabled && !n.deletedAt;
    }
    return true;
  });
});

// Helper functions for UI cards
const clockNow = ref(Date.now());
let clockTimer: ReturnType<typeof setInterval> | undefined;


function getNodeStatusTone(node: NodeView): string {
  if (node.deletedAt || !node.enabled) return 'disabled';
  if (node.maintenance) return 'maintenance';
  if (node.status === 'pending') return 'pending';
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') return 'core_error';
    if (node.status === 'applying') return 'applying';
    if (node.status === 'rollback') return 'rollback';
    return 'active';
  }
  if (node.status === 'error') return 'core_error';
  return 'offline';
}

function isAgentOnline(node: NodeView): boolean {
  if (!node.lastHeartbeat || !node.enabled || node.deletedAt) return false;
  const hbTime = Date.parse(node.lastHeartbeat);
  if (isNaN(hbTime)) return false;
  if (clockNow.value - hbTime >= 45000) return false;
  return node.status === 'online' || node.status === 'degraded' || node.status === 'applying' || node.status === 'rollback';
}

function statusCapsuleText(node: NodeView): string {
  if (node.deletedAt) return '已删除';
  if (!node.enabled) return '已停用';
  if (node.maintenance) return '维护中';
  if (node.status === 'pending') return '等待登记';
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') return '核心异常';
    if (node.status === 'applying') return '发布中';
    if (node.status === 'rollback') return '回滚中';
    return '在线';
  }
  if (node.status === 'error') return '异常';
  return '离线';
}

function statusCapsuleClass(node: NodeView): string {
  if (node.deletedAt || !node.enabled) {
    return 'bg-neutral-100 text-neutral-500';
  }
  if (node.maintenance) {
    return 'bg-[#FEF6EE] text-[#F79009]';
  }
  if (node.status === 'pending') {
    return 'bg-neutral-100 text-neutral-500';
  }
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') {
      return 'bg-[#FEF6EE] text-[#F79009]';
    }
    if (node.status === 'applying' || node.status === 'rollback') {
      return 'bg-blue-50 text-ios-blue';
    }
    return 'bg-[#E8F8F0] text-[#12B76A]';
  }
  if (node.status === 'error') {
    return 'bg-rose-50 text-ios-red';
  }
  return 'bg-neutral-100 text-neutral-500';
}

function statusDotClass(node: NodeView): string {
  if (node.deletedAt || !node.enabled) return 'bg-neutral-400';
  if (node.maintenance) return 'bg-[#F79009]';
  if (node.status === 'pending') return 'bg-neutral-400';
  if (isAgentOnline(node)) {
    if (node.status === 'degraded') return 'bg-[#F79009]';
    if (node.status === 'applying' || node.status === 'rollback') return 'bg-ios-blue';
    return 'bg-[#12B76A]';
  }
  if (node.status === 'error') return 'bg-ios-red';
  return 'bg-neutral-400';
}

function calculateRunningTime(node: NodeView): string {
  return '—';
}

function formatPercent(val?: number): string {
  return typeof val === 'number' ? `${Math.round(val)}%` : '—';
}

function barColor(val?: number): string {
  if (typeof val !== 'number') return 'bg-neutral-300';
  if (val >= 90) return 'bg-rose-500';
  if (val >= 75) return 'bg-amber-500';
  return 'bg-[#12B76A]';
}

function formatCompactBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null) return '—';
  if (bytes === 0) return '0M';
  if (bytes < 1024 * 1024 * 1024) return `${Math.round(bytes / 1024 / 1024)}M`;
  return `${Math.round(bytes / 1024 / 1024 / 1024)}G`;
}

// Geo Country Flag & Region helpers
const geoTick = ref(0);
let unsubGeo: (() => void) | undefined;

onMounted(async () => {
  loadStoredData();
  {
    try {
      const inboundRes = await proxyAPI.inbounds('', 1, 100).catch(() => ({ items: [], total: 0 }));
      serverInbounds.value = inboundRes.items;
    } catch {}
  }
  unsubGeo = onGeoResolved(() => {
    geoTick.value++;
  });
});

onUnmounted(() => {
  if (unsubGeo) unsubGeo();
  if (clockTimer) clearInterval(clockTimer);
});

function getNodeCountry(node: NodeView) {
  void geoTick.value;
  const meta = getNodeMeta(node.id);
  return resolveCountryInfo(meta.region, node.publicIp, node.name, node.hostname);
}

function getNodeRegion(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return meta.region || '';
}

// Renewal date & price helpers
function getRenewalDate(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return meta.renewDate || '';
}

function getRenewalPriceDisplay(node: NodeView): string {
  const meta = getNodeMeta(node.id);
  return formatRenewPrice(meta);
}

function getRenewalInfo(node: NodeView) {
  const meta = getNodeMeta(node.id);
  return calculateRemainingDays(meta.renewDate);
}

// Monthly traffic
const nodeMonthTraffic = ref<Record<string, { uploadBytes: number; downloadBytes: number }>>({});

function getNodeMonthTraffic(node: NodeView): number | null {
  const item = nodeMonthTraffic.value[node.id];
  return item ? item.uploadBytes + item.downloadBytes : null;
}

// ----------------- Actions (Install, Upgrade, Rollback, Edit, Delete) -----------------
const tokenDialog = ref(false);
const enrollment = ref<Enrollment | null>(null);
const installHost = ref('');
const installHTTPPort = ref(443);
const installCore = ref('xray');
const installAvailable = computed(() => Boolean(enrollment.value?.install?.version && enrollment.value?.install?.caSha256));
const installInputValid = computed(() => /^[A-Za-z0-9.-]+$/.test(installHost.value) && Number.isInteger(installHTTPPort.value) && installHTTPPort.value >= 1 && installHTTPPort.value <= 65535);
const installCommandText = computed(() => {
  const install = enrollment.value?.install;
  if (!install || !installAvailable.value || !installInputValid.value) return '';
  return installCommand({
    version: install.version,
    host: installHost.value,
    httpPort: installHTTPPort.value,
    grpcPort: install.grpcPort,
    caSha256: install.caSha256,
    token: enrollment.value?.enrollmentToken || '',
    core: installCore.value
  });
});

async function openInstallDialog(node: NodeView) {
  if (node.status === 'pending' && !node.deletedAt) {
    try {
      enrollment.value = await nodeAPI.reissueEnrollment(node.id);
      installHost.value = window.location.hostname;
      installHTTPPort.value = Number(window.location.port) || 443;
      installCore.value = 'xray';
      tokenDialog.value = true;
    } catch (e) {
      showToast((e as Error).message, 'error');
    }
    return;
  }
  showToast('该服务器已登记；如需重装或升级请使用 qui-update（见部署文档）', 'info');
}

// Upgrade Dialog
const upgradeDialog = ref(false);
const upgradeTarget = ref<NodeView | null>(null);

function openUpgradeDialog(node: NodeView) {
  upgradeTarget.value = node;
  upgradeDialog.value = true;
}

function confirmUpgrade() {
  upgradeDialog.value = false;
  router.push('/nodes/versions?tab=agents');
}

// Rollback
const rollbackDialog = ref(false);
const rollbackTarget = ref<NodeView | null>(null);
const rollbackChoices = ref<Revision[]>([]);
const rollbackSelectedId = ref<number | null>(null);
const rollbackLoading = ref(false);

async function promptRollback(node: NodeView) {
  rollbackTarget.value = node;
  rollbackChoices.value = [];
  rollbackSelectedId.value = null;
  rollbackLoading.value = true;
  rollbackDialog.value = true;
  try {
    const res = await deploymentAPI.nodeRevisions(node.id);
    const items = res.items || [];
    const successful = items.filter((r: Revision) => r.status === 'success' || r.status === 'succeeded' || r.status === 'applied');
    const currentSuccessful = node.heartbeat?.successful_revision;
    const choices = successful.filter((r: Revision) => {
      if (currentSuccessful && r.id === currentSuccessful) return false;
      return true;
    });
    rollbackChoices.value = choices;
    if (choices.length > 0) {
      rollbackSelectedId.value = choices[0].id;
    }
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    rollbackLoading.value = false;
  }
}

async function executeRollback() {
  if (!rollbackTarget.value || rollbackSelectedId.value == null) return;
  try {
    await deploymentAPI.rollbackNode(rollbackTarget.value.id, rollbackSelectedId.value);
    showToast(`回退到版本 #${rollbackSelectedId.value} 请求已提交`);
    rollbackDialog.value = false;
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// Edit Drawer
const editDrawer = ref(false);
const editingSaving = ref(false);
const editTarget = ref<NodeView | null>(null);
const editForm = reactive({
  name: '',
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  enabled: true,
  allowSubscription: true,
  maintenance: false
});

function openEditDrawer(node: NodeView) {
  editTarget.value = node;
  const meta = getNodeMeta(node.id);
  const parsed = parseRenewPrice(meta.renewPrice, meta.renewCurrency, meta.renewCycle);
  editForm.name = node.name;
  editForm.region = meta.region || '';
  editForm.remark = meta.remark || '';
  editForm.renewDate = meta.renewDate || '';
  editForm.renewCurrency = parsed.currency;
  editForm.renewPrice = parsed.price;
  editForm.renewCycle = parsed.cycle;
  editForm.enabled = node.enabled;
  editForm.allowSubscription = node.allowSubscription;
  editForm.maintenance = node.maintenance;
  editDrawer.value = true;
}

async function saveEdit() {
  if (!editTarget.value || editingSaving.value) return;
  editingSaving.value = true;
  try {
    await nodeAPI.save({
      id: editTarget.value.id,
      name: editForm.name,
      publicIp: editTarget.value.publicIp || '',
      enabled: editForm.enabled,
      allowSubscription: editForm.allowSubscription,
      maintenance: editForm.maintenance,
      outboundSourceIp: editTarget.value.outboundSourceIp || '',
      outboundInterface: editTarget.value.outboundInterface || '',
      hostSourceIpv4: editTarget.value.hostSourceIpv4 || '',
      hostSourceIpv6: editTarget.value.hostSourceIpv6 || ''
    });

    await saveNodeMeta(editTarget.value.id, {
      region: editForm.region.trim(),
      remark: editForm.remark.trim(),
      renewDate: editForm.renewDate,
      renewCurrency: editForm.renewCurrency,
      renewPrice: editForm.renewPrice.trim(),
      renewCycle: editForm.renewCycle
    });

    showToast('服务器设置已更新');
    editDrawer.value = false;
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    editingSaving.value = false;
  }
}

// Delete Dialog
const deleteTarget = ref<NodeView | null>(null);
const deletePreview = ref<DeletionPreview | null>(null);
const deleteDialog = ref(false);
const deleteLoading = ref(false);
const deleteError = ref('');
const deleting = ref(false);
const deleteBlockers = computed(() => deletePreview.value?.blockers ?? []);
const blockerLabels: Record<string, string> = {
  inbound: '入站',
  outbound: '出站',
  outbound_group: '出口组',
  outbound_target: '其他节点的出站',
  proxy_chain: '代理链',
  policy: '分流规则',
  deployment_batch: '进行中的批量发布'
};
function blockerLabel(kind: string) { return blockerLabels[kind] || kind; }

async function promptDelete(node: NodeView) {
  deleteTarget.value = node;
  deletePreview.value = null;
  deleteError.value = '';
  deleteDialog.value = true;
  deleteLoading.value = true;
  try {
    deletePreview.value = await nodeAPI.deletionPreview(node.id);
  } catch (e) {
    deleteError.value = (e as Error).message;
  } finally {
    deleteLoading.value = false;
  }
}

async function confirmDelete() {
  if (!deleteTarget.value || !deletePreview.value || deleting.value) return;
  const mode = deletePreview.value.mode;
  deleting.value = true;
  deleteError.value = '';
  try {
    await nodeAPI.deleteNode(deleteTarget.value.id);
    showToast(mode === 'cancel' ? '节点登记已取消' : '服务器已删除');
    deleteDialog.value = false;
    await load();
  } catch (e) {
    deleteError.value = (e as Error).message;
    const blockers = (e as APIError).details?.blockers;
    if (Array.isArray(blockers) && deletePreview.value) {
      deletePreview.value = { ...deletePreview.value, blockers: blockers as any[] };
    }
  } finally {
    deleting.value = false;
  }
}

// Create Drawer
const createDrawer = ref(false);
const name = ref('');
const createRenewDate = ref('');
const createRenewCurrency = ref('CNY');
const createRenewPrice = ref('');
const createRenewCycle = ref('/年');
const creating = ref(false);

async function create() {
  if (creating.value) return;
  creating.value = true;
  try {
    enrollment.value = await nodeAPI.enroll(name.value);
    installHost.value = window.location.hostname;
    installHTTPPort.value = Number(window.location.port) || 443;
    if (enrollment.value?.node?.id) {
      await saveNodeMeta(enrollment.value.node.id, {
        renewDate: createRenewDate.value,
        renewCurrency: createRenewCurrency.value,
        renewPrice: createRenewPrice.value.trim(),
        renewCycle: createRenewCycle.value
      });
    }
    createDrawer.value = false;
    tokenDialog.value = true;
    name.value = '';
    createRenewDate.value = '';
    createRenewCurrency.value = 'CNY';
    createRenewPrice.value = '';
    createRenewCycle.value = '/年';
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    creating.value = false;
  }
}

// ----------------- Data Loading -----------------
let generation = 0;
async function load() {
  const current = ++generation;
  loading.value = true;
  error.value = '';
  try {
    const nodeResult = await nodeAPI.list(page.value, keyword.value, showDeleted.value);

    await Promise.all(nodeResult.items.map(node => loadNodeMeta(node.id, isOwner.value)));
    if (current === generation) {
      nodes.value = nodeResult.items;
      total.value = nodeResult.total;
    }
    await fetchMonthlyTraffic();
  } catch (e) {
    if (current === generation) error.value = (e as Error).message;
  } finally {
    if (current === generation) loading.value = false;
  }
}

async function fetchMonthlyTraffic() {
  const result = await request<{ items: { nodeId: string; uploadBytes: number; downloadBytes: number }[] }>('/traffic/nodes/monthly');
  nodeMonthTraffic.value = Object.fromEntries(result.items.map(item => [item.nodeId, { uploadBytes: item.uploadBytes, downloadBytes: item.downloadBytes }]));
}

function search() {
  page.value = 1;
  load();
}

function changePage(value: number) {
  page.value = value;
  load();
}

onMounted(() => {
  clockTimer = setInterval(() => { clockNow.value = Date.now(); }, 5000);
  load();
  window.addEventListener('proxy-node-status', load);
});

onUnmounted(() => {
  if (clockTimer) clearInterval(clockTimer);
  window.removeEventListener('proxy-node-status', load);
  generation++;
});


const serverInbounds = ref<Inbound[]>([]);

const route = useRoute();
const router = useRouter();
</script>
