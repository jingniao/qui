<template>
  <div class="space-y-6 pb-16">
    <!-- Header -->
    <PageHeader
      title="客户端策略"
      parent="分流控制"
      description="管理 Clash / Mihomo 客户端的分流规则、优先级、策略组与运行参数。通过完整配置订阅下发，用户更新订阅后生效。"
    >
      <template #badge>
        <span
          v-if="isDirty"
          class="px-2.5 py-0.5 rounded-full text-xs font-semibold bg-amber-50 text-amber-700 border border-amber-200/80 inline-flex items-center gap-1.5 shadow-2xs"
        >
          <span class="w-1.5 h-1.5 rounded-full bg-amber-500 animate-pulse"></span>
          <span>未保存草稿</span>
        </span>
        <span
          v-else
          class="px-2.5 py-0.5 rounded-full text-xs font-medium bg-emerald-50 text-emerald-700 border border-emerald-200/60 inline-flex items-center gap-1.5"
        >
          <span class="w-1.5 h-1.5 rounded-full bg-emerald-500"></span>
          <span>已同步订阅配置</span>
        </span>
      </template>

      <template #actions>
        <div class="flex items-center gap-2.5 min-w-0">
          <!-- 新建配置 -->
          <button
            type="button"
            @click="openNewConfigModal"
            class="ios-btn-primary inline-flex items-center gap-1.5 shadow-sm text-xs sm:text-sm font-semibold"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
            </svg>
            <span>新建配置</span>
          </button>
        </div>
      </template>
    </PageHeader>

    <!-- 主页面：客户端策略配置卡片集合 (已建好的策略配置) -->
    <div v-if="savedPolicies.length > 0" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
      <div
        v-for="policy in savedPolicies"
        :key="policy.id"
        class="panel-card p-5 flex flex-col justify-between space-y-4 hover:shadow-md transition-all border border-black/[0.06]"
      >
        <div class="space-y-3">
          <!-- 头部：标题与启用状态 -->
          <div class="flex items-start justify-between gap-2">
            <div class="min-w-0 flex-1">
              <h4 class="text-sm font-bold text-neutral-900 truncate" :title="policy.name">{{ policy.name }}</h4>
              <p class="text-xs text-neutral-500 mt-1 line-clamp-2 leading-relaxed">
                {{ policy.description || '自定义客户端分流策略配置' }}
              </p>
            </div>
            <span
              v-if="activePolicyId === policy.id"
              class="shrink-0 text-[11px] bg-emerald-50 text-emerald-700 border border-emerald-200/80 font-bold px-2 py-0.5 rounded-full inline-flex items-center gap-1"
            >
              <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 animate-pulse"></span>
              已启用
            </span>
            <span
              v-else
              class="shrink-0 text-[11px] bg-neutral-100 text-neutral-500 font-medium px-2 py-0.5 rounded-full"
            >
              未启用
            </span>
          </div>

          <!-- 核心指标摘要 -->
          <div class="grid grid-cols-3 gap-2 py-2 border-y border-black/[0.05] text-center bg-black/[0.015] rounded-xl px-2">
            <div>
              <span class="text-[10px] text-neutral-400 block">模式</span>
              <span class="text-xs font-bold text-neutral-800 uppercase mt-0.5 block truncate">
                {{ formatModeName(policy.config.settings.mode) }}
              </span>
            </div>
            <div>
              <span class="text-[10px] text-neutral-400 block">策略组</span>
              <span class="text-xs font-bold text-neutral-800 mt-0.5 block">
                {{ policy.config.proxyGroups.length }} 个
              </span>
            </div>
            <div>
              <span class="text-[10px] text-neutral-400 block">规则数</span>
              <span class="text-xs font-bold text-neutral-800 mt-0.5 block">
                {{ policy.config.rules.filter(r => r.enabled).length }} 条
              </span>
            </div>
          </div>

          <div class="flex items-center justify-between text-[11px] text-neutral-400 pt-0.5 font-mono">
            <span>端口: {{ policy.config.settings.mixedPort }}</span>
            <span>{{ formatDateTime(policy.updatedAt) }}</span>
          </div>
        </div>

        <!-- 卡片底部 3 个按钮：编辑、启用、删除（统一三等分规格与标准按钮样式） -->
        <div class="grid grid-cols-3 gap-2 pt-3 border-t border-black/[0.06]">
          <!-- 1. 编辑 -->
          <button
            type="button"
            @click="editPolicy(policy)"
            class="ios-btn-secondary text-xs h-8 px-2 rounded-xl font-medium inline-flex items-center justify-center gap-1.5 transition-all shadow-2xs"
            title="编辑该策略的分流规则与配置"
          >
            <svg class="w-3.5 h-3.5 text-neutral-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
            </svg>
            <span>编辑</span>
          </button>

          <!-- 2. 启用 / 已启用 -->
          <button
            v-if="activePolicyId !== policy.id"
            type="button"
            @click="enablePolicy(policy)"
            :disabled="saving"
            class="ios-btn-primary text-xs h-8 px-2 rounded-xl font-medium inline-flex items-center justify-center gap-1.5 shadow-2xs transition-all"
            title="设为当前生效策略并下发订阅"
          >
            <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
            </svg>
            <span>启用</span>
          </button>
          <button
            v-else
            type="button"
            disabled
            class="ios-btn-secondary text-xs h-8 px-2 rounded-xl font-semibold inline-flex items-center justify-center gap-1.5 bg-emerald-50 text-emerald-700 border-emerald-200/90 cursor-default opacity-100 shadow-2xs"
            title="该策略当前正在生效中"
          >
            <span class="w-1.5 h-1.5 rounded-full bg-emerald-500 shrink-0"></span>
            <span>已启用</span>
          </button>

          <!-- 3. 删除 -->
          <button
            type="button"
            @click="promptDeletePolicy(policy)"
            :class="[
              activePolicyId === policy.id
                ? 'opacity-40 cursor-not-allowed text-neutral-400 border-black/[0.08] hover:bg-white hover:text-neutral-400'
                : 'text-neutral-600 hover:text-red-600 hover:bg-red-50 hover:border-red-200 border-black/[0.08]'
            ]"
            class="ios-btn-secondary text-xs h-8 px-2 rounded-xl font-medium inline-flex items-center justify-center gap-1.5 transition-all shadow-2xs"
            :title="activePolicyId === policy.id ? '当前生效中的策略不可删除' : '删除此策略配置'"
          >
            <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
            <span>删除</span>
          </button>
        </div>
      </div>
    </div>

    <!-- 空状态：暂无配置 -->
    <div v-else class="panel-card p-12 text-center space-y-4">
      <div class="w-12 h-12 mx-auto rounded-2xl bg-blue-50 text-ios-blue flex items-center justify-center">
        <svg class="w-6 h-6" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
        </svg>
      </div>
      <div>
        <h3 class="text-sm font-bold text-neutral-900">暂无客户端策略配置</h3>
        <p class="text-xs text-neutral-500 mt-1">点击右上角「新建配置」创建首个分流策略</p>
      </div>
      <button
        type="button"
        @click="openNewConfigModal"
        class="ios-btn-primary text-xs inline-flex items-center gap-1.5"
      >
        <span>新建配置</span>
      </button>
    </div>

    <!-- 弹窗页面：配置编辑与详细内容模态弹窗 (AppDialog) -->
    <AppDialog
      v-model="configModalVisible"
      :title="modalTitle"
      description="左侧选择配置模块，右侧修改模块内容。右上角可保存并生效、查看版本信息或关闭。"
      maxWidth="full"
      maxWidthClass="max-w-[1400px] xl:max-w-[1520px] 2xl:max-w-[1640px] w-full"
      heightClass="h-[88vh] max-h-[900px] min-h-[640px]"
      bodyClass="flex flex-col flex-1 min-h-0 p-6 overflow-hidden"
      :showFooter="false"
    >
      <!-- 右上角 3 个按钮：版本、保存、关闭 -->
      <template #header-actions>
        <div class="flex items-center gap-2">
          <!-- 1. 版本 -->
          <button
            type="button"
            @click="showVersionDialog = true"
            class="ios-btn-secondary text-xs px-2.5 py-1.5 inline-flex items-center gap-1 font-mono"
            title="查看策略修订版本与更新记录"
          >
            <svg class="w-3.5 h-3.5 text-neutral-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 8v4l3 3m6-3a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
            <span>版本 #{{ clientConfig.version }}</span>
          </button>

          <!-- 2. 保存 -->
          <button
            type="button"
            @click="saveAll"
            :disabled="saving"
            class="ios-btn-primary text-xs px-3.5 py-1.5 inline-flex items-center gap-1.5 font-semibold shadow-xs"
          >
            <svg v-if="!saving" class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
            </svg>
            <svg v-else class="w-3.5 h-3.5 animate-spin" viewBox="0 0 24 24" fill="none">
              <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle>
              <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8v8H4z"></path>
            </svg>
            <span>{{ saving ? '保存中…' : (isDirty ? '保存 (已修改)' : '保存') }}</span>
          </button>

          <!-- 3. 关闭 -->
          <button
            type="button"
            @click="handleCloseModal"
            class="ios-btn-secondary text-xs px-3 py-1.5 inline-flex items-center gap-1"
          >
            <span>关闭</span>
          </button>
        </div>
      </template>

      <!-- 弹窗内部：左边选择模块，右边设置对应模块内容 -->
      <div @click="activeRuleMenuId = null" class="flex flex-col md:flex-row gap-6 flex-1 min-h-0 h-full overflow-hidden">
        <!-- 左侧导航：模块选择（紧凑缩窄，悬停浮窗查看详细描述） -->
        <div class="w-full md:w-56 lg:w-60 shrink-0 space-y-2 border-b md:border-b-0 md:border-r border-black/[0.06] pb-4 md:pb-0 md:pr-4 overflow-y-auto h-full pr-1">
          <div class="px-2 py-1 text-[11px] font-semibold text-neutral-400 uppercase tracking-wider">
            配置模块清单
          </div>

          <button
            v-for="mod in modulesList"
            :key="mod.id"
            type="button"
            @click="activeModule = mod.id"
            :title="mod.desc"
            :class="[
              'w-full text-left p-2.5 rounded-xl transition-all border flex items-center justify-between group',
              activeModule === mod.id
                ? 'bg-blue-50/70 border-ios-blue text-ios-blue ring-1 ring-ios-blue/30 shadow-2xs'
                : 'bg-white border-black/[0.06] hover:border-black/[0.15] hover:bg-black/[0.015] text-neutral-800'
            ]"
          >
            <div class="flex items-center gap-2.5 min-w-0">
              <div
                :class="[
                  'w-7 h-7 rounded-lg flex items-center justify-center shrink-0 transition-colors',
                  activeModule === mod.id
                    ? 'bg-ios-blue text-white shadow-2xs'
                    : 'bg-black/[0.04] text-neutral-600 group-hover:bg-black/[0.08]'
                ]"
              >
                <component :is="mod.icon" class="w-3.5 h-3.5" />
              </div>
              <div class="min-w-0">
                <span class="text-xs font-bold block truncate">{{ mod.name }}</span>
                <span class="text-[9px] font-mono opacity-60 block uppercase truncate">{{ mod.en }}</span>
              </div>
            </div>

            <!-- 状态徽标 -->
            <span
              v-if="mod.id === 'groups'"
              class="text-[10px] px-1.5 py-0.5 rounded-md font-mono bg-black/[0.04] text-neutral-600 shrink-0"
            >
              {{ clientConfig.proxyGroups.length }} 组
            </span>
            <span
              v-else-if="mod.id === 'rules'"
              class="text-[10px] px-1.5 py-0.5 rounded-md font-mono bg-black/[0.04] text-neutral-600 shrink-0"
            >
              {{ clientConfig.rules.length }} 条
            </span>
            <span
              v-else-if="mod.id === 'dns'"
              class="text-[9px] px-1.5 py-0.5 rounded-md font-medium shrink-0"
              :class="clientConfig.extra.dns.enable ? 'bg-emerald-50 text-emerald-700' : 'bg-neutral-100 text-neutral-500'"
            >
              {{ clientConfig.extra.dns.enable ? '已启用' : '关闭' }}
            </span>
            <span
              v-else-if="mod.id === 'providers'"
              class="text-[10px] px-1.5 py-0.5 rounded-md font-mono bg-black/[0.04] text-neutral-600 shrink-0"
            >
              {{ clientConfig.extra.providers.length + serviceSets.length }}
            </span>
          </button>
        </div>

        <!-- 右侧区域：对应模块设置内容 -->
        <div
          class="flex-1 min-w-0 px-4 sm:px-6 h-full flex flex-col"
          :class="activeModule === 'rules' ? 'overflow-hidden pb-1' : 'overflow-y-auto overflow-x-hidden space-y-4 pb-4'"
        >
          <!-- 模块 1：基础设置 (Settings) -->
          <div v-if="activeModule === 'settings'" class="space-y-6">
            <div class="border-b border-black/[0.06] pb-3">
              <h3 class="text-base font-bold text-neutral-900">基础设置 (Settings)</h3>
              <p class="text-xs text-neutral-500 mt-0.5">Clash 如何运行 比如监听端口、运行模式、是否允许局域网设备连接等。</p>
            </div>

            <!-- 策略名称与说明 -->
            <div class="panel-card p-5 space-y-4 bg-black/[0.015] border border-black/[0.06]">
              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">
                    策略配置名称 <span class="text-red-500">*</span>
                  </label>
                  <input
                    v-model="policyNameInput"
                    type="text"
                    placeholder="例如: 常用分流策略 / 基础直连"
                    class="form-input-base text-xs"
                  />
                </div>
                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">策略配置说明 (可选)</label>
                  <input
                    v-model="policyDescInput"
                    type="text"
                    placeholder="简短描述该策略的适用场景"
                    class="form-input-base text-xs"
                  />
                </div>
              </div>
              <div v-if="modalMode === 'create'" class="pt-1">
                <label class="inline-flex items-center gap-2 text-xs text-neutral-700 cursor-pointer">
                  <input v-model="setAsActiveOnSave" type="checkbox" />
                  <span>保存后立即设为当前生效策略并下发生效</span>
                </label>
              </div>
            </div>

            <!-- 运行基础参数表单 -->
            <div class="panel-card p-6 space-y-5 border border-black/[0.06]">
              <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-5">

                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">
                    混合端口 (Mixed Port)
                  </label>
                  <input
                    v-model.number="clientConfig.settings.mixedPort"
                    type="number"
                    min="1024"
                    max="65535"
                    class="form-input-base text-xs font-mono"
                    placeholder="7890"
                    @input="markDirty"
                  />
                  <span v-if="mixedPortError" class="text-[11px] text-red-500 mt-1 block">{{ mixedPortError }}</span>
                  <span v-else class="text-[11px] text-neutral-400 mt-1 block">同时支持 HTTP / SOCKS5 (1024~65535)</span>
                </div>

                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">
                    允许局域网连接 (Allow LAN)
                  </label>
                  <AppSelect
                    v-model="clientConfig.settings.allowLan"
                    :options="allowLanOptions"
                    @change="markDirty"
                  />
                  <span class="text-[11px] text-neutral-400 mt-1 block">绑定 0.0.0.0 提供代理服务</span>
                </div>

                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">
                    日志级别 (Log Level)
                  </label>
                  <AppSelect
                    v-model="clientConfig.settings.logLevel"
                    :options="logLevelOptions"
                    @change="markDirty"
                  />
                  <span class="text-[11px] text-neutral-400 mt-1 block">客户端本地控制台输出等级</span>
                </div>
              </div>

              <div class="pt-2 border-t border-black/[0.04]">
                <label class="inline-flex items-center gap-2 text-xs text-neutral-700 cursor-pointer">
                  <input v-model="clientConfig.extra.ipv6" type="checkbox" @change="markDirty" />
                  <span>启用 IPv6 解析支持 (客户端优先向双栈目标建立连接)</span>
                </label>
              </div>
            </div>

          </div>

          <!-- 模块 2：代理组 (Groups) - 用途组 + 地区组分层架构 -->
          <div v-else-if="activeModule === 'groups'" class="space-y-5">
            <!-- 头部：标题与主操作按钮群 -->
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-black/[0.06] pb-3">
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="text-base font-bold text-neutral-900">代理组编排 (Proxy Groups)</h3>
                  <span class="text-[11px] px-2 py-0.5 rounded-full bg-blue-50 text-ios-blue font-semibold border border-blue-100">
                    双层拓扑
                  </span>
                </div>
                <p class="text-xs text-neutral-500 mt-0.5">
                  用途组决定业务流量选哪个出口，地区组在区域内优选低延迟落地节点。
                </p>
              </div>

              <!-- 顶部右侧主操作群 -->
              <div class="flex flex-wrap items-center gap-2">
                <button
                  type="button"
                  @click="refreshDelays"
                  :disabled="isRefreshingDelays"
                  class="ios-btn-secondary text-xs px-2.5 py-1.5 inline-flex items-center gap-1.5"
                  title="刷新各节点测速延迟"
                >
                  <svg
                    class="w-3.5 h-3.5 text-neutral-600"
                    :class="{ 'animate-spin': isRefreshingDelays }"
                    fill="none"
                    viewBox="0 0 24 24"
                    stroke="currentColor"
                  >
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                  </svg>
                  <span>{{ isRefreshingDelays ? '测速中...' : '刷新延迟' }}</span>
                </button>

                <button
                  type="button"
                  @click="addNewServiceGroup"
                  class="ios-btn-primary text-xs px-2.5 py-1.5 inline-flex items-center gap-1 shadow-2xs font-semibold"
                >
                  <span>＋ 新建用途组</span>
                </button>

                <button
                  type="button"
                  @click="addNewRegionGroup"
                  class="ios-btn-secondary text-xs px-2.5 py-1.5 inline-flex items-center gap-1 font-semibold text-neutral-800 hover:border-ios-blue"
                >
                  <span>＋ 新建地区组</span>
                </button>
              </div>
            </div>

            <!-- 循环引用警示条 (若检测到环路) -->
            <div
              v-if="cycleWarning"
              class="p-3 bg-red-50/90 border border-red-200 rounded-2xl flex items-start gap-2.5 text-xs text-red-800 shadow-2xs"
            >
              <span class="text-base leading-none">⚠️</span>
              <div class="space-y-0.5">
                <span class="font-bold">严重错误：代理组之间形成了闭环循环引用！</span>
                <p class="text-[11px] text-red-700">
                  环路路径：<code class="font-mono font-bold bg-white/80 px-1.5 py-0.5 rounded border border-red-200">{{ cycleWarning }}</code>。请调整对应组的可选出口以解除循环。
                </p>
              </div>
            </div>

            <!-- 分组分类 Tab 栏与搜索栏 -->
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 bg-neutral-50/80 p-2 rounded-2xl border border-black/[0.05]">
              <!-- 分类 Tabs (Segmented Control) -->
              <div class="flex items-center gap-1 p-1 bg-black/[0.04] rounded-xl text-xs font-semibold">
                <button
                  type="button"
                  @click="groupCategoryTab = 'service'"
                  :class="[
                    'px-3 py-1.5 rounded-lg transition-all flex items-center gap-1.5',
                    groupCategoryTab === 'service'
                      ? 'bg-white text-neutral-900 shadow-2xs'
                      : 'text-neutral-500 hover:text-neutral-900'
                  ]"
                >
                  <span>🎯 用途分组</span>
                  <span class="text-[10px] px-1.5 py-0.2 rounded-full font-mono" :class="groupCategoryTab === 'service' ? 'bg-blue-100 text-blue-800 font-bold' : 'bg-neutral-200 text-neutral-600'">
                    {{ serviceGroupsCount }}
                  </span>
                </button>

                <button
                  type="button"
                  @click="groupCategoryTab = 'region'"
                  :class="[
                    'px-3 py-1.5 rounded-lg transition-all flex items-center gap-1.5',
                    groupCategoryTab === 'region'
                      ? 'bg-white text-neutral-900 shadow-2xs'
                      : 'text-neutral-500 hover:text-neutral-900'
                  ]"
                >
                  <span>🌏 地区分组</span>
                  <span class="text-[10px] px-1.5 py-0.2 rounded-full font-mono" :class="groupCategoryTab === 'region' ? 'bg-amber-100 text-amber-800 font-bold' : 'bg-neutral-200 text-neutral-600'">
                    {{ regionGroupsCount }}
                  </span>
                </button>

                <button
                  type="button"
                  @click="groupCategoryTab = 'all'"
                  :class="[
                    'px-3 py-1.5 rounded-lg transition-all flex items-center gap-1.5',
                    groupCategoryTab === 'all'
                      ? 'bg-white text-neutral-900 shadow-2xs'
                      : 'text-neutral-500 hover:text-neutral-900'
                  ]"
                >
                  <span>🔀 全部 / 其他</span>
                  <span class="text-[10px] px-1.5 py-0.2 rounded-full font-mono" :class="groupCategoryTab === 'all' ? 'bg-neutral-800 text-white font-bold' : 'bg-neutral-200 text-neutral-600'">
                    {{ classifiedGroups.length }}
                  </span>
                </button>
              </div>

              <!-- 搜索框 -->
              <div class="relative w-full sm:w-64">
                <input
                  v-model="groupSearchQuery"
                  type="text"
                  placeholder="搜索分组名称、出口或节点..."
                  style="padding-left: 2.25rem !important;"
                  class="form-input-base text-xs py-1.5 pr-3 w-full"
                />
                <svg class="w-3.5 h-3.5 absolute left-2.5 top-1/2 -translate-y-1/2 text-neutral-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                </svg>
                <button
                  v-if="groupSearchQuery"
                  type="button"
                  @click="groupSearchQuery = ''"
                  class="absolute right-2.5 top-1/2 -translate-y-1/2 text-neutral-400 hover:text-neutral-600 text-xs"
                >
                  ✕
                </button>
              </div>
            </div>

            <!-- 列表渲染：无冗余双层嵌套，以出口选择卡片为视觉焦点的优雅卡片设计 -->
            <div v-if="displayedGroups.length > 0" class="space-y-5">
              <div
                v-for="item in displayedGroups"
                :key="item.index"
                class="panel-card p-5 sm:p-6 space-y-4 border border-black/[0.06] hover:border-black/[0.12] transition-colors rounded-2xl relative shadow-2xs"
                :class="item.category === 'service' ? 'border-l-[3px] border-l-ios-blue' : (item.category === 'region' ? 'border-l-[3px] border-l-amber-500' : 'border-l-[3px] border-l-purple-500')"
              >
                <!-- ============================================== -->
                <!-- 层级 1：卡片头部 (身份与控制) -->
                <!-- ============================================== -->
                <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2.5 pb-2.5 border-b border-black/[0.05]">
                  <div class="flex items-center gap-2.5 flex-1 min-w-0">
                    <span class="w-6 h-6 rounded-lg bg-black/[0.04] text-[11px] font-mono font-bold flex items-center justify-center text-neutral-500 shrink-0">
                      #{{ item.index + 1 }}
                    </span>
                    <input
                      v-model="item.group.name"
                      type="text"
                      placeholder="分组名称"
                      class="text-sm font-bold text-neutral-900 bg-transparent hover:bg-black/[0.03] focus:bg-white focus:ring-1 focus:ring-ios-blue/40 px-2 py-0.5 rounded-lg border border-transparent hover:border-black/[0.08] focus:border-ios-blue transition-all w-40 sm:w-52"
                      @input="markDirty"
                    />
                    <span
                      class="text-[10px] px-2 py-0.5 rounded-full font-bold uppercase tracking-wider shrink-0 border"
                      :class="item.category === 'service' ? 'bg-blue-50 text-ios-blue border-blue-200/80' : (item.category === 'region' ? 'bg-amber-50 text-amber-800 border-amber-200/80' : 'bg-purple-50 text-purple-700 border-purple-200/80')"
                    >
                      {{ item.category === 'service' ? '用途分组' : (item.category === 'region' ? '地区分组' : '其他分组') }}
                    </span>
                  </div>

                  <div class="flex items-center gap-1.5 shrink-0 self-end sm:self-auto">
                    <AppSelect
                      v-model="item.group.type"
                      :options="proxyGroupTypeOptions"
                      size="sm"
                      class="w-36"
                      @change="markDirty"
                    />

                    <div class="h-3.5 w-px bg-black/[0.08] mx-1"></div>

                    <button
                      type="button"
                      :disabled="item.index === 0"
                      @click="moveGroup(item.index, -1)"
                      class="p-1 rounded-lg hover:bg-black/[0.04] text-neutral-400 hover:text-neutral-700 disabled:opacity-20 transition-colors"
                      title="上移此组"
                    >
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 15l7-7 7 7" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      :disabled="item.index === clientConfig.proxyGroups.length - 1"
                      @click="moveGroup(item.index, 1)"
                      class="p-1 rounded-lg hover:bg-black/[0.04] text-neutral-400 hover:text-neutral-700 disabled:opacity-20 transition-colors"
                      title="下移此组"
                    >
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
                      </svg>
                    </button>
                    <button
                      type="button"
                      @click="promptDeleteGroup(item.index)"
                      class="p-1 rounded-lg hover:bg-red-50 text-neutral-400 hover:text-red-500 transition-colors"
                      title="删除此组"
                    >
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                      </svg>
                    </button>
                  </div>
                </div>

                <!-- ============================================== -->
                <!-- 层级 2：用途分组核心 (出口卡片网格选择器) -->
                <!-- ============================================== -->
                <div v-if="item.category === 'service'" class="space-y-3">
                  <div class="space-y-2">
                    <div class="flex items-center justify-between text-xs">
                      <div class="flex items-center gap-2">
                        <span class="font-semibold text-neutral-800">可选出口清单</span>
                        <span class="text-[11px] text-neutral-400 font-normal">
                          (点击任意卡片立即设为当前生效出口，客户端自主切换)
                        </span>
                      </div>
                      <span class="text-[11px] text-neutral-400 font-mono">
                        {{ getGroupMembers(item.group).length }} 个出口
                      </span>
                    </div>

                    <!-- 出口卡片网格 -->
                    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2.5">
                      <!-- 候选出口卡片 -->
                      <div
                        v-for="(member, mIdx) in getGroupMembers(item.group)"
                        :key="mIdx"
                        @click="setGroupPrimaryExit(item.group, member)"
                        class="group/card relative cursor-pointer p-3 rounded-xl border text-left transition-all select-none flex flex-col justify-between min-h-[62px]"
                        :class="mIdx === 0
                          ? 'bg-blue-50/70 border-ios-blue text-ios-blue ring-1 ring-ios-blue/30 shadow-xs'
                          : 'bg-white border-black/[0.07] hover:border-black/[0.18] text-neutral-800 hover:bg-neutral-50/50'"
                      >
                        <!-- 第一行：状态指示点 + 出口名称 + 移除按钮 -->
                        <div class="flex items-center justify-between gap-1">
                          <div class="flex items-center gap-1.5 min-w-0">
                            <span
                              class="w-2 h-2 rounded-full shrink-0"
                              :class="mIdx === 0 ? 'bg-ios-blue ring-2 ring-blue-200' : 'bg-neutral-300 group-hover/card:bg-neutral-400'"
                            ></span>
                            <span class="text-xs font-bold truncate">
                              {{ formatExitLabel(member).title }}
                            </span>
                          </div>
                          <button
                            type="button"
                            @click.stop="removeGroupMember(item.group, mIdx)"
                            class="opacity-0 group-hover/card:opacity-100 hover:text-red-500 p-0.5 rounded text-[11px] text-neutral-400 transition-opacity"
                            title="移除此出口"
                          >
                            ✕
                          </button>
                        </div>

                        <!-- 第二行：最终落地节点与延迟 -->
                        <div class="flex items-center justify-between gap-1 pt-1 mt-1 border-t border-black/[0.03] text-[10px]">
                          <span class="text-neutral-500 truncate font-mono">
                            {{ getFinalNodeForGroup(member).name.split('·')[0].trim() }}
                          </span>
                          <span
                            class="font-mono font-bold shrink-0"
                            :class="getFinalNodeForGroup(member).status === 'ok' ? 'text-emerald-600' : 'text-neutral-400'"
                          >
                            {{ getFinalNodeForGroup(member).latency }}ms
                          </span>
                        </div>

                        <!-- 当前生效角标 -->
                        <span
                          v-if="mIdx === 0"
                          class="absolute -top-1.5 -right-1 text-[9px] bg-ios-blue text-white font-bold px-1.5 py-0.2 rounded-full shadow-2xs uppercase tracking-wider"
                        >
                          生效中
                        </span>
                      </div>

                      <!-- 添加可选出口卡片/按钮 -->
                      <div class="relative min-h-[62px]">
                        <select
                          class="absolute inset-0 w-full h-full opacity-0 cursor-pointer z-10"
                          @change="e => { const val = (e.target as HTMLSelectElement).value; if (val) { addGroupMemberDirect(item.group, val); (e.target as HTMLSelectElement).value = ''; } }"
                        >
                          <option value="">＋ 添加可选出口...</option>
                          <optgroup label="🌏 地区分组">
                            <option
                              v-for="rg in clientConfig.proxyGroups.filter(g => getGroupCategory(g) === 'region' && g.name !== item.group.name)"
                              :key="rg.name"
                              :value="rg.name"
                            >
                              {{ rg.name }}
                            </option>
                          </optgroup>
                          <optgroup label="🚀 动作直连">
                            <option value="DIRECT">DIRECT (用户直连)</option>
                            <option value="REJECT">REJECT (客户端拦截)</option>
                          </optgroup>
                          <optgroup label="🔀 其他用途组">
                            <option
                              v-for="og in clientConfig.proxyGroups.filter(g => g.name !== item.group.name && getGroupCategory(g) !== 'region')"
                              :key="og.name"
                              :value="og.name"
                            >
                              {{ og.name }}
                            </option>
                          </optgroup>
                        </select>

                        <div class="w-full h-full p-2.5 rounded-xl border border-dashed border-black/[0.15] hover:border-ios-blue hover:bg-blue-50/20 text-neutral-400 hover:text-ios-blue transition-colors flex flex-col items-center justify-center gap-0.5 text-center">
                          <span class="text-xs font-bold flex items-center gap-1">
                            <span>＋</span> <span>添加出口</span>
                          </span>
                          <span class="text-[10px] opacity-75">下拉选择候选</span>
                        </div>
                      </div>
                    </div>
                  </div>

                  <!-- 层级 3：关联分流规则轻量化底栏 -->
                  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 pt-3 border-t border-black/[0.05] text-xs">
                    <div class="flex items-center gap-2 flex-1 min-w-0">
                      <span class="text-neutral-400 font-medium shrink-0 flex items-center gap-1">
                        <svg class="w-3.5 h-3.5 text-neutral-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13.828 10.172a4 4 0 00-5.656 0l-4 4a4 4 0 105.656 5.656l1.102-1.101m-.758-4.899a4 4 0 005.656 0l4-4a4 4 0 00-5.656-5.656l-1.1 1.1" />
                        </svg>
                        <span>关联规则:</span>
                      </span>

                      <div v-if="getRulesTargetingGroup(item.group.name).length > 0" class="flex flex-wrap items-center gap-1.5 min-w-0">
                        <span
                          v-for="r in getRulesTargetingGroup(item.group.name)"
                          :key="r.id"
                          @click="openEditDrawer(r)"
                          class="px-2 py-0.5 rounded bg-black/[0.03] hover:bg-blue-50 hover:text-ios-blue hover:border-blue-200 text-neutral-700 border border-black/[0.06] text-[11px] font-medium font-mono cursor-pointer transition-colors"
                          title="点击编辑此条规则"
                        >
                          {{ r.type }}: {{ r.payload || '兜底全部' }}
                        </span>
                      </div>
                      <span v-else class="text-amber-600/90 text-[11px] font-medium flex items-center gap-1">
                        <span>⚠️ 暂无分流规则将流量派发给此组</span>
                      </span>
                    </div>

                    <button
                      type="button"
                      @click="handleGroupRuleAction(item.group.name)"
                      class="text-[11px] text-ios-blue hover:underline font-semibold shrink-0"
                    >
                      编辑规则
                    </button>
                  </div>
                </div>

                <!-- ============================================== -->
                <!-- 层级 2：地区分组核心 (优选胜出看板 + 关键词匹配) -->
                <!-- ============================================== -->
                <div v-else-if="item.category === 'region'" class="space-y-3">
                  <!-- 节点网格与健康测速看板 -->
                  <div class="space-y-1.5">
                    <div class="flex items-center justify-between text-xs">
                      <div class="flex items-center gap-2">
                        <span class="font-semibold text-neutral-800">组内节点优选池</span>
                        <span class="text-[11px] text-neutral-400 font-normal">
                          (自动测速并优选延迟最低节点转发)
                        </span>
                      </div>
                      <div class="text-[11px] text-neutral-400 font-mono">
                        {{ getRegionNodeStats(item.group).total }} 个节点 ({{ getRegionNodeStats(item.group).ok }} 在线 · {{ getRegionNodeStats(item.group).timeout }} 超时)
                      </div>
                    </div>

                    <!-- 节点网格 -->
                    <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-2.5">
                      <!-- 优胜节点卡片 (高亮显示) -->
                      <div class="p-3 rounded-xl border bg-emerald-50/60 border-emerald-300 text-emerald-950 shadow-xs flex flex-col justify-between min-h-[62px] relative">
                        <div class="flex items-center justify-between gap-1">
                          <div class="flex items-center gap-1.5 min-w-0">
                            <span class="w-2 h-2 rounded-full bg-emerald-500 ring-2 ring-emerald-200 shrink-0"></span>
                            <span class="text-xs font-bold truncate">
                              {{ getFinalNodeForGroup(item.group.name).name }}
                            </span>
                          </div>
                        </div>
                        <div class="flex items-center justify-between gap-1 pt-1 mt-1 border-t border-emerald-200/60 text-[10px]">
                          <span class="text-emerald-700 font-medium">当前优选落地</span>
                          <span class="font-mono font-bold text-emerald-700">
                            {{ getFinalNodeForGroup(item.group.name).latency }}ms · 优选在线
                          </span>
                        </div>
                        <span class="absolute -top-1.5 -right-1 text-[9px] bg-emerald-600 text-white font-bold px-1.5 py-0.2 rounded-full shadow-2xs">
                          当前胜出
                        </span>
                      </div>

                      <!-- 快捷添加节点卡片 -->
                      <div class="p-3 rounded-xl border border-dashed border-black/[0.12] bg-neutral-50/50 flex flex-col justify-between min-h-[62px] text-xs">
                        <div class="flex items-center justify-between">
                          <span class="font-semibold text-neutral-600">添加节点来源</span>
                        </div>
                        <div class="flex items-center gap-1.5 pt-1 mt-1 border-t border-black/[0.04]">
                          <button
                            type="button"
                            @click="addGroupMemberDirect(item.group, '@all')"
                            class="px-1.5 py-0.5 rounded bg-blue-50 text-blue-700 font-medium text-[10px] hover:bg-blue-100"
                          >
                            ＋ @all 全部
                          </button>
                          <button
                            type="button"
                            @click="addGroupMemberDirect(item.group, 'DIRECT')"
                            class="px-1.5 py-0.5 rounded bg-emerald-50 text-emerald-700 font-medium text-[10px] hover:bg-emerald-100"
                          >
                            ＋ 直连
                          </button>
                        </div>
                      </div>
                    </div>
                  </div>

                  <!-- 关键词筛选单行轻量条 -->
                  <div class="p-2.5 bg-neutral-50/80 rounded-xl border border-black/[0.04] flex flex-wrap items-center justify-between gap-2 text-xs">
                    <div class="flex items-center gap-2 flex-1 min-w-0">
                      <span class="text-[11px] font-bold text-neutral-500 shrink-0">关键词自动匹配:</span>
                      <input
                        v-model="getGroupKeywordFilter(item.index, item.group).include"
                        type="text"
                        :placeholder="getDefaultIncludeKeywords(item.group.name)"
                        class="form-input-base text-xs font-mono py-1 px-2 flex-1 min-w-[130px] bg-white"
                        title="包含关键词"
                      />
                      <input
                        v-model="getGroupKeywordFilter(item.index, item.group).exclude"
                        type="text"
                        placeholder="排除关键词 (如维护)"
                        class="form-input-base text-xs font-mono py-1 px-2 w-28 bg-white hidden lg:inline"
                        title="排除关键词"
                      />
                    </div>

                    <div class="flex items-center gap-2 shrink-0">
                      <span class="text-[11px] text-neutral-500">
                        匹配到 <strong class="text-neutral-900 font-mono">{{ getMatchedNodesForGroup(item.index, item.group).length }}</strong> 个
                      </span>
                      <button
                        type="button"
                        @click="applyMatchedNodesToGroup(item.index, item.group)"
                        class="ios-btn-secondary text-[11px] px-2 py-0.5 text-ios-blue font-semibold hover:border-ios-blue"
                      >
                        同步至本组
                      </button>
                    </div>
                  </div>

                  <!-- 地区组 Footer -->
                  <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-2 pt-3 border-t border-black/[0.05] text-xs text-neutral-500">
                    <div class="flex items-center gap-2">
                      <span class="text-neutral-400 font-medium">被引用情况:</span>
                      <div v-if="getGroupsReferencingGroup(item.group.name).length > 0" class="flex flex-wrap items-center gap-1.5">
                        <span
                          v-for="rg in getGroupsReferencingGroup(item.group.name)"
                          :key="rg.name"
                          class="px-2 py-0.5 rounded bg-blue-50 text-blue-800 border border-blue-200/80 font-bold text-[11px]"
                        >
                          🎯 {{ rg.name }}
                        </span>
                      </div>
                      <span v-else class="text-neutral-400 italic">
                        暂未被任何用途组引用
                      </span>
                    </div>

                    <div class="text-[11px] text-neutral-400 font-mono">
                      测速周期: {{ item.group.interval || 300 }}s
                    </div>
                  </div>
                </div>

                <!-- ============================================== -->
                <!-- 层级 2：其他分组通用展示 -->
                <!-- ============================================== -->
                <div v-else class="space-y-3">
                  <div class="p-3 bg-neutral-50/80 rounded-xl border border-black/[0.04] space-y-2">
                    <div class="flex flex-wrap items-center gap-1.5 min-h-[42px] p-2 bg-white rounded-lg border border-black/[0.04]">
                      <span
                        v-for="(member, mIdx) in getGroupMembers(item.group)"
                        :key="mIdx"
                        class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg text-xs font-medium border shadow-2xs"
                        :class="formatMemberBadge(member).color"
                      >
                        <span>{{ formatMemberBadge(member).icon }}</span>
                        <span class="font-mono">{{ formatMemberBadge(member).label }}</span>
                        <button
                          type="button"
                          @click="removeGroupMember(item.group, mIdx)"
                          class="hover:opacity-75 p-0.5 rounded-full hover:bg-black/[0.08]"
                        >
                          ✕
                        </button>
                      </span>
                    </div>

                    <div class="flex flex-wrap items-center gap-1.5 pt-1 text-xs">
                      <button
                        type="button"
                        @click="addGroupMemberDirect(item.group, '@all')"
                        class="px-2 py-0.5 rounded-md bg-blue-50 text-blue-700 font-medium border border-blue-100"
                      >
                        🌐 @all
                      </button>
                      <button
                        type="button"
                        @click="addGroupMemberDirect(item.group, 'DIRECT')"
                        class="px-2 py-0.5 rounded-md bg-emerald-50 text-emerald-700 font-medium border border-emerald-100"
                      >
                        🚀 DIRECT
                      </button>
                    </div>
                  </div>
                </div>
              </div>
            </div>

            <!-- 空状态 -->
            <div
              v-else
              class="p-12 text-center bg-neutral-50/60 rounded-2xl border border-dashed border-black/[0.1] space-y-3"
            >
              <div class="text-3xl">🎯</div>
              <div class="text-sm font-bold text-neutral-800">未找到符合条件的代理组</div>
              <p class="text-xs text-neutral-500 max-w-sm mx-auto">
                当前筛选条件下没有代理组。您可以点击上方按钮新建分组，或点击“推荐标准拓扑”一键建立基础分流架构。
              </p>
              <div class="pt-2 flex items-center justify-center gap-2">
                <button
                  type="button"
                  @click="groupSearchQuery = ''; groupCategoryTab = 'all'"
                  class="ios-btn-secondary text-xs px-3 py-1.5"
                >
                  清除搜索筛选
                </button>
                <button
                  type="button"
                  @click="applyStandardTopology"
                  class="ios-btn-primary text-xs px-3 py-1.5"
                >
                  ⚡ 应用推荐拓扑
                </button>
              </div>
            </div>
          </div>

          <!-- 模块 3：分流规则 (Rules) -->
          <div v-else-if="activeModule === 'rules'" class="flex-1 min-h-0 flex flex-col space-y-3 h-full">
            <!-- 1. 顶层标题与操作栏 -->
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-black/[0.06] pb-3 shrink-0">
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="text-base font-bold text-neutral-900">分流规则列表 (Rules)</h3>
                  <span class="text-[11px] px-2 py-0.5 rounded-full font-mono bg-neutral-100 text-neutral-600 font-medium">
                    共 {{ clientConfig.rules.length }} 条
                  </span>
                </div>
                <p class="text-xs text-neutral-500 mt-1">
                  按从上到下顺序匹配，先命中生效；未命中流量由底部兜底规则接管。
                </p>
              </div>
              <div class="flex items-center gap-2">
                <button
                  type="button"
                  @click="openCreateRuleDrawer"
                  class="ios-btn-primary text-xs px-3 py-1.5 inline-flex items-center gap-1 font-semibold shadow-2xs"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>添加规则</span>
                </button>
              </div>
            </div>

            <!-- 2. 一体化规则工作台卡片 (纵向填满容器，彻底解决下方留白过多) -->
            <div class="bg-white rounded-2xl border border-black/[0.08] shadow-2xs overflow-hidden flex-1 min-h-0 flex flex-col mb-1">
              <!-- 卡片顶栏：一体化搜索与筛选工具栏 -->
              <div class="p-3 bg-[#FAFAFA] border-b border-black/[0.06] flex items-center gap-2.5 text-xs w-full min-w-0 flex-wrap sm:flex-nowrap shrink-0">
                <!-- 一体化搜索筛选条 -->
                <div class="flex items-center flex-1 min-w-0 bg-white rounded-xl border border-black/[0.08] hover:border-black/[0.15] focus-within:border-ios-blue focus-within:ring-2 focus-within:ring-ios-blue/20 shadow-2xs divide-x divide-black/[0.06] transition-all">
                  <!-- 1. 搜索输入 -->
                  <div class="relative flex-1 min-w-[120px] flex items-center">
                    <svg class="w-3.5 h-3.5 text-neutral-400 absolute left-2.5 pointer-events-none" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
                    </svg>
                    <input
                      v-model="ruleSearchKeyword"
                      type="text"
                      placeholder="搜索规则…"
                      class="w-full text-xs pl-8 pr-6 py-2 bg-transparent border-0 focus:outline-none text-neutral-800 placeholder-neutral-400"
                    />
                    <button
                      v-if="ruleSearchKeyword"
                      type="button"
                      @click="ruleSearchKeyword = ''"
                      class="absolute right-2 text-neutral-400 hover:text-neutral-600 font-bold text-xs"
                      title="清空搜索"
                    >
                      ×
                    </button>
                  </div>

                  <!-- 2. 规则类型筛选 -->
                  <div class="relative shrink-0">
                    <AppSelect
                      v-model="ruleTypeFilter"
                      :options="ruleTypeFilterOptions"
                      size="sm"
                      trigger-class="border-0 bg-transparent hover:bg-black/[0.04] shadow-none py-1.5 px-2.5 text-xs font-medium text-neutral-700"
                      menu-class="min-w-[190px]"
                    />
                  </div>

                  <!-- 3. 处理目标筛选 -->
                  <div class="relative shrink-0">
                    <AppSelect
                      v-model="ruleTargetFilter"
                      :options="ruleTargetFilterOptions"
                      size="sm"
                      trigger-class="border-0 bg-transparent hover:bg-black/[0.04] shadow-none py-1.5 px-2.5 text-xs font-medium text-neutral-700"
                      menu-class="min-w-[190px]"
                    />
                  </div>
                </div>

                <!-- 代理组进入或筛选目标时的可清除标签 -->
                <span
                  v-if="ruleTargetFilter"
                  class="inline-flex items-center gap-1 px-2.5 py-1.5 rounded-xl bg-blue-50 text-ios-blue text-xs font-semibold border border-blue-100 shrink-0 shadow-2xs"
                >
                  <span>目标: {{ ruleTargetFilter }}</span>
                  <button
                    type="button"
                    @click="ruleTargetFilter = ''"
                    class="hover:text-blue-900 font-bold ml-0.5 text-xs leading-none"
                    title="清除目标筛选"
                  >
                    ×
                  </button>
                </span>

                <!-- 一键重置筛选 -->
                <button
                  v-if="ruleSearchKeyword || ruleTypeFilter || ruleTargetFilter"
                  type="button"
                  @click="resetRuleFilters"
                  class="text-xs text-neutral-400 hover:text-neutral-600 underline shrink-0 whitespace-nowrap ml-1"
                >
                  重置
                </button>

                <!-- 统计数量 -->
                <span class="text-xs text-neutral-400 shrink-0 ml-auto whitespace-nowrap pl-1">
                  共 {{ filteredNonMatchRules.length }} / {{ totalRegularRulesCount }} 条
                </span>
              </div>

              <!-- 表格主体 (flex-1 min-h-0 可垂直滚动) -->
              <div class="flex-1 min-h-0 overflow-y-auto overflow-x-auto">
                <table class="w-full text-left text-xs table-fixed">
                  <thead class="bg-[#F5F5F7] text-neutral-500 font-semibold border-b border-black/[0.06] sticky top-0 z-10">
                    <tr>
                      <th class="py-2.5 px-3.5 w-20">顺序</th>
                      <th class="py-2.5 px-3">匹配条件</th>
                      <th class="py-2.5 px-3 w-48">处理目标</th>
                      <th class="py-2.5 px-3.5 w-36 text-right">操作</th>
                    </tr>
                  </thead>
                  <tbody class="divide-y divide-black/[0.04]">
                    <tr
                      v-for="(rule, index) in filteredNonMatchRules"
                      :key="rule.id"
                      :draggable="!isFiltered"
                      @dragstart="onRuleDragStart($event, rule)"
                      @dragover="onRuleDragOver($event, rule)"
                      @dragleave="onRuleDragLeave(rule)"
                      @drop="onRuleDrop($event, rule)"
                      @dragend="onRuleDragEnd"
                      class="transition-colors group select-none relative"
                      :class="[
                        !rule.enabled ? 'opacity-50 bg-black/[0.01]' : 'hover:bg-blue-50/[0.25]',
                        draggingRuleId === rule.id ? 'opacity-30 bg-blue-50/60' : '',
                        dragOverRuleId === rule.id && dropPosition === 'before' ? 'border-t-2 border-ios-blue bg-blue-50/20' : '',
                        dragOverRuleId === rule.id && dropPosition === 'after' ? 'border-b-2 border-ios-blue bg-blue-50/20' : ''
                      ]"
                    >
                      <!-- 1. 顺序 -->
                      <td class="py-2.5 px-3.5 w-20">
                        <div class="flex items-center gap-2">
                          <span
                            :class="[
                              'p-0.5 rounded transition-colors shrink-0',
                              isFiltered
                                ? 'cursor-not-allowed text-neutral-200'
                                : 'cursor-grab active:cursor-grabbing text-neutral-300 group-hover:text-neutral-500 hover:!text-ios-blue'
                            ]"
                            :title="isFiltered ? '筛选状态下已锁定排序，清除筛选后可拖动调整' : '按住拖动调整匹配执行顺序'"
                          >
                            <svg class="w-3.5 h-3.5" viewBox="0 0 24 24" fill="currentColor">
                              <circle cx="8" cy="6" r="1.5"/><circle cx="16" cy="6" r="1.5"/>
                              <circle cx="8" cy="12" r="1.5"/><circle cx="16" cy="12" r="1.5"/>
                              <circle cx="8" cy="18" r="1.5"/><circle cx="16" cy="18" r="1.5"/>
                            </svg>
                          </span>
                          <div class="flex items-baseline gap-1 font-mono">
                            <span class="text-xs font-bold text-neutral-800">{{ index + 1 }}</span>
                            <span class="text-[10px] text-neutral-400" :title="'排序权重: #' + rule.priority">#{{ rule.priority }}</span>
                          </div>
                        </div>
                      </td>

                      <!-- 2. 匹配条件 (中文名称 · 内容，下方显示备注，规则集支持点击查看) -->
                      <td class="py-2.5 px-3">
                        <div class="flex items-center gap-1.5 flex-wrap">
                          <span
                            :class="['px-2 py-0.5 rounded text-[11px] font-medium shrink-0', getRuleTypeBadgeClass(rule.type)]"
                            :title="'技术标识: ' + rule.type"
                          >
                            {{ getRuleTypeChineseName(rule.type) }}
                          </span>
                          <span class="text-neutral-300">·</span>
                          <button
                            v-if="rule.type === 'RULE-SET'"
                            type="button"
                            @click="viewRuleSet(rule.payload)"
                            class="font-mono text-ios-blue hover:underline font-semibold inline-flex items-center gap-0.5"
                            title="点击查看此规则集"
                          >
                            <span>{{ getRuleSetName(rule.payload) }}</span>
                            <span class="text-[10px]">↗</span>
                          </button>
                          <span v-else class="font-mono text-neutral-800 font-medium break-all">
                            {{ rule.payload }}
                          </span>
                          <span v-if="rule.noResolve" class="text-[9px] bg-neutral-100 text-neutral-600 px-1 py-0.2 rounded font-mono" title="跳过 DNS 解析">
                            no-resolve
                          </span>
                        </div>
                        <p v-if="rule.description" class="text-[11px] text-neutral-400 mt-0.5 truncate" :title="rule.description">
                          {{ rule.description }}
                        </p>
                      </td>

                      <!-- 3. 处理目标 (代理组支持点击查看) -->
                      <td class="py-2.5 px-3 w-48">
                        <button
                          v-if="clientConfig.proxyGroups.some(g => g.name === rule.target)"
                          type="button"
                          @click="navigateToProxyGroup(rule.target)"
                          class="inline-flex items-center gap-1.5 text-xs font-semibold text-neutral-800 hover:text-ios-blue transition-colors group"
                          title="点击前往查看该代理组"
                        >
                          <span :class="['w-1.5 h-1.5 rounded-full shrink-0', getActionDotClass(rule.target)]"></span>
                          <span class="group-hover:underline">{{ formatActionLabel(rule.target) }}</span>
                          <span class="text-neutral-300 group-hover:text-ios-blue text-[10px]">↗</span>
                        </button>
                        <span v-else class="inline-flex items-center gap-1.5 text-xs font-semibold text-neutral-800">
                          <span :class="['w-1.5 h-1.5 rounded-full shrink-0', getActionDotClass(rule.target)]"></span>
                          <span>{{ formatActionLabel(rule.target) }}</span>
                        </span>
                      </td>

                      <!-- 4. 操作 (状态开关 · 编辑 · 更多) -->
                      <td class="py-2.5 px-3.5 w-36 text-right">
                        <div class="flex items-center justify-end gap-2 relative">
                          <!-- 状态开关切换（替代原文本标签） -->
                          <button
                            type="button"
                            @click.stop="toggleRule(rule)"
                            class="relative inline-flex h-4 w-7 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
                            :class="rule.enabled ? 'bg-[#34C759]' : 'bg-neutral-200'"
                            :title="rule.enabled ? '规则已启用 (点击停用)' : '规则已停用 (点击启用)'"
                          >
                            <span
                              class="pointer-events-none inline-block h-3 w-3 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                              :class="rule.enabled ? 'translate-x-3' : 'translate-x-0'"
                            />
                          </button>

                          <!-- 编辑 -->
                          <button
                            type="button"
                            @click="openEditDrawer(rule)"
                            class="text-xs font-medium text-ios-blue hover:text-blue-700 hover:underline"
                          >
                            编辑
                          </button>

                          <span class="text-neutral-300">·</span>

                          <!-- 更多操作下拉 -->
                          <div class="relative inline-block text-left">
                            <button
                              type="button"
                              @click.stop="toggleRuleMenu(rule.id)"
                              class="text-xs font-medium text-neutral-500 hover:text-neutral-800 px-1 py-0.5 rounded hover:bg-black/[0.04]"
                            >
                              更多
                            </button>
                            <div
                              v-if="activeRuleMenuId === rule.id"
                              class="absolute right-0 top-full mt-1 w-32 bg-white rounded-xl shadow-lg border border-black/[0.08] py-1 z-30 text-xs text-left"
                            >
                              <button
                                type="button"
                                :disabled="isRuleAtBound(rule, -1)"
                                @click.stop="moveRuleInList(rule, -1)"
                                class="w-full text-left px-3 py-1.5 text-neutral-700 hover:bg-neutral-50 disabled:opacity-30 disabled:pointer-events-none flex items-center justify-between"
                              >
                                <span>上移一位</span>
                                <span class="text-neutral-400">↑</span>
                              </button>
                              <button
                                type="button"
                                :disabled="isRuleAtBound(rule, 1)"
                                @click.stop="moveRuleInList(rule, 1)"
                                class="w-full text-left px-3 py-1.5 text-neutral-700 hover:bg-neutral-50 disabled:opacity-30 disabled:pointer-events-none flex items-center justify-between"
                              >
                                <span>下移一位</span>
                                <span class="text-neutral-400">↓</span>
                              </button>
                              <button
                                type="button"
                                @click.stop="duplicateRule(rule)"
                                class="w-full text-left px-3 py-1.5 text-neutral-700 hover:bg-neutral-50 flex items-center justify-between"
                              >
                                <span>复制规则</span>
                                <span class="text-neutral-400">📋</span>
                              </button>
                              <div class="border-t border-black/[0.05] my-1"></div>
                              <button
                                type="button"
                                @click.stop="activeRuleMenuId = null; promptDelete(rule)"
                                class="w-full text-left px-3 py-1.5 text-red-600 hover:bg-red-50 flex items-center justify-between font-medium"
                              >
                                <span>删除规则</span>
                                <span>🗑️</span>
                              </button>
                            </div>
                          </div>
                        </div>
                      </td>
                    </tr>
                    <tr v-if="filteredNonMatchRules.length === 0">
                      <td colspan="4" class="py-8 text-center text-xs text-neutral-400">
                        <div class="space-y-2">
                          <p>没有找到符合条件的分流规则</p>
                          <button
                            v-if="isFiltered"
                            type="button"
                            @click="resetRuleFilters"
                            class="ios-btn-secondary text-xs px-2.5 py-1"
                          >
                            清除筛选条件
                          </button>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>

              <!-- 3. 卡片底栏：MATCH 兜底规则 (固定在卡片最底端，4 列严格对齐！) -->
              <div v-if="matchRule" class="bg-[#F8F9FA] border-t-2 border-black/[0.06] shrink-0 font-normal">
                <table class="w-full text-left text-xs table-fixed">
                  <tbody>
                    <tr class="hover:bg-neutral-100/60 transition-colors">
                      <!-- 顺序列：兜底标记 -->
                      <td class="py-2.5 px-3.5 w-20 align-middle">
                        <div class="flex items-center gap-1.5">
                          <span class="text-xs" title="兜底规则，始终固定在列表末尾">🛡️</span>
                          <span class="text-[10px] font-bold text-neutral-600 bg-black/[0.05] px-1.5 py-0.5 rounded">兜底</span>
                        </div>
                      </td>

                      <!-- 匹配条件列：说明 -->
                      <td class="py-2.5 px-3 align-middle">
                        <div class="flex items-center gap-2 flex-wrap">
                          <span class="text-xs font-bold text-neutral-900">其他未匹配流量</span>
                          <span class="text-neutral-300">·</span>
                          <span class="px-1.5 py-0.5 rounded text-[10px] font-mono bg-white border border-black/[0.08] text-neutral-600 font-semibold" title="Clash MATCH 兜底规则">
                            MATCH
                          </span>
                        </div>
                        <p class="text-[11px] text-neutral-400 mt-0.5 truncate" :title="matchRule.description || '未命中上方任何具体规则的访问请求将强制交由此目标处理'">
                          {{ matchRule.description || '漏网请求默认走此目标' }}
                        </p>
                      </td>

                      <!-- 处理目标列：对齐上方的处理目标列 -->
                      <td class="py-2.5 px-3 w-48 align-middle">
                        <button
                          v-if="clientConfig.proxyGroups.some(g => g.name === matchRule.target)"
                          type="button"
                          @click="navigateToProxyGroup(matchRule.target)"
                          class="inline-flex items-center gap-1.5 text-xs font-semibold text-neutral-800 hover:text-ios-blue transition-colors group"
                          title="点击查看该代理组"
                        >
                          <span :class="['w-1.5 h-1.5 rounded-full shrink-0', getActionDotClass(matchRule.target)]"></span>
                          <span class="group-hover:underline">{{ formatActionLabel(matchRule.target) }}</span>
                          <span class="text-neutral-300 group-hover:text-ios-blue text-[10px]">↗</span>
                        </button>
                        <span v-else class="inline-flex items-center gap-1.5 text-xs font-semibold text-neutral-800">
                          <span :class="['w-1.5 h-1.5 rounded-full shrink-0', getActionDotClass(matchRule.target)]"></span>
                          <span>{{ formatActionLabel(matchRule.target) }}</span>
                        </span>
                      </td>

                      <!-- 操作列：对齐上方的操作列 -->
                      <td class="py-2.5 px-3.5 w-36 text-right align-middle">
                        <div class="flex items-center justify-end gap-2">
                          <button
                            type="button"
                            @click.stop="toggleRule(matchRule)"
                            class="relative inline-flex h-4 w-7 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none"
                            :class="matchRule.enabled ? 'bg-[#34C759]' : 'bg-neutral-200'"
                            :title="matchRule.enabled ? '兜底规则已启用' : '兜底规则已停用'"
                          >
                            <span
                              class="pointer-events-none inline-block h-3 w-3 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out"
                              :class="matchRule.enabled ? 'translate-x-3' : 'translate-x-0'"
                            />
                          </button>
                          <button
                            type="button"
                            @click="openEditDrawer(matchRule)"
                            class="text-xs font-medium text-ios-blue hover:text-blue-700 hover:underline px-1 py-0.5"
                          >
                            编辑
                          </button>
                        </div>
                      </td>
                    </tr>
                  </tbody>
                </table>
              </div>
            </div>
          </div>

          <!-- 模块 4：DNS 设置 (DNS) -->
          <div v-else-if="activeModule === 'dns'" class="space-y-6">
            <div class="border-b border-black/[0.06] pb-3">
              <h3 class="text-base font-bold text-neutral-900">DNS 设置 (DNS Resolvers)</h3>
              <p class="text-xs text-neutral-500 mt-0.5">dns 控制域名如何解析，例如使用哪些 DNS 服务器、是否启用 Fake-IP，以及是否为不同域名指定不同解析方式。</p>
            </div>

            <!-- DNS 开关与增强模式 -->
            <div class="panel-card p-5 space-y-4 border border-black/[0.06]">
              <div class="flex items-center justify-between">
                <div>
                  <h4 class="text-xs font-bold text-neutral-900">启用客户端内部 DNS 解析 (dns.enable)</h4>
                  <p class="text-[11px] text-neutral-400 mt-0.5">由 Clash / Mihomo 内部接管 DNS 查询，避免本地 DNS 污染与泄露。</p>
                </div>
                <label class="relative inline-flex items-center cursor-pointer">
                  <input
                    type="checkbox"
                    v-model="clientConfig.extra.dns.enable"
                    class="sr-only peer"
                    @change="markDirty"
                  />
                  <div class="w-11 h-6 bg-neutral-200 peer-focus:outline-none rounded-full peer peer-checked:after:translate-x-full peer-checked:after:border-white after:content-[''] after:absolute after:top-[2px] after:left-[2px] after:bg-white after:border-gray-300 after:border after:rounded-full after:h-5 after:w-5 after:transition-all peer-checked:bg-ios-blue"></div>
                </label>
              </div>

              <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-3 border-t border-black/[0.04]">
                <div>
                  <label class="block text-xs font-semibold text-neutral-700 mb-1">增强模式 (Enhanced Mode)</label>
                  <AppSelect
                    v-model="clientConfig.extra.dns.enhancedMode"
                    :options="enhancedModeOptions"
                    @change="markDirty"
                  />
                </div>
              </div>
            </div>

            <!-- 上游 DNS 服务器列表 (Nameservers) -->
            <div class="panel-card p-5 space-y-4 border border-black/[0.06]">
              <div class="flex items-center justify-between">
                <div>
                  <h4 class="text-xs font-bold text-neutral-900">常规上游 DNS 服务器 (Nameservers)</h4>
                  <p class="text-[11px] text-neutral-400 mt-0.5">首选国内与直连查询服务器，支持 UDP、TCP 及 DoH 地址。</p>
                </div>
              </div>

              <div class="flex items-center gap-2">
                <input
                  v-model="dnsNameserverInput"
                  type="text"
                  placeholder="例如: 223.5.5.5 或 https://dns.alidns.com/dns-query"
                  class="form-input-base text-xs font-mono flex-1"
                  @keyup.enter="addDnsNameserver"
                />
                <button
                  type="button"
                  @click="addDnsNameserver"
                  class="ios-btn-secondary text-xs px-3 py-2 font-medium"
                >
                  添加服务器
                </button>
              </div>

              <!-- 已添加列表 -->
              <div class="space-y-1.5 max-h-40 overflow-y-auto">
                <div
                  v-for="(ns, idx) in clientConfig.extra.dns.nameservers"
                  :key="idx"
                  class="flex items-center justify-between px-3 py-1.5 rounded-xl bg-black/[0.02] border border-black/[0.04] text-xs font-mono"
                >
                  <span class="text-neutral-800">{{ ns }}</span>
                  <button
                    type="button"
                    @click="removeDnsNameserver(idx)"
                    class="text-neutral-400 hover:text-red-500 p-0.5"
                    title="移除"
                  >
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
                <div v-if="!clientConfig.extra.dns.nameservers || clientConfig.extra.dns.nameservers.length === 0" class="text-[11px] text-neutral-400 py-1">
                  暂未配置，将默认回退到客户端系统 DNS。
                </div>
              </div>

              <!-- 快捷公共 DNS -->
              <div class="flex flex-wrap items-center gap-1.5 pt-1 text-[11px] text-neutral-400 border-t border-black/[0.04]">
                <span>快捷填充:</span>
                <button
                  type="button"
                  @click="quickAddDns('223.5.5.5')"
                  class="px-2 py-0.5 rounded bg-black/[0.04] hover:bg-black/[0.08] text-neutral-700 font-mono"
                >
                  + 阿里 DNS (223.5.5.5)
                </button>
                <button
                  type="button"
                  @click="quickAddDns('119.29.29.29')"
                  class="px-2 py-0.5 rounded bg-black/[0.04] hover:bg-black/[0.08] text-neutral-700 font-mono"
                >
                  + 腾讯 DNSPod (119.29.29.29)
                </button>
                <button
                  type="button"
                  @click="quickAddDns('https://dns.alidns.com/dns-query')"
                  class="px-2 py-0.5 rounded bg-black/[0.04] hover:bg-black/[0.08] text-neutral-700 font-mono"
                >
                  + 阿里 DoH
                </button>
              </div>
            </div>

            <!-- Fallback 回退服务器列表 -->
            <div class="panel-card p-5 space-y-4 border border-black/[0.06]">
              <div>
                <h4 class="text-xs font-bold text-neutral-900">回退防污染 DNS 服务器 (Fallback)</h4>
                <p class="text-[11px] text-neutral-400 mt-0.5">当常规解析判定为境外或受污染 IP 时作为权威比对与回退解析源。</p>
              </div>

              <div class="flex items-center gap-2">
                <input
                  v-model="dnsFallbackInput"
                  type="text"
                  placeholder="例如: https://1.1.1.1/dns-query 或 8.8.4.4"
                  class="form-input-base text-xs font-mono flex-1"
                  @keyup.enter="addDnsFallback"
                />
                <button
                  type="button"
                  @click="addDnsFallback"
                  class="ios-btn-secondary text-xs px-3 py-2 font-medium"
                >
                  添加回退源
                </button>
              </div>

              <div class="space-y-1.5 max-h-40 overflow-y-auto">
                <div
                  v-for="(fb, idx) in clientConfig.extra.dns.fallback"
                  :key="idx"
                  class="flex items-center justify-between px-3 py-1.5 rounded-xl bg-black/[0.02] border border-black/[0.04] text-xs font-mono"
                >
                  <span class="text-neutral-800">{{ fb }}</span>
                  <button
                    type="button"
                    @click="removeDnsFallback(idx)"
                    class="text-neutral-400 hover:text-red-500 p-0.5"
                    title="移除"
                  >
                    <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M6 18L18 6M6 6l12 12" />
                    </svg>
                  </button>
                </div>
              </div>
            </div>
          </div>

          <!-- 模块 5：规则集 (Rule Providers / 统一规则集) -->
          <div v-else-if="activeModule === 'providers'" class="space-y-4">
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-black/[0.06] pb-3">
              <div>
                <h3 class="text-base font-bold text-neutral-900">规则集 (RULE-PROVIDERS)</h3>
                <p class="text-xs text-neutral-500 mt-0.5">统一维护可复用的域名、域名后缀、IP 与 CIDR 集合。只定义匹配目标，不包含转发动作，供客户端策略与节点策略共同引用。</p>
              </div>
              <div class="flex items-center gap-2 shrink-0">
                <button
                  type="button"
                  @click="openCreateService"
                  class="ios-btn-primary text-xs px-3 py-1.5 inline-flex items-center gap-1.5"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>创建规则集</span>
                </button>
                <button
                  type="button"
                  @click="openAddProviderDrawer"
                  class="ios-btn-secondary text-xs px-3 py-1.5 inline-flex items-center gap-1.5"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>新增外部规则集</span>
                </button>
              </div>
            </div>

            <!-- 规则集定位与两端更新生效机制提示 (与图2保持一致) -->
            <div class="p-3.5 bg-blue-50/60 rounded-2xl border border-blue-100 text-xs text-neutral-600 leading-relaxed">
              <div class="flex items-center gap-2 font-bold text-neutral-800 mb-1">
                <svg class="w-4 h-4 text-ios-blue shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
                </svg>
                <span>规则集定位与两端更新生效机制</span>
              </div>
              <div class="text-[11px] text-neutral-500 space-y-1">
                <div>• <strong>纯目标定义</strong>：规则集仅定义域名、IP 或网段的匹配目标（如 OpenAI、常见广告黑名单、内网私有地址），<strong>不包含任何转发或阻断动作</strong>。动作在“客户端策略”与“节点策略”中自由绑定。</div>
                <div>• <strong>两端更新方式（保存不承诺立即生效）</strong>：<br />
                  &nbsp;&nbsp;1. <strong>客户端端</strong>：Clash / Mihomo 需等待其 rule-providers 的定时拉取周期（默认 24h）或在客户端中手动点击“刷新规则集”；<br />
                  &nbsp;&nbsp;2. <strong>节点端</strong>：保存后规则集版本号递增，需通过 Agent 发布配置草稿后，目标服务器才会加载最新集合。
                </div>
              </div>
            </div>

            <!-- 系统统一服务规则集表格 (完全同步图2结构与字段) -->
            <div class="overflow-x-auto rounded-2xl border border-black/[0.06] bg-white">
              <table class="w-full text-left border-collapse text-xs min-w-[760px]">
                <thead>
                  <tr class="border-b border-black/[0.06] bg-neutral-50/75 text-neutral-500 font-semibold text-[11px]">
                    <th class="py-3 px-4">规则集名称</th>
                    <th class="py-3 px-3">来源 / 版本</th>
                    <th class="py-3 px-3">匹配规模 (域名 / IP)</th>
                    <th class="py-3 px-3">CLASH RULE-PROVIDER 引用</th>
                    <th class="py-3 px-3">状态</th>
                    <th class="py-3 px-3">更新时间</th>
                    <th class="py-3 px-4 text-right">操作</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-black/[0.04]">
                  <tr v-for="set in serviceSets" :key="set.id" class="hover:bg-black/[0.015] transition-colors">
                    <!-- 1. 规则集名称 -->
                    <td class="py-3.5 px-4">
                      <div class="flex items-center gap-2">
                        <span class="font-bold text-neutral-900 text-xs">{{ set.name }}</span>
                        <span v-if="set.builtin" class="px-1.5 py-0.2 rounded text-[10px] font-bold bg-blue-50 text-ios-blue uppercase">
                          内置
                        </span>
                      </div>
                      <p class="text-[11px] text-neutral-400 mt-0.5 line-clamp-1 max-w-xs">{{ set.description || '内置基础域名集合' }}</p>
                    </td>

                    <!-- 2. 来源 / 版本 -->
                    <td class="py-3.5 px-3">
                      <span class="font-mono font-bold text-neutral-700 bg-neutral-100 px-2 py-0.5 rounded-md text-[11px]">
                        v{{ set.version || 1 }}
                      </span>
                    </td>

                    <!-- 3. 匹配规模 -->
                    <td class="py-3.5 px-3 font-mono text-neutral-600">
                      <span>{{ (set.domains?.length || 0) + (set.suffixes?.length || 0) }} 域名</span>
                      <span class="text-neutral-300 mx-1">/</span>
                      <span>{{ set.cidrs?.length || 0 }} IP网段</span>
                    </td>

                    <!-- 4. CLASH RULE-PROVIDER 引用 -->
                    <td class="py-3.5 px-3">
                      <button
                        type="button"
                        @click="openProviderModal(set)"
                        class="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-lg bg-neutral-100 hover:bg-neutral-200 text-neutral-700 font-mono text-[11px] transition-colors"
                        title="查看 rule-providers 配置片段"
                      >
                        <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z" />
                        </svg>
                        <span>Provider 配置</span>
                      </button>
                    </td>

                    <!-- 5. 状态 -->
                    <td class="py-3.5 px-3">
                      <span class="inline-flex items-center gap-1.5 text-[11px] font-medium" :class="set.enabled !== false ? 'text-emerald-600' : 'text-neutral-400'">
                        <span class="w-1.5 h-1.5 rounded-full" :class="set.enabled !== false ? 'bg-emerald-500' : 'bg-neutral-400'"></span>
                        <span>{{ set.enabled !== false ? '正常启用' : '已停用' }}</span>
                      </span>
                    </td>

                    <!-- 6. 更新时间 -->
                    <td class="py-3.5 px-3 text-[11px] font-mono text-neutral-400">
                      {{ formatTime(set.updatedAt) }}
                    </td>

                    <!-- 7. 操作 -->
                    <td class="py-3.5 px-4 text-right space-x-1.5 whitespace-nowrap">
                      <button
                        type="button"
                        @click="quickCreateRuleForService(set)"
                        class="ios-action-btn-primary"
                        title="一键为此规则集创建分流规则"
                      >
                        引用
                      </button>
                      <button
                        type="button"
                        @click="openEditService(set)"
                        class="ios-action-btn-secondary"
                      >
                        编辑
                      </button>
                      <button
                        type="button"
                        @click="cloneService(set)"
                        class="ios-action-btn-secondary"
                      >
                        克隆
                      </button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <!-- 表格底部总数 -->
            <div class="flex items-center justify-between text-xs text-neutral-400 px-1">
              <span>共 {{ serviceSets.length }} 条记录</span>
            </div>

            <!-- 外部订阅规则集 (External Rule Providers) -->
            <div class="space-y-3 pt-3 border-t border-black/[0.06]">
              <div class="flex items-center justify-between">
                <span class="text-xs font-bold text-neutral-800">外部订阅规则集 (External Rule Providers)</span>
                <span class="text-[11px] text-neutral-400">客户端定期自远端更新</span>
              </div>
              <div v-if="clientConfig.extra.providers.length > 0" class="space-y-2">
                <div
                  v-for="(prov, idx) in clientConfig.extra.providers"
                  :key="prov.name"
                  class="panel-card p-3.5 flex items-center justify-between border border-black/[0.06]"
                >
                  <div class="min-w-0 pr-3">
                    <div class="flex items-center gap-2">
                      <span class="text-xs font-bold text-neutral-900 font-mono">{{ prov.name }}</span>
                      <span class="text-[10px] px-1.5 py-0.2 rounded bg-black/[0.04] text-neutral-600 font-mono">{{ prov.behavior }}</span>
                      <span class="text-[10px] px-1.5 py-0.2 rounded bg-black/[0.04] text-neutral-600 font-mono">{{ prov.format }}</span>
                    </div>
                    <p class="text-[11px] text-neutral-400 truncate mt-0.5 font-mono">{{ prov.url }}</p>
                  </div>
                  <div class="flex items-center gap-2 shrink-0">
                    <span class="text-[11px] text-neutral-400 font-mono">{{ prov.intervalSeconds }}s</span>
                    <button
                      type="button"
                      @click="removeProvider(idx)"
                      class="text-neutral-400 hover:text-red-500 p-1"
                      title="移除此规则集"
                    >
                      <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
                      </svg>
                    </button>
                  </div>
                </div>
              </div>
              <div v-else class="panel-card p-5 text-center text-xs text-neutral-400 border border-dashed border-black/[0.1]">
                暂未配置外部 Rule Provider，可点击右上角「新增外部规则集」添加。
              </div>
            </div>
          </div>

          <!-- 模块 6：YAML 实时预览 (YAML Preview) -->
          <div v-else-if="activeModule === 'preview'" class="space-y-4">
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-black/[0.06] pb-3">
              <div>
                <h3 class="text-base font-bold text-neutral-900">订阅实际生成的配置 YAML 预览</h3>
                <p class="text-xs text-neutral-500 mt-0.5">预览根据当前客户端策略生成的完整订阅配置，支持一键复制完整配置。</p>
              </div>
              <div class="flex items-center gap-2">
                <button
                  type="button"
                  @click="copyConfig"
                  :disabled="!previewBody"
                  class="ios-btn-secondary text-xs px-3 py-1.5 inline-flex items-center gap-1.5"
                >
                  <svg v-if="!copied" class="w-3.5 h-3.5 text-neutral-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 5H6a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2v-1M8 5a2 2 0 002 2h2a2 2 0 002-2M8 5a2 2 0 012-2h2a2 2 0 012 2m0 0h2a2 2 0 012 2v3m2 4H10m0 0l3-3m-3 3l3 3" />
                  </svg>
                  <svg v-else class="w-3.5 h-3.5 text-emerald-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
                  </svg>
                  <span>{{ copied ? '已复制到剪贴板' : '复制完整 YAML' }}</span>
                </button>
              </div>
            </div>

            <!-- 格式选择器与说明 -->
            <div class="flex flex-wrap items-center justify-between gap-3 text-xs bg-black/[0.015] p-3 rounded-xl border border-black/[0.05]">
              <div class="flex items-center gap-2">
                <span class="font-medium text-neutral-600 shrink-0">客户端格式:</span>
                <AppSelect
                  v-model="previewFormat"
                  :options="previewFormatOptions"
                  size="sm"
                  class="w-44"
                  @change="loadPreview"
                />
              </div>
              <div class="text-[11px] text-neutral-400">
                根据当前客户端策略实时合成，用于客户端订阅配置测试与复制
              </div>
            </div>

            <!-- 渲染代码预览框 -->
            <div class="relative rounded-2xl bg-neutral-900 text-neutral-100 p-4 font-mono text-xs overflow-x-auto max-h-[460px] leading-relaxed shadow-inner">
              <pre>{{ previewBody || '# 正在生成客户端完整订阅 YAML 配置预览…' }}</pre>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>

    <!-- Rule Edit / Create Drawer -->
    <FormDrawer
      v-model="drawerVisible"
      :title="editingRule ? '编辑客户端分流规则' : (ruleTargetGroupContext ? ('为「' + ruleTargetGroupContext + '」添加分流规则') : '新建客户端分流规则')"
      :description="ruleTargetGroupContext ? ('设定匹配条件，当请求命中本规则时，流量将直接派发给【' + ruleTargetGroupContext + '】出口组处理。') : '配置 Clash / Mihomo 客户端分流规则，优先级数值越小越优先匹配。'"
      width="2xl"
      height="xl"
      :loading="saving"
      @confirm="saveRule"
    >
      <div class="space-y-4 text-sm">
        <!-- 顶部流向指示卡 (Flow Preview) -->
        <div class="p-3 bg-blue-50/70 rounded-2xl border border-blue-200/80 flex items-center justify-between gap-3 text-xs">
          <div class="flex items-center gap-2.5 min-w-0">
            <div class="w-8 h-8 rounded-xl bg-blue-100/80 text-ios-blue flex items-center justify-center shrink-0 font-bold text-sm">
              🌐
            </div>
            <div class="min-w-0">
              <div class="font-bold text-neutral-900 truncate">
                匹配目标: <span class="text-ios-blue font-mono font-semibold">{{ form.payload || (form.type === 'MATCH' ? '全部流量兜底' : '（请输入目标值）') }}</span>
              </div>
              <div class="text-[11px] text-neutral-500 mt-0.5 flex items-center gap-1.5">
                <span>类型: <strong class="text-neutral-700 font-mono">{{ form.type }}</strong></span>
                <span class="text-neutral-300">·</span>
                <span>优先级: <strong class="text-neutral-700 font-mono">#{{ form.priority }}</strong></span>
              </div>
            </div>
          </div>

          <div class="flex items-center gap-1.5 shrink-0">
            <span class="text-neutral-400 font-bold text-xs">──▶</span>
            <span class="px-2.5 py-1 rounded-xl bg-white border border-blue-200/80 text-ios-blue font-bold shadow-2xs text-xs truncate max-w-[140px]">
              🎯 {{ form.target }}
            </span>
          </div>
        </div>

        <!-- 规则集快捷填入 (展示系统已有的规则集) -->
        <div v-if="!editingRule && availableServiceRulePresets.length > 0" class="space-y-1.5">
          <div class="flex items-center justify-between text-xs">
            <span class="font-semibold text-neutral-700 flex items-center gap-1">
              <span>📦</span> <span>可用规则集 (点击一键填入分流):</span>
            </span>
            <span class="text-[11px] text-neutral-400">共 {{ availableServiceRulePresets.length }} 个规则集</span>
          </div>
          <div class="flex flex-wrap items-center gap-1.5">
            <button
              v-for="preset in availableServiceRulePresets"
              :key="preset.name + preset.payload"
              type="button"
              @click="applyRulePreset(preset)"
              class="px-2.5 py-1 rounded-lg text-xs font-medium border transition-all text-left flex items-center gap-1.5 hover:border-ios-blue"
              :class="form.payload === preset.payload && form.type === preset.type
                ? 'bg-blue-50 text-ios-blue border-ios-blue ring-1 ring-ios-blue/20 font-bold'
                : 'bg-black/[0.02] text-neutral-700 border-black/[0.06] hover:bg-black/[0.04]'"
              :title="preset.desc"
            >
              <span class="text-neutral-500">📦</span>
              <span>{{ preset.name }}</span>
              <span v-if="preset.count" class="text-[10px] text-neutral-400 font-mono">({{ preset.count }})</span>
            </button>
          </div>
        </div>

        <!-- 规则匹配类型 (按业务清晰分组) -->
        <div>
          <label class="block text-xs font-semibold text-neutral-700 mb-1">
            规则匹配类型 (Match Type)
          </label>
          <AppSelect
            v-model="form.type"
            :options="ruleTypeOptions"
            @change="onRuleTypeChange"
          />
        </div>

        <!-- 匹配内容 (Payload) 输入区 -->
        <div v-if="form.type === 'RULE-SET'">
          <label class="block text-xs font-semibold text-neutral-700 mb-1">
            统一规则集引用 (Rule Provider)
          </label>
          <AppSelect
            v-model="form.payload"
            :options="ruleSetPayloadOptions"
          />

          <!-- 规则集详情展开预览卡片 -->
          <div v-if="selectedRuleSetDetail" class="p-3 bg-purple-50/50 rounded-xl border border-purple-200/60 text-xs space-y-1.5 mt-2">
            <div class="flex items-center justify-between">
              <div class="flex items-center gap-1.5">
                <span class="font-bold text-neutral-900">{{ selectedRuleSetDetail.name }}</span>
                <span v-if="selectedRuleSetDetail.builtin" class="px-1.5 py-0.2 rounded text-[10px] font-bold bg-blue-50 text-ios-blue uppercase">内置</span>
              </div>
              <span class="text-[11px] font-mono font-bold text-purple-700 bg-purple-100/80 px-2 py-0.5 rounded">v{{ selectedRuleSetDetail.version || 1 }}</span>
            </div>
            <p class="text-[11px] text-neutral-500">{{ selectedRuleSetDetail.description || '面板统一维护的服务分流规则集合' }}</p>
            <div class="text-[11px] text-neutral-600 flex items-center gap-2 font-mono">
              <span>匹配规模: {{ (selectedRuleSetDetail.domains?.length || 0) + (selectedRuleSetDetail.suffixes?.length || 0) }} 域名</span>
              <span class="text-neutral-300">/</span>
              <span>{{ selectedRuleSetDetail.cidrs?.length || 0 }} IP 网段</span>
            </div>
            <div v-if="selectedRuleSetDetail.domains?.length || selectedRuleSetDetail.suffixes?.length" class="flex flex-wrap gap-1 pt-1">
              <span
                v-for="d in [...(selectedRuleSetDetail.domains || []), ...(selectedRuleSetDetail.suffixes || [])].slice(0, 8)"
                :key="d"
                class="px-1.5 py-0.5 rounded bg-white border border-purple-200/80 text-[10px] font-mono text-neutral-600"
              >
                {{ d }}
              </span>
              <span
                v-if="((selectedRuleSetDetail.domains?.length || 0) + (selectedRuleSetDetail.suffixes?.length || 0)) > 8"
                class="text-[10px] text-neutral-400 self-center font-mono"
              >
                +{{ ((selectedRuleSetDetail.domains?.length || 0) + (selectedRuleSetDetail.suffixes?.length || 0)) - 8 }} 更多...
              </span>
            </div>
          </div>

          <p class="text-[11px] text-neutral-400 mt-1">
            选择规则集后，命中此规则集内全部域名与 IP 的流量均将派发给指定出口目标。
          </p>
        </div>

        <div v-else-if="form.type !== 'MATCH'">
          <label class="block text-xs font-semibold text-neutral-700 mb-1">
            匹配内容 (Payload)
            <span v-if="['DOMAIN-SUFFIX', 'DOMAIN'].includes(form.type)" class="text-[11px] font-normal text-neutral-400 ml-1">
              (支持输入单个或多个域名，多个用逗号/换行分隔)
            </span>
          </label>

          <textarea
            v-if="['DOMAIN-SUFFIX', 'DOMAIN'].includes(form.type)"
            v-model="form.payload"
            rows="2"
            :placeholder="form.type === 'DOMAIN-SUFFIX' ? '例如: openai.com, chatgpt.com (多个用逗号或换行分隔)' : '例如: google.com, youtube.com'"
            class="form-input-base text-xs font-mono py-2"
            required
          ></textarea>
          <input
            v-else
            v-model="form.payload"
            :placeholder="getPayloadPlaceholder(form.type)"
            class="form-input-base text-xs font-mono"
            required
          />

          <!-- 批量检测提示 -->
          <div v-if="parsedPayloadItemsCount > 1" class="p-2 mt-1.5 bg-blue-50/80 rounded-xl border border-blue-200 text-xs text-ios-blue flex items-center gap-1.5">
            <span>💡</span>
            <span>已识别出 <strong>{{ parsedPayloadItemsCount }}</strong> 个目标，保存时将自动批量创建 {{ parsedPayloadItemsCount }} 条分流规则。</span>
          </div>

          <p v-else class="text-[11px] text-neutral-400 mt-1">
            {{ getPayloadHelpText(form.type, form.payload) }}
          </p>
        </div>

        <div v-else class="p-3 bg-amber-50/70 border border-amber-200/80 rounded-xl text-xs text-amber-800">
          <p class="font-bold">兜底规则提示</p>
          <p class="mt-0.5 text-[11px]">MATCH 类型规则无需填写目标值，用于拦截上述所有规则均未命中的流量，始终排在列表末尾生效。</p>
        </div>

        <!-- 命中后执行动作 (Target) -->
        <div>
          <label class="block text-xs font-semibold text-neutral-700 mb-1">
            命中后派发出向 (Target)
          </label>
          <AppSelect
            v-model="form.target"
            :options="ruleTargetOptions"
          />
        </div>

        <!-- 备注说明 -->
        <div>
          <label class="block text-xs font-semibold text-neutral-700 mb-1">备注说明 (可选)</label>
          <input
            v-model="form.description"
            placeholder="例如: AI 常用智能助手服务分流"
            maxlength="60"
            class="form-input-base text-xs"
          />
        </div>

        <!-- 高级设置折叠面板 -->
        <div class="pt-2 border-t border-black/[0.05]">
          <button
            type="button"
            @click="showAdvancedRuleSettings = !showAdvancedRuleSettings"
            class="text-xs text-neutral-500 hover:text-neutral-800 flex items-center gap-1 font-medium"
          >
            <span>{{ showAdvancedRuleSettings ? '收起高级设置' : '展开高级设置 (优先级、no-resolve、启用状态)' }}</span>
            <svg class="w-3.5 h-3.5 transition-transform" :class="{ 'rotate-180': showAdvancedRuleSettings }" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          <div v-show="showAdvancedRuleSettings" class="mt-3 p-3 bg-neutral-50/70 rounded-xl border border-black/[0.04] space-y-3">
            <div class="grid grid-cols-2 gap-3">
              <div>
                <label class="block text-xs font-semibold text-neutral-700 mb-1">
                  优先级编号 (Priority)
                </label>
                <input
                  v-model.number="form.priority"
                  type="number"
                  min="1"
                  max="9999"
                  class="form-input-base text-xs font-mono"
                  required
                />
                <span class="text-[10px] text-neutral-400 mt-0.5 block">数值越小越先匹配，兜底在末尾</span>
              </div>

              <div>
                <label class="block text-xs font-semibold text-neutral-700 mb-1">
                  启用状态
                </label>
                <div class="pt-1.5">
                  <label class="inline-flex items-center gap-2 text-xs text-neutral-700 cursor-pointer">
                    <input v-model="form.enabled" type="checkbox" />
                    <span>启用此条规则</span>
                  </label>
                </div>
              </div>
            </div>

            <div v-if="['IP-CIDR', 'IP-CIDR6', 'GEOIP'].includes(form.type)" class="pt-2 border-t border-black/[0.04]">
              <label class="inline-flex items-center gap-2 text-xs text-neutral-700 cursor-pointer">
                <input v-model="form.noResolve" type="checkbox" />
                <span>no-resolve (跳过域名解析，仅对直接发往 IP 的请求生效)</span>
              </label>
            </div>
          </div>
        </div>
      </div>
    </FormDrawer>


    <!-- Rule-Provider Dialog (同步自图2) -->
    <AppDialog
      v-model="providerModalVisible"
      :title="`规则集【${selectedService?.name || ''}】的 Clash / Mihomo 引用`"
      :show-footer="false"
      class="max-w-2xl"
    >
      <div class="space-y-4">
        <p class="text-xs text-neutral-500 leading-relaxed">
          在「客户端策略」中用 <code class="font-mono text-ios-blue">RULE-SET</code> 选择此规则集后，用户的 Clash / mihomo 订阅会自动带上对应的 rule-provider，无需手工配置。地址以用户自己的订阅 Token 鉴权，下列信息供手工配置时参考：
        </p>

        <div class="relative">
          <pre class="bg-neutral-900 text-neutral-100 p-4 rounded-2xl text-xs font-mono overflow-x-auto leading-relaxed">{{ providerYamlSnippet }}</pre>
          <button
            type="button"
            @click="copySnippet"
            class="absolute top-3 right-3 px-3 py-1.5 rounded-lg bg-white/10 hover:bg-white/20 text-white text-xs font-medium backdrop-blur-md transition-colors"
          >
            {{ snippetCopied ? '已复制！' : '复制代码' }}
          </button>
        </div>

        <div class="space-y-1.5 pt-1">
          <span class="text-xs font-medium text-neutral-600">直接 URL 地址:</span>
          <div class="flex items-center gap-2">
            <input
              readonly
              :value="providerDirectUrl"
              class="form-input-base text-xs font-mono text-neutral-700 bg-neutral-50"
            />
            <button type="button" @click="copyUrl" class="ios-btn-secondary shrink-0 text-xs py-2">
              复制 URL
            </button>
          </div>
        </div>

        <div class="flex justify-end pt-2">
          <button type="button" @click="providerModalVisible = false" class="ios-btn-secondary">
            关闭
          </button>
        </div>
      </div>
    </AppDialog>

    <!-- Form Drawer: Edit / Create Service Rule Set -->
    <FormDrawer
      v-model="serviceDrawerVisible"
      :title="editingService ? `编辑规则集 · v${editingService.version || 1}` : '创建自定义规则集'"
      description="统一维护目标域名、后缀与 IP/CIDR。保存后版本号自动增加，客户端与节点端均可引用。"
      :confirm-text="editingService ? '保存修改' : '创建规则集'"
      :loading="savingService"
      @confirm="saveService"
    >
      <div class="space-y-4 text-sm">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">规则集名称</span>
          <input
            v-model="serviceForm.name"
            placeholder="例如: OpenAI、常见流媒体"
            class="form-input-base mt-1.5"
            required
          />
        </label>

        <label class="block">
          <span class="text-xs font-medium text-neutral-600">描述说明 (可选)</span>
          <textarea
            v-model="serviceForm.description"
            rows="2"
            maxlength="300"
            placeholder="说明该规则集的收录范围与用途…"
            class="form-input-base mt-1.5"
          ></textarea>
        </label>

        <div>
          <span class="text-xs font-medium text-neutral-600">完整域名 (每行一个精确域名)</span>
          <textarea
            v-model="serviceDomainsText"
            rows="3"
            placeholder="api.example.com&#10;login.example.com"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
        </div>

        <div>
          <span class="text-xs font-medium text-neutral-600">域名后缀 (每行一个，子域名自动命中)</span>
          <textarea
            v-model="serviceSuffixesText"
            rows="3"
            placeholder="example.com&#10;openai.com"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
        </div>

        <div>
          <span class="text-xs font-medium text-neutral-600">IP 网段 / CIDR (每行一个)</span>
          <textarea
            v-model="serviceCidrsText"
            rows="3"
            placeholder="192.168.1.0/24&#10;10.0.0.0/8"
            class="form-input-base mt-1.5 font-mono text-xs"
          ></textarea>
        </div>

        <label class="flex items-center gap-2 pt-1 cursor-pointer">
          <input type="checkbox" v-model="serviceForm.enabled" class="rounded text-ios-blue focus:ring-ios-blue" />
          <span class="text-xs font-medium text-neutral-700">启用此规则集</span>
        </label>
      </div>
    </FormDrawer>
    <!-- Provider Create Drawer -->
    <FormDrawer
      v-model="providerDrawerVisible"
      title="添加外部规则集提供者"
      description="配置客户端自远端订阅规则集 (rule-provider)。"
      :loading="saving"
      @confirm="addProvider"
    >
      <div class="space-y-4 text-sm">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">规则集标识名称 (Name)</span>
          <input v-model="providerForm.name" placeholder="例如: reject-ad" class="form-input-base mt-1.5" required />
        </label>
        <div class="block">
          <span class="text-xs font-medium text-neutral-600 block mb-1.5">匹配行为 (Behavior)</span>
          <AppSelect v-model="providerForm.behavior" :options="providerBehaviorOptions" />
        </div>
        <div class="block">
          <span class="text-xs font-medium text-neutral-600 block mb-1.5">格式 (Format)</span>
          <AppSelect v-model="providerForm.format" :options="providerFormatOptions" />
        </div>
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">订阅下载 URL</span>
          <input v-model="providerForm.url" placeholder="https://example.com/rules.yaml" class="form-input-base mt-1.5" required />
        </label>
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">更新周期 (秒)</span>
          <input v-model.number="providerForm.intervalSeconds" type="number" min="60" class="form-input-base mt-1.5" />
        </label>
      </div>
    </FormDrawer>

    <!-- Confirm Delete Rule Dialog -->
    <ConfirmDialog
      v-model="confirmDeleteVisible"
      title="删除客户端规则"
      :message="'确定删除此条客户端规则？删除后需保存策略并由用户刷新订阅才会在客户端生效。'"
      is-danger
      @confirm="executeDelete"
    />

    <!-- Confirm Delete Group Dialog -->
    <ConfirmDialog
      v-model="confirmDeleteGroupVisible"
      title="删除策略组"
      :message="deletingGroupPromptMessage || ('确定删除策略组「' + deletingGroupName + '」吗？若已有分流规则指向此策略组，请在保存配置前将其调整为其他目标。')"
      is-danger
      @confirm="executeDeleteGroup"
    />

    <!-- Confirm Delete Policy Dialog -->
    <ConfirmDialog
      v-model="confirmDeletePolicyVisible"
      title="删除策略配置"
      :message="'确定要删除策略配置「' + (deletingPolicy?.name || '') + '」吗？删除后此配置无法恢复。'"
      is-danger
      confirm-text="确认删除"
      @confirm="executeDeletePolicy"
    />

    <!-- Version Info Dialog -->
    <ConfirmDialog
      v-model="showVersionDialog"
      title="策略版本与修订信息"
      :message="'当前修订版本：#' + clientConfig.version + '\n最后更新时间：' + formatDateTime(clientConfig.updatedAt) + '\n最后操作人：' + (clientConfig.updatedBy || 'Owner') + '\n\n每次保存策略配置时，服务端版本号将自动递增。客户端刷新订阅后即刻加载生效最新的版本。'"
      confirmText="知道了"
      :showCancel="false"
      @confirm="showVersionDialog = false"
    />

    <!-- Confirm Discard Unsaved Changes Dialog -->
    <ConfirmDialog
      v-model="confirmDiscardVisible"
      title="放弃未保存的修改"
      message="确定放弃当前页面所有未保存的配置修改吗？页面将还原为最近一次保存时的状态。"
      is-danger
      confirm-text="确认放弃"
      @confirm="executeDiscardChanges"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, h, watch } from 'vue';
