<template>
  <div class="space-y-6">
    <PageHeader
      title="节点分组"
      parent="节点配置"
      description="按代理节点灵活管理分组，支持在分流策略与规则中引用。"
    >
      <template #actions>
        <button class="ios-btn-secondary px-3.5 py-1.5 h-8 font-medium inline-flex items-center gap-1.5 shadow-2xs" :disabled="loading || busy" @click="load">
          <svg class="w-3.5 h-3.5" :class="{ 'animate-spin': loading }" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
          </svg>
          <span>刷新</span>
        </button>
        <button v-if="isOwner && !error" class="ios-btn-primary px-3.5 py-1.5 h-8 font-medium inline-flex items-center gap-1.5 shadow-2xs" :disabled="loading || busy" @click="createGroup">
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          <span>新建节点组</span>
        </button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <div v-else-if="loading" role="status" class="flex items-center justify-center p-12 text-sm text-neutral-400 gap-2">
      <svg class="w-4 h-4 animate-spin text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
      </svg>
      <span>正在加载节点分组…</span>
    </div>
    <EmptyState
      v-else-if="!groups.length"
      title="暂无节点分组"
      description="将代理节点加入分组，可在分流控制与策略中统一引用。"
    />
    <div v-else class="space-y-4">
      <!-- 顶部搜索工具栏与统计 -->
      <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3">
        <div class="relative flex items-center max-w-xs w-full">
          <svg
            class="w-3.5 h-3.5 text-neutral-400 absolute left-3 pointer-events-none shrink-0"
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
          </svg>
          <input
            v-model="keyword"
            type="text"
            style="padding-left: 2.25rem !important; padding-right: 2rem !important;"
            class="w-full h-9 bg-black/[0.04] hover:bg-black/[0.06] focus:bg-white rounded-xl border border-black/[0.06] focus:border-ios-blue focus:ring-2 focus:ring-ios-blue/20 text-neutral-800 placeholder:text-neutral-400 transition-all outline-none text-sm"
            placeholder="搜索节点分组…"
            aria-label="搜索节点分组"
          />
          <button
            v-if="keyword"
            type="button"
            class="absolute right-2.5 w-4 h-4 rounded-full bg-neutral-300 hover:bg-neutral-400 text-white flex items-center justify-center text-xs transition-colors cursor-pointer select-none"
            title="清空搜索"
            @click="keyword = ''"
          >
            ✕
          </button>
        </div>
        <div class="text-xs text-neutral-400 font-mono">
          共 {{ groups.length }} 个分组<span v-if="keyword">（筛选出 {{ filteredGroups.length }} 个）</span>
        </div>
      </div>

      <!-- 分组卡片列表 (Card Grid) -->
      <div v-if="filteredGroups.length" class="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        <div
          v-for="group in filteredGroups"
          :key="group.id"
          class="panel-card p-5 flex flex-col justify-between space-y-4 hover:shadow-md transition-all border border-black/[0.06]"
        >
          <div class="space-y-3">
            <!-- 头部：标题与状态 -->
            <div class="flex items-start justify-between gap-2">
              <div class="min-w-0 flex-1">
                <div class="flex items-center gap-1.5">
                  <span
                    class="w-2 h-2 rounded-full shrink-0"
                    :class="group.enabled ? 'bg-emerald-500 shadow-[0_0_6px_rgba(16,185,129,0.35)]' : 'bg-neutral-300'"
                    :title="group.enabled ? '正常启用' : '已停用'"
                  />
                  <h4 class="text-neutral-900 truncate text-lg font-bold leading-snug" :title="group.name">
                    {{ group.name }}
                  </h4>
                </div>
                <p class="text-xs text-neutral-500 mt-1 line-clamp-2 leading-relaxed">
                  {{ group.remark || '暂无分组备注说明' }}
                </p>
              </div>
              <div class="flex flex-col items-end gap-1 shrink-0">
                <span
                  class="text-xs px-2 py-0.5 rounded-full font-semibold border"
                  :class="group.purpose === 'subscription' ? 'bg-blue-50 text-ios-blue border-blue-200/60' : (group.purpose === 'mixed' ? 'bg-purple-50 text-purple-700 border-purple-200/60' : 'bg-amber-50 text-amber-700 border-amber-200/60')"
                >
                  {{ groupPurposeNames[group.purpose] || '节点分组' }}
                </span>
                <span
                  v-if="!group.subscriptionVisible"
                  class="text-xs px-1.5 py-0.2 rounded font-medium bg-neutral-100 text-neutral-500 border border-neutral-200/70"
                >
                  订阅隐藏
                </span>
              </div>
            </div>

            <!-- 核心统计指标摘要 -->
            <div class="grid grid-cols-3 gap-2 py-2 border-y border-black/[0.05] text-center bg-black/[0.015] rounded-xl px-2">
              <div>
                <span class="text-xs text-neutral-400 block">组内节点</span>
                <span class="text-xs font-bold text-neutral-800 font-mono tabular-nums mt-0.5 block">
                  {{ groupNodesFor(group.id).length }} 个
                </span>
              </div>
              <div>
                <span class="text-xs text-neutral-400 block">授权用户</span>
                <span class="text-xs font-bold text-neutral-800 font-mono tabular-nums mt-0.5 block">
                  {{ group.grantedUserIds?.length || 0 }} 位
                </span>
              </div>
              <div>
                <span class="text-xs text-neutral-400 block">运行状态</span>
                <span
                  class="text-xs font-bold mt-0.5 block"
                  :class="group.enabled ? 'text-emerald-600' : 'text-neutral-500'"
                >
                  {{ group.enabled ? '正常' : '停用' }}
                </span>
              </div>
            </div>
          </div>

          <!-- 卡片底部按钮：编辑、删除 -->
          <div class="grid grid-cols-2 gap-2 pt-3 border-t border-black/[0.06]">
            <!-- 1. 编辑 -->
            <button
              type="button"
              class="ios-btn-secondary h-8 px-2 rounded-xl font-medium inline-flex items-center justify-center gap-1.5 transition-all shadow-2xs"
              :disabled="busy"
              title="配置该分组的节点与授权账号"
              @click="openModalForGroup(group)"
            >
              <svg class="w-3.5 h-3.5 text-neutral-500 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M11 5H6a2 2 0 00-2 2v11a2 2 0 002 2h11a2 2 0 002-2v-5m-1.414-9.414a2 2 0 112.828 2.828L11.828 15H9v-2.828l8.586-8.586z" />
              </svg>
              <span>编辑</span>
            </button>

            <!-- 2. 删除 -->
            <button
              v-if="isOwner"
              type="button"
              class="h-8 px-2 rounded-xl border border-red-200 bg-red-50/60 hover:bg-red-100/80 text-red-600 text-xs font-medium inline-flex items-center justify-center gap-1.5 transition-colors disabled:opacity-40 disabled:pointer-events-none shadow-2xs"
              :disabled="busy"
              title="删除该节点分组"
              @click="askDeleteGroup(group)"
            >
              <svg class="w-3.5 h-3.5 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
              </svg>
              <span>删除</span>
            </button>
            <div v-else class="text-center text-xs text-neutral-400 self-center">只读模式</div>
          </div>
        </div>
      </div>
      <p v-else class="text-xs text-neutral-400 text-center py-12 panel-card">没有符合条件的节点分组</p>
    </div>

    <!-- 弹窗页面：节点分组管理模态弹窗 (AppDialog，参考客户端策略弹窗布局) -->
    <AppDialog
      v-model="groupModalVisible"
      :title="selected ? `编辑分组「${selected.name}」` : '节点分组管理'"
      description="左侧选择管理模块，右侧配置对应的节点成员或授权账号信息。"
      maxWidth="full"
      maxWidthClass="max-w-[1300px] xl:max-w-[1440px] 2xl:max-w-[1560px] w-full"
      heightClass="h-[88vh] max-h-[880px] min-h-[600px]"
      bodyClass="flex flex-col flex-1 min-h-0 p-6 overflow-hidden"
      :showFooter="false"
    >
      <template #header-actions>
        <div class="flex items-center gap-2">
          <!-- 基础属性编辑 (弹出小抽屉编辑名称/用途/备注) -->
          <button
            v-if="isOwner && selected"
            type="button"
            class="ios-btn-secondary px-2.5 py-1.5 inline-flex items-center gap-1"
            title="修改分组名称、用途或备注"
            @click="editGroupSettings(selected)"
          >
            <svg class="w-3.5 h-3.5 text-neutral-500" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.065 2.572c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.572 1.065c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.065-2.572c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z" />
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
            </svg>
            <span>分组设置</span>
          </button>

          <!-- 关闭按钮 -->
          <button
            type="button"
            class="ios-btn-secondary px-3 py-1.5 inline-flex items-center gap-1"
            @click="groupModalVisible = false"
          >
            <span>关闭</span>
          </button>
        </div>
      </template>

      <!-- 弹窗内部：左边菜单（账号、节点），右边对应内容 -->
      <div v-if="selected" class="flex flex-col md:flex-row gap-6 flex-1 min-h-0 h-full overflow-hidden">
        <!-- 左侧菜单：账号 与 节点 -->
        <div class="w-full md:w-56 lg:w-60 shrink-0 space-y-2 border-b md:border-b-0 md:border-r border-black/[0.06] pb-4 md:pb-0 md:pr-4 overflow-y-auto h-full pr-1">
          <div class="px-2 py-1 text-xs font-semibold text-neutral-400 uppercase tracking-wider">
            管理菜单
          </div>

          <!-- 1. 账号 (Users / Accounts) -->
          <button
            type="button"
            @click="activeMenu = 'users'"
            :class="[
              'w-full text-left p-2.5 rounded-xl transition-all border flex items-center justify-between group',
              activeMenu === 'users'
                ? 'bg-blue-50/70 border-ios-blue text-ios-blue ring-1 ring-ios-blue/30 shadow-2xs'
                : 'bg-white border-black/[0.06] hover:border-black/[0.15] hover:bg-black/[0.015] text-neutral-800'
            ]"
          >
            <div class="flex items-center gap-2.5 min-w-0">
              <div
                :class="[
                  'w-7 h-7 rounded-lg flex items-center justify-center shrink-0 transition-colors',
                  activeMenu === 'users'
                    ? 'bg-ios-blue text-white shadow-2xs'
                    : 'bg-black/[0.04] text-neutral-600 group-hover:bg-black/[0.08]'
                ]"
              >
                <!-- 账号图标 -->
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M17 20h5v-2a3 3 0 00-5.356-1.857M17 20H7m10 0v-2c0-.656-.126-1.283-.356-1.857M7 20H2v-2a3 3 0 015.356-1.857M7 20v-2c0-.656.126-1.283.356-1.857m0 0a5.002 5.002 0 019.288 0M15 7a3 3 0 11-6 0 3 3 0 016 0zm6 3a2 2 0 11-4 0 2 2 0 014 0zM7 10a2 2 0 11-4 0 2 2 0 014 0z" />
                </svg>
              </div>
              <div class="min-w-0">
                <span class="text-xs font-bold block truncate">授权账号</span>
                <span class="text-xs font-mono opacity-60 block uppercase truncate">ACCOUNTS</span>
              </div>
            </div>
            <span class="text-xs px-1.5 py-0.5 rounded-md font-mono bg-black/[0.04] text-neutral-600 shrink-0 tabular-nums">
              {{ selected.grantedUserIds?.length || 0 }} 位
            </span>
          </button>

          <!-- 2. 节点 (Nodes) -->
          <button
            type="button"
            @click="activeMenu = 'nodes'"
            :class="[
              'w-full text-left p-2.5 rounded-xl transition-all border flex items-center justify-between group',
              activeMenu === 'nodes'
                ? 'bg-blue-50/70 border-ios-blue text-ios-blue ring-1 ring-ios-blue/30 shadow-2xs'
                : 'bg-white border-black/[0.06] hover:border-black/[0.15] hover:bg-black/[0.015] text-neutral-800'
            ]"
          >
            <div class="flex items-center gap-2.5 min-w-0">
              <div
                :class="[
                  'w-7 h-7 rounded-lg flex items-center justify-center shrink-0 transition-colors',
                  activeMenu === 'nodes'
                    ? 'bg-ios-blue text-white shadow-2xs'
                    : 'bg-black/[0.04] text-neutral-600 group-hover:bg-black/[0.08]'
                ]"
              >
                <!-- 节点网络图标 -->
                <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                </svg>
              </div>
              <div class="min-w-0">
                <span class="text-xs font-bold block truncate">组内节点</span>
                <span class="text-xs font-mono opacity-60 block uppercase truncate">NODES</span>
              </div>
            </div>
            <span class="text-xs px-1.5 py-0.5 rounded-md font-mono bg-black/[0.04] text-neutral-600 shrink-0 tabular-nums">
              {{ currentGroupNodes.length }} 个
            </span>
          </button>
        </div>

        <!-- 右侧内容区域：根据 activeMenu 切换展示账号或节点 -->
        <div class="flex-1 min-w-0 h-full overflow-y-auto space-y-4 pr-1">
          <!-- 菜单 1：授权账号 (Accounts) -->
          <div v-if="activeMenu === 'users'" class="space-y-4">
            <div class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 border-b border-black/[0.06] pb-3">
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="text-neutral-900 text-base font-semibold leading-normal">分组授权账号</h3>
                  <span class="px-2 py-0.5 rounded-full text-xs font-mono font-semibold bg-neutral-200/70 text-neutral-700 tabular-nums">
                    {{ selected.grantedUserIds?.length || 0 }}
                  </span>
                </div>
                <p v-if="!selected.enabled || !selected.subscriptionVisible" class="text-xs text-amber-600 mt-1 flex items-center gap-1">
                  <span>⚠️ 分组停用或订阅隐藏时，已授权用户无法通过订阅拉取接入此组节点。</span>
                </p>
                <p v-else class="text-xs text-neutral-400 mt-0.5">
                  已授权用户可在其订阅客户端中获取并使用该分组下的所有节点。
                </p>
              </div>

              <!-- 添加授权快捷工具条 -->
              <div v-if="isOwner && isUserGroup(selected)" class="flex items-center gap-2 shrink-0">
                <AppSelect
                  v-model="grantUserId"
                  :options="grantCandidateOptions"
                  size="sm"
                  class="w-48 sm:w-56"
                  placeholder="选择待授权用户…"
                />
                <button
                  v-if="grantUserId"
                  type="button"
                  class="ios-btn-primary px-3.5 py-1.5 h-8 font-medium inline-flex items-center gap-1 shrink-0 shadow-2xs"
                  :disabled="busy"
                  @click="grantUser"
                >
                  <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                  </svg>
                  <span>添加授权</span>
                </button>
              </div>
            </div>

            <!-- 用户卡片网格 -->
            <div v-if="grantedUsers.length" class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
              <div
                v-for="user in grantedUsers"
                :key="user.id"
                class="flex items-center justify-between p-3 rounded-xl border border-black/[0.06] bg-neutral-50/50 hover:bg-white hover:border-black/[0.12] hover:shadow-2xs transition-all"
              >
                <div class="flex items-center gap-2.5 min-w-0 pr-2">
                  <div class="w-8 h-8 rounded-full bg-blue-50 text-ios-blue font-bold text-xs flex items-center justify-center shrink-0 border border-blue-100">
                    {{ (user.displayName || user.username || '?').charAt(0).toUpperCase() }}
                  </div>
                  <div class="min-w-0">
                    <div class="flex items-center gap-1.5">
                      <span class="text-xs font-semibold text-neutral-800 truncate" :title="user.displayName || user.username">
                        {{ user.displayName || user.username }}
                      </span>
                      <span
                        v-if="user.status === 'expired'"
                        class="px-1.5 py-0.2 rounded text-xs font-medium bg-amber-50 text-amber-700 border border-amber-200/60 shrink-0"
                      >
                        已到期
                      </span>
                      <span
                        v-else-if="user.status === 'disabled'"
                        class="px-1.5 py-0.2 rounded text-xs font-medium bg-neutral-100 text-neutral-500 shrink-0"
                      >
                        已禁用
                      </span>
                      <span
                        v-else
                        class="px-1.5 py-0.2 rounded text-xs font-medium bg-emerald-50 text-emerald-700 border border-emerald-200/60 shrink-0"
                      >
                        正常
                      </span>
                    </div>
                    <span v-if="user.displayName && user.displayName !== user.username" class="text-xs text-neutral-400 font-mono truncate block mt-0.5">
                      @{{ user.username }}
                    </span>
                  </div>
                </div>

                <button
                  v-if="isOwner"
                  type="button"
                  class="h-7 px-2.5 rounded-lg border border-red-200/70 text-red-500 hover:text-red-700 hover:bg-red-50 text-xs font-medium transition-colors shrink-0"
                  :disabled="busy"
                  title="撤销该用户的分组授权"
                  @click="askRevoke(user.id)"
                >
                  撤销
                </button>
              </div>
            </div>
            <div v-else class="py-12 text-center text-xs text-neutral-400 bg-black/[0.01] rounded-2xl border border-dashed border-black/[0.08]">
              暂无授权用户。请在上方选择用户并点击「添加授权」。
            </div>
          </div>

          <!-- 菜单 2：组内节点 (Nodes) -->
          <div v-else-if="activeMenu === 'nodes'" class="space-y-4">
            <div class="flex flex-wrap items-center justify-between gap-3 border-b border-black/[0.06] pb-3">
              <div>
                <div class="flex items-center gap-2">
                  <h3 class="text-neutral-900 text-base font-semibold leading-normal">组内节点列表</h3>
                  <span class="px-2 py-0.5 rounded-full text-xs font-mono font-semibold bg-neutral-200/70 text-neutral-700 tabular-nums">
                    {{ currentGroupNodes.length }}
                  </span>
                </div>
                <p class="text-xs text-neutral-400 mt-0.5">
                  分组内包含的代理节点，可作为用户订阅入口或分流出口。
                </p>
              </div>
              <button
                v-if="isOwner && hasAvailableNodes"
                type="button"
                class="ios-btn-primary px-3.5 py-1.5 h-8 font-medium inline-flex items-center gap-1.5 shadow-2xs"
                :disabled="busy"
                @click="openPicker"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
                </svg>
                <span>添加节点</span>
              </button>
            </div>

            <!-- 节点表格 -->
            <p v-if="currentGroupNodes.some(node => node.invalid)" class="text-sm text-ios-orange">组内有失效成员，请逐个移出后保存。其他成员仍可编辑。</p>
            <div v-if="pendingRemovedIds.length" class="flex flex-wrap items-center gap-3 text-sm">
              <span>已移出 {{ pendingRemovedIds.length }} 个成员，尚未保存。</span>
              <button class="ios-btn-primary" :disabled="busy" @click="saveRemovedMembers">保存成员变更</button>
              <button class="ios-btn-secondary" :disabled="busy" @click="cancelRemovedMembers">取消变更</button>
            </div>
            <div class="rounded-2xl border border-black/[0.06] overflow-hidden bg-white shadow-2xs">
              <DataTable v-if="currentGroupNodes.length" class="border-0 shadow-none rounded-none">
                <template #header>
                  <th>节点名称</th>
                  <th>来源类型</th>
                  <th>协议</th>
                  <th>端口</th>
                  <th>所属服务器 / 来源</th>
                  <th>状态</th>
                  <th v-if="isOwner">操作</th>
                </template>
                <template #body>
                  <tr v-for="node in currentGroupNodes" :key="node.id" class="hover:bg-neutral-50/60 transition-colors">
                    <td>
                      <span class="block text-sm font-semibold text-neutral-800">
                        {{ node.name }}
                      </span>
                      <EntryRouteSummary :summary="node.routeSummary" :show-scheme-link="isOwner" />
                      <span v-if="node.server" class="block text-xs font-mono text-ios-secondary break-all">{{ node.server }}:{{ node.port }}</span>
                      <span v-if="node.internalRelay" class="block text-xs text-ios-orange">
                        内部监听（不应作为用户成员）
                      </span>
                      <span class="block text-xs text-neutral-400 truncate max-w-[220px]" :title="memberServerLabel(node)">
                        {{ memberServerLabel(node) }}
                      </span>
                    </td>
                    <td>
                      <span
                        class="px-2 py-0.5 rounded text-xs font-medium"
                        :class="sourceBadgeClass(node.sourceType)"
                      >
                        {{ node.sourceTypeName }}
                      </span>
                    </td>
                    <td>
                      <span class="inline-flex items-center px-2 py-0.5 rounded-md text-xs font-mono font-bold uppercase bg-blue-50 text-ios-blue border border-blue-200/50">
                        {{ node.protocol }}
                      </span>
                    </td>
                    <td class="font-mono tabular-nums text-xs text-neutral-600">
                      {{ node.port }}
                    </td>
                    <td>
                      <router-link
                        v-if="node.serverId && serverMap[node.serverId]"
                        :to="`/nodes/${node.serverId}`"
                        class="text-xs text-ios-blue hover:underline inline-flex items-center gap-1.5"
                      >
                        <span
                          class="w-1.5 h-1.5 rounded-full inline-block shrink-0"
                          :class="serverMap[node.serverId].status === 'online' ? 'bg-emerald-500' : 'bg-neutral-300' "
                        />
                        <span class="truncate max-w-[160px]">{{ serverMap[node.serverId].name }}</span>
                      </router-link>
                      <span v-else class="text-xs text-neutral-500 font-medium">
                        {{ node.sourceName }}
                      </span>
                    </td>
                    <td>
                      <StatusBadge v-if="node.sourceType === 'self_hosted'" :status="node.enabled ? 'active' : 'disabled'" />
                      <span v-else class="text-xs text-neutral-400">—</span>
                    </td>
                    <td v-if="isOwner">
                      <button
                        type="button"
                        class="h-7 px-2.5 rounded-lg border border-red-200 text-red-600 hover:bg-red-50 text-xs font-medium transition-colors"
                        :disabled="busy"
                        @click="removeNode(node.id)"
                      >
                        移出
                      </button>
                    </td>
                  </tr>
                </template>
              </DataTable>
              <div v-else class="p-8">
                <EmptyState
                  title="暂无组内节点"
                  description="点击右上角「+ 添加节点」将自建入站或导入节点加入此分组。"
                />
              </div>
            </div>

            <!-- 中转 / 出口分组显式服务器成员配置 -->
            <div v-if="!isUserGroup(selected)" class="panel-card p-5 space-y-3 mt-4">
              <p class="text-xs text-neutral-500">中转 / 出口分组不授予用户接入权限。</p>
              <template v-if="selected.serverIds !== undefined">
                <h4 class="text-neutral-800 text-base font-semibold leading-normal">显式服务器成员</h4>
                <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-2 pt-1">
                  <label v-for="server in servers" :key="server.id" class="flex items-center gap-2 text-xs text-neutral-700 p-2 rounded-lg border border-black/[0.06] hover:bg-black/[0.01]">
                    <input v-model="serverSelection" type="checkbox" :value="server.id" :disabled="!isOwner || busy" class="accent-ios-blue" />
                    <span class="truncate">{{ server.name }}</span>
                  </label>
                </div>
                <button v-if="isOwner" class="ios-btn-secondary px-3 py-1.5 h-8 font-medium mt-2" :disabled="busy" @click="saveServers">保存服务器成员</button>
              </template>
            </div>
          </div>
        </div>
      </div>
    </AppDialog>

    <!-- 弹窗组件群 (Modals) -->
    <LineGroupEditor v-model="editorOpen" :group="editing" @saved="saved" />
    <LineGroupPicker
      v-if="pickerGroup"
      v-model="pickerOpen"
      :group="pickerGroup"
      :servers="servers"
      :nodes="allAvailableNodes"
      :current-selected-ids="pickerNodeIds"
      :saving="busy"
      :error="pickerError"
      @save="savePicker"
    />
    <ConfirmDialog v-model="confirmOpen" :title="confirmTitle" :message="confirmMessage" @confirm="runConfirmed" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { request, type Page } from '../../../app/apiClient';
