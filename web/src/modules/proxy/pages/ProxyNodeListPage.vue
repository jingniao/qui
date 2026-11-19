<template>
  <div class="space-y-6">
    <InboundDeleteDialog ref="inboundDeleteDialog" @changed="queueDraftRefresh" />
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
          <span>机场订阅管理 ({{ airportSubscriptions.length }})</span>
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
        <button v-if="isOwner"
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

        <!-- Filter 1: Source -->
        <select v-model="filterSource" @change="applyFilter" class="form-input-base !w-auto py-2">
          <option value="all">全部来源</option>
          <option value="self_hosted">服务器自建</option>
          <option value="subscription">机场订阅</option>
          <option value="manual">单独导入</option>
        </select>

        <!-- Filter 2: Protocol -->
        <select v-model="filterProtocol" @change="applyFilter" class="form-input-base !w-auto py-2">
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
        <select v-model="filterStatus" @change="applyFilter" class="form-input-base !w-auto py-2">
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
          v-if="allNodes.some(node => node.sourceType === 'self_hosted')"
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
        <th>传输</th>
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
              <span class="text-xl shrink-0 select-none leading-none cursor-default" :title="countryTitle(getCountryInfo(node))">
                {{ getCountryInfo(node).flag }}
              </span>
              <div class="min-w-0">
                <div class="flex items-center gap-1.5">
                  <span class="font-bold text-base text-neutral-900 truncate tracking-normal" :title="node.name">
                    {{ node.name }}
                  </span>
                </div>
                <div class="mt-1 flex flex-wrap items-center gap-1.5 text-xs">
                  <ProtocolBadge :protocol="node.protocol" compact class="uppercase" />
                  <span v-if="node.sourceType !== 'self_hosted'" class="px-1.5 py-0.5 rounded font-semibold" :class="sourceCapsuleColor(node.sourceType)">{{ node.sourceName }}</span>
                  <EntryRouteSummary :summary="entryRouteSummaries.get(node.inboundId || '')" :show-scheme-link="isOwner" :show-type="false" />
                </div>
                <p v-if="node.sourceType !== 'self_hosted' && node.sourceId" class="text-xs text-ios-secondary">所属分组：{{ importedGroupNames(node) }}</p>
                <p class="text-xs text-neutral-400 font-mono mt-0.5 truncate">
                  {{ networkLabel(node) }}
                </p>
              </div>
            </div>
          </td>

          <!-- 2. Protocol & Transport -->
          <td>
            <div class="space-y-0.5">
              <div class="text-xs text-neutral-400 font-mono">
                {{ transportLabel(node) }}
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
            <div v-if="!relayCount(node)" class="text-neutral-400">
              无额外中转入口
            </div>
            <div v-else class="space-y-1">
              <div v-for="(r, idx) in getNodeRoutes(node)" :key="idx">
                <div
                  class="flex items-center gap-1.5 cursor-pointer text-neutral-800 hover:text-ios-blue"
                  @click="goToEditRelayRoute(r)"
                  :title="'点击前往画布编辑此线路: ' + r.pathDisplay"
                >
                  <span class="font-semibold truncate max-w-[150px]">{{ r.pathDisplay }}</span>
                  <span
                    class="px-1 py-0.2 rounded text-xs font-bold shrink-0"
                    :class="!r.published ? 'bg-neutral-100 text-neutral-600' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800')"
                  >
                    {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '未发布修改' : '已发布') }}
                  </span>
                </div>
                <RelayRouteEntries :entries="routeEntries(r)" @copy="copyAddress" />
              </div>
            </div>
            <NodeRelayList :relays="linkedRelays(node)" :error="relayErrors[node.sourceId || '']" variant="table" :is-owner="isOwner" :entry-address="relayEntryAddress" @copy="copyAddress" />
          </td>

          <!-- 5. Real Status & Latency (Same as Server Page) -->
          <td>
            <div class="flex items-center gap-2">
              <span class="inline-flex shrink-0 select-none items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold" :class="nodeStatus(node).capsule" :title="nodeStatus(node).title">
                <span class="h-1.5 w-1.5 rounded-full" :class="nodeStatus(node).dot"></span>
                <span>{{ nodeStatus(node).label }}</span>
              </span>
              <span v-if="node.status === 'online' && node.latencyMs && !notRunning(node)" class="text-xs font-mono font-bold text-emerald-600">
                {{ node.latencyMs }} ms
              </span>
            </div>
          </td>

          <!-- 6. Actions (Aligned with Server Page Buttons) -->
          <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
            <button v-if="canAddRelay(node)"
              type="button"
              @click="handleAddRelay(node)"
              class="ios-action-btn-primary"
            >
              添加中转
            </button>
            <button v-if="canUseExistingTunnel(node)"
              type="button"
              @click="relayNode = node"
              class="ios-action-btn-secondary"
            >
              使用已有隧道
            </button>
            <button
              type="button"
              @click="openNodeDetail(node)"
              class="ios-action-btn-secondary"
            >
              详情
            </button>
            <button v-if="isOwner && node.sourceType !== 'subscription'"
              type="button"
              @click="openEditNode(node)"
              class="ios-action-btn-secondary"
            >
              {{ node.sourceType === 'manual' ? '改名' : '编辑' }}
            </button>
            <button v-if="isOwner && node.sourceType !== 'subscription'"
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
        v-for="node in filteredNodes"
        :key="node.id"
        class="bg-white rounded-[24px] p-5 shadow-[0_4px_24px_rgba(0,0,0,0.03)] border border-black/[0.05] hover:shadow-[0_8px_30px_rgba(0,0,0,0.08)] hover:-translate-y-0.5 transition-all duration-300 flex flex-col justify-between space-y-4"
      >
        <div class="space-y-3.5">
          <!-- Row 1: Flag · Name -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-2 min-w-0">
              <span
                class="text-xl shrink-0 select-none leading-none cursor-default"
                :title="countryTitle(getCountryInfo(node))"
              >
                {{ getCountryInfo(node).flag }}
              </span>
              <span
                class="font-bold text-lg text-neutral-900 truncate tracking-normal"
                :title="node.name"
              >
                {{ node.name }}
              </span>
            </div>
          </div>

          <!-- Row 2: protocol · imported source · route · transport · latency; status at the end -->
          <div class="flex items-center justify-between gap-2">
            <div class="flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-xs text-neutral-400 font-mono tracking-normal">
              <ProtocolBadge :protocol="node.protocol" compact class="uppercase shrink-0" />
              <span v-if="node.sourceType !== 'self_hosted'" class="shrink-0 px-1.5 py-0.5 rounded text-xs font-semibold" :class="sourceCapsuleColor(node.sourceType)">{{ node.sourceName }}</span>
              <EntryRouteSummary inline :summary="entryRouteSummaries.get(node.inboundId || '')" :show-scheme-link="isOwner" :show-type="false" class="min-w-0 font-sans" />
              <span class="shrink-0 text-neutral-300">·</span>
              <span class="truncate" :title="transportLabel(node)">{{ transportLabel(node) }}</span>
              <template v-if="node.status === 'online' && node.latencyMs && !notRunning(node)">
                <span class="shrink-0 text-neutral-300">·</span>
                <span class="shrink-0 font-bold text-emerald-600">{{ node.latencyMs }} ms</span>
              </template>
            </div>
            <span class="inline-flex shrink-0 select-none items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs font-semibold" :class="nodeStatus(node).capsule" :title="nodeStatus(node).title">
              <span class="h-1.5 w-1.5 rounded-full" :class="nodeStatus(node).dot"></span>
              <span>{{ nodeStatus(node).label }}</span>
            </span>
          </div>

          <!-- Row 3: Direct Address Entrance (Compact & Clean) -->
          <div>
            <div v-if="node.onlyViaRelay" class="bg-neutral-50 rounded-xl px-3 py-1.5 border border-black/[0.03] text-xs text-neutral-400 font-mono italic">
              直连: 仅通过中转提供 · 无直连入口
            </div>
            <div v-else class="bg-[#F8F9FA] rounded-xl px-3 py-1.5 border border-black/[0.03] flex items-center justify-between text-xs font-mono">
              <div class="flex items-center gap-1.5 min-w-0 truncate text-neutral-700">
                <span class="text-neutral-400 font-sans text-xs shrink-0 font-medium">接入:</span>
                <span class="truncate font-medium select-all" :title="node.server + ':' + node.port">
                  {{ node.server }}:{{ node.port }}
                </span>
              </div>
              <button
                type="button"
                @click="copyAddress(node.server + ':' + node.port)"
                class="text-neutral-400 hover:text-ios-blue p-1 rounded-md hover:bg-black/5 transition-colors shrink-0 ml-1"
                title="复制接入地址"
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
              @click="toggleRelayCollapse(node.id, relayCount(node))"
              class="flex items-center justify-between text-xs cursor-pointer hover:bg-black/[0.02] p-1 rounded-lg transition-colors select-none"
            >
              <div class="flex items-center gap-1.5">
                <span class="text-neutral-500 font-bold">中转线路 ({{ relayCount(node) }})</span>
                <span
                  v-if="getNodeRoutes(node).some(r => r.hasUnpublishedChanges)"
                  class="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"
                  title="方案存在未发布修改"
                ></span>
              </div>
              <div class="flex items-center gap-1 text-neutral-400 font-mono text-xs">
                <span>{{ isRelayCollapsed(node.id, relayCount(node)) ? '展开' : '收起' }}</span>
                <svg
                  class="w-3 h-3 transition-transform duration-200"
                  :class="isRelayCollapsed(node.id, relayCount(node)) ? '' : 'rotate-180'"
                  fill="none"
                  viewBox="0 0 24 24"
                  stroke="currentColor"
                >
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                </svg>
              </div>
            </div>

            <!-- Expanded Routes Content -->
            <div v-show="!isRelayCollapsed(node.id, relayCount(node))" class="space-y-1.5">
              <div v-if="!relayCount(node)" class="bg-[#F8F9FA] rounded-xl p-2 text-center text-xs text-neutral-400 border border-black/[0.03]">
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
                      class="px-1.5 py-0.2 rounded text-xs font-semibold"
                      :class="r.executionLocation === 'client' ? 'bg-blue-50 text-ios-blue' : 'bg-purple-50 text-purple-700'"
                    >
                      {{ r.executionLocation === 'client' ? '客户端代理链' : '服务器执行' }}
                    </span>
                    <span class="text-neutral-300">·</span>
                    <span
                      class="px-1.5 py-0.2 rounded text-xs font-semibold"
                      :class="!r.published ? 'bg-neutral-200 text-neutral-700' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-[#E8F8F0] text-[#12B76A]')"
                    >
                      {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '有未发布修改' : '已发布') }}
                    </span>
                  </div>
                  <RelayRouteEntries :entries="routeEntries(r)" @copy="copyAddress" />
                </div>

                <button v-if="isOwner"
                  type="button"
                  @click.stop="goToEditRelayRoute(r)"
                  class="px-2 py-1 text-xs font-bold text-ios-blue hover:bg-ios-blue/10 rounded-lg transition-colors shrink-0"
                  title="在画布中定位并编辑该线路"
                >
                  编辑
                </button>
              </div>
              <NodeRelayList :relays="linkedRelays(node)" :error="relayErrors[node.sourceId || '']" variant="card" :is-owner="isOwner" :entry-address="relayEntryAddress" @copy="copyAddress" />
            </div>
          </div>
        </div>

        <!-- Row 5: Action Grid Buttons (Exact Same Style as Server Card Bottom Grid) -->
        <div class="pt-2 border-t border-black/[0.04] grid grid-cols-3 gap-1.5">
          <!-- 1. 添加中转 -->
          <button v-if="canAddRelay(node)"
            type="button"
            @click="handleAddRelay(node)"
            class="py-1.5 rounded-xl bg-[#EFF8FF] text-[#175CD3] hover:bg-[#D1E9FF] font-semibold text-xs text-center transition-colors flex items-center justify-center gap-1"
            title="添加中转"
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
          <div v-if="(isOwner && node.sourceType !== 'subscription') || (node.sourceType === 'self_hosted' && node.inboundId)" class="relative">
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
              <button v-if="canUseExistingTunnel(node)"
                type="button"
                @click="relayNode = node; activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-black/5 text-ios-label text-sm"
              >
                使用已有隧道
              </button>
              <button v-if="isOwner"
                type="button"
                @click="openEditNode(node); activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-black/5 text-ios-label"
              >
                {{ node.sourceType === 'manual' ? '改名' : '编辑节点' }}
              </button>
              <button
                v-if="node.sourceType === 'self_hosted' && node.inboundId"
                type="button"
                @click="testSingleNode(node); activeMoreId = null"
                class="w-full text-left px-3 py-1.5 hover:bg-black/5 text-ios-label"
              >
                连通性测试
              </button>
              <button v-if="isOwner"
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



    <ImportedProxyDialog v-if="importAction" :node="importAction.node" :mode="importAction.mode"
      @close="importAction = null" @saved="importAction = null; loadData()" />

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
            <ProtocolBadge :protocol="selectedDetailNode.protocol" compact class="uppercase" />
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
              {{ transportLabel(selectedDetailNode) }}
            </span>
          </div>
          <div class="flex justify-between items-center">
            <span class="text-neutral-500 font-medium">网络协议支持</span>
            <span class="font-mono text-neutral-900">
              {{ networkLabel(selectedDetailNode) }}
            </span>
          </div>
        </div>

        <!-- Relay Routes Restorability Section -->
        <div class="space-y-2.5">
          <h4 class="text-neutral-900 flex items-center gap-1.5 text-base font-semibold leading-normal">
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
                  class="px-2 py-0.5 rounded text-xs font-semibold"
                  :class="!r.published ? 'bg-neutral-100 text-neutral-600' : (r.hasUnpublishedChanges ? 'bg-amber-100 text-amber-800' : 'bg-emerald-100 text-emerald-800')"
                >
                  {{ !r.published ? '草稿' : (r.hasUnpublishedChanges ? '有未发布修改' : '已发布') }}
                </span>
              </div>

              <!-- Restorable Info -->
              <div v-if="r.canRestoreToCanvas" class="p-2 rounded-lg bg-emerald-50/70 border border-emerald-200/60 text-emerald-800 text-xs flex items-center justify-between">
                <div class="flex items-center gap-1.5">
                  <svg class="w-4 h-4 text-emerald-600 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                  </svg>
                  <span>配置完整，已准备可编辑画布拓扑。</span>
                </div>
                <button v-if="isOwner"
                  type="button"
                  @click="openDetailModal = false; goToEditRelayRoute(r)"
                  class="px-2 py-0.5 rounded bg-emerald-600 text-white font-semibold hover:bg-emerald-700 transition-colors"
                >
                  去画布编辑
                </button>
              </div>
              <div v-else class="p-2 rounded-lg bg-amber-50/80 border border-amber-200/80 text-amber-800 text-xs space-y-1">
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

    <QuickRelayDialog v-if="quickRelayInboundId || quickRelayImportId" :servers="servers" :inbounds="serverInbounds" :groups="nodeGroups" :imports="proxyNodeStore.manualNodes" :initial-exit-inbound-id="quickRelayInboundId" :initial-exit-import-id="quickRelayImportId" @close="quickRelayInboundId = ''; quickRelayImportId = ''" @created="loadData" />
    <AddRelayDialog v-if="relayNode" :node-id="relayNode.sourceId || ''" :node-name="relayNode.name" :inbounds="serverInbounds" :initial-inbound-id="relayNode.inboundId" :create-scheme="createRelayScheme" :copy="copyAddress" @close="relayNode = null" @saved="loadNodeRelays" />
    <NodeEditorDrawer v-if="openSelfHostedModal" :inbound="editingInbound" :server-choices="inventory.editor.servers" :inbound-choices="inventory.editor.inbounds" :group-choices="inventory.editor.groups" @close="openSelfHostedModal = false" @saved="queueDraftRefresh" />

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
            <button type="button" @click="handleQuickParse" class="ios-btn-primary !py-1 shrink-0">
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

    <!-- Modal 3: Import Source Manager (airport subscriptions + single imports) -->
    <AppDialog
      v-model="openSubModal"
      title="导入来源管理"
      description="添加并管理外部机场订阅源与单独导入的节点内容；两类来源都可在此编辑与删除，并可在「节点分组」中分配所属节点组。"
      max-width="2xl"
      :show-confirm="false"
    >
      <div class="space-y-5 text-sm">
        <!-- New Subscription Form Card -->
        <div class="bg-black/[0.02] p-4 sm:p-5 rounded-2xl border border-black/[0.06] space-y-4">
          <div class="flex items-center justify-between">
            <div class="flex items-center gap-2">
              <span class="w-6 h-6 rounded-lg bg-ios-blue/10 text-ios-blue flex items-center justify-center">
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                </svg>
              </span>
              <h4 class="text-ios-label text-base font-semibold leading-normal">添加新的机场订阅</h4>
            </div>
            <span class="text-xs text-ios-tertiary">凭据加密保存 · 支持 HTTPS</span>
          </div>

          <!-- Name & URL Inputs -->
          <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <label class="block text-xs font-medium text-ios-secondary">
              订阅备注
              <input
                v-model="newSubName"
                placeholder="例如：主力香港中转"
                class="form-input-base mt-1"
              />
            </label>
            <label class="block text-xs font-medium text-ios-secondary sm:col-span-2">
              订阅链接（仅支持 HTTPS）
              <input
                v-model="newSubUrl"
                placeholder="https://airport.example.com/api/v1/client/subscribe?token=..."
                type="password"
                autocomplete="off"
                class="form-input-base mt-1 font-mono"
              />
            </label>
          </div>

          <!-- Auto-refresh interval with presets -->
          <div class="flex flex-wrap items-center justify-between gap-2 pt-1">
            <label class="text-xs font-medium text-ios-secondary">自动同步刷新周期</label>
            <div class="flex items-center gap-1.5 flex-wrap">
              <button
                v-for="preset in [
                  { label: '1 小时', sec: 3600 },
                  { label: '6 小时', sec: 21600 },
                  { label: '12 小时', sec: 43200 },
                  { label: '24 小时', sec: 86400 }
                ]"
                :key="preset.sec"
                type="button"
                @click="refreshSeconds = preset.sec"
                :class="[
                  'px-2.5 py-1 rounded-lg text-xs font-medium transition-all',
                  refreshSeconds === preset.sec
                    ? 'bg-ios-blue text-white shadow-xs'
                    : 'bg-black/[0.04] text-ios-secondary hover:bg-black/[0.08] hover:text-ios-label'
                ]"
              >
                {{ preset.label }}
              </button>
              <div class="flex items-center gap-1 ml-1 text-xs text-ios-tertiary">
                <input
                  v-model.number="refreshSeconds"
                  type="number"
                  min="60"
                  max="2592000"
                  class="form-input-base !w-20 !py-1 font-mono text-center"
                />
                <span>秒</span>
              </div>
            </div>
          </div>

          <!-- Submit Button -->
          <div class="flex justify-end pt-1">
            <button
              v-if="newSubUrl.trim()"
              type="button"
              :disabled="submittingSub"
              @click="addNewSubscription"
              class="ios-btn-primary inline-flex items-center gap-1.5 !py-1.5 px-4 disabled:opacity-40"
            >
              <svg v-if="submittingSub" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
              </svg>
              <span>{{ submittingSub ? '正在添加并首次拉取…' : '添加订阅并拉取' }}</span>
            </button>
          </div>
        </div>

        <!-- Source List Section -->
        <div class="space-y-3">
          <div class="flex items-center justify-between text-xs font-semibold text-ios-secondary">
            <span>已添加的机场订阅（{{ airportSubscriptions.length }}）</span>
            <span class="font-normal text-ios-tertiary">
              单独导入节点请在节点列表中管理
            </span>
          </div>

          <!-- Empty State -->
          <div
            v-if="!airportSubscriptions.length"
            class="p-8 text-center text-xs text-ios-tertiary bg-black/[0.01] rounded-2xl border border-dashed border-black/[0.08]"
          >
            暂无已添加的机场订阅源，可在上方表单输入订阅链接添加
          </div>

          <!-- Only URL subscriptions expose source-level management. -->
          <div v-else class="space-y-3 max-h-[380px] overflow-y-auto pr-1">
            <div
              v-for="sub in airportSubscriptions"
              :key="sub.id"
              class="p-4 bg-white rounded-2xl border transition-all space-y-3"
              :class="editingSubId === sub.id ? 'border-ios-blue shadow-sm ring-2 ring-ios-blue/10' : 'border-black/[0.08] shadow-xs hover:border-black/[0.14]'"
            >
              <!-- EDIT MODE -->
              <div v-if="editingSubId === sub.id" class="space-y-4">
                <div class="flex items-center justify-between border-b border-black/[0.05] pb-2.5">
                  <div class="flex items-center gap-2">
                    <span class="w-5 h-5 rounded-md bg-ios-blue/10 text-ios-blue flex items-center justify-center text-xs">✏️</span>
                    <span class="font-bold text-sm text-ios-label">编辑订阅信息</span>
                  </div>
                  <div class="flex items-center gap-2">
                    <button
                      type="button"
                      @click="cancelEditSub"
                      :disabled="savingSub"
                      class="ios-action-btn-secondary"
                    >
                      取消
                    </button>
                    <button
                      type="button"
                      @click="saveEditSub(sub)"
                      :disabled="savingSub"
                      class="ios-btn-primary !py-1 inline-flex items-center gap-1.5"
                    >
                      <svg v-if="savingSub" class="w-3.5 h-3.5 animate-spin" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                      </svg>
                      <span>{{ savingSub ? '保存中…' : '保存修改' }}</span>
                    </button>
                  </div>
                </div>

                <p class="text-xs text-ios-secondary">通过节点分组可用：{{ sub.userIds?.length ?? 0 }} 个用户</p>
                <!-- Inputs -->
                <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
                  <label class="block text-xs font-medium text-ios-secondary">
                    订阅备注
                    <input
                      v-model="editSubForm.name"
                      placeholder="例如：主力香港中转"
                      class="form-input-base mt-1"
                    />
                  </label>
                  <label class="block text-xs font-medium text-ios-secondary sm:col-span-2" v-if="sub.kind !== 'content'">
                    更新订阅链接（留空保持原有加密链接）
                    <input
                      v-model="editSubForm.url"
                      placeholder="留空保持原有加密链接，或填入新 HTTPS 链接"
                      type="password"
                      autocomplete="off"
                      class="form-input-base mt-1 font-mono"
                    />
                  </label>
                  <div v-else class="block text-xs text-ios-secondary sm:col-span-2 p-2.5 rounded-xl bg-black/[0.02] border border-black/[0.06]">
                    该来源由单独导入的内容创建，没有订阅链接可更新；如需换成订阅链接，请删除后重新添加。
                  </div>
                </div>

                <!-- Interval presets in edit mode -->
                <div class="flex flex-wrap items-center justify-between gap-2 pt-1">
                  <label class="text-xs font-medium text-ios-secondary">自动同步刷新周期</label>
                  <div class="flex items-center gap-1.5 flex-wrap">
                    <button
                      v-for="preset in [
                        { label: '1 小时', sec: 3600 },
                        { label: '6 小时', sec: 21600 },
                        { label: '12 小时', sec: 43200 },
                        { label: '24 小时', sec: 86400 }
                      ]"
                      :key="preset.sec"
                      type="button"
                      @click="editSubForm.intervalSeconds = preset.sec"
                      :class="[
                        'px-2.5 py-1 rounded-lg text-xs font-medium transition-all',
                        editSubForm.intervalSeconds === preset.sec
                          ? 'bg-ios-blue text-white shadow-xs'
                          : 'bg-black/[0.04] text-ios-secondary hover:bg-black/[0.08] hover:text-ios-label'
                      ]"
                    >
                      {{ preset.label }}
                    </button>
                    <div class="flex items-center gap-1 ml-1 text-xs text-ios-tertiary">
                      <input
                        v-model.number="editSubForm.intervalSeconds"
                        type="number"
                        min="60"
                        max="2592000"
                        class="form-input-base !w-20 !py-1 font-mono text-center"
                      />
                      <span>秒</span>
                    </div>
                  </div>
                </div>
              </div>

              <!-- VIEW MODE -->
              <div v-else class="space-y-3">
                <!-- Top Row: Name + Badges + Actions -->
                <div class="flex flex-wrap items-center justify-between gap-2">
                  <div class="flex items-center gap-2 min-w-0">
                    <span class="font-bold text-sm text-ios-label truncate" :title="sub.name">{{ sub.name }}</span>
                    <span
                      class="px-2 py-0.5 rounded-full text-xs font-medium"
                      :class="sub.kind === 'content' ? 'bg-blue-100 text-ios-blue' : 'bg-purple-100 text-purple-700'"
                    >
                      {{ sub.kind === 'content' ? '单独导入' : '机场订阅' }}
                    </span>
                    <span class="text-xs text-ios-secondary">通过节点分组可用：{{ sub.userIds?.length ?? 0 }} 个用户</span>
                    <span
                      v-if="sub.kind === 'content'"
                      class="px-2 py-0.5 rounded-full text-xs font-medium bg-black/[0.04] text-ios-secondary border border-black/[0.06]"
                    >
                      内容导入
                    </span>
                    <span
                      v-else-if="sub.lastStatus === 'failed' || sub.lastError"
                      class="px-2 py-0.5 rounded-full text-xs font-medium bg-amber-500/10 text-amber-600 border border-amber-500/20"
                    >
                      拉取异常
                    </span>
                    <span
                      v-else-if="sub.latestResult?.outcome === 'partial'"
                      class="px-2 py-0.5 rounded-full text-xs font-medium bg-sky-500/10 text-sky-600 border border-sky-500/20"
                    >
                      部分导入
                    </span>
                    <span
                      v-else-if="sub.lastStatus === 'pending'"
                      class="px-2 py-0.5 rounded-full text-xs font-medium bg-black/[0.04] text-ios-secondary border border-black/[0.08]"
                    >
                      等待抓取
                    </span>
                    <span
                      v-else
                      class="px-2 py-0.5 rounded-full text-xs font-medium bg-emerald-500/10 text-emerald-600 border border-emerald-500/20"
                    >
                      正常运行
                    </span>
                    <span class="px-2 py-0.5 rounded-md text-xs font-mono bg-black/[0.04] text-ios-secondary">
                      {{ sub.nodeCount }} 个节点
                    </span>
                  </div>

                  <!-- Action Buttons -->
                  <div class="flex items-center gap-1.5 shrink-0">
                    <button
                      type="button"
                      @click="startEditSub(sub)"
                      class="ios-action-btn-secondary inline-flex items-center gap-1"
                      title="编辑此订阅备注、链接与刷新周期"
                    >
                      <svg class="w-3.5 h-3.5 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
                      </svg>
                      <span>编辑</span>
                    </button>
                    <button
                      type="button"
                      v-if="sub.kind !== 'content'"
                      @click="updateSubscription(sub)"
                      :disabled="updatingSubId === sub.id"
                      class="ios-action-btn-secondary inline-flex items-center gap-1"
                      title="立即拉取最新节点"
                    >
                      <svg class="w-3.5 h-3.5 text-ios-secondary" :class="{ 'animate-spin': updatingSubId === sub.id }" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                      </svg>
                      <span>{{ updatingSubId === sub.id ? '拉取中…' : '更新' }}</span>
                    </button>
                    <button
                      type="button"
                      @click="deleteSubscription(sub.id)"
                      class="ios-action-btn-danger"
                      title="删除此订阅源及已导入节点"
                    >
                      删除
                    </button>
                  </div>
                </div>

                <!-- Metadata Info Row -->
                <div class="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ios-secondary">
                  <span class="font-mono text-ios-tertiary truncate max-w-[280px]" :title="sub.url">
                    {{ sub.kind === 'content' ? '内容导入（无订阅链接）' : sub.url }}
                  </span>
                  <span v-if="sub.lastFetchAt && sub.kind !== 'content'">上次尝试：{{ formatImportTime(sub.lastFetchAt) }}</span>
                  <span v-if="sub.lastSuccessAt">上次成功：{{ formatImportTime(sub.lastSuccessAt) }}</span>
                  <span v-if="sub.kind !== 'content'">刷新间隔：{{ formatInterval(sub.intervalSeconds) }}</span>
                </div>

                <!-- Attempt counts: what this attempt imported and what a
                     subscription can use right now. -->
                <div v-if="sub.latestResult" class="flex flex-wrap items-center gap-2 text-xs text-ios-secondary">
                  <span class="font-mono text-xs">{{ importCountsLine(sub.latestResult) }}</span>
                  <button
                    type="button"
                    @click="openImportDetails(sub)"
                    class="px-2 py-0.5 rounded-md text-xs font-medium text-ios-blue bg-ios-blue/10 hover:bg-ios-blue/15"
                    title="查看本次导入的逐条原因"
                  >
                    查看原因<span v-if="sub.latestResult.diagnosticsTruncated">（已截断）</span>
                  </button>
                </div>

                <!-- Error Banner -->
                <div v-if="sub.lastError" class="p-2.5 rounded-xl bg-amber-500/10 border border-amber-500/20 text-xs text-amber-800 flex items-start gap-2">
                  <svg class="w-4 h-4 text-amber-600 shrink-0 mt-0.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                  </svg>
                  <div class="space-y-0.5">
                    <p class="font-medium text-amber-900">拉取状态说明</p>
                    <p class="text-xs leading-relaxed break-all">{{ sourceFailureText(sub) }}</p>
                  </div>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>
    <ImportResultDialog v-if="importDetails" :import-details="importDetails" @close="importDetails = null" />
  </div>
