<template>
  <div class="space-y-6">
    <!-- 1. Notification Rules Section -->
    <section class="panel-card p-6 max-w-4xl space-y-5">
      <div class="flex flex-wrap justify-between items-center gap-3">
        <div>
          <h2 class="text-base font-semibold leading-normal">通知规则</h2>
          <p class="text-xs text-gray-500 mt-1">配置监控指标的触发阈值、持续窗口与告警冷却间隔。</p>
        </div>
        <div class="flex items-center gap-2">
          <button
            class="ios-btn-secondary"
            :disabled="rulesLoading || rulesSaving"
            @click="loadRules"
          >
            刷新规则
          </button>
          <button
            class="ios-btn-primary"
            :disabled="rulesLoading || rulesSaving"
            @click="saveRules"
          >
            {{ rulesSaving ? '保存中…' : '保存规则' }}
          </button>
        </div>
      </div>

      <div v-if="rulesLoading && rules.length === 0" class="py-6 text-center text-xs text-ios-tertiary">
        加载中…
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm text-left border-collapse">
          <thead>
            <tr class="border-b border-black/[0.06] text-xs text-ios-tertiary">
              <th class="py-2.5 px-3 font-medium">启用</th>
              <th class="py-2.5 px-3 font-medium">规则类型</th>
              <th class="py-2.5 px-3 font-medium">触发阈值</th>
              <th class="py-2.5 px-3 font-medium">持续时间</th>
              <th class="py-2.5 px-3 font-medium">告警冷却</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-black/[0.04]">
            <tr v-for="rule in rules" :key="rule.kind" class="hover:bg-black/[0.01]">
              <!-- Switch -->
              <td class="py-3 px-3">
                <input
                  type="checkbox"
                  v-model="rule.enabled"
                  class="rounded text-ios-blue focus:ring-ios-blue"
                />
              </td>
              <!-- Kind & Description -->
              <td class="py-3 px-3">
                <div class="font-medium text-ios-label">{{ ruleKindLabel(rule.kind) }}</div>
                <div class="text-[11px] text-ios-tertiary">{{ ruleKindDesc(rule.kind) }}</div>
              </td>
              <!-- Threshold -->
              <td class="py-3 px-3">
                <div v-if="hasThreshold(rule.kind)" class="flex items-center gap-1">
                  <input
                    type="number"
                    min="1"
                    max="100"
                    v-model.number="rule.threshold"
                    class="form-input-base w-20 py-1 text-xs"
                  />
                  <span class="text-xs text-ios-tertiary">%</span>
                </div>
                <span v-else class="text-xs text-ios-tertiary">&mdash;</span>
              </td>
              <!-- Duration -->
              <td class="py-3 px-3">
                <div v-if="hasDuration(rule.kind)" class="flex items-center gap-1">
                  <input
                    type="number"
                    min="0"
                    max="1440"
                    v-model.number="rule.durationMinutes"
                    class="form-input-base w-20 py-1 text-xs"
                  />
                  <span class="text-xs text-ios-tertiary">分钟</span>
                </div>
                <span v-else class="text-xs text-ios-tertiary">&mdash;</span>
              </td>
              <!-- Cooldown -->
              <td class="py-3 px-3">
                <div class="flex items-center gap-1">
                  <input
                    type="number"
                    min="0"
                    max="1440"
                    v-model.number="rule.cooldownMinutes"
                    class="form-input-base w-20 py-1 text-xs"
                  />
                  <span class="text-xs text-ios-tertiary">分钟</span>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 2. Notification Channels Section -->
    <section class="panel-card p-6 max-w-4xl space-y-5">
      <div class="flex flex-wrap justify-between items-center gap-3">
        <div>
          <h2 class="text-base font-semibold leading-normal">通知渠道</h2>
          <p class="text-xs text-gray-500 mt-1">配置将告警与通知外发至外部平台（如 Telegram Bot 或自定义 Webhook）。</p>
        </div>
        <button
          type="button"
          class="ios-btn-primary"
          @click="openAddChannel"
        >
          添加渠道
        </button>
      </div>

      <div v-if="channelsLoading && channels.length === 0" class="py-6 text-center text-xs text-ios-tertiary">
        加载中…
      </div>
      <div v-else-if="channels.length === 0" class="py-8 text-center text-xs text-ios-tertiary border border-dashed border-black/[0.08] rounded-xl">
        暂未配置任何通知渠道。添加渠道后，产生的告警与通知将自动推送。
      </div>
      <div v-else class="overflow-x-auto">
        <table class="w-full text-sm text-left border-collapse">
          <thead>
            <tr class="border-b border-black/[0.06] text-xs text-ios-tertiary">
              <th class="py-2.5 px-3 font-medium">启用</th>
              <th class="py-2.5 px-3 font-medium">渠道名称</th>
              <th class="py-2.5 px-3 font-medium">类型</th>
              <th class="py-2.5 px-3 font-medium">密钥/地址摘要</th>
              <th class="py-2.5 px-3 font-medium">最近投递状态</th>
              <th class="py-2.5 px-3 font-medium text-right">操作</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-black/[0.04]">
            <tr v-for="ch in channels" :key="ch.id" class="hover:bg-black/[0.01]">
              <td class="py-3 px-3">
                <input
                  type="checkbox"
                  :checked="ch.enabled"
                  @change="toggleChannelEnabled(ch)"
                  class="rounded text-ios-blue focus:ring-ios-blue"
                />
              </td>
              <td class="py-3 px-3 font-medium text-ios-label">
                {{ ch.name }}
              </td>
              <td class="py-3 px-3">
                <span class="px-2 py-0.5 text-xs rounded-full bg-black/[0.05] font-medium text-ios-secondary">
                  {{ ch.kind === 'telegram' ? 'Telegram' : 'Webhook' }}
                </span>
              </td>
              <td class="py-3 px-3 text-xs text-ios-secondary font-mono">
                <span v-if="ch.last4">***{{ ch.last4 }}</span>
                <span v-else class="text-ios-tertiary">&mdash;</span>
              </td>
              <td class="py-3 px-3 text-xs">
                <div class="flex flex-col">
                  <span :class="deliveryCodeClass(ch.lastResultCode)">
                    {{ deliveryCodeLabel(ch.lastResultCode) }}
                  </span>
                  <span v-if="ch.lastSentAt" class="text-[10px] text-ios-tertiary mt-0.5">
                    {{ formatTimestamp(ch.lastSentAt) }}
                  </span>
                </div>
              </td>
              <td class="py-3 px-3 text-right">
                <div class="flex items-center justify-end gap-2 text-xs">
                  <button
                    type="button"
                    :disabled="testingChannelId === ch.id"
                    @click="testChannel(ch)"
                    class="text-ios-blue hover:text-blue-700 disabled:opacity-50"
                  >
                    {{ testingChannelId === ch.id ? '测试中…' : '发送测试' }}
                  </button>
                  <button
                    type="button"
                    @click="openEditChannel(ch)"
                    class="text-ios-label hover:text-black"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    @click="deleteChannel(ch)"
                    class="text-ios-red hover:text-red-700"
                  >
                    删除
                  </button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- Channel Modal (Add / Edit) -->
    <AppDialog
      v-model="channelModalOpen"
      :title="isEditingChannel ? '编辑通知渠道' : '添加通知渠道'"
      :description="isEditingChannel ? '修改渠道配置。密钥字段留空表示保持原有已保存密钥。' : '新增一个通知外发渠道。'"
      @close="channelModalOpen = false"
    >
      <div class="space-y-4 text-sm">
        <label class="block">
          渠道类型
          <select
            v-model="channelForm.kind"
            :disabled="isEditingChannel"
            class="form-input-base mt-1.5 w-full disabled:bg-gray-100"
          >
            <option value="telegram">Telegram Bot</option>
            <option value="webhook">Webhook (HTTPS)</option>
          </select>
        </label>

        <label class="block">
          渠道名称
          <input
            v-model.trim="channelForm.name"
            placeholder="例如：运维告警群"
            maxlength="64"
            class="form-input-base mt-1.5 w-full"
          />
        </label>

        <div class="flex items-center gap-2 pt-1">
          <input
            type="checkbox"
            id="channel-enabled"
            v-model="channelForm.enabled"
            class="rounded text-ios-blue focus:ring-ios-blue"
          />
          <label for="channel-enabled" class="text-sm font-medium text-ios-label cursor-pointer">
            启用该渠道
          </label>
        </div>

        <!-- Telegram specific fields -->
        <template v-if="channelForm.kind === 'telegram'">
          <label class="block">
            Bot Token
            <input
              v-model.trim="channelForm.botToken"
              :placeholder="isEditingChannel ? '留空表示保留原 Token' : '例如：123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11'"
              class="form-input-base mt-1.5 w-full font-mono text-xs"
            />
          </label>
          <label class="block">
            Chat ID
            <input
              v-model.trim="channelForm.chatId"
              :placeholder="isEditingChannel ? '留空表示保留原 Chat ID' : '例如：-1001234567890'"
              class="form-input-base mt-1.5 w-full font-mono text-xs"
            />
          </label>
        </template>

        <!-- Webhook specific fields -->
        <template v-if="channelForm.kind === 'webhook'">
          <label class="block">
            Webhook URL (须为 HTTPS)
            <input
              v-model.trim="channelForm.url"
              :placeholder="isEditingChannel ? '留空表示保留原 URL' : 'https://example.com/webhook'"
              class="form-input-base mt-1.5 w-full font-mono text-xs"
            />
          </label>
        </template>

        <p v-if="channelFormError" class="text-xs text-ios-red font-medium">
          {{ channelFormError }}
        </p>
      </div>

      <template #footer>
        <button
          type="button"
          @click="channelModalOpen = false"
          class="ios-dialog-btn-secondary"
        >
          取消
        </button>
        <button
          type="button"
          :disabled="channelSaving"
          @click="submitChannel"
          class="ios-dialog-btn-primary"
        >
          {{ channelSaving ? '保存中…' : '确认保存' }}
        </button>
      </template>
    </AppDialog>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue';
