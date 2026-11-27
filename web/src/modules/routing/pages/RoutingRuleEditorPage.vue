<template>
  <div class="space-y-6">
    <PageHeader
      :title="editingId ? '编辑节点策略' : '新建节点策略'"
      parent="分流控制"
      description="配置服务器端的分流与访问控制规则。保存为草稿后，需发布到对应节点生效。"
    >
      <template #actions>
        <router-link to="/routing/rules" class="ios-btn-secondary">
          返回节点策略
        </router-link>
      </template>
    </PageHeader>

    <ErrorState v-if="error" :message="error" @retry="load" />
    <LoadingSkeleton v-else-if="loading && !detailReady" />

    <form v-else @submit.prevent="save" class="space-y-5">
      <!-- 1. 基础信息 -->
      <section class="panel-card p-6">
        <div class="flex flex-wrap items-center justify-between gap-2 pb-3.5 mb-5 border-b border-black/[0.06]">
          <h2 class="text-base font-semibold leading-normal text-ios-label">基础信息</h2>
          <span
            class="inline-flex items-center gap-1.5 px-2.5 min-h-[24px] rounded-full text-xs font-medium leading-normal border"
            :class="form.enabled
              ? 'bg-ios-green/10 text-ios-green border-ios-green/20'
              : 'bg-ios-bg text-ios-tertiary border-black/[0.08]'"
          >
            <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="form.enabled ? 'bg-ios-green' : 'bg-ios-quaternary'"></span>
            <span>{{ form.enabled ? '策略已启用' : '策略已禁用' }}</span>
          </span>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-12 gap-4 items-start">
          <label class="block md:col-span-6">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">策略名称</span>
            <input
              v-model="form.name"
              required
              maxlength="80"
              placeholder="例如：海外流媒体解锁出口 / 广告域名拦截"
              class="form-input-base"
            />
          </label>

          <label class="block md:col-span-3">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">优先级</span>
            <input
              v-model.number="form.priority"
              type="number"
              min="0"
              max="1000000"
              required
              class="form-input-base font-mono tabular-nums"
            />
            <span class="block text-xs text-ios-tertiary mt-1.5">数字越小越先匹配</span>
          </label>

          <div class="md:col-span-3">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">运行开关</span>
            <label
              class="flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl border transition-colors cursor-pointer select-none"
              :class="form.enabled
                ? 'bg-ios-green/[0.07] border-ios-green/30 text-ios-label'
                : 'bg-ios-bg border-ios-border/70 text-ios-secondary hover:border-ios-quaternary'"
            >
              <span class="text-sm font-medium">启用此节点策略</span>
              <input v-model="form.enabled" type="checkbox" class="w-4 h-4 shrink-0" />
            </label>
          </div>
        </div>
      </section>

      <!-- 2. 生效范围 -->
      <section class="panel-card p-6">
        <div class="flex flex-wrap items-start justify-between gap-2 pb-3.5 mb-5 border-b border-black/[0.06]">
          <div>
            <h2 class="text-base font-semibold leading-normal text-ios-label">生效范围</h2>
            <p class="text-xs text-ios-tertiary mt-0.5">点击卡片多选；不选表示适用于全部，同时限定多个类别时取交集生效。</p>
          </div>
          <span
            class="inline-flex items-center px-2.5 min-h-[24px] rounded-full text-xs font-medium leading-normal border tabular-nums"
            :class="selectedUserCount || selectedNodeCount || selectedGroupCount
              ? 'bg-ios-blue/10 text-ios-blue border-ios-blue/20'
              : 'bg-ios-bg text-ios-secondary border-black/[0.06]'"
          >
            {{ scopeSummaryText }}
          </span>
        </div>

        <div class="space-y-3.5">
          <!-- 限定用户 -->
          <div role="group" aria-label="限定用户" class="p-3 rounded-2xl bg-ios-bg/75 border border-black/[0.05]">
            <div class="flex items-center justify-between gap-2 mb-2.5">
              <span class="text-xs font-medium text-ios-secondary">限定用户</span>
              <div class="flex items-center gap-2">
                <span
                  class="px-2 py-0.5 rounded-full text-xs font-medium tabular-nums"
                  :class="selectedUserCount ? 'bg-ios-blue/10 text-ios-blue' : 'bg-white text-ios-tertiary border border-black/[0.06]'"
                >
                  {{ selectedUserCount ? `已选 ${selectedUserCount} / ${users.length}` : '全部用户' }}
                </span>
                <button
                  v-if="selectedUserCount"
                  type="button"
                  class="text-xs text-ios-tertiary hover:text-ios-label transition-colors"
                  @click="form.scope.user_ids = []"
                >
                  清空
                </button>
              </div>
            </div>
            <div v-if="orderedUsers.length" class="grid grid-cols-[repeat(auto-fill,minmax(145px,1fr))] gap-2 max-h-56 overflow-y-auto p-0.5">
              <label
                v-for="user in orderedUsers"
                :key="user.id"
                :title="user.displayName && user.displayName !== user.username ? `${user.username} (${user.displayName})` : user.username"
                class="group relative flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl border transition-all duration-150 cursor-pointer select-none active:scale-[0.98] has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ios-blue/30"
                :class="isSelected(form.scope.user_ids, user.id)
                  ? 'bg-ios-blue/[0.08] border-ios-blue shadow-xs'
                  : 'bg-white border-black/[0.08] shadow-xs hover:border-ios-border hover:bg-white/90'"
              >
                <input v-model="form.scope.user_ids" type="checkbox" :value="user.id" class="sr-only" />
                <div class="min-w-0">
                  <div
                    class="text-sm leading-snug truncate"
                    :class="isSelected(form.scope.user_ids, user.id) ? 'font-semibold text-ios-blue' : 'font-medium text-ios-label'"
                  >
                    {{ user.username }}
                  </div>
                  <div
                    v-if="user.displayName && user.displayName !== user.username"
                    class="text-xs text-ios-tertiary truncate mt-0.5"
                  >
                    {{ user.displayName }}
                  </div>
                </div>
                <span
                  class="w-4 h-4 rounded-full border flex items-center justify-center shrink-0 transition-colors"
                  :class="isSelected(form.scope.user_ids, user.id)
                    ? 'bg-ios-blue border-ios-blue text-white'
                    : 'bg-white border-ios-border group-hover:border-ios-quaternary'"
                >
                  <svg v-if="isSelected(form.scope.user_ids, user.id)" class="w-2.5 h-2.5" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M2.5 6.2 4.8 8.5 9.5 3.5" />
                  </svg>
                </span>
              </label>
            </div>
            <div v-else class="text-xs text-ios-quaternary py-2">暂无用户</div>
          </div>

          <!-- 限定入口服务器节点 -->
          <div role="group" aria-label="限定入口服务器节点" class="p-3 rounded-2xl bg-ios-bg/75 border border-black/[0.05]">
            <div class="flex items-center justify-between gap-2 mb-2.5">
              <span class="text-xs font-medium text-ios-secondary">限定入口服务器节点</span>
              <div class="flex items-center gap-2">
                <span
                  class="px-2 py-0.5 rounded-full text-xs font-medium tabular-nums"
                  :class="selectedNodeCount ? 'bg-ios-blue/10 text-ios-blue' : 'bg-white text-ios-tertiary border border-black/[0.06]'"
                >
                  {{ selectedNodeCount ? `已选 ${selectedNodeCount} / ${nodes.length}` : '全部节点' }}
                </span>
                <button
                  v-if="selectedNodeCount"
                  type="button"
                  class="text-xs text-ios-tertiary hover:text-ios-label transition-colors"
                  @click="form.scope.node_ids = []"
                >
                  清空
                </button>
              </div>
            </div>
            <div v-if="orderedNodes.length" class="grid grid-cols-[repeat(auto-fill,minmax(168px,1fr))] gap-2 max-h-60 overflow-y-auto p-0.5">
              <label
                v-for="node in orderedNodes"
                :key="node.id"
                :title="node.name"
                class="group relative flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl border transition-all duration-150 cursor-pointer select-none active:scale-[0.98] has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ios-blue/30"
                :class="isSelected(form.scope.node_ids, node.id)
                  ? 'bg-ios-blue/[0.08] border-ios-blue shadow-xs'
                  : 'bg-white border-black/[0.08] shadow-xs hover:border-ios-border hover:bg-white/90'"
              >
                <input v-model="form.scope.node_ids" type="checkbox" :value="node.id" class="sr-only" />
                <div class="flex items-center gap-2 min-w-0">
                  <span class="w-1.5 h-1.5 rounded-full shrink-0" :class="nodeDotClass(node)"></span>
                  <span
                    class="text-sm leading-snug truncate"
                    :class="isSelected(form.scope.node_ids, node.id) ? 'font-semibold text-ios-blue' : 'font-medium text-ios-label'"
                  >
                    {{ node.name }}
                  </span>
                </div>
                <span
                  class="w-4 h-4 rounded-full border flex items-center justify-center shrink-0 transition-colors"
                  :class="isSelected(form.scope.node_ids, node.id)
                    ? 'bg-ios-blue border-ios-blue text-white'
                    : 'bg-white border-ios-border group-hover:border-ios-quaternary'"
                >
                  <svg v-if="isSelected(form.scope.node_ids, node.id)" class="w-2.5 h-2.5" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M2.5 6.2 4.8 8.5 9.5 3.5" />
                  </svg>
                </span>
              </label>
            </div>
            <div v-else class="text-xs text-ios-quaternary py-2">暂无节点</div>
          </div>

          <!-- 限定入口节点分组 -->
          <div role="group" aria-label="限定入口节点分组" class="p-3 rounded-2xl bg-ios-bg/75 border border-black/[0.05]">
            <div class="flex items-center justify-between gap-2 mb-2.5">
              <span class="text-xs font-medium text-ios-secondary">限定入口节点分组</span>
              <div class="flex items-center gap-2">
                <span
                  class="px-2 py-0.5 rounded-full text-xs font-medium tabular-nums"
                  :class="selectedGroupCount ? 'bg-ios-blue/10 text-ios-blue' : 'bg-white text-ios-tertiary border border-black/[0.06]'"
                >
                  {{ selectedGroupCount ? `已选 ${selectedGroupCount} / ${groups.length}` : '全部分组' }}
                </span>
                <button
                  v-if="selectedGroupCount"
                  type="button"
                  class="text-xs text-ios-tertiary hover:text-ios-label transition-colors"
                  @click="form.scope.group_ids = []"
                >
                  清空
                </button>
              </div>
            </div>
            <div v-if="orderedGroups.length" class="grid grid-cols-[repeat(auto-fill,minmax(145px,1fr))] gap-2 max-h-56 overflow-y-auto p-0.5">
              <label
                v-for="group in orderedGroups"
                :key="group.id"
                :title="group.name"
                class="group relative flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl border transition-all duration-150 cursor-pointer select-none active:scale-[0.98] has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ios-blue/30"
                :class="isSelected(form.scope.group_ids, group.id)
                  ? 'bg-ios-blue/[0.08] border-ios-blue shadow-xs'
                  : 'bg-white border-black/[0.08] shadow-xs hover:border-ios-border hover:bg-white/90'"
              >
                <input v-model="form.scope.group_ids" type="checkbox" :value="group.id" class="sr-only" />
                <div class="min-w-0">
                  <div
                    class="text-sm leading-snug truncate"
                    :class="isSelected(form.scope.group_ids, group.id) ? 'font-semibold text-ios-blue' : 'font-medium text-ios-label'"
                  >
                    {{ group.name }}
                  </div>
                  <div
                    v-if="(group.serverIds?.length || group.nodeIds?.length)"
                    class="text-xs text-ios-tertiary tabular-nums mt-0.5"
                  >
                    {{ group.serverIds?.length || group.nodeIds?.length }} 台节点
                  </div>
                </div>
                <span
                  class="w-4 h-4 rounded-full border flex items-center justify-center shrink-0 transition-colors"
                  :class="isSelected(form.scope.group_ids, group.id)
                    ? 'bg-ios-blue border-ios-blue text-white'
                    : 'bg-white border-ios-border group-hover:border-ios-quaternary'"
                >
                  <svg v-if="isSelected(form.scope.group_ids, group.id)" class="w-2.5 h-2.5" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2">
                    <path stroke-linecap="round" stroke-linejoin="round" d="M2.5 6.2 4.8 8.5 9.5 3.5" />
                  </svg>
                </span>
              </label>
            </div>
            <div v-else class="text-xs text-ios-quaternary py-2">暂无节点分组</div>
          </div>
        </div>
      </section>

      <!-- 3. 地址与服务规则集匹配 -->
      <section class="panel-card p-6">
        <div class="flex flex-wrap items-start justify-between gap-2 pb-3.5 mb-4 border-b border-black/[0.06]">
          <div>
            <h2 class="text-base font-semibold leading-normal text-ios-label">地址与规则集匹配</h2>
            <p class="text-xs text-ios-tertiary mt-0.5">以下四类目标条件满足任意一项即触发；全部留空则匹配所有目标地址。</p>
          </div>
          <span
            class="inline-flex items-center px-2.5 min-h-[24px] rounded-full text-xs font-medium leading-normal border tabular-nums"
            :class="totalMatchCount
              ? 'bg-ios-blue/10 text-ios-blue border-ios-blue/20'
              : 'bg-ios-bg text-ios-tertiary border-black/[0.06]'"
          >
            {{ totalMatchCount ? `已配置 ${totalMatchCount} 项条件` : '全部目标地址' }}
          </span>
        </div>

        <p class="text-xs text-ios-secondary bg-ios-bg/90 border border-black/[0.06] rounded-xl px-3.5 py-2.5 mb-5 leading-relaxed">
          IP / CIDR 仅匹配流量中的字面 IP 地址，不发起 DNS 解析，也不将域名解析为 IP 后二次匹配。
        </p>

        <div class="grid grid-cols-1 md:grid-cols-3 gap-4">
          <label class="block">
            <span class="flex items-center justify-between gap-2 mb-1.5">
              <span class="text-xs font-medium text-ios-secondary">精确 / 通配域名（每行一个）</span>
              <span class="text-xs tabular-nums" :class="domainCount ? 'text-ios-blue font-medium' : 'text-ios-quaternary'">
                {{ domainCount ? `${domainCount} 条` : '未填写' }}
              </span>
            </span>
            <textarea
              v-model="domainsText"
              rows="4"
              placeholder="api.example.com&#10;*.stream.example"
              class="form-input-base font-mono text-xs leading-relaxed"
            ></textarea>
          </label>

          <label class="block">
            <span class="flex items-center justify-between gap-2 mb-1.5">
              <span class="text-xs font-medium text-ios-secondary">域名后缀（每行一个）</span>
              <span class="text-xs tabular-nums" :class="suffixCount ? 'text-ios-blue font-medium' : 'text-ios-quaternary'">
                {{ suffixCount ? `${suffixCount} 条` : '未填写' }}
              </span>
            </span>
            <textarea
              v-model="suffixesText"
              rows="4"
              placeholder="netflix.com&#10;openai.com"
              class="form-input-base font-mono text-xs leading-relaxed"
            ></textarea>
          </label>

          <label class="block">
            <span class="flex items-center justify-between gap-2 mb-1.5">
              <span class="text-xs font-medium text-ios-secondary">IP / CIDR 网段（每行一个）</span>
              <span class="text-xs tabular-nums" :class="cidrCount ? 'text-ios-blue font-medium' : 'text-ios-quaternary'">
                {{ cidrCount ? `${cidrCount} 条` : '未填写' }}
              </span>
            </span>
            <textarea
              v-model="cidrsText"
              rows="4"
              placeholder="192.0.2.0/24&#10;2001:db8::/32"
              class="form-input-base font-mono text-xs leading-relaxed"
            ></textarea>
          </label>
        </div>

        <!-- 关联统一规则集 -->
        <div role="group" aria-label="关联统一规则集" class="mt-4 p-3 rounded-2xl bg-ios-bg/75 border border-black/[0.05]">
          <div class="flex items-center justify-between gap-2 mb-2.5">
            <span class="text-xs font-medium text-ios-secondary">关联统一规则集</span>
            <div class="flex items-center gap-2">
              <span
                class="px-2 py-0.5 rounded-full text-xs font-medium tabular-nums"
                :class="selectedServiceCount ? 'bg-ios-blue/10 text-ios-blue' : 'bg-white text-ios-tertiary border border-black/[0.06]'"
              >
                {{ selectedServiceCount ? `已选 ${selectedServiceCount} / ${services.length}` : '未选择' }}
              </span>
              <button
                v-if="selectedServiceCount"
                type="button"
                class="text-xs text-ios-tertiary hover:text-ios-label transition-colors"
                @click="form.match.service_ids = []"
              >
                清空
              </button>
            </div>
          </div>
          <div v-if="orderedServices.length" class="grid grid-cols-[repeat(auto-fill,minmax(160px,1fr))] gap-2 max-h-56 overflow-y-auto p-0.5">
            <label
              v-for="service in orderedServices"
              :key="service.id"
              :title="service.name"
              class="group relative flex items-center justify-between gap-2 px-2.5 py-2 rounded-xl border transition-all duration-150 cursor-pointer select-none active:scale-[0.98] has-[:focus-visible]:ring-2 has-[:focus-visible]:ring-ios-blue/30"
              :class="isSelected(form.match.service_ids, service.id)
                ? 'bg-ios-blue/[0.08] border-ios-blue shadow-xs'
                : 'bg-white border-black/[0.08] shadow-xs hover:border-ios-border hover:bg-white/90'"
            >
              <input v-model="form.match.service_ids" type="checkbox" :value="service.id" class="sr-only" />
              <div class="min-w-0">
                <div
                  class="text-sm leading-snug truncate"
                  :class="isSelected(form.match.service_ids, service.id) ? 'font-semibold text-ios-blue' : 'font-medium text-ios-label'"
                >
                  {{ service.name }}
                </div>
                <div v-if="!service.enabled" class="text-xs text-ios-orange font-medium mt-0.5">
                  已禁用
                </div>
              </div>
              <span
                class="w-4 h-4 rounded-full border flex items-center justify-center shrink-0 transition-colors"
                :class="isSelected(form.match.service_ids, service.id)
                  ? 'bg-ios-blue border-ios-blue text-white'
                  : 'bg-white border-ios-border group-hover:border-ios-quaternary'"
              >
                <svg v-if="isSelected(form.match.service_ids, service.id)" class="w-2.5 h-2.5" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M2.5 6.2 4.8 8.5 9.5 3.5" />
                </svg>
              </span>
            </label>
          </div>
          <div v-else class="text-xs text-ios-quaternary py-2">暂无规则集</div>
        </div>
      </section>

      <!-- 4. 端口、网络与时间 -->
      <section class="panel-card p-6">
        <div class="pb-3.5 mb-5 border-b border-black/[0.06]">
          <h2 class="text-base font-semibold leading-normal text-ios-label">端口、网络与生效时间</h2>
          <p class="text-xs text-ios-tertiary mt-0.5">进一步按传输协议、目标端口或时段收窄匹配条件。</p>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 items-start">
          <label class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">端口或范围</span>
            <input
              v-model="portsText"
              placeholder="80,443,8000-9000"
              class="form-input-base font-mono tabular-nums"
            />
            <span class="block text-xs text-ios-tertiary mt-1.5">逗号分隔，留空不限</span>
          </label>

          <label class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">协议网络</span>
            <select v-model="form.match.network" class="form-input-base">
              <option value="all">TCP + UDP</option>
              <option value="tcp">仅 TCP</option>
              <option value="udp">仅 UDP</option>
            </select>
          </label>

          <label class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">生效时间段</span>
            <input
              v-model="form.match.time_range"
              placeholder="23:00-06:00"
              class="form-input-base font-mono tabular-nums"
            />
            <span class="block text-xs text-ios-tertiary mt-1.5">留空表示全天 24 小时</span>
          </label>

          <label class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">时间段时区</span>
            <input
              v-model="form.timezone"
              placeholder="Asia/Shanghai"
              class="form-input-base font-mono"
            />
          </label>
        </div>

        <p
          v-if="form.match.network !== 'tcp' && form.scope.user_ids?.length"
          class="mt-4 text-xs text-ios-label bg-ios-orange/10 border border-ios-orange/25 rounded-xl px-3.5 py-2.5 leading-relaxed"
        >
          当前 Xray 的 SOCKS5 UDP 不携带账户身份，涉及该入站的用户/设备 UDP 策略会被拒绝发布。请使用可执行该范围的协议或核心。
        </p>
      </section>

      <!-- 5. 服务器端执行动作 -->
      <section class="panel-card p-6">
        <div class="pb-3.5 mb-5 border-b border-black/[0.06]">
          <h2 class="text-base font-semibold leading-normal text-ios-label">执行动作</h2>
          <p class="text-xs text-ios-tertiary mt-0.5">流量命中本策略后，由服务器节点执行的出站或拦截行为。</p>
        </div>

        <div class="grid grid-cols-1 md:grid-cols-2 gap-4 items-start">
          <label class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">动作类型</span>
            <select v-model="form.action.type" class="form-input-base" @change="changeAction">
              <option value="Direct">Direct · 服务器直连</option>
              <option value="Block">Block · 服务器阻断</option>
              <option value="ServerEgress">指定服务器出站</option>
              <option value="ServerEgressGroup">指定服务器组出站</option>
              <option v-if="legacyAction" :value="legacyAction.type">{{ actionNames[legacyAction.type] }}（旧策略）</option>
            </select>
          </label>

          <label v-if="serverEgress" class="block">
            <span class="block text-xs font-medium text-ios-secondary mb-1.5">
              {{ form.action.type === 'ServerEgress' ? '出站服务器' : '出站服务器组' }}
            </span>
            <select v-model="form.action.target_id" class="form-input-base" required>
              <option value="">请选择{{ form.action.type === 'ServerEgress' ? '服务器' : '服务器组' }}</option>
              <option v-for="target in egressTargets" :key="target.id" :value="target.id">{{ target.name }}</option>
            </select>
            <p v-if="!egressTargets.length" class="mt-1.5 text-xs text-ios-tertiary">
              {{ form.action.type === 'ServerEgress' ? '暂无可用的出站服务器。' : '请先在节点策略页面创建出站服务器组。' }}
            </p>
            <p
              v-else-if="form.action.target_id && !egressTargets.some(target => target.id === form.action.target_id)"
              class="mt-1.5 text-xs text-ios-red"
            >
              原目标已不可用，请选择新的目标。
            </p>
          </label>

          <dl v-else-if="usingLegacyAction" class="block">
            <dt class="text-xs font-medium text-ios-secondary mb-1.5">旧策略目标</dt>
            <dd class="px-3.5 py-2.5 rounded-xl bg-ios-bg border border-black/[0.06] text-sm text-ios-label break-all">
              {{ legacyTargetName(form.action.type, form.action.target_id) }}
            </dd>
          </dl>
        </div>

        <p class="mt-4 text-xs text-ios-secondary bg-ios-bg/90 border border-black/[0.06] rounded-xl px-3.5 py-2.5 leading-relaxed">
          <template v-if="form.action.type === 'Direct'">
            <strong>服务器直连</strong>：由当前入口服务器直接发起外部连接，不同于用户设备本地直连。
          </template>
          <template v-else-if="form.action.type === 'Block'">
            <strong>服务器阻断</strong>：命中流量将在服务器节点侧直接拒绝。
          </template>
          <template v-else-if="serverEgress">
            使用最终出站服务器的强制出站 IP，未配置时使用系统默认地址。{{ form.action.type === 'ServerEgressGroup' ? '按组内顺序选择可用服务器，全部不可用时阻断。' : '目标不可用时阻断。' }}
          </template>
          <template v-else>
            保留原有旧策略目标与失败容灾行为；切换到新动作类型后将按新动作规则执行。
          </template>
        </p>
      </section>

      <!-- 6. 失败容灾处理（仅旧策略显示） -->
      <section v-if="usingLegacyAction" class="panel-card p-6">
        <div class="pb-3.5 mb-4 border-b border-black/[0.06]">
          <h2 class="text-base font-semibold leading-normal text-ios-label">失败容灾处理</h2>
        </div>
        <p class="text-sm text-ios-label">
          {{ failureNames[form.action.failover || 'Block'] || form.action.failover }}<span v-if="form.action.backup_id" class="text-ios-secondary"> · {{ legacyTargetName(form.action.failover?.replace(/^Backup/, '') || '', form.action.backup_id) }}</span>
        </p>
      </section>

      <!-- 7. 记录与备注 -->
      <section class="panel-card p-6">
        <div class="pb-3.5 mb-5 border-b border-black/[0.06]">
          <h2 class="text-base font-semibold leading-normal text-ios-label">记录与备注</h2>
          <p class="text-xs text-ios-tertiary mt-0.5">控制命中该策略时的审计记录选项及维护说明。</p>
        </div>

        <div class="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <label
            class="flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl border transition-colors cursor-pointer select-none"
            :class="form.logging.log_domain
              ? 'bg-ios-blue/[0.07] border-ios-blue/30 text-ios-label'
              : 'bg-ios-bg/80 border-black/[0.06] text-ios-secondary hover:text-ios-label'"
          >
            <span class="text-sm font-medium">记录请求域名</span>
            <input v-model="form.logging.log_domain" type="checkbox" class="w-4 h-4 shrink-0" />
          </label>

          <label
            class="flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl border transition-colors cursor-pointer select-none"
            :class="form.logging.log_traffic
              ? 'bg-ios-blue/[0.07] border-ios-blue/30 text-ios-label'
              : 'bg-ios-bg/80 border-black/[0.06] text-ios-secondary hover:text-ios-label'"
          >
            <span class="text-sm font-medium">记录流量统计</span>
            <input v-model="form.logging.log_traffic" type="checkbox" class="w-4 h-4 shrink-0" />
          </label>

          <label
            class="flex items-center justify-between gap-3 px-3.5 py-2.5 rounded-xl border transition-colors cursor-pointer select-none"
            :class="form.logging.alert_on_block
              ? 'bg-ios-blue/[0.07] border-ios-blue/30 text-ios-label'
              : 'bg-ios-bg/80 border-black/[0.06] text-ios-secondary hover:text-ios-label'"
          >
            <span class="text-sm font-medium">阻断时触发告警</span>
            <input v-model="form.logging.alert_on_block" type="checkbox" class="w-4 h-4 shrink-0" />
          </label>
        </div>

        <label class="block mt-5">
          <span class="flex items-center justify-between gap-2 mb-1.5">
            <span class="text-xs font-medium text-ios-secondary">备注说明</span>
            <span class="text-xs text-ios-quaternary tabular-nums">{{ (form.remark || '').length }} / 300</span>
          </span>
          <textarea
            v-model="form.remark"
            maxlength="300"
            rows="2"
            placeholder="说明此策略的业务意图…"
            class="form-input-base"
          ></textarea>
        </label>
      </section>

      <!-- Bottom Actions -->
      <div class="flex flex-wrap items-center justify-between gap-3 pt-1">
        <p class="text-xs text-ios-tertiary">保存后仅写入配置草稿，需在节点策略页发布到节点后生效。</p>
        <div class="flex items-center gap-3 ml-auto">
          <router-link to="/routing/rules" class="ios-btn-secondary">
            取消
          </router-link>
          <button :disabled="saving || loading" class="ios-btn-primary disabled:opacity-40">
            {{ saving ? '保存中…' : '保存节点策略草稿' }}
          </button>
        </div>
      </div>
    </form>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { actionNames, policyAPI, serverEgressGroupAPI, defaults, input, type Action, type PolicyInput, type ServerEgressGroup, type ServiceSet } from '../api';