import { showToast } from '../../../app/api';
import { isOwner } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import ErrorState from '../../../components/ErrorState.vue';
import EmptyState from '../../../components/EmptyState.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import AppSelect, { type SelectOption } from '../../../components/AppSelect.vue';
import LineGroupEditor from '../components/LineGroupEditor.vue';
import LineGroupPicker, { type GroupPickerNode } from '../components/LineGroupPicker.vue';
import { groupAPI, freshGroup, type NodeGroup } from '../../nodes/groupsApi';
import { userAPI, type ProxyUser } from '../../users/api';
import type { NodeView } from '../../nodes/api';
import { choices, proxyAPI, type Inbound } from '../api';
import { importSourceKind, loadImports } from '../importApi';
import { proxyNodeStore } from '../proxyNodes';
import { policyAPI, type PolicyRecord, type StoredRuleTemplate } from '../../routing/api';
import { groupInput, importMemberId, isUserGroup, groupPurposeNames, assertValidImportMembers, invalidImportMembers } from '../lineGroups';
import { importProxyKey, resolveImportMember } from '../importIdentity';
import { isUserEntry } from '../userEntries';
import EntryRouteSummary from '../components/EntryRouteSummary.vue';

const route = useRoute();
const router = useRouter();

const groups = ref<NodeGroup[]>([]);
const servers = ref<NodeView[]>([]);
const inbounds = ref<Inbound[]>([]);
const policies = ref<PolicyRecord[]>([]);
const clientTemplate = ref<StoredRuleTemplate | null>(null);