import { showToast } from '../../../app/api';
import AppDialog from '../../../components/AppDialog.vue';
import {
  notificationsAPI,
  type NotificationRule,
  type NotificationChannel,
  type NotificationChannelInput
} from '../notificationsApi';

// 1. Rules Logic
const rules = ref<NotificationRule[]>([]);
const rulesLoading = ref(false);
const rulesSaving = ref(false);

function ruleKindLabel(kind: string): string {
  switch (kind) {
    case 'node_offline': return '节点离线';
    case 'cpu': return 'CPU 占用过高';
    case 'memory': return '内存占用过高';
    case 'disk': return '磁盘占用过高';
    case 'public_ip': return '公网 IP 变化';
    case 'machine_traffic': return '整机流量超额';
    case 'system_alert': return '系统告警';
    default: return kind;
  }
}

function ruleKindDesc(kind: string): string {
  switch (kind) {
    case 'node_offline': return '节点超过指定时间没有心跳上报';
    case 'cpu': return '节点 CPU 使用率持续超过阈值';
    case 'memory': return '节点内存使用率持续超过阈值';
    case 'disk': return '节点磁盘使用率超过阈值';
    case 'public_ip': return '检测到节点公网出口 IP 发生变更';
    case 'machine_traffic': return '整机累计流量达到设定额度百分比';
    case 'system_alert': return '包含证书续期、发布、服务器续费等系统重要事件';
    default: return '';
  }
}

