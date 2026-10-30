<template>
  <div class="space-y-6">
    <!-- Top PageHeader with Unified Actions (Aligned with Server Page) -->
    <PageHeader
      title="节点配置"
      parent="节点配置"
      description="统一展示与管理落地节点及中转线路。支持直连地址查看、中转方案多跳追溯，并与画布编辑器实现双向联动。"
    >
      <template #actions>
        <!-- 1. Subscription Manager -->
        <button
          type="button"
          v-if="isOwner"
          @click="openSubModal = true"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <span>机场订阅管理 ({{ proxyNodeStore.subscriptions.length }})</span>
        </button>

        <!-- 2. Import External Node -->
        <button
          type="button"
          v-if="isOwner"
          @click="openImportNodeModal = true"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-8l-4-4m0 0L8 8m4-4v12" />
          </svg>
          <span>导入节点</span>
        </button>

        <!-- 3. Primary Action: Create Self-Hosted Node -->
        <button
          type="button"
          @click="openCreateNodeModal"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-[0_2px_8px_rgba(0,113,227,0.28)]"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
          </svg>
          <span>新建节点</span>
        </button>
      </template>
    </PageHeader>

    <!-- Search & Filter Bar (Aligned with Server & User Pages) -->
    <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
      <!-- Search Form -->
      <form @submit.prevent="applyFilter" class="flex flex-wrap items-center gap-3 flex-1">
        <div class="relative flex-1 min-w-[200px] max-w-md">
          <svg class="w-4 h-4 text-ios-tertiary absolute left-3 top-1/2 -translate-y-1/2 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            v-model="searchKeyword"
            placeholder="搜索节点名称、服务器地址或端口…"
            class="ios-search-input"
          />
          <button
            v-if="searchKeyword"
            type="button"
            @click="searchKeyword = ''; applyFilter()"
            class="absolute right-2.5 top-1/2 -translate-y-1/2 text-ios-tertiary hover:text-ios-label p-0.5 rounded-full hover:bg-black/5 transition-colors"
            title="清空"
          >
            <svg class="w-3.5 h-3.5" viewBox="0 0 20 20" fill="currentColor">
              <path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd" />
            </svg>
          </button>
        </div>

        <button class="ios-search-btn shrink-0">查询</button>

        <!-- Filter 1: Source -->
        <select v-model="filterSource" @change="applyFilter" class="form-input-base !w-auto text-xs py-2">
          <option value="all">全部来源</option>
          <option value="self_hosted">服务器自建</option>
          <option value="subscription">机场订阅</option>
          <option value="manual">单独导入</option>
        </select>

        <!-- Filter 2: Protocol -->
        <select v-model="filterProtocol" @change="applyFilter" class="form-input-base !w-auto text-xs py-2">
          <option value="all">全部协议</option>
          <option value="vless">VLESS</option>
          <option value="vmess">VMess</option>
          <option value="shadowsocks">Shadowsocks</option>
          <option value="trojan">Trojan</option>
          <option value="hysteria2">Hysteria2</option>
          <option value="tuic">TUIC</option>
          <option value="wireguard">WireGuard</option>
          <option value="socks5">Socks5</option>
          <option value="http">HTTP</option>
        </select>

        <!-- Filter 3: Status -->
        <select v-model="filterStatus" @change="applyFilter" class="form-input-base !w-auto text-xs py-2">
          <option value="all">全部状态</option>
          <option value="online">在线</option>
          <option value="untested">未检测</option>
          <option value="offline">离线</option>
        </select>
      </form>

      <!-- Right: View Mode Toggle & Tools (100% Aligned with Server Page) -->
      <div class="flex items-center gap-2 shrink-0 self-end sm:self-auto">
        <!-- View Switcher (List vs Grid, Same as Server Page) -->
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

        <!-- Speed Test -->
        <button
          type="button"
          @click="testAllNodes"
          :disabled="testingAll"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <svg
            class="w-4 h-4 text-emerald-600"
            :class="testingAll ? 'animate-spin' : ''"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
          </svg>
          <span class="hidden sm:inline">{{ testingAll ? '检查中…' : '一键连通性检查' }}</span>
        </button>

        <button
          type="button"
          @click="loadData"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
          title="刷新列表"
        >
          <svg class="w-4 h-4 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span class="hidden sm:inline">刷新</span>
        </button>
      </div>
    </div>

    <!-- Loading Skeleton & Empty State -->
    <LoadingSkeleton v-if="loading && !allNodes.length" />
    <EmptyState
      v-else-if="!filteredNodes.length"
      :title="searchKeyword || filterSource !== 'all' || filterProtocol !== 'all' ? '未找到匹配的代理节点' : '暂无代理节点'"
      :description="searchKeyword ? '请尝试调整筛选条件或搜索关键词。' : '您可以点击右上角“新建节点”为服务器添加自建协议入站，或点击“导入节点”导入外部节点。'"
    />

    <!-- ================= Mode 1: Table List View (Aligned with Server Page DataTable) ================= -->
    <DataTable v-else-if="displayMode === 'list'">
      <template #header>
        <th>节点名称</th>
        <th>协议与传输</th>
        <th>直连地址</th>
        <th>中转线路 (数量)</th>
        <th>真实状态 / 延迟</th>
        <th class="text-right pr-6">操作</th>
      </template>
      <template #body>
        <tr v-for="node in pagedNodes" :key="node.id" class="hover:bg-black/[0.015] transition-colors">
          <!-- 1. Node Name & Source -->
          <td class="p-4">
            <div class="flex items-center gap-2.5">
              <!-- Flag / Protocol Badge -->
              <span class="text-xl shrink-0 select-none leading-none cursor-default" :title="getCountryInfo(node).name">
                {{ getCountryInfo(node).flag }}
              </span>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <span class="font-bold text-[15px] text-neutral-900 truncate tracking-tight" :title="node.name">
                    {{ node.name }}
                  </span>
                  <!-- Source Capsule -->
                  <span class="px-1.5 py-0.2 rounded text-[10px] font-semibold shrink-0" :class="sourceCapsuleColor(node.sourceType)">
                    {{ node.sourceName }}
                  </span>
                </div>
                <p class="text-xs text-neutral-400 font-mono mt-0.5 truncate">
                  {{ node.supportTcp ? 'TCP' : '' }} {{ node.supportUdp ? '· UDP' : '' }}
                </p>
              </div>
            </div>
          </td>

          <!-- 2. Protocol & Transport -->
          <td>
            <div class="space-y-0.5">
              <span class="px-2 py-0.5 rounded-lg text-xs font-mono font-semibold uppercase bg-black/[0.05] text-neutral-800">
                {{ node.protocol }}
              </span>
              <div class="text-[11px] text-neutral-400 font-mono">
                {{ node.settings?.transport || 'tcp' }}{{ node.settings?.security ? ' + ' + node.settings.security : '' }}
              </div>
            </div>
          </td>

          <!-- 3. Direct Server Address & Port -->
          <td class="font-mono text-xs">
            <div v-if="node.onlyViaRelay" class="text-neutral-400 italic">
              仅中转提供 (无直连入口)
            </div>
            <div v-else class="flex items-center gap-1.5 text-neutral-700">
              <span class="truncate max-w-[180px]" :title="node.server + ':' + node.port">
                {{ node.server }}:{{ node.port }}
              </span>
              <button
                type="button"
                @click="copyAddress(node.server + ':' + node.port)"
                class="text-neutral-400 hover:text-ios-blue p-0.5 transition-colors"
                title="复制地址"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                </svg>
              </button>
            </div>
          </td>

          <!-- 4. Relay Routes Summary Column -->
          <td class="text-xs">
            <div v-if="!getNodeRoutes(node).length" class="text-neutral-400">
              无中转 (直连)
            </div>
            <div v-else class="space-y-1">
              <div
                v-for="(r, idx) in getNodeRoutes(node)"
                :key="idx"
                class="flex items-center gap-1.5 cursor-pointer text-neutral-800 hover:text-ios-blue"
                @click="goToEditRelayRoute(r)"
                :title="'点击前往画布编辑此线路: ' + r.pathDisplay"
              >
                <span class="font-semibold truncate max-w-[150px]">{{ r.pathDisplay }}</span>
                <span
                  class="px-1 py-0.2 rounded text-[9px] font-bold shrink-0"
                  :class="!r.published ? 'bg-neutral-100 text-neutral-600' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800')"
                >
                  {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '未发布修改' : '已发布') }}
                </span>
              </div>
            </div>
          </td>

          <!-- 5. Real Status & Latency (Same as Server Page) -->
          <td>
            <div class="flex items-center gap-2">
              <span
                class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold shrink-0 select-none"
                :class="statusCapsuleClass(node)"
              >
                <span class="w-1.5 h-1.5 rounded-full" :class="statusDotClass(node)"></span>
                <span>{{ statusCapsuleText(node) }}</span>
              </span>
              <span v-if="node.status === 'online' && node.latencyMs" class="text-xs font-mono font-bold text-emerald-600">
                {{ node.latencyMs }} ms
              </span>
            </div>
          </td>

          <!-- 6. Actions (Aligned with Server Page Buttons) -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <button
              type="button"
              @click="handleAddRelay(node)"
              class="ios-action-btn-primary"
            >
              添加中转
            </button>
            <button
              type="button"
              @click="openNodeDetail(node)"
              class="ios-action-btn-secondary"
            >
              详情
            </button>
            <button
              type="button"
              @click="openEditNode(node)"
              class="ios-action-btn-secondary"
            >
              编辑
            </button>
            <button
              type="button"
              @click="deleteNode(node)"
              class="ios-action-btn-danger"
            >
              删除
            </button>
          </td>
        </tr>
      </template>

      <template #footer>
        <Pagination
          :total="filteredNodes.length"
          :current-page="currentPage"
          :page-size="pageSize"
          @update:page="currentPage = $event"
        />
      </template>
    </DataTable>

    <!-- ================= Mode 2: Grid Card View (100% Aligned with Server Page Card Layout) ================= -->
    <div
      v-else-if="displayMode === 'grid'"
      class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-5"
    >
      <div
        v-for="node in pagedNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <div class="space-y-3.5">
          <!-- Row 1: Flag · Name · Protocol & Server-style Status Capsule -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="getCountryInfo(node).name"
              >
                {{ getCountryInfo(node).flag }}
              </span>
              <span
                class="font-bold text-[17px] text-neutral-900 truncate tracking-tight"
                :title="node.name"
              >
                {{ node.name }}
              </span>
              <span class="rounded bg-black/[0.04] px-1.5 py-0.5 text-[10px] font-mono font-bold uppercase text-neutral-700 shrink-0">
                {{ node.protocol }}
              </span>
            </div>

            <!-- Status Badge Capsule (Exact same class as Server Page) -->
            <span
              class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold shrink-0 select-none"
              :class="statusCapsuleClass(node)"
            >
              <span class="w-1.5 h-1.5 rounded-full" :class="statusDotClass(node)"></span>
              <span>{{ statusCapsuleText(node) }}</span>
            </span>
          </div>

          <!-- Row 2: Sub-info (Source · Transport · Latency) -->
          <div class="text-[12px] text-neutral-400 font-mono tracking-tight flex items-center gap-1.5 flex-wrap">
            <span class="px-1.5 py-0.2 rounded text-[10px] font-semibold" :class="sourceCapsuleColor(node.sourceType)">
              {{ node.sourceName }}
            </span>
            <span class="text-neutral-300">·</span>
            <span>{{ node.settings?.transport || 'tcp' }}{{ node.settings?.security ? ' + ' + node.settings.security : '' }}</span>
            <span v-if="node.status === 'online' && node.latencyMs" class="text-neutral-300">·</span>
            <span v-if="node.status === 'online' && node.latencyMs" class="font-bold text-emerald-600">
              {{ node.latencyMs }} ms
            </span>
          </div>

          <!-- Row 3: Direct Address Entrance (Compact & Clean) -->
          <div>
            <div v-if="node.onlyViaRelay" class="bg-neutral-50 rounded-xl px-3 py-1.5 border border-black/[0.03] text-xs text-neutral-400 font-mono italic">
              直连: 仅通过中转提供 · 无直连入口
            </div>
            <div v-else class="bg-[#F8F9FA] rounded-xl px-3 py-1.5 border border-black/[0.03] flex items-center justify-between text-xs font-mono">
              <div class="flex items-center gap-1.5 min-w-0 truncate text-neutral-700">
                <span class="text-neutral-400 font-sans text-[11px] shrink-0 font-medium">直连:</span>
                <span class="truncate font-medium select-all" :title="node.server + ':' + node.port">
                  {{ node.server }}:{{ node.port }}
                </span>
              </div>
              <button
                type="button"
                @click="copyAddress(node.server + ':' + node.port)"
                class="text-neutral-400 hover:text-ios-blue p-1 rounded-md hover:bg-black/5 transition-colors shrink-0 ml-1"
                title="复制直连地址"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                </svg>
              </button>
            </div>
          </div>

          <!-- Row 4: Relay Routes (Accordion Style, Same as Server Latency Test Point Cards) -->
          <div class="space-y-1.5 pt-0.5">
            <div
              @click="toggleRelayCollapse(node.id, getNodeRoutes(node).length)"
              class="flex items-center justify-between text-[11px] cursor-pointer hover:bg-black/[0.02] p-1 rounded-lg transition-colors select-none"
            >
              <div class="flex items-center gap-1.5">
                <span class="text-neutral-500 font-bold">中转线路 ({{ getNodeRoutes(node).length }})</span>
                <span
                  v-if="getNodeRoutes(node).some(r => r.hasUnpublishedChanges)"
                  class="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"
                  title="方案存在未发布修改"
                ></span>
              </div>
              <div class="flex items-center gap-1 text-neutral-400 font-mono text-[10px]">
                <span>{{ isRelayCollapsed(node.id, getNodeRoutes(node).length) ? '展开' : '收起' }}</span>
                <svg
                  class="w-3 h-3 transition-transform duration-200"
                  :class="isRelayCollapsed(node.id, getNodeRoutes(node).length) ? '' : 'rotate-180'"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                </svg>
              </div>
            </div>

            <!-- Expanded Routes Content -->
            <div v-show="!isRelayCollapsed(node.id, getNodeRoutes(node).length)" class="space-y-1.5">
              <div v-if="!getNodeRoutes(node).length" class="bg-[#F8F9FA] rounded-xl p-2 text-center text-[11px] text-neutral-400 border border-black/[0.03]">
                无中转线路 · 支持直连接入
              </div>
              <div
                v-for="(r, rIdx) in getNodeRoutes(node)"
                :key="r.connectionId + '-' + rIdx"
                class="bg-[#F8F9FA] rounded-xl p-2.5 border border-black/[0.03] flex items-center justify-between gap-2 hover:bg-[#F0F2F5] transition-colors"
              >
                <div class="min-w-0 flex-1">
                  <div class="font-bold text-xs text-neutral-800 truncate" :title="r.pathDisplay">
                    {{ r.pathDisplay }}
                  </div>
                  <div class="flex items-center gap-1.5 mt-0.5">
                    <span
                      class="px-1.5 py-0.2 rounded text-[10px] font-semibold"
                      :class="r.executionLocation === 'client' ? 'bg-blue-50 text-ios-blue' : 'bg-purple-50 text-purple-700'"
                    >
                      {{ r.executionLocation === 'client' ? '客户端代理链' : '服务器执行' }}
                    </span>
                    <span class="text-neutral-300">·</span>
                    <span
                      class="px-1.5 py-0.2 rounded text-[10px] font-semibold"
                      :class="!r.published ? 'bg-neutral-200 text-neutral-700' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-[#E8F8F0] text-[#12B76A]')"
                    >
                      {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '有未发布修改' : '已发布') }}
                    </span>
                  </div>
                </div>

                <button
                  type="button"
                  @click.stop="goToEditRelayRoute(r)"
                  class="px-2 py-1 text-xs font-bold text-ios-blue hover:bg-ios-blue/10 rounded-lg transition-colors shrink-0"
                  title="在画布中定位并编辑该线路"
                >
                  编辑
                </button>
              </div>
            </div>
          </div>
        </div>

        <!-- Row 5: Action Grid Buttons (Exact Same Style as Server Card Bottom Grid) -->
        <div class="pt-2 border-t border-black/[0.04] grid grid-cols-3 gap-1.5">
          <!-- 1. 添加中转 -->
          <button
            type="button"
            @click="handleAddRelay(node)"
            class="py-1.5 rounded-xl bg-[#EFF8FF] text-[#175CD3] hover:bg-[#D1E9FF] font-semibold text-xs text-center transition-colors flex items-center justify-center gap-1"
            title="自动创建草稿方案并将当前节点设为落地出口"
          >
            <span>+ 中转</span>
          </button>

          <!-- 2. 查看详情 -->
          <button
            type="button"
            @click="openNodeDetail(node)"
            class="py-1.5 rounded-xl bg-[#F8F9FA] text-neutral-700 hover:bg-[#EBECEF] font-semibold text-xs text-center transition-colors flex items-center justify-center"
          >
            详情
          </button>

          <!-- 3. 更多操作下拉 -->
          <div class="relative">
            <button
              type="button"
              @click.stop="toggleMoreDropdown(node.id)"
              class="w-full py-1.5 rounded-xl bg-[#F8F9FA] text-neutral-700 hover:bg-[#EBECEF] font-semibold text-xs text-center transition-colors flex items-center justify-center"
              title="更多操作"
            >
              ···
            </button>

            <div
              v-if="activeMoreId === node.id"
              class="absolute right-0 bottom-full mb-1.5 w-32 bg-white rounded-xl shadow-lg border border-black/[0.08] py-1 z-30 text-xs"
            >
              <button
                type="button"
                @click="openEditNode(node); activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-black/5 text-ios-label"
              >
                编辑节点
              </button>
              <button
                type="button"
                @click="testSingleNode(node); activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-black/5 text-ios-label"
              >
                连通性测试
              </button>
              <button
                type="button"
                @click="deleteNode(node); activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-rose-50 text-rose-600"
              >
                删除节点
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <!-- Pagination for Cards -->
    <div v-if="displayMode === 'grid'" class="pt-2">
      <Pagination
        :total="filteredNodes.length"
        :current-page="currentPage"
        :page-size="pageSize"
        @update:page="currentPage = $event"
      />
    </div>

    <!-- Modal 0: Node Details & Topology Restorability Evaluation -->
    <AppDialog
      v-model="openDetailModal"
      :title="'节点详情 · ' + (selectedDetailNode ? selectedDetailNode.name : '')"
      :show-confirm="false"
    >
      <div v-if="selectedDetailNode" class="space-y-4 text-xs">
        <!-- Node Metadata Section -->
        <div class="p-3 bg-[#F5F5F7] rounded-xl space-y-2">
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">节点名称</span>
            <span class="font-bold text-neutral-900">{{ selectedDetailNode.name }}</span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">协议类型</span>
            <span class="font-mono font-bold uppercase text-ios-blue">{{ selectedDetailNode.protocol }}</span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">直连接入地址</span>
            <span class="font-mono text-neutral-900">
              {{ selectedDetailNode.onlyViaRelay ? '仅通过中转提供 (无直连入口)' : selectedDetailNode.server + ':' + selectedDetailNode.port }}
            </span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">传输与安全</span>
            <span class="font-mono text-neutral-900">
              {{ selectedDetailNode.settings?.transport || 'tcp' }} + {{ selectedDetailNode.settings?.security || 'none' }}
            </span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">网络协议支持</span>
            <span class="font-mono text-neutral-900">
              {{ selectedDetailNode.supportTcp ? 'TCP' : '' }} {{ selectedDetailNode.supportUdp ? '· UDP' : '' }}
            </span>
          </div>
        </div>

        <!-- Relay Routes Restorability Section -->
        <div class="space-y-2.5">
          <h4 class="font-bold text-xs text-neutral-900 flex items-center gap-1.5">
            <span>关联中转线路还原评估</span>
            <span class="text-neutral-400 font-normal">({{ getNodeRoutes(selectedDetailNode).length }} 条线路)</span>
          </h4>

          <div v-if="!getNodeRoutes(selectedDetailNode).length" class="p-3 bg-neutral-50 rounded-xl text-neutral-400">
            当前节点尚未配置中转线路。
          </div>
          <div v-else class="space-y-2">
            <div
              v-for="(r, idx) in getNodeRoutes(selectedDetailNode)"
              :key="idx"
              class="p-3 rounded-xl border border-black/[0.06] bg-white space-y-2 shadow-2xs"
            >
              <div class="flex items-center justify-between">
                <span class="font-bold text-neutral-900 text-xs">{{ r.pathDisplay }}</span>
                <span
                  class="px-2 py-0.5 rounded text-[10px] font-semibold"
                  :class="!r.published ? 'bg-neutral-100 text-neutral-600' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800')"
                >
                  {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '有未发布修改' : '已发布') }}
                </span>
              </div>

              <!-- Restorable Info -->
              <div v-if="r.canRestoreToCanvas" class="p-2 rounded-lg bg-emerald-50/70 border border-emerald-200/60 text-emerald-800 text-[11px] flex items-center justify-between">
                <div class="flex items-center gap-1.5">
                  <svg class="w-4 h-4 text-emerald-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                  </svg>
                  <span>配置完整，已准备可编辑画布拓扑。</span>
                </div>
                <button
                  type="button"
                  @click="openDetailModal = false; goToEditRelayRoute(r)"
                  class="px-2 py-0.5 rounded bg-emerald-600 text-white font-semibold hover:bg-emerald-700 transition-colors"
                >
                  去画布编辑
                </button>
              </div>
              <div v-else class="p-2 rounded-lg bg-amber-50/80 border border-amber-200/80 text-amber-800 text-[11px] space-y-1">
                <div class="flex items-center gap-1.5 font-bold">
                  <svg class="w-4 h-4 text-amber-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                  </svg>
                  <span>无法还原为可编辑拓扑画布</span>
                </div>
                <p class="text-amber-700">
                  原因: {{ r.unrestorableReason || '该线路包含未知中转参数或属于第三方固定闭源订阅链，未生成不完整的编辑拓扑。' }}
                </p>
              </div>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>

    <NodeEditorDrawer v-if="openSelfHostedModal" :inbound="editingInbound" @close="openSelfHostedModal = false" @saved="loadData" />

    <!-- Modal 2: Import External Node -->
    <AppDialog
      v-model="openImportNodeModal"
      :title="editingNode ? '编辑单独导入节点' : '导入外部代理节点'"
      :show-confirm="true"
      :confirm-text="editingNode ? '保存修改' : '确认导入'"
      @confirm="saveManualNodeModal"
    >
      <div class="space-y-4 text-sm">
        <div class="space-y-1.5 bg-blue-50/60 p-3 rounded-xl border border-blue-100">
          <label class="block text-xs font-bold text-ios-blue">导入内容（URI、Base64 或 Clash YAML；凭据加密保存）</label>
          <div class="flex gap-2">
            <textarea
              v-model="quickLinkInput"
              autocomplete="off"
              placeholder="粘贴包含凭据的分享链接或订阅内容；不支持的条目会省略"
              class="form-input-base text-xs flex-1 font-mono"
            ></textarea>
            <button type="button" @click="handleQuickParse" class="ios-btn-primary !py-1 text-xs shrink-0">
              解析
            </button>
          </div>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <label class="block text-xs font-medium text-ios-label">
            节点名称
            <input v-model="nodeForm.name" placeholder="例如 香港 01 · 专线" required class="form-input-base mt-1" />
          </label>
          <label class="block text-xs font-medium text-ios-label">
            协议类型
            <select v-model="nodeForm.protocol" class="form-input-base mt-1">
              <option value="vless">VLESS</option>
              <option value="vmess">VMess</option>
              <option value="shadowsocks">Shadowsocks</option>
              <option value="trojan">Trojan</option>
              <option value="hysteria2">Hysteria2</option>
              <option value="tuic">TUIC</option>
              <option value="socks5">Socks5</option>
              <option value="http">HTTP</option>
            </select>
          </label>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <label class="block text-xs font-medium text-ios-label sm:col-span-2">
            服务器地址 / 域名
            <input v-model="nodeForm.server" placeholder="hk01.example.com" required class="form-input-base mt-1 font-mono" />
          </label>
          <label class="block text-xs font-medium text-ios-label">
            端口
            <input v-model.number="nodeForm.port" type="number" min="1" max="65535" required class="form-input-base mt-1 font-mono" />
          </label>
        </div>

        <div class="flex items-center gap-4 text-xs font-medium text-ios-label pt-1">
          <label class="flex items-center gap-1.5 cursor-pointer">
            <input v-model="nodeForm.supportTcp" type="checkbox" class="rounded text-ios-blue border-black/20" />
            <span>支持 TCP</span>
          </label>
          <label class="flex items-center gap-1.5 cursor-pointer">
            <input v-model="nodeForm.supportUdp" type="checkbox" class="rounded text-ios-blue border-black/20" />
            <span>支持 UDP</span>
          </label>
        </div>
      </div>
    </AppDialog>

    <!-- Modal 3: Subscription Manager -->
    <AppDialog
      v-model="openSubModal"
      title="机场订阅管理"
      :show-confirm="false"
    >
      <div class="space-y-4 text-sm">
        <div class="bg-black/[0.02] p-3.5 rounded-2xl border border-black/[0.05] space-y-3">
          <h4 class="text-xs font-bold text-ios-label">添加新的机场订阅</h4>
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-2">
            <input v-model="newSubName" placeholder="订阅备注 (如 某某机场)" class="form-input-base text-xs" />
            <input v-model="newSubUrl" placeholder="订阅链接 (仅 HTTPS)" type="password" autocomplete="off" class="form-input-base text-xs sm:col-span-2 font-mono" />
          </div>
          <label class="block text-xs">授权用户（多选；未授权不出现在订阅中）<select v-model="grantUserIds" multiple class="form-input-base"><option v-for="u in importUsers" :key="u.id" :value="u.id">{{ u.displayName || u.username }}</option></select></label>
          <label class="block text-xs">刷新间隔（秒）<input v-model.number="refreshSeconds" type="number" min="60" max="2592000" class="form-input-base" /></label>
          <div class="flex justify-end">
            <button type="button" @click="addNewSubscription" class="ios-btn-primary !py-1 text-xs">
              添加订阅并拉取
            </button>
          </div>
        </div>

        <!-- Subscription List -->
        <div class="space-y-2 max-h-[300px] overflow-y-auto">
          <div
            v-for="sub in proxyNodeStore.subscriptions"
            :key="sub.id"
            class="p-3 bg-white rounded-xl border border-black/[0.06] flex items-center justify-between"
          >
            <div>
              <div class="font-bold text-xs text-ios-label">{{ sub.name }}</div>
              <div class="text-[11px] text-ios-tertiary font-mono truncate max-w-[260px]">{{ sub.url }}</div>
              <label class="block text-xs">授权用户<select v-model="sub.userIds" multiple class="form-input-base" @change="saveSourceGrants(sub)"><option v-for="u in importUsers" :key="u.id" :value="u.id">{{ u.displayName || u.username }}</option></select></label>
              <div class="text-xs text-amber-600">{{ sub.lastError }}</div>
              <div class="text-[10px] text-ios-tertiary mt-0.5">
                节点: {{ sub.nodeCount }} 个 · 已用: {{ sub.usedTraffic || '—' }} / {{ sub.totalTraffic || '—' }} · {{ sub.lastStatus }}
              </div>
            </div>
            <div class="flex items-center gap-2">
              <button
                type="button"
                @click="updateSubscription(sub)"
                class="ios-action-btn-secondary"
              >
                更新
              </button>
              <button
                type="button"
                @click="deleteSubscription(sub.id)"
                class="ios-action-btn-danger"
              >
                删除
              </button>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted } from 'vue';
