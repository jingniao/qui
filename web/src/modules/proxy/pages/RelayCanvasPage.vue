<template>
  <div class="h-[calc(100vh-80px)] flex flex-col space-y-3 -m-4 sm:-m-6 p-4 sm:p-6 overflow-hidden">
    <!-- 1. Top Header Bar -->
    <div class="bg-white/90 backdrop-blur-md rounded-2xl border border-black/[0.06] p-3 shadow-2xs flex flex-wrap items-center justify-between gap-3 shrink-0">
      <!-- Left: Tabs & Scheme Selector -->
      <div class="flex items-center gap-3">
        <!-- Scheme Switcher -->
        <div class="flex items-center gap-1.5">
          <label class="text-xs text-ios-secondary font-medium">方案:</label>
          <select
            v-model="activeSchemeId"
            @change="switchScheme"
            class="form-input-base !py-1 text-xs font-semibold text-ios-label !w-[200px]"
          >
            <option v-for="s in proxyNodeStore.schemes" :key="s.id" :value="s.id">
              {{ s.name }} ({{ s.executionLocation === 'client' ? '客户端' : '服务器' }})
            </option>
          </select>
          <button
            type="button"
            @click="createNewScheme"
            class="p-1 rounded-lg hover:bg-black/5 text-ios-tertiary hover:text-ios-blue transition-colors"
            title="新建中转方案"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2.2" d="M12 4v16m8-8H4" />
            </svg>
          </button>
          <span
            class="px-2 py-0.5 rounded-full text-[10px] font-bold"
            :class="!currentScheme.published ? 'bg-neutral-100 text-neutral-600' : (currentScheme.hasUnpublishedChanges ? 'bg-amber-100 text-amber-700' : 'bg-emerald-100 text-emerald-700')"
          >
            {{ !currentScheme.published ? '草稿' : (currentScheme.hasUnpublishedChanges ? '有未发布修改' : '已发布') }}
          </span>
        </div>
        <!-- One-time import of the schemes an earlier build kept in this browser -->
        <div
          v-if="pendingLocalSchemes.length"
          class="flex items-center gap-1.5 px-2 py-1 rounded-xl bg-amber-50 border border-amber-200/70 text-[11px] text-amber-800"
        >
          <span>发现本地方案 {{ pendingLocalSchemes.length }} 个，导入到服务器？</span>
          <button type="button" @click="importLocalSchemes" class="font-bold text-amber-900 hover:underline">导入</button>
          <button type="button" @click="confirmIgnoreLocal = true" class="text-amber-700 hover:underline">忽略</button>
        </div>

      </div>

      <!-- Center: Execution Target Selector (Core Requirement 3) -->
      <div class="flex items-center gap-2 bg-[#F5F5F7] px-3 py-1 rounded-xl border border-black/[0.04]">
        <span class="text-xs font-medium text-ios-secondary">执行位置:</span>
        <label class="inline-flex items-center gap-1 text-xs font-semibold cursor-pointer">
          <input
            type="radio"
            value="client"
            v-model="currentScheme.executionLocation"
            @change="handleExecutionLocationChange"
            class="text-ios-blue focus:ring-ios-blue/20"
          />
          <span :class="currentScheme.executionLocation === 'client' ? 'text-ios-blue' : 'text-ios-label'">客户端执行</span>
        </label>
        <span class="text-neutral-300">/</span>
        <label class="inline-flex items-center gap-1 text-xs font-semibold cursor-pointer">
          <input
            type="radio"
            value="server"
            v-model="currentScheme.executionLocation"
            @change="handleExecutionLocationChange"
            class="text-ios-blue focus:ring-ios-blue/20"
          />
          <span :class="currentScheme.executionLocation === 'server' ? 'text-ios-blue' : 'text-ios-label'">服务器执行</span>
        </label>
      </div>

      <!-- Right Action Tools -->
      <div class="flex items-center gap-2 shrink-0">
        <!-- Tunnel List Button -->
        <button
          type="button"
          @click="focusTunnelList"
          class="ios-btn-secondary !py-1 text-xs inline-flex items-center gap-1"
        >
          <svg class="w-3.5 h-3.5 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
          </svg>
          <span>隧道列表 ({{ currentTunnels.length }})</span>
        </button>

        <!-- Validate Button -->
        <button
          type="button"
          @click="validateScheme"
          class="ios-btn-secondary !py-1 text-xs inline-flex items-center gap-1"
        >
          <svg class="w-3.5 h-3.5 text-emerald-600" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" />
          </svg>
          <span>校验链路</span>
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
          <span>保存草稿</span>
        </button>

        <button type="button" class="ios-btn-secondary !py-1 text-xs" @click="checkScheme">检查</button>

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
      </div>
    </div>

    <!-- 2. Main 3-Column Studio Layout -->
    <div class="flex-1 flex gap-3 min-h-0 overflow-hidden">
      <!-- ================= Left: Resource Library (Col 1) ================= -->
      <div class="w-64 xl:w-72 bg-white rounded-2xl border border-black/[0.06] shadow-2xs flex flex-col shrink-0 overflow-hidden">
        <!-- Resource Search & Header -->
        <div class="p-3 border-b border-black/[0.05] space-y-2 shrink-0">
          <div class="flex items-center justify-between">
            <span class="font-bold text-xs text-ios-label">拓扑资源库</span>
            <span class="text-[10px] text-ios-tertiary">拖拽节点到画布</span>
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
        <div class="flex-1 overflow-y-auto p-2 space-y-2 text-xs">
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

          <!-- Group 2: 代理节点 (Proxy Nodes) -->
          <div class="rounded-xl border border-black/[0.04] overflow-hidden bg-neutral-50/50">
            <button
              type="button"
              @click="toggleGroup('nodes')"
              class="w-full px-2.5 py-2 flex items-center justify-between text-left font-bold text-ios-label hover:bg-black/[0.03] transition-colors"
            >
              <div class="flex items-center gap-1.5">
                <svg class="w-3.5 h-3.5 text-ios-blue transition-transform" :class="groupExpanded.nodes ? 'rotate-90' : ''" viewBox="0 0 20 20" fill="currentColor">
                  <path fill-rule="evenodd" d="M7.293 14.707a1 1 0 010-1.414L10.586 10 7.293 6.707a1 1 0 011.414-1.414l4 4a1 1 0 010 1.414l-4 4a1 1 0 01-1.414 0z" clip-rule="evenodd" />
                </svg>
                <span>代理节点</span>
              </div>
              <span class="px-1.5 py-0.2 rounded-full bg-black/[0.05] text-[10px] text-ios-secondary">
                {{ filteredNodesGroup.length }}
              </span>
            </button>

            <div v-show="groupExpanded.nodes" class="p-1 space-y-1 border-t border-black/[0.04] bg-white">
              <!-- Sub-group: 自建节点 -->
              <div class="text-[10px] font-bold text-ios-tertiary px-1.5 pt-1">自建节点</div>
              <div
                v-for="node in filteredNodesGroup.filter(n => n.sourceType === 'self_hosted')"
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

              <!-- Sub-group: 机场订阅节点 -->
              <div class="text-[10px] font-bold text-ios-tertiary px-1.5 pt-1.5">机场订阅节点</div>
              <div
                v-for="node in filteredNodesGroup.filter(n => n.sourceType === 'subscription')"
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

              <!-- Sub-group: 单独导入节点 -->
              <div class="text-[10px] font-bold text-ios-tertiary px-1.5 pt-1.5">单独导入节点</div>
              <div
                v-for="node in filteredNodesGroup.filter(n => n.sourceType === 'manual')"
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
        </div>
      </div>

      <!-- ================= Center: Infinite Visual Canvas (Col 2) ================= -->
      <div
        ref="canvasContainer"
        class="flex-1 bg-[#F9F9FB] rounded-2xl border border-black/[0.06] relative overflow-hidden select-none cursor-default"
        @wheel.prevent="onWheel"
        @mousedown="onCanvasMouseDown"
        @dragover.prevent="onCanvasDragOver"
        @drop="onCanvasDrop"
      >
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
      </div>

      <!-- ================= Right: Inspector Property Panel (Col 3) ================= -->
      <div class="w-72 xl:w-80 bg-white rounded-2xl border border-black/[0.06] shadow-2xs flex flex-col shrink-0 overflow-y-auto p-4 space-y-4">
        <!-- Panel 1: Selected Node Inspector -->
        <div v-if="selectedNode" class="space-y-4">
          <div class="flex items-center justify-between border-b border-black/[0.06] pb-2">
            <h3 class="font-bold text-xs text-ios-label">节点属性配置</h3>
            <button
              type="button"
              @click="selectedNode = null"
              class="text-xs text-ios-tertiary hover:text-ios-label"
            >
              关闭
            </button>
          </div>

          <div class="space-y-3 text-xs">
            <label class="block">
              <span class="font-medium text-ios-secondary">实例别名 (显示名称)</span>
              <input v-model="selectedNode.name" class="form-input-base text-xs mt-1" />
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
              <span class="font-bold text-ios-label text-[11px]">服务器转发配置</span>
              <label class="block">
                <span class="text-ios-secondary">监听转发端口</span>
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

        <!-- Panel 3: Global Scheme Inspector -->
        <div v-else class="space-y-4">
          <div class="border-b border-black/[0.06] pb-2">
            <h3 class="font-bold text-xs text-ios-label">方案全局概况</h3>
          </div>

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
          <TunnelList ref="tunnelList" :scheme="currentScheme" :plan="plan" :checking="checking" :error="planError" @create="openEditor()" @edit="selectConnection" />
        </div>
      </div>
    </div>

    <TunnelEditorDrawer v-if="editor" :key="editor.key" :scheme="currentScheme" :servers="servers" :context="canvasContext()"
      :connection="editor.connection" :source="editor.source" :target="editor.target" :persist="persistTunnelResult" @close="closeEditor" />
    <AppDialog v-model="openPlanSummary" title="方案发布检查" :show-confirm="false">
      <TunnelPlanSummary :plan="plan" :checking="checking" :error="planError" />
    </AppDialog>

    <!-- 4. Modal: Publish / Generate Subscription (Core Requirement 5) -->
    <AppDialog
      v-model="openPublishModal"
      :title="currentScheme.executionLocation === 'client' ? '生成客户端订阅与配置' : '发布中转方案到服务器'"
      :show-confirm="true"
      :confirm-text="currentScheme.executionLocation === 'client' ? '发布客户端方案' : '确认下发发布'"
      @confirm="handleConfirmPublish"
    >
      <div class="space-y-4 text-xs">
        <div v-if="currentScheme.executionLocation === 'client'" class="space-y-3">
          <div class="flex items-center gap-2">
            <span class="text-ios-secondary font-medium">客户端格式:</span>
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
            <div>• 状态: {{ checking ? '检查中…' : planError || (plan?.ok ? '计划检查通过' : plan?.message || '请先检查方案') }}</div>
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
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue';
import TunnelList from '../components/relay/TunnelList.vue';
import TunnelEditorDrawer from '../components/relay/TunnelEditorDrawer.vue';
import TunnelPlanSummary from '../components/relay/TunnelPlanSummary.vue';
import { useTunnelPlan } from '../components/relay/useTunnelPlan';
import { useRoute } from 'vue-router';
import PageHeader from '../../../components/PageHeader.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import { loadImports } from '../importApi';
import { nodeAPI, type NodeView } from '../../nodes/api';
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
  type RelayScheme
} from '../proxyNodes';