import { authState } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { userAPI, type UserChoice } from '../../users/api';
import { groupAPI, type NodeGroup } from '../../nodes/groupsApi';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { nodeHasUserCore } from '../../nodes/coreSummary';
import { proxyAPI, type Outbound, type OutboundGroup, type ProxyChain, choices, failureNames } from '../../proxy/api';
import PageHeader from '../../../components/PageHeader.vue';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';

const outboundGroups = ref<OutboundGroup[]>([]);
const chains = ref<ProxyChain[]>([]);
const egressGroups = ref<ServerEgressGroup[]>([]);
const legacyAction = ref<Action | null>(null);
const usingLegacyAction = computed(() => form.action.type === legacyAction.value?.type);
const serverEgress = computed(() => form.action.type === 'ServerEgress' || form.action.type === 'ServerEgressGroup');
const egressTargets = computed(() => form.action.type === 'ServerEgressGroup' ? egressGroups.value :
  nodes.value.filter(node => node.enabled && !node.deletedAt && node.status !== 'pending' && node.status !== 'disabled' && nodeHasUserCore(node)));

function legacyTargetName(type: string, id?: string) {
  if (!id) return '';
  const targets = type === 'Outbound' ? outbounds.value : type === 'OutboundGroup' ? outboundGroups.value : chains.value;
  return targets.find(target => target.id === id)?.name || id;
}