</template>

<script setup lang="ts">
import InboundDeleteDialog from '../components/InboundDeleteDialog.vue';
import ImportedProxyDialog from '../components/ImportedProxyDialog.vue';
const inboundDeleteDialog = ref<InstanceType<typeof InboundDeleteDialog> | null>(null);
import { ref, computed, reactive, onMounted, onUnmounted, watch } from 'vue';
import { revisionResult } from '../../system/revisionResult';
import type { InboundStateItem, InboundPublishState } from '../../system/deploymentApi';
import AddRelayDialog from '../../tunnels/components/AddRelayDialog.vue';
import QuickRelayDialog from '../components/QuickRelayDialog.vue';
import { useNodeRelayActions } from '../useNodeRelayActions';
import ImportResultDialog from '../components/ImportResultDialog.vue';
import NodeRelayList from '../../tunnels/components/NodeRelayList.vue';
import RelayRouteEntries from '../components/RelayRouteEntries.vue';
import { nestRelayEntries, ruleEntryAddress } from '../nestedRelayEntries';
import { authState } from '../../../app/auth';
import type { NodeGroup } from '../../nodes/groupsApi';
import { importAPI, loadImports, airportSources, describeImportResult, importCountsLine, failedImportResult } from '../importApi';
import type { ImportResult } from '../importApi';
import PageHeader from '../../../components/PageHeader.vue';
import ProtocolBadge from '../../../components/ProtocolBadge.vue';
import DataTable from '../../../components/DataTable.vue';
import Pagination from '../../../components/Pagination.vue';
import AppDialog from '../../../components/AppDialog.vue';
import NodeEditorDrawer from '../components/node-editor/NodeEditorDrawer.vue';
import { listenNetworks } from '../components/node-editor/nodeEditorModel';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import EmptyState from '../../../components/EmptyState.vue';
import { showToast } from '../../../app/api';
import { throttleTrailing, nodeStatusEventChangesInventory } from '../../../app/nodeStatusEvents';
import type { RuntimeSnapshot } from '../../nodes/runtimeSnapshots';
import { proxyPageInventory } from '../proxyPageInventory';
import { request } from '../../../app/apiClient';
import type { NodeView } from '../../nodes/api';
import { serverStatus } from '../../nodes/components/serverListModel';
import { deploymentAPI } from '../../system/deploymentApi';
import { canvasAPI, type Inbound, choices, loadProtocolSupport } from '../api';
import { resolveCountryInfo, countryTitle, getNodeMeta, type CountryInfo } from '../../nodes/nodeMeta';
import { connectAddress } from '../../nodes/nodeAddress';
import { isUserEntry } from '../userEntries';
import EntryRouteSummary from '../components/EntryRouteSummary.vue';
import {
  proxyNodeStore,
  loadStoredData,
  saveManualNodes,
  saveSubscriptions,
  parseProxyLink,
  loadCanvasSchemes,
  buildProxyLink,
  type ProxyNodeItem,
  type NodeRelayRoute,
  type SubscriptionItem
} from '../proxyNodes';

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
const inventory = proxyPageInventory();
const now = ref(Date.now());
const serverInbounds = ref<Inbound[]>([]);
const entryRouteSummaries = computed(() => new Map(serverInbounds.value.filter(isUserEntry).map(inbound => [inbound.id, inbound.routeSummary])));
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
  // Owner 2026-11-15: every card starts collapsed; the header still shows the count.
  void routeCount;
  return true;
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

