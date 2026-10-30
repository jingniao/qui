<template>
  <div class="space-y-6">
    <PageHeader
      :title="node?.name || '服务器详情'"
      parent="服务器"
      description="以“列表看状态、详情做管理”为原则，统一管理服务器基础信息、监控、线路、配置与调优。"
    >
      <template #actions>
        <router-link to="/nodes" class="ios-btn-secondary inline-flex items-center gap-1.5">
          <svg class="w-3.5 h-3.5 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10 19l-7-7m0 0l7-7m-7 7h18" />
          </svg>
          <span>返回列表</span>
        </router-link>
        <button @click="load" class="ios-btn-secondary inline-flex items-center gap-1.5">
          <svg class="w-3.5 h-3.5 text-ios-secondary" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>刷新</span>
        </button>
        <button
          v-if="isOwner && node && !node.deletedAt"
          @click="selectTab('settings')"
          class="ios-btn-primary inline-flex items-center gap-1.5"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
          </svg>
          <span>管理设置</span>
        </button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!node" />

    <template v-else>
      <!-- 全局异常警报 -->
      <section v-if="node.heartbeat?.core_healthy === false" class="panel-card p-5 border border-amber-300 bg-amber-50 text-sm text-amber-900" role="status">
        <p class="font-semibold">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '代理已停止，流量消费需要核对' : '代理核心未通过健康检查' }}</p>
        <p class="mt-2">{{ node.heartbeat.core_error === 'accounting_interrupted' ? '服务器保持管理连接，但不会根据旧计数重新放行。已采集流量会继续补传；无法确认的消费仍需处理。' : '服务器仍可连接管理端。代理恢复前，请检查核心运行状态和最近配置发布结果。' }}</p>
        <p class="mt-2 text-xs">以上为最后一次心跳上报的状态。</p>
      </section>

      <!-- 顶部状态全貌卡片：明确区分 Agent 在线、核心健康和接入可用 -->
      <section class="panel-card p-5">
        <div class="flex flex-col md:flex-row md:items-center justify-between gap-4">
          <div class="flex items-center gap-3">
            <div class="w-12 h-12 rounded-2xl bg-black/[0.04] flex items-center justify-center text-ios-blue shrink-0">
              <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
              </svg>
            </div>
            <div>
              <div class="flex items-center gap-2">
                <span class="text-2xl select-none leading-none cursor-default" :title="`${countryInfo.name} (${countryInfo.source === 'admin_region' ? '管理员指定地区' : 'IP归属识别'})`">{{ countryInfo.flag }}</span>
                <h2 class="text-xl font-bold text-ios-label tracking-tight">{{ node.name }}</h2>
                <span v-if="node.deletedAt" class="rounded-full bg-black/[0.05] px-2 py-0.5 text-xs font-semibold text-ios-secondary">已删除</span>
              </div>
              <p class="text-xs text-ios-tertiary font-mono mt-0.5">
                {{ node.publicIp || node.hostname || '未分配 IP' }} · {{ nodeMeta.region || '未指定地区' }}
                <span v-if="nodeMeta.remark" class="ml-1 text-ios-secondary">({{ nodeMeta.remark }})</span>
              </p>
            </div>
          </div>

          <!-- 三态分离指示器 -->
          <div class="flex flex-wrap items-center gap-2.5">
            <!-- 1. Agent 状态 -->
            <div class="flex items-center gap-1.5 px-3 py-1.5 rounded-xl border text-xs font-medium" :class="agentBadgeClass(node)">
              <span class="w-2 h-2 rounded-full" :class="agentDotClass(node)"></span>
              <span>Agent: {{ agentStatusText(node) }}</span>
            </div>
            <!-- 2. 核心健康 -->
            <div class="flex items-center gap-1.5 px-3 py-1.5 rounded-xl border text-xs font-medium" :class="coreHealthBadgeClass(node)">
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
              <span>核心健康: {{ coreHealthText(node) }}</span>
            </div>
            <!-- 3. 接入可用 -->
            <div class="flex items-center gap-1.5 px-3 py-1.5 rounded-xl border text-xs font-medium" :class="accessUsableBadgeClass(node)">
              <span>接入服务: {{ accessUsableText(node) }}</span>
            </div>
          </div>
        </div>
      </section>

      <!-- iOS 分段药丸导航（6 大 Tab） -->
      <div class="ios-segmented-container inline-flex overflow-x-auto max-w-full">
        <button
          v-for="tab in visibleTabs"
          :key="tab.key"
          type="button"
          @click="selectTab(tab.key)"
          :class="['ios-segmented-item whitespace-nowrap', activeTab === tab.key ? 'active' : '']"
        >
          {{ tab.label }}
        </button>
      </div>

      <!-- ================= Tab 1: 概况 (Overview) ================= -->
      <div v-if="activeTab === 'overview'" class="space-y-6">
        <!-- 基础信息卡片 -->
        <section class="panel-card p-6">
          <h2 class="font-semibold mb-4 text-[17px] text-ios-label tracking-tight">基础信息</h2>
          <dl class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-5 text-sm">
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">服务器 ID</dt>
              <dd class="font-mono break-all mt-1 text-sm font-medium text-ios-label">{{ node.id }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">公网 IP / 接入地址</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono tabular-nums">{{ node.publicIp || '尚未上报' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">主机名</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label">{{ node.hostname || '等待注册' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">地区</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label flex items-center gap-1.5">
                <span class="text-base select-none leading-none cursor-default">{{ countryInfo.flag }}</span>
                <span>{{ nodeMeta.region || countryInfo.name || '未指定地区' }}</span>
                <span class="text-[10px] text-ios-tertiary font-normal">({{ countryInfo.source === 'admin_region' ? '管理员指定' : 'IP归属识别' }})</span>
              </dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">服务器备注</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label">{{ nodeMeta.remark || '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">操作系统</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label">{{ node.os || '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">Agent 版本</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono">{{ node.agentVersion || '—' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">代理核心</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono">
                {{ node.capabilities?.core_name || '未检测' }} {{ node.capabilities?.core_version || '' }}
              </dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">最后心跳</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono tabular-nums">{{ formatTime(node.lastHeartbeat) }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">主机运行时间 / 核心运行时间</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono">{{ calculateUptimeText() }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">到期时间</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono" :class="remainingDaysInfo.isNear ? 'text-amber-600 font-semibold' : ''">
                {{ nodeMeta.renewDate || '未设置' }}
                <span v-if="nodeMeta.renewDate" class="text-xs font-normal text-ios-tertiary ml-1">({{ remainingDaysInfo.label }})</span>
              </dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">续费价格</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono">
                <span v-if="renewPriceDisplay" class="font-bold text-neutral-900 bg-black/[0.05] px-2 py-0.5 rounded text-xs">{{ renewPriceDisplay }}</span>
                <span v-else class="text-ios-tertiary">未设置</span>
              </dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">创建时间</dt>
              <dd class="mt-1 text-sm font-medium text-ios-label font-mono tabular-nums">{{ formatTime(node.createdAt) }}</dd>
            </div>
          </dl>
        </section>

        <!-- 实时资源占用卡片 -->
        <section class="panel-card p-6">
          <div class="flex items-center justify-between mb-4">
            <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">实时资源</h2>
            <span class="text-xs text-ios-tertiary">采样窗口约 10~30 秒，随心跳刷新</span>
          </div>
          <div class="grid grid-cols-1 md:grid-cols-3 gap-6">
            <div class="p-4 rounded-2xl bg-black/[0.02] border border-black/[0.04] space-y-2">
              <span class="text-xs font-medium text-ios-tertiary">CPU 使用率</span>
              <p class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ formatResource(node.heartbeat?.cpu_percent) }}</p>
              <div class="h-2 w-full bg-black/[0.06] rounded-full overflow-hidden">
                <div class="h-full rounded-full transition-all duration-300" :class="progressColor(node.heartbeat?.cpu_percent)" :style="{ width: `${Math.min(node.heartbeat?.cpu_percent ?? 0, 100)}%` }"></div>
              </div>
            </div>
            <div class="p-4 rounded-2xl bg-black/[0.02] border border-black/[0.04] space-y-2">
              <span class="text-xs font-medium text-ios-tertiary">内存使用率</span>
              <p class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ formatResource(node.heartbeat?.memory_percent) }}</p>
              <div class="h-2 w-full bg-black/[0.06] rounded-full overflow-hidden">
                <div class="h-full rounded-full transition-all duration-300" :class="progressColor(node.heartbeat?.memory_percent)" :style="{ width: `${Math.min(node.heartbeat?.memory_percent ?? 0, 100)}%` }"></div>
              </div>
            </div>
            <div class="p-4 rounded-2xl bg-black/[0.02] border border-black/[0.04] space-y-2">
              <span class="text-xs font-medium text-ios-tertiary">磁盘使用率</span>
              <p class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ formatResource(node.heartbeat?.disk_percent) }}</p>
              <div class="h-2 w-full bg-black/[0.06] rounded-full overflow-hidden">
                <div class="h-full rounded-full transition-all duration-300" :class="progressColor(node.heartbeat?.disk_percent)" :style="{ width: `${Math.min(node.heartbeat?.disk_percent ?? 0, 100)}%` }"></div>
              </div>
            </div>
          </div>
        </section>

        <!-- 关联入站与所属线路组 -->
        <div class="grid grid-cols-1 lg:grid-cols-2 gap-6">
          <!-- 关联入站列表 -->
          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between mb-3">
                <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">关联入站服务</h2>
                <router-link v-if="isOwner" to="/proxy/nodes" class="text-xs text-ios-blue hover:underline">管理入站</router-link>
              </div>
              <p class="text-xs text-ios-tertiary mb-3">运行在此服务器上的代理协议入站端口。</p>
              <div v-if="associatedInbounds.length" class="divide-y divide-black/[0.04]">
                <div v-for="inbound in associatedInbounds" :key="inbound.id" class="py-2.5 flex items-center justify-between text-xs">
                  <div>
                    <span class="font-semibold text-ios-label">{{ inbound.name }}</span>
                    <span class="ml-2 font-mono text-[11px] px-1.5 py-0.5 rounded bg-black/[0.04] uppercase">{{ inbound.protocol }}</span>
                    <span class="ml-1.5 font-mono text-ios-tertiary">:{{ inbound.port }}</span>
                  </div>
                  <span class="text-[11px] font-medium" :class="inbound.enabled ? 'text-emerald-600' : 'text-ios-tertiary'">
                    {{ inbound.enabled ? '已启用' : '已停用' }}
                  </span>
                </div>
              </div>
              <p v-else class="text-xs text-ios-tertiary py-4 text-center">暂未在此服务器上创建入站配置</p>
            </div>
          </section>

          <!-- 所属线路组 -->
          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between mb-3">
                <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">所属线路组</h2>
                <router-link v-if="isOwner" to="/proxy/groups" class="text-xs text-ios-blue hover:underline">管理线路组</router-link>
              </div>
              <p class="text-xs text-ios-tertiary mb-3">服务器所在的逻辑分组与用途编排。</p>
              <div v-if="associatedGroups.length" class="flex flex-wrap gap-2">
                <template v-for="group in associatedGroups" :key="group.id">
                  <router-link v-if="isOwner" :to="{ path: '/proxy/groups', query: { group: group.id } }"
                    class="rounded-full bg-ios-blue/10 px-3 py-1 text-xs text-ios-blue active:scale-95 transition-transform" :class="{ 'opacity-50': !group.enabled }">
                    {{ group.name }} · {{ purposeText(group.purpose) }} · {{ group.enabled ? '已启用' : '已停用' }}
                  </router-link>
                  <span v-else class="rounded-full bg-ios-bg px-3 py-1 text-xs text-ios-secondary">{{ group.name }}</span>
                </template>
              </div>
              <p v-else class="text-xs text-ios-tertiary py-4 text-center">当前未加入任何线路组（可前往“设置”分配）</p>
            </div>
          </section>
        </div>

        <!-- 核心协议能力矩阵 -->
        <section class="panel-card p-6">
          <h2 class="font-semibold mb-2 text-[17px] text-ios-label tracking-tight">核心与协议能力</h2>
          <p class="text-xs text-ios-secondary mb-4">{{ node.capabilities?.core_name ? `${node.capabilities.core_name} ${node.capabilities.core_version || ''}` : '尚未上报代理核心，无法判断协议能力。' }}</p>
          <table v-if="node.capabilities?.protocols?.length" class="w-full text-xs">
            <thead>
              <tr class="border-b border-black/[0.06] text-left text-ios-tertiary uppercase tracking-wider">
                <th class="py-2 font-medium">协议</th>
                <th class="font-medium">入站</th>
                <th class="font-medium">出站</th>
                <th class="font-medium">TCP</th>
                <th class="font-medium">UDP</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-black/[0.04]">
              <tr v-for="cap in node.capabilities.protocols" :key="cap.protocol" class="text-ios-label">
                <td class="py-2 font-semibold font-mono">{{ cap.protocol }}</td>
                <td>{{ cap.inbound ? '支持' : '—' }}</td>
                <td>{{ cap.outbound ? '支持' : '—' }}</td>
                <td>{{ cap.tcp ? '支持' : '—' }}</td>
                <td>{{ cap.udp ? '支持' : '—' }}</td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>

      <!-- ================= Tab 2: 监控 (Monitoring) ================= -->
      <div v-else-if="activeTab === 'monitoring'" class="space-y-6">
        <!-- 实时与累计流量卡片 -->
        <div class="grid grid-cols-1 md:grid-cols-2 gap-6">
          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <h2 class="font-semibold mb-2 text-[17px] text-ios-label tracking-tight">网络流量统计</h2>
              <p class="text-xs text-ios-tertiary mb-4">统计范围：自核心本次启动以来的核心计数（Xray 来自 StatsService，sing-box 来自 Clash API）</p>
              <dl class="grid grid-cols-2 gap-4 text-sm">
                <div>
                  <dt class="text-xs font-medium text-ios-tertiary">核心累计上传</dt>
                  <dd class="mt-2 text-2xl font-bold text-ios-label tabular-nums font-mono">
                    <TrafficValue v-if="node.heartbeat?.upload_bytes !== undefined" :bytes="node.heartbeat.upload_bytes" />
                    <span v-else>—</span>
                  </dd>
                </div>
                <div>
                  <dt class="text-xs font-medium text-ios-tertiary">核心累计下载</dt>
                  <dd class="mt-2 text-2xl font-bold text-ios-label tabular-nums font-mono">
                    <TrafficValue v-if="node.heartbeat?.download_bytes !== undefined" :bytes="node.heartbeat.download_bytes" />
                    <span v-else>—</span>
                  </dd>
                </div>
              </dl>
            </div>
            <p class="text-xs text-ios-tertiary mt-4 leading-relaxed">
              核心重启后从零重新累计，不影响按月出账账单统计。<span v-if="node.capabilities?.core_name === 'sing-box' && !node.capabilities?.singbox_user_stats">此 sing-box 核心仅上报节点总量，无法按用户统计限额。</span>
            </p>
          </section>

          <section class="panel-card p-6 flex flex-col justify-between">
            <div>
              <h2 class="font-semibold mb-2 text-[17px] text-ios-label tracking-tight">当前活跃连接与实时速率</h2>
              <p class="text-xs text-ios-tertiary mb-4">统计对象：去重活跃客户端 IP 会话数（有效窗口 45 秒）</p>
              <div class="grid grid-cols-2 gap-4">
                <div>
                  <span class="text-xs font-medium text-ios-tertiary">活跃客户端会话</span>
                  <p class="text-3xl font-bold text-ios-label tabular-nums tracking-tight mt-1">{{ connectionCount() }}</p>
                </div>
                <div>
                  <span class="text-xs font-medium text-ios-tertiary">实时上传 / 下载速率</span>
                  <p class="text-sm font-mono text-ios-tertiary mt-2">↑ — / ↓ —</p>
                  <span class="text-[10px] text-ios-quaternary block mt-0.5">心跳尚未接入瞬时速率采样</span>
                </div>
              </div>
            </div>
            <p class="text-xs text-ios-tertiary mt-4 leading-relaxed">
              Xray 节点上报该用户的去重客户端 IP 数（回环地址排除）；sing-box 节点不提供每用户在线数，显示未知。
            </p>
          </section>
        </div>

        <!-- 本地断线缓存 (Spool 状态) -->
        <section class="panel-card p-6 space-y-4">
          <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">本地断线缓存与日志队列</h2>
          <p class="text-sm" :class="spoolFresh() && node.heartbeat.spool_status?.status === 'capacity_exceeded' ? 'text-ios-red font-medium' : 'text-ios-secondary'">
            {{ spoolLabel() }}
          </p>
          <dl v-if="spoolFresh() && node.heartbeat.spool_status?.measured" class="grid sm:grid-cols-2 lg:grid-cols-4 gap-4 text-sm">
            <div><dt class="text-xs font-medium text-ios-tertiary">实际缓存文件占用 / 总预算</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.disk_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.disk_limit) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">数据库页占用 / 预留上限</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.database_bytes) }} / {{ cacheBytes(node.heartbeat.spool_status.database_limit) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">加密记录载荷</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ cacheBytes(node.heartbeat.spool_status.payload_bytes) }}</dd></div>
            <div><dt class="text-xs font-medium text-ios-tertiary">待补传批次 / 淘汰记录</dt><dd class="mt-1 font-mono tabular-nums text-sm font-medium text-ios-label">{{ node.heartbeat.spool_status.pending_batches ?? 0 }} / {{ node.heartbeat.spool_status.dropped_allowed_rollups ?? 0 }}</dd></div>
          </dl>
        </section>

        <!-- 待办：时序趋势监控指标说明 -->
        <section class="panel-card p-6 border-dashed border-2 border-black/[0.08] bg-black/[0.01]">
          <div class="flex items-start gap-3">
            <div class="w-8 h-8 rounded-lg bg-ios-blue/10 text-ios-blue flex items-center justify-center shrink-0 mt-0.5">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
              </svg>
            </div>
            <div class="space-y-1">
              <h3 class="font-semibold text-sm text-ios-label">待办功能：历史时序趋势监控（1小时 / 24小时 / 7天）</h3>
              <p class="text-xs text-ios-secondary leading-relaxed">
                当前控制面采用心跳瞬时指标展示真实运行状态。为保证数据真实透明，不使用静态伪造假图表冒充。历史趋势图将在后端时序探针（Prometheus/InfluxDB 协议）与 Agent 高频时序指标采集接入后自动点亮。
              </p>
            </div>
          </div>
        </section>
      </div>

      <!-- ================= Tab 3: 线路检测 (Line Test) ================= -->
      <div v-else-if="activeTab === 'testing'" class="space-y-6">
        <!-- 线路探测概览卡片 -->
        <section class="panel-card p-6 space-y-4">
          <div class="flex flex-wrap items-center justify-between gap-3">
            <div>
              <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">线路质量与管理通道探测</h2>
              <p class="text-xs text-ios-tertiary mt-1">测试方向：中央控制端 ➔ 节点 Agent（主动心跳链路与健康探测）</p>
            </div>
            <button
              v-if="isOwner && !node.deletedAt"
              type="button"
              :disabled="testingHealth"
              @click="triggerHealthCheck"
              class="ios-btn-primary inline-flex items-center gap-1.5"
            >
              <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 10V3L4 14h7v7l9-11h-7z" />
              </svg>
              <span>{{ testingHealth ? '检测中…' : '立即重测管理通道' }}</span>
            </button>
          </div>

          <div class="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-2">
            <div class="p-3.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
              <span class="text-xs text-ios-tertiary">控制通道状态</span>
              <p class="text-lg font-bold mt-1" :class="isAgentOnline(node) ? 'text-emerald-600' : 'text-ios-tertiary'">
                {{ isAgentOnline(node) ? '双向可达' : '离线 / 超时' }}
              </p>
            </div>
            <div class="p-3.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
              <span class="text-xs text-ios-tertiary">最近心跳延迟</span>
              <p class="text-lg font-bold font-mono text-ios-label mt-1">
                {{ node.lastHeartbeat ? `${Math.max(0, Math.round((clockNow - Date.parse(node.lastHeartbeat)) / 1000))} 秒前` : '—' }}
              </p>
            </div>
            <div class="p-3.5 rounded-xl bg-black/[0.02] border border-black/[0.04]">
              <span class="text-xs text-ios-tertiary">历史丢包率</span>
              <p class="text-lg font-bold font-mono text-ios-label mt-1">
                —
              </p>
            </div>
          </div>
        </section>

        <!-- 核心健康检查与重测记录 -->
        <NodeHealthPanel v-if="isOwner && !node.deletedAt" :node="node" />

        <!-- 待办：多目标网络质量探针 -->
        <section class="panel-card p-6 border-dashed border-2 border-black/[0.08] bg-black/[0.01]">
          <div class="flex items-start gap-3">
            <div class="w-8 h-8 rounded-lg bg-ios-blue/10 text-ios-blue flex items-center justify-center shrink-0 mt-0.5">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064" />
              </svg>
            </div>
            <div class="space-y-1">
              <TCPProbesPanel v-if="isOwner" :node-id="routeNodeId" />
              <p v-else class="text-xs text-ios-secondary">出口探针由 Owner 管理。</p>
            </div>
          </div>
        </section>
      </div>

      <!-- ================= Tab 4: 程序与配置 (Program & Config) ================= -->
      <div v-else-if="activeTab === 'config'" class="space-y-6">
        <!-- 核心健康检查与重启 -->
        <NodeHealthPanel v-if="isOwner && !node.deletedAt" :node="node" />
        <NodeRestartPanel v-if="isOwner && !node.deletedAt" :node="node" />

        <!-- 配置同步状态与版本回滚 -->
        <section class="panel-card p-6 space-y-4">
          <div class="flex items-center justify-between gap-3">
            <div>
              <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">配置发布记录与版本回滚</h2>
              <p class="text-xs text-ios-tertiary mt-1">
                当前配置版本: <span class="font-mono font-medium text-ios-label">#{{ node.heartbeat?.current_revision ?? '—' }}</span> ·
                成功应用版本: <span class="font-mono font-medium text-ios-label">#{{ node.heartbeat?.successful_revision ?? '—' }}</span>
              </p>
            </div>
            <div class="flex items-center gap-2.5 shrink-0">
              <button
                v-if="isOwner && !node.deletedAt"
                type="button"
                @click="publishCurrentVisible = true"
                class="ios-action-btn-primary"
                :disabled="publishingCurrent"
              >
                {{ publishingCurrent ? '发布中…' : '发布当前配置' }}
              </button>
              <span
                class="px-2.5 py-1 rounded-full text-xs font-semibold"
                :class="node.heartbeat?.current_revision === node.heartbeat?.successful_revision ? 'bg-emerald-50 text-emerald-600' : 'bg-amber-50 text-amber-600'"
              >
                {{ node.heartbeat?.current_revision === node.heartbeat?.successful_revision ? '已同步' : '版本待同步' }}
              </span>
            </div>
          </div>

          <div v-if="recentRevisions.length" class="divide-y divide-black/[0.05]">
            <div v-for="rev in recentRevisions" :key="rev.id" class="py-3 flex items-center justify-between text-xs gap-3">
              <div>
                <span class="font-semibold text-ios-label">#{{ rev.id }}</span>
                <span class="text-ios-secondary ml-2">{{ rev.summary }}</span>
                <span class="text-ios-tertiary ml-2">{{ formatTime(rev.createdAt) }}</span>
                <span class="ml-2 font-mono px-1.5 py-0.5 rounded bg-black/[0.04] text-ios-secondary">{{ revisionStatusLabel(rev.status) }}</span>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <button type="button" @click="openDiff(rev)" class="ios-action-btn-secondary">
                  查看差异
                </button>
                <button
                  v-if="isOwner && rev.status === 'draft'"
                  type="button"
                  @click="confirmPublishRevision(rev.id)"
                  class="ios-action-btn-primary"
                  :disabled="publishingRevision"
                >
                  {{ publishingRevision && publishRevisionId === rev.id ? '发布中…' : '发布' }}
                </button>
                <button
                  v-if="isOwner && rev.status === 'success' && rev.id !== node.heartbeat?.successful_revision"
                  type="button"
                  @click="openRollback(rev.id)"
                  class="ios-action-btn-secondary"
                  :disabled="rollingBack"
                >
                  {{ rollingBack ? '回滚中…' : '回滚到此版本' }}
                </button>
              </div>
            </div>
          </div>
          <p v-else class="text-xs text-ios-tertiary py-2">暂无该节点的配置版本记录</p>
        </section>

        <!-- 证书与登记管理 -->
        <NodeCertificatePanel v-if="isOwner && !node.deletedAt" :node="node" />
      </div>

      <!-- ================= Tab 5: 调优 (Tuning) ================= -->
      <div v-else-if="activeTab === 'tuning'" class="space-y-6">
        <!-- 当前网络参数卡片 -->
        <section class="panel-card p-6 space-y-4">
          <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">当前网络与出站参数</h2>
          <p class="text-xs text-ios-tertiary leading-relaxed">
            核心出站源 IP/网卡只影响代理核心转发的流量；服务器强制出站作用于整台服务器经该网卡新建的出站连接。
          </p>
          <dl class="grid grid-cols-1 md:grid-cols-2 gap-5 text-sm pt-2">
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">核心出站源 IP</dt>
              <dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.outboundSourceIp || '— (未配置)' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">核心出站网卡</dt>
              <dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.outboundInterface || '— (未配置)' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">服务器强制出站 IPv4</dt>
              <dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.hostSourceIpv4 || '— (未配置)' }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium text-ios-tertiary">服务器强制出站 IPv6</dt>
              <dd class="mt-1 font-mono break-all text-sm font-medium text-ios-label">{{ node.hostSourceIpv6 || '— (未配置)' }}</dd>
            </div>
          </dl>
          <div class="pt-2">
            <button v-if="isOwner" type="button" @click="selectTab('settings')" class="ios-btn-secondary text-xs">
              修改出站网络参数
            </button>
          </div>
        </section>

        <!-- 待办：内核基础调优、BBR、测速验证 -->
        <section class="panel-card p-6 border-dashed border-2 border-black/[0.08] bg-black/[0.01] space-y-4">
          <div class="flex items-start gap-3">
            <div class="w-8 h-8 rounded-lg bg-ios-blue/10 text-ios-blue flex items-center justify-center shrink-0 mt-0.5">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 6V4m0 2a2 2 0 100 4m0-4a2 2 0 110 4m-6 8a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4m6 6v10m6-2a2 2 0 100-4m0 4a2 2 0 110-4m0 4v2m0-6V4" />
              </svg>
            </div>
            <div class="space-y-1">
              <h3 class="font-semibold text-sm text-ios-label">待办功能：主机内核网络基础调优 (BBR / TCP 缓冲区)</h3>
              <p class="text-xs text-ios-secondary leading-relaxed">
                规划支持一键开启 Linux BBR 拥塞控制、优化 TCP Socket 缓冲区（`rmem/wmem`）、扩展最大连接队列（`somaxconn`）以及文件描述符上限（`nofile`），并记录执行记录与支持一键恢复原配置。
              </p>
            </div>
          </div>
          <div class="p-3 rounded-xl bg-white/60 border border-black/[0.04] text-xs text-ios-tertiary space-y-1.5 font-mono">
            <p>当前值（只读）</p>
            <p class="text-ios-label">net.core.default_qdisc = {{ currentHost.default_qdisc ?? '—' }}</p>
            <p class="text-ios-label">net.ipv4.tcp_congestion_control = {{ currentHost.tcp_congestion_control ?? '—' }}</p>
          </div>
        </section>
      </div>

      <!-- ================= Tab 6: 设置 (Settings) ================= -->
      <div v-else-if="activeTab === 'settings' && isOwner" class="space-y-6">
        <form @submit.prevent="saveAllSettings" class="panel-card p-6 space-y-5">
          <h2 class="font-semibold text-[17px] text-ios-label tracking-tight">服务器综合管理设置</h2>

          <!-- 基础设置 -->
          <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
            <label class="block">
              <span class="text-xs font-medium text-ios-secondary">服务器名称</span>
              <input v-model="settingsForm.name" maxlength="40" class="form-input-base mt-1.5" />
            </label>
            <label class="block">
              <span class="text-xs font-medium text-ios-secondary">订阅接入 IP / 域名</span>
              <input v-model="settingsForm.publicIp" placeholder="例如 203.0.113.10" class="form-input-base mt-1.5" />
            </label>
            <label class="block">
              <span class="text-xs font-medium text-ios-secondary">地区</span>
              <input v-model="settingsForm.region" placeholder="例如 香港、日本东京、美国洛杉矶" class="form-input-base mt-1.5" />
            </label>
            <label class="block">
              <span class="text-xs font-medium text-ios-secondary">服务器备注</span>
              <input v-model="settingsForm.remark" placeholder="机型、服务商等备注信息" class="form-input-base mt-1.5" />
            </label>
          </div>

          <!-- 维护与接入状态 -->
          <div class="border-t border-black/[0.06] pt-4 space-y-2.5">
            <p class="text-xs font-semibold text-ios-secondary">维护与接入控制</p>
            <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-ios-label">
              <input v-model="settingsForm.enabled" type="checkbox" class="rounded border-black/20" />
              <span>启用服务器控制连接（停用会拒绝其心跳与指令通信）</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-ios-label">
              <input v-model="settingsForm.allowSubscription" type="checkbox" class="rounded border-black/20" />
              <span>允许用户订阅（关闭后此服务器的入站不会分发给订阅用户）</span>
            </label>
            <label class="flex items-center gap-2 cursor-pointer text-xs font-medium text-ios-label">
              <input v-model="settingsForm.maintenance" type="checkbox" class="rounded border-black/20" />
              <span>设为维护模式（临时排除在用户订阅之外）</span>
            </label>
          </div>

          <!-- 关联线路组 -->
          <div class="border-t border-black/[0.06] pt-4 space-y-2.5">
            <p class="text-xs font-semibold text-ios-secondary">所属线路组</p>
            <p class="text-xs text-ios-tertiary">勾选服务器加入的线路组。修改后将自动更新对应线路组关联。</p>
            <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2 pt-1">
              <label
                v-for="g in allGroups"
                :key="g.id"
                class="flex items-center gap-2 p-2.5 rounded-xl border border-black/[0.06] hover:bg-black/[0.02] cursor-pointer text-xs"
              >
                <input
                  type="checkbox"
                  :value="g.id"
                  v-model="settingsForm.selectedGroupIds"
                  class="rounded border-black/20"
                />
                <span class="font-medium text-ios-label truncate">{{ g.name }}</span>
                <span class="text-[10px] text-ios-tertiary shrink-0">({{ purposeText(g.purpose) }})</span>
              </label>
            </div>
          </div>

          <!-- 续费提醒设置 -->
          <div class="border-t border-black/[0.06] pt-4 space-y-3">
            <p class="text-xs font-semibold text-ios-secondary">续费到期与费用设置</p>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
              <label class="block">
                <span class="text-xs font-medium text-ios-tertiary">到期时间 / 续费日</span>
                <input v-model="settingsForm.renewDate" type="date" class="form-input-base mt-1" />
              </label>

              <label class="block">
                <span class="text-xs font-medium text-ios-tertiary">提前提醒天数</span>
                <input v-model.number="settingsForm.renewNoticeDays" type="number" min="1" max="60" class="form-input-base mt-1" />
              </label>

              <div class="block md:col-span-2">
                <span class="text-xs font-medium text-ios-tertiary">续费价格与周期</span>
                <div class="flex items-center mt-1 rounded-xl border border-black/15 bg-white overflow-hidden shadow-2xs focus-within:ring-2 focus-within:ring-ios-blue/20 focus-within:border-ios-blue transition-all">
                  <select
                    v-model="settingsForm.renewCurrency"
                    class="bg-neutral-50 hover:bg-neutral-100 text-neutral-800 font-semibold text-xs px-3 py-2.5 border-r border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                  >
                    <option v-for="c in CURRENCY_LIST" :key="c.code" :value="c.code">{{ c.label }}</option>
                  </select>
                  <input
                    v-model="settingsForm.renewPrice"
                    type="number"
                    step="any"
                    min="0"
                    placeholder="输入续费金额，例如 120"
                    class="flex-1 min-w-0 px-3 py-2 text-xs font-mono font-medium text-neutral-900 placeholder:text-neutral-400 focus:outline-none bg-transparent"
                  />
                  <select
                    v-model="settingsForm.renewCycle"
                    class="bg-neutral-50 hover:bg-neutral-100 text-neutral-700 font-semibold text-xs px-3 py-2.5 border-l border-black/10 focus:outline-none shrink-0 cursor-pointer transition-colors"
                  >
                    <option v-for="cycle in CYCLE_LIST" :key="cycle" :value="cycle">{{ cycle }}</option>
                  </select>
                </div>
              </div>
            </div>
            <p v-if="settingsForm.renewDate" class="text-xs" :class="remainingDaysInfo.isNear ? 'text-amber-600 font-medium' : 'text-ios-tertiary'">
              当前计算：{{ calculateRemainingDays(settingsForm.renewDate).label }}
            </p>
          </div>

          <!-- 出站网络配置 -->
          <div class="border-t border-black/[0.06] pt-4 space-y-3">
            <p class="text-xs font-semibold text-ios-secondary">出站网络与源 IP 绑定</p>
            <p class="text-xs text-ios-tertiary">发布配置后生效；留空表示不限制出站源地址。</p>
            <div class="grid grid-cols-1 md:grid-cols-2 gap-4 text-sm">
              <label class="block">
                <span class="text-xs font-medium text-ios-secondary">核心出站源 IP</span>
                <input v-model="settingsForm.outboundSourceIp" placeholder="例如 203.0.113.10" class="form-input-base mt-1" />
              </label>
              <label class="block">
                <span class="text-xs font-medium text-ios-secondary">核心出站网卡</span>
                <input v-model="settingsForm.outboundInterface" maxlength="15" placeholder="例如 eth0" class="form-input-base mt-1" />
              </label>
              <label class="block">
                <span class="text-xs font-medium text-ios-secondary">服务器强制出站 IPv4</span>
                <input v-model="settingsForm.hostSourceIpv4" placeholder="例如 203.0.113.10" class="form-input-base mt-1" />
              </label>
              <label class="block">
                <span class="text-xs font-medium text-ios-secondary">服务器强制出站 IPv6</span>
                <input v-model="settingsForm.hostSourceIpv6" placeholder="例如 2001:db8::10" class="form-input-base mt-1" />
              </label>
            </div>
          </div>

          <p v-if="networkErrorText" role="alert" class="text-sm text-ios-red">{{ networkErrorText }}</p>

          <div class="pt-2 flex items-center justify-end gap-3">
            <button type="submit" class="ios-btn-primary" :disabled="saving">
              {{ saving ? '保存中…' : '保存全部设置' }}
            </button>
          </div>
        </form>

        <!-- 危险操作区 -->
        <section v-if="isOwner" class="panel-card p-6 border border-rose-200 bg-rose-50/20 space-y-4">
          <h2 class="font-semibold text-[17px] text-ios-red tracking-tight">危险区域</h2>
          <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 text-sm">
            <div>
              <p class="font-semibold text-ios-label">吊销服务器证书</p>
              <p class="text-xs text-ios-tertiary mt-0.5">吊销后该服务器不能再使用当前证书与中央端建立控制连接。</p>
            </div>
            <button type="button" @click="revokeVisible = true" class="ios-btn-danger shrink-0">
              吊销证书
            </button>
          </div>
        </section>
      </div>
    </template>

    <ConfirmDialog
      v-model="revokeVisible"
      title="吊销服务器证书"
      message="吊销后，此服务器不能再使用当前证书建立控制连接，已建立的连接也会在下一次消息校验时关闭。"
      @confirm="revoke"
    />

    <ConfirmDialog
      v-model="rollbackVisible"
      title="回滚配置版本"
      :message="`确认将该节点回退到版本 #${rollbackRevision ?? ''}？`"
      @confirm="confirmRollback"
    />

    <ConfirmDialog
      v-model="publishCurrentVisible"
      title="发布当前配置"
      message="按当前已保存的入站、出站、出口组、代理链、分流规则与用户数据生成一个新版本，并立即发布到该节点。节点确认后生效。"
      confirm-text="发布"
      :is-danger="false"
      @confirm="publishCurrent"
    />

    <ConfirmDialog
      v-model="publishRevisionVisible"
      title="发布配置版本"
      :message="`确认将配置版本 #${publishRevisionId ?? ''} 发布到该节点？`"
      confirm-text="发布"
      :is-danger="false"
      @confirm="publishRevision"
    />

    <AppDialog
      v-model="diffVisible"
      title="配置版本差异"
      description="默认与该节点的已确认运行版本比较。"
      max-width="3xl"
      @close="clearDiff"
    >
      <LoadingSkeleton v-if="diffLoading" />
      <ErrorState v-else-if="diffError" :message="diffError" @retry="loadDiff" />
      <ConfigDiffViewer v-else-if="diffResult" :diff="diffResult" />
      <p v-else class="text-sm text-ios-tertiary">该版本没有可比较的差异。</p>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { nodeAPI, type NodeView } from '../api';
import TCPProbesPanel from '../TCPProbesPanel.vue';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { proxyAPI, type Inbound } from '../../proxy/api';
import { deploymentAPI, type Revision, type RevisionDiff } from '../../system/deploymentApi';
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
  CYCLE_LIST,
  type NodeMeta
} from '../nodeMeta';
import { isOwner, formatTime } from '../../../app/auth';
import { hostMetrics, uptime } from '../metrics';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ConfigDiffViewer from '../../../components/ConfigDiffViewer.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import NodeHealthPanel from '../components/NodeHealthPanel.vue';
import NodeRestartPanel from '../components/NodeRestartPanel.vue';
import NodeCertificatePanel from '../components/NodeCertificatePanel.vue';

