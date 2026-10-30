<template>
  <div class="space-y-6">
    <!-- Header with Breadcrumbs & Action Toolbar -->
    <PageHeader :title="user?.displayName || user?.username || '用户详情'" parent="用户管理">
      <template #actions>
        <router-link to="/users" class="ios-btn-secondary inline-flex items-center gap-1">
          &larr; 返回列表
        </router-link>

        <button
          v-if="user"
          type="button"
          @click="openRenewDialog"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <span>⏳</span> 续期
        </button>

        <button
          v-if="user"
          type="button"
          @click="openPasswordDialog"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <span>🔑</span> 重置密码
        </button>

        <button
          v-if="user"
          type="button"
          @click="confirmResetTraffic"
          class="ios-btn-secondary inline-flex items-center gap-1.5"
        >
          <span>⚡</span> 清零流量
        </button>

        <button
          v-if="user"
          type="button"
          @click="openEdit"
          class="ios-btn-primary inline-flex items-center gap-1.5"
        >
          <span>✏️</span> 编辑用户
        </button>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="!user" />

    <template v-else>

      <!-- 1. 用量概况 (4 KPI Cards) -->
      <section class="space-y-3">
        <div class="flex items-center justify-between">
          <h2 class="text-base font-bold text-ios-label tracking-tight">用量概况</h2>
          <span class="text-xs text-ios-tertiary">统一监控流量、连接与账号生命周期</span>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 items-stretch">
          <!-- 流量用量与额度 -->
          <div class="panel-card p-5 bg-white flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between">
                <span class="text-xs font-semibold text-ios-tertiary">流量用量与额度</span>
                <span
                  :class="[
                    'text-[10px] font-semibold px-2 py-0.5 rounded-full border',
                    user.trafficLimit > 0 && user.usedTraffic >= user.trafficLimit
                      ? 'bg-rose-50 text-ios-red border-rose-200'
                      : 'bg-blue-50 text-ios-blue border-blue-200'
                  ]"
                >
                  {{ user.trafficLimit > 0 && user.usedTraffic >= user.trafficLimit ? '额度耗尽' : (user.trafficLimit > 0 ? '配额管理' : '不限流量') }}
                </span>
              </div>

              <div class="mt-3 flex items-baseline gap-1.5">
                <span class="text-2xl font-bold text-ios-label tabular-nums tracking-tight">
                  <TrafficValue :bytes="user.usedTraffic" />
                </span>
                <span class="text-xs text-ios-tertiary font-medium">
                  / {{ user.trafficLimit > 0 ? formatGiB(user.trafficLimit) : '无上限' }}
                </span>
              </div>
            </div>

            <div class="mt-4 pt-3 border-t border-black/[0.04] space-y-1.5">
              <div v-if="user.trafficLimit > 0" class="w-full bg-black/[0.05] h-1.5 rounded-full overflow-hidden">
                <div
                  class="h-full rounded-full transition-all duration-300"
                  :class="quotaPercent >= 90 ? 'bg-ios-red' : 'bg-ios-blue'"
                  :style="{ width: `${quotaPercent}%` }"
                ></div>
              </div>
              <div class="flex items-center justify-between text-[11px] text-ios-secondary">
                <span>{{ user.trafficLimit > 0 ? `已消耗 ${quotaPercent}%` : '自由使用' }}</span>
                <span v-if="user.trafficLimit > 0" class="text-ios-tertiary font-mono">
                  剩余 <TrafficValue :bytes="Math.max(0, user.trafficLimit - user.usedTraffic)" />
                </span>
              </div>
            </div>
          </div>

          <!-- 服务有效期限 -->
          <div class="panel-card p-5 bg-white flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between">
                <span class="text-xs font-semibold text-ios-tertiary">服务有效期限</span>
                <span
                  :class="[
                    'text-[10px] font-semibold px-2 py-0.5 rounded-full border',
                    isExpired ? 'bg-rose-50 text-ios-red border-rose-200' : 'bg-emerald-50 text-emerald-600 border-emerald-200'
                  ]"
                >
                  {{ isExpired ? '已过期' : (user.expiresAt ? '正常服务' : '长期有效') }}
                </span>
              </div>

              <div class="mt-3 tracking-tight">
                <div class="text-lg font-bold text-ios-label tabular-nums whitespace-nowrap">
                  {{ user.expiresAt ? (expiryParts.date || '—') : '永不过期' }}
                </div>
                <div v-if="user.expiresAt" class="text-xs font-semibold text-ios-secondary tabular-nums mt-0.5">
                  {{ expiryParts.time }}
                </div>
              </div>
            </div>

            <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-[11px] text-ios-secondary">
              <span>{{ user.expiresAt ? expiryCountdownText : '无需续期' }}</span>
              <span class="text-ios-tertiary font-mono">{{ formatResetCycle(meta) }}</span>
            </div>
          </div>

          <!-- 连接控制 -->
          <div class="panel-card p-5 bg-white flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between">
                <span class="text-xs font-semibold text-ios-tertiary">连接控制</span>
                <span class="text-[10px] font-semibold px-2 py-0.5 rounded-full bg-black/[0.04] text-ios-secondary border border-black/[0.05]">
                  {{ user.deviceLimit === 0 ? '不限设备' : `最多 ${user.deviceLimit} 个地址` }}
                </span>
              </div>

              <div class="mt-3 flex items-baseline gap-1.5">
                <span class="text-2xl font-bold text-ios-label tabular-nums tracking-tight">
                  {{ user.onlineConnections == null ? '未知' : user.onlineConnections }}
                </span>
                <span class="text-xs text-ios-tertiary font-medium">当前连接</span>
              </div>
            </div>

            <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-[11px] text-ios-secondary">
              <span>一个用户一条订阅，可在多台设备上使用</span>
              <span class="text-ios-tertiary">{{ user.onlineConnections == null ? '暂无节点上报' : '节点已上报' }}</span>
            </div>
          </div>

          <!-- 账号状态与权限 -->
          <div class="panel-card p-5 bg-white flex flex-col justify-between">
            <div>
              <div class="flex items-center justify-between">
                <span class="text-xs font-semibold text-ios-tertiary">账号状态</span>
                <StatusBadge :status="effectiveStatus" />
              </div>

              <div class="mt-3 text-base font-bold text-ios-label truncate">
                {{ user.username }}
              </div>
            </div>

            <div class="mt-4 pt-3 border-t border-black/[0.04] flex items-center justify-between text-[11px] text-ios-secondary">
              <span>{{ accountStatusText }}</span>
              <span class="text-ios-tertiary">{{ user.allowedProtocols.length }} 种协议</span>
            </div>
          </div>
        </div>
      </section>

      <!-- 2. 订阅与可用节点 (两栏并列) -->
      <div class="grid grid-cols-1 lg:grid-cols-12 gap-6 items-stretch">
        <!-- 左侧 (7列): 一键订阅配置 -->
        <section class="lg:col-span-7 panel-card p-5 sm:p-6 bg-white flex flex-col justify-between space-y-4">
          <div>
            <div class="flex items-center justify-between">
              <div>
                <h3 class="text-base font-bold text-ios-label tracking-tight">专属订阅与配置</h3>
                <p class="text-xs text-ios-tertiary mt-0.5">一个用户对应一条订阅链接，自动下发所有已授权节点</p>
              </div>

              <button
                type="button"
                :disabled="subRotating"
                @click="rotateConfirmVisible = true"
                class="ios-btn-secondary text-xs h-7 px-2.5 inline-flex items-center gap-1"
                title="重置后旧订阅链接立即失效"
              >
                <svg :class="['w-3 h-3', subRotating ? 'animate-spin' : '']" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                  <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
                </svg>
                <span>{{ subRotating ? '重置中…' : '重置订阅 Token' }}</span>
              </button>
            </div>

            <!-- 订阅链接输入框 -->
            <div class="mt-4 p-3.5 rounded-2xl bg-[#F8F8FA] border border-black/[0.05] space-y-2">
              <div class="flex items-center justify-between text-xs text-ios-tertiary font-medium">
                <span>通用订阅 URL</span>
                <span class="font-mono text-[11px]">{{ subUrl ? '可直接复制或导入' : '重置后可查看' }}</span>
              </div>
              <div class="flex items-center gap-2">
                <input
                  :value="subUrl"
                  readonly
                  class="form-input-base text-xs font-mono select-all bg-white"
                  placeholder="重置后可查看"
                />
                <button
                  type="button"
                  @click="copyText(subUrl)"
                  :disabled="!subUrl"
                  class="ios-btn-primary text-xs shrink-0 px-3 h-9 disabled:opacity-50 disabled:cursor-not-allowed"
                >
                  复制
                </button>
              </div>
            </div>

            <p v-if="linkMessage" class="text-xs text-ios-secondary mt-3">{{ linkMessage }}</p>
            <div v-for="format in subFormats" :key="format.id" class="mt-3 space-y-2">
              <label class="text-xs font-semibold">{{ format.id }}</label>
              <div class="flex gap-2">
                <input readonly :value="absoluteLink(format.url)" class="form-input-base text-xs font-mono select-all" />
                <button class="ios-btn-secondary text-xs" @click="copyText(absoluteLink(format.url))">复制</button>
                <button class="ios-btn-secondary text-xs" @click="formatQR(format.url)">二维码</button>
              </div>
            </div>
          </div>

          <div class="space-y-4 border-t border-black/[0.04] pt-4">
            <div class="flex justify-between items-center"><h2 class="text-base font-semibold">用户门户登录</h2><router-link :to="{ path: '/portal-preview', query: { user: userId } }" class="ios-btn-secondary">只读预览</router-link></div>
            <div class="flex flex-wrap items-center gap-3">
              <label class="text-sm"><input v-model="loginEnabled" type="checkbox" /> 允许登录用户门户</label>
              <input v-model="loginPassword" type="password" autocomplete="new-password" placeholder="设置 / 重置登录密码（留空保留）" class="form-input-base max-w-sm" />
              <button :disabled="loginBusy" class="ios-btn-primary" @click="saveLogin">保存登录设置</button>
              <button :disabled="loginBusy" class="ios-btn-secondary" @click="revokeVisible = true">撤销所有登录会话</button>
            </div>
            <p class="text-xs text-ios-secondary">关闭登录或设置新密码会立即撤销此用户的所有门户会话。代理连接凭证单独管理。</p>
            <p v-if="user.allowedProtocols.includes('wireguard')" class="text-xs text-amber-700">WireGuard 一次只能在一台设备上使用，且不计入设备上限。</p>
          </div>

          <!-- 二维码扫描展示 -->
          <div class="mt-2 pt-3 border-t border-black/[0.04] flex items-center gap-4">
            <div v-if="subQrSvg" v-html="subQrSvg" class="p-2 bg-white rounded-xl shadow-xs border border-black/[0.06] shrink-0"></div>
            <div class="space-y-1 text-xs">
              <p class="font-semibold text-ios-label">扫码快速导入客户端</p>
              <p class="text-ios-tertiary leading-relaxed text-[11px]">
                支持 Shadowrocket、v2rayNG、sing-box 等扫码一键配置。账号凭证信息内置于订阅内。
              </p>
            </div>
          </div>
        </section>

        <!-- 右侧 (5列): 可用节点分组与节点列表 -->
        <section class="lg:col-span-5 panel-card p-5 sm:p-6 bg-white flex flex-col justify-between space-y-4">
          <div>
            <div class="flex items-center justify-between">
              <div>
                <h3 class="text-base font-bold text-ios-label tracking-tight">可用节点</h3>
                <p class="text-xs text-ios-tertiary mt-0.5">该用户有权接入的网络节点</p>
              </div>
              <router-link to="/nodes" class="text-xs font-semibold text-ios-blue hover:underline">
                节点网络 &rarr;
              </router-link>
            </div>

            <!-- 可用节点明细列表 -->
            <div class="mt-3.5 space-y-2 max-h-56 overflow-y-auto pr-1 scrollbar-thin">
              <div
                v-for="node in availableNodes"
                :key="node.id"
                class="p-2.5 rounded-xl border border-black/[0.05] bg-[#F8F8FA] flex items-center justify-between text-xs"
              >
                <div class="min-w-0">
                  <div class="flex items-center gap-1.5">
                    <span :class="['w-1.5 h-1.5 rounded-full shrink-0', node.status === 'online' ? 'bg-emerald-500' : 'bg-ios-red']"></span>
                    <span class="font-semibold text-ios-label truncate">{{ node.name }}</span>
                  </div>
                  <p class="font-mono text-[10px] text-ios-tertiary truncate mt-0.5">
                    {{ node.publicIp || node.hostname || '—' }}
                  </p>
                </div>

                <div class="text-right shrink-0">
                  <span class="text-[10px] font-medium px-1.5 py-0.5 rounded bg-black/[0.04] text-ios-secondary">
                    {{ node.capabilities?.core_name || '—' }}
                  </span>
                </div>
              </div>

              <div v-for="source in availableSources" :key="source.id" class="p-2.5 rounded-xl bg-ios-bg text-xs text-ios-label">
                导入来源：{{ source.name }}<span class="text-ios-secondary">（分组内导入成员；不参与节点额度与设备门控）</span>
              </div>
              <p class="text-xs text-ios-tertiary">按分组与入站授权筛选；订阅内容还需核对已发布凭据及来源可用性。</p>
              <div v-if="availableNodes.length === 0 && availableSources.length === 0" class="text-center py-6 text-xs text-ios-tertiary">
                暂无节点接入该授权组
              </div>
            </div>
          </div>

          <div class="pt-3 border-t border-black/[0.04] text-[11px] text-ios-tertiary flex items-center justify-between">
            <span>共 {{ availableNodes.length }} 台服务器、{{ availableSources.length }} 个导入来源</span>
            <span class="font-mono">协议: {{ user.allowedProtocols.join(' · ') }}</span>
          </div>
        </section>
      </div>

      <!-- 3. 流量历史 (Traffic History) -->
      <section class="panel-card p-5 sm:p-6 bg-white space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <h3 class="text-base font-bold text-ios-label tracking-tight">流量历史与访问</h3>
            <p class="text-xs text-ios-tertiary mt-0.5">记录该账号的流量吞吐分布与近期请求明细</p>
            <p v-if="availableNodes.some(node => node.capabilities?.core_name === 'sing-box' && !node.capabilities?.singbox_user_stats)" class="text-xs text-ios-tertiary mt-0.5">部分 sing-box 节点仅上报节点总量，其流量不计入用户额度。</p>
          </div>
          <router-link to="/access" class="text-xs font-semibold text-ios-blue hover:underline">
            完整审计日志 &rarr;
          </router-link>
        </div>

        <!-- 流量统计指标 -->
        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3 p-4 rounded-2xl bg-[#F8F8FA] border border-black/[0.04]">
          <div>
            <span class="text-[11px] text-ios-tertiary font-medium">↑ 今日上行</span>
            <div class="text-lg font-bold text-ios-label tabular-nums mt-0.5">
              <TrafficValue :bytes="trafficSummary.uploadBytes" />
            </div>
          </div>
          <div>
            <span class="text-[11px] text-ios-tertiary font-medium">↓ 今日下行</span>
            <div class="text-lg font-bold text-ios-label tabular-nums mt-0.5">
              <TrafficValue :bytes="trafficSummary.downloadBytes" />
            </div>
          </div>
          <div>
            <span class="text-[11px] text-ios-tertiary font-medium">采样次数（每 5 秒）</span>
            <div class="text-lg font-bold text-ios-label tabular-nums mt-0.5">
              {{ trafficSummary.observations }} 次
            </div>
          </div>
        </div>

        <!-- 最近访问明细表格 -->
        <div v-if="recentAccessList.length > 0" class="overflow-x-auto">
          <table class="w-full text-xs text-left">
            <thead>
              <tr class="text-ios-tertiary border-b border-black/[0.05]">
                <th class="py-2 font-medium">目标域名 / 地址</th>
                <th class="py-2 font-medium">接入协议</th>
                <th class="py-2 font-medium">状态</th>
                <th class="py-2 font-medium text-right font-mono">时间</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-black/[0.03]">
              <tr v-for="rec in recentAccessList" :key="rec.id" class="hover:bg-black/[0.015]">
                <td class="py-2.5 font-mono text-ios-label font-medium truncate max-w-xs">
                  {{ rec.domain || rec.target_ip }}
                </td>
                <td class="py-2.5 text-ios-secondary">
                  <span class="px-1.5 py-0.5 rounded text-[10px] bg-black/[0.04] font-mono">
                    {{ rec.inbound_protocol }}
                  </span>
                </td>
                <td class="py-2.5">
                  <span
                    :class="[
                      'text-[10px] font-semibold px-1.5 py-0.2 rounded shrink-0',
                      rec.allowed ? 'bg-emerald-50 text-emerald-700' : 'bg-rose-50 text-ios-red'
                    ]"
                  >
                    {{ rec.allowed ? '通过' : '阻断' }}
                  </span>
                </td>
                <td class="py-2.5 text-right font-mono text-ios-tertiary">
                  {{ rec.bucket ? rec.bucket.slice(11, 19) : '刚刚' }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-6 text-xs text-ios-tertiary">
          暂无该用户的网络访问明细上报
        </div>
      </section>

      <!-- 4. 管理记录 (Management Records) -->
      <section class="panel-card p-5 sm:p-6 bg-white space-y-4">
        <div class="flex items-center justify-between">
          <div>
            <h3 class="text-base font-bold text-ios-label tracking-tight">管理记录</h3>
            <p class="text-xs text-ios-tertiary mt-0.5">管理员对该用户账号的配置变更与安全审计日志</p>
          </div>
          <router-link to="/system/operations" class="text-xs font-semibold text-ios-blue hover:underline">
            所有操作日志 &rarr;
          </router-link>
        </div>

        <div v-if="userOperations.length > 0" class="overflow-x-auto">
          <table class="w-full text-xs text-left">
            <thead>
              <tr class="text-ios-tertiary border-b border-black/[0.05]">
                <th class="py-2 font-medium">操作时间</th>
                <th class="py-2 font-medium">操作管理员</th>
                <th class="py-2 font-medium">动作</th>
                <th class="py-2 font-medium">结果</th>
                <th class="py-2 font-medium text-right font-mono">来源 IP</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-black/[0.03]">
              <tr v-for="op in userOperations" :key="op.id" class="hover:bg-black/[0.015]">
                <td class="py-2.5 font-mono text-ios-tertiary">
                  {{ formatTime(op.timestamp) }}
                </td>
                <td class="py-2.5 font-medium text-ios-label">
                  {{ op.operator }}
                </td>
                <td class="py-2.5">
                  <span class="px-2 py-0.5 rounded-md text-[11px] font-medium bg-black/[0.04] text-ios-secondary">
                    {{ op.action }}
                  </span>
                </td>
                <td class="py-2.5">
                  <span
                    :class="[
                      'text-[10px] font-semibold px-2 py-0.5 rounded-full border',
                      op.result === 'success' || op.result === '成功'
                        ? 'bg-emerald-50 text-emerald-800 border-emerald-200'
                        : 'bg-rose-50 text-rose-800 border-rose-200'
                    ]"
                  >
                    {{ op.result === 'success' ? '成功' : op.result }}
                  </span>
                </td>
                <td class="py-2.5 text-right font-mono text-ios-tertiary">
                  {{ op.ip }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="text-center py-6 text-xs text-ios-tertiary">
          暂无该用户的管理审计记录
        </div>
      </section>
    </template>

    <!-- 弹窗 1: 续期 -->
    <AppDialog
      v-model="renewDialogOpen"
      title="用户账号续期"
      :description="`为用户 ${user?.displayName || user?.username || ''} 延长服务有效期。`"
      :loading="renewBusy"
      @confirm="submitRenew"
      confirm-text="确认续期"
    >
      <div class="space-y-4 pt-1">
        <div class="p-3.5 rounded-2xl bg-[#F8F8FA] border border-black/[0.05] text-xs flex items-center justify-between">
          <span class="text-ios-tertiary">当前到期时间:</span>
          <span class="font-mono font-medium text-ios-label">
            {{ user?.expiresAt ? formatTime(user.expiresAt) : '永不过期' }}
          </span>
        </div>

        <div class="space-y-1.5">
          <label class="text-xs font-medium text-ios-secondary">选择延长时长</label>
          <div class="grid grid-cols-4 gap-2">
            <button
              v-for="days in [30, 90, 180, 365]"
              :key="days"
              type="button"
              @click="applyRenewDays(days)"
              class="p-2.5 rounded-xl border text-xs font-semibold transition-all"
              :class="selectedRenewDays === days ? 'bg-ios-blue text-white border-ios-blue shadow-2xs' : 'bg-white text-ios-label border-black/[0.08] hover:border-ios-blue'"
            >
              +{{ days === 365 ? '1年' : `${days / 30}个月` }}
            </button>
          </div>
        </div>

        <label class="block text-xs font-medium text-ios-secondary">
          续期后到期时间
          <input v-model="renewTargetTime" type="datetime-local" class="form-input-base mt-1.5 font-mono" />
        </label>
      </div>
    </AppDialog>

    <!-- 弹窗 2: 重置连接密码 -->
    <AppDialog
      v-model="passwordDialogOpen"
      title="重置连接密码"
      :description="`为用户 ${user?.username || ''} 重新生成其允许协议的连接凭证（UUID / 密码）。`"
      :loading="passwordBusy"
      @confirm="submitResetPassword"
      confirm-text="重置凭证"
    >
      <div class="space-y-4 pt-1">
        <div class="p-3.5 rounded-2xl bg-amber-50/70 border border-amber-200/60 text-xs text-amber-800 space-y-1">
          <p class="leading-relaxed text-[11px]">重置会为该用户的所有协议签发新凭证，并自动发布到其授权节点；节点应用后旧凭证失效，客户端更新订阅即可获得新凭证。</p>
        </div>
      </div>
    </AppDialog>

    <!-- 弹窗 3: 编辑抽屉 -->
    <FormDrawer
      v-model="drawer"
      width="xl"
      title="编辑用户"
      :loading="saving"
      @confirm="save"
      confirm-text="保存修改"
    >
      <div class="space-y-4 text-sm">
        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            用户名
            <input :value="form.username" disabled class="form-input-base mt-1.5 font-mono bg-black/[0.03]" />
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            显示名称 / 备注别名
            <input v-model="form.displayName" maxlength="80" class="form-input-base mt-1.5" />
          </label>
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            账号状态
            <select v-model="form.status" class="form-input-base mt-1.5">
              <option value="active">启用 (正常接入)</option>
              <option value="disabled">禁用 (中断连接)</option>
            </select>
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            到期时间（{{ authState.timezone }}）
            <input v-model="expiresLocal" type="datetime-local" class="form-input-base mt-1.5 font-mono" />
          </label>
        </div>

        <div class="grid sm:grid-cols-2 gap-4">
          <label class="block text-xs font-medium text-ios-secondary">
            流量额度 (GiB，0 为不限额)
            <input v-model.number="trafficGiB" type="number" min="0" step="0.001" class="form-input-base mt-1.5 font-mono" />
          </label>
          <label class="block text-xs font-medium text-ios-secondary">同时在线设备上限（0 为不限）
            <input v-model.number="form.deviceLimit" type="number" min="0" max="100" step="1" class="form-input-base mt-1.5" />
          </label>
          <label class="block text-xs font-medium text-ios-secondary">
            流量重置周期
            <select v-model="formMeta.resetCycle" class="form-input-base mt-1.5">
              <option value="none">不自动重置</option>
              <option value="monthly">每月锚点日重置（月末自动调整）</option>
              <option value="30days">每 30 天自动重置</option>
            </select>
          </label>
        </div>



        <label class="block text-xs font-medium text-ios-secondary">
          管理备注
          <textarea v-model="formMeta.rawRemark" rows="2" class="form-input-base mt-1.5"></textarea>
        </label>
      </div>
    </FormDrawer>

    <!-- 弹窗 4: 确认清零流量 -->
    <ConfirmDialog
      v-model="resetTrafficVisible"
      title="清零已用流量"
      :message="`确定要将用户 ${user?.displayName || user?.username || ''} 的已用流量立即清零吗？此操作无法撤销。`"
      @confirm="submitResetTraffic"
    />

    <ConfirmDialog v-model="revokeVisible" title="撤销登录会话" message="此用户的所有门户会话将立即退出。确定继续？" @confirm="revokeSessions" />

    <ConfirmDialog
      v-model="rotateConfirmVisible"
      title="重置订阅链接"
      message="重置后旧订阅链接立即失效，使用旧链接的客户端需要重新导入。确定继续？"
      @confirm="doRotateSubscription"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, reactive, onMounted, onUnmounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import {
  userAPI,
  type ProxyUser,
  type UserInput,
  type UserMeta
} from '../api';
import { proxyAPI, type Inbound } from '../../proxy/api';
import { importAPI, type ImportSource, type ImportedProxy } from '../../proxy/importApi';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { accessAPI, type AccessRecord } from '../../access/api';
import { request, APIError, type Page } from '../../../app/apiClient';
import { authState, formatTime } from '../../../app/auth';
import { localInput, utcInput, formatMinuteParts } from '../../../app/time';
import { generateQRCodeSVG } from '../../../app/qrcode';
import { showToast } from '../../../app/api';
import PageHeader from '../../../components/PageHeader.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import TrafficValue from '../../../components/TrafficValue.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import AppDialog from '../../../components/AppDialog.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

interface Operation {
  id: number;
  timestamp: string;
  operator: string;
  action: string;
  target: string;
  result: string;
  ip: string;
}

const route = useRoute();
const router = useRouter();
const userId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''));