function closeMoreDropdown() { activeMoreId.value = null; }
onMounted(() => window.addEventListener('click', closeMoreDropdown));
onUnmounted(() => window.removeEventListener('click', closeMoreDropdown));

// Modals
const openDetailModal = ref(false);
const importAction = ref<{ node: ProxyNodeItem; mode: 'rename' | 'delete' } | null>(null);
const selectedDetailNode = ref<ProxyNodeItem | null>(null);

function openNodeDetail(node: ProxyNodeItem) {
  selectedDetailNode.value = node;
  openDetailModal.value = true;
}

const openSelfHostedModal = ref(false);
const openImportNodeModal = ref(false);
const isOwner = computed(() => authState.currentUser?.role === 'Owner');
const nodeGroups = ref<NodeGroup[]>([]);
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
// The bounded reasons of one attempt, opened from the source card. The dialog
// shows what the panel recorded; it never re-runs a fetch.
const importDetails = ref<{ name: string; result: ImportResult } | null>(null);
function openImportDetails(source: SubscriptionItem) {
  if (source.latestResult) importDetails.value = { name: source.name, result: source.latestResult };
}
// The panel stores every import source in one table; only the sources it derived
// as "url" are airport subscriptions, so the header count and the airport list
// must not include content imports (which are 单独导入 and live in manualNodes).
const airportSubscriptions = computed(() => airportSources(proxyNodeStore.subscriptions));
const newSubName = ref('');
const newSubUrl = ref('');
const submittingSub = ref(false);
const updatingSubId = ref<string | null>(null);
const editingSubId = ref<string | null>(null);
const savingSub = ref(false);
const editSubForm = reactive({
  name: '',
  url: '',
  intervalSeconds: 3600
});