const users = ref<ProxyUser[]>([]);
const grantUserId = ref('');
const serverSelection = ref<string[]>([]);
const loading = ref(true);
const error = ref('');
const busy = ref(false);
const keyword = ref('');
const editorOpen = ref(false);
const editing = ref<NodeGroup | null>(null);

// 弹窗状态与菜单控制
const groupModalVisible = ref(false);
const activeMenu = ref<'users' | 'nodes'>('users');

const pickerOpen = ref(false);
const pickerGroup = ref<NodeGroup | null>(null);
const pickerError = ref('');
const pendingRemovals = ref<Record<string, string[]>>({});
const pendingRemovedIds = computed(() => selected.value ? pendingRemovals.value[selected.value.id] || [] : []);

const serverMap = computed(() => {
  const map: Record<string, NodeView> = {};
  for (const s of servers.value) {
    map[s.id] = s;
  }
  return map;
});

function sourceBadgeClass(type: string) {
  if (type === 'self_hosted') return 'bg-emerald-50 text-emerald-700 border border-emerald-200/60';
  if (type === 'subscription') return 'bg-purple-50 text-purple-700 border border-purple-200/60';
  return 'bg-blue-50 text-ios-blue border border-blue-200/60';
}

// Member names can repeat across servers, so the server (or import source) is shown
// as a secondary line under the name instead of relying on the scrollable column.
function memberServerLabel(node: GroupPickerNode) {
  if (node.serverId && serverMap.value[node.serverId]) return serverMap.value[node.serverId].name;
  return node.sourceName || '—';
}