const user = ref<ProxyUser | null>(null);
const groups = ref<NodeGroup[]>([]);
const allNodes = ref<NodeView[]>([]);
const memberInbounds = ref<Inbound[]>([]);
const importedSources = ref<ImportSource[]>([]);
const importedProxies = ref<ImportedProxy[]>([]);
const error = ref('');

// 订阅与二维码
const subUrl = ref('');
const subQrSvg = ref('');
const subRotating = ref(false);
const subFormats = ref<{ id: string; url: string }[]>([]);
const linkMessage = ref('');
const loginEnabled = ref(false), loginPassword = ref(''), loginBusy = ref(false), revokeVisible = ref(false);
function absoluteLink(path: string) { return new URL(path, window.location.origin).href; }
function formatQR(path: string) {
  try { subQrSvg.value = generateQRCodeSVG(absoluteLink(path), 160); }
  catch { subQrSvg.value = ''; showToast('链接过长，无法生成二维码，请复制链接。', 'warning'); }
}
async function loadSubscription() {
  subUrl.value = ''; subFormats.value = []; subQrSvg.value = ''; linkMessage.value = '';
  try {
    const link = await request<{ path: string; formats: { id: string; url: string }[] }>(`/users/${userId.value}/subscription-link`);
    subUrl.value = absoluteLink(link.path); subFormats.value = link.formats;
  } catch (e) {
    if (e instanceof APIError && e.code === 'link_not_recoverable') linkMessage.value = '重置后可查看';
    else if (e instanceof APIError && e.status === 404) linkMessage.value = '尚无订阅链接，请重置以创建';
    else linkMessage.value = (e as Error).message;
  }
}
async function saveLogin() {
  loginBusy.value = true;
  try {
    await request(`/users/${userId.value}/login`, 'PUT', { enabled: loginEnabled.value, ...(loginPassword.value ? { password: loginPassword.value } : {}) });
    loginPassword.value = ''; await load(); showToast('登录设置已保存');
  } catch (e) { showToast((e as Error).message, 'error'); }
  finally { loginBusy.value = false; }
}
async function revokeSessions() {
  loginBusy.value = true;
  try { await request(`/users/${userId.value}/sessions`, 'DELETE'); revokeVisible.value = false; showToast('所有门户会话已撤销'); }
  catch (e) { showToast((e as Error).message, 'error'); }
  finally { loginBusy.value = false; }
}