function importedGroupNames(node: ProxyNodeItem): string {
  return nodeGroups.value.filter(g => g.imports?.some(item => item.sourceId === node.sourceId &&
    ((node.sourceType === 'subscription' && item.proxyKey === '') || item.proxyKey === node.proxyKey))).map(g => g.name).join('、') || '—';
}
// Pagination
const currentPage = ref(1);
const pageSize = 12;

// Load real data
let loadAgain = false;
async function loadData() {
  if (loading.value) { loadAgain = true; return; }
  const generation = ++serverRefreshGeneration;
  loading.value = true;
  try {
    // Keep old browser data intact, but never mix its ungranted entries into
    // the server-owned U5 list.
    proxyNodeStore.manualNodes = [];
    proxyNodeStore.subscriptions = [];
    const resources = Promise.all([
      inventory.readServers().then(nodeRes => { if (generation === serverRefreshGeneration) servers.value = nodeRes; }),
      inventory.readInbounds()
    ]).then(([, inboundRes]) => {
      if (publishStateDisposed) return;
      serverInbounds.value = inboundRes;
      void loadNodeRelays();
    });
    // Relay routes shown on this page come from the schemes the canvas stores on
    // the server. Fetch them alongside the resources, then resolve references.
    const results = await Promise.allSettled([
      choices((page, size) => canvasAPI.list(page, size)),
      resources,
      isOwner.value ? loadImports() : Promise.resolve(),
      isOwner.value ? inventory.readGroups().then(items => { nodeGroups.value = items; }) : Promise.resolve()
    ]);
    if (publishStateDisposed) return;
    const labels = ['中转方案', '服务器与入站', '导入节点', '节点分组'];
    results.forEach((result, index) => {
      if (result.status === 'rejected') showToast(`${labels[index]}读取失败：${(result.reason as Error).message}`, 'error');
    });
    const [schemes, resourceResult] = results;
    if (schemes.status === 'fulfilled' && resourceResult.status === 'fulfilled') {
      await loadCanvasSchemes({ servers: servers.value, inbounds: serverInbounds.value, imported: proxyNodeStore.manualNodes }, schemes.value);
    }
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    loading.value = false;
    if (loadAgain && !publishStateDisposed) { loadAgain = false; void loadData(); }
  }
}