const route = useRoute();
const router = useRouter();
const routeNodeId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''));

const loadedNode = ref<NodeView | null>(null);
const node = computed<NodeView | null>(() => (loadedNode.value && loadedNode.value.id === routeNodeId.value ? loadedNode.value : null));
const error = ref('');
const revokeVisible = ref(false);

const nodeMeta = ref<NodeMeta>({});
const allGroups = ref<NodeGroup[]>([]);
const associatedInbounds = ref<Inbound[]>([]);
const recentRevisions = ref<Revision[]>([]);

// 6 Tabs Configuration
const tabs = [
  { key: 'overview', label: '概况' },
  { key: 'monitoring', label: '监控' },
  { key: 'testing', label: '线路检测' },
  { key: 'config', label: '程序与配置' },
  { key: 'tuning', label: '调优' },
  { key: 'settings', label: '设置' },
];

const visibleTabs = computed(() => tabs.filter(t => isOwner.value || t.key !== 'settings'));

function normalize(tab: unknown) {
  return typeof tab === 'string' && visibleTabs.value.some(t => t.key === tab) ? tab : 'overview';
}

const activeTab = ref<string>(normalize(route.query.tab));

watch(() => route.query.tab, (val) => {
  activeTab.value = normalize(val);
});

function selectTab(key: string) {
  activeTab.value = key;
  void router.replace({ query: { ...route.query, tab: key } });
}