// 流量历史统计与访问
const trafficSummary = ref<{ uploadBytes: number; downloadBytes: number; observations: number }>({
  uploadBytes: 0,
  downloadBytes: 0,
  observations: 0
});
const recentAccessList = ref<AccessRecord[]>([]);

// 管理记录
const userOperations = ref<Operation[]>([]);

// 计算属性
const meta = computed<UserMeta>(() => {
  return { resetCycle: user.value?.trafficResetCycle ?? 'none', rawRemark: user.value?.remark ?? '' };
});

const isExpired = computed(() => {
  if (!user.value?.expiresAt) return false;
  return new Date(user.value.expiresAt).getTime() <= Date.now();
});

const quotaPercent = computed(() => {
  if (!user.value || !user.value.trafficLimit) return 0;
  return Math.min(100, Math.round((user.value.usedTraffic / user.value.trafficLimit) * 100));
});

const expiryCountdownText = computed(() => {
  if (!user.value?.expiresAt) return '永不过期';
  const diff = new Date(user.value.expiresAt).getTime() - Date.now();
  if (diff < 0) return '已过期';
  const days = Math.floor(diff / (1000 * 3600 * 24));
  return `还剩 ${days} 天到期`;
});

// Narrow KPI card: the date is the headline and the time sits on a second line so
// nothing is clipped at desktop widths.
const expiryParts = computed(() => formatMinuteParts(user.value?.expiresAt || ''));

