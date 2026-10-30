<template>
  <div class="space-y-6">
    <!-- Header -->
    <PageHeader
      title="客户端策略"
      parent="分流控制"
      description="管理 Clash / Mihomo 客户端的分流规则、优先级、策略组与运行参数。通过完整配置订阅下发，用户更新订阅后生效。"
    >
      <template #actions>
        <div class="flex flex-wrap sm:flex-nowrap items-center gap-2.5 min-w-0 w-full sm:w-auto">
        <button
          type="button"
          @click="resetToDefault"
          class="ios-btn-secondary text-xs"
        >
          恢复默认模板
        </button>
        <button
          type="button"
          @click="openCreateRuleDrawer"
          class="ios-btn-primary inline-flex items-center gap-1.5"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          <span>添加规则</span>
        </button>
        <button
          type="button"
          @click="saveAll"
          :disabled="saving"
          class="ios-btn-primary inline-flex items-center gap-1.5 shadow-sm"
        >
          <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M5 13l4 4L19 7" />
          </svg>
          <span>{{ saving ? '保存中…' : '保存策略配置' }}</span>
        </button>
        <router-link to="/routing/simulator?tab=client" class="ios-btn-secondary">
          策略模拟
        </router-link>
        </div>
      </template>
    </PageHeader>

    <!-- Important Notice Banner -->
    <div class="p-4 rounded-2xl bg-ios-blue/[0.06] border border-ios-blue/15 space-y-2">
      <div class="flex items-center gap-2 text-ios-blue font-semibold text-xs tracking-wide">
        <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
        </svg>
        <span>客户端策略下发与生效机制</span>
      </div>
      <p class="text-xs text-neutral-600 leading-relaxed">
        • <strong>客户端直连 (DIRECT)</strong>：指用户终端设备直接发起网络连接，不经过任何代理服务器，免消耗流量与服务器资源，请勿与服务器端直连混淆。<br />
        • <strong>下发生效方式</strong>：仅加载节点的 proxy-providers 并不会加载策略；完整分流策略需通过<strong>完整配置订阅</strong>下发给 Clash / Mihomo 客户端，用户在客户端执行“更新订阅”后即刻生效。<br />
        • <strong>规则集联动</strong>：支持通过 <code class="px-1.5 py-0.5 rounded bg-black/[0.04] font-mono text-[11px]">RULE-SET</code> 引用系统统一规则集（如 OpenAI、广告拦截），客户端将通过 rule-providers 自动同步。
      </p>
    </div>

    <!-- 1. Quick Presets Section (从原系统设置融合) -->
    <div class="panel-card p-5 space-y-3">
      <div class="flex items-center justify-between">
        <div>
          <h3 class="text-sm font-semibold text-neutral-900">快速套用分流预设</h3>
          <p class="text-xs text-neutral-400 mt-0.5">点击预设卡片可一键重组常用分流规则与策略组模板</p>
        </div>
        <span class="text-xs text-neutral-400 font-mono">当前预设: {{ activePresetName }}</span>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-3">
        <button
          v-for="preset in clientPresets"
          :key="preset.id"
          type="button"
          @click="applyPreset(preset.id)"
          :class="[
            'p-3.5 rounded-2xl border text-left transition-all relative select-none',
            activePreset === preset.id
              ? 'border-ios-blue bg-ios-blue/[0.04] shadow-sm ring-1 ring-ios-blue/30'
              : 'border-black/[0.06] hover:border-black/20 hover:bg-black/[0.01]'
          ]"
        >
          <div class="flex items-center justify-between mb-1">
            <span class="text-xs font-bold text-neutral-900">{{ preset.name }}</span>
            <span v-if="preset.recommended" class="text-[10px] bg-emerald-50 text-emerald-700 font-bold px-1.5 py-0.5 rounded-full">
              推荐
            </span>
          </div>
          <p class="text-[11px] text-neutral-500 line-clamp-2 leading-relaxed">{{ preset.desc }}</p>
        </button>
      </div>
    </div>

    <!-- 2. Client Runtime Parameters (从原系统设置融合) -->
    <div class="panel-card p-5 space-y-4">
      <div class="flex items-center justify-between border-b border-black/[0.06] pb-2.5">
        <div>
          <h3 class="text-sm font-semibold text-neutral-900">客户端运行基础参数</h3>
          <p class="text-xs text-neutral-400 mt-0.5">生成 Clash / Mihomo 配置文件顶层核心字段</p>
        </div>
        <span class="text-xs text-neutral-400 font-mono">Mixed Port / Mode / Allow LAN</span>
      </div>

      <div class="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-4 gap-4 text-xs">
        <label class="block">
          <span class="text-neutral-600 font-medium">运行模式 (Mode)</span>
          <select v-model="clientConfig.settings.mode" class="form-input-base mt-1.5">
            <option value="rule">Rule (规则分流 · 推荐)</option>
            <option value="global">Global (全局代理)</option>
            <option value="direct">Direct (全局直连)</option>
          </select>
        </label>

        <label class="block">
          <span class="text-neutral-600 font-medium">混合端口 (Mixed Port)</span>
          <input
            v-model.number="clientConfig.settings.mixedPort"
            type="number"
            min="1024"
            max="65535"
            class="form-input-base mt-1.5 font-mono"
          />
        </label>

        <label class="block">
          <span class="text-neutral-600 font-medium">允许局域网连接 (Allow LAN)</span>
          <select v-model="clientConfig.settings.allowLan" class="form-input-base mt-1.5">
            <option :value="true">是 (true · 允许同一局域网设备接入)</option>
            <option :value="false">否 (false · 仅本机回环访问)</option>
          </select>
        </label>

        <label class="block">
          <span class="text-neutral-600 font-medium">日志级别 (Log Level)</span>
          <select v-model="clientConfig.settings.logLevel" class="form-input-base mt-1.5">
            <option value="info">Info (标准日志)</option>
            <option value="warning">Warning (仅告警)</option>
            <option value="error">Error (仅错误)</option>
            <option value="silent">Silent (静默)</option>
          </select>
        </label>
      </div>
    </div>

    <!-- 3. Proxy Groups Configuration (策略组定义) -->
    <div class="panel-card p-5 space-y-4">
      <div class="flex items-center justify-between border-b border-black/[0.06] pb-2.5">
        <div>
          <h3 class="text-sm font-semibold text-neutral-900">策略组定义 (Proxy Groups)</h3>
          <p class="text-xs text-neutral-400 mt-0.5">客户端界面展现的节点选择组、自动优选测速组与容灾备用组</p>
        </div>
        <button
          type="button"
          @click="openAddGroupDrawer"
          class="ios-btn-secondary text-xs flex items-center gap-1"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M12 4v16m8-8H4" />
          </svg>
          <span>添加策略组</span>
        </button>
      </div>

      <div class="space-y-2.5">
        <div
          v-for="(grp, idx) in clientConfig.proxyGroups"
          :key="idx"
          class="flex flex-wrap sm:flex-nowrap items-center gap-3 p-3.5 rounded-2xl bg-black/[0.02] border border-black/[0.06] transition-colors hover:bg-black/[0.03]"
        >
          <!-- Group Name -->
          <div class="w-full sm:w-44">
            <span class="text-[11px] text-neutral-400 font-medium block mb-1">组名称 (Group Name)</span>
            <input v-model="grp.name" class="form-input-base text-xs font-mono font-bold" placeholder="如 PROXY" />
          </div>

          <!-- Group Type -->
          <div class="w-full sm:w-40">
            <span class="text-[11px] text-neutral-400 font-medium block mb-1">策略类型 (Type)</span>
            <select v-model="grp.type" class="form-input-base text-xs">
              <option value="select">select (手动选择)</option>
              <option value="url-test">url-test (自动测速)</option>
              <option value="fallback">fallback (故障转移)</option>
            </select>
          </div>

          <!-- Group Proxies -->
          <div class="flex-1 w-full">
            <span class="text-[11px] text-neutral-400 font-medium block mb-1">包含节点 / 策略 (逗号分隔)</span>
            <input
              v-model="grp.proxies"
              class="form-input-base text-xs font-mono"
              placeholder="如 AUTO, @all, @nodeGroup:节点分组ID, DIRECT"
            />
          </div>

          <!-- Remove Group Action -->
          <button
            type="button"
            class="text-red-600 hover:bg-red-50 p-2 rounded-xl transition-colors self-end sm:self-center shrink-0"
            title="删除此策略组"
            @click="removeGroup(idx)"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 7l-.867 12.142A2 2 0 0116.138 21H7.862a2 2 0 01-1.995-1.858L5 7m5 4v6m4-6v6m1-10V4a1 1 0 00-1-1h-4a1 1 0 00-1 1v3M4 7h16" />
            </svg>
          </button>
        </div>
      </div>
    </div>

    <!-- 4. Rules List Editor (分流规则列表) -->
    <div class="space-y-3">
      <div class="flex items-center justify-between">
        <div class="flex items-center gap-2">
          <h3 class="text-xs font-semibold text-neutral-500 uppercase tracking-wider">分流规则列表 (Rules)</h3>
          <span class="text-xs font-mono px-2 py-0.5 rounded-full bg-black/[0.05] text-neutral-600 font-semibold">
            共 {{ clientConfig.rules.length }} 条
          </span>
        </div>
        <div class="flex items-center gap-2 text-xs text-neutral-400">
          <span>优先级数字越小越先匹配执行 (Match First)</span>
        </div>
      </div>

      <DataTable>
        <template #header>
          <th class="w-16">优先级</th>
          <th>规则类型</th>
          <th>匹配目标 / 关键字</th>
          <th>执行动作 (Target)</th>
          <th>说明备注</th>
          <th>状态</th>
          <th class="text-right pr-6">操作</th>
        </template>
        <template #body>
          <tr v-for="(rule, idx) in sortedRules" :key="rule.id" class="hover:bg-black/[0.015] transition-colors">
            <!-- 1. Priority -->
            <td class="font-mono text-xs font-bold text-neutral-500">
              #{{ rule.priority }}
            </td>

            <!-- 2. Type -->
            <td>
              <span class="font-mono text-xs font-semibold px-2 py-0.5 rounded-md" :class="getRuleTypeBadgeClass(rule.type)">
                {{ rule.type }}
              </span>
            </td>

            <!-- 3. Payload / Target -->
            <td class="font-mono text-xs text-neutral-900 max-w-[260px] truncate" :title="rule.payload || '任意匹配 (MATCH)'">
              <template v-if="rule.type === 'RULE-SET'">
                <span class="inline-flex items-center gap-1 font-sans text-neutral-800 font-medium">
                  <svg class="w-3.5 h-3.5 text-ios-blue" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10" />
                  </svg>
                  {{ getRuleSetName(rule.payload) }}
                </span>
              </template>
              <template v-else-if="rule.type === 'MATCH'">
                <span class="text-neutral-400 italic font-sans">所有未命中流量 (Final Match)</span>
              </template>
              <template v-else>
                <span>{{ rule.payload }}</span>
                <span v-if="rule.noResolve" class="ml-1.5 px-1 py-0.2 rounded text-[10px] bg-neutral-100 text-neutral-500">no-resolve</span>
              </template>
            </td>

            <!-- 4. Action -->
            <td>
              <span class="inline-flex items-center gap-1.5 px-2.5 py-0.5 rounded-full text-xs font-semibold" :class="getActionBadgeClass(rule.target)">
                <span class="w-1.5 h-1.5 rounded-full" :class="getActionDotClass(rule.target)"></span>
                <span>{{ formatActionLabel(rule.target) }}</span>
              </span>
            </td>

            <!-- 5. Description -->
            <td class="text-xs text-neutral-500 max-w-[200px] truncate" :title="rule.description">
              {{ rule.description || '—' }}
            </td>

            <!-- 6. Status -->
            <td>
              <StatusBadge :status="rule.enabled ? 'active' : 'disabled'" />
            </td>

            <!-- 7. Actions -->
            <td class="text-right pr-6 space-x-1.5 whitespace-nowrap">
              <button type="button" @click="moveRule(idx, -1)" :disabled="idx === 0" class="ios-action-btn-secondary disabled:opacity-30" title="上移">
                ↑
              </button>
              <button type="button" @click="moveRule(idx, 1)" :disabled="idx === sortedRules.length - 1" class="ios-action-btn-secondary disabled:opacity-30" title="下移">
                ↓
              </button>
              <button type="button" @click="openEditDrawer(rule)" class="ios-action-btn-primary">
                编辑
              </button>
              <button type="button" @click="toggleRule(rule)" class="ios-action-btn-secondary">
                {{ rule.enabled ? '停用' : '启用' }}
              </button>
              <button type="button" @click="promptDelete(rule)" class="ios-action-btn-danger">
                删除
              </button>
            </td>
          </tr>
        </template>
      </DataTable>
    </div>

    <!-- 5. Real-Time YAML Preview Section (从原系统设置融合) -->
    <div class="panel-card p-5 space-y-3">
      <div class="flex items-center justify-between border-b border-black/[0.06] pb-2.5">
        <div>
          <h3 class="text-sm font-semibold text-neutral-900">Clash / Mihomo 配置 YAML 实时渲染预览</h3>
          <p class="text-xs text-neutral-400 mt-0.5">由服务器按所选用户与格式渲染已保存的模板，与该用户订阅拿到的内容一致</p>
        </div>
        <div class="flex items-center gap-2">
        <select v-model="previewUserId" class="form-input-base text-xs w-36" @change="loadPreview">
          <option value="">选择用户…</option>
          <option v-for="user in previewUsers" :key="user.id" :value="user.id">{{ user.username }}</option>
        </select>
        <select v-model="previewFormat" class="form-input-base text-xs w-32" @change="loadPreview">
          <option value="clash">clash</option>
          <option value="clash-legacy">clash-legacy</option>
        </select>
        <button
          type="button"
          class="ios-btn-secondary text-xs flex items-center gap-1.5"
          :disabled="!previewBody"
          @click="copyConfig"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M8 7v8a2 2 0 002 2h6M8 7V5a2 2 0 012-2h4.586a1 1 0 01.707.293l4.414 4.414a1 1 0 01.293.707V15a2 2 0 01-2 2h-2M8 7H6a2 2 0 00-2 2v10a2 2 0 002 2h8a2 2 0 002-2v-2" />
          </svg>
          <span>{{ copied ? '已复制！' : '复制完整 YAML' }}</span>
        </button>
        </div>
      </div>

      <pre class="p-4 rounded-2xl bg-neutral-900 text-neutral-100 font-mono text-xs overflow-x-auto max-h-80 scrollbar-thin select-all leading-relaxed">{{ previewBody || previewHint }}</pre>

      <div v-if="previewWarnings.length" class="p-3 rounded-xl bg-amber-50 border border-amber-200/60 text-xs text-amber-800 leading-relaxed space-y-0.5">
        <p class="font-semibold">该用户的订阅中有以下内容被调整或省略：</p>
        <p v-for="(warning, index) in previewWarnings" :key="index">• {{ warning.message }}</p>
      </div>

      <div class="p-3 rounded-xl bg-amber-50 border border-amber-200/60 text-xs text-amber-800 leading-relaxed">
        <strong>安全与权限保证</strong>：完整订阅导出受专属 Token 鉴权保护，系统严格校验用户对节点的可用权限，不会泄露其他用户的账号密码与密钥凭据。
      </div>
    </div>

    <!-- Rule Edit / Create Drawer -->
    <FormDrawer
      v-model="drawerVisible"
      :title="editingRule ? '编辑客户端规则' : '添加客户端规则'"
      description="配置 Clash / Mihomo 客户端分流规则，优先级数值越小越优先执行。"
      :loading="saving"
      @confirm="saveRule"
    >
      <div class="space-y-4 text-sm">
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">规则匹配类型</span>
          <select v-model="form.type" class="form-input-base mt-1.5">
            <option value="DOMAIN">DOMAIN (域名完全匹配)</option>
            <option value="DOMAIN-SUFFIX">DOMAIN-SUFFIX (域名后缀匹配)</option>
            <option value="DOMAIN-KEYWORD">DOMAIN-KEYWORD (域名关键字)</option>
            <option value="IP-CIDR">IP-CIDR (目标 IP 网段)</option>
            <option value="IP-CIDR6">IP-CIDR6 (目标 IPv6 网段)</option>
            <option value="DST-PORT">DST-PORT (目标端口，多个用 / 分隔)</option>
            <option value="RULE-SET">RULE-SET (引用系统统一规则集)</option>
            <option value="GEOIP">GEOIP (地理位置，如 CN)</option>
            <option value="GEOSITE">GEOSITE (域名分类，如 category-ads-all)</option>
            <option value="MATCH">MATCH (全部未匹配的默认兜底)</option>
          </select>
        </label>

        <!-- Dynamic Payload Field -->
        <div v-if="form.type === 'RULE-SET'">
          <label class="block">
            <span class="text-xs font-medium text-neutral-600">选择引用的规则集</span>
            <select v-model="form.payload" class="form-input-base mt-1.5" required>
              <option value="">请选择规则集...</option>
              <option v-for="set in serviceSets" :key="set.id" :value="`service:${set.id}`">
                {{ set.name }} ({{ set.domains.length + set.suffixes.length }} 域名, {{ set.cidrs.length }} IP)
              </option>
            </select>
          </label>
          <p class="text-xs text-neutral-400 mt-1">规则集由系统统一维护，客户端将以 rule-providers 形式订阅自动更新。</p>
        </div>

        <div v-else-if="form.type !== 'MATCH'">
          <label class="block">
            <span class="text-xs font-medium text-neutral-600">匹配目标值 (Payload)</span>
            <input
              v-model="form.payload"
              :placeholder="getPayloadPlaceholder(form.type)"
              class="form-input-base mt-1.5 font-mono text-xs"
              required
            />
          </label>
          <div v-if="['IP-CIDR', 'IP-CIDR6', 'GEOIP'].includes(form.type)" class="mt-2">
            <label class="inline-flex items-center gap-2 text-xs text-neutral-600">
              <input v-model="form.noResolve" type="checkbox" />
              <span>no-resolve (跳过对域名的 DNS 反查，仅匹配纯 IP 连接)</span>
            </label>
          </div>
        </div>

        <!-- Target Action -->
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">执行动作 (Target)</span>
          <select v-model="form.target" class="form-input-base mt-1.5">
            <optgroup label="基础动作">
              <option value="DIRECT">DIRECT (用户设备本地直连 · 不走代理)</option>
              <option value="REJECT">REJECT (客户端本地阻断拦截)</option>
            </optgroup>
            <optgroup label="策略组">
              <option v-for="g in clientConfig.proxyGroups" :key="g.name" :value="g.name">
                {{ g.name }} ({{ g.type }})
              </option>
            </optgroup>
            <optgroup v-if="availableNodes.length" label="指定单节点">
              <option v-for="node in availableNodes" :key="node.id" :value="`node:${node.id}`">
                {{ node.name }} ({{ node.publicIp || '未知 IP' }})
              </option>
            </optgroup>
          </select>
        </label>

        <!-- Priority & Enabled -->
        <div class="grid grid-cols-2 gap-3">
          <label class="block">
            <span class="text-xs font-medium text-neutral-600">优先级 (越小越先执行)</span>
            <input v-model.number="form.priority" type="number" min="1" max="9999" class="form-input-base mt-1.5" required />
          </label>
          <label class="block">
            <span class="text-xs font-medium text-neutral-600">启用状态</span>
            <div class="mt-2.5">
              <label class="inline-flex items-center gap-2 text-xs text-neutral-700">
                <input v-model="form.enabled" type="checkbox" />
                <span>启用此条规则</span>
              </label>
            </div>
          </label>
        </div>

        <!-- Description -->
        <label class="block">
          <span class="text-xs font-medium text-neutral-600">备注说明 (可选)</span>
          <input v-model="form.description" placeholder="例如：国内常用金融与政务域名直连" maxlength="60" class="form-input-base mt-1.5" />
        </label>
      </div>
    </FormDrawer>

    <!-- Confirm Delete Dialog -->
    <ConfirmDialog
      v-model="confirmDeleteVisible"
      title="删除客户端规则"
      :message="`确定删除此条客户端规则？删除后需用户刷新订阅才会在客户端生效。`"
      @confirm="executeDelete"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue';