// Associated Groups
const associatedGroups = computed(() => {
  if (!node.value) return [];
  return allGroups.value.filter(g => g.nodeIds?.includes(node.value!.id));
});

function purposeText(purpose?: string): string {
  const map: Record<string, string> = {
    subscription: '订阅接入',
    relay: '中转中继',
    egress: '出口落地',
    mixed: '混合用途'
  };
  return map[purpose || ''] || purpose || '默认';
}

// Clock & Geo
const clockNow = ref(Date.now());
let clockTimer: ReturnType<typeof setInterval> | undefined;
const geoTick = ref(0);
let unsubGeo: (() => void) | undefined;

onMounted(() => {
  unsubGeo = onGeoResolved(() => {
    geoTick.value++;
  });
});

onUnmounted(() => {
  if (unsubGeo) unsubGeo();
  if (clockTimer) clearInterval(clockTimer);
});

const countryInfo = computed(() => {
  void geoTick.value;
  if (!node.value) return { code: '', name: '未知地区', flag: '🌐', source: 'fallback' as const };
  return resolveCountryInfo(nodeMeta.value.region, node.value.publicIp, node.value.name, node.value.hostname);
});

const renewPriceDisplay = computed(() => {
  return formatRenewPrice(nodeMeta.value);
});

function isAgentOnline(n: NodeView): boolean {
  if (!n.lastHeartbeat || !n.enabled || n.deletedAt) return false;
  const hbTime = Date.parse(n.lastHeartbeat);
  if (isNaN(hbTime)) return false;
  return clockNow.value - hbTime < 45000 && ['online', 'degraded', 'applying', 'rollback'].includes(n.status);
}