const route = useRoute();
const servers = ref<NodeView[]>([]);
const resourceSearch = ref('');
const activeSchemeId = ref('');

// Group collapse states
const groupExpanded = reactive({
  servers: true,
  nodes: false
});

// Canvas Transform State
const zoom = ref(1);
const panX = ref(60);
const panY = ref(60);
const isPanning = ref(false);
const panStart = { x: 0, y: 0 };

// Selection states
const selectedNode = ref<CanvasNodeInstance | null>(null);
const selectedConnection = ref<CanvasConnection | null>(null);

// Connecting state
const connectingSource = ref<CanvasNodeInstance | null>(null);
const connectingMousePos = reactive({ x: 0, y: 0 });

// Modals
const tunnelList = ref<InstanceType<typeof TunnelList>>();
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

// Active scheme reference. The placeholder only exists while a scheme is being
// created on the server; the server assigns the real id.
const currentScheme = computed<RelayScheme>(() => {
  let s = proxyNodeStore.schemes.find(item => item.id === activeSchemeId.value);
  if (!s) {
    if (proxyNodeStore.schemes.length > 0) {
      s = proxyNodeStore.schemes[0];
      activeSchemeId.value = s.id;
    } else {
      s = {
        id: 'scheme-' + Date.now(),
        name: '新建中转方案',
        executionLocation: 'server',
        nodes: [],
        connections: [],
        published: false,
        updatedAt: new Date().toISOString()
      };
      proxyNodeStore.schemes.push(s);
      activeSchemeId.value = s.id;
    }
  }
  return s;
});