import {
  clientPolicyAPI,
  clientPresets,
  policyAPI,
  type ClientPolicyConfig,
  type ClientProxyGroup,
  type ClientRule,
  type ClientRuleType,
  type RulePreset,
  type ServiceSet,
  type ServiceInput
} from '../api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { userAPI, type ProxyUser } from '../../users/api';
import { showToast } from '../../../app/api';
import { APIError, request } from '../../../app/apiClient';
import { authState, formatTime } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import AppSelect, { type SelectOption } from '../../../components/AppSelect.vue';

// 已建好的策略配置接口与持久化 Key
export interface SavedClientPolicy {
  id: string;
  name: string;
  description: string;
  createdAt: string;
  updatedAt: string;
  config: ClientPolicyConfig;
}

const SAVED_POLICIES_KEY = 'qui_client_saved_policies_v2';
const ACTIVE_POLICY_ID_KEY = 'qui_client_active_policy_id_v2';

const savedPolicies = ref<SavedClientPolicy[]>([]);
const activePolicyId = ref<string>('');
const editingPolicyId = ref<string | null>(null);
const policyNameInput = ref('');
const policyDescInput = ref('');
const setAsActiveOnSave = ref(false);
const deletingPolicy = ref<SavedClientPolicy | null>(null);
const confirmDeletePolicyVisible = ref(false);
const showVersionDialog = ref(false);