function agentStatusText(n: NodeView): string {
  if (n.deletedAt) return '已删除';
  if (!n.enabled) return '已停用';
  if (n.status === 'pending') return '等待连接';
  if (isAgentOnline(n) && n.status === 'degraded') return '在线（核心异常）';
  if (n.status === 'applying') return '发布中';
  if (n.status === 'rollback') return '回滚中';
  if (isAgentOnline(n) && n.status === 'online') return '在线';
  if (n.status === 'error') return '异常';
  return '离线';
}

function agentBadgeClass(n: NodeView): string {
  if (n.deletedAt || !n.enabled) return 'bg-neutral-100 border-neutral-200 text-neutral-600';
  if (n.status === 'degraded') return 'bg-amber-50 border-amber-200 text-amber-700';
  if (n.status === 'applying' || n.status === 'rollback') return 'bg-blue-50 border-blue-200 text-ios-blue';
  if (n.status === 'error') return 'bg-rose-50 border-rose-200 text-rose-700';
  return isAgentOnline(n) ? 'bg-emerald-50 border-emerald-200 text-emerald-700' : 'bg-neutral-100 border-neutral-200 text-neutral-600';
}

function agentDotClass(n: NodeView): string {
  if (n.deletedAt || !n.enabled) return 'bg-neutral-400';
  if (n.status === 'degraded') return 'bg-amber-500';
  if (n.status === 'applying' || n.status === 'rollback') return 'bg-ios-blue';
  if (n.status === 'error') return 'bg-rose-500';
  return isAgentOnline(n) ? 'bg-emerald-500' : 'bg-neutral-400';
}