// Map self-hosted server inbounds into ProxyNodeItem with clean naming
const selfHostedNodes = computed<ProxyNodeItem[]>(() => {
  const result: ProxyNodeItem[] = [];
  for (const srv of servers.value) {
    if (!srv.enabled || srv.deletedAt) continue;
    const inbounds = serverInbounds.value.filter(ib => ib.nodeId === srv.id && isUserEntry(ib));
    if (inbounds.length > 0) {
      for (const ib of inbounds) {
        let cleanName = (ib.name || '').trim();
        if (cleanName.startsWith(srv.name)) {
          cleanName = cleanName.slice(srv.name.length).replace(/^[·\s\-_]+/, '').trim();
        }
        const finalDisplayName = cleanName ? `${srv.name} · ${cleanName}` : `${srv.name} · ${ib.protocol.toUpperCase()}`;
        const name = ib.enabled ? finalDisplayName : finalDisplayName + ' (已停用)';
        const status = ['online', 'degraded'].includes(serverStatus(srv, now.value).key) && ib.enabled ? 'online' : 'offline';
        // The panel holds the inbound, so the transports are read from it with the
        // same helper the server detail page uses, instead of guessing from the
        // protocol name (that showed "TCP · UDP" for every self-hosted node).
        const networks = listenNetworks(ib);

        result.push({
          id: `self-${srv.id}-${ib.id}`,
          name: name,
          sourceType: 'self_hosted',
          sourceName: srv.name,
          sourceId: srv.id,
          inboundId: ib.id,
          server: connectAddress(srv) || '未设置连接地址',
          port: ib.port || 443,
          protocol: ib.protocol || 'vless',
          status: status,
          latencyMs: undefined,
          supportTcp: networks.includes('tcp'),
          supportUdp: networks.includes('udp'),
          settings: ib.settings as any,
          createdAt: ib.createdAt || srv.createdAt,
          onlyViaRelay: ib.internalRelay
        });
      }
    }
  }
  return result;
});