function changeAction() {
  if (legacyAction.value && form.action.type === legacyAction.value.type) {
    form.action = { ...legacyAction.value };
    return;
  }
  form.action = { type: form.action.type, target_id: '', failover: 'Block', backup_id: '' };
}

function isSelected(list: string[] | undefined, id: string): boolean {
  return !!list && list.includes(id);
}

function selectedFirst<T extends { id: string }>(items: T[], selected: string[] | undefined): T[] {
  if (!selected?.length) return items;
  const picked = new Set(selected);
  return [...items.filter(item => picked.has(item.id)), ...items.filter(item => !picked.has(item.id))];
}

function nodeDotClass(node: NodeView): string {
  if (!node.enabled || node.status === 'disabled' || node.status === 'pending') return 'bg-ios-quaternary';
  if (node.maintenance || node.status === 'degraded' || node.status === 'applying' || node.status === 'rollback') return 'bg-ios-orange';
  if (node.status === 'online') return 'bg-ios-green';
  return 'bg-ios-red';
}

const route = useRoute();
const router = useRouter();
const editingId = computed(() => (typeof route.params.id === 'string' ? route.params.id : ''));

const form = reactive<PolicyInput>(defaults());
const domainsText = ref('');
const suffixesText = ref('');
const cidrsText = ref('');
const portsText = ref('');
const users = ref<UserChoice[]>([]);
const nodes = ref<NodeView[]>([]);
const groups = ref<NodeGroup[]>([]);
const services = ref<ServiceSet[]>([]);
const outbounds = ref<Outbound[]>([]);
const loading = ref(false);
const detailReady = ref(false);
const saving = ref(false);
const error = ref('');