function hasThreshold(kind: string): boolean {
  return ['cpu', 'memory', 'disk', 'machine_traffic'].includes(kind);
}

function hasDuration(kind: string): boolean {
  return ['node_offline', 'cpu', 'memory', 'disk'].includes(kind);
}

async function loadRules() {
  try {
    rulesLoading.value = true;
    rules.value = await notificationsAPI.rules();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    rulesLoading.value = false;
  }
}

async function saveRules() {
  try {
    rulesSaving.value = true;
    for (const r of rules.value) {
      if (hasThreshold(r.kind) && (r.threshold < 1 || r.threshold > 100)) {
        showToast(`规则「${ruleKindLabel(r.kind)}」阈值须在 1%～100% 之间`, 'error');
        return;
      }
      if (r.cooldownMinutes < 0 || r.cooldownMinutes > 1440) {
        showToast(`规则「${ruleKindLabel(r.kind)}」冷却时间须在 0～1440 分钟之间`, 'error');
        return;
      }
    }
    await notificationsAPI.saveRules(rules.value);
    showToast('通知规则已保存');
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    rulesSaving.value = false;
  }
}

// 2. Channels Logic
const channels = ref<NotificationChannel[]>([]);
const channelsLoading = ref(false);
const testingChannelId = ref<string | null>(null);

const channelModalOpen = ref(false);
const isEditingChannel = ref(false);
const channelSaving = ref(false);
const channelFormError = ref('');

const channelForm = reactive({
  id: '',
  kind: 'telegram',
  name: '',
  enabled: true,
  botToken: '',
  chatId: '',
  url: ''
});

function deliveryCodeLabel(code: string): string {
  switch (code) {
    case '': return '未发送 / 成功';
    case 'channel_timeout': return '连接超时';
    case 'channel_network': return '网络异常';
    case 'channel_http': return 'HTTP 错误响应';
    case 'channel_rejected': return '请求被拒绝 / 参数无效';
    default: return code;
  }
}

function deliveryCodeClass(code: string): string {
  if (!code) return 'text-ios-tertiary';
  return 'text-ios-red font-medium';
}