// All aggregated nodes. Entries made by a node's 添加中转 are listed in their exit node's 中转 section only.
const relayEntryAddress = (rule: Parameters<typeof ruleEntryAddress>[0]) => ruleEntryAddress(rule, selfHostedNodes.value, serverInbounds.value, servers.value);
const relayNesting = computed(() => nestRelayEntries(selfHostedNodes.value, proxyNodeStore.manualNodes, serverInbounds.value, computeNodeRoutes, isLinkedRelayRule));
const getNodeRoutes = (node: ProxyNodeItem) => relayNesting.value.routes.get(node.id) || [];
const relayCount = (node: ProxyNodeItem) => getNodeRoutes(node).length + linkedRelays(node).length;
const routeEntries = (route: NodeRelayRoute) => relayNesting.value.entries.get(route.schemeId) || [];
const hiddenServerIds = computed(() => new Set(servers.value
  .filter(server => server.maintenance || serverStatus(server, now.value).key === 'offline')
  .map(server => server.id)));
const allNodes = computed<ProxyNodeItem[]>(() => [...selfHostedNodes.value.filter(node => !relayNesting.value.hidden.has(node.id) && !hiddenServerIds.value.has(node.sourceId!)), ...proxyNodeStore.manualNodes]);

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

const {
  relayNode, quickRelayInboundId, quickRelayImportId, relayErrors, getNodeRoutes: computeNodeRoutes, goToEditRelayRoute,
  linkedRelays, loadNodeRelays, canUseExistingTunnel, canAddRelay,
  handleAddRelay, createRelayScheme, isLinkedRelayRule,
} = useNodeRelayActions(isOwner, servers, serverInbounds);

// Flag resolution helper
function getCountryInfo(node: ProxyNodeItem): CountryInfo {
  // Self-hosted inbounds take their server's flag; imported nodes only have a name.
  const server = node.sourceType === 'self_hosted' ? servers.value.find(item => item.id === node.sourceId) : undefined;
  if (server) return resolveCountryInfo({ region: getNodeMeta(server.id).region, countryCode: server.countryCode, name: server.name, hostname: server.hostname });
  return resolveCountryInfo({ name: node.name, hostname: node.server });
}

// Styling helpers (Exact same status tones as Server Page)
function networkLabel(node: Pick<ProxyNodeItem, 'supportTcp' | 'supportUdp'>): string {
  const parts = [node.supportTcp ? 'TCP' : '', node.supportUdp ? 'UDP' : ''].filter(Boolean);
  return parts.length ? parts.join(' · ') : '—';
}

function sourceCapsuleColor(sourceType: string): string {
  if (sourceType === 'self_hosted') return 'bg-emerald-100 text-emerald-800';
  if (sourceType === 'subscription') return 'bg-purple-100 text-purple-800';
  return 'bg-blue-100 text-ios-blue';
}

// One badge per node: the server's reachability and the inbound's publish state
// combined, showing the state that needs attention; the tooltip names both.
const statusLook = {
  green: { capsule: 'bg-[#E8F8F0] text-[#12B76A]', dot: 'bg-[#12B76A]' },
  yellow: { capsule: 'bg-amber-50 text-amber-700', dot: 'bg-amber-500' },
  blue: { capsule: 'bg-blue-50 text-blue-600', dot: 'bg-blue-500' },
  red: { capsule: 'bg-red-50 text-red-600', dot: 'bg-red-500' },
  gray: { capsule: 'bg-neutral-100 text-neutral-500', dot: 'bg-neutral-400' },
};
function nodeStatus(node: ProxyNodeItem): { label: string; capsule: string; dot: string; title: string } {
  const reach = node.status === 'online' ? '在线' : node.status === 'untested' ? '未检测' : '离线';
  const publish = inboundBadge(node);
  const failure = publish?.title?.startsWith('最近一次发布失败') ? `；${publish.title}` : '';
  const title = publish ? `服务器：${reach}；发布状态：${publish.label}${failure}` : `状态：${reach}`;
  const pick = (label: string, tone: keyof typeof statusLook) => ({ label, title, ...statusLook[tone] });
  if (inboundStatusUnknown(node)) return pick('状态未知', 'yellow');
  if (node.status !== 'online' && node.status !== 'untested') return pick('离线', 'gray');
  const state = inboundState(node)?.state;
  if (state === 'unpublished') return pick('未生效', 'red');
  if (state === 'pending') return pick('发布中', 'blue');
  if (state === 'modified') return pick('在线·有修改', 'yellow');
  return node.status === 'online' ? pick('在线', 'green') : pick('未检测', 'gray');
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
  if (!isOwner.value) return;
  editingNode.value = null;
  editingInbound.value = null;
  openSelfHostedModal.value = true;
}