// Same rule as the user list: an active user past expiresAt shows as expired
// before the backend's state machine has caught up.
const effectiveStatus = computed(() => {
  const u = user.value;
  if (!u) return '';
  return u.status === 'active' && u.expiresAt && Date.parse(u.expiresAt) <= Date.now() ? 'expired' : u.status;
});

const accountStatusText = computed(() => {
  const u = user.value;
  if (!u) return '—';
  if (u.status !== 'active') return u.status === 'disabled' ? '账号已禁用' : u.status === 'expired' ? '账号已到期' : u.status === 'over_quota' ? '流量已用完' : u.status;
  if (u.expiresAt && Date.parse(u.expiresAt) <= Date.now()) return '账号已到期';
  if (u.trafficLimit > 0 && u.usedTraffic >= u.trafficLimit) return '流量已用完';
  return '账号正常';
});
const accessGroups = computed(() => groups.value.filter(g => user.value?.nodeGroupIds.includes(g.id) && g.enabled && g.subscriptionVisible && (g.purpose === 'subscription' || g.purpose === 'mixed')));
const availableSources = computed(() => importedSources.value.filter(source => accessGroups.value.some(g => g.imports?.some(item => item.sourceId === source.id && importedProxies.value.some(proxy => proxy.sourceId === item.sourceId && (item.proxyKey === '' || item.proxyKey === proxy.proxyKey) && user.value?.allowedProtocols.includes(proxy.protocol))))));
const availableNodes = computed(() => {
  if (!user.value) return [];
  const inboundIds = new Set(accessGroups.value.flatMap(g => g.inboundIds || []));
  const grantedNodes = new Set(memberInbounds.value.filter(ib => inboundIds.has(ib.id) && ib.enabled && ib.subscriptionVisible && !ib.internalRelay &&
    (!ib.allowedUserIds.length || ib.allowedUserIds.includes(user.value!.id)) && user.value!.allowedProtocols.includes(ib.protocol)).map(ib => ib.nodeId));
  return allNodes.value.filter(n => grantedNodes.has(n.id) && !n.deletedAt && n.enabled && !n.maintenance && n.allowSubscription);
});