import { useRouter } from 'vue-router';
import { authState } from '../../../app/auth';
import { userAPI, type ProxyUser } from '../../users/api';
import { importAPI, loadImports } from '../importApi';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import AppDialog from '../../../components/AppDialog.vue';
import NodeEditorDrawer from '../components/node-editor/NodeEditorDrawer.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
import { showToast } from '../../../app/api';
import { request } from '../../../app/apiClient';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { deploymentAPI } from '../../system/deploymentApi';
import { proxyAPI, type Inbound, loadProtocolSupport, choices } from '../api';
import { resolveCountryInfo, type CountryInfo } from '../../nodes/nodeMeta';
import {
  proxyNodeStore,
  loadStoredData,
  saveManualNodes,
  saveSubscriptions,
  parseProxyLink,
  getNodeRelayRoutes,
  createDraftSchemeForTargetNode,
  loadCanvasSchemes,
  buildProxyLink,
  type ProxyNodeItem,
  type SubscriptionItem,
  type NodeRelayRoute
} from '../proxyNodes';

const router = useRouter();
const loading = ref(false);

// Display Mode: 'list' (Table, Default aligned with Server page) or 'grid' (Cards)
const displayMode = ref<'grid' | 'list'>(
  (localStorage.getItem('qui_proxy_display_mode') as 'grid' | 'list') || 'list'
);