const orderedUsers = computed(() => selectedFirst(users.value, form.scope.user_ids));
const orderedNodes = computed(() => selectedFirst(nodes.value, form.scope.node_ids));
const orderedGroups = computed(() => selectedFirst(groups.value, form.scope.group_ids));
const orderedServices = computed(() => selectedFirst(services.value, form.match.service_ids));

const selectedUserCount = computed(() => form.scope.user_ids?.length || 0);
const selectedNodeCount = computed(() => form.scope.node_ids?.length || 0);
const selectedGroupCount = computed(() => form.scope.group_ids?.length || 0);
const scopeSummaryText = computed(() => {
  const parts = [
    selectedUserCount.value ? `${selectedUserCount.value} 位用户` : '',
    selectedNodeCount.value ? `${selectedNodeCount.value} 台节点` : '',
    selectedGroupCount.value ? `${selectedGroupCount.value} 个分组` : ''
  ].filter(Boolean);
  return parts.length ? `已限定：${parts.join(' · ')}` : '全部用户与节点';
});

const domainCount = computed(() => lines(domainsText.value).length);
const suffixCount = computed(() => lines(suffixesText.value).length);
const cidrCount = computed(() => lines(cidrsText.value).length);
const selectedServiceCount = computed(() => form.match.service_ids?.length || 0);
const totalMatchCount = computed(() => domainCount.value + suffixCount.value + cidrCount.value + selectedServiceCount.value);