import {
  clientPolicyAPI,
  clientPresets,
  policyAPI,
  type ClientPolicyConfig,
  type ClientRule,
  type ClientRuleType,
  type RulePreset,
  type ServiceSet
} from '../api';
import { nodeAPI, type NodeView } from '../../nodes/api';
import { userAPI, type ProxyUser } from '../../users/api';
import { showToast } from '../../../app/api';
import { APIError, request } from '../../../app/apiClient';
import { authState } from '../../../app/auth';
import PageHeader from '../../../components/PageHeader.vue';
import DataTable from '../../../components/DataTable.vue';
import StatusBadge from '../../../components/StatusBadge.vue';
import FormDrawer from '../../../components/FormDrawer.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';

// Until the server answers, the page shows an empty template; nothing is saved
// from it because `loaded` stays false.
function emptyConfig(): ClientPolicyConfig {
  return {
    version: 0,
    updatedAt: '',
    activePreset: '',
    settings: { mode: 'rule', mixedPort: 7890, allowLan: true, logLevel: 'info' },
    proxyGroups: [],
    rules: [],
    extra: { ipv6: false, dns: { enable: false, enhancedMode: '', nameservers: [], fallback: [] }, providers: [] }
  };
}

const clientConfig = ref<ClientPolicyConfig>(emptyConfig());
const loaded = ref(false);
const presetTemplates = ref<RulePreset[]>([]);
const serviceSets = ref<ServiceSet[]>([]);
const availableNodes = ref<NodeView[]>([]);
const isOwner = computed(() => authState.currentUser?.role === 'Owner');