// Tunnels in active scheme
const currentTunnels = computed(() => currentScheme.value.connections);
const planInput = computed(() => relaySchemeToCanvasInput(currentScheme.value, canvasContext()));
const { plan, checking, error: planError, run: runPlan } = useTunnelPlan(
  () => JSON.stringify([currentScheme.value.id, currentScheme.value.serverUpdatedAt, planInput.value]),
  () => currentScheme.value.serverUpdatedAt ? canvasAPI.plan(currentScheme.value.id, planInput.value.layout) : null
);
watch(() => currentScheme.value.id, () => { editor.value = null; });
async function checkScheme() { openPlanSummary.value = true; await runPlan(); }
async function focusTunnelList() {
  selectedNode.value = null;
  closeEditor();
  await nextTick();
  tunnelList.value?.focus();
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
  try { await saveDraft(true); showToast('已保存到方案'); }
  catch (e) { Object.assign(scheme, before); throw e; }
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

function toggleGroup(g: 'servers' | 'nodes') {
  groupExpanded[g] = !groupExpanded[g];
}

// Switching Execution Location (Core Requirement 2: Default expansion)
function applyExecutionDefaults() {
  if (currentScheme.value.executionLocation === 'client') {
    groupExpanded.nodes = true;
    groupExpanded.servers = false;
  } else {
    groupExpanded.servers = true;
    groupExpanded.nodes = false;
  }
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
    const scheme = await createCanvasScheme(`未命名方案 ${proxyNodeStore.schemes.length + 1}`, 'server', canvasContext());
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
  try {
    await deleteCanvasScheme(scheme.id);
    selectedNode.value = null;
    selectedConnection.value = null;
    activeSchemeId.value = proxyNodeStore.schemes[0]?.id || '';
    if (!proxyNodeStore.schemes.length) await createNewScheme();
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
    role: currentScheme.value.nodes.length === 0 ? 'entry' : 'relay'
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
    window.removeEventListener('mouseup', onMouseUp);
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp);
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
    window.removeEventListener('mouseup', onMouseUp);
    saveDraft();
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp);
}