function coreHealthText(n: NodeView): string {
  if (!isAgentOnline(n)) return '—';
  if (n.heartbeat?.core_healthy === true) return '健康';
  if (n.heartbeat?.core_healthy === false) {
    return n.heartbeat.core_error === 'accounting_interrupted' ? '记账中断' : '核心未通过健康检查';
  }
  return '未知';
}

function coreHealthBadgeClass(n: NodeView): string {
  if (!isAgentOnline(n)) return 'bg-neutral-100 border-neutral-200 text-neutral-600';
  if (n.heartbeat?.core_healthy === true) return 'bg-emerald-50 border-emerald-200 text-emerald-700';
  if (n.heartbeat?.core_healthy === false) return 'bg-rose-50 border-rose-200 text-rose-700 font-semibold';
  return 'bg-neutral-100 border-neutral-200 text-neutral-600';
}

function accessUsableText(n: NodeView): string {
  if (n.deletedAt) return '已注销';
  if (n.maintenance) return '维护中 (暂不分发)';
  if (!n.enabled) return '已停用';
  if (!n.allowSubscription) return '仅私有直连';
  return isAgentOnline(n) ? '正常服务' : '离线中';
}

function accessUsableBadgeClass(n: NodeView): string {
  if (n.maintenance) return 'bg-amber-50 border-amber-200 text-amber-700';
  if (!n.enabled || n.deletedAt) return 'bg-neutral-100 border-neutral-200 text-neutral-600';
  return isAgentOnline(n) ? 'bg-emerald-50 border-emerald-200 text-emerald-700' : 'bg-neutral-100 border-neutral-200 text-neutral-600';
}