// Keep internal listeners here so historical group members remain visible and removable.
const allGroupNodes = computed<GroupPickerNode[]>(() => {
  const result: GroupPickerNode[] = [];

  // 1. Self-hosted Inbounds
  for (const ib of inbounds.value) {
    const srv = serverMap.value[ib.nodeId];
    result.push({
      id: ib.id,
      name: ib.name || `${ib.protocol.toUpperCase()}-${ib.port}`,
      protocol: ib.protocol,
      port: ib.port,
      sourceType: 'self_hosted',
      sourceTypeName: '自建',
      sourceName: srv ? srv.name : (ib.nodeId || '自建服务器'),
      serverId: ib.nodeId,
      status: ib.enabled ? 'active' : 'disabled',
      enabled: ib.enabled,
      subscriptionVisible: ib.subscriptionVisible,
      internalRelay: ib.internalRelay,
      routeSummary: isUserEntry(ib) ? ib.routeSummary : undefined
    });
  }

  // Stable API membership keys survive content hash changes on refresh.
  for (const source of proxyNodeStore.subscriptions) {
    if (importSourceKind(source) !== 'url') continue;
    result.push({ id: importMemberId({ sourceId: source.id, proxyKey: '' }),
      sourceId: source.id, proxyKey: '', name: source.name + '（整个来源）', protocol: '—', port: '—',
      sourceType: 'subscription', sourceTypeName: '整源', sourceName: source.name, status: 'active', enabled: true });
  }
  for (const node of proxyNodeStore.manualNodes) {
    if (!node.sourceId || node.proxyKey === undefined) continue;
    const proxyKey = importProxyKey({ id: node.id, proxyKey: node.proxyKey, kind: node.sourceType === 'manual' ? 'content' : 'url' });
    const id = importMemberId({ sourceId: node.sourceId, proxyKey });
    if (result.some(item => item.id === id)) continue;
    result.push({ id, sourceId: node.sourceId, proxyKey, name: node.name,
      protocol: node.protocol, port: node.port, server: node.server, sourceType: node.sourceType,
      sourceTypeName: node.sourceType === 'manual' ? '单独导入' : '机场导入',
      sourceName: node.sourceName || '—', status: 'active', enabled: true });
  }

  return result;
});