// 配置弹窗模态页面状态
const configModalVisible = ref(false);
const modalMode = ref<'create' | 'edit'>('edit');
const activeModule = ref<'settings' | 'groups' | 'rules' | 'dns' | 'providers' | 'preview'>('settings');

const modalTitle = computed(() => {
  if (modalMode.value === 'create') {
    return '新建客户端分流策略配置';
  }
  const current = savedPolicies.value.find(p => p.id === editingPolicyId.value);
  return current ? ('编辑策略配置: ' + current.name) : '编辑客户端分流策略配置';
});

// 模块定义列表与 SVG 图标
const SettingsIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z' }),
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M15 12a3 3 0 11-6 0 3 3 0 016 0z' })
  ])
};

const GroupsIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10' })
  ])
};

const RulesIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M9 5H7a2 2 0 00-2 2v12a2 2 0 002 2h10a2 2 0 002-2V7a2 2 0 00-2-2h-2M9 5a2 2 0 002 2h2a2 2 0 002-2M9 5a2 2 0 012-2h2a2 2 0 012 2m-3 7h3m-3 4h3m-6-4h.01M9 16h.01' })
  ])
};

const DnsIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M21 12a9 9 0 01-9 9m9-9a9 9 0 00-9-9m9 9H3m9 9a9 9 0 01-9-9m9 9c1.657 0 3-4.03 3-9s-1.343-9-3-9m0 18c-1.657 0-3-4.03-3-9s1.343-9 3-9m-9 9a9 9 0 019-9' })
  ])
};

const ProvidersIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M4 6h16M4 10h16M4 14h16M4 18h16' })
  ])
};

const PreviewIcon = {
  render: () => h('svg', { fill: 'none', viewBox: '0 0 24 24', stroke: 'currentColor' }, [
    h('path', { strokeLinecap: 'round', strokeLinejoin: 'round', strokeWidth: '2', d: 'M10 20l4-16m4 4l4 4-4 4M6 16l-4-4 4-4' })
  ])
};

const modulesList = [
  {
    id: 'settings' as const,
    name: '基础设置',
    en: 'Settings',
    desc: 'Clash 如何运行 比如监听端口、运行模式、是否允许局域网设备连接等。',
    icon: SettingsIcon
  },
  {
    id: 'groups' as const,
    name: '代理组',
    en: 'proxy-groups',
    desc: 'proxy-groups 把节点组织起来，方便手动选择、自动测速或故障切换。组里也可以包含其他代理组。',
    icon: GroupsIcon
  },
  {
    id: 'rules' as const,
    name: '分流规则',
    en: 'rules',
    desc: 'rules 决定一个请求应该交给哪个节点或代理组，也可以指定直连 DIRECT 或拦截 REJECT。',
    icon: RulesIcon
  },
  {
    id: 'dns' as const,
    name: 'DNS 设置',
    en: 'dns',
    desc: 'dns 控制域名如何解析，例如使用哪些 DNS 服务器、是否启用 Fake-IP，以及是否为不同域名指定不同解析方式。',
    icon: DnsIcon
  },
  {
    id: 'providers' as const,
    name: '规则集',
    en: 'rule-providers',
    desc: '引用规则集里面的内容决定那些域名ip直连，那些走代理等等。',
    icon: ProvidersIcon
  },
  {
    id: 'preview' as const,
    name: 'YAML 预览',
    en: 'preview',
    desc: '订阅实际生成的配置 YAML 预览与诊断警告检查。',
    icon: PreviewIcon
  }
];