function formatResource(val?: number): string {
  return typeof val === 'number' ? `${val.toFixed(1)}%` : '—';
}

function progressColor(val?: number): string {
  if (typeof val !== 'number') return 'bg-neutral-300';
  if (val >= 90) return 'bg-rose-500';
  if (val >= 75) return 'bg-amber-500';
  return 'bg-emerald-500';
}

const currentHost = computed(() => node.value ? hostMetrics(node.value, clockNow.value) : {});
function calculateUptimeText(): string {
  const started = currentHost.value.core_started_at?.seconds;
  return `${uptime(currentHost.value.host_uptime_seconds)} / ${uptime(started === undefined ? undefined : clockNow.value / 1000 - started)}`;
}

const remainingDaysInfo = computed(() => {
  return calculateRemainingDays(nodeMeta.value.renewDate);
});

function connectionCount() {
  const n = node.value;
  const h = n?.heartbeat;
  const observed = (h?.active_connections_observed_at?.seconds ?? 0) * 1000;
  return n?.enabled && n.status === 'online' && h?.core_healthy === true && h.active_connections_measured === true && h.active_connections_scope === 'proxy_user_sessions_v1' && clockNow.value - observed >= -5000 && clockNow.value - observed < 45000 && clockNow.value - Date.parse(n.lastHeartbeat) < 45000 ? (h.active_connections ?? 0) : '尚未测量或已过期';
}