function openEditNode(node: ProxyNodeItem) {
  if (!isOwner.value) return;
  if (node.sourceType === 'manual') {
    importAction.value = { node, mode: 'rename' };
    return;
  }
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
  const name = nodeForm.name || '手动导入';
  try {
    const created = await importAPI.create({name, content:quickLinkInput.value.trim(), intervalSeconds:refreshSeconds.value, userIds:[]});
    quickLinkInput.value='';
    openImportNodeModal.value=false;
    // A reload failure cannot undo a saved import, so it never becomes an error and
    // never causes a second save.
    await loadImports().catch(() => {});
    // A pasted import runs through the same analyzer as a fetch, so its result is
    // the toast; a partial or failed paste opens the bounded reasons directly,
    // because a content source has no card of its own.
    showImportResult(name, created.result);
  } catch(e) {
    // The draft and the dialog survive a rejection, and the panel's own result
    // explains every rejected entry.
    const result = failedImportResult(e);
    if (result) importDetails.value = { name, result };
    showToast(importFailureMessage(e), 'error');
  }
}

// showImportResult reports one attempt the same way everywhere a save returns a
// result: the toast follows the business outcome, and the reasons open when there
// are any.
function showImportResult(name: string, result?: ImportResult) {
  if (!result) { showToast('节点已保存，可在「节点分组」中分配'); return; }
  const outcome = importOutcomeMessage(result);
  showToast(outcome.message, outcome.kind);
  if (result.outcome !== 'ok' || result.diagnostics.length) importDetails.value = { name, result };
}

async function deleteNode(node: ProxyNodeItem) {
  if (!isOwner.value) return;
  if (node.sourceType === 'manual') {
    importAction.value = { node, mode: 'delete' };
    return;
  }
  if (node.sourceType === 'self_hosted' && node.inboundId) {
    await inboundDeleteDialog.value?.prompt({ id: node.inboundId, name: node.name });
  }
}

// Subscriptions Helper Functions
function startEditSub(sub: SubscriptionItem) {
  editingSubId.value = sub.id;
  editSubForm.name = sub.name;
  editSubForm.url = '';
  editSubForm.intervalSeconds = sub.intervalSeconds || 3600;
}

function cancelEditSub() {
  editingSubId.value = null;
}

async function refreshSubscriptionSource(id: string) {
  try {
    const result = await importAPI.refresh(id);
    await loadImports();
    return result;
  } catch (e) {
    // Refresh may have saved a failed result. Keep its error if reloading also fails.
    await loadImports().catch(() => {});
    throw e;
  }
}

// The toast follows the business outcome, not the HTTP status: only a complete
// import is a success, a partial import is a warning, and a failure names what
// happened to the cached endpoints. A failure without a recorded result shows the
// panel's message only.
function importOutcomeMessage(result: ImportResult) {
  return { message: describeImportResult(result), kind: result.outcome === 'ok' ? 'success' as const : 'warning' as const };
}

function importFailureMessage(error: unknown) {
  const result = failedImportResult(error);
  return result ? describeImportResult(result) : (error as Error).message;
}

async function saveEditSub(sub: SubscriptionItem) {
  if (!editSubForm.name.trim()) {
    showToast('订阅备注不能为空', 'error');
    return;
  }
  savingSub.value = true;
  try {
    const payload: { name: string; intervalSeconds: number; userIds: string[]; url?: string } = {
      name: editSubForm.name.trim(),
      intervalSeconds: editSubForm.intervalSeconds || 3600,
      userIds: [...(sub.userIds || [])]
    };
    const hasNewUrl = editSubForm.url.trim().length > 0;
    if (hasNewUrl) {
      payload.url = editSubForm.url.trim();
    }
    await importAPI.update(sub.id, payload);
    if (hasNewUrl) {
      const outcome = importOutcomeMessage(await refreshSubscriptionSource(sub.id));
      showToast(outcome.message, outcome.kind);
    } else {
      await loadImports();
      showToast('订阅配置已更新', 'success');
    }
    editingSubId.value = null;
  } catch (e) {
    showToast(importFailureMessage(e), 'error');
  } finally {
    savingSub.value = false;
  }
}