function formatGiB(bytes: number) {
  return (bytes / 1073741824).toFixed(1) + ' GiB';
}

function getGroupName(groupId: string): string {
  const g = groups.value.find(item => item.id === groupId);
  return g ? g.name : groupId;
}



function formatResetCycle(meta: UserMeta): string {
  if (meta.resetCycle === 'monthly') return '每月锚点日重置（月末自动调整）';
  if (meta.resetCycle === '30days') return '每30天重置';
  return '不自动重置';
}

// 续期弹窗状态
const renewDialogOpen = ref(false);
const renewBusy = ref(false);
const selectedRenewDays = ref(30);
const renewTargetTime = ref('');

function openRenewDialog() {
  if (!user.value) return;
  selectedRenewDays.value = 30;
  applyRenewDays(30);
  renewDialogOpen.value = true;
}

function applyRenewDays(days: number) {
  selectedRenewDays.value = days;
  const baseTime = user.value?.expiresAt && new Date(user.value.expiresAt).getTime() > Date.now()
    ? new Date(user.value.expiresAt).getTime()
    : Date.now();
  const targetDate = new Date(baseTime + days * 24 * 60 * 60 * 1000);
  renewTargetTime.value = localInput(targetDate.toISOString());
}

async function submitRenew() {
  if (!user.value) return;
  renewBusy.value = true;
  try {
    const updatedExpiry = renewTargetTime.value ? utcInput(renewTargetTime.value) : '';
    await userAPI.save(user.value.id, {
      username: user.value.username,
      trafficResetCycle: user.value.trafficResetCycle,
      trafficResetAnchor: user.value.trafficResetAnchor,
      displayName: user.value.displayName,
      status: user.value.status,
      expiresAt: updatedExpiry,
      trafficLimit: user.value.trafficLimit,
 deviceLimit: user.value.deviceLimit,
      allowedProtocols: user.value.allowedProtocols,
      nodeGroupIds: user.value.nodeGroupIds,
      remark: user.value.remark
    });
    showToast(`续期成功，服务有效期已延长至 ${formatTime(updatedExpiry)}`);
    renewDialogOpen.value = false;
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    renewBusy.value = false;
  }
}