function spoolFresh() {
  const n = node.value;
  const sample = n?.heartbeat.spool_status;
  const observed = (sample?.observed_at?.seconds ?? 0) * 1000;
  return !!(n?.enabled && ['online', 'degraded'].includes(n.status) && sample && clockNow.value - observed >= -5000 && clockNow.value - observed < 45000 && clockNow.value - Date.parse(n.lastHeartbeat) < 45000);
}

function spoolLabel() {
  if (!spoolFresh()) return '尚未上报或样本已过期';
  const s = node.value?.heartbeat.spool_status;
  if (s?.status === 'capacity_exceeded') return '已达到缓存容量上限，先补传已缓存记录并核对容量文件。';
  if (s?.status === 'near_capacity') return '接近缓存容量（达到85%），请核对补传进度与磁盘状态。';
  return { ok: '断线缓存队列与存储空间正常', unavailable: '缓存容量或队首记录无法校验' }[s?.status ?? 'unavailable'];
}

function cacheBytes(value: number | undefined) {
  const size = value ?? 0;
  return size < 1024 * 1024 ? `${(size / 1024).toFixed(1)} KiB` : `${(size / 1024 / 1024).toFixed(1)} MiB`;
}

// ----------------- Line Testing -----------------
const testingHealth = ref(false);

async function triggerHealthCheck() {
  if (!node.value || testingHealth.value) return;
  testingHealth.value = true;
  try {
    await nodeAPI.requestHealth(node.value.id, node.value.heartbeat?.current_revision ?? 0);
    showToast('健康探测任务已下发，等待服务器回执');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    testingHealth.value = false;
  }
}

// ----------------- Rollback -----------------
const rollingBack = ref(false);
const rollbackVisible = ref(false);
const rollbackRevision = ref<number | null>(null);

function openRollback(revision: number) {
  rollbackRevision.value = revision;
  rollbackVisible.value = true;
}

async function confirmRollback() {
  if (!node.value || rollbackRevision.value === null || rollingBack.value) return;
  const target = rollbackRevision.value;
  rollingBack.value = true;
  try {
    await deploymentAPI.rollbackNode(node.value.id, target);
    showToast(`回退到版本 #${target} 的请求已提交`);
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    rollingBack.value = false;
  }
}

// ----------------- Publishing -----------------
const publishingCurrent = ref(false);
const publishCurrentVisible = ref(false);
const publishingRevision = ref(false);
const publishRevisionVisible = ref(false);
const publishRevisionId = ref<number | null>(null);

// Drafts and the current configuration are published through the same
// one-step endpoint the proxy pages use, so a refused deploy still leaves the
// revision in the history below.
async function publishCurrent() {
  if (!node.value || publishingCurrent.value) return;
  publishingCurrent.value = true;
  try {
    const result = await deploymentAPI.publishNode(node.value.id, '手动发布当前配置');
    showToast(`已发布配置 #${result.revision}，节点确认后生效`);
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    publishingCurrent.value = false;
  }
}

function confirmPublishRevision(revision: number) {
  publishRevisionId.value = revision;
  publishRevisionVisible.value = true;
}

async function publishRevision() {
  if (publishRevisionId.value === null || publishingRevision.value) return;
  const target = publishRevisionId.value;
  publishingRevision.value = true;
  try {
    await deploymentAPI.deployRevision(target);
    showToast(`版本 #${target} 已提交发布，等待节点确认`);
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    publishingRevision.value = false;
  }
}

function revisionStatusLabel(status: string) {
  return ({ draft: '草稿', pending: '待节点确认', success: '已生效', failed: '失败', rolled_back: '已回滚' } as Record<string, string>)[status] || status;
}

// ----------------- Revision Diff -----------------
const diffVisible = ref(false);
const diffLoading = ref(false);
const diffError = ref('');
const diffResult = ref<RevisionDiff | null>(null);
const diffTarget = ref<Revision | null>(null);
let diffRequest = 0;

function openDiff(revision: Revision) {
  clearDiff();
  diffTarget.value = revision;
  diffVisible.value = true;
  void loadDiff();
}

function clearDiff() {
  diffRequest++;
  diffTarget.value = null;
  diffResult.value = null;
  diffError.value = '';
  diffLoading.value = false;
}

async function loadDiff() {
  if (!diffTarget.value) return;
  const serial = ++diffRequest;
  const revision = diffTarget.value.id;
  diffResult.value = null;
  diffError.value = '';
  diffLoading.value = true;
  try {
    const result = await deploymentAPI.diff(revision);
    if (serial === diffRequest && diffVisible.value) diffResult.value = result;
  } catch (e) {
    if (serial === diffRequest && diffVisible.value) diffError.value = (e as Error).message;
  } finally {
    if (serial === diffRequest) diffLoading.value = false;
  }
}

// ----------------- Settings Tab Logic -----------------
const saving = ref(false);
const networkErrorText = ref('');
const settingsForm = reactive({
  name: '',
  publicIp: '',
  region: '',
  remark: '',
  renewDate: '',
  renewCurrency: 'CNY',
  renewPrice: '',
  renewCycle: '/年',
  renewNoticeDays: 7,
  enabled: true,
  allowSubscription: true,
  maintenance: false,
  selectedGroupIds: [] as string[],
  outboundSourceIp: '',
  outboundInterface: '',
  hostSourceIpv4: '',
  hostSourceIpv6: ''
});