const allAvailableNodes = computed(() => allGroupNodes.value.filter(isUserEntry));

const selected = computed(() => groups.value.find(group => group.id === route.query.group) || groups.value[0]);

function memberIds(group: NodeGroup) {
  return [...(group.inboundIds || []), ...(group.imports || []).map(importMemberId)];
}
function groupNodesFor(groupId: string): GroupPickerNode[] {
  const group = groups.value.find(item => item.id === groupId);
  if (!group) return [];
  return memberIds(group).filter(id => !pendingRemovals.value[groupId]?.includes(id)).map(id => {
    const existing = allGroupNodes.value.find(item => item.id === id);
    if (existing) return existing;
    const member = group.imports.find(item => importMemberId(item) === id);
    const source = proxyNodeStore.subscriptions.find(item => item.id === member?.sourceId);
    const resolved = member ? resolveImportMember(member, proxyNodeStore.subscriptions, []) : null;
    return {
      id, name: resolved?.name || `已失效：${id}`, protocol: '—', port: '—',
      sourceId: member?.sourceId, proxyKey: member?.proxyKey,
      sourceType: importSourceKind(source) === 'content' ? 'manual' : 'subscription',
      sourceTypeName: '失效', sourceName: source?.name || '—', status: 'disabled', enabled: false, invalid: true
    };
  });
}
const currentGroupNodes = computed(() => selected.value ? groupNodesFor(selected.value.id) : []);
const hasAvailableNodes = computed(() => allAvailableNodes.value.some(node =>
  !currentGroupNodes.value.some(member => member.id === node.id)
  && (node.sourceType !== 'self_hosted' || (node.enabled && node.subscriptionVisible))
));
const pickerNodeIds = computed(() => pickerGroup.value ? memberIds(pickerGroup.value).filter(id => !pendingRemovals.value[pickerGroup.value!.id]?.includes(id)) : []);

