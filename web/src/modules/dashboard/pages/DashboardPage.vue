<template>
  <div class="space-y-6">
    <!-- Standard iOS Page Header -->
    <PageHeader
      title="系统总览"
      description="实时监控节点网络状态、处理系统告警、追踪今日流量与用户额度。"
    >
      <template #actions>
        <!-- Auto-refresh status indicator -->
        <div class="hidden sm:inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-black/[0.04] text-[11px] text-ios-tertiary font-medium">
          <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
          <span>{{ dateRefreshText }}</span>
        </div>



        <button
          :disabled="loading"
          @click="loadAll"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <svg :class="['w-3.5 h-3.5 text-ios-secondary', loading ? 'animate-spin' : '']" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          {{ loading ? '刷新中…' : '刷新' }}
        </button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="loadAll" />
    <LoadingSkeleton v-else-if="!dashboard" />

    <template v-else>
      <!-- ======================================================== -->
      <!-- 模式一：ADMIN (后台总览)                                   -->
      <!-- 严格对齐各模块高度、外缘边界与排版基线                         -->
      <!-- ======================================================== -->
        <div class="space-y-6">
          <!-- 1. 运行概况：4 栏核心状态指标卡片 (高度与基线统一对齐) -->
          <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 items-stretch">
            <!-- 节点状态 -->
            <div class="panel-card p-5 bg-white flex flex-col justify-between h-full">
              <div>
                <div class="flex items-center justify-between">
                  <span class="text-xs font-semibold text-ios-tertiary">节点状态</span>
                  <div class="w-8 h-8 rounded-xl bg-blue-50 text-ios-blue flex items-center justify-center shrink-0">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 12h14M5 12a2 2 0 01-2-2V6a2 2 0 012-2h14a2 2 0 012 2v4a2 2 0 01-2 2M5 12a2 2 0 00-2 2v4a2 2 0 002 2h14a2 2 0 002-2v-4a2 2 0 00-2-2m-2-4h.01M17 16h.01" />
                    </svg>
                  </div>
                </div>

                <div class="mt-3 flex items-baseline gap-1.5">
                  <span class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ adminOnlineNodesCount }}</span>
                  <span class="text-xs text-ios-tertiary font-medium">/ {{ allNodes.length }} 在线</span>
                </div>
              </div>

              <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-xs">
                <span
                  :class="[
                    'inline-flex items-center gap-1.5 font-medium',
                    offlineNodesCount === 0 && allNodes.length > 0 ? 'text-emerald-600' : 'text-ios-red'
                  ]"
                >
                  <span :class="['w-1.5 h-1.5 rounded-full shrink-0', offlineNodesCount === 0 && allNodes.length > 0 ? 'bg-emerald-500' : 'bg-ios-red']"></span>
                  {{ offlineNodesCount === 0 && allNodes.length > 0 ? '全部就绪' : `${offlineNodesCount} 台离线` }}
                </span>
                <span class="text-ios-tertiary tabular-nums text-[11px] font-medium">{{ allNodes.length > 0 ? Math.round((adminOnlineNodesCount / allNodes.length) * 100) : 0 }}%</span>
              </div>
            </div>

            <!-- 代理核心 -->
            <div class="panel-card p-5 bg-white flex flex-col justify-between h-full">
              <div>
                <div class="flex items-center justify-between">
                  <span class="text-xs font-semibold text-ios-tertiary">核心运行</span>
                  <div class="w-8 h-8 rounded-xl bg-emerald-50 text-emerald-600 flex items-center justify-center shrink-0">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M9 3v2m6-2v2M9 19v2m6-2v2M5 9H3m2 6H3m18-6h-2m2 6h-2M7 19h10a2 2 0 002-2V7a2 2 0 00-2-2H7a2 2 0 00-2 2v10a2 2 0 002 2zM9 9h6v6H9V9z" />
                    </svg>
                  </div>
                </div>

                <div class="mt-3 flex items-baseline gap-1.5">
                  <span class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ healthyNodesCount }}</span>
                  <span class="text-xs text-ios-tertiary font-medium">/ {{ allNodes.length }} 正常</span>
                </div>
              </div>

              <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-xs">
                <span
                  :class="[
                    'inline-flex items-center gap-1.5 font-medium',
                    healthyNodesCount === allNodes.length && allNodes.length > 0 ? 'text-emerald-600' : 'text-amber-600'
                  ]"
                >
                  <span :class="['w-1.5 h-1.5 rounded-full shrink-0', healthyNodesCount === allNodes.length && allNodes.length > 0 ? 'bg-emerald-500' : 'bg-amber-500']"></span>
                  {{ healthyNodesCount === allNodes.length && allNodes.length > 0 ? '状态健康' : `${allNodes.length - healthyNodesCount} 核心待检` }}
                </span>
                <span class="text-ios-tertiary tabular-nums text-[11px] font-medium">{{ allNodes.length > 0 ? Math.round((healthyNodesCount / allNodes.length) * 100) : 0 }}%</span>
              </div>
            </div>

            <!-- 有效用户 -->
            <div class="panel-card p-5 bg-white flex flex-col justify-between h-full">
              <div>
                <div class="flex items-center justify-between">
                  <span class="text-xs font-semibold text-ios-tertiary">有效用户</span>
                  <div class="w-8 h-8 rounded-xl bg-purple-50 text-purple-600 flex items-center justify-center shrink-0">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4.354a4 4 0 110 5.292M15 21H3v-1a6 6 0 0112 0v1zm0 0h6v-1a6 6 0 00-9-5.197M13 7a4 4 0 11-8 0 4 4 0 018 0z" />
                    </svg>
                  </div>
                </div>

                <div class="mt-3 flex items-baseline gap-1.5">
                  <span class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">{{ activeUsersCount }}</span>
                  <span class="text-xs text-ios-tertiary font-medium">/ {{ allUsers.length }} 活跃</span>
                </div>
              </div>

              <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-xs">
                <span
                  :class="[
                    'inline-flex items-center gap-1.5 font-medium',
                    warningUsersCount > 0 ? 'text-amber-600' : 'text-emerald-600'
                  ]"
                >
                  <span :class="['w-1.5 h-1.5 rounded-full shrink-0', warningUsersCount > 0 ? 'bg-amber-500' : 'bg-emerald-500']"></span>
                  {{ warningUsersCount > 0 ? `${warningUsersCount} 位待关注` : '账号正常' }}
                </span>
                <span class="text-ios-tertiary tabular-nums text-[11px] font-medium">{{ allUsers.length > 0 ? Math.round((activeUsersCount / allUsers.length) * 100) : 0 }}%</span>
              </div>
            </div>

            <!-- 今日流量 -->
            <div class="panel-card p-5 bg-white flex flex-col justify-between h-full">
              <div>
                <div class="flex items-center justify-between">
                  <span class="text-xs font-semibold text-ios-tertiary">今日流量</span>
                  <div class="w-8 h-8 rounded-xl bg-orange-50 text-orange-600 flex items-center justify-center shrink-0">
                    <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 7h8m0 0v8m0-8l-8 8-4-4-6 6" />
                    </svg>
                  </div>
                </div>

                <div class="mt-3 flex items-baseline gap-1.5">
                  <span class="text-3xl font-bold text-ios-label tabular-nums tracking-tight">
                    <TrafficValue :bytes="todayTrafficTotal" />
                  </span>
                </div>
              </div>

              <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-[11px] text-ios-secondary">
                <span>↑ <TrafficValue :bytes="todayUpload" /></span>
                <span>↓ <TrafficValue :bytes="todayDownload" /></span>
              </div>
            </div>
          </div>

          <!-- 2. 中部核心区：左侧待处理+节点列表 (7列) | 右侧节点流量排行 (5列) -->
          <!-- 使用 items-stretch 让左右两栏底部完全水平对齐 -->
          <div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-stretch">
            <!-- 左侧主栏 (7列): 待处理问题 + 节点列表 -->
            <div class="lg:col-span-7 flex flex-col gap-6">
              <!-- 待处理问题 -->
              <section class="panel-card p-5 sm:p-6 bg-white space-y-4">
                <div class="flex items-center justify-between">
                  <div>
                    <div class="flex items-center gap-2">
                      <h2 class="text-base font-bold text-ios-label tracking-tight">待处理问题</h2>
                      <span
                        v-if="pendingIssues.length > 0"
                        class="text-xs px-2 py-0.5 rounded-full font-semibold bg-rose-50 text-ios-red border border-rose-200/80"
                      >
                        {{ pendingIssues.length }} 项需关注
                      </span>
                      <span
                        v-else
                        class="text-xs px-2 py-0.5 rounded-full font-medium bg-emerald-50 text-emerald-700 border border-emerald-200/80"
                      >
                        系统正常
                      </span>
                    </div>
                    <p class="text-xs text-ios-tertiary mt-0.5">异常告警与需人工介入事件</p>
                  </div>
                </div>

                <!-- 告警列表 -->
                <div v-if="pendingIssues.length > 0" class="space-y-3">
                  <div
                    v-for="(issue, idx) in pendingIssues"
                    :key="idx"
                    class="p-4 rounded-2xl border transition-all flex items-start justify-between gap-3"
                    :class="issue.level === 'danger' ? 'bg-rose-50/50 border-rose-200/70' : 'bg-amber-50/50 border-amber-200/70'"
                  >
                    <div class="flex items-start gap-3 min-w-0">
                      <div
                        class="w-8 h-8 rounded-xl flex items-center justify-center shrink-0 mt-0.5 text-xs font-bold"
                        :class="issue.level === 'danger' ? 'bg-rose-100 text-ios-red' : 'bg-amber-100 text-amber-700'"
                      >
                        <svg v-if="issue.level === 'danger'" class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                        </svg>
                        <svg v-else class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                        </svg>
                      </div>

                      <div class="min-w-0 space-y-1">
                        <div class="font-semibold text-sm text-ios-label truncate">{{ issue.title }}</div>
                        <p class="text-xs text-ios-secondary leading-relaxed">{{ issue.desc }}</p>
                        <div v-if="issue.tag" class="pt-0.5">
                          <span class="inline-block px-2 py-0.5 rounded-md text-[11px] font-medium bg-black/[0.04] text-ios-secondary border border-black/[0.04]">
                            {{ issue.tag }}
                          </span>
                        </div>
                      </div>
                    </div>

                    <router-link
                      :to="issue.link"
                      class="ios-btn-secondary text-xs h-7 px-2.5 shrink-0 self-center"
                    >
                      去处理 &rarr;
                    </router-link>
                  </div>
                </div>

                <!-- 正常无异常状态 -->
                <div v-else class="py-5 flex flex-col items-center justify-center text-center space-y-1.5">
                  <div class="w-8 h-8 rounded-full bg-emerald-50 text-emerald-600 flex items-center justify-center font-bold text-xs">
                    ✓
                  </div>
                  <p class="text-xs font-semibold text-ios-label">运行健康，暂无待处理告警</p>
                  <p class="text-[11px] text-ios-tertiary">所有节点连接活跃、代理核心运行正常。</p>
                </div>
              </section>

              <!-- 节点列表 (在线情况) -->
              <section class="panel-card p-5 sm:p-6 bg-white flex-1 flex flex-col justify-between space-y-4">
                <div class="flex items-center justify-between">
                  <div>
                    <h2 class="text-base font-bold text-ios-label tracking-tight">节点列表</h2>
                    <p class="text-xs text-ios-tertiary mt-0.5">实时心跳与网络可用性监控</p>
                  </div>
                  <router-link to="/nodes" class="text-xs font-semibold text-ios-blue hover:underline">
                    查看全部 &rarr;
                  </router-link>
                </div>

                <EmptyState
                  v-if="allNodes.length === 0"
                  title="暂无节点"
                  description="尚未连接节点服务器，请前往节点管理配置并注册 Agent。"
                />

                <!-- 2 列自适应网格卡片 (对称整洁，不留单卡空白) -->
                <div v-else class="grid grid-cols-1 sm:grid-cols-2 gap-3 flex-1 items-start">
                  <div
                    v-for="node in allNodes"
                    :key="node.id"
                    class="p-4 rounded-2xl border border-black/[0.06] bg-white hover:border-ios-blue/30 transition-all flex flex-col justify-between space-y-3 shadow-2xs group"
                  >
                    <div class="flex items-center justify-between gap-2">
                      <router-link
                        :to="`/nodes/${node.id}`"
                        class="font-semibold text-sm text-ios-label group-hover:text-ios-blue truncate block"
                        :title="node.name"
                      >
                        {{ node.name }}
                      </router-link>

                      <!-- Status Badge -->
                      <span
                        :class="[
                          'text-[10px] font-semibold px-2 py-0.5 rounded-full shrink-0 border inline-flex items-center gap-1',
                          isNodeOnline(node)
                            ? 'bg-emerald-50 text-emerald-600 border-emerald-200/80'
                            : 'bg-rose-50 text-ios-red border-rose-200/80'
                        ]"
                      >
                        <span :class="['w-1.5 h-1.5 rounded-full shrink-0', isNodeOnline(node) ? 'bg-emerald-500' : 'bg-ios-red']"></span>
                        {{ isNodeOnline(node) ? '在线' : '离线' }}
                      </span>
                    </div>

                    <div class="pt-2 border-t border-black/[0.04] flex items-center justify-between text-xs text-ios-secondary">
                      <span class="tabular-nums text-[11px] text-ios-tertiary truncate">
                        {{ node.publicIp || node.hostname || '未分配IP' }}
                      </span>
                      <span class="tabular-nums text-[11px] text-ios-tertiary shrink-0">
                        {{ node.lastHeartbeat ? formatTime(node.lastHeartbeat).slice(11, 19) : '未同步' }}
                      </span>
                    </div>
                  </div>
                </div>
              </section>
            </div>

            <!-- 右侧副栏 (5列): 节点流量 (优化后：层次丰富、结构饱满，消除空旷空白) -->
            <div class="lg:col-span-5 flex flex-col">
              <section class="panel-card p-5 sm:p-6 bg-white h-full flex flex-col justify-between space-y-4">
                <!-- 头部：标题与时间维度切换 (累计 / 今日) -->
                <div class="flex items-center justify-between">
                  <div>
                    <h2 class="text-base font-bold text-ios-label tracking-tight">节点流量</h2>
                    <p class="text-xs text-ios-tertiary mt-0.5">节点吞吐与用量分布</p>
                  </div>
                  <!-- 维度切换药丸按钮 -->
                  <div class="inline-flex rounded-lg bg-black/[0.05] p-0.5 text-[11px] font-medium select-none">
                    <button
                      type="button"
                      @click="nodeTrafficPeriod = 'total'"
                      :class="[
                        'px-2.5 py-0.5 rounded-md transition-all',
                        nodeTrafficPeriod === 'total'
                          ? 'bg-white text-ios-blue shadow-2xs font-semibold'
                          : 'text-ios-tertiary hover:text-ios-label'
                      ]"
                    >
                      累计
                    </button>
                    <button
                      type="button"
                      @click="nodeTrafficPeriod = 'today'"
                      :class="[
                        'px-2.5 py-0.5 rounded-md transition-all',
                        nodeTrafficPeriod === 'today'
                          ? 'bg-white text-ios-blue shadow-2xs font-semibold'
                          : 'text-ios-tertiary hover:text-ios-label'
                      ]"
                    >
                      今日
                    </button>
                  </div>
                </div>

                <!-- 吞吐总量与上/下行分布盒子 -->
                <div class="p-3.5 sm:p-4 rounded-2xl bg-[#F8F8FA] border border-black/[0.04] space-y-3">
                  <div class="flex items-center justify-between">
                    <div>
                      <div class="text-[11px] text-ios-tertiary font-medium">
                        {{ nodeTrafficPeriod === 'today' ? '今日吞吐总量' : '累计吞吐总量' }}
                      </div>
                      <div class="text-lg sm:text-xl font-bold text-ios-label tabular-nums tracking-tight mt-0.5">
                        <TrafficValue :bytes="displayTotalBytes" />
                      </div>
                    </div>
                    <div class="flex items-center gap-3 text-xs">
                      <div class="text-right">
                        <span class="text-[10px] text-ios-tertiary block font-medium">↑ 上行</span>
                        <span class="font-semibold text-ios-label tabular-nums"><TrafficValue :bytes="displayUploadBytes" /></span>
                      </div>
                      <div class="w-px h-6 bg-black/[0.08]"></div>
                      <div class="text-right">
                        <span class="text-[10px] text-ios-tertiary block font-medium">↓ 下行</span>
                        <span class="font-semibold text-ios-label tabular-nums"><TrafficValue :bytes="displayDownloadBytes" /></span>
                      </div>
                    </div>
                  </div>

                  <!-- 比例指示条 -->
                  <div class="space-y-1">
                    <div class="w-full bg-black/[0.05] h-2 rounded-full overflow-hidden flex">
                      <template v-if="displayTotalBytes > 0">
                        <div
                          class="bg-[#0071E3] h-full transition-all duration-500"
                          :style="{ width: `${uploadRatioPercent}%` }"
                          :title="`上行: ${uploadRatioPercent}%`"
                        ></div>
                        <div
                          class="bg-[#34C759] h-full transition-all duration-500"
                          :style="{ width: `${downloadRatioPercent}%` }"
                          :title="`下行: ${downloadRatioPercent}%`"
                        ></div>
                      </template>
                      <div v-else class="w-full h-full bg-black/[0.04]"></div>
                    </div>
                    <div class="flex justify-between text-[10px] text-ios-tertiary">
                      <span class="flex items-center gap-1.5">
                        <span class="w-1.5 h-1.5 rounded-full" :class="displayTotalBytes > 0 ? 'bg-[#0071E3]' : 'bg-black/20'"></span>
                        上行 {{ displayTotalBytes > 0 ? `${uploadRatioPercent}%` : '0%' }}
                      </span>
                      <span class="flex items-center gap-1.5">
                        <span class="w-1.5 h-1.5 rounded-full" :class="displayTotalBytes > 0 ? 'bg-[#34C759]' : 'bg-black/20'"></span>
                        下行 {{ displayTotalBytes > 0 ? `${downloadRatioPercent}%` : '0%' }}
                      </span>
                    </div>
                  </div>
                </div>

                <!-- 节点用量排行明细 (充实整个空间，消除空旷) -->
                <div class="flex-1 flex flex-col justify-between space-y-2.5">
                  <div class="flex items-center justify-between text-xs font-semibold text-ios-secondary px-0.5">
                    <span>各节点用量排行</span>
                    <span class="text-[11px] text-ios-tertiary font-normal">实时监控</span>
                  </div>

                  <EmptyState
                    v-if="allNodes.length === 0"
                    title="暂无节点"
                    description="尚未添加节点服务器，请前往节点管理配置。"
                  />

                  <div v-else class="space-y-2 flex-1 flex flex-col justify-around">
                    <div
                      v-for="(item, index) in sortedNodeTrafficList"
                      :key="item.node.id"
                      class="p-2.5 rounded-xl border border-black/[0.04] bg-[#FAFAFA]/70 hover:bg-[#FAFAFA] transition-all space-y-1.5"
                    >
                      <div class="flex items-center justify-between text-xs">
                        <div class="flex items-center gap-2 min-w-0">
                          <span
                            :class="[
                              'w-4 h-4 rounded text-[10px] font-bold flex items-center justify-center shrink-0',
                              index === 0 ? 'bg-amber-100 text-amber-800' :
                              index === 1 ? 'bg-slate-200 text-slate-700' :
                              index === 2 ? 'bg-amber-50 text-amber-900 border border-amber-200/50' :
                              'bg-black/[0.04] text-ios-tertiary'
                            ]"
                          >
                            {{ index + 1 }}
                          </span>

                          <span
                            :class="['w-1.5 h-1.5 rounded-full shrink-0', isNodeOnline(item.node) ? 'bg-emerald-500' : 'bg-ios-red']"
                            :title="isNodeOnline(item.node) ? '在线' : '离线'"
                          ></span>

                          <router-link
                            :to="`/nodes/${item.node.id}`"
                            class="font-medium text-ios-label hover:text-ios-blue truncate max-w-[130px] sm:max-w-[170px]"
                            :title="item.node.name"
                          >
                            {{ item.node.name }}
                          </router-link>

                          <span class="tabular-nums text-[10px] text-ios-tertiary truncate hidden sm:inline">
                            {{ item.node.publicIp || item.node.hostname || '' }}
                          </span>
                        </div>

                        <div class="flex items-center gap-2 shrink-0">
                          <span class="text-ios-label text-xs font-semibold tabular-nums">
                            <TrafficValue :bytes="item.traffic" />
                          </span>
                          <span class="tabular-nums text-ios-tertiary text-[10px] w-8 text-right font-medium">
                            {{ item.percent }}%
                          </span>
                        </div>
                      </div>

                      <div class="w-full bg-black/[0.04] h-1.5 rounded-full overflow-hidden">
                        <div
                          class="bg-gradient-to-r from-ios-blue to-[#5856D6] h-full rounded-full transition-all duration-500"
                          :style="{ width: `${item.traffic > 0 ? item.percent : 0}%` }"
                        ></div>
                      </div>
                    </div>
                  </div>

                  <!-- 底端辅助栏 -->
                  <div class="pt-2.5 border-t border-black/[0.04] flex items-center justify-between text-[11px] text-ios-tertiary">
                    <span class="flex items-center gap-1.5">
                      <span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
                      已接入 {{ allNodes.length }} 个节点
                    </span>
                    <router-link to="/nodes" class="hover:text-ios-blue transition-colors font-medium">
                      节点管理 &rarr;
                    </router-link>
                  </div>
                </div>
              </section>
            </div>
          </div>

          <!-- 3. 下部一排并列：用户用量与到期 (左) + 近期访问审计 (右) -->
          <!-- 使用 items-stretch 让两张卡片上下完全等高对齐 -->
          <div class="grid grid-cols-1 lg:grid-cols-2 gap-6 items-stretch">
            <!-- 用户用量与到期 -->
            <section
              :class="[
                'panel-card p-5 sm:p-6 bg-white h-full flex flex-col justify-between space-y-4',
                isOwner ? '' : 'lg:col-span-2'
              ]"
            >
              <div class="flex items-center justify-between">
                <div>
                  <h2 class="text-base font-bold text-ios-label tracking-tight">用户用量与到期</h2>
                  <p class="text-xs text-ios-tertiary mt-0.5">额度使用进度与续约提醒</p>
                </div>
                <router-link to="/users" class="text-xs font-semibold text-ios-blue hover:underline">
                  全部用户 &rarr;
                </router-link>
              </div>

              <div v-if="allUsers.length === 0" class="text-xs text-ios-tertiary py-8 text-center flex-1 flex items-center justify-center">
                暂无用户账户
              </div>

              <div v-else class="space-y-2.5 flex-1 flex flex-col justify-start">
                <div
                  v-for="u in topAttentionUsers"
                  :key="u.id"
                  class="p-3 rounded-2xl border border-black/[0.04] bg-[#FAFAFA] space-y-2 text-xs"
                >
                  <div class="flex items-center justify-between gap-2">
                    <div class="flex items-center gap-2 truncate">
                      <span class="text-[10px] font-semibold px-1.5 py-0.5 rounded-md bg-blue-50 text-ios-blue border border-blue-200/60 shrink-0">
                        账户
                      </span>
                      <span class="font-semibold text-ios-label truncate">
                        {{ u.displayName || u.username }}
                      </span>
                    </div>

                    <span
                      :class="[
                        'text-[10px] font-medium px-2 py-0.5 rounded-full shrink-0',
                        getUserExpiryBadgeClass(u)
                      ]"
                    >
                      {{ getUserExpiryLabel(u) }}
                    </span>
                  </div>

                  <!-- Quota progress bar -->
                  <div class="space-y-1">
                    <div class="flex items-center justify-between text-[11px] text-ios-tertiary">
                      <span>已用: <TrafficValue :bytes="u.usedTraffic" /></span>
                      <span>{{ u.trafficLimit > 0 ? `配额: ${formatGiB(u.trafficLimit)}` : '不限限额' }}</span>
                    </div>
                    <div v-if="u.trafficLimit > 0" class="w-full bg-black/[0.05] h-1.5 rounded-full overflow-hidden">
                      <div
                        class="h-full rounded-full transition-all"
                        :class="userUsagePercent(u) >= 90 ? 'bg-ios-red' : 'bg-ios-blue'"
                        :style="{ width: `${userUsagePercent(u)}%` }"
                      ></div>
                    </div>
                  </div>
                </div>
              </div>
            </section>

            <!-- 近期访问审计 (仅 Owner 可见，与用户用量严格等高对齐) -->
            <section v-if="isOwner" class="panel-card p-5 sm:p-6 bg-white h-full flex flex-col justify-between space-y-4">
              <div class="flex items-center justify-between">
                <div>
                  <h2 class="text-base font-bold text-ios-label tracking-tight">近期访问审计</h2>
                  <p class="text-xs text-ios-tertiary mt-0.5">最近转发请求与拦截事件</p>
                </div>
                <router-link to="/access" class="text-xs font-semibold text-ios-blue hover:underline">
                  完整审计 &rarr;
                </router-link>
              </div>

              <!-- 暂无日志时的优雅等高占位 -->
              <div
                v-if="!dashboard.recentAccess?.length && !dashboard.recentBlocked?.length"
                class="flex-1 min-h-[220px] rounded-2xl border border-dashed border-black/[0.08] flex flex-col items-center justify-center text-center p-6 space-y-2"
              >
                <div class="w-10 h-10 rounded-2xl bg-black/[0.03] text-ios-tertiary flex items-center justify-center">
                  <svg class="w-5 h-5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8" d="M9 12h6m-6 4h6m2 5H7a2 2 0 01-2-2V5a2 2 0 012-2h5.586a1 1 0 01.707.293l5.414 5.414a1 1 0 01.293.707V19a2 2 0 01-2 2z" />
                  </svg>
                </div>
                <p class="text-sm font-semibold text-ios-label">暂无访问日志</p>
                <p class="text-xs text-ios-tertiary max-w-xs leading-relaxed">
                  代理核心处理流量时将自动上报转发与拦截审计事件。
                </p>
              </div>

              <!-- 有日志时的紧凑列表 (flex-1 纵向撑满对齐) -->
              <div v-else class="space-y-2 divide-y divide-black/[0.04] flex-1">
                <div
                  v-for="(item, idx) in combinedRecentRecords"
                  :key="idx"
                  class="pt-2.5 first:pt-0 flex items-center justify-between gap-2 text-xs"
                >
                  <div class="min-w-0">
                    <div class="flex items-center gap-1.5">
                      <span
                        :class="[
                          'text-[9px] font-semibold px-1 py-0.2 rounded shrink-0',
                          item.allowed ? 'bg-emerald-50 text-emerald-700' : 'bg-rose-50 text-ios-red font-bold'
                        ]"
                      >
                        {{ item.allowed ? '通过' : '阻断' }}
                      </span>
                      <p class="tabular-nums text-ios-label truncate font-medium text-[11px]">
                        {{ item.domain || item.targetIp }}
                      </p>
                    </div>
                    <p class="text-[10px] text-ios-tertiary truncate mt-0.5">
                      {{ item.userName || '匿名' }} · {{ item.nodeName }}
                    </p>
                  </div>
                  <span class="text-[10px] tabular-nums text-ios-quaternary shrink-0">
                    {{ formatTime(item.time).slice(11, 19) }}
                  </span>
                </div>
              </div>
            </section>
          </div>
        </div>

    </template>


  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue';
