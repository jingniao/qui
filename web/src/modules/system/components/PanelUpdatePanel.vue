<template>
  <section class="panel-card p-6 max-w-3xl space-y-5 text-sm">
    <div class="flex flex-wrap justify-between items-center gap-3">
      <h2 class="text-base font-semibold leading-normal">面板更新</h2>
      <button v-if="!busy" class="ios-btn-secondary" :disabled="checking" @click="check">{{ checking ? '检查中…' : '检查更新' }}</button>
    </div>
    <ErrorState v-if="error" :message="error" @retry="check" />
    <LoadingSkeleton v-else-if="!state" />
    <template v-else>
      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <div class="text-xs text-gray-500">当前版本</div>
          <div class="mt-1 font-mono">{{ currentVersionText }}</div>
        </div>
        <div>
          <div class="text-xs text-gray-500">最新正式版</div>
          <div v-if="latest" class="mt-1 space-y-1">
            <div class="font-mono">
              <a :href="latest.url" target="_blank" rel="noopener noreferrer" class="text-ios-blue hover:underline">v{{ latest.version }}</a>
            </div>
            <div v-if="latest.publishedAt" class="text-xs text-gray-500">发布于 {{ formatTime(latest.publishedAt) }}</div>
          </div>
          <div v-else-if="state.latestError" class="mt-1 text-ios-red">无法获取最新正式版本，请稍后重试。</div>
          <div v-else class="mt-1">—</div>
        </div>
      </div>
      <p v-if="unsupportedMessage" class="text-xs text-gray-500 leading-relaxed">{{ unsupportedMessage }}</p>
      <p v-else-if="stalledHint" class="text-xs text-ios-red leading-relaxed">{{ stalledHint }}</p>
      <p v-else-if="interruptedHint" class="text-xs text-ios-red leading-relaxed">{{ interruptedHint }}</p>
      <p v-else-if="timeoutHint" class="text-xs text-ios-red leading-relaxed">{{ timeoutHint }}</p>
      <p v-else-if="resultMessage" class="text-sm leading-relaxed" :class="resultTone">{{ resultMessage }}</p>
      <p v-if="busy && !resultMessage" class="text-sm text-gray-600">正在更新…面板服务会重启，页面断开十几到几十秒。请保持此页打开。</p>
      <div v-if="showUpdateButton || reloadAfterUpdate" class="flex flex-wrap justify-end gap-2">
        <button v-if="reloadAfterUpdate" class="ios-btn-primary" @click="reloadPage">刷新页面</button>
        <button v-if="showUpdateButton" class="ios-btn-primary" @click="confirmation = true">
          {{ `更新到 v${state.latest?.version ?? ''}` }}
        </button>
      </div>
    </template>
    <ConfirmDialog
      v-model="confirmation"
      title="确认更新面板"
      :message="`将把面板更新到 v${state?.latest?.version ?? ''}。更新期间面板服务会重启，页面会断开十几到几十秒。`"
      warning-details="更新前会自动备份数据库；新版本无法稳定运行时会自动回到当前版本。只更新面板，不更新节点 Agent。"
      confirm-text="确认更新"
      @confirm="submit"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import { formatTime } from '../../../app/auth';
import { showToast } from '../../../app/api';
import { APIError } from '../../../app/apiClient';
import ErrorState from '../../../components/ErrorState.vue';
import LoadingSkeleton from '../../../components/LoadingSkeleton.vue';
import ConfirmDialog from '../../../components/ConfirmDialog.vue';
import { settingsAPI, type PanelUpdateInfo, type PanelUpdateStatus } from '../settingsApi';

// The panel cannot replace its own binary: it writes a request file for a root
// helper, which runs qui-update (backup, health check, rollback). The panel service
// therefore restarts under us, so the poll below treats network errors, 502 and a
// refused connection as expected and keeps asking.
const POLL_INTERVAL_MS = 3000;
const POLL_LIMIT_MS = 15 * 60 * 1000;
// A finished status is only news while it is recent; after the update the page reloads
// within minutes, so anything older is a result from another day.
const RECENT_STATUS_MS = 24 * 60 * 60 * 1000;

const state = ref<PanelUpdateInfo | null>(null), error = ref('');
const checking = ref(false), updating = ref(false), confirmation = ref(false);
const timeoutHint = ref(''), finished = ref<PanelUpdateStatus | null>(null);
// Set only when this page's poll saw the request it follows succeed: a status merely
// loaded on mount is shown, but reloading the page for it would gain nothing.
const reloadAfterUpdate = ref(false);
// The request this page follows: the one it submitted, or the one the panel reports
// as pending/running when the card is opened again after the restart.
const watchedRequestId = ref('');
let pollTimer: number | undefined, pollStartedAt = 0, lastStatusRendered = false;

const latest = computed(() => state.value?.latest ?? null);
const currentVersionText = computed(() => state.value?.currentVersion ? `v${state.value.currentVersion}` : '未标注版本（开发版本）');
// Work in progress is a fresh request nothing has answered or a running update no
// reboot interrupted; a stalled request or an interrupted run is offered again below.
const pending = computed(() => {
  const request = state.value?.pending;
  if (request) return !request.stalled;
  const status = state.value?.status;
  return status?.state === 'running' && !status.interrupted;
});
const busy = computed(() => updating.value || pending.value);