const grantCandidates = computed(() => users.value.filter(user => !selected.value?.grantedUserIds?.includes(user.id)));

const grantCandidateOptions = computed<SelectOption[]>(() => {
  return [
    { value: '', label: '选择待授权用户…' },
    ...grantCandidates.value.map(user => ({
      value: user.id,
      label: user.displayName ? `${user.displayName} (${user.username})` : user.username,
      desc: user.status ? `用户状态: ${user.status}` : undefined
    }))
  ];
});

const grantedUsers = computed(() => {
  const ids = selected.value?.grantedUserIds || [];
  return ids.map(id => {
    const user = users.value.find(u => u.id === id);
    return {
      id,
      username: user?.username || '—',
      displayName: user?.displayName,
      status: user?.status,
      expiresAt: user?.expiresAt
    };
  });
});

watch(() => selected.value, group => {
  serverSelection.value = [...(group?.serverIds || [])];
  grantUserId.value = '';
});

const filteredGroups = computed(() => {
  const q = keyword.value.trim().toLowerCase();
  if (!q) return groups.value;
  return groups.value.filter(group => group.name.toLowerCase().includes(q));
});

const references = computed(() => {
  const id = selected.value?.id;
  if (!id) return [];
  return [
    ...policies.value.filter(policy => policy.scope.group_ids?.includes(id)).map(policy => ({
      key: `policy:${policy.id}`,
      label: `节点策略：${policy.name}${policy.enabled ? '' : '（已停用）'}`,
      path: `/routing/rules/${policy.id}/edit`
    })),
    ...(clientTemplate.value?.template.groups || []).filter(group => group.members.includes(`@nodeGroup:${id}`)).map(group => ({
      key: `client:${group.name}`,
      label: `客户端策略：${group.name}`,
      path: '/routing/client'
    }))
  ];
});