import { dashboardAPI, type Dashboard, type Hour, type Recent } from '../api';
import { accessAPI } from '../../access/api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { unhealthyCoreLabel } from '../../nodes/coreSummary';
import { userAPI, type ProxyUser } from '../../users/api';
import { deploymentAPI, type Revision } from '../../system/deploymentApi';
import { authState, isOwner, formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { generateQRCodeSVG } from '../../../app/qrcode';
import PageHeader from '../../../components/PageHeader.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import EmptyState from '../../../components/EmptyState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ErrorState from '../../../components/ErrorState.vue';
import AppDialog from '../../../components/AppDialog.vue';

const dashboard = ref<Dashboard | null>(null);
const allNodes = ref<NodeView[]>([]);
const allUsers = ref<ProxyUser[]>([]);
const allRevisions = ref<Revision[]>([]);
const loading = ref(false);
const error = ref('');
let timer: ReturnType<typeof setInterval> | null = null;

// Dynamic Date & Auto-refresh String
const dateRefreshText = computed(() => {
  const now = new Date();
  const m = now.getMonth() + 1;
  const d = now.getDate();
  const weekDays = ['周日', '周一', '周二', '周三', '周四', '周五', '周六'];
  const w = weekDays[now.getDay()];
  return `${m}月${d}日 ${w} · 30s 自动刷新`;
});

// Traffic Periods
const traffic24h = ref<{ upload: number; download: number } | null>(null);
const traffic7d = ref<{ upload: number; download: number } | null>(null);
const trafficMonth = ref<{ upload: number; download: number } | null>(null);
const trafficLimit = ref<number | null>(null);
const usedTraffic = ref<number>(0);

// Active Tab in Combined Traffic Card (Portal)
const activeTrafficPeriod = ref<'24h' | '7d' | 'month'>('24h');

// User Connection & Device State
const currentUserOnlineConnections = ref<number | null>(null);
const currentUserMaxConnections = ref<number>(0);
const currentUserOnlineDevicesCount = ref<number>(0);
const currentUserTotalDevicesCount = ref<number>(0);
const latestActiveDeviceName = ref<string>('');

const maxTraffic = computed(() => Math.max(1, ...(dashboard.value?.trend || []).map(h => h.upload + h.download)));

function barWidth(hour: Hour) {
  return ((hour.upload + hour.download) / maxTraffic.value * 100).toFixed(2) + '%';
}

// ==========================================
// 1. 运行概况指标计算
// ==========================================
function isNodeOnline(node: NodeView): boolean {
  if (!node.enabled || node.deletedAt || node.maintenance) return false;
  return node.status === 'online' || node.status === 'pending';
}

const adminOnlineNodesCount = computed(() => allNodes.value.filter(isNodeOnline).length);
const offlineNodesCount = computed(() => allNodes.value.filter(n => !isNodeOnline(n)).length);

const healthyNodesCount = computed(() => {
  return allNodes.value.filter(n => isNodeOnline(n) && n.heartbeat?.core_healthy !== false && !n.heartbeat?.core_error).length;
});

const activeUsersCount = computed(() => allUsers.value.filter(u => u.status === 'active').length);

const warningUsersCount = computed(() => {
  const now = Date.now();
  return allUsers.value.filter(u => {
    if (u.trafficLimit && u.usedTraffic >= u.trafficLimit) return true;
    if (u.expiresAt) {
      const exp = new Date(u.expiresAt).getTime();
      const diffDays = (exp - now) / (1000 * 3600 * 24);
      if (diffDays <= 7) return true;
    }
    return false;
  }).length;
});

const todayUpload = computed(() => dashboard.value?.counts.todayUpload || 0);
const todayDownload = computed(() => dashboard.value?.counts.todayDownload || 0);
const todayTrafficTotal = computed(() => todayUpload.value + todayDownload.value);

const nodeTrafficPeriod = ref<'total' | 'today'>('total');

const displayUploadBytes = computed(() => {
  if (nodeTrafficPeriod.value === 'today') {
    return dashboard.value?.counts.todayUpload || 0;
  }
  return allNodes.value.reduce((acc, n) => acc + (n.heartbeat?.upload_bytes || 0), 0);
});

const displayDownloadBytes = computed(() => {
  if (nodeTrafficPeriod.value === 'today') {
    return dashboard.value?.counts.todayDownload || 0;
  }
  return allNodes.value.reduce((acc, n) => acc + (n.heartbeat?.download_bytes || 0), 0);
});

const displayTotalBytes = computed(() => {
  return displayUploadBytes.value + displayDownloadBytes.value;
});

const uploadRatioPercent = computed(() => {
  const total = displayTotalBytes.value;
  if (total <= 0) return 0;
  return Math.round((displayUploadBytes.value / total) * 100);
});

const downloadRatioPercent = computed(() => {
  const total = displayTotalBytes.value;
  if (total <= 0) return 0;
  return 100 - uploadRatioPercent.value;
});

// 节点流量排行列表
const sortedNodeTrafficList = computed(() => {
  const list = allNodes.value.map(node => {
    const t = (node.heartbeat?.upload_bytes || 0) + (node.heartbeat?.download_bytes || 0);
    return { node, traffic: t, percent: 0 };
  });

  list.sort((a, b) => b.traffic - a.traffic);
  const max = Math.max(1, list[0]?.traffic || 1);
  return list.slice(0, 5).map(item => ({
    ...item,
    percent: item.traffic > 0 ? Math.min(100, Math.max(4, Math.round((item.traffic / max) * 100))) : 0
  }));
});

// ==========================================
// 2. 待处理问题
// ==========================================
interface PendingIssue {
  title: string;
  desc: string;
  tag: string;
  level: 'danger' | 'warning';
  link: string;
}

const pendingIssues = computed<PendingIssue[]>(() => {
  const issues: PendingIssue[] = [];

  // A. 节点离线
  for (const n of allNodes.value) {
    if (n.deletedAt) continue;
    if (n.enabled === false) continue;
    if (n.maintenance) continue;
    if (isNodeOnline(n)) continue;
    if (n.status === 'pending') {
      issues.push({
        title: `节点待登记: ${n.name}`,
        desc: '等待登记。',
        tag: n.publicIp || n.hostname || '未分配IP',
        level: 'warning',
        link: `/nodes/${n.id}`
      });
      continue;
    }
    if (n.status === 'error') {
      issues.push({
        title: `节点异常: ${n.name}`,
        desc: '节点异常。',
        tag: n.publicIp || n.hostname || '未分配IP',
        level: 'danger',
        link: `/nodes/${n.id}`
      });
      continue;
    }
    if (n.status === 'offline') {
      issues.push({
        title: `节点离线: ${n.name}`,
        desc: '节点离线，心跳中断。',
        tag: n.publicIp || n.hostname || '未分配IP',
        level: 'danger',
        link: `/nodes/${n.id}`
      });
    }
  }

  // B. 核心异常 (core_healthy === false 或 core_error)
  for (const n of allNodes.value) {
    if (n.deletedAt) continue;
    if (n.heartbeat?.core_healthy === false || n.heartbeat?.core_error) {
      issues.push({
        title: `代理核心异常: ${n.name}`,
        desc: `核心状态未通过健康检查 (${n.heartbeat.core_error || '核心未运行'})。`,
        tag: unhealthyCoreLabel(n) || n.capabilities?.core_name || '代理核心',
        level: 'danger',
        link: `/nodes/${n.id}`
      });
    }
  }

  // C. 配置发布失败
  const failedRevs = allRevisions.value.filter(r => r.status === 'failed' || r.resultCode === 'error');
  for (const r of failedRevs.slice(0, 3)) {
    issues.push({
      title: `配置发布失败: 版本 #${r.id}`,
      desc: r.summary || '节点配置发布未成功应用。',
      tag: `节点 #${r.nodeId}`,
      level: 'warning',
      link: '/system/revisions'
    });
  }

  // D. 用户流量或到期告警 (超额或 <= 7天)
  for (const u of allUsers.value) {
    if (u.trafficLimit > 0 && u.usedTraffic >= u.trafficLimit) {
      issues.push({
        title: `用户流量耗尽: ${u.displayName || u.username}`,
        desc: `已消耗超出授权配额 (${formatGiB(u.usedTraffic)} / ${formatGiB(u.trafficLimit)})。`,
        tag: `@${u.username}`,
        level: 'warning',
        link: `/users/${u.id}`
      });
    }
  }

  return issues;
});

// ==========================================
// 3. 用户用量与到期提醒
// ==========================================
const topAttentionUsers = computed(() => {
  const users = [...allUsers.value];
  users.sort((a, b) => {
    const aWarn = a.trafficLimit > 0 && a.usedTraffic >= a.trafficLimit ? 1 : 0;
    const bWarn = b.trafficLimit > 0 && b.usedTraffic >= b.trafficLimit ? 1 : 0;
    if (aWarn !== bWarn) return bWarn - aWarn;
    const aPct = a.trafficLimit > 0 ? a.usedTraffic / a.trafficLimit : 0;
    const bPct = b.trafficLimit > 0 ? b.usedTraffic / b.trafficLimit : 0;
    return bPct - aPct;
  });
  return users.slice(0, 4);
});

function userUsagePercent(u: ProxyUser): number {
  if (!u.trafficLimit || u.trafficLimit <= 0) return 0;
  return Math.min(100, Math.round((u.usedTraffic / u.trafficLimit) * 100));
}

function formatGiB(bytes: number): string {
  if (!bytes || bytes <= 0) return '0 GiB';
  return (bytes / 1073741824).toFixed(1) + ' GiB';
}

function getUserExpiryBadgeClass(u: ProxyUser): string {
  if (u.trafficLimit && u.usedTraffic >= u.trafficLimit) {
    return 'bg-rose-50 text-ios-red border border-rose-200/80';
  }
  if (!u.expiresAt) {
    return 'bg-black/[0.03] text-ios-secondary';
  }
  const diffDays = (new Date(u.expiresAt).getTime() - Date.now()) / (1000 * 3600 * 24);
  if (diffDays <= 0) {
    return 'bg-rose-50 text-ios-red border border-rose-200/80';
  }
  if (diffDays <= 7) {
    return 'bg-amber-50 text-amber-700 border border-amber-200/80';
  }
  return 'bg-emerald-50 text-emerald-600 border border-emerald-200/80';
}

function getUserExpiryLabel(u: ProxyUser): string {
  if (u.trafficLimit && u.usedTraffic >= u.trafficLimit) {
    return '已超额';
  }
  if (!u.expiresAt) return '永久有效';
  const diffDays = Math.ceil((new Date(u.expiresAt).getTime() - Date.now()) / (1000 * 3600 * 24));
  if (diffDays <= 0) return '已过期';
  if (diffDays <= 7) return `剩 ${diffDays} 天`;
  return `有效 (余${diffDays}天)`;
}

// 审计访问记录组合
const combinedRecentRecords = computed(() => {
  const access = dashboard.value?.recentAccess || [];
  const blocked = dashboard.value?.recentBlocked || [];
  const combined = [
    ...access.map(r => ({ ...r, allowed: true })),
    ...blocked.map(r => ({ ...r, allowed: false }))
  ];
  combined.sort((a, b) => new Date(b.time).getTime() - new Date(a.time).getTime());
  return combined.slice(0, 5);
});

// ==========================================
// PORTAL METRICS COMPUTATION
// ==========================================
const usedPercent = computed(() => {
  if (!trafficLimit.value || trafficLimit.value <= 0) return 0;
  return Math.min(100, Math.round((usedTraffic.value / trafficLimit.value) * 100));
});

const currentTrafficMetric = computed(() => {
  if (activeTrafficPeriod.value === '7d') {
    const t = traffic7d.value;
    if (t === null || t === undefined) {
      return {
        periodLabel: '近 7 天流量',
        total: null,
        upload: null,
        download: null
      };
    }
    return {
      periodLabel: '近 7 天流量',
      total: t.upload + t.download,
      upload: t.upload,
      download: t.download
    };
  }
  if (activeTrafficPeriod.value === 'month') {
    const t = trafficMonth.value;
    if (t === null || t === undefined) {
      return {
        periodLabel: '当月累计流量',
        total: null,
        upload: null,
        download: null
      };
    }
    return {
      periodLabel: '当月累计流量',
      total: t.upload + t.download,
      upload: t.upload,
      download: t.download
    };
  }
  const t = traffic24h.value;
  if (t === null || t === undefined) {
    return {
      periodLabel: '近 24 小时流量',
      total: null,
      upload: null,
      download: null
    };
  }
  return {
    periodLabel: '近 24 小时流量',
    total: t.upload + t.download,
    upload: t.upload,
    download: t.download
  };
});

const userConnections = computed(() => {
  return currentUserOnlineConnections.value;
});

const maxConnectionsLimit = computed(() => currentUserMaxConnections.value);
const onlineDevicesCount = computed(() => currentUserOnlineDevicesCount.value);
const totalDevicesCount = computed(() => Math.max(currentUserTotalDevicesCount.value, currentUserOnlineDevicesCount.value));

const remainingTraffic = computed(() => {
  if (trafficLimit.value && trafficLimit.value > 0) {
    return Math.max(0, trafficLimit.value - usedTraffic.value);
  }
  return null;
});

// Data Loading
async function loadTrafficData() {
  const now = new Date();
  const from24h = new Date(now.getTime() - 24 * 60 * 60 * 1000);
  const from7d = new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000);
  const fromMonth = new Date(now.getFullYear(), now.getMonth(), 1, 0, 0, 0, 0);

  // Fetch nodes list
  try {
    const res = await nodeAPI.list(1, '', false);
    allNodes.value = res.items;
  } catch {
    if (dashboard.value?.nodes) {
      allNodes.value = dashboard.value.nodes;
    }
  }

  // Fetch users & revisions if admin/owner
  if (isOwner.value) {
    try {
      const [uRes] = await Promise.all([
        userAPI.list(1, '', 50).catch(() => ({ items: [], total: 0 }))
      ]);
      allUsers.value = uRes.items;
      if (dashboard.value?.revisions) {
        allRevisions.value = dashboard.value.revisions;
      }
    } catch {
      // ignore
    }
  }

}

async function loadDashboard() {
  try {
    dashboard.value = await dashboardAPI.get();
    if (dashboard.value?.revisions) {
      allRevisions.value = dashboard.value.revisions;
    }
    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  }
}

async function loadAll() {
  if (loading.value) return;
  loading.value = true;
  try {
    await Promise.all([loadDashboard(), loadTrafficData()]);
  } finally {
    loading.value = false;
  }
}

onMounted(() => {
  void loadAll();
  timer = setInterval(() => {
    void loadAll();
  }, 30000);
});

onUnmounted(() => {
  if (timer) clearInterval(timer);
});
</script>