// ----------------- Directed Connecting Logic -----------------
function startConnecting(e: MouseEvent, source: CanvasNodeInstance) {
  connectingSource.value = source;
  connectingMousePos.x = source.x + 224;
  connectingMousePos.y = source.y + 45;

  const onMouseMove = (ev: MouseEvent) => {
    const rect = (document.querySelector('.cursor-default') as HTMLElement)?.getBoundingClientRect();
    if (rect) {
      connectingMousePos.x = (ev.clientX - rect.left - panX.value) / zoom.value;
      connectingMousePos.y = (ev.clientY - rect.top - panY.value) / zoom.value;
    }
  };

  const onMouseUp = () => {
    connectingSource.value = null;
    window.removeEventListener('mousemove', onMouseMove);
    window.removeEventListener('mouseup', onMouseUp);
  };

  window.addEventListener('mousemove', onMouseMove);
  window.addEventListener('mouseup', onMouseUp);
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
  scheme.updatedAt = new Date().toISOString();
  if (scheme.published) {
    scheme.hasUnpublishedChanges = true;
  }
  if (immediate === true) {
    const pending = saveTimers.get(scheme);
    if (pending) { clearTimeout(pending); saveTimers.delete(scheme); }
    const flushed = saveQueue.then(() => persistCanvasScheme(scheme, canvasContext()));
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

function scheduleSave(scheme: RelayScheme) {
  const pending = saveTimers.get(scheme);
  if (pending) clearTimeout(pending);
  saveTimers.set(scheme, setTimeout(() => {
    saveTimers.delete(scheme);
    saveQueue = saveQueue.then(() => persistScheme(scheme)).catch(() => undefined);
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
  if (!proxyNodeStore.schemes.length) await createNewScheme();
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
    const flushed = saveQueue.then(() => persistCanvasScheme(scheme, canvasContext()));
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
    inbounds.value = inboundRes;
  } catch {}

  // Schemes are stored by the server now; the browser keeps no copy of them.
  await reloadSchemes();
  readLocalSchemes();

  const qSchemeId = route.query.schemeId as string | undefined;
  const qFocusConnId = route.query.focusConnId as string | undefined;
  const qFocusInstId = route.query.focusInstId as string | undefined;

  if (qSchemeId && proxyNodeStore.schemes.some(s => s.id === qSchemeId)) {
    activeSchemeId.value = qSchemeId;
  }
  if (!proxyNodeStore.schemes.length) {
    await createNewScheme();
  }
  applyExecutionDefaults();

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
</script>