function setDisplayMode(mode: 'grid' | 'list') {
  displayMode.value = mode;
  localStorage.setItem('qui_proxy_display_mode', mode);
}

const servers = ref<NodeView[]>([]);
const serverInbounds = ref<Inbound[]>([]);
const editingInbound = ref<Inbound | null>(null);

// Filter states
const searchKeyword = ref('');
const filterSource = ref<'all' | 'self_hosted' | 'subscription' | 'manual'>('all');
const filterProtocol = ref<string>('all');
const filterStatus = ref<'all' | 'online' | 'untested' | 'offline'>('all');

// Collapsed states for relay routes accordion in card view
const collapsedRelayState = reactive<Record<string, boolean>>({});

function isRelayCollapsed(nodeId: string, routeCount: number): boolean {
  if (collapsedRelayState[nodeId] !== undefined) {
    return collapsedRelayState[nodeId];
  }
  // Default: if has routes, expand; if 0 routes, keep collapsed to stay compact
  return routeCount === 0;
}

function toggleRelayCollapse(nodeId: string, routeCount: number) {
  collapsedRelayState[nodeId] = !isRelayCollapsed(nodeId, routeCount);
}

// More Actions Dropdown
const activeMoreId = ref<string | null>(null);
function toggleMoreDropdown(nodeId: string) {
  if (activeMoreId.value === nodeId) {
    activeMoreId.value = null;
  } else {
    activeMoreId.value = nodeId;
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('click', () => {
    activeMoreId.value = null;
  });
}

// Modals
const openDetailModal = ref(false);
const selectedDetailNode = ref<ProxyNodeItem | null>(null);

function openNodeDetail(node: ProxyNodeItem) {
  selectedDetailNode.value = node;
  openDetailModal.value = true;
}

const openSelfHostedModal = ref(false);
const openImportNodeModal = ref(false);
const isOwner = computed(() => authState.currentUser?.role === 'Owner');
const importUsers = ref<ProxyUser[]>([]);
const grantUserIds = ref<string[]>([]);
const refreshSeconds = ref(3600);

const editingNode = ref<ProxyNodeItem | null>(null);
const quickLinkInput = ref('');
const nodeForm = reactive({
  name: '',
  protocol: 'vless',
  server: '',
  port: 443,
  supportTcp: true,
  supportUdp: true
});

const openSubModal = ref(false);
const newSubName = ref('');
const newSubUrl = ref('');

// Pagination
const currentPage = ref(1);
const pageSize = 12;

// Load real data
async function loadData() {
  loading.value = true;
  try {
    // Keep old browser data intact, but never mix its ungranted entries into
    // the server-owned U5 list.
    proxyNodeStore.manualNodes = [];
    proxyNodeStore.subscriptions = [];
    const [nodeRes, inboundRes] = await Promise.all([
      choices((page, size) => nodeAPI.list(page, '')),
      choices((page, size) => proxyAPI.inbounds('', page, size))
    ]);
    servers.value = nodeRes;
    serverInbounds.value = inboundRes;
    // Relay routes shown on this page come from the schemes the canvas stores on
    // the server, so they must be loaded here too.
    if (isOwner.value) { await loadImports(); importUsers.value = await choices((page, size) => userAPI.list(page, '', size)); }
    await loadCanvasSchemes({ servers: servers.value, inbounds: serverInbounds.value, imported: proxyNodeStore.manualNodes });
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    loading.value = false;
  }
}

function getTcpUdpSupport(protocol: string) {
  const p = (protocol || '').toLowerCase();
  if (p === 'hysteria2' || p === 'tuic' || p === 'wireguard') {
    return { supportTcp: false, supportUdp: true };
  }
  if (p === 'http') {
    return { supportTcp: true, supportUdp: false };
  }
  return { supportTcp: true, supportUdp: true };
}

// Map self-hosted server inbounds into ProxyNodeItem with clean naming
const selfHostedNodes = computed<ProxyNodeItem[]>(() => {
  const result: ProxyNodeItem[] = [];
  for (const srv of servers.value) {
    if (!srv.enabled || srv.deletedAt) continue;
    const inbounds = serverInbounds.value.filter(ib => ib.nodeId === srv.id);
    if (inbounds.length > 0) {
      for (const ib of inbounds) {
        let cleanName = (ib.name || '').trim();
        if (cleanName.startsWith(srv.name)) {
          cleanName = cleanName.slice(srv.name.length).replace(/^[·\s\-_]+/, '').trim();
        }
        const finalDisplayName = cleanName ? `${srv.name} · ${cleanName}` : `${srv.name} · ${ib.protocol.toUpperCase()}`;
        const name = ib.internalRelay ? finalDisplayName + ' (内部中转)' : (ib.enabled ? finalDisplayName : finalDisplayName + ' (已停用)');
        const status = (srv.status === 'online' || srv.status === 'degraded') && ib.enabled ? 'online' : 'offline';
        const tu = getTcpUdpSupport(ib.protocol);

        result.push({
          id: `self-${srv.id}-${ib.id}`,
          name: name,
          sourceType: 'self_hosted',
          sourceName: srv.name,
          sourceId: srv.id,
          inboundId: ib.id,
          server: srv.publicIp || srv.hostname || '127.0.0.1',
          port: ib.port || 443,
          protocol: ib.protocol || 'vless',
          status: status,
          latencyMs: undefined,
          supportTcp: tu.supportTcp,
          supportUdp: tu.supportUdp,
          settings: ib.settings as any,
          createdAt: ib.createdAt || srv.createdAt,
          onlyViaRelay: ib.internalRelay
        });
      }
    }
  }
  return result;
});

// All aggregated nodes
const allNodes = computed<ProxyNodeItem[]>(() => {
  return [...selfHostedNodes.value, ...proxyNodeStore.manualNodes];
});

// Filtered nodes
const filteredNodes = computed<ProxyNodeItem[]>(() => {
  return allNodes.value.filter(node => {
    if (searchKeyword.value.trim()) {
      const q = searchKeyword.value.trim().toLowerCase();
      const matchName = (node.name || '').toLowerCase().includes(q);
      const matchServer = (node.server || '').toLowerCase().includes(q);
      const matchPort = String(node.port).includes(q);
      const matchSource = (node.sourceName || '').toLowerCase().includes(q);
      if (!matchName && !matchServer && !matchPort && !matchSource) return false;
    }
    if (filterSource.value !== 'all' && node.sourceType !== filterSource.value) {
      return false;
    }
    if (filterProtocol.value !== 'all' && node.protocol.toLowerCase() !== filterProtocol.value.toLowerCase()) {
      return false;
    }
    if (filterStatus.value !== 'all') {
      if (filterStatus.value === 'offline' && (node.status === 'offline' || node.status === 'error')) {
        // match
      } else if (node.status !== filterStatus.value) {
        return false;
      }
    }
    return true;
  });
});

const pagedNodes = computed(() => {
  const start = (currentPage.value - 1) * pageSize;
  return filteredNodes.value.slice(start, start + pageSize);
});

function applyFilter() {
  currentPage.value = 1;
}

// Relay routes linkage helpers
function getNodeRoutes(node: ProxyNodeItem): NodeRelayRoute[] {
  return getNodeRelayRoutes(node, proxyNodeStore.schemes);
}

function goToEditRelayRoute(route: NodeRelayRoute) {
  router.push({
    path: '/proxy/relay',
    query: {
      schemeId: route.schemeId,
      focusConnId: route.connectionId,
      focusInstId: route.targetInstanceId
    }
  });
}

async function handleAddRelay(node: ProxyNodeItem) {
  try {
    const { scheme, exitInstanceId } = await createDraftSchemeForTargetNode(node, { servers: servers.value, inbounds: serverInbounds.value, imported: proxyNodeStore.manualNodes });
    showToast(`已创建草稿，自动将 [${node.name}] 设为落地出口`, 'success');
    router.push({
      path: '/proxy/relay',
      query: {
        schemeId: scheme.id,
        focusInstId: exitInstanceId
      }
    });
  } catch (error) {
    showToast(`创建中转草稿失败: ${(error as Error).message}`, 'error');
  }
}

// Flag resolution helper
function getCountryInfo(node: ProxyNodeItem): CountryInfo {
  return resolveCountryInfo(node.name || node.server);
}

// Styling helpers (Exact same status tones as Server Page)
function sourceCapsuleColor(sourceType: string): string {
  if (sourceType === 'self_hosted') return 'bg-emerald-100 text-emerald-800';
  if (sourceType === 'subscription') return 'bg-purple-100 text-purple-800';
  return 'bg-blue-100 text-ios-blue';
}

function statusCapsuleClass(node: ProxyNodeItem): string {
  if (node.status === 'online') {
    return 'bg-[#E8F8F0] text-[#12B76A]';
  }
  return 'bg-neutral-100 text-neutral-500';
}

function statusDotClass(node: ProxyNodeItem): string {
  if (node.status === 'online') return 'bg-[#12B76A]';
  return 'bg-neutral-400';
}

function statusCapsuleText(node: ProxyNodeItem): string {
  if (node.status === 'online') return '在线';
  if (node.status === 'untested') return '未检测';
  return '离线';
}

function copyAddress(text: string) {
  navigator.clipboard.writeText(text);
  showToast('已复制地址到剪贴板');
}

function copyNodeLink(node: ProxyNodeItem) {
  const link = buildProxyLink(node);
  if (!link) {
    showToast('自建节点的链接请在用户详情中通过订阅获取', 'info');
    return;
  }
  navigator.clipboard.writeText(link);
  showToast('已复制节点链接到剪贴板');
}

// Create / Edit Self-Hosted Nodes
function openCreateNodeModal() {
  editingNode.value = null;
  editingInbound.value = null;
  openSelfHostedModal.value = true;
}

function openEditNode(node: ProxyNodeItem) {
  if (node.sourceType === 'self_hosted' && node.onlyViaRelay) {
    showToast('内部中转入站请在中转搭建中管理', 'info');
    return;
  }
  editingNode.value = node;
  if (node.sourceType === 'self_hosted') {
    const ib = serverInbounds.value.find(i => i.id === node.inboundId);
    if (!ib) {
      showToast('未找到入站详情', 'error');
      return;
    }
    editingInbound.value = ib;
    openSelfHostedModal.value = true;
  } else {
    nodeForm.name = node.name;
    nodeForm.protocol = node.protocol;
    nodeForm.server = node.server;
    nodeForm.port = node.port;
    nodeForm.supportTcp = node.supportTcp;
    nodeForm.supportUdp = node.supportUdp;
    openImportNodeModal.value = true;
  }
}

// Testing
const testingAll = ref(false);
async function testAllNodes() {
  testingAll.value = true;
  try { for (const item of allNodes.value.filter(n=>n.sourceType==='self_hosted')) await testSingleNode(item); }
  finally {
    testingAll.value = false;
  }
}

async function testSingleNode(node: ProxyNodeItem) {
  if(node.sourceType!=='self_hosted'||!node.inboundId){showToast('当前仅支持自建节点的面板 TCP/TLS 连通性检查','info');return;}
  try {const r=await request<{reachable:boolean;code:string}>(`/inbounds/${encodeURIComponent(node.inboundId)}/reachability`,'POST');showToast(`${node.name}：${r.reachable?'面板可连接':'面板无法连接'}`,r.reachable?'success':'error');}
  catch(e){showToast((e as Error).message,'error');}
}

// Manual Nodes
function handleQuickParse() {
  if (!quickLinkInput.value) return;
  const parsed = parseProxyLink(quickLinkInput.value);
  if (parsed) {
    if (parsed.name) nodeForm.name = parsed.name;
    if (parsed.server) nodeForm.server = parsed.server;
    if (parsed.port) nodeForm.port = parsed.port;
    if (parsed.protocol) nodeForm.protocol = parsed.protocol;
    showToast('链接解析成功！已自动填充表单');
  } else {
    showToast('无法解析此链接，请确认格式或手动输入', 'error');
  }
}

async function saveManualNodeModal() {
  if (!isOwner.value) return;
  if (!quickLinkInput.value.trim()) { showToast('请粘贴包含凭据的 URI、Base64 或 Clash 内容', 'error'); return; }
  try {
    await importAPI.create({name:nodeForm.name || '手动导入', content:quickLinkInput.value.trim(), intervalSeconds:refreshSeconds.value, userIds:grantUserIds.value});
    quickLinkInput.value=''; await loadImports(); openImportNodeModal.value=false;
    showToast('节点已保存，请在订阅管理中授权用户');
  } catch(e) {showToast((e as Error).message,'error');}
}

async function deleteNode(node: ProxyNodeItem) {
  if (!confirm(`确定要删除节点 [${node.name}] 吗？`)) return;
  if (node.sourceType === 'self_hosted') {
    if (node.inboundId) {
      try {
        await proxyAPI.deleteInbound(node.inboundId);
        if (node.sourceId) {
          try {
            const r = await deploymentAPI.publishNode(node.sourceId, `删除自建入站: ${node.name}`);
            showToast(`已保存并发布配置 #${r.revision}，节点确认后生效`);
          } catch (e: any) {
            showToast(`已保存，但发布失败：${e?.message || e}（可在服务器详情的配置版本中重试）`, 'error');
          }
        } else {
          showToast('自建节点已删除');
        }
        await loadData();
      } catch (e) {
        showToast((e as Error).message, 'error');
      }
    } else {
      showToast('该自建节点为基础服务器实例，请在“服务器”模块中管理', 'error');
    }
  } else {
    if (node.sourceId) await deleteSubscription(node.sourceId);
  }
}

// Subscriptions
async function addNewSubscription() {
  if (!isOwner.value) return;
  try {
    const result=await importAPI.create({name:newSubName.value.trim(),url:newSubUrl.value.trim(),intervalSeconds:refreshSeconds.value,userIds:grantUserIds.value});
    newSubUrl.value=''; newSubName.value='';
    await importAPI.refresh(result.id); await loadImports(); showToast('来源已保存，请查看刷新状态');
  } catch(e) {showToast((e as Error).message,'error');}
}
async function saveSourceGrants(sub: SubscriptionItem) {
  try {await importAPI.update(sub.id,{name:sub.name,intervalSeconds:sub.intervalSeconds || 3600,userIds:sub.userIds || []}); await loadImports();}
  catch(e) {showToast((e as Error).message,'error'); await loadImports();}
}
async function updateSubscription(sub: SubscriptionItem) {
  try {await importAPI.refresh(sub.id); await loadImports(); showToast('刷新完成，请查看来源状态');}
  catch(e) {showToast((e as Error).message,'error');}
}
async function deleteSubscription(subId: string) {
  if (!confirm('确定删除该来源及其所有导入节点吗？')) return;
  try {await importAPI.remove(subId); await loadImports(); showToast('来源已删除');}
  catch(e) {showToast((e as Error).message,'error');}
}

onMounted(() => {
  loadProtocolSupport();
  loadData();
});
</script>