function syncSettingsForm() {
  if (!node.value) return;
  nodeMeta.value = getNodeMeta(node.value.id);
  settingsForm.name = node.value.name;
  settingsForm.publicIp = node.value.publicIp || '';
  settingsForm.region = nodeMeta.value.region || '';
  settingsForm.remark = nodeMeta.value.remark || '';
  settingsForm.renewDate = nodeMeta.value.renewDate || '';
  const parsed = parseRenewPrice(nodeMeta.value.renewPrice, nodeMeta.value.renewCurrency, nodeMeta.value.renewCycle);
  settingsForm.renewCurrency = parsed.currency;
  settingsForm.renewPrice = parsed.price;
  settingsForm.renewCycle = parsed.cycle;
  settingsForm.renewNoticeDays = nodeMeta.value.renewNoticeDays ?? 7;
  settingsForm.enabled = node.value.enabled;
  settingsForm.allowSubscription = node.value.allowSubscription;
  settingsForm.maintenance = node.value.maintenance;
  settingsForm.outboundSourceIp = node.value.outboundSourceIp || '';
  settingsForm.outboundInterface = node.value.outboundInterface || '';
  settingsForm.hostSourceIpv4 = node.value.hostSourceIpv4 || '';
  settingsForm.hostSourceIpv6 = node.value.hostSourceIpv6 || '';
  settingsForm.selectedGroupIds = allGroups.value
    .filter(g => g.nodeIds?.includes(node.value!.id))
    .map(g => g.id);
}

const interfacePattern = /^[A-Za-z0-9_.:-]{1,15}$/;
function isIPv4(value: string) {
  const parts = value.split('.');
  return parts.length === 4 && parts.every(part => /^\d{1,3}$/.test(part) && Number(part) <= 255);
}
function isIPv6(value: string) {
  return value.includes(':') && /^[0-9a-fA-F:]+$/.test(value) && !value.includes(':::');
}

function validateNetwork() {
  if (settingsForm.outboundSourceIp && !isIPv4(settingsForm.outboundSourceIp) && !isIPv6(settingsForm.outboundSourceIp)) {
    return '核心出站源 IP 需要 IPv4 或 IPv6 地址';
  }
  if (settingsForm.outboundInterface && !interfacePattern.test(settingsForm.outboundInterface)) {
    return '核心出站网卡名只能是 1～15 个字母、数字或 _ . : -';
  }
  if (settingsForm.hostSourceIpv4 && !isIPv4(settingsForm.hostSourceIpv4)) {
    return '服务器强制出站 IPv4 需要 IPv4 地址';
  }
  if (settingsForm.hostSourceIpv6 && !isIPv6(settingsForm.hostSourceIpv6)) {
    return '服务器强制出站 IPv6 需要 IPv6 地址';
  }
  return '';
}

async function saveAllSettings() {
  if (!node.value || saving.value) return;
  const invalid = validateNetwork();
  if (invalid) {
    networkErrorText.value = invalid;
    showToast(invalid, 'error');
    return;
  }
  networkErrorText.value = '';
  saving.value = true;
  try {
    // 1. Save Node Base Settings & Outbound Network
    await nodeAPI.save({
      id: node.value.id,
      name: settingsForm.name,
      publicIp: settingsForm.publicIp,
      enabled: settingsForm.enabled,
      allowSubscription: settingsForm.allowSubscription,
      maintenance: settingsForm.maintenance,
      outboundSourceIp: settingsForm.outboundSourceIp,
      outboundInterface: settingsForm.outboundInterface,
      hostSourceIpv4: settingsForm.hostSourceIpv4,
      hostSourceIpv6: settingsForm.hostSourceIpv6
    });

    // 2. Save NodeMeta (region, remark, renewDate, renewCurrency, renewPrice, renewCycle, renewNoticeDays)
    await saveNodeMeta(node.value.id, {
      region: settingsForm.region.trim(),
      remark: settingsForm.remark.trim(),
      renewDate: settingsForm.renewDate,
      renewCurrency: settingsForm.renewCurrency,
      renewPrice: settingsForm.renewPrice.trim(),
      renewCycle: settingsForm.renewCycle,
      renewNoticeDays: settingsForm.renewNoticeDays
    });
    nodeMeta.value = getNodeMeta(node.value.id);

    // 3. Sync Group Memberships
    const nodeId = node.value.id;
    for (const group of allGroups.value) {
      const isSelected = settingsForm.selectedGroupIds.includes(group.id);
      const isCurrentlyIn = group.nodeIds?.includes(nodeId) || false;
      if (isSelected !== isCurrentlyIn) {
        let updatedIds = [...(group.nodeIds || [])];
        if (isSelected) {
          if (!updatedIds.includes(nodeId)) updatedIds.push(nodeId);
        } else {
          updatedIds = updatedIds.filter(id => id !== nodeId);
        }
        await groupAPI.save(group.id, {
          name: group.name,
          purpose: group.purpose,
          enabled: group.enabled,
          subscriptionVisible: group.subscriptionVisible,
          nodeIds: updatedIds,
          remark: group.remark
        });
      }
    }

    showToast('服务器设置与分组分配已保存');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

async function revoke() {
  const target = node.value?.id ?? '';
  if (!target) return;
  try {
    await nodeAPI.revoke(target);
    showToast('服务器证书已吊销');
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// ----------------- Data Loading -----------------
let generation = 0;

async function load() {
  const id = routeNodeId.value;
  const current = ++generation;
  if (!id) {
    loadedNode.value = null;
    error.value = '';
    return;
  }
  try {
    const [nodeRes, groupsRes, inboundsRes, revisionsRes] = await Promise.all([
      nodeAPI.get(id),
      groupAPI.list(1, 100).catch(() => ({ items: [], total: 0 })),
      isOwner.value ? proxyAPI.inbounds(id, 1, 50).catch(() => ({ items: [], total: 0 })) : Promise.resolve({ items: [], total: 0 }),
      isOwner.value ? deploymentAPI.nodeRevisions(id).catch(() => ({ items: [], total: 0 })) : Promise.resolve({ items: [], total: 0 })
    ]);

    if (current !== generation || id !== routeNodeId.value) return;

    await loadNodeMeta(id, isOwner.value);
    if (current !== generation || id !== routeNodeId.value) return;
    loadedNode.value = nodeRes;
    allGroups.value = groupsRes.items || [];
    associatedInbounds.value = inboundsRes.items || [];
    recentRevisions.value = revisionsRes.items || [];
    syncSettingsForm();
    error.value = '';
  } catch (e) {
    if (current !== generation || id !== routeNodeId.value) return;
    loadedNode.value = null;
    error.value = (e as Error).message;
  }
}

watch(() => route.params.id, () => {
  generation++;
  loadedNode.value = null;
  error.value = '';
  void load();
});

onMounted(() => {
  clockTimer = setInterval(() => { clockNow.value = Date.now(); }, 5000);
  load();
  window.addEventListener('proxy-node-status', load);
});

onUnmounted(() => {
  generation++;
  if (clockTimer) clearInterval(clockTimer);
  window.removeEventListener('proxy-node-status', load);
});
</script>