function formatInterval(seconds?: number) {
  if (!seconds || seconds <= 0) return '未设置';
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`;
  if (seconds < 86400) return `${(seconds / 3600).toFixed(seconds % 3600 ? 1 : 0)} 小时`;
  return `${(seconds / 86400).toFixed(seconds % 86400 ? 1 : 0)} 天`;
}

// Attempt and success stamps arrive as RFC3339 UTC strings.
function formatImportTime(value?: string) {
  if (!value) return '—';
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? value : new Date(parsed).toLocaleString();
}

// A failure recorded before results existed cannot name a cause; say so instead
// of presenting the old generic text as a diagnosis.
function sourceFailureText(sub: SubscriptionItem): string {
  return sub.latestResult ? sub.lastError : '上次刷新失败；未记录原因';
}

// Subscriptions
async function addNewSubscription() {
  if (!isOwner.value || !newSubUrl.value.trim() || submittingSub.value) return;
  submittingSub.value = true;
  let savedId: string | undefined;
  try {
    const result = await importAPI.create({
      name: newSubName.value.trim() || '机场订阅',
      url: newSubUrl.value.trim(),
      intervalSeconds: refreshSeconds.value || 3600,
      userIds: []
    });
    savedId = result.id;
    newSubUrl.value = '';
    newSubName.value = '';
    const outcome = importOutcomeMessage(await refreshSubscriptionSource(result.id));
    showToast(outcome.message, outcome.kind);
  } catch (e) {
    const saved = proxyNodeStore.subscriptions.find(s => s.id === savedId);
    if (saved) startEditSub(saved);
    showToast(importFailureMessage(e), 'error');
  } finally {
    submittingSub.value = false;
  }
}

async function updateSubscription(sub: SubscriptionItem) {
  if (updatingSubId.value) return;
  updatingSubId.value = sub.id;
  try {
    const outcome = importOutcomeMessage(await refreshSubscriptionSource(sub.id));
    showToast(outcome.message, outcome.kind);
  } catch (e) {
    showToast(importFailureMessage(e), 'error');
  } finally {
    updatingSubId.value = null;
  }
}

async function deleteSubscription(subId: string) {
  if (!confirm('确定删除该来源及其所有导入节点吗？')) return;
  try {
    await importAPI.remove(subId);
    await loadImports();
    showToast('来源已删除');
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// Publish state of each self-hosted inbound against the revision its server runs,
// keyed by `<serverId>/<inboundId>`. Failed reads retain the snapshot but mark it unknown.
const inboundStateMap = ref<Map<string, InboundStateItem>>(new Map());
const inboundStateErrors = ref<Map<string, string>>(new Map());

function inboundStatusUnknown(node: ProxyNodeItem): boolean {
  return node.sourceType === 'self_hosted' && inboundStateErrors.value.has(node.sourceId!);
}

const inboundBadgeLook: Record<InboundPublishState, { label: string; tone: string }> = {
  effective: { label: '已生效', tone: 'green' },
  modified: { label: '有未发布修改', tone: 'yellow' },
  pending: { label: '发布中', tone: 'blue' },
  unpublished: { label: '未生效', tone: 'red' },
};

function inboundState(node: ProxyNodeItem): InboundStateItem | undefined {
  if (node.sourceType !== 'self_hosted' || !node.sourceId || !node.inboundId) return undefined;
  return inboundStateMap.value.get(`${node.sourceId}/${node.inboundId}`);
}

function inboundBadge(node: ProxyNodeItem) {
  if (inboundStatusUnknown(node)) return { label: '状态未知', tone: 'yellow', title: inboundStateErrors.value.get(node.sourceId!) };
  const item = inboundState(node);
  if (!item) return undefined;
  const look = inboundBadgeLook[item.state];
  const failure = item.state === 'unpublished' && item.code ? `最近一次发布失败：${revisionResult(item.code)}` : '';
  return { ...look, title: failure || `入站「${node.name}」的发布状态` };
}

// A modified inbound still runs its old configuration, so only an inbound the
// server does not run yet drops the server's online badge and latency.
function notRunning(node: ProxyNodeItem): boolean {
  if (inboundStatusUnknown(node)) return true;
  const state = inboundState(node)?.state;
  return state === 'unpublished' || state === 'pending';
}

function transportLabel(node: Pick<ProxyNodeItem, 'protocol' | 'settings'>): string {
  if (node.protocol === 'hysteria2' || node.protocol === 'tuic') return 'udp + quic';
  return `${node.settings?.transport || 'tcp'}${node.settings?.security ? ' + ' + node.settings.security : ''}`;
}

let publishStateGeneration = 0;
let publishStateReading = false, publishStateAgain = false, publishStateDisposed = false, requestedPublishKey = '', nextPublishKey = '';
function visibleServerIds() {
  const nodes = displayMode.value === 'list' ? pagedNodes.value : filteredNodes.value;
  return new Set(nodes.filter(n => n.sourceType === 'self_hosted' && n.sourceId).map(n => n.sourceId!));
}
async function loadServerPublishStates() {
  if (document.hidden || publishStateDisposed) return;
  const serverIds = [...visibleServerIds()];
  nextPublishKey = [...serverIds].sort().join(',');
  if (publishStateReading) { publishStateAgain = true; return; }
  if (!serverIds.length) return;
  publishStateReading = true; requestedPublishKey = nextPublishKey;
  const generation = ++publishStateGeneration;
  const results: (readonly [string, InboundStateItem[] | string])[] = [];
  try {
    // The endpoint validates at most 200 IDs per request.
    for (let offset = 0; offset < serverIds.length; offset += 200) {
      const batch = await deploymentAPI.inboundStatesBatch(serverIds.slice(offset, offset + 200));
      results.push(...batch.items.map(item => [item.nodeId, item.error || item.items] as const));
    }
  } catch (e) {
    results.push(...serverIds.map(id => [id, (e as Error).message] as const));
  }

  publishStateReading = false;
  const readAgain = publishStateAgain; publishStateAgain = false;
  if (generation !== publishStateGeneration) return;
  if (requestedPublishKey !== nextPublishKey) { void loadServerPublishStates(); return; }
  const nextMap = new Map(inboundStateMap.value);
  const errors = new Map(inboundStateErrors.value);
  for (const [id, items] of results) {
    if (typeof items === 'string') {
      errors.set(id, `入站状态读取失败：${items}`);
      continue;
    }
    errors.delete(id);
    for (const key of [...nextMap.keys()]) if (key.startsWith(`${id}/`)) nextMap.delete(key);
    for (const item of items) nextMap.set(`${id}/${item.inboundId}`, item);
  }
  inboundStateMap.value = nextMap;
  inboundStateErrors.value = errors;
  if (readAgain) void loadServerPublishStates();
}

function visiblePublishKey() {
  const ids = visibleServerIds();
  return servers.value.filter(node => ids.has(node.id)).map(node => node.id + '/' + (node.heartbeat?.current_revision || 0)).sort().join(',');
}
watch([visiblePublishKey, serverInbounds], () => {
  loadServerPublishStates();
}, { immediate: true });

let serverRefreshGeneration = 0;
async function refreshServersOnNodeStatus() {
  if (document.hidden) return;
  const generation = ++serverRefreshGeneration;
  try {
    const items = await inventory.readServers();
    if (generation === serverRefreshGeneration) servers.value = items;
  } catch (error) {
    if (generation === serverRefreshGeneration) showToast(`服务器状态读取失败：${(error as Error).message}`, 'error');
  }
}
function onNodeRuntime(event: Event) {
  if (inventory.applyEvent(servers.value, (event as CustomEvent<RuntimeSnapshot>).detail)) refreshInventory();
}
const refreshInventory = throttleTrailing(() => { void refreshServersOnNodeStatus(); void loadServerPublishStates(); }, 1000);
const refreshDeployment = throttleTrailing(() => { void loadServerPublishStates(); }, 1000);
function onNodeStatus(event: Event) {
  if (nodeStatusEventChangesInventory(event)) refreshInventory();
}
let draftTimer: ReturnType<typeof setTimeout> | undefined;
function queueDraftRefresh() {
  clearTimeout(draftTimer);
  if (!document.hidden) draftTimer = setTimeout(() => { void loadData(); }, 150);
}
function resyncInventory() { inventory.resetStream(); queueDraftRefresh(); }
function publishVisibilityChanged() { if (!document.hidden) resyncInventory(); }
let statusClock: ReturnType<typeof setInterval> | undefined;
onMounted(() => {
  statusClock = setInterval(() => {
    now.value = Date.now();
    if (inboundStateErrors.value.size && !publishStateReading && [...visibleServerIds()].some(id => inboundStateErrors.value.has(id))) void loadServerPublishStates();
  }, 5000);
  if (isOwner.value) void loadProtocolSupport().catch(e => showToast(`协议支持表读取失败：${(e as Error).message}`, 'error'));
  loadData();
  window.addEventListener('proxy-deployment-status', refreshDeployment);
  window.addEventListener('proxy-draft-changed', queueDraftRefresh);
  window.addEventListener('proxy-node-status', onNodeStatus);
  window.addEventListener('proxy-node-runtime', onNodeRuntime);
  window.addEventListener('proxy-node-runtime-open', resyncInventory);
  document.addEventListener('visibilitychange', publishVisibilityChanged);
});

onUnmounted(() => {
  clearInterval(statusClock);
  serverRefreshGeneration++;
  publishStateDisposed = true;
  publishStateGeneration++;
  document.removeEventListener('visibilitychange', publishVisibilityChanged);
  clearTimeout(draftTimer);
  refreshInventory.cancel(); refreshDeployment.cancel();
  window.removeEventListener('proxy-deployment-status', refreshDeployment);
  window.removeEventListener('proxy-draft-changed', queueDraftRefresh);
  window.removeEventListener('proxy-node-status', onNodeStatus);
  window.removeEventListener('proxy-node-runtime', onNodeRuntime);
  window.removeEventListener('proxy-node-runtime-open', resyncInventory);
});
</script>