async function load() {
  loading.value = true;
  error.value = '';
  try {
    const data = await Promise.all([
      choices(groupAPI.list),
      choices((page, size) => request<Page<NodeView>>(`/nodes?page=${page}&pageSize=${size}`)),
      choices((page, size) => proxyAPI.inbounds('', page, size)),
      choices((page, size) => policyAPI.list(page, '', size)),
      request<StoredRuleTemplate>('/subscription-rules'),
      loadImports(),
      choices((page, size) => userAPI.list(page, '', size))
    ]);
    [groups.value, servers.value, inbounds.value, policies.value, clientTemplate.value] = data;
    users.value = data[6];
    syncSelection();
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

const ours = () => route.path === '/proxy/groups';
function selectGroup(id: string) {
  if (!ours()) return;
  router.replace({ query: { ...route.query, group: id } });
}

function syncSelection() {
  if (!ours()) return;
  if (selected.value && selected.value.id !== route.query.group) {
    selectGroup(selected.value.id);
  } else if (!groups.value.length && route.query.group) {
    const query = { ...route.query };
    delete query.group;
    router.replace({ query });
  }
}
watch(() => route.query.group, syncSelection);

function openModalForGroup(group: NodeGroup) {
  selectGroup(group.id);
  activeMenu.value = 'users';
  groupModalVisible.value = true;
}

function createGroup() {
  editing.value = null;
  editorOpen.value = true;
}

function editGroupSettings(group: NodeGroup) {
  editing.value = group;
  editorOpen.value = true;
}

async function saved(group: NodeGroup) {
  showToast('节点分组已保存');
  await load();
  selectGroup(group.id);
}

function openPicker() {
  pickerGroup.value = selected.value || null;
  pickerError.value = '';
  pickerOpen.value = true;
}

async function mutate(action: () => Promise<unknown>) {
  if (busy.value || !isOwner.value) return;
  busy.value = true;
  try {
    await action();
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    busy.value = false;
  }
}

async function savePicker(ids: string[]) {
  const group = pickerGroup.value;
  if (!group || busy.value || !isOwner.value) return;
  busy.value = true;
  pickerError.value = '';

  try {
    const fresh = await freshGroup(group.id);
    const input = memberEditInput(fresh);
    for (const id of ids) {
      const candidate = allAvailableNodes.value.find(item => item.id === id);
      if (!candidate) throw new Error('候选节点已变化，请刷新后重试');
      if (candidate.sourceId !== undefined && candidate.proxyKey !== undefined) {
        if (!input.imports.some(item => importMemberId(item) === id)) input.imports.push({ sourceId: candidate.sourceId, proxyKey: candidate.proxyKey });
      } else {
        if (!candidate.enabled || !candidate.subscriptionVisible || candidate.internalRelay) throw new Error('入站不符合用户接入条件');
        if (!input.inboundIds.includes(id)) input.inboundIds.push(id);
      }
    }
    assertValidImportMembers(input.imports, proxyNodeStore.subscriptions, proxyNodeStore.manualNodes);
    await groupAPI.save(group.id, input);
    delete pendingRemovals.value[group.id];

    pickerOpen.value = false;
    showToast(`已向分组添加 ${ids.length} 个代理节点`);
    await load();
  } catch (e) {
    pickerError.value = (e as Error).message;
    await load();
    pickerGroup.value = groups.value.find(item => item.id === group.id) || null;
  } finally {
    busy.value = false;
  }
}

function removeNode(nodeId: string) {
  const group = selected.value;
  if (!group) return;
  if (invalidImportMembers(group.imports, proxyNodeStore.subscriptions, proxyNodeStore.manualNodes).length || pendingRemovedIds.value.length) {
    pendingRemovals.value[group.id] = [...pendingRemovedIds.value, nodeId];
    return;
  }

  mutate(async () => {
    const fresh = await freshGroup(group.id);
    const input = groupInput(fresh);
    input.inboundIds = input.inboundIds.filter(id => id !== nodeId);
    input.imports = input.imports.filter(item => importMemberId(item) !== nodeId);
    assertValidImportMembers(input.imports, proxyNodeStore.subscriptions, proxyNodeStore.manualNodes);
    await groupAPI.save(group.id, input);
    showToast('节点已从分组中移出');
  });
}

function memberEditInput(group: NodeGroup) {
  const input = groupInput(group);
  const removed = pendingRemovals.value[group.id] || [];
  input.inboundIds = input.inboundIds.filter(id => !removed.includes(id));
  input.imports = input.imports.filter(member => !removed.includes(importMemberId(member)));
  return input;
}

function saveRemovedMembers() {
  const group = selected.value;
  if (!group) return;
  mutate(async () => {
    const input = memberEditInput(await freshGroup(group.id));
    assertValidImportMembers(input.imports, proxyNodeStore.subscriptions, proxyNodeStore.manualNodes);
    await groupAPI.save(group.id, input);
    delete pendingRemovals.value[group.id];
    showToast('成员变更已保存');
  });
}

function cancelRemovedMembers() {
  if (selected.value) delete pendingRemovals.value[selected.value.id];
}

const confirmOpen = ref(false);
const confirmTitle = ref('');
const confirmMessage = ref('');
let confirmedAction: () => Promise<unknown> = async () => {};

function askDeleteGroup(group: NodeGroup) {
  confirmTitle.value = '删除节点分组';
  confirmMessage.value = `确认删除“${group.name}”？相关策略引用可能受影响；代理节点本身会保留。`;
  confirmedAction = async () => {
    await groupAPI.delete(group.id);
    if (selected.value?.id === group.id) {
      groupModalVisible.value = false;
    }
    showToast('节点分组已删除');
  };
  confirmOpen.value = true;
}

async function grantUser() {
  const group = selected.value, userId = grantUserId.value;
  if (!group || !userId) return;
  await mutate(() => userAPI.grantGroup(userId, group.id));
  grantUserId.value = '';
}
function askRevoke(userId: string) {
  const group = selected.value;
  if (!group) return;
  const user = users.value.find(u => u.id === userId);
  const name = user?.displayName || user?.username || '该用户';
  confirmTitle.value = '撤销分组授权';
  confirmMessage.value = `确认撤销对“${name}”的分组授权？用户仍可通过其他已授权分组访问相同节点。`;
  confirmedAction = () => userAPI.revokeGroup(userId, group.id);
  confirmOpen.value = true;
}
async function saveServers() {
  const group = selected.value;
  const before = group?.serverIds || [], picked = [...serverSelection.value];
  if (!group || isUserGroup(group) || group.serverIds === undefined) return;
  await mutate(async () => {
    const fresh = await freshGroup(group.id);
    if (fresh.serverIds === undefined || isUserGroup(fresh)) throw new Error('服务器成员能力或分组用途已变化，请刷新');
    const input = groupInput(fresh);
    input.serverIds = [...new Set([...fresh.serverIds.filter(id => !before.includes(id) || picked.includes(id)), ...picked.filter(id => !before.includes(id))])];
    assertValidImportMembers(input.imports, proxyNodeStore.subscriptions, proxyNodeStore.manualNodes);
    await groupAPI.save(group.id, input);
  });
}
function runConfirmed() {
  mutate(confirmedAction);
}

onMounted(load);
</script>