function loadSavedPolicies() {
  try {
    const raw = localStorage.getItem(SAVED_POLICIES_KEY);
    if (raw) {
      const parsed = JSON.parse(raw);
      if (Array.isArray(parsed) && parsed.length > 0) {
        savedPolicies.value = parsed;
        const storedActiveId = localStorage.getItem(ACTIVE_POLICY_ID_KEY);
        if (storedActiveId && savedPolicies.value.some(p => p.id === storedActiveId)) {
          activePolicyId.value = storedActiveId;
        } else {
          activePolicyId.value = savedPolicies.value[0].id;
        }
        return;
      }
    }
  } catch (e) {
    console.error('Failed to parse saved client policies from localStorage:', e);
  }

  // 初次加载时，以当前服务端已生效的策略为初始配置卡片
  const defaultPreset = clientPresets.find(p => p.id === clientConfig.value.activePreset);
  const initialName = defaultPreset ? defaultPreset.name : '标准分流策略';
  const initialDesc = defaultPreset ? defaultPreset.desc : '当前下发至客户端的订阅分流策略配置';

  savedPolicies.value = [
    {
      id: 'policy-default',
      name: initialName,
      description: initialDesc,
      createdAt: clientConfig.value.updatedAt || new Date().toISOString(),
      updatedAt: clientConfig.value.updatedAt || new Date().toISOString(),
      config: JSON.parse(JSON.stringify(clientConfig.value))
    }
  ];
  activePolicyId.value = 'policy-default';
  saveSavedPolicies();
}

function saveSavedPolicies() {
  try {
    localStorage.setItem(SAVED_POLICIES_KEY, JSON.stringify(savedPolicies.value));
    localStorage.setItem(ACTIVE_POLICY_ID_KEY, activePolicyId.value);
  } catch (e) {
    console.error('Failed to save client policies to localStorage:', e);
  }
}

function editPolicy(policy: SavedClientPolicy) {
  editingPolicyId.value = policy.id;
  policyNameInput.value = policy.name;
  policyDescInput.value = policy.description;
  setAsActiveOnSave.value = policy.id === activePolicyId.value;
  clientConfig.value = JSON.parse(JSON.stringify(policy.config));
  originalSnapshot.value = JSON.stringify(policy.config);
  isDirty.value = false;
  modalMode.value = 'edit';
  activeModule.value = 'settings';
  configModalVisible.value = true;
}