async function load() {
  loading.value = true; detailReady.value = false;
  try {
    const [u, n, g, s, egress, policy] = await Promise.all([
      userAPI.choices(1, '', 100),
      choices((p, size) => nodeAPI.choices(p, '', size)),
      groupAPI.list(1, 100),
      policyAPI.services(1, 100),
      serverEgressGroupAPI.list(),
      editingId.value ? policyAPI.get(editingId.value) : Promise.resolve(null)
    ]);
    legacyAction.value = policy && ['Outbound', 'OutboundGroup', 'ProxyChain'].includes(policy.action.type) ? { ...policy.action } : null;
    if (legacyAction.value) {
      [outbounds.value, outboundGroups.value, chains.value] = await Promise.all([
        choices((p, size) => proxyAPI.outbounds('', p, size)),
        choices((p, size) => proxyAPI.groups('', p, size)),
        choices((p, size) => proxyAPI.chains('', p, size))
      ]);
    }
    Object.assign(form, policy ? input(policy) : defaults());
    if (!policy) form.timezone = authState.timezone;
    domainsText.value = form.match.domains?.join('\n') || '';
    suffixesText.value = form.match.suffixes?.join('\n') || '';
    cidrsText.value = form.match.cidrs?.join('\n') || '';
    portsText.value = form.match.ports?.join(',') || '';
    users.value = u.items;
    nodes.value = n;
    groups.value = g.items;
    services.value = s.items;
    egressGroups.value = egress;
    detailReady.value = true;

    error.value = '';
  } catch (e) {
    error.value = (e as Error).message;
  } finally {
    loading.value = false;
  }
}

function lines(value: string) {
  return value
    .split('\n')
    .map(item => item.trim())
    .filter(Boolean);
}

async function save() {
  if (saving.value || loading.value || error.value) return;
  saving.value = true;
  try {
    const value: PolicyInput = JSON.parse(JSON.stringify(form));
    value.match.domains = lines(domainsText.value);
    value.match.suffixes = lines(suffixesText.value);
    value.match.cidrs = lines(cidrsText.value);
    value.match.ports = portsText.value
      .split(',')
      .map(item => item.trim())
      .filter(Boolean);

    if (value.action.type === 'Direct' || value.action.type === 'Block') {
      value.action.failover = 'Block';
      value.action.backup_id = '';
      value.action.target_id = '';
    }

    await policyAPI.save(editingId.value || null, value);
    showToast('节点策略草稿已保存，请通过 Agent 发布配置生效');
    await router.push('/routing/rules');
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    saving.value = false;
  }
}

onMounted(load);
</script>