const drawerVisible = ref(false);
const saving = ref(false);
const editingRule = ref<ClientRule | null>(null);

const confirmDeleteVisible = ref(false);
const deletingRule = ref<ClientRule | null>(null);
const copied = ref(false);

const activePreset = ref<string>('standard');
const activePresetName = computed(() => {
  return clientPresets.find(p => p.id === activePreset.value)?.name || '自定义';
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

const sortedRules = computed(() => {
  return [...clientConfig.value.rules].sort((a, b) => a.priority - b.priority);
});

async function load() {
  try {
    const config = await clientPolicyAPI.get();
    // An earlier build kept the template in this browser only, so it never
    // reached a client. Offer it once as an unsaved draft.
    const draft = config.version === 0 ? clientPolicyAPI.legacyDraft() : null;
    if (draft) {
      config.settings = draft.settings;
      config.proxyGroups = draft.proxyGroups;
      config.rules = draft.rules;
      config.activePreset = '';
      showToast('已载入本浏览器中旧版（未生效）的客户端策略，检查后点击「保存策略配置」才会生效', 'warning');
    }
    clientConfig.value = config;
    activePreset.value = config.activePreset || '';
    loaded.value = true;
  } catch (e) {
    showToast(`载入客户端策略失败: ${(e as Error).message}`, 'error');
  }
  try {
    const [presetsResult, setsResult, nodesResult] = await Promise.all([
      clientPolicyAPI.presets(),
      policyAPI.services(1, 100),
      nodeAPI.list(1, '')
    ]);
    presetTemplates.value = presetsResult.items;
    serviceSets.value = setsResult.items;
    availableNodes.value = nodesResult.items;
  } catch (e) {
    console.error('Failed to load auxiliary data for client policies:', e);
  }
  if (isOwner.value) {
    try {
      previewUsers.value = (await userAPI.list(1, '', 100)).items;
    } catch {
      previewUsers.value = [];
    }
  }
}

// persist saves the whole template. The server validates it as a whole, so an
// edit that would leave it invalid (for example a MATCH that is no longer last)
// is refused and the page keeps the previous saved state.
async function persist(success: string): Promise<boolean> {
  if (!loaded.value) return false;
  if (!isOwner.value) {
    showToast('只有 Owner 可以修改客户端策略', 'error');
    return false;
  }
  saving.value = true;
  try {
    clientConfig.value = await clientPolicyAPI.save(clientConfig.value);
    activePreset.value = clientConfig.value.activePreset || '';
    clientPolicyAPI.forgetLegacyDraft();
    showToast(success);
    if (previewUserId.value) loadPreview();
    return true;
  } catch (e) {
    if (e instanceof APIError && e.code === 'stale_rules') {
      showToast('客户端策略已在其他地方修改，已重新载入最新版本', 'error');
      await load();
    } else if (e instanceof APIError && e.code === 'invalid_rules') {
      showToast(`${fieldLabel(String(e.details.field || ''))}：${e.message}`, 'error');
    } else {
      showToast((e as Error).message, 'error');
    }
    return false;
  } finally {
    saving.value = false;
  }
}

// fieldLabel names the field of a server validation error the way the page
// shows it, e.g. rules[3].payload → "规则 #20 的匹配内容".
function fieldLabel(field: string): string {
  const rule = /^rules\[(\d+)\]\.?(\w*)/.exec(field);
  if (rule) {
    const item = clientConfig.value.rules[Number(rule[1])];
    const part: Record<string, string> = { payload: '匹配内容', target: '执行动作', priority: '优先级', noResolve: 'no-resolve', description: '备注', id: 'ID' };
    return `规则 #${item?.priority ?? Number(rule[1]) + 1} 的${part[rule[2]] || '设置'}`;
  }
  const group = /^groups\[(\d+)\]\.?(\w*)/.exec(field);
  if (group) {
    const item = clientConfig.value.proxyGroups[Number(group[1])];
    const part: Record<string, string> = { name: '名称', type: '类型', members: '成员', testUrl: '测速地址', intervalSeconds: '测速间隔' };
    return `策略组 ${item?.name || Number(group[1]) + 1} 的${part[group[2]] || '设置'}`;
  }
  const names: Record<string, string> = { 'client.mode': '运行模式', 'client.mixedPort': '混合端口', 'client.logLevel': '日志级别', rules: '规则列表', groups: '策略组' };
  return names[field] || field || '客户端策略';
}

async function applyPreset(id: string) {
  const preset = presetTemplates.value.find(item => item.id === id);
  if (!preset) {
    showToast('预设尚未载入，请稍后重试', 'error');
    return;
  }
  const previous = clientConfig.value;
  const next: ClientPolicyConfig = JSON.parse(JSON.stringify(previous));
  next.activePreset = id;
  next.settings = { ...preset.template.client };
  next.proxyGroups = preset.template.groups.map(group => ({ name: group.name, type: group.type, proxies: group.members.join(', '), url: group.testUrl || undefined, interval: group.intervalSeconds || undefined }));
  next.rules = preset.template.rules.map(rule => ({ ...rule, type: rule.type as ClientRuleType }));
  clientConfig.value = next;
  if (!(await persist(`已套用「${preset.name}」预设，用户更新订阅后生效`))) {
    clientConfig.value = previous;
  }
}

function openAddGroupDrawer() {
  clientConfig.value.proxyGroups.push({
    name: `分组${clientConfig.value.proxyGroups.length + 1}`,
    type: 'select',
    proxies: 'PROXY, DIRECT'
  });
  showToast('已添加新策略组，点击「保存策略配置」后生效');
}

async function removeGroup(idx: number) {
  if (clientConfig.value.proxyGroups.length <= 1) {
    showToast('至少保留一个主策略组', 'warning');
    return;
  }
  const removed = clientConfig.value.proxyGroups.splice(idx, 1);
  if (!(await persist('策略组已删除'))) {
    clientConfig.value.proxyGroups.splice(idx, 0, ...removed);
  }
}

function openCreateRuleDrawer() {
  editingRule.value = null;
  const maxPriority = clientConfig.value.rules.filter(r => r.type !== 'MATCH').reduce((max, r) => Math.max(max, r.priority), 0);
  form.value = {
    type: 'DOMAIN-SUFFIX',
    payload: '',
    target: 'PROXY',
    noResolve: false,
    enabled: true,
    priority: maxPriority + 5,
    description: ''
  };
  drawerVisible.value = true;
}

function openEditDrawer(rule: ClientRule) {
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
  drawerVisible.value = true;
}

async function saveRule() {
  const before = JSON.parse(JSON.stringify(clientConfig.value.rules)) as ClientRule[];
  if (editingRule.value) {
    const idx = clientConfig.value.rules.findIndex(r => r.id === editingRule.value?.id);
    if (idx !== -1) {
      clientConfig.value.rules[idx] = { ...editingRule.value, ...form.value };
    }
  } else {
    clientConfig.value.rules.push({ id: `cr-${Date.now()}`, ...form.value });
  }
  if (await persist('客户端规则已更新，用户更新订阅后生效')) {
    drawerVisible.value = false;
  } else {
    clientConfig.value.rules = before;
  }
}

async function toggleRule(rule: ClientRule) {
  rule.enabled = !rule.enabled;
  if (!(await persist(rule.enabled ? '规则已启用' : '规则已停用'))) {
    rule.enabled = !rule.enabled;
  }
}

async function moveRule(index: number, delta: number) {
  const current = sortedRules.value[index];
  const target = sortedRules.value[index + delta];
  if (!current || !target) return;
  const tempPriority = current.priority;
  current.priority = target.priority;
  target.priority = tempPriority;
  if (!(await persist('规则顺序已更新'))) {
    target.priority = current.priority;
    current.priority = tempPriority;
  }
}

function promptDelete(rule: ClientRule) {
  deletingRule.value = rule;
  confirmDeleteVisible.value = true;
}

async function executeDelete() {
  if (!deletingRule.value) return;
  const before = clientConfig.value.rules;
  clientConfig.value.rules = before.filter(r => r.id !== deletingRule.value?.id);
  if (!(await persist('规则已删除'))) {
    clientConfig.value.rules = before;
  }
}

async function resetToDefault() {
  if (confirm('确定恢复默认客户端策略模板吗？当前自定义规则将被重置。')) {
    await applyPreset('standard');
  }
}

async function saveAll() {
  await persist('客户端策略配置已全部保存，用户更新订阅后即刻生效');
}

function getRuleSetName(payload: string): string {
  const id = payload.startsWith('service:') ? payload.slice('service:'.length) : '';
  const set = serviceSets.value.find(s => s.id === id);
  return set ? set.name : (payload || '未指定规则集');
}

function getRuleTypeBadgeClass(type: ClientRuleType): string {
  switch (type) {
    case 'RULE-SET':
      return 'bg-purple-50 text-purple-700';
    case 'IP-CIDR':
    case 'IP-CIDR6':
      return 'bg-emerald-50 text-emerald-700';
    case 'MATCH':
      return 'bg-neutral-100 text-neutral-600';
    case 'GEOIP':
    case 'GEOSITE':
      return 'bg-amber-50 text-amber-700';
    default:
      return 'bg-blue-50 text-ios-blue';
  }
}

function formatActionLabel(target: string): string {
  switch (target) {
    case 'DIRECT':
      return '用户设备直连';
    case 'REJECT':
      return '客户端阻断';
    case 'PROXY':
      return '策略组 · PROXY';
    case 'AUTO':
      return '策略组 · 自动测速';
    case 'FALLBACK':
      return '策略组 · 故障转移';
    default:
      if (target.startsWith('node:')) {
        const node = availableNodes.value.find(item => item.id === target.slice('node:'.length));
        return `节点: ${node ? node.name : '已删除的节点'}`;
      }
      return `目标: ${target}`;
  }
}

function getActionBadgeClass(target: string): string {
  switch (target) {
    case 'DIRECT':
      return 'bg-emerald-50 text-emerald-700';
    case 'REJECT':
      return 'bg-red-50 text-red-600';
    case 'PROXY':
      return 'bg-blue-50 text-ios-blue';
    case 'AUTO':
      return 'bg-purple-50 text-purple-700';
    case 'FALLBACK':
      return 'bg-amber-50 text-amber-700';
    default:
      return 'bg-neutral-100 text-neutral-800';
  }
}

function getActionDotClass(target: string): string {
  switch (target) {
    case 'DIRECT':
      return 'bg-emerald-500';
    case 'REJECT':
      return 'bg-red-500';
    case 'PROXY':
      return 'bg-ios-blue';
    case 'AUTO':
      return 'bg-purple-500';
    case 'FALLBACK':
      return 'bg-amber-500';
    default:
      return 'bg-neutral-600';
  }
}

function getPayloadPlaceholder(type: ClientRuleType): string {
  switch (type) {
    case 'DOMAIN':
      return '例如: google.com';
    case 'DOMAIN-SUFFIX':
      return '例如: openai.com';
    case 'DOMAIN-KEYWORD':
      return '例如: netflix';
    case 'IP-CIDR':
      return '例如: 192.168.0.0/16 或 10.0.0.0/8';
    case 'IP-CIDR6':
      return '例如: 2001:db8::/32';
    case 'DST-PORT':
      return '例如: 80/443/8000-8100';
    case 'GEOIP':
      return '例如: CN 或 LAN';
    case 'GEOSITE':
      return '例如: category-ads-all';
    default:
      return '';
  }
}

// The preview is rendered by the server for one user and one format, exactly as
// that user's subscription would return it, with the warnings the Owner needs.
const previewUsers = ref<ProxyUser[]>([]);
const previewUserId = ref('');
const previewFormat = ref('clash');
const previewBody = ref('');
const previewWarnings = ref<{ stage: string; ref: string; message: string }[]>([]);
const previewHint = computed(() => isOwner.value ? '选择一个用户查看其订阅中实际生成的配置。' : '只有 Owner 可以查看用户订阅的渲染结果。');

async function loadPreview() {
  previewBody.value = '';
  previewWarnings.value = [];
  if (!previewUserId.value) return;
  try {
    const result = await request<{ body: string; warnings: { stage: string; ref: string; message: string }[] }>(
      `/users/${encodeURIComponent(previewUserId.value)}/subscription-preview?format=${encodeURIComponent(previewFormat.value)}`
    );
    previewBody.value = result.body;
    previewWarnings.value = result.warnings;
  } catch (e) {
    previewBody.value = `# 预览失败: ${(e as Error).message}`;
  }
}

function copyConfig() {
  if (!previewBody.value) return;
  navigator.clipboard.writeText(previewBody.value);
  copied.value = true;
  showToast('完整配置 YAML 已复制到剪贴板');
  setTimeout(() => {
    copied.value = false;
  }, 2500);
}

onMounted(load);
</script>