async function enablePolicy(policy: SavedClientPolicy) {
  if (activePolicyId.value === policy.id) return;
  saving.value = true;
  try {
    clientConfig.value = await clientPolicyAPI.save(policy.config);
    activePolicyId.value = policy.id;
    originalSnapshot.value = JSON.stringify(clientConfig.value);
    isDirty.value = false;

    const idx = savedPolicies.value.findIndex(p => p.id === policy.id);
    if (idx !== -1) {
      savedPolicies.value[idx].updatedAt = clientConfig.value.updatedAt || new Date().toISOString();
      savedPolicies.value[idx].config = JSON.parse(JSON.stringify(clientConfig.value));
      saveSavedPolicies();
    }
    showToast('已启用策略「' + policy.name + '」，已下发至客户端订阅配置');
  } catch (e) {
    showToast('启用策略失败: ' + (e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

function promptDeletePolicy(policy: SavedClientPolicy) {
  if (savedPolicies.value.length <= 1) {
    showToast('至少需要保留一个策略配置，无法删除全部配置', 'warning');
    return;
  }
  if (policy.id === activePolicyId.value) {
    showToast('该配置当前正在生效中，请先启用其他配置后再删除', 'warning');
    return;
  }
  deletingPolicy.value = policy;
  confirmDeletePolicyVisible.value = true;
}

function executeDeletePolicy() {
  if (!deletingPolicy.value) return;
  const name = deletingPolicy.value.name;
  savedPolicies.value = savedPolicies.value.filter(p => p.id !== deletingPolicy.value?.id);
  saveSavedPolicies();
  confirmDeletePolicyVisible.value = false;
  deletingPolicy.value = null;
  showToast('策略配置「' + name + '」已删除');
}

function openNewConfigModal() {
  editingPolicyId.value = null;
  policyNameInput.value = '自定义策略 #' + (savedPolicies.value.length + 1);
  policyDescInput.value = '';
  setAsActiveOnSave.value = false;

  const standard = presetTemplates.value.find(p => p.id === 'standard');
  if (standard) {
    clientConfig.value = {
      version: 0,
      updatedAt: '',
      activePreset: 'standard',
      settings: { ...standard.template.client },
      proxyGroups: standard.template.groups.map(g => ({
        name: g.name,
        type: g.type,
        proxies: g.members.join(', '),
        url: g.testUrl || undefined,
        interval: g.intervalSeconds || undefined
      })),
      rules: standard.template.rules.map(r => ({ ...r, type: r.type as ClientRuleType })),
      extra: { ipv6: false, dns: { enable: false, enhancedMode: 'fake-ip', nameservers: ['223.5.5.5', '119.29.29.29'], fallback: ['https://1.1.1.1/dns-query'] }, providers: [] }
    };
  } else {
    clientConfig.value = emptyConfig();
  }
  originalSnapshot.value = JSON.stringify(clientConfig.value);
  isDirty.value = false;
  modalMode.value = 'create';
  activeModule.value = 'settings';
  configModalVisible.value = true;
}

function handleCloseModal() {
  if (isDirty.value) {
    confirmDiscardVisible.value = true;
  } else {
    configModalVisible.value = false;
  }
}

// 快速应用预设模板到当前策略表单
function applyPresetTemplate(presetId: string) {
  const preset = presetTemplates.value.find(p => p.id === presetId);
  if (!preset) {
    showToast('预设模板尚未加载，请稍候重试', 'error');
    return;
  }
  clientConfig.value.activePreset = presetId;
  clientConfig.value.settings = { ...preset.template.client };
  clientConfig.value.proxyGroups = preset.template.groups.map(group => ({
    name: group.name,
    type: group.type,
    proxies: group.members.join(', '),
    url: group.testUrl || undefined,
    interval: group.intervalSeconds || undefined
  }));
  clientConfig.value.rules = preset.template.rules.map(rule => ({ ...rule, type: rule.type as ClientRuleType }));
  markDirty();
  showToast('已载入「' + preset.name + '」模板拓扑');
}

function emptyConfig(): ClientPolicyConfig {
  return {
    version: 0,
    updatedAt: '',
    activePreset: '',
    settings: { mode: 'rule', mixedPort: 7890, allowLan: true, logLevel: 'info' },
    proxyGroups: [],
    rules: [],
    extra: { ipv6: false, dns: { enable: false, enhancedMode: 'fake-ip', nameservers: ['223.5.5.5'], fallback: [] }, providers: [] }
  };
}

const clientConfig = ref<ClientPolicyConfig>(emptyConfig());
const originalSnapshot = ref<string>('');
const isDirty = ref(false);
const loaded = ref(false);
const saving = ref(false);
const presetTemplates = ref<RulePreset[]>([]);
const serviceSets = ref<ServiceSet[]>([]);
const availableNodes = ref<NodeView[]>([]);
const isOwner = computed(() => authState.currentUser?.role === 'Owner');

// 下拉框统一定义与预设选项 (Unified AppSelect Options)
const enhancedModeOptions: SelectOption[] = [
  {
    value: 'fake-ip',
    label: 'fake-ip',
    badge: '推荐',
    desc: '虚拟 IP 响应 · 快速响应低延迟，推荐大多数场景使用'
  },
  {
    value: 'redir-host',
    label: 'redir-host',
    desc: '真实 IP 解析 · 传统兼容模式，由客户端执行真实 DNS 查询'
  }
];

const allowLanOptions: SelectOption[] = [
  {
    value: true,
    label: '允许局域网 (true)',
    desc: '绑定 0.0.0.0 · 允许同局域网其他设备连接使用此代理'
  },
  {
    value: false,
    label: '仅本机 (false)',
    desc: '仅绑定 127.0.0.1 · 仅限本机网络进程访问，保障私密安全'
  }
];

const logLevelOptions: SelectOption[] = [
  {
    value: 'info',
    label: 'Info',
    badge: '推荐',
    desc: '标准日志 · 兼顾性能与排错信息 (默认推荐)'
  },
  {
    value: 'warning',
    label: 'Warning',
    desc: '仅告警 · 忽略常规运行输出，仅记录异常与告警'
  },
  {
    value: 'error',
    label: 'Error',
    desc: '仅错误 · 仅输出致命或阻断性错误信息'
  },
  {
    value: 'debug',
    label: 'Debug',
    desc: '排错详情 · 输出完整调试链路日志，性能消耗较大'
  },
  {
    value: 'silent',
    label: 'Silent',
    desc: '静默模式 · 完全关闭客户端本地日志输出'
  }
];

const proxyGroupTypeOptions: SelectOption[] = [
  {
    value: 'select',
    label: '手动选择',
    icon: '🎯',
    desc: '用户或客户端在节点列表中手动指定生效节点'
  },
  {
    value: 'url-test',
    label: '自动测速',
    icon: '⚡',
    desc: '周期性对节点并发测速，自动选出延迟最低节点'
  },
  {
    value: 'fallback',
    label: '故障转移',
    icon: '🛡️',
    desc: '按优先级尝试连接，主节点失效时自动顺延后继节点'
  }
];

const providerBehaviorOptions: SelectOption[] = [
  { value: 'domain', label: 'domain', desc: '域名列表 · 快速匹配海量域名' },
  { value: 'ipcidr', label: 'ipcidr', desc: 'IP 网段列表 · 针对 CIDR 地址段' },
  { value: 'classical', label: 'classical', desc: '传统复合规则 · 支持完整语法' }
];

const providerFormatOptions: SelectOption[] = [
  { value: 'yaml', label: 'yaml', desc: '标准 YAML 格式规则清单' },
  { value: 'text', label: 'text', desc: '纯文本每行一条记录' },
  { value: 'mrs', label: 'mrs', badge: 'Mihomo', desc: '二进制预编译规则集 (高解析速度)' }
];

const ruleTypeFilterOptions: SelectOption[] = [
  { value: '', label: '规则类型 ▼' },
  { value: 'DOMAIN', label: '域名类', desc: '包含 DOMAIN, DOMAIN-SUFFIX, DOMAIN-KEYWORD' },
  { value: 'IP-CIDR', label: 'IP 网段', desc: '包含 IP-CIDR, IP-CIDR6' },
  { value: 'RULE-SET', label: '规则集', desc: 'RULE-SET 集合引用' },
  { value: 'GEOIP', label: '地理库', desc: 'GEOIP / GEOSITE 分类库' },
  { value: 'DST-PORT', label: '目标端口', desc: 'DST-PORT 端口匹配' }
];

const ruleTargetFilterOptions = computed<SelectOption[]>(() => [
  { value: '', label: '处理目标 ▼' },
  { value: 'DIRECT', label: '直连 (DIRECT)' },
  { value: 'REJECT', label: '拦截 (REJECT)' },
  ...clientConfig.value.proxyGroups.map(g => ({
    value: g.name,
    label: '组: ' + g.name
  }))
]);

function markDirty() {
  if (!loaded.value) return;
  isDirty.value = JSON.stringify(clientConfig.value) !== originalSnapshot.value;
}

// 混合端口校验
const mixedPortError = computed(() => {
  const p = Number(clientConfig.value.settings.mixedPort);
  if (!p || isNaN(p) || p < 1024 || p > 65535) {
    return '端口必须是 1024 到 65535 之间的整数';
  }
  return '';
});

function formatModeName(mode: string): string {
  switch (mode?.toLowerCase()) {
    case 'rule': return 'Rule (规则分流)';
    case 'global': return 'Global (全局代理)';
    case 'direct': return 'Direct (全局直连)';
    default: return mode || 'Rule';
  }
}

function formatDateTime(iso: string): string {
  if (!iso) return '未保存';
  try {
    const d = new Date(iso);
    if (isNaN(d.getTime())) return iso;
    return d.toLocaleString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    });
  } catch {
    return iso;
  }
}

// 代理组管理（用途分组 + 地区分组分层拓扑架构）
const systemNodeGroups = ref<NodeGroup[]>([]);
const rawEditMode = ref<Record<number, boolean>>({});
const tempCustomMember = ref<Record<number, string>>({});
const deletingGroupName = ref('');
const deletingGroupPromptMessage = ref('');
const confirmDeleteGroupVisible = ref(false);
let deletingGroupIndex = -1;

// 分类与视图状态
const groupCategoryTab = ref<'service' | 'region' | 'all'>('service');
const groupSearchQuery = ref('');
const isRefreshingDelays = ref(false);
const groupExpandedNodes = ref<Record<number, boolean>>({});
const groupKeywordFilters = ref<Record<number, { include: string; exclude: string }>>({});

// 延迟模拟数据
const simulatedLatencyCache = ref<Record<string, number>>({});

function getNodeSimulatedLatency(name: string): number {
  if (simulatedLatencyCache.value[name]) return simulatedLatencyCache.value[name];
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = (hash << 5) - hash + name.charCodeAt(i);
    hash |= 0;
  }
  const base = Math.abs(hash) % 80;
  let lat = 120;
  const n = name.toLowerCase();
  if (n.includes('港') || n.includes('hk') || n.includes('hong')) lat = 32 + (base % 25);
  else if (n.includes('台') || n.includes('tw') || n.includes('taiwan')) lat = 45 + (base % 30);
  else if (n.includes('日') || n.includes('jp') || n.includes('tokyo')) lat = 68 + (base % 35);
  else if (n.includes('新') || n.includes('sg') || n.includes('singapore')) lat = 78 + (base % 40);
  else if (n.includes('韩') || n.includes('kr') || n.includes('seoul')) lat = 55 + (base % 30);
  else if (n.includes('美') || n.includes('us') || n.includes('united')) lat = 148 + (base % 45);
  else if (n.includes('英') || n.includes('uk') || n.includes('德') || n.includes('de')) lat = 175 + (base % 40);
  else lat = 95 + (base % 50);

  simulatedLatencyCache.value[name] = lat;
  return lat;
}

function refreshDelays() {
  isRefreshingDelays.value = true;
  simulatedLatencyCache.value = {};
  setTimeout(() => {
    isRefreshingDelays.value = false;
    showToast('已完成全节点延迟测速刷新', 'success');
  }, 600);
}

function formatExitLabel(name: string): { icon: string; title: string } {
  if (name === 'DIRECT') return { icon: '🚀', title: 'DIRECT (本地直连)' };
  if (name === 'REJECT') return { icon: '🛑', title: 'REJECT (客户端拦截)' };
  if (name === '@all') return { icon: '🌐', title: '全部节点 (@all)' };
  if (name === 'AUTO') return { icon: '⚡', title: 'AUTO (自动测速)' };
  if (name === 'FALLBACK') return { icon: '🛡️', title: 'FALLBACK (故障转移)' };
  if (name === 'PROXY') return { icon: '🎯', title: 'PROXY (自选总控)' };
  if (name.startsWith('@nodeGroup:')) {
    const id = name.replace('@nodeGroup:', '');
    const ng = systemNodeGroups.value.find(g => g.id === id);
    return { icon: '📁', title: ng ? ng.name : ('节点分组 ' + id.slice(0, 6)) };
  }
  return { icon: '', title: name };
}

// 判定组别类型（用途组、地区组、其他）
function getGroupCategory(group: ClientProxyGroup): 'service' | 'region' | 'other' {
  const name = (group.name || '').toLowerCase();
  const members = getGroupMembers(group);

  const regionKeywords = [
    '香港', 'hk', 'hongkong', '🇭🇰',
    '日本', 'jp', 'japan', '🇯🇵',
    '美国', 'us', 'usa', 'united states', '🇺🇸',
    '新加坡', 'sg', 'singapore', '🇸🇬',
    '台湾', 'tw', 'taiwan', '台北', '🇹🇼',
    '韩国', 'kr', 'korea', '首尔', '🇰🇷',
    '英国', 'uk', '🇬🇧',
    '德国', 'de', '🇩🇪',
    '地区', '节点'
  ];

  const serviceKeywords = [
    'ai', 'openai', 'chatgpt', 'claude',
    'youtube', 'yt', 'bilibili',
    'telegram', 'tg',
    'netflix', 'disney', 'media', '流媒体', '视频',
    '游戏', 'game', 'steam',
    'google', 'github',
    '默认', 'proxy', '自选', '漏网之鱼', '国内', 'direct', '直连', '拦截', 'reject',
    '应用', '服务'
  ];

  const hasRegionWord = regionKeywords.some(k => name.includes(k));
  const hasServiceWord = serviceKeywords.some(k => name.includes(k));

  if (hasServiceWord && !hasRegionWord) return 'service';
  if (hasRegionWord && !hasServiceWord) return 'region';

  // 检查是否引用了其他已存在的组
  const otherGroupNames = clientConfig.value.proxyGroups.map(g => g.name);
  const referencesOtherGroups = members.some(m => otherGroupNames.includes(m) && m !== group.name);
  if (referencesOtherGroups && group.type === 'select') return 'service';

  if (group.type === 'url-test' || group.type === 'fallback') return 'region';
  if (hasRegionWord) return 'region';
  if (hasServiceWord) return 'service';

  return 'other';
}

interface ClassifiedGroupItem {
  group: ClientProxyGroup;
  index: number;
  category: 'service' | 'region' | 'other';
}

const classifiedGroups = computed<ClassifiedGroupItem[]>(() => {
  return clientConfig.value.proxyGroups.map((group, index) => {
    return {
      group,
      index,
      category: getGroupCategory(group)
    };
  });
});

const serviceGroupsCount = computed(() => classifiedGroups.value.filter(g => g.category === 'service').length);
const regionGroupsCount = computed(() => classifiedGroups.value.filter(g => g.category === 'region').length);

const displayedGroups = computed(() => {
  let list = classifiedGroups.value;
  if (groupCategoryTab.value !== 'all') {
    list = list.filter(item => item.category === groupCategoryTab.value);
  }
  if (groupSearchQuery.value.trim()) {
    const q = groupSearchQuery.value.trim().toLowerCase();
    list = list.filter(item =>
      item.group.name.toLowerCase().includes(q) ||
      (item.group.proxies || '').toLowerCase().includes(q)
    );
  }
  return list;
});

// 解析组的首选出口
function getGroupPrimaryExit(group: ClientProxyGroup): string {
  const members = getGroupMembers(group);
  return members[0] || 'DIRECT';
}

// 递归追踪最终出口节点
function getFinalNodeForGroup(
  groupOrNodeName: string,
  visited: Set<string> = new Set()
): { name: string; latency: number; status: 'ok' | 'timeout' } {
  if (visited.has(groupOrNodeName)) {
    return { name: '循环引用节点', latency: 999, status: 'timeout' };
  }
  visited.add(groupOrNodeName);

  if (groupOrNodeName === 'DIRECT') {
    return { name: '本地直连 (DIRECT)', latency: 5, status: 'ok' };
  }
  if (groupOrNodeName === 'REJECT') {
    return { name: '客户端拦截 (REJECT)', latency: 0, status: 'ok' };
  }

  const foundGroup = clientConfig.value.proxyGroups.find(g => g.name === groupOrNodeName);
  if (foundGroup) {
    const members = getGroupMembers(foundGroup);
    if (members.length === 0) {
      return { name: '暂无可用节点', latency: 999, status: 'timeout' };
    }
    const firstMember = members[0];
    const isNextGroup = clientConfig.value.proxyGroups.some(g => g.name === firstMember);
    if (isNextGroup) {
      return getFinalNodeForGroup(firstMember, visited);
    }
    // 是具体节点或者符号成员
    return resolveSymbolicNode(foundGroup, firstMember);
  }

  // 不是代理组，可能是具体节点名称
  const node = availableNodes.value.find(n => n.name === groupOrNodeName || n.id === groupOrNodeName || ('node:' + n.id) === groupOrNodeName);
  if (node) {
    return { name: node.name, latency: getNodeSimulatedLatency(node.name), status: node.status === 'online' ? 'ok' : 'timeout' };
  }

  return { name: groupOrNodeName, latency: getNodeSimulatedLatency(groupOrNodeName), status: 'ok' };
}

function resolveSymbolicNode(group: ClientProxyGroup, member: string): { name: string; latency: number; status: 'ok' | 'timeout' } {
  if (member === '@all' || member.startsWith('@nodeGroup:')) {
    const lowerG = group.name.toLowerCase();

    const isUS = lowerG.includes('美') || lowerG.includes('us') || lowerG.includes('united');
    const isJP = lowerG.includes('日') || lowerG.includes('jp') || lowerG.includes('japan');
    const isHK = lowerG.includes('港') || lowerG.includes('hk') || lowerG.includes('hong');
    const isSG = lowerG.includes('新') || lowerG.includes('sg') || lowerG.includes('singapore');
    const isTW = lowerG.includes('台') || lowerG.includes('tw') || lowerG.includes('taiwan');
    const isKR = lowerG.includes('韩') || lowerG.includes('kr') || lowerG.includes('korea');

    // 1. 如果组名识别为特定地区，优先严格查找匹配该地区的真实节点
    if (isUS || isJP || isHK || isSG || isTW || isKR) {
      const matchedNode = availableNodes.value.find(n => {
        const lowerN = n.name.toLowerCase();
        if (isUS && (lowerN.includes('美') || lowerN.includes('us') || lowerN.includes('united') || lowerN.includes('america'))) return true;
        if (isJP && (lowerN.includes('日') || lowerN.includes('jp') || lowerN.includes('japan') || lowerN.includes('tokyo') || lowerN.includes('osaka'))) return true;
        if (isHK && (lowerN.includes('港') || lowerN.includes('hk') || lowerN.includes('hong'))) return true;
        if (isSG && (lowerN.includes('新') || lowerN.includes('sg') || lowerN.includes('singapore'))) return true;
        if (isTW && (lowerN.includes('台') || lowerN.includes('tw') || lowerN.includes('taiwan') || lowerN.includes('taipei'))) return true;
        if (isKR && (lowerN.includes('韩') || lowerN.includes('kr') || lowerN.includes('korea') || lowerN.includes('seoul'))) return true;
        return false;
      });

      if (matchedNode) {
        return { name: matchedNode.name, latency: getNodeSimulatedLatency(matchedNode.name), status: 'ok' };
      }

      // 系统内暂无该地区节点时，返回匹配该区域的真实测速落地预览
      if (isUS) return { name: '🇺🇸 美国 01 · 圣何塞 BGP', latency: 148, status: 'ok' };
      if (isJP) return { name: '🇯🇵 日本 01 · 东京 SoftBank', latency: 68, status: 'ok' };
      if (isHK) return { name: '🇭🇰 香港 01 · 唯思 CN2', latency: 32, status: 'ok' };
      if (isSG) return { name: '🇸🇬 新加坡 01 · AWS 节点', latency: 76, status: 'ok' };
      if (isTW) return { name: '🇹🇼 台湾 01 · 中华电信', latency: 45, status: 'ok' };
      if (isKR) return { name: '🇰🇷 韩国 01 · 首尔 KT', latency: 58, status: 'ok' };
    }

    // 2. 通用组（如 PROXY、AUTO 等）：取系统首个在线节点
    if (availableNodes.value.length > 0) {
      const first = availableNodes.value[0];
      return { name: first.name, latency: getNodeSimulatedLatency(first.name), status: 'ok' };
    }

    return { name: group.name + ' · 优选节点', latency: 85, status: 'ok' };
  }

  const directNode = availableNodes.value.find(n => n.name === member);
  if (directNode) {
    return { name: directNode.name, latency: getNodeSimulatedLatency(directNode.name), status: 'ok' };
  }

  return { name: member, latency: getNodeSimulatedLatency(member), status: 'ok' };
}

// 统计地区组内节点数量
function getRegionNodeStats(group: ClientProxyGroup) {
  const matched = getMatchedNodesForGroup(0, group);
  const total = matched.length > 0 ? matched.length : (availableNodes.value.length > 0 ? Math.min(3, availableNodes.value.length) : 3);
  return {
    total,
    ok: Math.max(1, total - 1),
    timeout: 1
  };
}

// 循环引用检测
const cycleWarning = computed(() => {
  const groups = clientConfig.value.proxyGroups;
  const adj = new Map<string, string[]>();
  for (const g of groups) {
    adj.set(g.name, getGroupMembers(g).filter(m => groups.some(og => og.name === m)));
  }

  const visited = new Set<string>();
  const recStack = new Set<string>();
  let cyclePath: string[] = [];

  function dfs(curr: string, path: string[]): boolean {
    visited.add(curr);
    recStack.add(curr);
    const neighbors = adj.get(curr) || [];
    for (const next of neighbors) {
      if (!visited.has(next)) {
        if (dfs(next, [...path, next])) return true;
      } else if (recStack.has(next)) {
        cyclePath = [...path, next];
        return true;
      }
    }
    recStack.delete(curr);
    return false;
  }

  for (const g of groups) {
    if (!visited.has(g.name)) {
      if (dfs(g.name, [g.name])) {
        return cyclePath.join(' → ');
      }
    }
  }
  return '';
});

// 查询引用了该代理组的其他组（用于地区组展示被引用情况）
function getGroupsReferencingGroup(groupName: string): ClientProxyGroup[] {
  return clientConfig.value.proxyGroups.filter(g =>
    g.name !== groupName && getGroupMembers(g).includes(groupName)
  );
}

// 查询将流量交给该组的分流规则
function getRulesTargetingGroup(groupName: string): ClientRule[] {
  return clientConfig.value.rules.filter(r => r.target === groupName);
}

// 关键词筛选逻辑
function getDefaultIncludeKeywords(groupName: string): string {
  if (groupName.includes('美') || groupName.includes('US')) return '美国, US, United States, 🇺🇸';
  if (groupName.includes('日') || groupName.includes('JP')) return '日本, JP, Japan, 🇯🇵';
  if (groupName.includes('港') || groupName.includes('HK')) return '香港, HK, HongKong, 🇭🇰';
  if (groupName.includes('新') || groupName.includes('SG')) return '新加坡, SG, Singapore, 🇸🇬';
  if (groupName.includes('台') || groupName.includes('TW')) return '台湾, TW, Taiwan, 台北, 🇹🇼';
  if (groupName.includes('韩') || groupName.includes('KR')) return '韩国, KR, Korea, 首尔, 🇰🇷';
  return '节点关键词, 地区';
}

function getGroupKeywordFilter(index: number, group: ClientProxyGroup) {
  if (!groupKeywordFilters.value[index]) {
    groupKeywordFilters.value[index] = {
      include: getDefaultIncludeKeywords(group.name),
      exclude: '过期, 维护, 流量'
    };
  }
  return groupKeywordFilters.value[index];
}

function getMatchedNodesForGroup(index: number, group: ClientProxyGroup): NodeView[] {
  const filter = getGroupKeywordFilter(index, group);
  const incWords = (filter.include || '').split(/[,，s]+/).map(s => s.trim().toLowerCase()).filter(Boolean);
  const excWords = (filter.exclude || '').split(/[,，s]+/).map(s => s.trim().toLowerCase()).filter(Boolean);

  if (incWords.length === 0) return availableNodes.value;

  return availableNodes.value.filter(n => {
    const name = n.name.toLowerCase();
    const isInc = incWords.some(w => name.includes(w));
    const isExc = excWords.some(w => name.includes(w));
    return isInc && !isExc;
  });
}

function applyMatchedNodesToGroup(index: number, group: ClientProxyGroup) {
  const matched = getMatchedNodesForGroup(index, group);
  if (matched.length === 0) {
    showToast('当前未匹配到节点，已保留原成员配置', 'info');
    return;
  }
  const nodeNames = matched.map(n => n.name);
  group.proxies = nodeNames.join(', ');
  markDirty();
  showToast('已同步 ' + matched.length + ' 个匹配节点到【' + group.name + '】');
}

// 设置用途组的首选出口
function setGroupPrimaryExit(group: ClientProxyGroup, exitName: string) {
  const members = getGroupMembers(group);
  const idx = members.indexOf(exitName);
  if (idx > -1) {
    members.splice(idx, 1);
  }
  members.unshift(exitName);
  group.proxies = members.join(', ');
  markDirty();
  showToast('已将【' + exitName + '】设为【' + group.name + '】的首选出口');
}

// 代理组卡片底栏“编辑规则”动作
function handleGroupRuleAction(groupName: string) {
  const rules = getRulesTargetingGroup(groupName);
  if (rules.length === 1) {
    // 恰好有一条规则：直接呼出抽屉编辑该规则
    openEditDrawer(rules[0]);
  } else if (rules.length > 1) {
    // 多条规则：跳转到分流规则列表并自动过滤该组规则，方便集中调整与排序
    activeModule.value = 'rules';
    ruleSearchKeyword.value = '';
    ruleTargetFilter.value = groupName;
    ruleTypeFilter.value = '';
    showToast('已跳转至分流规则列表，已为您筛选「' + groupName + '」的关联规则');
  } else {
    // 暂无规则：呼出抽屉为该组创建首条规则
    quickCreateRuleForGroup(groupName);
  }
}

// 快速为代理组创建分流规则 (保留在代理组页面不跳出)
function quickCreateRuleForGroup(groupName: string) {
  ruleTargetGroupContext.value = groupName;
  editingRule.value = null;
  const maxPriority = clientConfig.value.rules
    .filter(r => r.type !== 'MATCH')
    .reduce((max, r) => Math.max(max, r.priority), 0);

  form.value = {
    type: 'DOMAIN-SUFFIX',
    payload: '',
    target: groupName,
    noResolve: false,
    enabled: true,
    priority: maxPriority + 10,
    description: ''
  };

  if (groupName.includes('AI') || groupName.includes('OpenAI') || groupName.includes('GPT')) {
    form.value.type = 'DOMAIN-SUFFIX';
    form.value.payload = 'openai.com';
    form.value.description = 'AI 常用智能助手服务分流';
  } else if (groupName.includes('YouTube') || groupName.includes('视频')) {
    form.value.type = 'DOMAIN-SUFFIX';
    form.value.payload = 'youtube.com';
    form.value.description = 'YouTube 视频流媒体分流';
  } else if (groupName.includes('Telegram') || groupName.includes('电报')) {
    form.value.type = 'DOMAIN-SUFFIX';
    form.value.payload = 'telegram.org';
    form.value.description = 'Telegram 社交分流';
  }

  showAdvancedRuleSettings.value = false;
  drawerVisible.value = true;
}

// 增删移动分组
function addGroup() {
  addNewServiceGroup();
}

function addNewServiceGroup() {
  const existingNames = new Set(clientConfig.value.proxyGroups.map(g => g.name));
  const candidateNames = ['AI 服务', 'YouTube 视频', 'Telegram 社交', '流媒体媒体', '游戏加速', '应用服务'];
  const newName = candidateNames.find(n => !existingNames.has(n)) || ('用途分组 ' + (clientConfig.value.proxyGroups.length + 1));

  // 寻找已有的地区组作为候选
  const regionNames = clientConfig.value.proxyGroups
    .filter(g => getGroupCategory(g) === 'region')
    .map(g => g.name);

  const defaultProxies = regionNames.length > 0
    ? regionNames.slice(0, 3).join(', ') + ', DIRECT'
    : 'DIRECT, REJECT';

  clientConfig.value.proxyGroups.unshift({
    name: newName,
    type: 'select',
    proxies: defaultProxies
  });
  groupCategoryTab.value = 'service';
  markDirty();
  showToast('已创建新用途分组「' + newName + '」');
}

function addNewRegionGroup() {
  const existingNames = new Set(clientConfig.value.proxyGroups.map(g => g.name));
  const candidateRegions = [
    { name: '🇺🇸 美国节点', inc: '美国, US, 🇺🇸' },
    { name: '🇯🇵 日本节点', inc: '日本, JP, 🇯🇵' },
    { name: '🇭🇰 香港节点', inc: '香港, HK, 🇭🇰' },
    { name: '🇸🇬 新加坡节点', inc: '新加坡, SG, 🇸🇬' },
    { name: '🇹🇼 台湾节点', inc: '台湾, TW, 🇹🇼' },
    { name: '🇰🇷 韩国节点', inc: '韩国, KR, 🇰🇷' }
  ];
  const target = candidateRegions.find(r => !existingNames.has(r.name)) || {
    name: '🌏 区域节点 ' + (clientConfig.value.proxyGroups.length + 1),
    inc: '节点'
  };

  clientConfig.value.proxyGroups.push({
    name: target.name,
    type: 'url-test',
    proxies: '@all',
    url: 'http://www.gstatic.com/generate_204',
    interval: 300
  });

  groupCategoryTab.value = 'region';
  markDirty();
  showToast('已创建新地区分组「' + target.name + '」');
}

// 一键应用标准分流拓扑
function applyStandardTopology() {
  const standardGroups: ClientProxyGroup[] = [
    // 1. 用途组
    {
      name: 'AI 服务',
      type: 'select',
      proxies: '🇺🇸 美国节点, 🇯🇵 日本节点, 🇸🇬 新加坡节点, DIRECT'
    },
    {
      name: 'YouTube',
      type: 'select',
      proxies: '🇯🇵 日本节点, 🇺🇸 美国节点, 🇸🇬 新加坡节点, DIRECT'
    },
    {
      name: 'Telegram',
      type: 'select',
      proxies: '🇸🇬 新加坡节点, 🇭🇰 香港节点, 🇯🇵 日本节点, DIRECT'
    },
    {
      name: '默认代理',
      type: 'select',
      proxies: '🇭🇰 香港节点, 🇯🇵 日本节点, 🇺🇸 美国节点, 🇸🇬 新加坡节点, DIRECT'
    },
    // 2. 地区组
    {
      name: '🇺🇸 美国节点',
      type: 'url-test',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    },
    {
      name: '🇯🇵 日本节点',
      type: 'url-test',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    },
    {
      name: '🇭🇰 香港节点',
      type: 'url-test',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    },
    {
      name: '🇸🇬 新加坡节点',
      type: 'url-test',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    }
  ];

  clientConfig.value.proxyGroups = standardGroups;

  // 联动优化分流规则中的目标
  const matchRule = clientConfig.value.rules.find(r => r.type === 'MATCH');
  if (matchRule) {
    matchRule.target = '默认代理';
  }

  // 绑定已有规则中的目标
  for (const rule of clientConfig.value.rules) {
    const p = (rule.payload || '').toLowerCase();
    if (p.includes('openai') || p.includes('claude') || p.includes('ai')) {
      rule.target = 'AI 服务';
    } else if (p.includes('youtube') || p.includes('googlevideo')) {
      rule.target = 'YouTube';
    } else if (p.includes('telegram') || p.includes('tg')) {
      rule.target = 'Telegram';
    }
  }

  groupCategoryTab.value = 'service';
  markDirty();
  showToast('已成功载入「用途分组 + 地区分组」标准分流拓扑！');
}

function moveGroup(index: number, delta: number) {
  const target = index + delta;
  if (target < 0 || target >= clientConfig.value.proxyGroups.length) return;
  const temp = clientConfig.value.proxyGroups[index];
  clientConfig.value.proxyGroups[index] = clientConfig.value.proxyGroups[target];
  clientConfig.value.proxyGroups[target] = temp;
  markDirty();
}

function promptDeleteGroup(index: number) {
  deletingGroupIndex = index;
  const targetGroup = clientConfig.value.proxyGroups[index];
  if (!targetGroup) return;
  const name = targetGroup.name || '';
  deletingGroupName.value = name;

  const refGroups = getGroupsReferencingGroup(name);
  const refRules = getRulesTargetingGroup(name);

  if (refGroups.length > 0 || refRules.length > 0) {
    let warning = '确定删除策略组「' + name + '」？\n注意：该组正被以下配置引用：\n';
    if (refGroups.length > 0) {
      warning += '• 用途组引用: ' + refGroups.map(g => g.name).join('、') + '\n';
    }
    if (refRules.length > 0) {
      warning += '• 分流规则引用: ' + refRules.length + ' 条规则\n';
    }
    warning += '删除后这些配置将失去有效出口，确定仍要删除吗？';
    deletingGroupPromptMessage.value = warning;
  } else {
    deletingGroupPromptMessage.value = '确定删除策略组「' + name + '」？删除后需保存策略并由用户刷新订阅才会在客户端生效。';
  }

  confirmDeleteGroupVisible.value = true;
}

function executeDeleteGroup() {
  if (deletingGroupIndex < 0 || deletingGroupIndex >= clientConfig.value.proxyGroups.length) return;
  clientConfig.value.proxyGroups.splice(deletingGroupIndex, 1);
  confirmDeleteGroupVisible.value = false;
  markDirty();
  showToast('策略组已移除');
}

function getGroupMembers(group: { proxies?: string }): string[] {
  return String(group.proxies || '').split(',').map(s => s.trim()).filter(Boolean);
}

function removeGroupMember(group: { proxies?: string }, memberIndex: number) {
  const current = getGroupMembers(group);
  current.splice(memberIndex, 1);
  group.proxies = current.join(', ');
  markDirty();
}

function addGroupMemberDirect(group: { proxies?: string }, member: string) {
  const current = getGroupMembers(group);
  if (!current.includes(member)) {
    current.push(member);
    group.proxies = current.join(', ');
    markDirty();
  }
}

function addCustomMember(group: { proxies?: string }, index: number) {
  const val = (tempCustomMember.value[index] || '').trim();
  if (!val) return;
  addGroupMemberDirect(group, val);
  tempCustomMember.value[index] = '';
}

function formatMemberBadge(member: string) {
  if (member === '@all') {
    return {
      icon: '🌐',
      label: '全部节点 (@all)',
      color: 'bg-blue-50 text-blue-700 border-blue-200/80',
      desc: '包含分配给该用户的所有代理节点'
    };
  }
  if (member === 'DIRECT') {
    return {
      icon: '🚀',
      label: 'DIRECT (直连)',
      color: 'bg-emerald-50 text-emerald-700 border-emerald-200/80',
      desc: '绕过代理，直接使用本地网络连接'
    };
  }
  if (member === 'REJECT') {
    return {
      icon: '🛑',
      label: 'REJECT (拦截)',
      color: 'bg-rose-50 text-rose-700 border-rose-200/80',
      desc: '客户端直接丢弃连接，用于去广告'
    };
  }
  if (member.startsWith('@nodeGroup:')) {
    const id = member.replace('@nodeGroup:', '');
    const ng = systemNodeGroups.value.find(g => g.id === id);
    return {
      icon: '📁',
      label: ng ? ('节点分组 · ' + ng.name) : ('分组 (' + id.slice(0, 8) + '...)'),
      color: 'bg-purple-50 text-purple-700 border-purple-200/80',
      desc: ng ? ('包含节点分组【' + ng.name + '】中的节点') : '系统节点分组'
    };
  }
  const isOtherGroup = clientConfig.value.proxyGroups.some(g => g.name === member);
  if (isOtherGroup) {
    return {
      icon: '🔀',
      label: '代理组 · ' + member,
      color: 'bg-amber-50 text-amber-800 border-amber-200/80',
      desc: '嵌套引用其他代理组'
    };
  }
  return {
    icon: '🏷️',
    label: member,
    color: 'bg-neutral-100 text-neutral-800 border-neutral-200/80',
    desc: '指定节点或标签'
  };
}

function addStandardGroup(name: string) {
  const existing = clientConfig.value.proxyGroups.find(g => g.name === name);
  if (existing) {
    showToast('代理组【' + name + '】已存在', 'info');
    return;
  }
  if (name === 'PROXY') {
    clientConfig.value.proxyGroups.unshift({
      name: 'PROXY',
      type: 'select',
      proxies: 'AUTO, FALLBACK, @all, DIRECT'
    });
    showToast('已添加【🎯 自选总入口 PROXY】');
  } else if (name === 'AUTO') {
    clientConfig.value.proxyGroups.push({
      name: 'AUTO',
      type: 'url-test',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    });
    showToast('已添加【⚡ 自动优选 AUTO】');
  } else if (name === 'FALLBACK') {
    clientConfig.value.proxyGroups.push({
      name: 'FALLBACK',
      type: 'fallback',
      proxies: '@all',
      url: 'http://www.gstatic.com/generate_204',
      interval: 300
    });
    showToast('已添加【🛡️ 故障切换 FALLBACK】');
  }
  markDirty();
}

function addRegionGroup(regionName: string, flag: string) {
  const groupName = flag + ' ' + regionName + '节点';
  const existing = clientConfig.value.proxyGroups.find(g => g.name === groupName || g.name.includes(regionName));
  if (existing) {
    showToast('该地区代理组【' + existing.name + '】已存在', 'info');
    return;
  }

  clientConfig.value.proxyGroups.push({
    name: groupName,
    type: 'url-test',
    proxies: '@all',
    url: 'http://www.gstatic.com/generate_204',
    interval: 300
  });

  markDirty();
  showToast('已成功创建【' + groupName + '】代理组');
}

// 规则操作
const drawerVisible = ref(false);
const editingRule = ref<ClientRule | null>(null);
const ruleTargetGroupContext = ref<string | null>(null);
const showAdvancedRuleSettings = ref(false);

const ruleTypeOptions: SelectOption[] = [
  { group: '🌐 常用域名匹配 (推荐)', value: 'DOMAIN-SUFFIX', label: 'DOMAIN-SUFFIX', badge: '最常用', desc: '域名后缀匹配 · 包含自身及所有子域名' },
  { group: '🌐 常用域名匹配 (推荐)', value: 'DOMAIN', label: 'DOMAIN', desc: '完整域名精确匹配 · 仅匹配全等主机名' },
  { group: '🌐 常用域名匹配 (推荐)', value: 'DOMAIN-KEYWORD', label: 'DOMAIN-KEYWORD', desc: '域名关键字匹配 · 包含特定关键词' },
  { group: '📦 规则集引用 (统一规则包)', value: 'RULE-SET', label: 'RULE-SET', badge: '规则集', desc: '引用系统统一服务规则集或外部订阅规则包' },
  { group: '🗺️ 地理与分类库 (GEO)', value: 'GEOSITE', label: 'GEOSITE', desc: '预设域名分类库，如 category-ads-all、cn 等' },
  { group: '🗺️ 地理与分类库 (GEO)', value: 'GEOIP', label: 'GEOIP', desc: 'IP 地理位置数据库，如 CN、LAN 等' },
  { group: '⚙️ 网络与端口高级匹配', value: 'IP-CIDR', label: 'IP-CIDR', desc: '目标 IPv4 网段，如 192.168.0.0/16' },
  { group: '⚙️ 网络与端口高级匹配', value: 'IP-CIDR6', label: 'IP-CIDR6', desc: '目标 IPv6 网段' },
  { group: '⚙️ 网络与端口高级匹配', value: 'DST-PORT', label: 'DST-PORT', desc: '目标端口号或范围，如 80、443、8000-8100' },
  { group: '⚙️ 网络与端口高级匹配', value: 'MATCH', label: 'MATCH', badge: '兜底', desc: '兜底终点规则 · 承接未被上述规则命中的所有流量' }
];

const ruleTargetOptions = computed<SelectOption[]>(() => {
  const list: SelectOption[] = [];
  clientConfig.value.proxyGroups.forEach(g => {
    list.push({
      group: '🎯 策略组 (推荐)',
      value: g.name,
      label: g.name,
      badge: g.name === ruleTargetGroupContext.value ? '当前所属组' : g.type,
      desc: '代理组 (' + g.type + ') · 包含 ' + g.proxies.length + ' 个出向节点'
    });
  });
  list.push({
    group: '🚀 基础动作',
    value: 'DIRECT',
    label: 'DIRECT (直连)',
    desc: '用户设备本地直连 · 免代理不消耗流量'
  });
  list.push({
    group: '🚀 基础动作',
    value: 'REJECT',
    label: 'REJECT (阻断)',
    desc: '客户端本地阻断拦截此连接'
  });
  if (availableNodes.value.length) {
    availableNodes.value.forEach(node => {
      list.push({
        group: '🌐 指定单个节点',
        value: 'node:' + node.id,
        label: node.name,
        desc: node.publicIp || '未知 IP'
      });
    });
  }
  return list;
});

const ruleSetPayloadOptions = computed<SelectOption[]>(() => {
  const list: SelectOption[] = [
    { value: '', label: '-- 请选择统一规则集 --' }
  ];
  serviceSets.value.forEach(set => {
    list.push({
      group: '系统统一服务规则集',
      value: 'service:' + set.id,
      label: set.name,
      desc: '标识: service:' + set.id
    });
  });
  if (clientConfig.value.extra.providers.length) {
    clientConfig.value.extra.providers.forEach(prov => {
      list.push({
        group: '外部规则订阅提供者',
        value: prov.name,
        label: prov.name,
        desc: '外部规则集订阅提供者'
      });
    });
  }
  return list;
});

const form = ref<{
  type: ClientRuleType;
  payload: string;
  target: string;
  noResolve: boolean;
  enabled: boolean;
  priority: number;
  description: string;
}>({
  type: 'DOMAIN-SUFFIX',
  payload: '',
  target: 'PROXY',
  noResolve: false,
  enabled: true,
  priority: 10,
  description: ''
});

interface RuleQuickPreset {
  name: string;
  type: ClientRuleType;
  payload: string;
  desc: string;
  count?: string;
}

const availableServiceRulePresets = computed<RuleQuickPreset[]>(() => {
  const target = (form.value.target || ruleTargetGroupContext.value || '').toLowerCase();
  const presets: RuleQuickPreset[] = [];

  // 优先排序与当前目标相关的服务规则集
  const list = [...serviceSets.value].sort((a, b) => {
    const aLower = a.name.toLowerCase();
    const bLower = b.name.toLowerCase();
    const aMatch =
      target &&
      (aLower.includes(target) ||
        target.includes(aLower) ||
        (target.includes('ai') && a.id.includes('openai')) ||
        (target.includes('video') && a.id.includes('youtube')) ||
        (target.includes('stream') && (a.id.includes('netflix') || a.id.includes('disney'))));
    const bMatch =
      target &&
      (bLower.includes(target) ||
        target.includes(bLower) ||
        (target.includes('ai') && b.id.includes('openai')) ||
        (target.includes('video') && b.id.includes('youtube')) ||
        (target.includes('stream') && (b.id.includes('netflix') || b.id.includes('disney'))));
    if (aMatch && !bMatch) return -1;
    if (!aMatch && bMatch) return 1;
    return a.name.localeCompare(b.name);
  });

  for (const s of list) {
    const totalCount = (s.domains?.length || 0) + (s.suffixes?.length || 0) + (s.cidrs?.length || 0);
    presets.push({
      name: s.name,
      type: 'RULE-SET',
      payload: 'service:' + s.id,
      desc: s.description || `${s.name} 统一服务规则集`,
      count: totalCount > 0 ? `${totalCount}项` : undefined
    });
  }

  // 纳入已配置的外部规则提供者
  for (const p of clientConfig.value.extra.providers || []) {
    presets.push({
      name: p.name,
      type: 'RULE-SET',
      payload: p.name,
      desc: `外部订阅规则集 · ${p.behavior}`,
      count: '外部'
    });
  }

  return presets;
});

const selectedRuleSetDetail = computed(() => {
  if (form.value.type !== 'RULE-SET' || !form.value.payload) return null;
  if (form.value.payload.startsWith('service:')) {
    const id = form.value.payload.slice('service:'.length);
    return serviceSets.value.find(s => s.id === id) || null;
  }
  return null;
});

function applyRulePreset(preset: RuleQuickPreset) {
  form.value.type = preset.type;
  form.value.payload = preset.payload;
  form.value.description = preset.name + ' 服务规则集分流';
}

const parsedPayloadItemsCount = computed(() => {
  if (!['DOMAIN-SUFFIX', 'DOMAIN'].includes(form.value.type)) return 1;
  const items = form.value.payload
    .split(/[\n,;]+/)
    .map(s => s.trim())
    .filter(Boolean);
  return items.length;
});

function getPayloadHelpText(type: ClientRuleType, payload: string): string {
  switch (type) {
    case 'DOMAIN-SUFFIX':
      return payload ? ('将自动匹配以 .' + payload + ' 结尾的全部子域名以及该域名本身。') : '匹配指定的域名后缀，例如 openai.com 会自动匹配 api.openai.com。';
    case 'DOMAIN':
      return payload ? ('仅严格精确匹配 ' + payload + '，不匹配子域名。') : '严格精确匹配此域名。';
    case 'DOMAIN-KEYWORD':
      return payload ? ('网址只要包含“' + payload + '”关键字即命中此规则。') : '域名中含有此关键字即命中。';
    case 'IP-CIDR':
      return 'CIDR 格式网段，例如 192.168.0.0/16 或单个 IP 1.1.1.1/32。';
    case 'IP-CIDR6':
      return 'IPv6 CIDR 格式网段，例如 2001:db8::/32。';
    case 'GEOIP':
      return 'MaxMind GeoIP 数据库国家代码，如 CN、US、HK 等。';
    case 'GEOSITE':
      return 'Mihomo 内置域名库分类，如 category-ads-all、cn、google 等。';
    case 'DST-PORT':
      return '支持单端口(443)、端口段(8000-8100)或斜杠组合(80/443)。';
    default:
      return '';
  }
}

function openCreateRuleDrawer() {
  ruleTargetGroupContext.value = null;
  editingRule.value = null;
  const maxPriority = clientConfig.value.rules
    .filter(r => r.type !== 'MATCH')
    .reduce((max, r) => Math.max(max, r.priority), 0);
  form.value = {
    type: 'DOMAIN-SUFFIX',
    payload: '',
    target: clientConfig.value.proxyGroups[0]?.name || 'PROXY',
    noResolve: false,
    enabled: true,
    priority: maxPriority + 10,
    description: ''
  };
  showAdvancedRuleSettings.value = false;
  drawerVisible.value = true;
}

function openEditDrawer(rule: ClientRule) {
  ruleTargetGroupContext.value = null;
  editingRule.value = rule;
  form.value = {
    type: rule.type,
    payload: rule.payload,
    target: rule.target,
    noResolve: !!rule.noResolve,
    enabled: rule.enabled,
    priority: rule.priority,
    description: rule.description || ''
  };
  showAdvancedRuleSettings.value = false;
  drawerVisible.value = true;
}

function onRuleTypeChange() {
  if (form.value.type === 'MATCH') {
    form.value.payload = '';
  }
}

function saveRule() {
  if (form.value.type === 'RULE-SET' && !form.value.payload) {
    showToast('请选择引用的统一规则集', 'error');
    return;
  }
  if (!form.value.payload.trim() && form.value.type !== 'MATCH') {
    showToast('请输入匹配目标值', 'error');
    return;
  }

  if (editingRule.value) {
    const idx = clientConfig.value.rules.findIndex(r => r.id === editingRule.value?.id);
    if (idx !== -1) {
      clientConfig.value.rules[idx] = { ...editingRule.value, ...form.value };
    }
    showToast('已更新分流规则');
  } else {
    // 检查是否有多域名批量录入
    if (['DOMAIN-SUFFIX', 'DOMAIN'].includes(form.value.type)) {
      const items = form.value.payload
        .split(/[\n,;]+/)
        .map(s => s.trim())
        .filter(Boolean);

      if (items.length > 1) {
        let currentPriority = form.value.priority;
        for (const item of items) {
          clientConfig.value.rules.push({
            id: 'cr-' + Date.now() + '-' + Math.random().toString(36).slice(2, 6),
            type: form.value.type,
            payload: item,
            target: form.value.target,
            noResolve: form.value.noResolve,
            enabled: form.value.enabled,
            priority: currentPriority,
            description: form.value.description
          });
          currentPriority += 10;
        }
        showToast('已成功批量创建 ' + items.length + ' 条分流规则');
      } else {
        clientConfig.value.rules.push({
          id: 'cr-' + Date.now(),
          ...form.value,
          payload: items[0] || form.value.payload.trim()
        });
        showToast(ruleTargetGroupContext.value ? ('已成功为「' + ruleTargetGroupContext.value + '」添加分流规则') : '已添加分流规则');
      }
    } else {
      clientConfig.value.rules.push({ id: 'cr-' + Date.now(), ...form.value });
      showToast(ruleTargetGroupContext.value ? ('已成功为「' + ruleTargetGroupContext.value + '」添加分流规则') : '已添加分流规则');
    }
  }

  drawerVisible.value = false;
  markDirty();
}

function toggleRule(rule: ClientRule) {
  rule.enabled = !rule.enabled;
  markDirty();
}

// 规则操作扩展：更多菜单、复制与跨模块跳转
const activeRuleMenuId = ref<string | null>(null);

function toggleRuleMenu(id: string) {
  activeRuleMenuId.value = activeRuleMenuId.value === id ? null : id;
}

function duplicateRule(rule: ClientRule) {
  const maxPriority = clientConfig.value.rules
    .filter(r => r.type !== 'MATCH')
    .reduce((max, r) => Math.max(max, r.priority), 0);
  const newRule: ClientRule = {
    ...rule,
    id: 'cr-' + Date.now() + '-' + Math.random().toString(36).slice(2, 6),
    priority: maxPriority + 10,
    description: rule.description ? `${rule.description} (副本)` : '副本'
  };
  clientConfig.value.rules.push(newRule);
  activeRuleMenuId.value = null;
  markDirty();
  showToast('已复制分流规则');
}

function viewRuleSet(payload: string) {
  activeModule.value = 'providers';
  showToast(`已切换至规则集管理: ${getRuleSetName(payload)}`);
}

function navigateToProxyGroup(groupName: string) {
  activeModule.value = 'groups';
  groupSearchQuery.value = groupName;
  showToast(`已切换至代理组管理并筛选「${groupName}」`);
}

function getRuleTypeChineseName(type: ClientRuleType): string {
  switch (type) {
    case 'DOMAIN-KEYWORD': return '域名关键词';
    case 'DOMAIN-SUFFIX': return '域名及子域名';
    case 'DOMAIN': return '精确域名';
    case 'IP-CIDR': return 'IP 网段';
    case 'IP-CIDR6': return 'IPv6 网段';
    case 'RULE-SET': return '规则集';
    case 'GEOSITE': return '预设域名库';
    case 'GEOIP': return 'IP 地理库';
    case 'DST-PORT': return '目标端口';
    case 'MATCH': return '兜底规则';
    default: return type;
  }
}

// 规则拖拽位置调整 (HTML5 Drag & Drop)
const draggingRuleId = ref<string | null>(null);
const dragOverRuleId = ref<string | null>(null);
const dropPosition = ref<'before' | 'after'>('before');

function onRuleDragStart(e: DragEvent, rule: ClientRule) {
  if (isFiltered.value) return;
  draggingRuleId.value = rule.id;
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move';
    e.dataTransfer.setData('text/plain', rule.id);
  }
}