function formatTimestamp(ts: string): string {
  try {
    const d = new Date(ts);
    if (isNaN(d.getTime())) return ts;
    return d.toLocaleString();
  } catch {
    return ts;
  }
}

async function loadChannels() {
  try {
    channelsLoading.value = true;
    channels.value = await notificationsAPI.channels();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    channelsLoading.value = false;
  }
}

function openAddChannel() {
  isEditingChannel.value = false;
  channelForm.id = '';
  channelForm.kind = 'telegram';
  channelForm.name = '';
  channelForm.enabled = true;
  channelForm.botToken = '';
  channelForm.chatId = '';
  channelForm.url = '';
  channelFormError.value = '';
  channelModalOpen.value = true;
}

function openEditChannel(ch: NotificationChannel) {
  isEditingChannel.value = true;
  channelForm.id = ch.id;
  channelForm.kind = ch.kind;
  channelForm.name = ch.name;
  channelForm.enabled = ch.enabled;
  channelForm.botToken = '';
  channelForm.chatId = '';
  channelForm.url = '';
  channelFormError.value = '';
  channelModalOpen.value = true;
}

async function submitChannel() {
  if (!channelForm.name.trim()) {
    channelFormError.value = '请输入渠道名称';
    return;
  }

  const input: NotificationChannelInput = {
    id: isEditingChannel.value ? channelForm.id : undefined,
    kind: channelForm.kind,
    name: channelForm.name.trim(),
    enabled: channelForm.enabled
  };

  if (!isEditingChannel.value) {
    if (channelForm.kind === 'telegram') {
      if (!channelForm.botToken.trim() || !channelForm.chatId.trim()) {
        channelFormError.value = '请输入 Telegram Bot Token 与 Chat ID';
        return;
      }
      input.config = {
        botToken: channelForm.botToken.trim(),
        chatId: channelForm.chatId.trim()
      };
    } else if (channelForm.kind === 'webhook') {
      if (!channelForm.url.trim() || !channelForm.url.trim().startsWith('https://')) {
        channelFormError.value = '请输入以 https:// 开头的有效 Webhook 地址';
        return;
      }
      input.config = {
        url: channelForm.url.trim()
      };
    }
  } else {
    // Editing: if secrets provided, update config
    if (channelForm.kind === 'telegram' && (channelForm.botToken.trim() || channelForm.chatId.trim())) {
      input.config = {
        botToken: channelForm.botToken.trim(),
        chatId: channelForm.chatId.trim()
      };
    } else if (channelForm.kind === 'webhook' && channelForm.url.trim()) {
      if (!channelForm.url.trim().startsWith('https://')) {
        channelFormError.value = 'Webhook 地址须以 https:// 开头';
        return;
      }
      input.config = {
        url: channelForm.url.trim()
      };
    }
  }

  try {
    channelSaving.value = true;
    channelFormError.value = '';
    await notificationsAPI.saveChannel(input);
    showToast(isEditingChannel.value ? '渠道配置已更新' : '通知渠道已添加');
    channelModalOpen.value = false;
    await loadChannels();
  } catch (e) {
    channelFormError.value = (e as Error).message;
  } finally {
    channelSaving.value = false;
  }
}

async function toggleChannelEnabled(ch: NotificationChannel) {
  try {
    await notificationsAPI.saveChannel({
      id: ch.id,
      kind: ch.kind,
      name: ch.name,
      enabled: !ch.enabled
    });
    ch.enabled = !ch.enabled;
    showToast(`渠道「${ch.name}」已${ch.enabled ? '启用' : '禁用'}`);
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

async function testChannel(ch: NotificationChannel) {
  try {
    testingChannelId.value = ch.id;
    const res = await notificationsAPI.testChannel(ch.id);
    if (!res.code) {
      showToast('测试消息已成功发送');
    } else {
      showToast(`发送测试失败：${deliveryCodeLabel(res.code)}`, 'error');
    }
    await loadChannels();
  } catch (e) {
    showToast((e as Error).message, 'error');
  } finally {
    testingChannelId.value = null;
  }
}

async function deleteChannel(ch: NotificationChannel) {
  if (!confirm(`确认删除通知渠道「${ch.name}」？`)) return;
  try {
    await notificationsAPI.deleteChannel(ch.id);
    showToast(`已删除通知渠道「${ch.name}」`);
    await loadChannels();
  } catch (e) {
    showToast((e as Error).message, 'error');
  }
}

onMounted(() => {
  loadRules();
  loadChannels();
});
</script>