const unsupportedMessage = computed(() => {
  if (state.value?.unsupportedReason === 'dev_build') return '开发版本不支持网页更新。';
  if (state.value?.unsupportedReason === 'helper_missing') return '这台面板还没有安装网页更新组件：请先在服务器上执行一次 qui-update --component panel，之后即可在这里更新。';
  return '';
});
// Two ways an update ends without ever reaching a finished status: the request the helper
// never picked up, and the run a reboot killed. Both say what to check and offer the
// update again instead of blocking the card.
const stalledHint = computed(() => state.value?.pending?.stalled
  ? '更新请求超过 10 分钟没有被服务器处理。请在服务器上执行 systemctl status proxy-panel-update.path 检查更新组件，然后重新提交。'
  : '');
const interruptedHint = computed(() => (state.value?.status?.state === 'running' && state.value.status.interrupted)
  ? '上次更新没有完成就被中断了（例如服务器重启）；服务器开机时会自动回到更新前的版本。可以重新提交更新。'
  : '');
// Owner UI rule: an action that cannot be taken right now is not shown at all.
const showUpdateButton = computed(() => !!state.value && state.value.updateAvailable && !busy.value);
const resultMessage = computed(() => {
  const status = finished.value;
  if (!status || status.state === 'running') return '';
  switch (status.code) {
    case 'succeeded': return `面板已更新到 v${status.version}。`;
    case 'not_newer': return `服务器上的更新器认为 v${status.version} 不比当前版本新，未执行更新。`;
    case 'invalid_request': return '服务器上的更新器认为这次请求不合法，未执行更新。请刷新页面后重试。';
    case 'update_failed': return `更新失败，已自动回到 v${status.previousVersion || state.value?.currentVersion || ''}。`;
    case 'version_mismatch': return `更新器拿到的版本与 v${status.version} 不一致，已放弃更新。`;
    default: return '更新未完成，请查看服务器上的更新器日志。';
  }
});
const resultTone = computed(() => finished.value?.state === 'succeeded' ? 'text-ios-green' : 'text-ios-red');


async function load(refresh: boolean, silent = false): Promise<PanelUpdateInfo | null> {
  try {
    const info = await settingsAPI.panelUpdate(refresh);
    state.value = info;
    error.value = '';
    return info;
  } catch (e) {
    if (!silent) error.value = (e as Error).message;
    return null;
  }
}

// One read of the helper's answer. A failure is expected while the panel restarts,
// so it is silent and the caller keeps polling.
async function poll(): Promise<boolean> {
  const info = await load(false, true);
  const status = info?.status;
  if (!status || status.requestId !== watchedRequestId.value || status.state === 'running') return false;
  finished.value = status;
  reloadAfterUpdate.value = status.state === 'succeeded';
  timeoutHint.value = '';
  stopPolling();
  return true;
}

function stopPolling() {
  if (pollTimer !== undefined) window.clearTimeout(pollTimer);
  pollTimer = undefined;
  updating.value = false;
}

function startPolling() {
  if (pollTimer !== undefined) return;
  updating.value = true;
  pollStartedAt = Date.now();
  timeoutHint.value = '';
  const tick = async () => {
    pollTimer = undefined;
    if (await poll()) return;
    if (Date.now() - pollStartedAt > POLL_LIMIT_MS) {
      stopPolling();
      timeoutHint.value = '长时间没有收到更新结果。请在服务器上执行 journalctl -u proxy-panel-update 查看更新器日志。';
      return;
    }
    pollTimer = window.setTimeout(tick, POLL_INTERVAL_MS);
  };
  pollTimer = window.setTimeout(tick, POLL_INTERVAL_MS);
}

// A pending request or a running update is followed even when another session
// submitted it: the status file is the only source of truth. A stalled request and an
// interrupted run are not followed: nothing is going to write their result.
function adoptRunning(info: PanelUpdateInfo) {
  const request = info.pending, status = info.status;
  if (request?.stalled) return;
  if (status?.state === 'running' && status.interrupted) return;
  const requestId = request?.requestId || (status?.state === 'running' ? status.requestId : '');
  if (!requestId) return;
  watchedRequestId.value = requestId;
  startPolling();
}

async function check() {
  if (checking.value || updating.value) return;
  checking.value = true;
  await load(true);
  checking.value = false;
}

function updateErrorText(e: unknown) {
  if (e instanceof APIError) {
    switch (e.code) {
      case 'panel_update_unsupported': return '当前面板不支持网页更新。';
      case 'panel_update_busy': return '已有待处理或正在进行的面板更新，请等待完成。';
      case 'panel_update_check_failed': return '无法获取最新正式版本，请稍后重试。';
      case 'panel_update_stale': return '最新正式版已变化，请重新检查更新后再试。';
    }
  }
  return (e as Error).message;
}

async function submit() {
  const target = latest.value?.version;
  if (!target || updating.value) return;
  updating.value = true;
  try {
    const answer = await settingsAPI.startPanelUpdate(target);
    watchedRequestId.value = answer.requestId;
    finished.value = null;
    reloadAfterUpdate.value = false;
    showToast(`已提交面板更新：v${answer.version}`);
    startPolling();
  } catch (e) {
    updating.value = false;
    showToast(updateErrorText(e), 'error');
    if (e instanceof APIError && e.code === 'panel_update_stale') void check();
  }
}

function reloadPage() { window.location.reload(); }

// The last finished status is shown once, which is what the Owner needs after the
// reload the update forces; an older result is no longer news.
function recentlyFinished(status: PanelUpdateStatus): boolean {
  const finishedAt = Date.parse(status.finishedAt);
  return Number.isFinite(finishedAt) && Date.now() - finishedAt < RECENT_STATUS_MS;
}

onMounted(async () => {
  const info = await load(false);
  if (info?.status && !lastStatusRendered) {
    lastStatusRendered = true;
    if (info.status.state !== 'running' && recentlyFinished(info.status)) finished.value = info.status;
  }
  if (info) adoptRunning(info);
});
onUnmounted(stopPolling);
</script>