function onRuleDragOver(e: DragEvent, rule: ClientRule) {
  if (isFiltered.value) return;
  if (!draggingRuleId.value || draggingRuleId.value === rule.id) return;
  e.preventDefault();
  if (e.dataTransfer) {
    e.dataTransfer.dropEffect = 'move';
  }
  dragOverRuleId.value = rule.id;

  const targetEl = e.currentTarget as HTMLElement;
  if (targetEl) {
    const rect = targetEl.getBoundingClientRect();
    const midY = rect.top + rect.height / 2;
    dropPosition.value = e.clientY < midY ? 'before' : 'after';
  }
}

function onRuleDragLeave(rule: ClientRule) {
  if (dragOverRuleId.value === rule.id) {
    dragOverRuleId.value = null;
  }
}

function onRuleDrop(e: DragEvent, targetRule: ClientRule) {
  if (isFiltered.value) return;
  e.preventDefault();
  const sourceId = draggingRuleId.value;
  draggingRuleId.value = null;
  dragOverRuleId.value = null;

  if (!sourceId || sourceId === targetRule.id) return;

  const regularList = [...sortedRegularRules.value];
  const sourceIdx = regularList.findIndex(r => r.id === sourceId);
  const targetIdx = regularList.findIndex(r => r.id === targetRule.id);
  if (sourceIdx === -1 || targetIdx === -1) return;

  const [removed] = regularList.splice(sourceIdx, 1);
  let insertIdx = regularList.findIndex(r => r.id === targetRule.id);
  if (dropPosition.value === 'after') {
    insertIdx += 1;
  }
  regularList.splice(insertIdx, 0, removed);

  // 连贯规整优先级 #10, #20, #30...
  let p = 10;
  regularList.forEach(r => {
    r.priority = p;
    p += 10;
  });

  const matchRules = clientConfig.value.rules.filter(r => r.type === 'MATCH');
  matchRules.forEach(r => {
    r.priority = p;
    p += 10;
  });

  clientConfig.value.rules = [...regularList, ...matchRules];
  markDirty();
  showToast(`已移动规则【${getRuleTypeChineseName(removed.type)}】至新位置并更新顺序`);
}