// 重置密码弹窗状态
const passwordDialogOpen = ref(false);
const passwordBusy = ref(false);

function openPasswordDialog() {
  passwordDialogOpen.value = true;
}

async function submitResetPassword() {
  if (!user.value) return;
  passwordBusy.value = true;
  try {
    // One call re-issues every credential and queues the automatic revision;
    // the old settings stop working once the nodes have applied it.
    await userAPI.resetCredentials(user.value.id);
    showToast('连接凭证已重置，节点应用新配置后旧凭证失效');
    passwordDialogOpen.value = false;
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    passwordBusy.value = false;
  }
}

// 清零流量
const resetTrafficVisible = ref(false);

function confirmResetTraffic() {
  resetTrafficVisible.value = true;
}

async function submitResetTraffic() {
  if (!user.value) return;
  try {
    await userAPI.resetTraffic(user.value.id);
    showToast(`用户 ${user.value.username} 已用流量已清零`);
    resetTrafficVisible.value = false;
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

// 重置订阅 Token
const rotateConfirmVisible = ref(false);

async function doRotateSubscription() {
  if (!user.value) return;
  subRotating.value = true;
  try {
    await userAPI.rotateToken(user.value.id);
    rotateConfirmVisible.value = false;
    await loadSubscription();
    showToast('专属订阅 Token 重置成功，旧链接已失效');
  } catch (e) {
    showToast('重置订阅失败: ' + (e as Error).message, 'error');
  } finally {
    subRotating.value = false;
  }
}

async function rotateSubscription() {
  rotateConfirmVisible.value = true;
}

// 编辑抽屉状态
const drawer = ref(false);
const saving = ref(false);
const expiresLocal = ref('');
const trafficGiB = ref(0);
let originalTrafficGiB = 0;

const form = reactive<UserInput>({
  username: '',
  displayName: '',
  status: 'active',
  expiresAt: '',
  trafficLimit: 0,
 deviceLimit: 0,
  allowedProtocols: ['socks5'],
  nodeGroupIds: [],
  remark: ''
});

const formMeta = reactive<UserMeta>({
  resetCycle: 'none',
  rawRemark: ''
});

function openEdit() {
  if (!user.value) return;
  form.username = user.value.username;
  form.displayName = user.value.displayName;
  form.status = user.value.status;
  form.expiresAt = user.value.expiresAt;
  form.trafficLimit = user.value.trafficLimit;
 form.deviceLimit = user.value.deviceLimit;
  form.allowedProtocols = [...user.value.allowedProtocols];
  form.nodeGroupIds = [...user.value.nodeGroupIds];

  Object.assign(formMeta, { resetCycle: user.value.trafficResetCycle, rawRemark: user.value.remark });
  form.trafficResetAnchor = user.value.trafficResetAnchor;

  expiresLocal.value = user.value.expiresAt ? localInput(user.value.expiresAt) : '';
  trafficGiB.value = user.value.trafficLimit ? +(user.value.trafficLimit / 1073741824).toFixed(3) : 0;
  originalTrafficGiB = trafficGiB.value;
  drawer.value = true;
}

async function save() {
  if (!user.value) return;
  saving.value = true;
  try {
    form.expiresAt = expiresLocal.value ? utcInput(expiresLocal.value) : '';
    form.trafficLimit = trafficGiB.value === originalTrafficGiB ? (user.value.trafficLimit) : (trafficGiB.value > 0 ? Math.round(trafficGiB.value * 1073741824) : 0);
    form.remark = formMeta.rawRemark;
    form.trafficResetCycle = formMeta.resetCycle;
    if (form.trafficResetCycle !== user.value.trafficResetCycle) form.trafficResetAnchor = '';

    await userAPI.save(user.value.id, form);
    showToast('用户配置保存成功');
    drawer.value = false;
    await load();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

async function copyText(text: string) {
  if (!text) return;
  try {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      await navigator.clipboard.writeText(text);
    } else {
      const el = document.createElement('textarea');
      el.value = text;
      document.body.appendChild(el);
      el.select();
      document.execCommand('copy');
      document.body.removeChild(el);
    }
    showToast('已复制到剪贴板');
  } catch {
    showToast('复制失败，请手动选择复制', 'error');
  }
}

// 主数据加载
async function readPages<T>(fetchPage: (page: number) => Promise<{ items: T[]; total: number }>): Promise<{ items: T[] }> {
  const first = await fetchPage(1), items = [...first.items];
  for (let page = 2; items.length < first.total; page++) {
    const next = await fetchPage(page);
    if (!next.items.length) throw new Error('授权节点分页读取不完整');
    items.push(...next.items);
  }
  return { items };
}
async function load() {
  const id = userId.value;
  if (!id || route.name !== 'UserDetail') return;
  error.value = '';
  try {
    const [u, gRes, nRes, ibRes, sourceRes, proxyRes] = await Promise.all([
      userAPI.get(id),
      readPages(page => groupAPI.list(page, 100)),
      readPages(page => nodeAPI.list(page, '', false)),
      readPages(page => proxyAPI.inbounds('', page, 100)),
      importAPI.list(),
      importAPI.proxies()
    ]);
    user.value = u;
    loginEnabled.value = u.loginEnabled;
    groups.value = gRes.items;
    allNodes.value = nRes.items;
    memberInbounds.value = ibRes.items;
    importedSources.value = sourceRes.items;
    importedProxies.value = proxyRes.items;

    await loadSubscription();

    // 获取流量历史与请求
    void accessAPI.accounts({ userId: u.id }).then(summary => {
      trafficSummary.value = {
        uploadBytes: summary.uploadBytes || 0,
        downloadBytes: summary.downloadBytes || 0,
        observations: summary.observations || 0
      };
    }).catch(() => {});

    void accessAPI.list({ userId: u.id }, 1).then(res => {
      recentAccessList.value = res.items.slice(0, 10);
    }).catch(() => {});

    // 获取管理员对该用户的操作记录
    Promise.all([
      request<Page<Operation>>(`/operations?page=1&pageSize=10&keyword=${encodeURIComponent(u.username)}`),
      request<Page<Operation>>(`/operations?page=1&pageSize=10&keyword=${encodeURIComponent(u.id)}`)
    ]).then(([res1, res2]) => {
      const map = new Map<number, Operation>();
      for (const it of res1.items) map.set(it.id, it);
      for (const it of res2.items) map.set(it.id, it);
      const merged = Array.from(map.values());
      merged.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime());
      userOperations.value = merged.slice(0, 10);
    }).catch(() => {});

  } catch (e) {
    error.value = (e as Error).message;
  }
}

onMounted(() => {
  void load();
  window.addEventListener('proxy-system-alert', load);
});
onUnmounted(() => {
  window.removeEventListener('proxy-system-alert', load);
});
</script>