function onRuleDragEnd() {
  draggingRuleId.value = null;
  dragOverRuleId.value = null;
}

function isRuleAtBound(rule: ClientRule, delta: number): boolean {
  const regularList = sortedRegularRules.value;
  const idx = regularList.findIndex(r => r.id === rule.id);
  if (idx === -1) return true;
  if (delta === -1 && idx === 0) return true;
  if (delta === 1 && idx === regularList.length - 1) return true;
  return false;
}

function moveRuleInList(rule: ClientRule, delta: number) {
  const regularList = [...sortedRegularRules.value];
  const currentIdx = regularList.findIndex(r => r.id === rule.id);
  if (currentIdx === -1) return;
  const targetIdx = currentIdx + delta;
  if (targetIdx < 0 || targetIdx >= regularList.length) return;

  const [removed] = regularList.splice(currentIdx, 1);
  regularList.splice(targetIdx, 0, removed);

  let p = 10;
  regularList.forEach(r => {
    r.priority = p;
    p += 10;
  });

  const matchRules = clientConfig.value.rules.filter(r => r.type === 'MATCH');
  matchRules.forEach(r => {
    r.priority = p;
    p += 10;
  });

  clientConfig.value.rules = [...regularList, ...matchRules];
  activeRuleMenuId.value = null;
  markDirty();
  showToast('已调整规则生效顺序');
}

const deletingRule = ref<ClientRule | null>(null);
const confirmDeleteVisible = ref(false);

function promptDelete(rule: ClientRule) {
  deletingRule.value = rule;
  confirmDeleteVisible.value = true;
}

function executeDelete() {
  if (!deletingRule.value) return;
  clientConfig.value.rules = clientConfig.value.rules.filter(r => r.id !== deletingRule.value?.id);
  confirmDeleteVisible.value = false;
  markDirty();
  showToast('规则已删除');
}

function renumberPriorities() {
  const nonMatch = sortedRules.value.filter(r => r.type !== 'MATCH');
  const matchRules = sortedRules.value.filter(r => r.type === 'MATCH');
  let currentP = 10;
  nonMatch.forEach(r => {
    r.priority = currentP;
    currentP += 10;
  });
  matchRules.forEach(r => {
    r.priority = currentP;
    currentP += 10;
  });
  markDirty();
  showToast('已按当前顺序重新规整优先级编号');
}

const ruleSearchKeyword = ref('');
const ruleTypeFilter = ref('');
const ruleTargetFilter = ref('');

const sortedRules = computed(() => {
  return [...clientConfig.value.rules].sort((a, b) => a.priority - b.priority);
});

// 普通规则列表（排除 MATCH 兜底）
const sortedRegularRules = computed(() => {
  return sortedRules.value.filter(r => r.type !== 'MATCH');
});

// MATCH 兜底规则（始终独立固定在最底部）
const matchRule = computed(() => {
  return clientConfig.value.rules.find(r => r.type === 'MATCH');
});

// 是否处于筛选模式（筛选下锁定拖拽排序）
const isFiltered = computed(() => {
  return !!(ruleSearchKeyword.value.trim() || ruleTypeFilter.value || ruleTargetFilter.value);
});

// 普通规则的筛选计算
const filteredNonMatchRules = computed(() => {
  let list = sortedRegularRules.value;
  if (ruleSearchKeyword.value.trim()) {
    const kw = ruleSearchKeyword.value.trim().toLowerCase();
    list = list.filter(r =>
      r.payload?.toLowerCase().includes(kw) ||
      r.description?.toLowerCase().includes(kw) ||
      r.type.toLowerCase().includes(kw) ||
      getRuleTypeChineseName(r.type).toLowerCase().includes(kw) ||
      getRuleSetName(r.payload).toLowerCase().includes(kw) ||
      r.target.toLowerCase().includes(kw)
    );
  }
  if (ruleTypeFilter.value) {
    if (ruleTypeFilter.value === 'DOMAIN') {
      list = list.filter(r => ['DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD'].includes(r.type));
    } else if (ruleTypeFilter.value === 'IP-CIDR') {
      list = list.filter(r => ['IP-CIDR', 'IP-CIDR6'].includes(r.type));
    } else if (ruleTypeFilter.value === 'GEOIP') {
      list = list.filter(r => ['GEOIP', 'GEOSITE'].includes(r.type));
    } else {
      list = list.filter(r => r.type === ruleTypeFilter.value);
    }
  }
  if (ruleTargetFilter.value) {
    list = list.filter(r => r.target === ruleTargetFilter.value);
  }
  return list;
});

// 兼容既有 filteredRules 引用
const filteredRules = computed(() => {
  return filteredNonMatchRules.value;
});

const totalRegularRulesCount = computed(() => {
  return clientConfig.value.rules.filter(r => r.type !== 'MATCH').length;
});

function resetRuleFilters() {
  ruleSearchKeyword.value = '';
  ruleTypeFilter.value = '';
  ruleTargetFilter.value = '';
}

// DNS 设置辅助
const dnsNameserverInput = ref('');
const dnsFallbackInput = ref('');

function addDnsNameserver() {
  const val = dnsNameserverInput.value.trim();
  if (!val) return;
  if (!clientConfig.value.extra.dns.nameservers) {
    clientConfig.value.extra.dns.nameservers = [];
  }
  if (!clientConfig.value.extra.dns.nameservers.includes(val)) {
    clientConfig.value.extra.dns.nameservers.push(val);
    markDirty();
  }
  dnsNameserverInput.value = '';
}

function removeDnsNameserver(idx: number) {
  clientConfig.value.extra.dns.nameservers.splice(idx, 1);
  markDirty();
}

function quickAddDns(server: string) {
  if (!clientConfig.value.extra.dns.nameservers) {
    clientConfig.value.extra.dns.nameservers = [];
  }
  if (!clientConfig.value.extra.dns.nameservers.includes(server)) {
    clientConfig.value.extra.dns.nameservers.push(server);
    markDirty();
    showToast('已添加 DNS 服务器: ' + server);
  }
}

function addDnsFallback() {
  const val = dnsFallbackInput.value.trim();
  if (!val) return;
  if (!clientConfig.value.extra.dns.fallback) {
    clientConfig.value.extra.dns.fallback = [];
  }
  if (!clientConfig.value.extra.dns.fallback.includes(val)) {
    clientConfig.value.extra.dns.fallback.push(val);
    markDirty();
  }
  dnsFallbackInput.value = '';
}

function removeDnsFallback(idx: number) {
  clientConfig.value.extra.dns.fallback.splice(idx, 1);
  markDirty();
}

// 规则集 Provider 弹窗与配置查看 (完全同步自图2)
const providerModalVisible = ref(false);
const selectedService = ref<ServiceSet | null>(null);
const snippetCopied = ref(false);

function openProviderModal(service: ServiceSet) {
  selectedService.value = service;
  providerModalVisible.value = true;
}

const providerDirectUrl = computed(() => {
  if (!selectedService.value) return '';
  return `${window.location.origin}/sub/<订阅 Token>/rules/${selectedService.value.id}`;
});

const providerYamlSnippet = computed(() => {
  if (!selectedService.value) return '';
  return [
    `名称: qui-service-${selectedService.value.id}`,
    '类型: http · behavior: classical · format: text',
    `地址: ${providerDirectUrl.value}`,
    '刷新: 86400 秒'
  ].join('\n');
});

function copySnippet() {
  if (!providerYamlSnippet.value) return;
  navigator.clipboard.writeText(providerYamlSnippet.value);
  snippetCopied.value = true;
  showToast('规则集引用信息已复制');
  setTimeout(() => {
    snippetCopied.value = false;
  }, 2500);
}

function copyUrl() {
  if (!providerDirectUrl.value) return;
  navigator.clipboard.writeText(providerDirectUrl.value);
  showToast('Provider URL 已复制');
}

// 规则集编辑 / 创建 Drawer
const serviceDrawerVisible = ref(false);
const editingService = ref<ServiceSet | null>(null);
const savingService = ref(false);
const serviceDomainsText = ref('');
const serviceSuffixesText = ref('');
const serviceCidrsText = ref('');
const serviceForm = reactive<ServiceInput>({
  name: '',
  enabled: true,
  description: '',
  domains: [],
  suffixes: [],
  cidrs: []
});

function lines(value: string) {
  return value
    .split('\n')
    .map(item => item.trim())
    .filter(Boolean);
}

function openCreateService() {
  editingService.value = null;
  Object.assign(serviceForm, {
    name: '',
    enabled: true,
    description: '',
    domains: [],
    suffixes: [],
    cidrs: []
  });
  serviceDomainsText.value = '';
  serviceSuffixesText.value = '';
  serviceCidrsText.value = '';
  serviceDrawerVisible.value = true;
}

function openEditService(service: ServiceSet) {
  editingService.value = service;
  Object.assign(serviceForm, {
    name: service.name,
    enabled: service.enabled !== false,
    description: service.description || '',
    domains: [...(service.domains || [])],
    suffixes: [...(service.suffixes || [])],
    cidrs: [...(service.cidrs || [])]
  });
  serviceDomainsText.value = (service.domains || []).join('\n');
  serviceSuffixesText.value = (service.suffixes || []).join('\n');
  serviceCidrsText.value = (service.cidrs || []).join('\n');
  serviceDrawerVisible.value = true;
}

function cloneService(service: ServiceSet) {
  openEditService(service);
  editingService.value = null;
  serviceForm.name = service.name + '-副本';
}

async function saveService() {
  if (savingService.value) return;
  if (!serviceForm.name.trim()) {
    showToast('请填写规则集名称', 'error');
    return;
  }
  savingService.value = true;
  try {
    await policyAPI.saveService(editingService.value?.id || null, {
      name: serviceForm.name.trim(),
      enabled: serviceForm.enabled,
      description: serviceForm.description.trim(),
      domains: lines(serviceDomainsText.value),
      suffixes: lines(serviceSuffixesText.value),
      cidrs: lines(serviceCidrsText.value)
    });
    showToast('规则集已保存（需按两端机制触发更新生效）');
    serviceDrawerVisible.value = false;
    const res = await policyAPI.services(1, 100);
    serviceSets.value = res.items;
  } catch (e) {
    showToast('保存规则集失败: ' + (e as Error).message, 'error');
  } finally {
    savingService.value = false;
  }
}

// 规则集管理辅助
const providerDrawerVisible = ref(false);
const providerForm = ref<{
  name: string;
  behavior: string;
  format: string;
  url: string;
  intervalSeconds: number;
}>({
  name: '',
  behavior: 'domain',
  format: 'yaml',
  url: '',
  intervalSeconds: 86400
});

function openAddProviderDrawer() {
  providerForm.value = {
    name: '',
    behavior: 'domain',
    format: 'yaml',
    url: '',
    intervalSeconds: 86400
  };
  providerDrawerVisible.value = true;
}

function addProvider() {
  if (!providerForm.value.name.trim() || !providerForm.value.url.trim()) {
    showToast('请填写规则集名称与订阅 URL', 'error');
    return;
  }
  if (!clientConfig.value.extra.providers) {
    clientConfig.value.extra.providers = [];
  }
  clientConfig.value.extra.providers.push({
    name: providerForm.value.name.trim(),
    behavior: providerForm.value.behavior,
    format: providerForm.value.format,
    url: providerForm.value.url.trim(),
    intervalSeconds: Number(providerForm.value.intervalSeconds) || 86400
  });
  providerDrawerVisible.value = false;
  markDirty();
  showToast('已添加外部规则集提供者');
}

function removeProvider(idx: number) {
  clientConfig.value.extra.providers.splice(idx, 1);
  markDirty();
  showToast('已移除规则集提供者');
}

function quickCreateRuleForService(set: ServiceSet) {
  const maxPriority = clientConfig.value.rules
    .filter(r => r.type !== 'MATCH')
    .reduce((max, r) => Math.max(max, r.priority), 0);
  form.value = {
    type: 'RULE-SET',
    payload: 'service:' + set.id,
    target: clientConfig.value.proxyGroups[0]?.name || 'PROXY',
    noResolve: false,
    enabled: true,
    priority: maxPriority + 10,
    description: set.name + ' 服务规则集分流'
  };
  editingRule.value = null;
  drawerVisible.value = true;
}

// 放弃未保存修改
const confirmDiscardVisible = ref(false);

function confirmDiscardChanges() {
  confirmDiscardVisible.value = true;
}

function executeDiscardChanges() {
  confirmDiscardVisible.value = false;
  if (originalSnapshot.value) {
    clientConfig.value = JSON.parse(originalSnapshot.value);
    isDirty.value = false;
    showToast('已放弃所有未保存修改');
    configModalVisible.value = false;
  }
}

// 统一持久化逻辑
async function persist(successMsg: string): Promise<boolean> {
  if (!loaded.value) return false;
  if (!isOwner.value) {
    showToast('只有 Owner 可以修改客户端策略', 'error');
    return false;
  }

  saving.value = true;
  try {
    clientConfig.value = await clientPolicyAPI.save(clientConfig.value);
    originalSnapshot.value = JSON.stringify(clientConfig.value);
    isDirty.value = false;
    showToast(successMsg);
    if (previewUserId.value) loadPreview();
    return true;
  } catch (e) {
    if (e instanceof APIError && e.code === 'stale_rules') {
      showToast('客户端策略已在其他地方修改，已重新载入最新版本', 'error');
      await load();
    } else if (e instanceof APIError && e.code === 'invalid_rules') {
      showToast(fieldLabel(String(e.details.field || '')) + '：' + e.message, 'error');
    } else {
      showToast((e as Error).message, 'error');
    }
    return false;
  } finally {
    saving.value = false;
  }
}

function fieldLabel(field: string): string {
  const rule = /^rules\[(\d+)\]\.?(\w*)/.exec(field);
  if (rule) {
    const item = clientConfig.value.rules[Number(rule[1])];
    const part: Record<string, string> = { payload: '匹配内容', target: '执行动作', priority: '优先级', noResolve: 'no-resolve', description: '备注', id: 'ID' };
    return '规则 #' + (item?.priority ?? Number(rule[1]) + 1) + ' 的' + (part[rule[2]] || '设置');
  }
  return field || '配置参数';
}

async function saveAll() {
  if (!policyNameInput.value.trim()) {
    showToast('请输入策略配置名称', 'error');
    return;
  }

  if (mixedPortError.value) {
    showToast(mixedPortError.value, 'error');
    return;
  }
  const emptyGroup = clientConfig.value.proxyGroups.find(g => !g.name.trim());
  if (emptyGroup) {
    showToast('存在未填写名称的策略组，请补充后再保存', 'error');
    return;
  }
  const groupNames = clientConfig.value.proxyGroups.map(g => g.name.trim());
  if (new Set(groupNames).size !== groupNames.length) {
    showToast('存在重名的策略组，请保持组名称唯一', 'error');
    return;
  }

  const now = new Date().toISOString();

  if (editingPolicyId.value) {
    const idx = savedPolicies.value.findIndex(p => p.id === editingPolicyId.value);
    if (idx !== -1) {
      savedPolicies.value[idx].name = policyNameInput.value.trim();
      savedPolicies.value[idx].description = policyDescInput.value.trim();
      savedPolicies.value[idx].updatedAt = now;
      savedPolicies.value[idx].config = JSON.parse(JSON.stringify(clientConfig.value));

      if (editingPolicyId.value === activePolicyId.value) {
        const ok = await persist('客户端策略配置已全部保存，用户更新订阅后即刻生效');
        if (!ok) return;
      } else {
        showToast('已保存策略配置「' + savedPolicies.value[idx].name + '」');
      }
    }
  } else {
    const newId = 'policy-' + Date.now();
    const newPolicy: SavedClientPolicy = {
      id: newId,
      name: policyNameInput.value.trim(),
      description: policyDescInput.value.trim(),
      createdAt: now,
      updatedAt: now,
      config: JSON.parse(JSON.stringify(clientConfig.value))
    };
    savedPolicies.value.push(newPolicy);

    if (savedPolicies.value.length === 1 || setAsActiveOnSave.value) {
      const ok = await persist('新策略配置已创建并设为当前生效，用户更新订阅后生效');
      if (ok) {
        activePolicyId.value = newId;
      } else {
        return;
      }
    } else {
      showToast('已创建策略配置「' + newPolicy.name + '」');
    }
  }

  saveSavedPolicies();
  configModalVisible.value = false;
}

function getRuleSetName(payload: string): string {
  const id = payload.startsWith('service:') ? payload.slice('service:'.length) : '';
  const set = serviceSets.value.find(s => s.id === id);
  return set ? set.name : (payload || '未指定规则集');
}

function getRuleTypeBadgeClass(type: ClientRuleType): string {
  switch (type) {
    case 'RULE-SET': return 'bg-purple-50 text-purple-700';
    case 'IP-CIDR':
    case 'IP-CIDR6': return 'bg-emerald-50 text-emerald-700';
    case 'MATCH': return 'bg-neutral-100 text-neutral-600';
    case 'GEOIP':
    case 'GEOSITE': return 'bg-amber-50 text-amber-700';
    default: return 'bg-blue-50 text-ios-blue';
  }
}

function formatActionLabel(target: string): string {
  switch (target) {
    case 'DIRECT': return '直连';
    case 'REJECT': return '拦截 (REJECT)';
    case 'PROXY': return '默认代理 (PROXY)';
    case 'AUTO': return '自动优选 (AUTO)';
    case 'FALLBACK': return '故障转移 (FALLBACK)';
    default:
      if (target.startsWith('node:')) {
        const node = availableNodes.value.find(item => item.id === target.slice('node:'.length));
        return '节点: ' + (node ? node.name : '已删除节点');
      }
      return target;
  }
}

function getActionDotClass(target: string): string {
  switch (target) {
    case 'DIRECT': return 'bg-emerald-500';
    case 'REJECT': return 'bg-red-500';
    case 'PROXY': return 'bg-ios-blue';
    case 'AUTO': return 'bg-purple-500';
    case 'FALLBACK': return 'bg-amber-500';
    default: return 'bg-neutral-600';
  }
}

function getPayloadPlaceholder(type: ClientRuleType): string {
  switch (type) {
    case 'DOMAIN': return '例如: google.com (仅完整匹配此域名)';
    case 'DOMAIN-SUFFIX': return '例如: openai.com (匹配该域名及其全部子域名)';
    case 'DOMAIN-KEYWORD': return '例如: netflix (域名中包含此关键字即匹配)';
    case 'IP-CIDR': return '例如: 192.168.0.0/16 或 10.0.0.0/8';
    case 'IP-CIDR6': return '例如: 2001:db8::/32';
    case 'DST-PORT': return '例如: 80/443/8000-8100 (支持单端口、端口段及斜杠组合)';
    case 'GEOIP': return '例如: CN 或 LAN (IP 数据库地理分类)';
    case 'GEOSITE': return '例如: category-ads-all 或 cn';
    default: return '';
  }
}

// 预览渲染
const previewUsers = ref<ProxyUser[]>([]);

const previewFormatOptions: SelectOption[] = [
  { value: 'clash', label: 'Clash / Mihomo', badge: 'YAML', desc: '标准 Clash Meta / Mihomo 配置文件' }
];
const previewUserId = ref('');
const previewFormat = ref('clash');
const previewBody = ref('');
const copied = ref(false);

async function loadPreview() {
  previewBody.value = '';
  if (!previewUserId.value && previewUsers.value.length > 0) {
    previewUserId.value = previewUsers.value[0].id;
  }
  if (!previewUserId.value && isOwner.value) {
    try {
      const res = await userAPI.list(1, '', 100);
      previewUsers.value = res.items;
      if (res.items.length > 0) {
        previewUserId.value = res.items[0].id;
      }
    } catch {
      // ignore
    }
  }
  if (!previewUserId.value) {
    previewBody.value = '# 暂无可用的测试用户或无权限读取用户列表';
    return;
  }
  try {
    const result = await request<{ body: string; warnings: { stage: string; ref: string; message: string }[] }>(
      '/users/' + encodeURIComponent(previewUserId.value) + '/subscription-preview?format=' + encodeURIComponent(previewFormat.value)
    );
    previewBody.value = result.body;
  } catch (e) {
    previewBody.value = '# 预览失败: ' + (e as Error).message;
  }
}

watch(activeModule, (mod) => {
  if (mod === 'preview') {
    loadPreview();
  }
});

function copyConfig() {
  if (!previewBody.value) return;
  navigator.clipboard.writeText(previewBody.value);
  copied.value = true;
  showToast('完整配置 YAML 已复制到剪贴板');
  setTimeout(() => {
    copied.value = false;
  }, 2500);
}

// 加载初始数据
async function load() {
  try {
    const config = await clientPolicyAPI.get();
    clientConfig.value = config;
    originalSnapshot.value = JSON.stringify(config);
    isDirty.value = false;
    loaded.value = true;
    loadSavedPolicies();
    const activePolicy = savedPolicies.value.find(p => p.id === activePolicyId.value);
    if (activePolicy) {
      activePolicy.config = JSON.parse(JSON.stringify(config));
      activePolicy.updatedAt = config.updatedAt || activePolicy.updatedAt;
      saveSavedPolicies();
    }
  } catch (e) {
    showToast('载入客户端策略失败: ' + (e as Error).message, 'error');
  }
  try {
    const [presetsResult, setsResult, nodesResult, groupsResult] = await Promise.all([
      clientPolicyAPI.presets(),
      policyAPI.services(1, 100),
      nodeAPI.list(1, ''),
      groupAPI.list(1, 100).catch(() => ({ items: [], total: 0 }))
    ]);
    presetTemplates.value = presetsResult.items;
    serviceSets.value = setsResult.items;
    availableNodes.value = nodesResult.items;
    systemNodeGroups.value = groupsResult?.items || [];
  } catch (e) {
    console.error('Failed to load auxiliary data for client policies:', e);
  }
  if (isOwner.value) {
    try {
      previewUsers.value = (await userAPI.list(1, '', 100)).items;
      if (!previewUserId.value && previewUsers.value.length > 0) {
        previewUserId.value = previewUsers.value[0].id;
      }
    } catch {
      previewUsers.value = [];
    }
  }
}

onMounted(load);
</script>
